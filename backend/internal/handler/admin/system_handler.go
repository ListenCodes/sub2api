package admin

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/sysutil"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// SystemHandler handles system-related operations
type SystemHandler struct {
	updateSvc systemUpdateService
	lockSvc   *service.SystemOperationLockService
}

// systemUpdateTimeout bounds a durable release trigger or versioned rollback.
// A rollback can include a large binary download over slow links, so this must
// stay above the GitHub download client timeout (10 minutes).
const systemUpdateTimeout = 15 * time.Minute

// systemUpdateContext detaches update/rollback work from the HTTP request
// lifetime. This keeps the durable trigger, idempotency record, system lock,
// and any rollback download alive after a client disconnect while retaining a
// finite server-side deadline (#4504).
func systemUpdateContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, systemUpdateTimeout)
}

type systemUpdateService interface {
	CheckUpdate(ctx context.Context, force bool) (*service.UpdateInfo, error)
	PerformUpdate(ctx context.Context) (*service.UpdateJob, error)
	PrepareUpdate(ctx context.Context) (*service.UpdateJob, error)
	ApplyUpdate(ctx context.Context, jobID string) (*service.UpdateJob, error)
	GetUpdateStatus(ctx context.Context, jobID string) (*service.UpdateJob, error)
	Rollback() error
	ListRollbackVersions(ctx context.Context) ([]service.RollbackVersion, error)
	RollbackToVersion(ctx context.Context, version string) error
}

// PrepareUpdate starts only the durable download/backup preparation phase.
// POST /api/v1/admin/system/update/prepare
func (h *SystemHandler) PrepareUpdate(c *gin.Context) {
	operationID := buildSystemOperationID(c, "update.prepare")
	payload := gin.H{"operation_id": operationID}
	updateCtx, cancel := systemUpdateContext(c.Request.Context())
	defer cancel()
	c.Request = c.Request.WithContext(updateCtx)
	executeAdminIdempotentAcceptedJSON(c, "admin.system.update.prepare", payload, service.DefaultSystemOperationIdempotencyTTL(), func(ctx context.Context) (any, error) {
		lock, release, err := h.acquireSystemLock(ctx, operationID)
		if err != nil {
			return nil, err
		}
		var releaseReason string
		succeeded := false
		defer func() { release(releaseReason, succeeded) }()

		job, err := h.updateSvc.PrepareUpdate(ctx)
		if err != nil {
			releaseReason = "SYSTEM_UPDATE_PREPARE_FAILED"
			return nil, err
		}
		succeeded = true
		return updateJobResponse(job, lock.OperationID()), nil
	})
}

// ApplyUpdate starts the explicit production switch for a prepared job.
// POST /api/v1/admin/system/update/apply
func (h *SystemHandler) ApplyUpdate(c *gin.Context) {
	var req struct {
		JobID string `json:"job_id"`
	}
	if c.Request.Body == nil || c.Request.ContentLength == 0 {
		response.ErrorFrom(c, service.ErrUpdateJobIDRequired)
		return
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	jobID := strings.TrimSpace(req.JobID)
	if jobID == "" {
		response.ErrorFrom(c, service.ErrUpdateJobIDRequired)
		return
	}
	operationID := buildSystemOperationID(c, "update.apply:"+jobID)
	payload := gin.H{"operation_id": operationID, "job_id": jobID}
	updateCtx, cancel := systemUpdateContext(c.Request.Context())
	defer cancel()
	c.Request = c.Request.WithContext(updateCtx)
	executeAdminIdempotentAcceptedJSON(c, "admin.system.update.apply", payload, service.DefaultSystemOperationIdempotencyTTL(), func(ctx context.Context) (any, error) {
		lock, release, err := h.acquireSystemLock(ctx, operationID)
		if err != nil {
			return nil, err
		}
		var releaseReason string
		succeeded := false
		defer func() { release(releaseReason, succeeded) }()

		job, err := h.updateSvc.ApplyUpdate(ctx, jobID)
		if err != nil {
			releaseReason = "SYSTEM_UPDATE_APPLY_FAILED"
			return nil, err
		}
		succeeded = true
		return updateJobResponse(job, lock.OperationID()), nil
	})
}

// NewSystemHandler creates a new SystemHandler
func NewSystemHandler(updateSvc systemUpdateService, lockSvc *service.SystemOperationLockService) *SystemHandler {
	return &SystemHandler{
		updateSvc: updateSvc,
		lockSvc:   lockSvc,
	}
}

// GetVersion returns the current version
// GET /api/v1/admin/system/version
func (h *SystemHandler) GetVersion(c *gin.Context) {
	info, _ := h.updateSvc.CheckUpdate(c.Request.Context(), false)
	response.Success(c, gin.H{
		"version": info.CurrentVersion,
	})
}

// CheckUpdates checks for available updates
// GET /api/v1/admin/system/check-updates
func (h *SystemHandler) CheckUpdates(c *gin.Context) {
	force := c.Query("force") == "true"
	info, err := h.updateSvc.CheckUpdate(c.Request.Context(), force)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, info)
}

