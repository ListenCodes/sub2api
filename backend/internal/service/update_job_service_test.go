//go:build unit

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpdateServicePerformUpdateReturnsJobBeforeScriptCompletes(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "sync-upstream.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 2\n"), 0755))

	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{TagName: "v0.1.133", Name: "v0.1.133"},
		},
		"0.1.132",
		"source",
	)
	svc.scriptPath = scriptPath
	svc.jobsDir = filepath.Join(tmpDir, "release-jobs")
	svc.jobIDPath = filepath.Join(tmpDir, "release-current-job-id")
	svc.startScript = func(string) (func() error, error) {
		return func() error {
			time.Sleep(2 * time.Second)
			return nil
		}, nil
	}

	started := time.Now()
	job, err := svc.PerformUpdate(context.Background())

	require.NoError(t, err)
	require.NotEmpty(t, job.JobID)
	require.Equal(t, UpdateStatusCheckingRelease, job.Status)
	require.False(t, job.NeedRestart)
	require.False(t, job.Published)
	require.Less(t, time.Since(started), 500*time.Millisecond)
}

func TestReadUpdateStatusIncludesPreparationMetadata(t *testing.T) {
	t.Parallel()

	statusPath := filepath.Join(t.TempDir(), "sync-status")
	require.NoError(t, os.WriteFile(statusPath, []byte(`{"job_id":"update-a","status":"success","message":"branch ready","integration_branch":"integration/upstream-20260713","need_restart":false,"ts":"2026-07-11T00:00:00Z","started_at":"2026-07-11T00:00:00Z"}`), 0644))

	job, err := readUpdateStatus(statusPath, "update-a")

	require.NoError(t, err)
	require.Equal(t, "integration/upstream-20260713", job.IntegrationBranch)
	require.False(t, job.NeedRestart)
}

func TestReadUpdateStatusIncludesPublishedMetadata(t *testing.T) {
	t.Parallel()

	statusPath := filepath.Join(t.TempDir(), "sync-status")
	require.NoError(t, os.WriteFile(statusPath, []byte(`{"job_id":"update-published","status":"success","message":"PUBLISH OK: commit=abc123","integration_branch":"integration/release-v0.1.158-20260716","base_commit":"base123","release_tag":"v0.1.158","release_commit":"26abd19a2812edba02bbef93c3e2a620141cc257","release_published_at":"2026-07-16T12:37:06Z","need_restart":false,"published":true,"published_commit":"abc123","ts":"2026-07-13T00:00:00Z","started_at":"2026-07-13T00:00:00Z"}`), 0644))

	job, err := readUpdateStatus(statusPath, "update-published")

	require.NoError(t, err)
	require.Equal(t, "base123", job.BaseCommit)
	require.Equal(t, "v0.1.158", job.ReleaseTag)
	require.Equal(t, "26abd19a2812edba02bbef93c3e2a620141cc257", job.ReleaseCommit)
	require.Equal(t, "2026-07-16T12:37:06Z", job.ReleasePublishedAt)
	require.True(t, job.Published)
	require.Equal(t, "abc123", job.PublishedCommit)
	require.False(t, job.NeedRestart)
}

