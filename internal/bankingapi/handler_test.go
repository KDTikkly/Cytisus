package bankingapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
)

func TestAdminCORSAllowsOnlyConfiguredOriginAndHeaders(t *testing.T) {
	t.Parallel()

	handler := NewAdmin(nil, config.EnvironmentTest, "http://localhost:3001")
	request := httptest.NewRequest(http.MethodOptions, "/internal/v1/admin/compliance/cases", nil)
	request.Header.Set("Origin", "http://localhost:3001")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected preflight status %d, got %d", http.StatusNoContent, recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3001" {
		t.Fatalf("expected configured admin origin, got %q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Headers"); got != "Authorization, Content-Type, Idempotency-Key, X-Admin-ID, X-Admin-Role" {
		t.Fatalf("unexpected allowed headers: %q", got)
	}
}

func TestAdminCORSRejectsOtherOrigin(t *testing.T) {
	t.Parallel()

	handler := NewAdmin(nil, config.EnvironmentTest, "http://localhost:3001")
	request := httptest.NewRequest(http.MethodOptions, "/internal/v1/admin/compliance/cases", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected allowed origin: %q", got)
	}
}
