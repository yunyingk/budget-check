package consumer

import (
	"budget/src/budget"
	"budget/src/ekb"
	"budget/src/types"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEvaluateRefusesPlainTextFlowDetailError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/openapi/v1/auth/getAccessToken" {
			_, _ = w.Write([]byte(`{"value":{"accessToken":"test-token"}}`))
			return
		}
		if r.URL.Path != "/api/openapi/v1.1/flowDetails/byCode" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("code"); got != "B26002975" {
			t.Fatalf("unexpected code: %s", got)
		}
		w.WriteHeader(http.StatusPreconditionFailed)
		_, _ = w.Write([]byte("单据已删除"))
	}))
	defer server.Close()

	checker := NewChecker(
		ekb.NewClient(server.URL, "app-key", "secret"),
		budget.NewStore(),
		map[string]string{"budget-check": "sign-key"},
		nil,
	)

	action, comment := checker.Evaluate(types.Task{
		Code:       "B26002975",
		WebhookKey: "budget-check",
	})

	if action != "refuse" {
		t.Fatalf("expected refuse, got %s", action)
	}
	if !strings.Contains(comment, "status=412") || !strings.Contains(comment, "单据已删除") {
		t.Fatalf("expected status and plain text body in comment, got %q", comment)
	}
}
