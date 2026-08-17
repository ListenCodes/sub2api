package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	ReleaseOperationUpdate   = "update"
	ReleaseOperationRollback = "rollback"
	ReleasePhasePrepare      = "prepare"
	ReleasePhaseApply        = "apply"
	ReleasePhaseExpire       = "expire"

	ReleaseStatusResolvingTarget     = "resolving_target"
	ReleaseStatusResolvingSnapshot   = "resolving_snapshot"
	ReleaseStatusVerifyingSnapshot   = "verifying_snapshot"
	ReleaseStatusVerifyingImages     = "verifying_images"
	ReleaseStatusDownloadingImages   = "downloading_images"
	ReleaseStatusRenderingCompose    = "rendering_compose"
	ReleaseStatusBackingUp           = "backing_up"
	ReleaseStatusValidatingBackup    = "validating_backup"
	ReleaseStatusPrepared            = "prepared"
	ReleaseStatusApplyQueued         = "apply_queued"
	ReleaseStatusValidatingManifest  = "validating_manifest"
	ReleaseStatusSwitchingExtensions = "switching_extensions"
	ReleaseStatusSwitchingMain       = "switching_main"
	ReleaseStatusHealthChecking      = "health_checking"
	ReleaseStatusRollingBack         = "rolling_back"
	ReleaseStatusSuccess             = "success"
	ReleaseStatusFailed              = "failed"
	ReleaseStatusConflict            = "conflict"
	ReleaseStatusExpired             = "expired"
	ReleaseStatusDrifted             = "drifted"
	ReleaseStatusFailedRolledBack    = "failed_rolled_back"
	ReleaseStatusRollbackFailed      = "rollback_failed"

	UpdateActionPrepare = ReleasePhasePrepare
	UpdateActionApply   = ReleasePhaseApply

	UpdateStatusCheckingUpdates     = ReleaseStatusResolvingTarget
	UpdateStatusCheckingRelease     = ReleaseStatusResolvingTarget
	UpdateStatusValidatingTag       = ReleaseStatusResolvingTarget
	UpdateStatusMergingRelease      = ReleaseStatusResolvingTarget
	UpdateStatusWaitingActions      = ReleaseStatusResolvingTarget
	UpdateStatusWaitingImages       = ReleaseStatusVerifyingImages
	UpdateStatusDownloadingImages   = ReleaseStatusDownloadingImages
	UpdateStatusPreparingCompose    = ReleaseStatusRenderingCompose
	UpdateStatusPromotingRelease    = ReleaseStatusResolvingTarget
	UpdateStatusBackingUp           = ReleaseStatusBackingUp
	UpdateStatusValidatingBackup    = ReleaseStatusValidatingBackup
	UpdateStatusPrepared            = ReleaseStatusPrepared
	UpdateStatusApplyQueued         = ReleaseStatusApplyQueued
	UpdateStatusDeployingExtensions = ReleaseStatusSwitchingExtensions
	UpdateStatusDeployingMain       = ReleaseStatusSwitchingMain
	UpdateStatusHealthChecking      = ReleaseStatusHealthChecking
	UpdateStatusRollingBack         = ReleaseStatusRollingBack
	UpdateStatusSuccess             = ReleaseStatusSuccess
	UpdateStatusFailed              = ReleaseStatusFailed
	UpdateStatusConflict            = ReleaseStatusConflict
	UpdateStatusExpired             = ReleaseStatusExpired
	UpdateStatusDrifted             = ReleaseStatusDrifted

	IdentityTransitionStage0SafeReset    = "stage0-safe-reset"
	IdentityTransitionStage1V2           = "stage1-v2"
	IdentityTransitionStage1IP           = "stage1-ip"
	IdentityTransitionStage1Device       = "stage1-device"
	IdentityTransitionStage2Admin        = "stage2-admin"
	IdentityTransitionStage3ShadowWindow = "stage3-shadow-window"
	IdentityTransitionStage3Rules        = "stage3-rules"
	IdentityTransitionStage4Geo          = "stage4-geo"

	defaultUpdateScriptPath = "/app/scripts/sync-upstream.sh"
	defaultUpdateJobsDir    = "/app/data/release-jobs"
	defaultUpdateJobIDPath  = "/app/data/release-current-job-id"
)

