//go:build unit

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type customReleaseHandlerServiceStub struct {
	*systemHandlerUpdateServiceStub
	customInfo           *service.CustomReleaseInfo
	job                  *service.UpdateJob
	prepareCall          int
	applyCall            int
	statusCall           int
	current              *service.ReleaseRecord
	releases             []service.ReleaseRecord
	rollbackPrepareCall  int
	rollbackApplyCall    int
	noticeUnread         bool
	noticeReadErr        error
	markNoticeReadErr    error
	noticeUserIDs        []int64
	noticeFingerprints   []string
	markReadUserIDs      []int64
	markReadFingerprints []string
}

func (s *customReleaseHandlerServiceStub) CheckCustomRelease(context.Context, bool) (*service.CustomReleaseInfo, error) {
	return s.customInfo, nil
}

func (s *customReleaseHandlerServiceStub) CustomReleaseNoticeUnread(_ context.Context, userID int64, fingerprint string) (bool, error) {
	s.noticeUserIDs = append(s.noticeUserIDs, userID)
	s.noticeFingerprints = append(s.noticeFingerprints, fingerprint)
	return s.noticeUnread, s.noticeReadErr
}

func (s *customReleaseHandlerServiceStub) MarkCustomReleaseNoticeRead(_ context.Context, userID int64, fingerprint string) error {
	s.markReadUserIDs = append(s.markReadUserIDs, userID)
	s.markReadFingerprints = append(s.markReadFingerprints, fingerprint)
	return s.markNoticeReadErr
}

func (s *customReleaseHandlerServiceStub) PrepareUpdate(context.Context) (*service.UpdateJob, error) {
	s.prepareCall++
	return s.job, nil
}

func (s *customReleaseHandlerServiceStub) ApplyUpdate(context.Context, string) (*service.UpdateJob, error) {
	s.applyCall++
	return s.job, nil
}

func (s *customReleaseHandlerServiceStub) GetUpdateStatus(context.Context, string) (*service.UpdateJob, error) {
	s.statusCall++
	return s.job, nil
}

func (s *customReleaseHandlerServiceStub) CurrentRelease(context.Context) (*service.ReleaseRecord, error) {
	return s.current, nil
}

func (s *customReleaseHandlerServiceStub) ListRollbackReleases(context.Context) ([]service.ReleaseRecord, error) {
	return s.releases, nil
}

func (s *customReleaseHandlerServiceStub) PrepareRollback(context.Context, string) (*service.UpdateJob, error) {
	s.rollbackPrepareCall++
	return s.job, nil
}

func (s *customReleaseHandlerServiceStub) ApplyRollback(context.Context, string) (*service.UpdateJob, error) {
	s.rollbackApplyCall++
	return s.job, nil
}

