//go:build unit

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestReleaseOperationAcceptsBothKindsAndEveryState(t *testing.T) {
	valid := []string{
		"resolving_target", "resolving_snapshot", "verifying_snapshot",
		"verifying_images", "downloading_images", "rendering_compose",
		"backing_up", "validating_backup", "prepared", "apply_queued",
		"validating_manifest", "switching_extensions", "switching_main",
		"health_checking", "rolling_back", "success", "failed", "conflict",
		"expired", "drifted", "failed_rolled_back", "rollback_failed",
	}
	for _, kind := range []string{ReleaseOperationUpdate, ReleaseOperationRollback} {
		for _, status := range valid {
			path := filepath.Join(t.TempDir(), kind+"-operation.json")
			require.NoError(t, writeUpdateStatus(path, &UpdateJob{
				JobID:         kind + "-valid-operation",
				OperationKind: kind,
				Action:        ReleasePhasePrepare,
				Status:        status,
			}))
		}
	}

	rollbackID, err := newReleaseOperationID(ReleaseOperationRollback)
	require.NoError(t, err)
	require.Regexp(t, `^rollback-[0-9]+-[0-9a-f]{16}$`, rollbackID)
}

func TestReleaseOperationRejectsLegacyRecordWithoutKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"job_id":"update-legacy","action":"prepare","status":"prepared","message":"legacy"}`), 0644))

	_, err := readUpdateStatus(path, "update-legacy")
	require.Error(t, err)
	require.Equal(t, "LEGACY_SINGLE_PHASE_UNSUPPORTED", infraerrors.Reason(err))
}

func TestReleaseOperationPrepareDoesNotTriggerOverLegacyCurrentJob(t *testing.T) {
	root := t.TempDir()
	operationsDir := filepath.Join(root, "release-ledger", "operations")
	legacyJobsDir := filepath.Join(root, "release-jobs")
	jobIDPath := filepath.Join(root, "release-current-job-id")
	scriptPath := filepath.Join(root, "sync-trigger.sh")
	require.NoError(t, os.MkdirAll(operationsDir, 0755))
	require.NoError(t, os.MkdirAll(legacyJobsDir, 0755))
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0755))
	require.NoError(t, os.WriteFile(jobIDPath, []byte("update-legacy\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(legacyJobsDir, "update-legacy.json"), []byte(`{"job_id":"update-legacy","action":"prepare","status":"prepared","message":"legacy"}`), 0644))
	t.Setenv("SUB2API_RELEASE_SCRIPT_PATH", scriptPath)
	t.Setenv("SUB2API_RELEASE_OPERATIONS_DIR", operationsDir)
	t.Setenv("SUB2API_LEGACY_RELEASE_JOBS_DIR", legacyJobsDir)
	t.Setenv("SUB2API_RELEASE_JOB_ID_PATH", jobIDPath)

	starts := 0
	previousStart := customReleaseStartScript
	customReleaseStartScript = func(string, string, string) (func() error, error) {
		starts++
		return func() error { return nil }, nil
	}
	t.Cleanup(func() { customReleaseStartScript = previousStart })

	_, err := NewUpdateService(nil, nil, "0.1.163", "release").PrepareUpdate(context.Background())
	require.ErrorIs(t, err, ErrLegacySinglePhase)
	require.Equal(t, "LEGACY_SINGLE_PHASE_UNSUPPORTED", infraerrors.Reason(err))
	require.Zero(t, starts)
}

func TestUpdateServicePrepareUpdateReturnsJobBeforeScriptCompletes(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "sync-upstream.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 2\n"), 0755))

	ledgerRoot := t.TempDir()
	current := releaseLedgerTestRecord(ledgerRoot, "release-current", 4, "2026-07-23T08:00:00Z")
	writeReleaseLedgerFixture(t, ledgerRoot, ReleaseLedgerState{SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 4, UpdatedAt: "2026-07-23T08:00:00Z"}, current)
	t.Setenv("SUB2API_RELEASE_LEDGER_ROOT", ledgerRoot)
	t.Setenv("SUB2API_RELEASE_BACKUP_ROOT", filepath.Join(ledgerRoot, "artifacts"))
	svc := NewUpdateService(&updateServiceCacheStub{}, &customReleaseGitHubClientStub{
		release:    &GitHubRelease{TagName: "v0.1.164", Name: "v0.1.164"},
		customHead: &GitRef{SHA: current.CustomCommit},
		tagCommits: map[string]string{"v0.1.164": strings.Repeat("c", 40)},
	}, "0.1.163", "source")
	t.Setenv("SUB2API_RELEASE_SCRIPT_PATH", scriptPath)
	t.Setenv("SUB2API_RELEASE_OPERATIONS_DIR", filepath.Join(tmpDir, "release-ledger", "operations"))
	t.Setenv("SUB2API_RELEASE_JOB_ID_PATH", filepath.Join(tmpDir, "release-current-job-id"))
	previousStart := customReleaseStartScript
	customReleaseStartScript = func(string, string, string) (func() error, error) {
		return func() error {
			time.Sleep(2 * time.Second)
			return nil
		}, nil
	}
	t.Cleanup(func() { customReleaseStartScript = previousStart })

	started := time.Now()
	job, err := svc.PrepareUpdate(context.Background())

	require.NoError(t, err)
	require.NotEmpty(t, job.JobID)
	require.Equal(t, ReleaseOperationUpdate, job.OperationKind)
	require.Equal(t, UpdateStatusCheckingUpdates, job.Status)
	require.False(t, job.NeedRestart)
	require.False(t, job.Published)
	require.Less(t, time.Since(started), 500*time.Millisecond)
}

func TestReadUpdateStatusIncludesPreparationMetadata(t *testing.T) {
	t.Parallel()

	statusPath := filepath.Join(t.TempDir(), "sync-status")
	require.NoError(t, os.WriteFile(statusPath, []byte(`{"job_id":"update-a","operation_kind":"update","action":"prepare","status":"success","message":"branch ready","integration_branch":"integration/upstream-20260713","need_restart":false,"ts":"2026-07-11T00:00:00Z","started_at":"2026-07-11T00:00:00Z"}`), 0644))

	job, err := readUpdateStatus(statusPath, "update-a")

	require.NoError(t, err)
	require.Equal(t, "integration/upstream-20260713", job.IntegrationBranch)
	require.False(t, job.NeedRestart)
}

func TestReadUpdateStatusIncludesPublishedMetadata(t *testing.T) {
	t.Parallel()

	statusPath := filepath.Join(t.TempDir(), "sync-status")
	require.NoError(t, os.WriteFile(statusPath, []byte(`{"job_id":"update-published","operation_kind":"update","action":"apply","status":"success","message":"PUBLISH OK: commit=abc123","integration_branch":"integration/release-v0.1.158-20260716","base_commit":"base123","release_tag":"v0.1.158","release_commit":"26abd19a2812edba02bbef93c3e2a620141cc257","release_published_at":"2026-07-16T12:37:06Z","need_restart":false,"published":true,"published_commit":"abc123","ts":"2026-07-13T00:00:00Z","started_at":"2026-07-13T00:00:00Z"}`), 0644))

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
	require.NoError(t, os.WriteFile(statusPath, []byte(`{"job_id":"update-conflict","operation_kind":"update","action":"prepare","status":"failed","message":"stable Release merge conflict","conflict_files":["backend/internal/server/routes/gateway.go","deploy/README.md"],"conflict_base":"custom123","conflict_upstream":"upstream456","release_tag":"v0.1.158","release_commit":"26abd19a2812edba02bbef93c3e2a620141cc257","release_published_at":"2026-07-16T12:37:06Z","conflict_release":"v0.1.158@26abd19a2812edba02bbef93c3e2a620141cc257","conflict_log":"/var/lib/docker/volumes/deploy_sub2api_data/_data/sync-conflicts/update-conflict/metadata.json","resolution_hint":"Resolve conflicts and retry.","ts":"2026-07-13T00:00:00Z","started_at":"2026-07-13T00:00:00Z","finished_at":"2026-07-13T00:01:00Z"}`), 0644))

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
	require.NoError(t, os.WriteFile(statusPath, []byte(`{"job_id":"update-a","operation_kind":"update","action":"prepare","status":"resolving_target","message":"syncing","ts":"2026-07-11T00:00:00Z","started_at":"2026-07-11T00:00:00Z"}`), 0644))

	_, err := readUpdateStatus(statusPath, "update-b")

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUpdateJobNotFound))
}

func TestWriteUpdateStatusAcceptsEveryReleaseState(t *testing.T) {
	t.Parallel()

	validStates := []string{
		UpdateStatusCheckingUpdates,
		UpdateStatusCheckingRelease,
		UpdateStatusValidatingTag,
		UpdateStatusMergingRelease,
		UpdateStatusWaitingActions,
		UpdateStatusWaitingImages,
		UpdateStatusDownloadingImages,
		UpdateStatusPreparingCompose,
		UpdateStatusPromotingRelease,
		UpdateStatusBackingUp,
		UpdateStatusValidatingBackup,
		UpdateStatusPrepared,
		UpdateStatusApplyQueued,
		UpdateStatusDeployingExtensions,
		UpdateStatusDeployingMain,
		UpdateStatusHealthChecking,
		UpdateStatusRollingBack,
		UpdateStatusSuccess,
		UpdateStatusFailed,
		UpdateStatusConflict,
		UpdateStatusExpired,
		UpdateStatusDrifted,
	}
	for _, state := range validStates {
		state := state
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "release-job.json")
			require.NoError(t, writeUpdateStatus(path, &UpdateJob{
				JobID:         "update-valid-state",
				OperationKind: ReleaseOperationUpdate,
				Action:        ReleasePhasePrepare,
				Status:        state,
			}))
		})
	}
}

func TestPreparedJobIsPollingSettledButNotTerminal(t *testing.T) {
	t.Parallel()

	require.True(t, IsPollingSettledUpdateStatus(UpdateStatusPrepared))
	require.False(t, IsTerminalUpdateStatus(UpdateStatusPrepared))
	require.True(t, IsTerminalUpdateStatus(UpdateStatusExpired))
	require.True(t, IsTerminalUpdateStatus(UpdateStatusDrifted))
}

func TestReadUpdateStatusIncludesPreparedManifestMetadata(t *testing.T) {
	t.Parallel()

	statusPath := filepath.Join(t.TempDir(), "release-job.json")
	require.NoError(t, os.WriteFile(statusPath, []byte(`{
		"job_id":"update-prepared",
		"operation_kind":"update",
		"action":"prepare",
		"status":"prepared",
		"message":"ready for confirmation",
		"prepared_manifest":"/app/data/release-prepared/update-prepared/manifest.json",
		"prepared_manifest_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"prepared_at":"2026-07-23T00:00:00Z",
		"expires_at":"2026-07-23T00:15:00Z",
		"need_restart":false,
		"ts":"2026-07-23T00:00:00Z",
		"updated_at":"2026-07-23T00:00:00Z",
		"started_at":"2026-07-23T00:00:00Z"
	}`), 0644))

	job, err := readUpdateStatus(statusPath, "update-prepared")

	require.NoError(t, err)
	require.Equal(t, UpdateActionPrepare, job.Action)
	require.Equal(t, UpdateStatusPrepared, job.Status)
	require.Equal(t, "/app/data/release-prepared/update-prepared/manifest.json", job.PreparedManifest)
	require.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", job.PreparedManifestSHA256)
	require.Equal(t, "2026-07-23T00:15:00Z", job.ExpiresAt)
}

func TestUpdateServiceGetUpdateStatusUsesCurrentJobWhenIDIsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewUpdateService(nil, nil, "0.1.132", "source")
	jobsDir := filepath.Join(tmpDir, "release-ledger", "operations")
	jobIDPath := filepath.Join(tmpDir, "release-current-job-id")
	t.Setenv("SUB2API_RELEASE_OPERATIONS_DIR", jobsDir)
	t.Setenv("SUB2API_RELEASE_JOB_ID_PATH", jobIDPath)
	require.NoError(t, os.MkdirAll(jobsDir, 0755))
	require.NoError(t, os.WriteFile(jobIDPath, []byte("update-current\n"), 0644))
	require.NoError(t, writeUpdateStatus(filepath.Join(jobsDir, "update-current.json"), &UpdateJob{
		JobID:         "update-current",
		OperationKind: ReleaseOperationUpdate,
		Action:        ReleasePhasePrepare,
		Status:        UpdateStatusWaitingActions,
	}))

	job, err := svc.GetUpdateStatus(context.Background(), " ")

	require.NoError(t, err)
	require.Equal(t, "update-current", job.JobID)
	require.Equal(t, UpdateStatusWaitingActions, job.Status)
}

func TestUpdateServiceGetUpdateStatusReadsSpecificDurableJob(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewUpdateService(nil, nil, "0.1.132", "source")
	jobsDir := filepath.Join(tmpDir, "release-ledger", "operations")
	t.Setenv("SUB2API_RELEASE_OPERATIONS_DIR", jobsDir)
	t.Setenv("SUB2API_RELEASE_JOB_ID_PATH", filepath.Join(tmpDir, "release-current-job-id"))
	require.NoError(t, os.MkdirAll(jobsDir, 0755))
	require.NoError(t, writeUpdateStatus(filepath.Join(jobsDir, "update-old.json"), &UpdateJob{
		JobID:         "update-old",
		OperationKind: ReleaseOperationUpdate,
		Action:        ReleasePhaseApply,
		Status:        UpdateStatusSuccess,
	}))

	job, err := svc.GetUpdateStatus(context.Background(), "update-old")

	require.NoError(t, err)
	require.Equal(t, "update-old", job.JobID)
}
