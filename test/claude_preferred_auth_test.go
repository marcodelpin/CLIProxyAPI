package test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	configaccess "github.com/router-for-me/CLIProxyAPI/v7/internal/access/config_access"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/api"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	runtimeexecutor "github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v7/sdk/access"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers/claude"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestClaudePreferredAuthQuotaFailover(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			const model = "gpt-6-astra"
			const preferred = "preferred@example.test"
			attempts := make(chan string, 8)
			var exhausted atomic.Bool
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				account := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
				attempts <- account
				if r.Header.Get("X-CLIProxy-Preferred-Auth") != "" {
					t.Error("routing preference leaked upstream")
				}
				if account == "preferred" && exhausted.Load() {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusTooManyRequests)
					_, errWrite := fmt.Fprint(w, `{"error":{"type":"usage_limit_reached","message":"Quota exhausted","resets_in_seconds":3600}}`)
					if errWrite != nil {
						t.Error(errWrite)
					}
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				// A real Codex stream carries its text in response.output_text.delta;
				// the codex->claude translator emits content_block_delta only from
				// those events, so a completed-only fixture would make the body
				// assertion below unable to see the payload in stream mode.
				for _, event := range []string{
					`{"type":"response.created","response":{"id":"resp-test","model":"gpt-6-astra"}}`,
					`{"type":"response.content_part.added"}`,
					`{"type":"response.output_text.delta","delta":"hello"}`,
					`{"type":"response.completed","response":{"id":"resp-test","model":"gpt-6-astra","status":"completed","output":[{"id":"msg-test","type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
				} {
					if _, errWrite := fmt.Fprint(w, "data: "+event+"\n\n"); errWrite != nil {
						t.Error(errWrite)
						return
					}
				}
			}))
			defer upstream.Close()

			selector := coreauth.NewSessionAffinitySelector(coreauth.NewPreferredAuthSelector(&coreauth.FillFirstSelector{}))
			defer selector.Stop()
			manager := coreauth.NewManager(nil, selector, nil)
			manager.SetRetryConfig(0, 0, 0)
			cfg := &config.Config{}
			manager.RegisterExecutor(runtimeexecutor.NewCodexExecutor(cfg))
			fallbackID := fmt.Sprintf("preferred-bridge-%t-a", stream)
			preferredID := fmt.Sprintf("preferred-bridge-%t-b", stream)
			for _, candidate := range []*coreauth.Auth{
				{ID: fallbackID, Provider: "codex", Label: "fallback@example.test", Status: coreauth.StatusActive,
					Attributes: map[string]string{"api_key": "fallback", "base_url": upstream.URL}},
				{ID: preferredID, Provider: "codex", Status: coreauth.StatusActive,
					Attributes: map[string]string{"api_key": "preferred", "base_url": upstream.URL},
					Metadata:   map[string]any{"email": preferred, "disable_cooling": false}},
			} {
				registry.GetGlobalRegistry().RegisterClient(candidate.ID, "codex", []*registry.ModelInfo{{ID: model}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(candidate.ID) })
				if _, errRegister := manager.Register(context.Background(), candidate); errRegister != nil {
					t.Fatal(errRegister)
				}
			}

			accessManager := sdkaccess.NewManager()
			cfg.APIKeys = []string{"bridge-test-token"}
			configaccess.Register(&cfg.SDKConfig)
			t.Cleanup(func() { configaccess.Register(nil) })
			accessManager.SetProviders(sdkaccess.RegisteredProviders())
			router := gin.New()
			bridge := claude.NewClaudeCodeAPIHandler(handlers.NewBaseAPIHandlers(&cfg.SDKConfig, manager))
			router.POST("/v1/messages", api.AuthMiddleware(accessManager), bridge.ClaudeMessages)
			run := func(preference, token string) *httptest.ResponseRecorder {
				t.Helper()
				body := fmt.Sprintf(`{"model":%q,"max_tokens":16,"stream":%t,"metadata":{"user_id":"user_bridge_session_preferred-auth"},"messages":[{"role":"user","content":"hello"}]}`, model, stream)
				request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
				request.Header.Set("Authorization", "Bearer "+token)
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("X-CLIProxy-Preferred-Auth", preference)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				if strings.Contains(recorder.Body.String(), preference) || recorder.Header().Get("X-CLIProxy-Preferred-Auth") != "" {
					t.Fatal("routing preference echoed to client")
				}
				return recorder
			}
			if denied := run(preferred, "invalid"); denied.Code != http.StatusUnauthorized || len(attempts) != 0 {
				t.Fatal("unauthenticated preference reached execution")
			}
			for i, preference := range []string{"  PREFERRED@EXAMPLE.TEST  ", "fallback@example.test", preferred} {
				if i == 1 {
					exhausted.Store(true)
				}
				response := run(preference, "bridge-test-token")
				if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "hello") {
					t.Fatalf("request %d: status=%d body=%s", i, response.Code, response.Body.String())
				}
			}
			for _, want := range []string{"preferred", "preferred", "fallback", "fallback"} {
				select {
				case got := <-attempts:
					if got != want {
						t.Fatalf("account = %s, want %s", got, want)
					}
				default:
					t.Fatalf("missing attempt for %s", want)
				}
			}
			if len(attempts) != 0 {
				t.Fatal("unexpected extra attempts")
			}
			cooled, _ := manager.GetByID(preferredID)
			if cooled.Quota.Reason != "credential_quota" || !cooled.Quota.NextRecoverAt.After(time.Now()) {
				t.Fatal("provider quota did not cool the preferred credential")
			}
		})
	}
}