// PerformUpdate downloads and applies the update
// POST /api/v1/admin/system/update
func (h *SystemHandler) PerformUpdate(c *gin.Context) {
	operationID := buildSystemOperationID(c, "update")
	payload := gin.H{"operation_id": operationID}
	updateCtx, cancel := systemUpdateContext(c.Request.Context())
	defer cancel()
	c.Request = c.Request.WithContext(updateCtx)
	executeAdminIdempotentAcceptedJSON(c, "admin.system.update", payload, service.DefaultSystemOperationIdempotencyTTL(), func(ctx context.Context) (any, error) {
		lock, release, err := h.acquireSystemLock(ctx, operationID)
		if err != nil {
			return nil, err
		}
		var releaseReason string
		succeeded := false
		defer func() {
			release(releaseReason, succeeded)
		}()

		// The legacy endpoint is a prepare-only compatibility alias. It never
		// enters the production apply phase.
		job, err := h.updateSvc.PrepareUpdate(ctx)
		if err != nil {
			releaseReason = "SYSTEM_UPDATE_FAILED"
			return nil, err
		}
		succeeded = true

		return gin.H{
			"job_id":               job.JobID,
			"status":               job.Status,
			"message":              job.Message,
			"integration_branch":   job.IntegrationBranch,
			"base_commit":          job.BaseCommit,
			"target_commit":        job.TargetCommit,
			"release_tag":          job.ReleaseTag,
			"release_commit":       job.ReleaseCommit,
			"release_published_at": job.ReleasePublishedAt,
			"workflow_url":         job.WorkflowURL,
			"main_digest":          job.MainDigest,
			"extensions_digest":    job.ExtensionsDigest,
			"conflict_files":       job.ConflictFiles,
			"conflict_base":        job.ConflictBase,
			"conflict_upstream":    job.ConflictUpstream,
			"conflict_release":     job.ConflictRelease,
			"conflict_log":         job.ConflictLog,
			"resolution_hint":      job.ResolutionHint,
			"artifact_path":        job.ArtifactPath,
			"need_restart":         job.NeedRestart,
			"published":            job.Published,
			"published_commit":     job.PublishedCommit,
			"production_changed":   job.ProductionChanged,
			"error_code":           job.ErrorCode,
			"rollback":             job.Rollback,
			"ts":                   job.Timestamp,
			"updated_at":           job.UpdatedAt,
			"started_at":           job.StartedAt,
			"finished_at":          job.FinishedAt,
			"operation_id":         lock.OperationID(),
		}, nil
	})
}

func updateJobResponse(job *service.UpdateJob, operationID string) gin.H {
	return gin.H{
		"job_id":                   job.JobID,
		"action":                   job.Action,
		"status":                   job.Status,
		"message":                  job.Message,
		"integration_branch":       job.IntegrationBranch,
		"base_commit":              job.BaseCommit,
		"target_commit":            job.TargetCommit,
		"target_custom_commit":     job.TargetCustomCommit,
		"update_kind":              job.UpdateKind,
		"production_commit":        job.ProductionCommit,
		"stable_release_tag":       job.StableReleaseTag,
		"stable_release_commit":    job.StableReleaseCommit,
		"release_tag":              job.ReleaseTag,
		"release_commit":           job.ReleaseCommit,
		"release_published_at":     job.ReleasePublishedAt,
		"workflow_url":             job.WorkflowURL,
		"main_digest":              job.MainDigest,
		"extensions_digest":        job.ExtensionsDigest,
		"conflict_files":           job.ConflictFiles,
		"conflict_base":            job.ConflictBase,
		"conflict_upstream":        job.ConflictUpstream,
		"conflict_release":         job.ConflictRelease,
		"conflict_log":             job.ConflictLog,
		"resolution_hint":          job.ResolutionHint,
		"artifact_path":            job.ArtifactPath,
		"prepared_manifest":        job.PreparedManifest,
		"prepared_manifest_sha256": job.PreparedManifestSHA256,
		"prepared_at":              job.PreparedAt,
		"expires_at":               job.ExpiresAt,
		"need_restart":             job.NeedRestart,
		"published":                job.Published,
		"published_commit":         job.PublishedCommit,
		"production_changed":       job.ProductionChanged,
		"error_code":               job.ErrorCode,
		"rollback":                 job.Rollback,
		"ts":                       job.Timestamp,
		"updated_at":               job.UpdatedAt,
		"started_at":               job.StartedAt,
		"finished_at":              job.FinishedAt,
		"operation_id":             operationID,
	}
}

