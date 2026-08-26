package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type riskCaseResolutionAdminStub struct {
	service.AdminService
	user        service.User
	getErr      error
	updateErr   error
	updateCalls int
}

func (s *riskCaseResolutionAdminStub) GetUser(context.Context, int64) (*service.User, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	copy := s.user
	return &copy, nil
}

func (s *riskCaseResolutionAdminStub) UpdateUser(_ context.Context, id int64, input *service.UpdateUserInput) (*service.User, error) {
	s.updateCalls++
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	s.user.ID = id
	s.user.Status = input.Status
	copy := s.user
	return &copy, nil
}

func resolveRiskCaseRequest(t *testing.T, handler *CustomUserHandler, payload string, requestID string) *httptest.ResponseRecorder {
	t.Helper()
	engine := gin.New()
	engine.POST("/admin/user-risk/cases/:id/resolve", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		handler.ResolveRiskCase(c)
	})
	request := httptest.NewRequest(http.MethodPost, "/admin/user-risk/cases/9/resolve", bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	if requestID != "" {
		request.Header.Set("Idempotency-Key", requestID)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func decodeResolveRiskCaseData(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func TestResolveRiskCaseReturnsStructuredSuccess(t *testing.T) {
	var getCalls, resolveCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/admin/review-cases/9":
			getCalls++
			_, _ = writer.Write([]byte(`{"id":9,"user_id":42,"status":"in_review","revision":3}`))
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/admin/review-cases/9/resolve":
			resolveCalls++
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["request_id"] != "resolve-9-attempt-1" || body["expected_revision"] != float64(3) {
				t.Fatalf("resolve body = %+v", body)
			}
			_, _ = writer.Write([]byte(`{"case":{"id":9,"user_id":42,"status":"resolved","resolution":"legitimate_shared","resolution_reason":"证据显示为正常共享环境","resolution_request_id":"resolve-9-attempt-1","revision":4},"resolved":true,"idempotent_replay":false}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "resolve-success-secret")
	handler := &CustomUserHandler{riskControlClient: service.NewRiskControlClientFromEnv()}
	response := resolveRiskCaseRequest(t, handler, `{"user_id":42,"resolution":"legitimate_shared","reason":"证据显示为正常共享环境","account_action":"none","expected_case_revision":3}`, "resolve-9-attempt-1")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	data := decodeResolveRiskCaseData(t, response)
	if data["result"] != "success" || data["request_id"] != "resolve-9-attempt-1" || getCalls != 1 || resolveCalls != 1 {
		t.Fatalf("data=%+v get=%d resolve=%d", data, getCalls, resolveCalls)
	}
}

func TestResolveRiskCaseReportsExtensionPartialFailure(t *testing.T) {
	var auditReport service.RiskAuditReport
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/v1/internal/audit" {
			if err := json.NewDecoder(request.Body).Decode(&auditReport); err != nil {
				t.Fatal(err)
			}
			_, _ = writer.Write([]byte(`{"ok":true}`))
			return
		}
		if request.Method == http.MethodGet {
			_, _ = writer.Write([]byte(`{"id":9,"user_id":42,"status":"in_review","revision":3}`))
			return
		}
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "resolve-partial-secret")
	handler := &CustomUserHandler{riskControlClient: service.NewRiskControlClientFromEnv()}
	response := resolveRiskCaseRequest(t, handler, `{"user_id":42,"resolution":"confirmed_abuse","reason":"多证据确认滥用","account_action":"none","expected_case_revision":3}`, "resolve-9-partial")
	data := decodeResolveRiskCaseData(t, response)
	caseStep, _ := data["case"].(map[string]any)
	if response.Code != http.StatusOK || data["result"] != "partial" || data["retryable"] != true || caseStep["result"] != "partial" {
		t.Fatalf("status=%d data=%+v", response.Code, data)
	}
	if auditReport.Result != "partial" || auditReport.Metadata["outcome_unknown"] != true {
		t.Fatalf("audit=%+v", auditReport)
	}
}

func TestResolveRiskCaseDoesNotMutateAccountWhenCaseResolveFails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodGet {
			_, _ = writer.Write([]byte(`{"id":9,"user_id":42,"status":"in_review","revision":3}`))
			return
		}
		writer.WriteHeader(http.StatusConflict)
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "resolve-conflict-secret")
	adminStub := &riskCaseResolutionAdminStub{user: service.User{ID: 42, Status: service.StatusActive}}
	handler := &CustomUserHandler{adminService: adminStub, riskControlClient: service.NewRiskControlClientFromEnv()}
	response := resolveRiskCaseRequest(t, handler, `{"user_id":42,"resolution":"confirmed_abuse","reason":"确认账号存在滥用行为","account_action":"disable","expected_case_revision":3}`, "resolve-9-conflict")
	data := decodeResolveRiskCaseData(t, response)
	accountStep, _ := data["account"].(map[string]any)
	if response.Code != http.StatusOK || data["result"] != "partial" || accountStep["result"] != "not_executed" || adminStub.updateCalls != 0 {
		t.Fatalf("status=%d data=%+v updates=%d", response.Code, data, adminStub.updateCalls)
	}
}

func TestResolveRiskCaseReportsAccountFailureAfterResolvingCase(t *testing.T) {
	resolveCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodGet {
			_, _ = writer.Write([]byte(`{"id":9,"user_id":42,"status":"in_review","revision":3}`))
			return
		}
		if request.URL.Path == "/api/v1/admin/review-cases/9/resolve" {
			resolveCalls++
			_, _ = writer.Write([]byte(`{"case":{"id":9,"user_id":42,"status":"resolved","resolution":"confirmed_abuse","resolution_reason":"确认账号存在滥用行为","resolution_request_id":"resolve-9-account-failed","revision":4},"resolved":true,"idempotent_replay":false}`))
			return
		}
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "resolve-account-failure-secret")
	adminStub := &riskCaseResolutionAdminStub{user: service.User{ID: 42, Status: service.StatusActive}, updateErr: errors.New("database unavailable")}
	handler := &CustomUserHandler{adminService: adminStub, riskControlClient: service.NewRiskControlClientFromEnv()}
	response := resolveRiskCaseRequest(t, handler, `{"user_id":42,"resolution":"confirmed_abuse","reason":"确认账号存在滥用行为","account_action":"disable","expected_case_revision":3}`, "resolve-9-account-failed")
	data := decodeResolveRiskCaseData(t, response)
	accountStep, _ := data["account"].(map[string]any)
	caseStep, _ := data["case"].(map[string]any)
	if response.Code != http.StatusOK || data["result"] != "partial" || accountStep["result"] != "failed" || accountStep["pending_step"] != "status_confirmation" || caseStep["result"] != "resolved" || resolveCalls != 1 || adminStub.updateCalls != 1 {
		t.Fatalf("status=%d data=%+v resolve=%d updates=%d", response.Code, data, resolveCalls, adminStub.updateCalls)
	}
}

func TestResolveRiskCaseRejectsUnverifiedSuccessResponseBeforeAccountMutation(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed json", body: `{"case":`},
		{name: "missing contract", body: `{}`},
		{name: "not resolved", body: `{"case":{"id":9,"user_id":42,"status":"in_review","revision":3},"resolved":false}`},
		{name: "wrong case", body: `{"case":{"id":10,"user_id":42,"status":"resolved","resolution":"confirmed_abuse","resolution_reason":"确认账号存在滥用行为","resolution_request_id":"resolve-unverified","revision":4},"resolved":true}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestID := "resolve-unverified-" + strings.ReplaceAll(test.name, " ", "-")
			upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				if request.Method == http.MethodGet {
					_, _ = writer.Write([]byte(`{"id":9,"user_id":42,"status":"in_review","revision":3}`))
					return
				}
				if request.URL.Path == "/api/v1/admin/review-cases/9/resolve" {
					_, _ = writer.Write([]byte(strings.ReplaceAll(test.body, "resolve-unverified", requestID)))
					return
				}
				_, _ = writer.Write([]byte(`{"ok":true}`))
			}))
			defer upstream.Close()
			t.Setenv("RISK_CONTROL_URL", upstream.URL)
			t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "resolve-unverified-secret")
			adminStub := &riskCaseResolutionAdminStub{user: service.User{ID: 42, Status: service.StatusActive}}
			handler := &CustomUserHandler{adminService: adminStub, riskControlClient: service.NewRiskControlClientFromEnv()}
			response := resolveRiskCaseRequest(t, handler, `{"user_id":42,"resolution":"confirmed_abuse","reason":"确认账号存在滥用行为","account_action":"disable","expected_case_revision":3}`, requestID)
			data := decodeResolveRiskCaseData(t, response)
			accountStep, _ := data["account"].(map[string]any)
			caseStep, _ := data["case"].(map[string]any)
			if response.Code != http.StatusOK || data["result"] != "partial" || data["retryable"] != true || accountStep["result"] != "not_executed" || caseStep["result"] != "partial" || adminStub.updateCalls != 0 {
				t.Fatalf("status=%d data=%+v updates=%d", response.Code, data, adminStub.updateCalls)
			}
		})
	}
}

