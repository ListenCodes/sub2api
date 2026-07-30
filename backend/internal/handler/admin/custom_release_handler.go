package admin

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type customReleaseService interface {
	CheckCustomRelease(context.Context, bool) (*service.CustomReleaseInfo, error)
	CustomReleaseNoticeUnread(context.Context, int64, string) (bool, error)
	MarkCustomReleaseNoticeRead(context.Context, int64, string) error
	CurrentRelease(context.Context) (*service.ReleaseRecord, error)
	ListRollbackReleases(context.Context) ([]service.ReleaseRecord, error)
	PrepareUpdate(context.Context) (*service.UpdateJob, error)
	ApplyUpdate(context.Context, string) (*service.UpdateJob, error)
	PrepareRollback(context.Context, string) (*service.UpdateJob, error)
	ApplyRollback(context.Context, string) (*service.UpdateJob, error)
	GetUpdateStatus(context.Context, string) (*service.UpdateJob, error)
}

var customReleaseFingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (h *SystemHandler) customReleaseService() (customReleaseService, error) {
	custom, ok := h.updateSvc.(customReleaseService)
	if !ok {
		return nil, fmt.Errorf("custom release service unavailable")
	}
	return custom, nil
}

func (h *SystemHandler) CheckCustomRelease(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not found in context")
		return
	}
	custom, err := h.customReleaseService()
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	info, err := custom.CheckCustomRelease(c.Request.Context(), c.Query("force") == "true")
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	decorated := *info
	if decorated.UpdateFingerprint != "" {
		decorated.NoticeUnread, err = custom.CustomReleaseNoticeUnread(c.Request.Context(), subject.UserID, decorated.UpdateFingerprint)
		if err != nil {
			decorated.NoticeUnread = true
			decorated.NoticeWarning = "update notice read state is unavailable"
		}
	}
	response.Success(c, &decorated)
}

func (h *SystemHandler) MarkCustomReleaseRead(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not found in context")
		return
	}
	custom, err := h.customReleaseService()
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	var request struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	fingerprint := strings.TrimSpace(request.Fingerprint)
	if !customReleaseFingerprintPattern.MatchString(fingerprint) {
		response.Error(c, http.StatusBadRequest, "invalid update fingerprint")
		return
	}
	err = custom.MarkCustomReleaseNoticeRead(c.Request.Context(), subject.UserID, fingerprint)
	response.Success(c, gin.H{
		"fingerprint": fingerprint,
		"persisted":   err == nil,
	})
}

func (h *SystemHandler) CurrentRelease(c *gin.Context) {
	custom, err := h.customReleaseService()
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	release, err := custom.CurrentRelease(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, release)
}

func (h *SystemHandler) ListRollbackReleases(c *gin.Context) {
	custom, err := h.customReleaseService()
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	releases, err := custom.ListRollbackReleases(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, releases)
}

func (h *SystemHandler) PrepareUpdate(c *gin.Context) {
	if !requireCustomReleaseIdempotencyKey(c) {
		return
	}
	custom, err := h.customReleaseService()
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
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

		job, err := custom.PrepareUpdate(ctx)
		if err != nil {
			releaseReason = "SYSTEM_UPDATE_PREPARE_FAILED"
			return nil, err
		}
		succeeded = true
		return customReleaseJobResponse(job, lock.OperationID()), nil
	})
}

func (h *SystemHandler) ApplyUpdate(c *gin.Context) {
	if !requireCustomReleaseIdempotencyKey(c) {
		return
	}
	custom, err := h.customReleaseService()
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	var request struct {
		JobID string `json:"job_id"`
	}
	if c.Request.Body == nil || c.Request.ContentLength == 0 {
		response.ErrorFrom(c, service.ErrUpdateJobIDRequired)
		return
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	jobID := strings.TrimSpace(request.JobID)
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

		job, err := custom.ApplyUpdate(ctx, jobID)
		if err != nil {
			releaseReason = "SYSTEM_UPDATE_APPLY_FAILED"
			return nil, err
		}
		succeeded = true
		return customReleaseJobResponse(job, lock.OperationID()), nil
	})
}