var (
	ErrUpdateJobNotFound   = infraerrors.NotFound("UPDATE_JOB_NOT_FOUND", "update job not found")
	ErrUpdateJobIDRequired = infraerrors.BadRequest("UPDATE_JOB_ID_REQUIRED", "job id is required")
	ErrUpdateInProgress    = infraerrors.Conflict("UPDATE_IN_PROGRESS", "an upstream update is already running")
	ErrUpdateNotPrepared   = infraerrors.Conflict("UPDATE_NOT_PREPARED", "update job is not prepared for confirmation")
	ErrUpdateExpired       = infraerrors.Conflict("UPDATE_PREPARATION_EXPIRED", "prepared update has expired; prepare again")
	ErrLegacySinglePhase   = infraerrors.Conflict("LEGACY_SINGLE_PHASE_UNSUPPORTED", "legacy single-phase release jobs cannot be resumed")
)

type UpdateJob struct {
	JobID                  string         `json:"job_id"`
	OperationKind          string         `json:"operation_kind"`
	Action                 string         `json:"action,omitempty"`
	Status                 string         `json:"status"`
	BaseReleaseID          string         `json:"base_release_id,omitempty"`
	BaseCustomHighWater    *int           `json:"base_custom_high_water,omitempty"`
	TargetReleaseID        string         `json:"target_release_id,omitempty"`
	CurrentOfficialVersion string         `json:"current_official_version,omitempty"`
	CurrentCustomVersion   string         `json:"current_custom_version,omitempty"`
	TargetOfficialVersion  string         `json:"target_official_version,omitempty"`
	TargetCustomVersion    string         `json:"target_custom_version,omitempty"`
	ProposedCustomSequence *int           `json:"proposed_custom_sequence,omitempty"`
	AdvancesCustomVersion  bool           `json:"advances_custom_version"`
	Message                string         `json:"message"`
	IntegrationBranch      string         `json:"integration_branch,omitempty"`
	BaseCommit             string         `json:"base_commit,omitempty"`
	ReleaseTag             string         `json:"release_tag,omitempty"`
	ReleaseCommit          string         `json:"release_commit,omitempty"`
	ReleasePublishedAt     string         `json:"release_published_at,omitempty"`
	ConflictFiles          []string       `json:"conflict_files,omitempty"`
	ConflictBase           string         `json:"conflict_base,omitempty"`
	ConflictUpstream       string         `json:"conflict_upstream,omitempty"`
	ConflictRelease        string         `json:"conflict_release,omitempty"`
	ConflictLog            string         `json:"conflict_log,omitempty"`
	ResolutionHint         string         `json:"resolution_hint,omitempty"`
	NeedRestart            bool           `json:"need_restart"`
	Published              bool           `json:"published"`
	PublishedCommit        string         `json:"published_commit,omitempty"`
	TargetCommit           string         `json:"target_commit,omitempty"`
	TargetCustomCommit     string         `json:"target_custom_commit,omitempty"`
	CustomDocsOnly         bool           `json:"custom_docs_only"`
	UpdateKind             string         `json:"update_kind,omitempty"`
	IdentityTransition     string         `json:"identity_transition,omitempty"`
	ProductionCommit       string         `json:"production_commit,omitempty"`
	StableReleaseTag       string         `json:"stable_release_tag,omitempty"`
	StableReleaseCommit    string         `json:"stable_release_commit,omitempty"`
	WorkflowURL            string         `json:"workflow_url,omitempty"`
	MainDigest             string         `json:"main_digest,omitempty"`
	ExtensionsDigest       string         `json:"extensions_digest,omitempty"`
	ProductionChanged      bool           `json:"production_changed"`
	ErrorCode              string         `json:"error_code,omitempty"`
	FailedCheck            string         `json:"failed_check,omitempty"`
	CheckURL               string         `json:"check_url,omitempty"`
	Conclusion             string         `json:"conclusion,omitempty"`
	ArtifactPath           string         `json:"artifact_path,omitempty"`
	PreparedManifest       string         `json:"prepared_manifest,omitempty"`
	PreparedManifestSHA256 string         `json:"prepared_manifest_sha256,omitempty"`
	PreparedAt             string         `json:"prepared_at,omitempty"`
	ExpiresAt              string         `json:"expires_at,omitempty"`
	Rollback               UpdateRollback `json:"rollback"`
	Timestamp              time.Time      `json:"ts"`
	UpdatedAt              time.Time      `json:"updated_at"`
	StartedAt              *time.Time     `json:"started_at"`
	FinishedAt             *time.Time     `json:"finished_at"`
}