func TestResolveRiskCaseMarksPermanentAccountPolicyErrorsNotRetryable(t *testing.T) {
	tests := []struct {
		name      string
		getErr    error
		updateErr error
	}{
		{name: "admin cannot be disabled", updateErr: service.ErrCannotDisableAdminUser},
		{name: "user no longer exists", getErr: service.ErrUserNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requestID := "resolve-permanent-" + strings.ReplaceAll(test.name, " ", "-")
			upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				if request.Method == http.MethodGet {
					_, _ = writer.Write([]byte(`{"id":9,"user_id":42,"status":"in_review","revision":3}`))
					return
				}
				if request.URL.Path == "/api/v1/admin/review-cases/9/resolve" {
					_, _ = writer.Write([]byte(strings.ReplaceAll(`{"case":{"id":9,"user_id":42,"status":"resolved","resolution":"confirmed_abuse","resolution_reason":"确认账号存在滥用行为","resolution_request_id":"resolve-permanent","revision":4},"resolved":true}`, "resolve-permanent", requestID)))
					return
				}
				_, _ = writer.Write([]byte(`{"ok":true}`))
			}))
			defer upstream.Close()
			t.Setenv("RISK_CONTROL_URL", upstream.URL)
			t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "resolve-permanent-secret")
			adminStub := &riskCaseResolutionAdminStub{user: service.User{ID: 42, Status: service.StatusActive}, getErr: test.getErr, updateErr: test.updateErr}
			handler := &CustomUserHandler{adminService: adminStub, riskControlClient: service.NewRiskControlClientFromEnv()}
			response := resolveRiskCaseRequest(t, handler, `{"user_id":42,"resolution":"confirmed_abuse","reason":"确认账号存在滥用行为","account_action":"disable","expected_case_revision":3}`, requestID)
			data := decodeResolveRiskCaseData(t, response)
			accountStep, _ := data["account"].(map[string]any)
			if response.Code != http.StatusOK || data["result"] != "partial" || data["retryable"] != false || accountStep["result"] != "not_executed" || accountStep["retryable"] != false {
				t.Fatalf("status=%d data=%+v", response.Code, data)
			}
		})
	}
}