func TestReadUpdateStatusIncludesConflictMetadata(t *testing.T) {
	t.Parallel()

	statusPath := filepath.Join(t.TempDir(), "sync-status")
	require.NoError(t, os.WriteFile(statusPath, []byte(`{"job_id":"update-conflict","status":"failed","message":"stable Release merge conflict","conflict_files":["backend/internal/server/routes/gateway.go","deploy/README.md"],"conflict_base":"custom123","conflict_upstream":"upstream456","release_tag":"v0.1.158","release_commit":"26abd19a2812edba02bbef93c3e2a620141cc257","release_published_at":"2026-07-16T12:37:06Z","conflict_release":"v0.1.158@26abd19a2812edba02bbef93c3e2a620141cc257","conflict_log":"/var/lib/docker/volumes/deploy_sub2api_data/_data/sync-conflicts/update-conflict/metadata.json","resolution_hint":"Resolve conflicts and retry.","ts":"2026-07-13T00:00:00Z","started_at":"2026-07-13T00:00:00Z","finished_at":"2026-07-13T00:01:00Z"}`), 0644))

	job, err := readUpdateStatus(statusPath, "update-conflict")

	require.NoError(t, err)
	require.Equal(t, []string{"backend/internal/server/routes/gateway.go", "deploy/README.md"}, job.ConflictFiles)
	require.Equal(t, "custom123", job.ConflictBase)
	require.Equal(t, "upstream456", job.ConflictUpstream)
	require.Equal(t, "v0.1.158", job.ReleaseTag)
	require.Equal(t, "26abd19a2812edba02bbef93c3e2a620141cc257", job.ReleaseCommit)
	require.Equal(t, "2026-07-16T12:37:06Z", job.ReleasePublishedAt)
	require.Equal(t, "v0.1.158@26abd19a2812edba02bbef93c3e2a620141cc257", job.ConflictRelease)
	require.Equal(t, "/var/lib/docker/volumes/deploy_sub2api_data/_data/sync-conflicts/update-conflict/metadata.json", job.ConflictLog)
	require.Equal(t, "Resolve conflicts and retry.", job.ResolutionHint)
}

func TestReadUpdateStatusRejectsDifferentJobID(t *testing.T) {
	t.Parallel()

	statusPath := filepath.Join(t.TempDir(), "sync-status")
	require.NoError(t, os.WriteFile(statusPath, []byte(`{"job_id":"update-a","status":"waiting_actions","message":"syncing","ts":"2026-07-11T00:00:00Z","started_at":"2026-07-11T00:00:00Z"}`), 0644))

	_, err := readUpdateStatus(statusPath, "update-b")

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUpdateJobNotFound))
}

func TestWriteUpdateStatusAcceptsEveryReleaseState(t *testing.T) {
	t.Parallel()

	validStates := []string{
		UpdateStatusCheckingRelease,
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
		UpdateStatusConflict,
	}
	for _, state := range validStates {
		state := state
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "release-job.json")
			require.NoError(t, writeUpdateStatus(path, &UpdateJob{
				JobID:  "update-valid-state",
				Status: state,
			}))
		})
	}
}

func TestUpdateServiceGetUpdateStatusUsesCurrentJobWhenIDIsEmpty(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	svc := NewUpdateService(nil, nil, "0.1.132", "source")
	svc.jobsDir = filepath.Join(tmpDir, "release-jobs")
	svc.jobIDPath = filepath.Join(tmpDir, "release-current-job-id")
	require.NoError(t, os.MkdirAll(svc.jobsDir, 0755))
	require.NoError(t, os.WriteFile(svc.jobIDPath, []byte("update-current\n"), 0644))
	require.NoError(t, writeUpdateStatus(filepath.Join(svc.jobsDir, "update-current.json"), &UpdateJob{
		JobID:  "update-current",
		Status: UpdateStatusWaitingActions,
	}))

	job, err := svc.GetUpdateStatus(context.Background(), " ")

	require.NoError(t, err)
	require.Equal(t, "update-current", job.JobID)
	require.Equal(t, UpdateStatusWaitingActions, job.Status)
}

func TestUpdateServiceGetUpdateStatusReadsSpecificDurableJob(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	svc := NewUpdateService(nil, nil, "0.1.132", "source")
	svc.jobsDir = filepath.Join(tmpDir, "release-jobs")
	svc.jobIDPath = filepath.Join(tmpDir, "release-current-job-id")
	require.NoError(t, os.MkdirAll(svc.jobsDir, 0755))
	require.NoError(t, writeUpdateStatus(filepath.Join(svc.jobsDir, "update-old.json"), &UpdateJob{
		JobID:  "update-old",
		Status: UpdateStatusSuccess,
	}))

	job, err := svc.GetUpdateStatus(context.Background(), "update-old")

	require.NoError(t, err)
	require.Equal(t, "update-old", job.JobID)
}
