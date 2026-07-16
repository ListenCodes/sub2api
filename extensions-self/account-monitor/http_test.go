package accountmonitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
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
			handler := NewHandler(backend)
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
	handler := NewHandler(backend)
	tests := []string{
		"/accounts?from=2026-07-02T00:00:00Z&to=2026-07-01T00:00:00Z",
		"/accounts?from=2026-01-01T00:00:00Z&to=2026-07-01T00:00:00Z",
		"/accounts?min_risk_score=-1",
		"/accounts?max_risk_score=101",
		"/accounts?min_risk_score=not-a-number",
		"/accounts?min_risk_score=80&max_risk_score=20",
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

func TestHandlerAcceptsConfiguredPageSizes(t *testing.T) {
	for _, pageSize := range []int{5, 12, 20, 100, 1000} {
		t.Run(strconv.Itoa(pageSize), func(t *testing.T) {
			backend := &fakeAdminBackend{}
			recorder := httptest.NewRecorder()
			path := "/accounts?page_size=" + strconv.Itoa(pageSize)
			req := httptest.NewRequest(http.MethodGet, "http://example.test"+path, nil)

			NewHandler(backend).ServeAdmin(recorder, req, "/accounts", 99)

			if recorder.Code != http.StatusOK || backend.request.PageSize != pageSize {
				t.Fatalf("page_size=%d status=%d request=%+v body=%s", pageSize, recorder.Code, backend.request, recorder.Body.String())
			}
		})
	}

	for _, pageSize := range []int{4, 1001} {
		recorder := httptest.NewRecorder()
		path := "/accounts?page_size=" + strconv.Itoa(pageSize)
		req := httptest.NewRequest(http.MethodGet, "http://example.test"+path, nil)
		NewHandler(&fakeAdminBackend{}).ServeAdmin(recorder, req, "/accounts", 99)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("page_size=%d status=%d body=%s", pageSize, recorder.Code, recorder.Body.String())
		}
	}
}

func TestHandlerDecodesRebuildRangeAndCapsItAt31Days(t *testing.T) {
	backend := &fakeAdminBackend{}
	handler := NewHandler(backend)
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
	NewHandler(&fakeAdminBackend{}).ServeAdmin(recorder, httptest.NewRequest(http.MethodGet, "/secret", nil), "/secret", 99)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d", recorder.Code)
	}
}

func TestHandlerRoutesGroupMonitorUsingCompleteFixedBucketRange(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 7, 30, 0, time.UTC)
	tests := []struct {
		path     string
		resource string
		groupID  int64
	}{
		{path: "/group-monitor/groups?range=6h&page_size=12", resource: "group-monitor-groups"},
		{path: "/group-monitor/groups/42?range=24h", resource: "group-monitor-group", groupID: 42},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			backend := &fakeAdminBackend{}
			handler := NewHandler(backend)
			handler.now = func() time.Time { return now }
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://example.test"+test.path, nil)
			handler.ServeAdmin(recorder, req, strings.Split(test.path, "?")[0], 99)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if backend.request.Resource != test.resource || adminRequestGroupID(t, backend.request) != test.groupID {
				t.Fatalf("request = %+v", backend.request)
			}
			wantTo := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
			if !backend.request.To.Equal(wantTo) {
				t.Fatalf("range end = %s, want %s", backend.request.To, wantTo)
			}
			wantDuration := 6 * time.Hour
			if test.groupID > 0 {
				wantDuration = 24 * time.Hour
			}
			if backend.request.To.Sub(backend.request.From) != wantDuration {
				t.Fatalf("range = %s", backend.request.To.Sub(backend.request.From))
			}
		})
	}
}

func TestHandlerRoutesGroupMonitorRanges(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 7, 30, 0, time.UTC)
	tests := []struct {
		rangeValue    string
		duration      time.Duration
		bucketSeconds int64
	}{
		{rangeValue: "6h", duration: 6 * time.Hour, bucketSeconds: 900},
		{rangeValue: "24h", duration: 24 * time.Hour, bucketSeconds: 3600},
		{rangeValue: "7d", duration: 7 * 24 * time.Hour, bucketSeconds: 25200},
		{rangeValue: "30d", duration: 30 * 24 * time.Hour, bucketSeconds: 108000},
	}
	for _, test := range tests {
		t.Run(test.rangeValue, func(t *testing.T) {
			backend := &fakeAdminBackend{}
			handler := NewHandler(backend)
			handler.now = func() time.Time { return now }
			path := "/group-monitor/groups?range=" + test.rangeValue
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://example.test"+path, nil)

			handler.ServeAdmin(recorder, req, "/group-monitor/groups", 99)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if got := backend.request.To.Sub(backend.request.From); got != test.duration {
				t.Fatalf("duration=%s, want %s", got, test.duration)
			}
			if got := adminRequestBucketSeconds(t, backend.request); got != test.bucketSeconds {
				t.Fatalf("bucket_seconds=%d, want %d", got, test.bucketSeconds)
			}
		})
	}
}

func adminRequestGroupID(t *testing.T, request AdminRequest) int64 {
	t.Helper()
	field := reflect.ValueOf(request).FieldByName("GroupID")
	if !field.IsValid() {
		t.Fatal("AdminRequest is missing GroupID")
	}
	return field.Int()
}

func adminRequestBucketSeconds(t *testing.T, request AdminRequest) int64 {
	t.Helper()
	field := reflect.ValueOf(request).FieldByName("BucketSeconds")
	if !field.IsValid() {
		t.Fatal("AdminRequest is missing BucketSeconds")
	}
	return field.Int()
}

func TestHandlerRejectsInvalidGroupMonitorOptions(t *testing.T) {
	for _, path := range []string{
		"/group-monitor/groups?range=2h",
		"/group-monitor/groups?range=1h",
		"/group-monitor/groups?range=12h",
		"/group-monitor/groups/no?range=6h",
	} {
		backend := &fakeAdminBackend{}
		handler := NewHandler(backend)
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test"+path, nil)
		handler.ServeAdmin(recorder, req, strings.Split(path, "?")[0], 99)
		if recorder.Code != http.StatusBadRequest && recorder.Code != http.StatusNotFound {
			t.Fatalf("path=%s status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestHandlerReturnsNotFoundWhenGroupDetailWasDeleted(t *testing.T) {
	backend := &fakeAdminBackend{err: sql.ErrNoRows}
	handler := NewHandler(backend)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/group-monitor/groups/42?range=6h", nil)

	handler.ServeAdmin(recorder, req, "/group-monitor/groups/42", 99)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestHandlerReturnsUnprocessableEntityForTooManyAccountCandidates(t *testing.T) {
	backend := &fakeAdminBackend{err: ErrAccountCandidateLimit}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/accounts?sort_by=risk_score", nil)

	NewHandler(backend).ServeAdmin(recorder, req, "/accounts", 99)

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