func TestCustomReleaseHandlerRequiresIdempotencyAndUsesDistinctActions(t *testing.T) {
	repo := newMemoryIdempotencyRepoStub()
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(repo, service.DefaultIdempotencyConfig()))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(nil) })
	stub := &customReleaseHandlerServiceStub{
		systemHandlerUpdateServiceStub: &systemHandlerUpdateServiceStub{},
		customInfo:                     &service.CustomReleaseInfo{ReleaseID: "release-current"},
		current:                        &service.ReleaseRecord{ReleaseID: "release-current"},
		releases:                       []service.ReleaseRecord{{ReleaseID: "release-v101"}},
		job:                            &service.UpdateJob{JobID: "update-prepared", OperationKind: service.ReleaseOperationUpdate, Action: service.ReleasePhasePrepare, Status: service.ReleaseStatusPrepared},
	}
	router := newCustomReleaseHandlerTestRouter(stub, repo)

	posts := []struct {
		path string
		body string
		key  string
	}{
		{path: "/api/v1/admin/system/update", body: `{}`, key: "update-legacy-route-prepare"},
		{path: "/api/v1/admin/system/update/prepare", body: `{}`, key: "update-prepare"},
		{path: "/api/v1/admin/system/update/apply", body: `{"job_id":"update-prepared"}`, key: "update-apply"},
		{path: "/api/v1/admin/system/rollback/prepare", body: `{"release_id":"release-v101"}`, key: "rollback-prepare"},
		{path: "/api/v1/admin/system/rollback/apply", body: `{"job_id":"rollback-prepared"}`, key: "rollback-apply"},
	}
	for _, request := range posts {
		withoutKey := httptest.NewRequest(http.MethodPost, request.path, bytes.NewBufferString(request.body))
		withoutKey.Header.Set("Content-Type", "application/json")
		withoutKeyRecorder := httptest.NewRecorder()
		router.ServeHTTP(withoutKeyRecorder, withoutKey)
		require.Equal(t, http.StatusBadRequest, withoutKeyRecorder.Code, request.path)

		withKey := httptest.NewRequest(http.MethodPost, request.path, bytes.NewBufferString(request.body))
		withKey.Header.Set("Content-Type", "application/json")
		withKey.Header.Set("Idempotency-Key", request.key)
		withKeyRecorder := httptest.NewRecorder()
		router.ServeHTTP(withKeyRecorder, withKey)
		require.Equal(t, http.StatusAccepted, withKeyRecorder.Code, withKeyRecorder.Body.String())
	}

	repo.mu.Lock()
	scopes := make(map[string]bool)
	for _, record := range repo.data {
		if strings.HasPrefix(record.Scope, "admin.system.") {
			scopes[record.Scope] = true
		}
	}
	repo.mu.Unlock()
	for _, scope := range []string{
		"admin.system.update.prepare",
		"admin.system.update.apply",
		"admin.system.rollback.prepare",
		"admin.system.rollback.apply",
	} {
		require.True(t, scopes[scope], scope)
	}
	require.Equal(t, 2, stub.prepareCall, "/system/update must remain prepare-only")
	require.Equal(t, 1, stub.applyCall)
	require.Equal(t, 1, stub.rollbackPrepareCall)
	require.Equal(t, 1, stub.rollbackApplyCall)
}

func TestCustomReleaseHandlerServesReleaseIdentityAndRollbackList(t *testing.T) {
	stub := &customReleaseHandlerServiceStub{
		systemHandlerUpdateServiceStub: &systemHandlerUpdateServiceStub{},
		customInfo:                     &service.CustomReleaseInfo{ReleaseID: "release-current"},
		current:                        &service.ReleaseRecord{ReleaseID: "release-current", OfficialVersion: "v0.1.163", CustomVersion: "v1.0.4"},
		releases:                       []service.ReleaseRecord{{ReleaseID: "release-v101", OfficialVersion: "v0.1.162", CustomVersion: "v1.0.1"}},
	}
	router := newCustomReleaseHandlerTestRouter(stub, newMemoryIdempotencyRepoStub())

	for _, path := range []string{
		"/api/v1/admin/system/custom-release/check",
		"/api/v1/admin/system/release",
		"/api/v1/admin/system/releases/rollback",
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, recorder.Code, path)
	}
}

