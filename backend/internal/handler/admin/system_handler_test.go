//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type systemHandlerUpdateServiceStub struct {
	performErr            error
	performJob            *service.UpdateJob
	updateInfo            *service.UpdateInfo
	checkErr              error
	checkForces           []bool
	statusJobIDs          []string
	performCall           int
	prepareCall           int
	applyCall             int
	applyJobIDs           []string
	performCtxErr         error
	performHasDeadline    bool
	rollbackCall          int
	rollbackToCall        int
	rollbackToCtxErr      error
	rollbackToHasDeadline bool
	rollbackToVersions    []string
	rollbackToErr         error
	rollbackVersions      []service.RollbackVersion
	rollbackVersionsErr   error
	rollbackVersionsCall  int
}

func (s *systemHandlerUpdateServiceStub) CheckUpdate(_ context.Context, force bool) (*service.UpdateInfo, error) {
	s.checkForces = append(s.checkForces, force)
	return s.updateInfo, s.checkErr
}

func (s *systemHandlerUpdateServiceStub) PerformUpdate(ctx context.Context) (*service.UpdateJob, error) {
	s.performCall++
	s.performCtxErr = ctx.Err()
	_, s.performHasDeadline = ctx.Deadline()
	return s.performJob, s.performErr
}

func (s *systemHandlerUpdateServiceStub) PrepareUpdate(ctx context.Context) (*service.UpdateJob, error) {
	s.prepareCall++
	s.performCtxErr = ctx.Err()
	_, s.performHasDeadline = ctx.Deadline()
	return s.performJob, s.performErr
}

func (s *systemHandlerUpdateServiceStub) ApplyUpdate(ctx context.Context, jobID string) (*service.UpdateJob, error) {
	s.applyCall++
	s.applyJobIDs = append(s.applyJobIDs, jobID)
	s.performCtxErr = ctx.Err()
	_, s.performHasDeadline = ctx.Deadline()
	return s.performJob, s.performErr
}

func (s *systemHandlerUpdateServiceStub) GetUpdateStatus(_ context.Context, jobID string) (*service.UpdateJob, error) {
	s.statusJobIDs = append(s.statusJobIDs, jobID)
	return s.performJob, nil
}

func (s *systemHandlerUpdateServiceStub) Rollback() error {
	s.rollbackCall++
	return nil
}

func (s *systemHandlerUpdateServiceStub) ListRollbackVersions(context.Context) ([]service.RollbackVersion, error) {
	s.rollbackVersionsCall++
	return s.rollbackVersions, s.rollbackVersionsErr
}

func (s *systemHandlerUpdateServiceStub) RollbackToVersion(ctx context.Context, version string) error {
	s.rollbackToCall++
	s.rollbackToCtxErr = ctx.Err()
	_, s.rollbackToHasDeadline = ctx.Deadline()
	s.rollbackToVersions = append(s.rollbackToVersions, version)
	return s.rollbackToErr
}

type systemUpdateResponseEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Message         string `json:"message"`
		AlreadyUpToDate bool   `json:"already_up_to_date"`
		CurrentVersion  string `json:"current_version"`
		LatestVersion   string `json:"latest_version"`
		OperationID     string `json:"operation_id"`
	} `json:"data"`
}

type systemUpdateErrorEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func newSystemHandlerTestRouter(t *testing.T, updateSvc *systemHandlerUpdateServiceStub, repo *memoryIdempotencyRepoStub) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service.SetDefaultIdempotencyCoordinator(nil)
	t.Cleanup(func() {
		service.SetDefaultIdempotencyCoordinator(nil)
	})

	lockSvc := service.NewSystemOperationLockService(repo, service.IdempotencyConfig{
		ProcessingTimeout:  time.Second,
		SystemOperationTTL: time.Minute,
	})
	handler := NewSystemHandler(updateSvc, lockSvc)

	router := gin.New()
	router.POST("/api/v1/admin/system/update", handler.PerformUpdate)
	router.POST("/api/v1/admin/system/update/prepare", handler.PrepareUpdate)
	router.POST("/api/v1/admin/system/update/apply", handler.ApplyUpdate)
	router.GET("/api/v1/admin/system/update/status", handler.GetUpdateStatus)
	router.POST("/api/v1/admin/system/rollback", handler.Rollback)
	router.GET("/api/v1/admin/system/rollback-versions", handler.GetRollbackVersions)
	return router
}