type UpdateRollback struct {
	Attempted bool   `json:"attempted"`
	Succeeded bool   `json:"succeeded"`
	Message   string `json:"message"`
}

func isValidUpdateStatus(status string) bool {
	switch status {
	case ReleaseStatusResolvingTarget,
		ReleaseStatusResolvingSnapshot,
		ReleaseStatusVerifyingSnapshot,
		ReleaseStatusVerifyingImages,
		ReleaseStatusDownloadingImages,
		ReleaseStatusRenderingCompose,
		ReleaseStatusBackingUp,
		ReleaseStatusValidatingBackup,
		ReleaseStatusPrepared,
		ReleaseStatusApplyQueued,
		ReleaseStatusValidatingManifest,
		ReleaseStatusSwitchingExtensions,
		ReleaseStatusSwitchingMain,
		ReleaseStatusHealthChecking,
		ReleaseStatusRollingBack,
		ReleaseStatusSuccess,
		ReleaseStatusFailed,
		ReleaseStatusConflict,
		ReleaseStatusExpired,
		ReleaseStatusDrifted,
		ReleaseStatusFailedRolledBack,
		ReleaseStatusRollbackFailed:
		return true
	default:
		return false
	}
}

func IsTerminalUpdateStatus(status string) bool {
	return status == ReleaseStatusSuccess || status == ReleaseStatusFailed || status == ReleaseStatusConflict || status == ReleaseStatusExpired || status == ReleaseStatusDrifted || status == ReleaseStatusFailedRolledBack || status == ReleaseStatusRollbackFailed
}

func IsPollingSettledUpdateStatus(status string) bool {
	return IsTerminalUpdateStatus(status) || status == UpdateStatusPrepared
}

func newReleaseOperationID(kind string) (string, error) {
	if !isValidReleaseOperationKind(kind) {
		return "", fmt.Errorf("invalid release operation kind %q", kind)
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate update job id: %w", err)
	}
	return fmt.Sprintf("%s-%d-%s", kind, time.Now().UnixNano(), hex.EncodeToString(random[:])), nil
}

func readUpdateStatus(path, expectedJobID string) (*UpdateJob, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrUpdateJobNotFound
		}
		return nil, fmt.Errorf("read update status: %w", err)
	}

	var job UpdateJob
	if err := json.Unmarshal(raw, &job); err != nil {
		return nil, fmt.Errorf("decode update status: %w", err)
	}
	if strings.TrimSpace(job.JobID) == "" {
		return nil, fmt.Errorf("update status has no job id")
	}
	if strings.TrimSpace(job.OperationKind) == "" {
		return nil, ErrLegacySinglePhase
	}
	if !isValidReleaseOperationKind(job.OperationKind) || !operationIDPattern.MatchString(job.JobID) || !strings.HasPrefix(job.JobID, job.OperationKind+"-") {
		return nil, fmt.Errorf("invalid release operation identity")
	}
	if !isValidReleasePhase(job.Action) {
		return nil, fmt.Errorf("invalid release operation phase %q", job.Action)
	}
	if !isValidUpdateStatus(job.Status) {
		return nil, fmt.Errorf("invalid update status %q", job.Status)
	}
	if job.UpdateKind == UpdateKindIdentityConfig {
		if !ValidIdentityTransition(job.IdentityTransition) {
			return nil, fmt.Errorf("invalid identity rollout transition")
		}
	} else if job.IdentityTransition != "" {
		return nil, fmt.Errorf("identity transition requires an identity configuration operation")
	}
	if expectedJobID != "" && job.JobID != expectedJobID {
		return nil, ErrUpdateJobNotFound
	}
	return &job, nil
}