// GetUpdateStatus returns the current status for an asynchronous upstream update.
// GET /api/v1/admin/system/update/status?job_id=...
func (h *SystemHandler) GetUpdateStatus(c *gin.Context) {
	jobID := strings.TrimSpace(c.Query("job_id"))
	job, err := h.updateSvc.GetUpdateStatus(c.Request.Context(), jobID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, job)
}

// GetRollbackVersions lists versions available for rollback
// GET /api/v1/admin/system/rollback-versions
func (h *SystemHandler) GetRollbackVersions(c *gin.Context) {
	versions, err := h.updateSvc.ListRollbackVersions(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	response.Success(c, gin.H{
		"versions": versions,
	})
}

// Rollback restores a previous version.
// Without a body (or with an empty version) it restores the local .backup binary
// left by the last in-place update. With {"version": "x.y.z"} it downloads and
// installs that specific release (must be one of the recent rollback versions).
// POST /api/v1/admin/system/rollback
func (h *SystemHandler) Rollback(c *gin.Context) {
	var req struct {
		Version string `json:"version"`
	}
	if c.Request.Body != nil && c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Error(c, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	targetVersion := strings.TrimSpace(req.Version)

	operation := "rollback"
	if targetVersion != "" {
		operation = "rollback:" + targetVersion
	}
	operationID := buildSystemOperationID(c, operation)
	payload := gin.H{"operation_id": operationID, "version": targetVersion}
	if targetVersion != "" {
		rollbackCtx, cancel := systemUpdateContext(c.Request.Context())
		defer cancel()
		c.Request = c.Request.WithContext(rollbackCtx)
	}
	executeAdminIdempotentJSON(c, "admin.system.rollback", payload, service.DefaultSystemOperationIdempotencyTTL(), func(ctx context.Context) (any, error) {
		lock, release, err := h.acquireSystemLock(ctx, operationID)
		if err != nil {
			return nil, err
		}
		var releaseReason string
		succeeded := false
		defer func() {
			release(releaseReason, succeeded)
		}()

		if targetVersion != "" {
			err = h.updateSvc.RollbackToVersion(ctx, targetVersion)
		} else {
			err = h.updateSvc.Rollback()
		}
		if err != nil {
			releaseReason = "SYSTEM_ROLLBACK_FAILED"
			return nil, err
		}
		succeeded = true

		return gin.H{
			"message":      "Rollback completed. Please restart the service.",
			"need_restart": true,
			"version":      targetVersion,
			"operation_id": lock.OperationID(),
		}, nil
	})
}

// RestartService restarts the systemd service
// POST /api/v1/admin/system/restart
func (h *SystemHandler) RestartService(c *gin.Context) {
	operationID := buildSystemOperationID(c, "restart")
	payload := gin.H{"operation_id": operationID}
	executeAdminIdempotentJSON(c, "admin.system.restart", payload, service.DefaultSystemOperationIdempotencyTTL(), func(ctx context.Context) (any, error) {
		lock, release, err := h.acquireSystemLock(ctx, operationID)
		if err != nil {
			return nil, err
		}
		succeeded := false
		defer func() {
			release("", succeeded)
		}()

		// Schedule service restart in background after sending response
		// This ensures the client receives the success response before the service restarts
		go func() {
			// Wait a moment to ensure the response is sent
			time.Sleep(500 * time.Millisecond)
			sysutil.RestartServiceAsync()
		}()
		succeeded = true
		return gin.H{
			"message":      "Service restart initiated",
			"operation_id": lock.OperationID(),
		}, nil
	})
}

func (h *SystemHandler) acquireSystemLock(
	ctx context.Context,
	operationID string,
) (*service.SystemOperationLock, func(string, bool), error) {
	if h.lockSvc == nil {
		return nil, nil, service.ErrIdempotencyStoreUnavail
	}
	lock, err := h.lockSvc.Acquire(ctx, operationID)
	if err != nil {
		return nil, nil, err
	}
	release := func(reason string, succeeded bool) {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = h.lockSvc.Release(releaseCtx, lock, succeeded, reason)
	}
	return lock, release, nil
}

func buildSystemOperationID(c *gin.Context, operation string) string {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		return "sysop-" + operation + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	actorScope := "admin:0"
	if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok {
		actorScope = "admin:" + strconv.FormatInt(subject.UserID, 10)
	}
	seed := operation + "|" + actorScope + "|" + c.FullPath() + "|" + key
	hash := service.HashIdempotencyKey(seed)
	if len(hash) > 24 {
		hash = hash[:24]
	}
	return "sysop-" + hash
}
