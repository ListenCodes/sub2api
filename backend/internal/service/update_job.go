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
	UpdateStatusRunning = "running"
	UpdateStatusSuccess = "success"
	UpdateStatusFailed  = "failed"

	defaultUpdateScriptPath = "/app/scripts/sync-upstream.sh"
	defaultUpdateStatusPath = "/app/data/sync-status"
	defaultUpdateJobIDPath  = "/app/data/sync-job-id"
)

var (
	ErrUpdateJobNotFound   = infraerrors.NotFound("UPDATE_JOB_NOT_FOUND", "update job not found")
	ErrUpdateJobIDRequired = infraerrors.BadRequest("UPDATE_JOB_ID_REQUIRED", "job id is required")
	ErrUpdateInProgress    = infraerrors.Conflict("UPDATE_IN_PROGRESS", "an upstream update is already running")
)

type UpdateJob struct {
	JobID              string     `json:"job_id"`
	Status             string     `json:"status"`
	Message            string     `json:"message"`
	IntegrationBranch  string     `json:"integration_branch,omitempty"`
	BaseCommit         string     `json:"base_commit,omitempty"`
	ReleaseTag         string     `json:"release_tag,omitempty"`
	ReleaseCommit      string     `json:"release_commit,omitempty"`
	ReleasePublishedAt string     `json:"release_published_at,omitempty"`
	ConflictFiles      []string   `json:"conflict_files,omitempty"`
	ConflictBase       string     `json:"conflict_base,omitempty"`
	ConflictUpstream   string     `json:"conflict_upstream,omitempty"`
	ConflictRelease    string     `json:"conflict_release,omitempty"`
	ConflictLog        string     `json:"conflict_log,omitempty"`
	ResolutionHint     string     `json:"resolution_hint,omitempty"`
	NeedRestart        bool       `json:"need_restart"`
	Published          bool       `json:"published"`
	PublishedCommit    string     `json:"published_commit,omitempty"`
	Timestamp          time.Time  `json:"ts"`
	StartedAt          *time.Time `json:"started_at"`
	FinishedAt         *time.Time `json:"finished_at"`
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
	if expectedJobID != "" && job.JobID != expectedJobID {
		return nil, ErrUpdateJobNotFound
	}
	return &job, nil
}

func writeUpdateStatus(path string, job *UpdateJob) error {
	if job == nil || strings.TrimSpace(job.JobID) == "" {
		return fmt.Errorf("update status job is required")
	}
	if job.Status != UpdateStatusRunning && job.Status != UpdateStatusSuccess && job.Status != UpdateStatusFailed {
		return fmt.Errorf("invalid update status %q", job.Status)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create update status directory: %w", err)
	}
	raw, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("encode update status: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sync-status-*")
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
		return nil, ErrUpdateJobIDRequired
	}
	return readUpdateStatus(s.statusPath, jobID)
}

func (s *UpdateService) setUpdateStatus(jobID, status, message string, startedAt, finishedAt *time.Time) error {
	return writeUpdateStatus(s.statusPath, &UpdateJob{
		JobID:       jobID,
		Status:      status,
		Message:     message,
		NeedRestart: false,
		Published:   false,
		Timestamp:   time.Now().UTC(),
		StartedAt:   startedAt,
		FinishedAt:  finishedAt,
	})
}
