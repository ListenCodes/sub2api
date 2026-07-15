package accountmonitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeAdminBackend struct {
	request AdminRequest
	result  any
	err     error
}

func (f *fakeAdminBackend) ExecuteAdmin(_ context.Context, request AdminRequest) (any, error) {
	f.request = request
	if f.result == nil {
		f.result = map[string]any{"ok": true}
	}
	return f.result, f.err
}

func TestHandlerRoutesAdminEndpoints(t *testing.T) {
	tests := []struct {
		method   string
		path     string
		resource string
	}{
		{http.MethodGet, "/overview", ResourceOverview},
		{http.MethodGet, "/accounts", ResourceAccounts},
		{http.MethodGet, "/accounts/42", ResourceAccount},
		{http.MethodGet, "/accounts/42/models", ResourceModels},
		{http.MethodGet, "/accounts/42/users", ResourceUsers},
		{http.MethodGet, "/accounts/42/errors", ResourceErrors},
		{http.MethodGet, "/accounts/42/trends", ResourceTrends},
		{http.MethodGet, "/attempts", ResourceAttempts},
		{http.MethodGet, "/data-quality", ResourceDataQuality},
		{http.MethodGet, "/thresholds", ResourceThresholds},
		{http.MethodPut, "/thresholds", ResourceThresholds},
		{http.MethodPost, "/rebuild-jobs", ResourceRebuildJobs},
		{http.MethodGet, "/rebuild-jobs/7", ResourceRebuildJob},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			backend := &fakeAdminBackend{}
			handler := NewHandler(backend, "")
			body := ""
			if tt.method == http.MethodPut {
				body = `{"scope":"global","scope_id":0,"success_rate":0.9}`
			} else if tt.method == http.MethodPost {
				body = `{"from":"2026-07-01T00:00:00Z","to":"2026-07-02T00:00:00Z"}`
			}
			req := httptest.NewRequest(tt.method, "http://example.test"+tt.path+"?from=2026-07-01T00:00:00Z&to=2026-07-02T00:00:00Z&page=1&page_size=20", strings.NewReader(body))
			recorder := httptest.NewRecorder()

			handler.ServeAdmin(recorder, req, tt.path, 99)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if backend.request.Resource != tt.resource || backend.request.ActorID != 99 {
				t.Fatalf("request = %+v", backend.request)
			}
		})
	}
}

func TestHandlerRejectsInvalidRangeAndPageSize(t *testing.T) {
	backend := &fakeAdminBackend{}
	handler := NewHandler(backend, "")
	tests := []string{
		"/accounts?from=2026-07-02T00:00:00Z&to=2026-07-01T00:00:00Z",
		"/accounts?from=2026-01-01T00:00:00Z&to=2026-07-01T00:00:00Z",
		"/accounts?page_size=21",
		"/accounts?page_size=101",
	}
	for _, path := range tests {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test"+path, nil)
		handler.ServeAdmin(recorder, req, "/accounts", 99)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("path=%s status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestHandlerDecodesRebuildRangeAndCapsItAt31Days(t *testing.T) {
	backend := &fakeAdminBackend{}
	handler := NewHandler(backend, "")
	body, _ := json.Marshal(map[string]string{
		"from": time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		"to":   time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
	})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.test/rebuild-jobs", strings.NewReader(string(body)))
	handler.ServeAdmin(recorder, req, "/rebuild-jobs", 99)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestHandlerRejectsUnknownEndpoint(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewHandler(&fakeAdminBackend{}, "").ServeAdmin(recorder, httptest.NewRequest(http.MethodGet, "/secret", nil), "/secret", 99)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d", recorder.Code)
	}
}
