//go:build unit

package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type customReleaseHandlerServiceStub struct {
	*systemHandlerUpdateServiceStub
	customInfo          *service.CustomReleaseInfo
	job                 *service.UpdateJob
	prepareCall         int
	applyCall           int
	statusCall          int
	current             *service.ReleaseRecord
	releases            []service.ReleaseRecord
	rollbackPrepareCall int
	rollbackApplyCall   int
}

func (s *customReleaseHandlerServiceStub) CheckCustomRelease(context.Context, bool) (*service.CustomReleaseInfo, error) {
	return s.customInfo, nil
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

func newCustomReleaseHandlerTestRouter(stub *customReleaseHandlerServiceStub, repo *memoryIdempotencyRepoStub) *gin.Engine {
	gin.SetMode(gin.TestMode)
	lock := service.NewSystemOperationLockService(repo, service.IdempotencyConfig{ProcessingTimeout: time.Second, SystemOperationTTL: time.Minute})
	handler := NewSystemHandler(stub, lock)
	router := gin.New()
	router.GET("/api/v1/admin/system/custom-release/check", handler.CheckCustomRelease)
	router.GET("/api/v1/admin/system/release", handler.CurrentRelease)
	router.GET("/api/v1/admin/system/releases/rollback", handler.ListRollbackReleases)
	router.POST("/api/v1/admin/system/update", handler.PrepareUpdate)
	router.POST("/api/v1/admin/system/update/prepare", handler.PrepareUpdate)
	router.POST("/api/v1/admin/system/update/apply", handler.ApplyUpdate)
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
