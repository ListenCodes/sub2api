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
	svc.statusPath = filepath.Join(tmpDir, "sync-status")
	svc.jobIDPath = filepath.Join(tmpDir, "sync-job-id")

	started := time.Now()
	job, err := svc.PerformUpdate(context.Background())

	require.NoError(t, err)
	require.NotEmpty(t, job.JobID)
	require.Equal(t, UpdateStatusRunning, job.Status)
	require.Less(t, time.Since(started), 500*time.Millisecond)
}

func TestReadUpdateStatusRejectsDifferentJobID(t *testing.T) {
	t.Parallel()

	statusPath := filepath.Join(t.TempDir(), "sync-status")
	require.NoError(t, os.WriteFile(statusPath, []byte(`{"job_id":"update-a","status":"running","message":"syncing","ts":"2026-07-11T00:00:00Z","started_at":"2026-07-11T00:00:00Z"}`), 0644))

	_, err := readUpdateStatus(statusPath, "update-b")

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUpdateJobNotFound))
}

func TestUpdateServiceGetUpdateStatusRequiresJobID(t *testing.T) {
	svc := NewUpdateService(nil, nil, "0.1.132", "source")

	_, err := svc.GetUpdateStatus(context.Background(), " ")

	require.ErrorIs(t, err, ErrUpdateJobIDRequired)
}
