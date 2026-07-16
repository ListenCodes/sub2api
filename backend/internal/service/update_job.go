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
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	UpdateStatusCheckingRelease     = "checking_release"
	UpdateStatusValidatingTag       = "validating_tag"
	UpdateStatusMergingRelease      = "merging_release"
	UpdateStatusWaitingActions      = "waiting_actions"
	UpdateStatusWaitingImages       = "waiting_images"
	UpdateStatusPromotingRelease    = "promoting_release"
	UpdateStatusBackingUp           = "backing_up"
	UpdateStatusDeployingExtensions = "deploying_extensions"
	UpdateStatusDeployingMain       = "deploying_main"
	UpdateStatusHealthChecking      = "health_checking"
	UpdateStatusRollingBack         = "rolling_back"
	UpdateStatusSuccess             = "success"
	UpdateStatusFailed              = "failed"
	UpdateStatusConflict            = "conflict"

	defaultUpdateScriptPath = "/app/scripts/sync-upstream.sh"
	defaultUpdateJobsDir    = "/app/data/release-jobs"
	defaultUpdateJobIDPath  = "/app/data/release-current-job-id"
)

var (
	ErrUpdateJobNotFound   = infraerrors.NotFound("UPDATE_JOB_NOT_FOUND", "update job not found")
	ErrUpdateJobIDRequired = infraerrors.BadRequest("UPDATE_JOB_ID_REQUIRED", "job id is required")
	ErrUpdateInProgress    = infraerrors.Conflict("UPDATE_IN_PROGRESS", "an upstream update is already running")
)

type UpdateJob struct {
	JobID              string         `json:"job_id"`
	Status             string         `json:"status"`
	Message            string         `json:"message"`
	IntegrationBranch  string         `json:"integration_branch,omitempty"`
	BaseCommit         string         `json:"base_commit,omitempty"`
	ReleaseTag         string         `json:"release_tag,omitempty"`
	ReleaseCommit      string         `json:"release_commit,omitempty"`
	ReleasePublishedAt string         `json:"release_published_at,omitempty"`
	ConflictFiles      []string       `json:"conflict_files,omitempty"`
	ConflictBase       string         `json:"conflict_base,omitempty"`
	ConflictUpstream   string         `json:"conflict_upstream,omitempty"`
	ConflictRelease    string         `json:"conflict_release,omitempty"`
	ConflictLog        string         `json:"conflict_log,omitempty"`
	ResolutionHint     string         `json:"resolution_hint,omitempty"`
	NeedRestart        bool           `json:"need_restart"`
	Published          bool           `json:"published"`
	PublishedCommit    string         `json:"published_commit,omitempty"`
	TargetCommit       string         `json:"target_commit,omitempty"`
	WorkflowURL        string         `json:"workflow_url,omitempty"`
	MainDigest         string         `json:"main_digest,omitempty"`
	ExtensionsDigest   string         `json:"extensions_digest,omitempty"`
	ProductionChanged  bool           `json:"production_changed"`
	ErrorCode          string         `json:"error_code,omitempty"`
	ArtifactPath       string         `json:"artifact_path,omitempty"`
	Rollback           UpdateRollback `json:"rollback"`
	Timestamp          time.Time      `json:"ts"`
	UpdatedAt          time.Time      `json:"updated_at"`
	StartedAt          *time.Time     `json:"started_at"`
	FinishedAt         *time.Time     `json:"finished_at"`
}

type UpdateRollback struct {
	Attempted bool   `json:"attempted"`
	Succeeded bool   `json:"succeeded"`
	Message   string `json:"message"`
}

func isValidUpdateStatus(status string) bool {
	switch status {
	case UpdateStatusCheckingRelease,
		UpdateStatusValidatingTag,
		UpdateStatusMergingRelease,
		UpdateStatusWaitingActions,
		UpdateStatusWaitingImages,
		UpdateStatusPromotingRelease,
		UpdateStatusBackingUp,
		UpdateStatusDeployingExtensions,
		UpdateStatusDeployingMain,
		UpdateStatusHealthChecking,
		UpdateStatusRollingBack,
		UpdateStatusSuccess,
		UpdateStatusFailed,
		UpdateStatusConflict:
		return true
	default:
		return false
	}
}

func IsTerminalUpdateStatus(status string) bool {
	return status == UpdateStatusSuccess || status == UpdateStatusFailed || status == UpdateStatusConflict
}

func newUpdateJobID() (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate update job id: %w", err)
	}
	return fmt.Sprintf("update-%d-%s", time.Now().UnixNano(), hex.EncodeToString(random[:])), nil
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
	if !isValidUpdateStatus(job.Status) {
		return nil, fmt.Errorf("invalid update status %q", job.Status)
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
	if !isValidUpdateStatus(job.Status) {
		return fmt.Errorf("invalid update status %q", job.Status)
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
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary update status: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace update status: %w", err)
	}
	return nil
}

func (s *UpdateService) GetUpdateStatus(ctx context.Context, jobID string) (*UpdateJob, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		current, err := os.ReadFile(s.jobIDPath)
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
	path, err := updateJobPath(s.jobsDir, jobID)
	if err != nil {
		return nil, ErrUpdateJobNotFound
	}
	return readUpdateStatus(path, jobID)
}

func (s *UpdateService) setUpdateStatus(jobID, status, message string, startedAt, finishedAt *time.Time) error {
	path, err := updateJobPath(s.jobsDir, jobID)
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
	if !strings.HasPrefix(jobID, "update-") || len(jobID) > 128 {
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
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary current update job id: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace current update job id: %w", err)
	}
	return nil
}