func writeUpdateStatus(path string, job *UpdateJob) error {
	if job == nil || strings.TrimSpace(job.JobID) == "" {
		return fmt.Errorf("update status job is required")
	}
	if !isValidReleaseOperationKind(job.OperationKind) || !operationIDPattern.MatchString(job.JobID) || !strings.HasPrefix(job.JobID, job.OperationKind+"-") {
		return fmt.Errorf("invalid release operation identity")
	}
	if !isValidReleasePhase(job.Action) {
		return fmt.Errorf("invalid release operation phase %q", job.Action)
	}
	if !isValidUpdateStatus(job.Status) {
		return fmt.Errorf("invalid update status %q", job.Status)
	}
	if job.UpdateKind == UpdateKindIdentityConfig {
		if !ValidIdentityTransition(job.IdentityTransition) {
			return fmt.Errorf("invalid identity rollout transition")
		}
	} else if job.IdentityTransition != "" {
		return fmt.Errorf("identity transition requires an identity configuration operation")
	}
	now := time.Now().UTC()
	if job.Timestamp.IsZero() {
		job.Timestamp = now
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = now
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create update status directory: %w", err)
	}
	raw, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("encode update status: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".release-job-*")
	if err != nil {
		return fmt.Errorf("create temporary update status: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0644); err != nil {
		return fmt.Errorf("chmod temporary update status: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		return fmt.Errorf("write temporary update status: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temporary update status: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary update status: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace update status: %w", err)
	}
	return syncReleaseDirectory(path)
}

func isValidReleaseOperationKind(kind string) bool {
	return kind == ReleaseOperationUpdate || kind == ReleaseOperationRollback
}

func isValidReleasePhase(phase string) bool {
	return phase == ReleasePhasePrepare || phase == ReleasePhaseApply
}

func (s *UpdateService) GetUpdateStatus(ctx context.Context, jobID string) (*UpdateJob, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		current, err := os.ReadFile(customReleaseJobIDPath())
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, ErrUpdateJobNotFound
			}
			return nil, fmt.Errorf("read current update job id: %w", err)
		}
		jobID = strings.TrimSpace(string(current))
		if jobID == "" {
			return nil, ErrUpdateJobNotFound
		}
	}
	path, err := customReleaseOperationPath(jobID)
	if err != nil {
		if errors.Is(err, ErrLegacySinglePhase) {
			return nil, ErrLegacySinglePhase
		}
		return nil, ErrUpdateJobNotFound
	}
	return readUpdateStatus(path, jobID)
}

func setCustomReleaseStatus(jobID, status, message string, startedAt, finishedAt *time.Time) error {
	path, err := customReleaseOperationPath(jobID)
	if err != nil {
		return err
	}
	job, readErr := readUpdateStatus(path, jobID)
	if readErr != nil && !errors.Is(readErr, ErrUpdateJobNotFound) {
		return readErr
	}
	if job == nil {
		job = &UpdateJob{JobID: jobID}
	}
	now := time.Now().UTC()
	job.Status = status
	job.Message = message
	job.Timestamp = now
	job.UpdatedAt = now
	job.StartedAt = startedAt
	job.FinishedAt = finishedAt
	return writeUpdateStatus(path, job)
}

func updateJobPath(jobsDir, jobID string) (string, error) {
	jobID = strings.TrimSpace(jobID)
	if (!strings.HasPrefix(jobID, "update-") && !strings.HasPrefix(jobID, "rollback-")) || len(jobID) > 128 {
		return "", fmt.Errorf("invalid update job id")
	}
	for _, char := range jobID {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' {
			continue
		}
		return "", fmt.Errorf("invalid update job id")
	}
	return filepath.Join(jobsDir, jobID+".json"), nil
}

func writeCurrentUpdateJobID(path, jobID string) error {
	if _, err := updateJobPath(filepath.Dir(path), jobID); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create current update job directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".release-current-job-id-*")
	if err != nil {
		return fmt.Errorf("create temporary current update job id: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0644); err != nil {
		return fmt.Errorf("chmod temporary current update job id: %w", err)
	}
	if _, err := tmp.WriteString(jobID + "\n"); err != nil {
		return fmt.Errorf("write temporary current update job id: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temporary current update job id: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary current update job id: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace current update job id: %w", err)
	}
	return syncReleaseDirectory(path)
}

func syncReleaseDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open release state directory: %w", err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return fmt.Errorf("sync release state directory: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close release state directory: %w", closeErr)
	}
	return nil
}