func TestResolveRiskCaseRejectsMissingIdempotencyKey(t *testing.T) {
	handler := &CustomUserHandler{}
	response := resolveRiskCaseRequest(t, handler, `{"user_id":42,"resolution":"confirmed_abuse","reason":"确认账号存在滥用行为","account_action":"none","expected_case_revision":3}`, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestResolveRiskCaseUsesTheSharedIdempotencyKeyContract(t *testing.T) {
	previous := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(nil)
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	handler := &CustomUserHandler{}
	payload := `{"user_id":42,"resolution":"confirmed_abuse","reason":"确认账号存在滥用行为","account_action":"none","expected_case_revision":3}`
	if response := resolveRiskCaseRequest(t, handler, payload, strings.Repeat("a", 128)); response.Code != http.StatusOK {
		t.Fatalf("128-byte key status=%d body=%s", response.Code, response.Body.String())
	}
	if response := resolveRiskCaseRequest(t, handler, payload, strings.Repeat("b", 129)); response.Code != http.StatusBadRequest {
		t.Fatalf("129-byte key status=%d body=%s", response.Code, response.Body.String())
	}
	if response := resolveRiskCaseRequest(t, handler, payload, "contains space"); response.Code != http.StatusBadRequest {
		t.Fatalf("non-ASCII contract key status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestResolveRiskCaseKeepsMissingCaseServiceRetryable(t *testing.T) {
	previous := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(nil)
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	handler := &CustomUserHandler{}
	response := resolveRiskCaseRequest(t, handler, `{"user_id":42,"resolution":"confirmed_abuse","reason":"确认账号存在滥用行为","account_action":"none","expected_case_revision":3}`, "resolve-9-unavailable")
	data := decodeResolveRiskCaseData(t, response)
	if response.Code != http.StatusOK || data["result"] != "partial" || data["retryable"] != true {
		t.Fatalf("status=%d data=%+v", response.Code, data)
	}
}

func TestResolveRiskCaseRejectsMismatchedCaseWithoutMutation(t *testing.T) {
	resolveCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPost && request.URL.Path == "/api/v1/admin/review-cases/9/resolve" {
			resolveCalls++
		}
		_, _ = writer.Write([]byte(`{"id":9,"user_id":99,"status":"in_review","revision":3}`))
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "resolve-mismatch-secret")
	handler := &CustomUserHandler{riskControlClient: service.NewRiskControlClientFromEnv()}
	response := resolveRiskCaseRequest(t, handler, `{"user_id":42,"resolution":"confirmed_abuse","reason":"确认账号存在滥用行为","account_action":"none","expected_case_revision":3}`, "resolve-9-mismatch")
	data := decodeResolveRiskCaseData(t, response)
	if response.Code != http.StatusOK || data["result"] != "partial" || data["retryable"] != false || resolveCalls != 0 {
		t.Fatalf("status=%d data=%+v resolve=%d", response.Code, data, resolveCalls)
	}
}

func TestResolveRiskCaseRejectsInvalidResolutionBeforeMutation(t *testing.T) {
	adminStub := &riskCaseResolutionAdminStub{user: service.User{ID: 42, Status: service.StatusActive}}
	handler := &CustomUserHandler{adminService: adminStub}
	response := resolveRiskCaseRequest(t, handler, `{"user_id":42,"resolution":"not-a-resolution","reason":"确认账号存在滥用行为","account_action":"disable","expected_case_revision":3}`, "resolve-9-invalid")
	if response.Code != http.StatusBadRequest || adminStub.updateCalls != 0 {
		t.Fatalf("status=%d updates=%d body=%s", response.Code, adminStub.updateCalls, response.Body.String())
	}
}

func TestResolveRiskCaseRejectsResolvedCaseFromDifferentRequestBeforeAccountMutation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":9,"user_id":42,"status":"resolved","resolution":"confirmed_abuse","resolution_reason":"确认账号存在滥用行为","resolution_request_id":"resolve-9-original","revision":4}`))
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "resolve-already-finished-secret")
	adminStub := &riskCaseResolutionAdminStub{user: service.User{ID: 42, Status: service.StatusActive}}
	handler := &CustomUserHandler{adminService: adminStub, riskControlClient: service.NewRiskControlClientFromEnv()}
	response := resolveRiskCaseRequest(t, handler, `{"user_id":42,"resolution":"confirmed_abuse","reason":"确认账号存在滥用行为","account_action":"disable","expected_case_revision":3}`, "resolve-9-different")
	data := decodeResolveRiskCaseData(t, response)
	if response.Code != http.StatusOK || data["result"] != "partial" || data["retryable"] != false || adminStub.updateCalls != 0 {
		t.Fatalf("status=%d data=%+v updates=%d", response.Code, data, adminStub.updateCalls)
	}
}

func TestResolveRiskCaseBindsIdempotencyKeyToFullRequestBeforeMutation(t *testing.T) {
	previous := service.DefaultIdempotencyCoordinator()
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	cfg.FailedRetryBackoff = 0
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(newMemoryIdempotencyRepoStub(), cfg))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })

	var getCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodGet {
			getCalls++
			_, _ = writer.Write([]byte(`{"id":9,"user_id":42,"status":"in_review","revision":3}`))
			return
		}
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	t.Setenv("RISK_CONTROL_URL", upstream.URL)
	t.Setenv("RISK_CONTROL_INTERNAL_SECRET", "resolve-fingerprint-secret")
	adminStub := &riskCaseResolutionAdminStub{user: service.User{ID: 42, Status: service.StatusActive}}
	handler := &CustomUserHandler{adminService: adminStub, riskControlClient: service.NewRiskControlClientFromEnv()}

	first := resolveRiskCaseRequest(t, handler, `{"user_id":42,"resolution":"confirmed_abuse","reason":"确认账号存在滥用行为","account_action":"none","expected_case_revision":3}`, "resolve-9-bound")
	if first.Code != http.StatusOK || decodeResolveRiskCaseData(t, first)["result"] != "partial" {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	second := resolveRiskCaseRequest(t, handler, `{"user_id":42,"resolution":"confirmed_abuse","reason":"确认账号存在滥用行为","account_action":"disable","expected_case_revision":3}`, "resolve-9-bound")
	if second.Code != http.StatusConflict || adminStub.updateCalls != 0 || getCalls != 1 {
		t.Fatalf("second status=%d updates=%d gets=%d body=%s", second.Code, adminStub.updateCalls, getCalls, second.Body.String())
	}
}