func TestSystemHandlerPrepareAndApplyUseSeparateActions(t *testing.T) {
	updateSvc := &systemHandlerUpdateServiceStub{performJob: &service.UpdateJob{
		JobID:  "update-two-phase",
		Action: service.UpdateActionPrepare,
		Status: service.UpdateStatusCheckingUpdates,
	}}
	repo := newMemoryIdempotencyRepoStub()
	router := newSystemHandlerTestRouter(t, updateSvc, repo)

	prepareRec := httptest.NewRecorder()
	prepareReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/update/prepare", nil)
	prepareReq.Header.Set("Idempotency-Key", "prepare-key")
	router.ServeHTTP(prepareRec, prepareReq)
	require.Equal(t, http.StatusAccepted, prepareRec.Code)
	require.Equal(t, 1, updateSvc.prepareCall)
	require.Equal(t, 0, updateSvc.applyCall)

	updateSvc.performJob.Action = service.UpdateActionApply
	updateSvc.performJob.Status = service.UpdateStatusApplyQueued
	applyRec := httptest.NewRecorder()
	applyReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/update/apply", strings.NewReader(`{"job_id":"update-two-phase"}`))
	applyReq.Header.Set("Content-Type", "application/json")
	applyReq.Header.Set("Idempotency-Key", "apply-key")
	router.ServeHTTP(applyRec, applyReq)
	require.Equal(t, http.StatusAccepted, applyRec.Code)
	require.Equal(t, 1, updateSvc.applyCall)
	require.Equal(t, []string{"update-two-phase"}, updateSvc.applyJobIDs)
}

func TestSystemHandlerLegacyUpdateAliasOnlyPrepares(t *testing.T) {
	updateSvc := &systemHandlerUpdateServiceStub{performJob: &service.UpdateJob{
		JobID:  "update-legacy-alias",
		Action: service.UpdateActionPrepare,
		Status: service.UpdateStatusCheckingUpdates,
	}}
	repo := newMemoryIdempotencyRepoStub()
	router := newSystemHandlerTestRouter(t, updateSvc, repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/update", nil)
	req.Header.Set("Idempotency-Key", "legacy-key")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Equal(t, 1, updateSvc.prepareCall)
	require.Equal(t, 0, updateSvc.applyCall)
}

func requireSystemLockStatus(t *testing.T, repo *memoryIdempotencyRepoStub, wantStatus string) {
	t.Helper()
	repo.mu.Lock()
	defer repo.mu.Unlock()

	for _, record := range repo.data {
		if record.Status == wantStatus {
			return
		}
	}
	t.Fatalf("system lock status %q not found in records: %#v", wantStatus, repo.data)
}

func TestSystemHandlerPerformUpdateFailureStillReturnsInternalError(t *testing.T) {
	updateSvc := &systemHandlerUpdateServiceStub{
		performErr: errors.New("download failed"),
	}
	repo := newMemoryIdempotencyRepoStub()
	router := newSystemHandlerTestRouter(t, updateSvc, repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/update", nil)
	req.Header.Set("Idempotency-Key", "real-failure")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, 1, updateSvc.prepareCall)
	require.Empty(t, updateSvc.checkForces)
	requireSystemLockStatus(t, repo, service.IdempotencyStatusFailedRetryable)

	var body systemUpdateErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, http.StatusInternalServerError, body.Code)
	require.Equal(t, "internal error", body.Message)
}