func (h *SystemHandler) PrepareRollback(c *gin.Context) {
	if !requireCustomReleaseIdempotencyKey(c) {
		return
	}
	custom, err := h.customReleaseService()
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	var request struct {
		ReleaseID string `json:"release_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	releaseID := strings.TrimSpace(request.ReleaseID)
	if releaseID == "" {
		response.ErrorFrom(c, service.ErrRollbackReleaseInvalid)
		return
	}
	operationID := buildSystemOperationID(c, "rollback.prepare:"+releaseID)
	payload := gin.H{"operation_id": operationID, "release_id": releaseID}
	updateCtx, cancel := systemUpdateContext(c.Request.Context())
	defer cancel()
	c.Request = c.Request.WithContext(updateCtx)
	executeAdminIdempotentAcceptedJSON(c, "admin.system.rollback.prepare", payload, service.DefaultSystemOperationIdempotencyTTL(), func(ctx context.Context) (any, error) {
		lock, release, err := h.acquireSystemLock(ctx, operationID)
		if err != nil {
			return nil, err
		}
		var releaseReason string
		succeeded := false
		defer func() { release(releaseReason, succeeded) }()
		job, err := custom.PrepareRollback(ctx, releaseID)
		if err != nil {
			releaseReason = "SYSTEM_ROLLBACK_PREPARE_FAILED"
			return nil, err
		}
		succeeded = true
		return customReleaseJobResponse(job, lock.OperationID()), nil
	})
}

func (h *SystemHandler) ApplyRollback(c *gin.Context) {
	if !requireCustomReleaseIdempotencyKey(c) {
		return
	}
	custom, err := h.customReleaseService()
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	var request struct {
		JobID string `json:"job_id"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	jobID := strings.TrimSpace(request.JobID)
	if jobID == "" {
		response.ErrorFrom(c, service.ErrUpdateJobIDRequired)
		return
	}
	operationID := buildSystemOperationID(c, "rollback.apply:"+jobID)
	payload := gin.H{"operation_id": operationID, "job_id": jobID}
	updateCtx, cancel := systemUpdateContext(c.Request.Context())
	defer cancel()
	c.Request = c.Request.WithContext(updateCtx)
	executeAdminIdempotentAcceptedJSON(c, "admin.system.rollback.apply", payload, service.DefaultSystemOperationIdempotencyTTL(), func(ctx context.Context) (any, error) {
		lock, release, err := h.acquireSystemLock(ctx, operationID)
		if err != nil {
			return nil, err
		}
		var releaseReason string
		succeeded := false
		defer func() { release(releaseReason, succeeded) }()
		job, err := custom.ApplyRollback(ctx, jobID)
		if err != nil {
			releaseReason = "SYSTEM_ROLLBACK_APPLY_FAILED"
			return nil, err
		}
		succeeded = true
		return customReleaseJobResponse(job, lock.OperationID()), nil
	})
}

func (h *SystemHandler) GetUpdateStatus(c *gin.Context) {
	custom, err := h.customReleaseService()
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	job, err := custom.GetUpdateStatus(c.Request.Context(), strings.TrimSpace(c.Query("job_id")))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, job)
}

func (h *SystemHandler) LegacyRollbackUnsupported(c *gin.Context) {
	response.Error(c, http.StatusConflict, "LEGACY_ROLLBACK_UNSUPPORTED")
}

func requireCustomReleaseIdempotencyKey(c *gin.Context) bool {
	key, err := service.NormalizeIdempotencyKey(c.GetHeader("Idempotency-Key"))
	if err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	if key == "" {
		response.ErrorFrom(c, service.ErrIdempotencyKeyRequired)
		return false
	}
	return true
}

func customReleaseJobResponse(job *service.UpdateJob, operationID string) gin.H {
	return gin.H{
		"job_id": job.JobID, "operation_kind": job.OperationKind, "action": job.Action, "status": job.Status, "message": job.Message,
		"base_release_id": job.BaseReleaseID, "target_release_id": job.TargetReleaseID,
		"current_official_version": job.CurrentOfficialVersion, "current_custom_version": job.CurrentCustomVersion,
		"target_official_version": job.TargetOfficialVersion, "target_custom_version": job.TargetCustomVersion,
		"proposed_custom_sequence": job.ProposedCustomSequence, "advances_custom_version": job.AdvancesCustomVersion,
		"integration_branch": job.IntegrationBranch, "base_commit": job.BaseCommit, "target_commit": job.TargetCommit,
		"target_custom_commit": job.TargetCustomCommit, "update_kind": job.UpdateKind, "production_commit": job.ProductionCommit,
		"stable_release_tag": job.StableReleaseTag, "stable_release_commit": job.StableReleaseCommit,
		"release_tag": job.ReleaseTag, "release_commit": job.ReleaseCommit, "release_published_at": job.ReleasePublishedAt,
		"workflow_url": job.WorkflowURL, "main_digest": job.MainDigest, "extensions_digest": job.ExtensionsDigest,
		"conflict_files": job.ConflictFiles, "conflict_base": job.ConflictBase, "conflict_upstream": job.ConflictUpstream,
		"conflict_release": job.ConflictRelease, "conflict_log": job.ConflictLog, "resolution_hint": job.ResolutionHint,
		"artifact_path": job.ArtifactPath, "prepared_manifest": job.PreparedManifest,
		"prepared_manifest_sha256": job.PreparedManifestSHA256, "prepared_at": job.PreparedAt, "expires_at": job.ExpiresAt,
		"need_restart": job.NeedRestart, "published": job.Published, "published_commit": job.PublishedCommit,
		"production_changed": job.ProductionChanged, "error_code": job.ErrorCode, "rollback": job.Rollback,
		"ts": job.Timestamp, "updated_at": job.UpdatedAt, "started_at": job.StartedAt, "finished_at": job.FinishedAt,
		"operation_id": operationID,
	}
}
