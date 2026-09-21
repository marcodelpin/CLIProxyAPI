package thinking_test

import (
	"errors"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/thinking/provider/codex"
	codexopenai "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/codex/openai/chat-completions"
	codexresponses "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/codex/openai/responses"
	"github.com/tidwall/gjson"
)

func TestAstraUltraTranslationAndThinking(t *testing.T) {
	for _, tc := range []struct {
		name   string
		model  string
		format string
		body   string
	}{
		{
			name: "responses body", model: "gpt-6-astra", format: "openai-response",
			body: `{"model":"gpt-6-astra","input":"hello","reasoning":{"effort":"ultra"}}`,
		},
		{
			name: "chat body", model: "gpt-6-astra", format: "openai",
			body: `{"model":"gpt-6-astra","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"ultra"}`,
		},
		{
			name: "suffix overrides body", model: "gpt-6-astra(ultra)", format: "openai-response",
			body: `{"model":"gpt-6-astra","input":"hello","reasoning":{"effort":"low"}}`,
		},
		{
			name: "case insensitive suffix", model: "gpt-6-astra(ULTRA)", format: "openai-response",
			body: `{"model":"gpt-6-astra","input":"hello","reasoning":{"effort":"low"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(tc.body)
			var translated []byte
			if tc.format == "openai" {
				translated = codexopenai.ConvertOpenAIRequestToCodex("gpt-6-astra", body, true)
			} else {
				translated = codexresponses.ConvertOpenAIResponsesRequestToCodex("gpt-6-astra", body, true)
			}
			applied, err := thinking.ApplyThinking(translated, tc.model, tc.format, "codex", "codex")
			if err != nil {
				t.Fatalf("ApplyThinking() error = %v", err)
			}
			if got := gjson.GetBytes(applied, "reasoning.effort").String(); got != "ultra" {
				t.Fatalf("upstream reasoning.effort = %q, want ultra", got)
			}
			if got := thinking.ExtractReasoningEffort(body, tc.format, tc.model); got != "ultra" {
				t.Errorf("request usage effort = %q, want ultra", got)
			}
			if got := thinking.ExtractTranslatedReasoningEffort(applied, "codex"); got != "ultra" {
				t.Errorf("upstream usage effort = %q, want ultra", got)
			}
		})
	}
}

func TestUltraValidationUsesModelCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name       string
		from       string
		to         string
		support    *registry.ThinkingSupport
		wantLevel  thinking.ThinkingLevel
		wantBudget int
		wantErr    bool
	}{
		{
			name: "supported Codex level", from: "openai-response", to: "codex",
			support: &registry.ThinkingSupport{Levels: []string{"low", "max", "ultra"}}, wantLevel: thinking.LevelUltra,
		},
		{
			name: "unsupported Codex level", from: "openai-response", to: "codex",
			support: &registry.ThinkingSupport{Levels: []string{"low", "xhigh"}}, wantErr: true,
		},
		{
			name: "cross provider clamp", from: "codex", to: "claude",
			support: &registry.ThinkingSupport{Levels: []string{"low", "high", "max"}}, wantLevel: thinking.LevelMax,
		},
		{
			name: "budget provider clamp", from: "codex", to: "gemini",
			support: &registry.ThinkingSupport{Min: 1024, Max: 32768}, wantBudget: 32768,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := thinking.ThinkingConfig{Mode: thinking.ModeLevel, Level: thinking.LevelUltra}
			model := &registry.ModelInfo{ID: "ultra-validation-test", Thinking: tc.support}
			got, err := thinking.ValidateConfig(config, model, tc.from, tc.to, false)
			if tc.wantErr {
				var thinkingErr *thinking.ThinkingError
				if !errors.As(err, &thinkingErr) || thinkingErr.Code != thinking.ErrLevelNotSupported {
					t.Fatalf("ValidateConfig() error = %v, want unsupported level", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateConfig() error = %v", err)
			}
			if got.Level != tc.wantLevel || got.Budget != tc.wantBudget {
				t.Fatalf("ValidateConfig() = %+v, want level=%q budget=%d", got, tc.wantLevel, tc.wantBudget)
			}
		})
	}
	for _, supportsMax := range []bool{false, true} {
		want := "high"
		if supportsMax {
			want = "max"
		}
		if got, ok := thinking.MapToClaudeEffort("ultra", supportsMax); !ok || got != want {
			t.Errorf("MapToClaudeEffort(ultra, %t) = %q, %t, want %q, true", supportsMax, got, ok, want)
		}
	}
}