func TestSystemHandlerPerformUpdateReturnsAcceptedJob(t *testing.T) {
	started := time.Now().UTC()
	updateSvc := &systemHandlerUpdateServiceStub{
		performJob: &service.UpdateJob{
			JobID:              "update-test",
			Status:             service.UpdateStatusCheckingRelease,
			Message:            "sync triggered",
			ReleaseTag:         "v0.1.158",
			ReleaseCommit:      "26abd19a2812edba02bbef93c3e2a620141cc257",
			ReleasePublishedAt: "2026-07-16T12:37:06Z",
			StartedAt:          &started,
		},
	}
	repo := newMemoryIdempotencyRepoStub()
	router := newSystemHandlerTestRouter(t, updateSvc, repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/update", nil)
	req.Header.Set("Idempotency-Key", "async-update")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	var body struct {
		Data service.UpdateJob `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "update-test", body.Data.JobID)
	require.Equal(t, service.UpdateStatusCheckingRelease, body.Data.Status)
	require.Equal(t, "v0.1.158", body.Data.ReleaseTag)
	require.Equal(t, "26abd19a2812edba02bbef93c3e2a620141cc257", body.Data.ReleaseCommit)
	require.Equal(t, "2026-07-16T12:37:06Z", body.Data.ReleasePublishedAt)
	requireSystemLockStatus(t, repo, service.IdempotencyStatusSucceeded)
}

// TestSystemHandlerPerformUpdateSurvivesClientDisconnect reproduces #4504
// for the durable release trigger. The trigger must not inherit a canceled
// browser request, but it must still have a bounded execution deadline.
func TestSystemHandlerPerformUpdateSurvivesClientDisconnect(t *testing.T) {
	updateSvc := &systemHandlerUpdateServiceStub{
		performJob: &service.UpdateJob{
			JobID:  "update-disconnected",
			Status: service.UpdateStatusCheckingRelease,
		},
	}
	repo := newMemoryIdempotencyRepoStub()
	router := newSystemHandlerTestRouter(t, updateSvc, repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/update", nil)
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	req = req.WithContext(canceledCtx)
	req.Header.Set("Idempotency-Key", "disconnected-update")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Equal(t, 1, updateSvc.prepareCall)
	require.NoError(t, updateSvc.performCtxErr,
		"update must not observe the canceled request context")
	require.True(t, updateSvc.performHasDeadline,
		"detached update context must still be bounded by a deadline")
	requireSystemLockStatus(t, repo, service.IdempotencyStatusSucceeded)
}

func TestSystemHandlerGetUpdateStatusWithoutJobIDReturnsCurrentJob(t *testing.T) {
	updateSvc := &systemHandlerUpdateServiceStub{
		performJob: &service.UpdateJob{
			JobID:  "update-current",
			Status: service.UpdateStatusWaitingImages,
		},
	}
	repo := newMemoryIdempotencyRepoStub()
	router := newSystemHandlerTestRouter(t, updateSvc, repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/update/status", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data service.UpdateJob `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "update-current", body.Data.JobID)
	require.Equal(t, service.UpdateStatusWaitingImages, body.Data.Status)
	require.Equal(t, []string{""}, updateSvc.statusJobIDs)
}

func TestSystemHandlerRollbackToVersionSurvivesClientDisconnect(t *testing.T) {
	updateSvc := &systemHandlerUpdateServiceStub{}
	repo := newMemoryIdempotencyRepoStub()
	router := newSystemHandlerTestRouter(t, updateSvc, repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/rollback",
		strings.NewReader(`{"version":"0.1.146"}`))
	req.Header.Set("Content-Type", "application/json")
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	req = req.WithContext(canceledCtx)
	req.Header.Set("Idempotency-Key", "disconnected-rollback")
	router.ServeHTTP(rec, req)

	require.Equal(t, 1, updateSvc.rollbackToCall)
	require.NoError(t, updateSvc.rollbackToCtxErr,
		"versioned rollback must not observe the canceled request context")
	require.True(t, updateSvc.rollbackToHasDeadline,
		"detached rollback context must still be bounded by a deadline")
	requireSystemLockStatus(t, repo, service.IdempotencyStatusSucceeded)
}

func TestSystemHandlerRollbackWithoutBodyUsesLegacyBackup(t *testing.T) {
	updateSvc := &systemHandlerUpdateServiceStub{}
	repo := newMemoryIdempotencyRepoStub()
	router := newSystemHandlerTestRouter(t, updateSvc, repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/rollback", nil)
	req.Header.Set("Idempotency-Key", "legacy-rollback")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, updateSvc.rollbackCall)
	require.Equal(t, 0, updateSvc.rollbackToCall)
	requireSystemLockStatus(t, repo, service.IdempotencyStatusSucceeded)
}

func TestSystemHandlerRollbackWithVersionCallsRollbackToVersion(t *testing.T) {
	updateSvc := &systemHandlerUpdateServiceStub{}
	repo := newMemoryIdempotencyRepoStub()
	router := newSystemHandlerTestRouter(t, updateSvc, repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/rollback",
		strings.NewReader(`{"version":"0.1.146"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "rollback-to-146")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 0, updateSvc.rollbackCall)
	require.Equal(t, 1, updateSvc.rollbackToCall)
	require.Equal(t, []string{"0.1.146"}, updateSvc.rollbackToVersions)
	requireSystemLockStatus(t, repo, service.IdempotencyStatusSucceeded)

	var body systemUpdateResponseEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	require.Equal(t, "Rollback completed. Please restart the service.", body.Data.Message)
}

func TestSystemHandlerRollbackWithDisallowedVersionReturnsBadRequest(t *testing.T) {
	updateSvc := &systemHandlerUpdateServiceStub{
		rollbackToErr: service.ErrRollbackVersionNotAllowed,
	}
	repo := newMemoryIdempotencyRepoStub()
	router := newSystemHandlerTestRouter(t, updateSvc, repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/rollback",
		strings.NewReader(`{"version":"9.9.9"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "rollback-to-bad")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, 1, updateSvc.rollbackToCall)
}

func TestSystemHandlerGetRollbackVersions(t *testing.T) {
	updateSvc := &systemHandlerUpdateServiceStub{
		rollbackVersions: []service.RollbackVersion{
			{Version: "0.1.146", PublishedAt: "2026-07-07T00:00:00Z", HTMLURL: "https://example.com/v0.1.146"},
			{Version: "0.1.145", PublishedAt: "2026-07-06T00:00:00Z", HTMLURL: "https://example.com/v0.1.145"},
		},
	}
	repo := newMemoryIdempotencyRepoStub()
	router := newSystemHandlerTestRouter(t, updateSvc, repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/rollback-versions", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, updateSvc.rollbackVersionsCall)

	var body struct {
		Code int `json:"code"`
		Data struct {
			Versions []service.RollbackVersion `json:"versions"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	require.Len(t, body.Data.Versions, 2)
	require.Equal(t, "0.1.146", body.Data.Versions[0].Version)
}

func TestSystemHandlerGetRollbackVersionsError(t *testing.T) {
	updateSvc := &systemHandlerUpdateServiceStub{
		rollbackVersionsErr: errors.New("github unavailable"),
	}
	repo := newMemoryIdempotencyRepoStub()
	router := newSystemHandlerTestRouter(t, updateSvc, repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/rollback-versions", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}