func TestCustomReleaseHandlerReturnsActionsFailureEvidence(t *testing.T) {
	stub := &customReleaseHandlerServiceStub{
		systemHandlerUpdateServiceStub: &systemHandlerUpdateServiceStub{},
		job: &service.UpdateJob{
			JobID:             "update-actions-failed",
			OperationKind:     service.ReleaseOperationUpdate,
			Action:            service.ReleasePhasePrepare,
			Status:            service.ReleaseStatusFailed,
			Message:           "required check deployment concluded failure",
			FailedCheck:       "deployment",
			CheckURL:          "https://github.com/ListenCodes/sub2api/actions/runs/1/job/2",
			Conclusion:        "failure",
			ErrorCode:         "ACTIONS_REQUIRED_CHECK_FAILED",
			ProductionChanged: false,
		},
	}
	router := newCustomReleaseHandlerTestRouter(stub, newMemoryIdempotencyRepoStub())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/update/status", nil))

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var envelope struct {
		Data struct {
			FailedCheck       string `json:"failed_check"`
			CheckURL          string `json:"check_url"`
			Conclusion        string `json:"conclusion"`
			ErrorCode         string `json:"error_code"`
			ProductionChanged bool   `json:"production_changed"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, "deployment", envelope.Data.FailedCheck)
	require.Equal(t, "https://github.com/ListenCodes/sub2api/actions/runs/1/job/2", envelope.Data.CheckURL)
	require.Equal(t, "failure", envelope.Data.Conclusion)
	require.Equal(t, "ACTIONS_REQUIRED_CHECK_FAILED", envelope.Data.ErrorCode)
	require.False(t, envelope.Data.ProductionChanged)
}

func TestCustomReleaseCheckDecoratesPerAdminNotice(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	stub := &customReleaseHandlerServiceStub{
		systemHandlerUpdateServiceStub: &systemHandlerUpdateServiceStub{},
		customInfo: &service.CustomReleaseInfo{
			HasUpdate:         true,
			UpdateFingerprint: fingerprint,
		},
		noticeUnread: true,
	}
	router := newCustomReleaseHandlerTestRouter(stub, newMemoryIdempotencyRepoStub())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/custom-release/check", nil))

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, []int64{41}, stub.noticeUserIDs)
	require.Equal(t, []string{fingerprint}, stub.noticeFingerprints)
	var envelope struct {
		Data service.CustomReleaseInfo `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.True(t, envelope.Data.HasUpdate)
	require.True(t, envelope.Data.NoticeUnread)
	require.Empty(t, envelope.Data.NoticeWarning)
}

func TestCustomReleaseCheckNoticeFailureIsAdvisory(t *testing.T) {
	fingerprint := strings.Repeat("b", 64)
	stub := &customReleaseHandlerServiceStub{
		systemHandlerUpdateServiceStub: &systemHandlerUpdateServiceStub{},
		customInfo: &service.CustomReleaseInfo{
			HasUpdate:         true,
			UpdateFingerprint: fingerprint,
		},
		noticeReadErr: errors.New("fixture state unavailable"),
	}
	router := newCustomReleaseHandlerTestRouter(stub, newMemoryIdempotencyRepoStub())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/custom-release/check", nil))

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var envelope struct {
		Data service.CustomReleaseInfo `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.True(t, envelope.Data.HasUpdate)
	require.True(t, envelope.Data.NoticeUnread)
	require.NotEmpty(t, envelope.Data.NoticeWarning)
}

func TestCustomReleaseMarkReadValidatesAndIsolatesAdmins(t *testing.T) {
	fingerprint := strings.Repeat("c", 64)
	stub := &customReleaseHandlerServiceStub{systemHandlerUpdateServiceStub: &systemHandlerUpdateServiceStub{}}
	router := newCustomReleaseHandlerTestRouter(stub, newMemoryIdempotencyRepoStub())

	for _, test := range []struct {
		name       string
		body       string
		userID     int64
		noAuth     bool
		wantStatus int
	}{
		{name: "invalid JSON", body: "{", userID: 41, wantStatus: http.StatusBadRequest},
		{name: "invalid fingerprint", body: `{"fingerprint":"bad"}`, userID: 41, wantStatus: http.StatusBadRequest},
		{name: "missing auth", body: `{"fingerprint":"` + fingerprint + `"}`, noAuth: true, wantStatus: http.StatusUnauthorized},
		{name: "admin 41", body: `{"fingerprint":"` + fingerprint + `"}`, userID: 41, wantStatus: http.StatusOK},
		{name: "admin 42", body: `{"fingerprint":"` + fingerprint + `"}`, userID: 42, wantStatus: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/custom-release/read", bytes.NewBufferString(test.body))
			request.Header.Set("Content-Type", "application/json")
			if test.noAuth {
				request.Header.Set("X-Test-No-Auth", "true")
			} else if test.userID > 0 {
				request.Header.Set("X-Test-User", strconv.FormatInt(test.userID, 10))
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, test.wantStatus, recorder.Code, recorder.Body.String())
		})
	}
	require.Equal(t, []int64{41, 42}, stub.markReadUserIDs)
	require.Equal(t, []string{fingerprint, fingerprint}, stub.markReadFingerprints)
}

func TestCustomReleaseMarkReadFailureIsAdvisory(t *testing.T) {
	fingerprint := strings.Repeat("d", 64)
	stub := &customReleaseHandlerServiceStub{
		systemHandlerUpdateServiceStub: &systemHandlerUpdateServiceStub{},
		markNoticeReadErr:              errors.New("fixture write failed"),
	}
	router := newCustomReleaseHandlerTestRouter(stub, newMemoryIdempotencyRepoStub())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/custom-release/read", bytes.NewBufferString(`{"fingerprint":"`+fingerprint+`"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var envelope struct {
		Data struct {
			Fingerprint string `json:"fingerprint"`
			Persisted   bool   `json:"persisted"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, fingerprint, envelope.Data.Fingerprint)
	require.False(t, envelope.Data.Persisted)
}

func newCustomReleaseHandlerTestRouter(stub *customReleaseHandlerServiceStub, repo *memoryIdempotencyRepoStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	lock := service.NewSystemOperationLockService(repo, service.IdempotencyConfig{ProcessingTimeout: time.Second, SystemOperationTTL: time.Minute})
	handler := NewSystemHandler(stub, lock)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if c.GetHeader("X-Test-No-Auth") != "true" {
			userID := int64(41)
			if raw := c.GetHeader("X-Test-User"); raw != "" {
				userID, _ = strconv.ParseInt(raw, 10, 64)
			}
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
		}
		c.Next()
	})
	router.GET("/api/v1/admin/system/custom-release/check", handler.CheckCustomRelease)
	router.POST("/api/v1/admin/system/custom-release/read", handler.MarkCustomReleaseRead)
	router.GET("/api/v1/admin/system/release", handler.CurrentRelease)
	router.GET("/api/v1/admin/system/releases/rollback", handler.ListRollbackReleases)
	router.POST("/api/v1/admin/system/update", handler.PrepareUpdate)
	router.POST("/api/v1/admin/system/update/prepare", handler.PrepareUpdate)
	router.POST("/api/v1/admin/system/update/apply", handler.ApplyUpdate)
	router.GET("/api/v1/admin/system/update/status", handler.GetUpdateStatus)
	router.POST("/api/v1/admin/system/rollback/prepare", handler.PrepareRollback)
	router.POST("/api/v1/admin/system/rollback/apply", handler.ApplyRollback)
	router.POST("/api/v1/admin/system/rollback", handler.LegacyRollbackUnsupported)
	router.GET("/api/v1/admin/system/rollback-versions", handler.LegacyRollbackUnsupported)
	return router
}

func TestCustomReleaseLegacyRollbackFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stableStub := &systemHandlerUpdateServiceStub{}
	stub := &customReleaseHandlerServiceStub{systemHandlerUpdateServiceStub: stableStub}
	handler := NewSystemHandler(stub, service.NewSystemOperationLockService(newMemoryIdempotencyRepoStub(), service.IdempotencyConfig{}))
	router := gin.New()
	router.POST("/api/v1/admin/system/rollback", handler.LegacyRollbackUnsupported)
	router.GET("/api/v1/admin/system/rollback-versions", handler.LegacyRollbackUnsupported)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/api/v1/admin/system/rollback"},
		{method: http.MethodGet, path: "/api/v1/admin/system/rollback-versions"},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(tc.method, tc.path, nil)
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusConflict, recorder.Code)
	}

	require.Zero(t, stableStub.rollbackCall)
	require.Zero(t, stableStub.rollbackToCall)
	require.Zero(t, stableStub.rollbackVersionsCall)
}
