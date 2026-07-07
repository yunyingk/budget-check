package web

import (
	"budget/src/types"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleWebhookEmptyBodyUsesTestPath(t *testing.T) {
	enqueued := false
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/budget-check", nil)
	rec := httptest.NewRecorder()

	handleWebhook(rec, req, "budget-check", func(types.Task) bool {
		enqueued = true
		return true
	}, func(string) string {
		t.Fatal("genID should not be called for test webhook")
		return ""
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if enqueued {
		t.Fatal("empty test request should not be enqueued")
	}
	if !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("expected success response, got %s", rec.Body.String())
	}
}

func TestHandleWebhookEmptyJSONUsesTestPath(t *testing.T) {
	enqueued := false
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/budget-check", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	handleWebhook(rec, req, "budget-check", func(types.Task) bool {
		enqueued = true
		return true
	}, func(string) string {
		t.Fatal("genID should not be called for test webhook")
		return ""
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if enqueued {
		t.Fatal("empty test request should not be enqueued")
	}
}

func TestHandleWebhookMissingPartialFieldsIsBadRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/budget-check", strings.NewReader(`{"code":"B26000001"}`))
	rec := httptest.NewRecorder()

	handleWebhook(rec, req, "budget-check", func(types.Task) bool {
		t.Fatal("invalid webhook should not be enqueued")
		return true
	}, func(string) string {
		t.Fatal("genID should not be called for invalid webhook")
		return ""
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d, body=%s", rec.Code, rec.Body.String())
	}
}
