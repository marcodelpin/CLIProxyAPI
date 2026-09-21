package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

func TestRequestExecutionMetadataPreferredAuth(t *testing.T) {
	for _, test := range []struct {
		name, header, want string
	}{
		{"absent", "", ""},
		{"blank", " \t ", ""},
		{"trimmed", " \t iauser02@example.test \t", "iauser02@example.test"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ginCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			ginCtx.Request.Header.Set("Idempotency-Key", "request-1")
			if test.header != "" {
				ginCtx.Request.Header.Set("X-CLIProxy-Preferred-Auth", test.header)
			}
			meta := requestExecutionMetadata(context.WithValue(context.Background(), "gin", ginCtx))
			got, exists := meta[coreexecutor.PreferredAuthMetadataKey]
			if test.want == "" {
				if exists {
					t.Fatal("empty preference added execution metadata")
				}
				encoded, errMarshal := json.Marshal(meta)
				if errMarshal != nil {
					t.Fatal(errMarshal)
				}
				if string(encoded) != `{"idempotency_key":"request-1","request_path":"/v1/messages"}` {
					t.Fatalf("metadata without preference changed: %s", encoded)
				}
			} else if got != test.want {
				t.Fatalf("preferred auth = %v, want %s", got, test.want)
			}
			if _, pinned := meta[coreexecutor.PinnedAuthMetadataKey]; pinned {
				t.Fatal("preference must not pin execution")
			}
		})
	}
}
