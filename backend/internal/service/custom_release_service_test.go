//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type customReleaseGitHubClientStub struct {
	release      *GitHubRelease
	customHead   *GitRef
	compareFiles []ChangedFile
	compareErr   error
	tagCommits   map[string]string
}

func (s *customReleaseGitHubClientStub) FetchLatestRelease(context.Context, string) (*GitHubRelease, error) {
	return s.release, nil
}

func (s *customReleaseGitHubClientStub) FetchRecentReleases(context.Context, string, int) ([]*GitHubRelease, error) {
	return nil, nil
}

func (s *customReleaseGitHubClientStub) FetchCustomReleaseHead(context.Context, string, string) (*GitRef, error) {
	return s.customHead, nil
}

func (s *customReleaseGitHubClientStub) CompareCommits(context.Context, string, string, string) ([]ChangedFile, error) {
	return s.compareFiles, s.compareErr
}

func (s *customReleaseGitHubClientStub) FetchRefCommit(_ context.Context, _ string, ref string) (string, error) {
	return s.tagCommits[ref], nil
}

func (s *customReleaseGitHubClientStub) DownloadFile(context.Context, string, string, int64) error {
	return errors.New("unexpected download")
}

func (s *customReleaseGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	return nil, errors.New("unexpected checksum download")
}

func configureCustomReleaseTestRuntime(t *testing.T) string {
	t.Helper()
	temporary := t.TempDir()
	scriptPath := filepath.Join(temporary, "sync-trigger.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0755))
	t.Setenv("SUB2API_RELEASE_SCRIPT_PATH", scriptPath)
	t.Setenv("SUB2API_RELEASE_OPERATIONS_DIR", filepath.Join(temporary, "release-ledger", "operations"))
	t.Setenv("SUB2API_RELEASE_JOB_ID_PATH", filepath.Join(temporary, "release-current-job-id"))
	previousStart := customReleaseStartScript
	customReleaseStartScript = func(string, string, string) (func() error, error) {
		return func() error { return nil }, nil
	}
	t.Cleanup(func() { customReleaseStartScript = previousStart })
	return temporary
}

func configureCustomReleaseLedgerTestRoot(t *testing.T, root string) {
	t.Helper()
	t.Setenv("SUB2API_RELEASE_LEDGER_ROOT", root)
	t.Setenv("SUB2API_RELEASE_BACKUP_ROOT", filepath.Join(root, "artifacts"))
}

func TestCustomReleaseJobsDefaultToLedgerOperations(t *testing.T) {
	ledgerRoot := t.TempDir()
	configureCustomReleaseLedgerTestRoot(t, ledgerRoot)
	t.Setenv("SUB2API_RELEASE_OPERATIONS_DIR", "")
	t.Setenv("SUB2API_RELEASE_JOBS_DIR", filepath.Join(t.TempDir(), "legacy-release-jobs"))

	require.Equal(t, filepath.Join(ledgerRoot, "operations"), customReleaseJobsDir())
}

func TestCustomReleaseLedgerUsesConfiguredBackupRoot(t *testing.T) {
	ledgerRoot := t.TempDir()
	backupRoot := t.TempDir()
	record := releaseLedgerTestRecord(ledgerRoot, "release-current", 0, "2026-07-23T08:00:00Z")
	record.BackupDir = filepath.Join(backupRoot, record.ReleaseID)
	writeReleaseLedgerFixture(t, ledgerRoot, ReleaseLedgerState{
		SchemaVersion:          1,
		CurrentReleaseID:       record.ReleaseID,
		CustomVersionHighWater: 0,
		UpdatedAt:              "2026-07-23T08:00:00Z",
	}, record)
	configureCustomReleaseLedgerTestRoot(t, ledgerRoot)
	t.Setenv("SUB2API_RELEASE_BACKUP_ROOT", backupRoot)

	current, err := NewUpdateService(nil, nil, "0.1.163", "release").CurrentRelease(context.Background())
	require.NoError(t, err)
	require.Equal(t, record.ReleaseID, current.ReleaseID)
}

func TestCustomReleaseLedgerMapsHostBackupPathIntoContainerRoot(t *testing.T) {
	ledgerRoot := t.TempDir()
	containerBackupRoot := t.TempDir()
	hostBackupRoot := filepath.Join(string(filepath.Separator), "var", "lib", "docker", "volumes", "deploy_sub2api_data", "_data", "release-backups")
	record := releaseLedgerTestRecord(ledgerRoot, "release-current", 1, "2026-07-24T10:48:23Z")
	containerBackupDir := filepath.Join(containerBackupRoot, record.ReleaseID)
	require.NoError(t, os.Rename(record.BackupDir, containerBackupDir))
	record.BackupDir = filepath.Join(hostBackupRoot, record.ReleaseID)
	writeReleaseLedgerFixture(t, ledgerRoot, ReleaseLedgerState{
		SchemaVersion:          1,
		CurrentReleaseID:       record.ReleaseID,
		CustomVersionHighWater: 1,
		UpdatedAt:              "2026-07-24T10:48:23Z",
	}, record)
	t.Setenv("SUB2API_RELEASE_LEDGER_ROOT", ledgerRoot)
	t.Setenv("SUB2API_RELEASE_BACKUP_ROOT", containerBackupRoot)
	t.Setenv("SUB2API_RELEASE_RECORD_BACKUP_ROOT", hostBackupRoot)

	current, err := NewUpdateService(nil, nil, "0.1.164", "release").CurrentRelease(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v1.0.1", current.CustomVersion)
	require.True(t, newCustomReleaseLedgerStore().rollbackArtifactsAvailable(current))
}

func TestCustomReleaseCheckClassifiesRuntimeAndDocumentationChanges(t *testing.T) {
	productionCommit := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	customCommit := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	stableCommit := "cccccccccccccccccccccccccccccccccccccccc"
	root := t.TempDir()
	current := releaseLedgerTestRecord(root, "release-current", 4, "2026-07-23T08:00:00Z")
	current.CustomCommit = productionCommit
	current.OfficialCommit = stableCommit
	writeReleaseLedgerFixture(t, root, ReleaseLedgerState{SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 4, UpdatedAt: "2026-07-23T08:00:00Z"}, current)
	configureCustomReleaseLedgerTestRoot(t, root)

	client := &customReleaseGitHubClientStub{
		release:      &GitHubRelease{TagName: "v0.1.163"},
		customHead:   &GitRef{SHA: customCommit},
		compareFiles: []ChangedFile{{Filename: "backend/main.go", Status: "modified"}},
		tagCommits:   map[string]string{"v0.1.163": stableCommit},
	}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.1.163", "release")

	info, err := svc.CheckCustomRelease(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, UpdateKindCustom, info.UpdateKind)
	require.True(t, info.RuntimeUpdate)
	require.Equal(t, customCommit, info.TargetCustomCommit)
	require.Equal(t, "v1.0.5", info.TargetCustomVersion)

	client.compareFiles = []ChangedFile{{Filename: "docs/guide.md", Status: "modified"}}
	info, err = svc.CheckCustomRelease(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, UpdateKindDocsOnly, info.UpdateKind)
	require.True(t, info.DocsOnly)
	require.False(t, info.RuntimeUpdate)
	require.Empty(t, info.TargetCustomVersion)
}

func TestCustomReleaseCompareLimitFallsBackToRuntimeUpdate(t *testing.T) {
	configureCustomReleaseTestRuntime(t)
	productionCommit := strings.Repeat("a", 40)
	customCommit := strings.Repeat("b", 40)
	stableCommit := strings.Repeat("c", 40)
	root := t.TempDir()
	current := releaseLedgerTestRecord(root, "release-current", 4, "2026-07-23T08:00:00Z")
	current.CustomCommit = productionCommit
	current.OfficialCommit = stableCommit
	writeReleaseLedgerFixture(t, root, ReleaseLedgerState{SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 4, UpdatedAt: "2026-07-23T08:00:00Z"}, current)
	configureCustomReleaseLedgerTestRoot(t, root)

	client := &customReleaseGitHubClientStub{
		release:      &GitHubRelease{TagName: current.OfficialVersion},
		customHead:   &GitRef{SHA: customCommit},
		compareFiles: []ChangedFile{{Filename: "docs/guide.md", Status: "modified"}},
		compareErr:   ErrGitHubCompareFilesTruncated,
		tagCommits:   map[string]string{current.OfficialVersion: stableCommit},
	}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.1.163", "release")

	info, err := svc.CheckCustomRelease(context.Background(), true)
	require.NoError(t, err)
	require.True(t, info.DetectionComplete)
	require.Equal(t, UpdateKindCustom, info.UpdateKind)
	require.True(t, info.RuntimeUpdate)
	require.False(t, info.DocsOnly)
	require.Empty(t, info.CustomScopeError)
	require.Contains(t, info.Warning, "treating as runtime update")
	require.Equal(t, "v1.0.5", info.TargetCustomVersion)

	client.compareErr = errors.New("compare unavailable")
	info, err = svc.CheckCustomRelease(context.Background(), true)
	require.NoError(t, err)
	require.False(t, info.DetectionComplete)
	require.Equal(t, "compare unavailable", info.CustomScopeError)
	_, err = svc.PrepareUpdate(context.Background())
	require.ErrorIs(t, err, ErrUpdateDetectionIncomplete)

	client.compareErr = ErrGitHubCompareFilesTruncated
	job, err := svc.PrepareUpdate(context.Background())
	require.NoError(t, err)
	require.Equal(t, UpdateKindCustom, job.UpdateKind)
	require.False(t, job.CustomDocsOnly)
}

func TestUpdateServiceCheckUpdateDualVersionMatrix(t *testing.T) {
	currentCustomCommit := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	newCustomCommit := "dddddddddddddddddddddddddddddddddddddddd"
	newOfficialCommit := "cccccccccccccccccccccccccccccccccccccccc"
	tests := []struct {
		name          string
		releaseTag    string
		releaseCommit string
		customCommit  string
		files         []ChangedFile
		wantKind      string
		wantOfficial  string
		wantCustom    string
		wantRuntime   bool
		wantHasUpdate bool
	}{
		{name: "official", releaseTag: "v0.1.164", releaseCommit: newOfficialCommit, customCommit: currentCustomCommit, wantKind: UpdateKindOfficial, wantOfficial: "v0.1.164", wantCustom: "v1.0.4", wantRuntime: true, wantHasUpdate: true},
		{name: "custom", releaseTag: "v0.1.163", releaseCommit: strings.Repeat("a", 40), customCommit: newCustomCommit, files: []ChangedFile{{Filename: "backend/main.go"}}, wantKind: UpdateKindCustom, wantOfficial: "v0.1.163", wantCustom: "v1.0.5", wantRuntime: true, wantHasUpdate: true},
		{name: "combined", releaseTag: "v0.1.164", releaseCommit: newOfficialCommit, customCommit: newCustomCommit, files: []ChangedFile{{Filename: "frontend/src/main.ts"}}, wantKind: UpdateKindCombined, wantOfficial: "v0.1.164", wantCustom: "v1.0.5", wantRuntime: true, wantHasUpdate: true},
		{name: "official with docs-only custom delta", releaseTag: "v0.1.164", releaseCommit: newOfficialCommit, customCommit: newCustomCommit, files: []ChangedFile{{Filename: "docs/guide.md"}}, wantKind: UpdateKindOfficial, wantOfficial: "v0.1.164", wantCustom: "v1.0.4", wantRuntime: true, wantHasUpdate: true},
		{name: "docs only", releaseTag: "v0.1.163", releaseCommit: strings.Repeat("a", 40), customCommit: newCustomCommit, files: []ChangedFile{{Filename: "docs/guide.md"}}, wantKind: UpdateKindDocsOnly, wantOfficial: "v0.1.163", wantCustom: "", wantRuntime: false, wantHasUpdate: true},
		{name: "none", releaseTag: "v0.1.163", releaseCommit: strings.Repeat("a", 40), customCommit: currentCustomCommit, wantKind: UpdateKindNone, wantOfficial: "v0.1.163", wantCustom: "v1.0.4", wantRuntime: false, wantHasUpdate: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			current := releaseLedgerTestRecord(root, "release-current", 4, "2026-07-23T08:00:00Z")
			current.CustomCommit = currentCustomCommit
			writeReleaseLedgerFixture(t, root, ReleaseLedgerState{SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 4, UpdatedAt: "2026-07-23T08:00:00Z"}, current)
			configureCustomReleaseLedgerTestRoot(t, root)
			client := &customReleaseGitHubClientStub{
				release:      &GitHubRelease{TagName: test.releaseTag},
				customHead:   &GitRef{SHA: test.customCommit},
				compareFiles: test.files,
				tagCommits:   map[string]string{test.releaseTag: test.releaseCommit},
			}

			info, err := NewUpdateService(&updateServiceCacheStub{}, client, "0.1.163", "release").CheckCustomRelease(context.Background(), true)
			require.NoError(t, err)
			require.Truef(t, info.DetectionComplete, "detection warning: %s", info.Warning)
			require.Equal(t, current.ReleaseID, info.ReleaseID)
			require.Equal(t, "v0.1.163", info.CurrentOfficialVersion)
			require.Equal(t, "v1.0.4", info.CurrentCustomVersion)
			require.Equal(t, test.wantOfficial, info.TargetOfficialVersion)
			require.Equal(t, test.releaseCommit, info.TargetOfficialCommit)
			require.Equal(t, test.wantCustom, info.TargetCustomVersion)
			require.Equal(t, test.wantKind, info.UpdateKind)
			require.Equal(t, test.wantRuntime, info.RuntimeUpdate)
			require.Equal(t, test.wantHasUpdate, info.HasUpdate)
			if test.wantHasUpdate {
				require.Regexp(t, `^[0-9a-f]{64}$`, info.UpdateFingerprint)
			} else {
				require.Empty(t, info.UpdateFingerprint)
			}
		})
	}
}

func TestUpdateServiceCheckUpdateFailsClosedWithoutLedger(t *testing.T) {
	t.Setenv("SUB2API_RELEASE_LEDGER_ROOT", filepath.Join(t.TempDir(), "missing"))
	client := &customReleaseGitHubClientStub{
		release:    &GitHubRelease{TagName: "v0.1.164"},
		customHead: &GitRef{SHA: strings.Repeat("b", 40)},
		tagCommits: map[string]string{"v0.1.164": strings.Repeat("c", 40)},
	}

	info, err := NewUpdateService(&updateServiceCacheStub{}, client, "0.1.163", "release").CheckCustomRelease(context.Background(), true)
	require.NoError(t, err)
	require.False(t, info.DetectionComplete)
	require.Empty(t, info.CurrentCustomVersion)
	require.Empty(t, info.TargetCustomVersion)
}

func TestCustomReleasePrepareAndApplyUseTheSameDurableJob(t *testing.T) {
	temporary := configureCustomReleaseTestRuntime(t)
	ledgerRoot := t.TempDir()
	current := releaseLedgerTestRecord(ledgerRoot, "release-current", 4, "2026-07-23T08:00:00Z")
	writeReleaseLedgerFixture(t, ledgerRoot, ReleaseLedgerState{SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 4, UpdatedAt: "2026-07-23T08:00:00Z"}, current)
	configureCustomReleaseLedgerTestRoot(t, ledgerRoot)
	client := &customReleaseGitHubClientStub{
		release:    &GitHubRelease{TagName: "v0.1.164"},
		customHead: &GitRef{SHA: current.CustomCommit},
		tagCommits: map[string]string{"v0.1.164": strings.Repeat("c", 40)},
	}
	svc := NewUpdateService(nil, client, "0.1.163", "release")

	job, err := svc.PrepareUpdate(context.Background())
	require.NoError(t, err)
	require.Equal(t, UpdateActionPrepare, job.Action)
	require.Equal(t, UpdateStatusCheckingUpdates, job.Status)

	preparedAt := time.Now().UTC()
	job.Action = UpdateActionPrepare
	job.Status = UpdateStatusPrepared
	job.PreparedAt = preparedAt.Format(time.RFC3339)
	job.ExpiresAt = preparedAt.Add(15 * time.Minute).Format(time.RFC3339)
	jobPath := filepath.Join(temporary, "release-ledger", "operations", job.JobID+".json")
	require.NoError(t, writeUpdateStatus(jobPath, job))
	jobJSON, err := os.ReadFile(jobPath)
	require.NoError(t, err)
	var preparedJob map[string]any
	require.NoError(t, json.Unmarshal(jobJSON, &preparedJob))
	preparedJob["base_custom_high_water"] = 4
	jobJSON, err = json.Marshal(preparedJob)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(jobPath, jobJSON, 0o600))

	applied, err := svc.ApplyUpdate(context.Background(), job.JobID)
	require.NoError(t, err)
	require.Equal(t, job.JobID, applied.JobID)
	require.Equal(t, UpdateActionApply, applied.Action)
	require.Equal(t, UpdateStatusApplyQueued, applied.Status)
	jobJSON, err = os.ReadFile(jobPath)
	require.NoError(t, err)
	var queuedJob map[string]any
	require.NoError(t, json.Unmarshal(jobJSON, &queuedJob))
	require.Equal(t, float64(4), queuedJob["base_custom_high_water"])
}

func TestIdentityRolloutPrepareBypassesGitHubAndPreservesReleaseIdentity(t *testing.T) {
	runtimeRoot := configureCustomReleaseTestRuntime(t)
	ledgerRoot := t.TempDir()
	current := releaseLedgerTestRecord(ledgerRoot, "release-current", 4, "2026-07-23T08:00:00Z")
	writeReleaseLedgerFixture(t, ledgerRoot, ReleaseLedgerState{
		SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 9, UpdatedAt: "2026-07-23T08:00:00Z",
	}, current)
	configureCustomReleaseLedgerTestRoot(t, ledgerRoot)
	svc := NewUpdateService(nil, nil, "0.1.163", "release")

	job, err := svc.PrepareIdentityRollout(context.Background(), IdentityTransitionStage1V2)
	require.NoError(t, err)
	require.Equal(t, UpdateKindIdentityConfig, job.UpdateKind)
	require.Equal(t, IdentityTransitionStage1V2, job.IdentityTransition)
	require.Equal(t, current.OfficialVersion, job.TargetOfficialVersion)
	require.Equal(t, current.CustomVersion, job.TargetCustomVersion)
	require.Equal(t, current.CustomCommit, job.TargetCustomCommit)
	require.Equal(t, current.MainDigest, job.MainDigest)
	require.Equal(t, current.ExtensionsDigest, job.ExtensionsDigest)
	require.NotNil(t, job.ProposedCustomSequence)
	require.Equal(t, current.CustomVersionSequence, *job.ProposedCustomSequence)
	require.False(t, job.AdvancesCustomVersion)

	path := filepath.Join(runtimeRoot, "release-ledger", "operations", job.JobID+".json")
	job.Status = ReleaseStatusPrepared
	job.ExpiresAt = time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	require.NoError(t, writeUpdateStatus(path, job))
	_, err = svc.ApplyUpdate(context.Background(), job.JobID)
	require.ErrorIs(t, err, ErrUpdateNotPrepared)
	applied, err := svc.ApplyIdentityRollout(context.Background(), job.JobID)
	require.NoError(t, err)
	require.Equal(t, ReleasePhaseApply, applied.Action)
}

func TestIdentityRolloutRejectsUnknownTransition(t *testing.T) {
	require.True(t, ValidIdentityTransition(IdentityTransitionStage0SafeReset))
	require.True(t, ValidIdentityTransition(IdentityTransitionStage4Geo))
	_, err := NewUpdateService(nil, nil, "0.1.163", "release").PrepareIdentityRollout(context.Background(), "stage4-enforce")
	require.ErrorIs(t, err, ErrIdentityTransitionInvalid)
}

func TestCustomReleaseRollbackPrepareAndApplyUseCompleteSnapshot(t *testing.T) {
	runtimeRoot := configureCustomReleaseTestRuntime(t)
	ledgerRoot := t.TempDir()
	current := releaseLedgerTestRecord(ledgerRoot, "release-current", 4, "2026-07-23T08:00:00Z")
	target := releaseLedgerTestRecord(ledgerRoot, "release-v101", 1, "2026-07-20T08:00:00Z")
	writeReleaseLedgerFixture(t, ledgerRoot, ReleaseLedgerState{SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 4, UpdatedAt: "2026-07-23T08:00:00Z"}, current, target)
	configureCustomReleaseLedgerTestRoot(t, ledgerRoot)
	svc := NewUpdateService(nil, nil, "0.1.163", "release")

	currentResult, err := svc.CurrentRelease(context.Background())
	require.NoError(t, err)
	require.Equal(t, current.ReleaseID, currentResult.ReleaseID)
	rollbackReleases, err := svc.ListRollbackReleases(context.Background())
	require.NoError(t, err)
	require.Equal(t, []ReleaseRecord{target}, rollbackReleases)

	job, err := svc.PrepareRollback(context.Background(), target.ReleaseID)
	require.NoError(t, err)
	require.Equal(t, ReleaseOperationRollback, job.OperationKind)
	require.Equal(t, ReleasePhasePrepare, job.Action)
	require.Equal(t, ReleaseStatusResolvingSnapshot, job.Status)
	require.Equal(t, current.ReleaseID, job.BaseReleaseID)
	require.Equal(t, target.ReleaseID, job.TargetReleaseID)

	preparedAt := time.Now().UTC()
	job.Status = ReleaseStatusPrepared
	job.PreparedAt = preparedAt.Format(time.RFC3339)
	job.ExpiresAt = preparedAt.Add(15 * time.Minute).Format(time.RFC3339)
	require.NoError(t, writeUpdateStatus(filepath.Join(runtimeRoot, "release-ledger", "operations", job.JobID+".json"), job))

	applied, err := svc.ApplyRollback(context.Background(), job.JobID)
	require.NoError(t, err)
	require.Equal(t, job.JobID, applied.JobID)
	require.Equal(t, ReleasePhaseApply, applied.Action)
	require.Equal(t, ReleaseStatusApplyQueued, applied.Status)
}

func TestCustomReleaseRollbackCandidatesNeedNoRuntimeTools(t *testing.T) {
	ledgerRoot := t.TempDir()
	current := releaseLedgerTestRecord(ledgerRoot, "release-current", 5, "2026-07-23T08:30:00Z")
	release4 := releaseLedgerTestRecord(ledgerRoot, "release-4", 4, "2026-07-23T08:20:00Z")
	release3 := releaseLedgerTestRecord(ledgerRoot, "release-3", 3, "2026-07-23T08:10:00Z")
	release2 := releaseLedgerTestRecord(ledgerRoot, "release-2", 2, "2026-07-23T08:00:00Z")
	release1 := releaseLedgerTestRecord(ledgerRoot, "release-1", 1, "2026-07-23T07:50:00Z")
	writeReleaseLedgerFixture(t, ledgerRoot, ReleaseLedgerState{
		SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 5,
		UpdatedAt: "2026-07-23T08:31:00Z",
	}, current, release4, release3, release2, release1)
	t.Setenv("SUB2API_RELEASE_LEDGER_ROOT", ledgerRoot)
	t.Setenv("SUB2API_RELEASE_BACKUP_ROOT", filepath.Join(ledgerRoot, "artifacts"))
	t.Setenv("SUB2API_REPO", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("PATH", t.TempDir())

	releases, err := NewUpdateService(nil, nil, "0.1.164", "release").ListRollbackReleases(context.Background())
	require.NoError(t, err)
	releaseIDs := make([]string, 0, len(releases))
	for _, release := range releases {
		releaseIDs = append(releaseIDs, release.ReleaseID)
	}
	require.Equal(t, []string{"release-4", "release-3", "release-2"}, releaseIDs)
}

func TestCustomReleasePrepareIsIdempotentForSameOperationAndRejectsDifferentTarget(t *testing.T) {
	configureCustomReleaseTestRuntime(t)
	ledgerRoot := t.TempDir()
	current := releaseLedgerTestRecord(ledgerRoot, "release-current", 4, "2026-07-23T08:00:00Z")
	target := releaseLedgerTestRecord(ledgerRoot, "release-v101", 1, "2026-07-20T08:00:00Z")
	other := releaseLedgerTestRecord(ledgerRoot, "release-v102", 2, "2026-07-21T08:00:00Z")
	writeReleaseLedgerFixture(t, ledgerRoot, ReleaseLedgerState{SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 4, UpdatedAt: "2026-07-23T08:00:00Z"}, current, target, other)
	configureCustomReleaseLedgerTestRoot(t, ledgerRoot)
	svc := NewUpdateService(nil, nil, "0.1.163", "release")

	first, err := svc.PrepareRollback(context.Background(), target.ReleaseID)
	require.NoError(t, err)
	duplicate, err := svc.PrepareRollback(context.Background(), target.ReleaseID)
	require.NoError(t, err)
	require.Equal(t, first.JobID, duplicate.JobID)
	_, err = svc.PrepareRollback(context.Background(), other.ReleaseID)
	require.ErrorIs(t, err, ErrUpdateInProgress)
}

func TestCustomReleaseRollbackApplyRejectsExpiredPreparation(t *testing.T) {
	runtimeRoot := configureCustomReleaseTestRuntime(t)
	var startedAction string
	previousStart := customReleaseStartScript
	customReleaseStartScript = func(_ string, action string, _ string) (func() error, error) {
		startedAction = action
		return func() error { return nil }, nil
	}
	t.Cleanup(func() { customReleaseStartScript = previousStart })
	ledgerRoot := t.TempDir()
	current := releaseLedgerTestRecord(ledgerRoot, "release-current", 4, "2026-07-23T08:00:00Z")
	target := releaseLedgerTestRecord(ledgerRoot, "release-v101", 1, "2026-07-20T08:00:00Z")
	writeReleaseLedgerFixture(t, ledgerRoot, ReleaseLedgerState{SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 4, UpdatedAt: "2026-07-23T08:00:00Z"}, current, target)
	configureCustomReleaseLedgerTestRoot(t, ledgerRoot)
	svc := NewUpdateService(nil, nil, "0.1.163", "release")

	job, err := svc.PrepareRollback(context.Background(), target.ReleaseID)
	require.NoError(t, err)
	job.Status = ReleaseStatusPrepared
	job.PreparedAt = time.Now().UTC().Add(-16 * time.Minute).Format(time.RFC3339)
	job.ExpiresAt = time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	path := filepath.Join(runtimeRoot, "release-ledger", "operations", job.JobID+".json")
	require.NoError(t, writeUpdateStatus(path, job))
	writeReleaseLedgerJSON(t, filepath.Join(ledgerRoot, "state.json"), ReleaseLedgerState{
		SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 4,
		ActiveOperationID: job.JobID, UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	_, err = svc.ApplyRollback(context.Background(), job.JobID)
	require.ErrorIs(t, err, ErrUpdateExpired)
	queued, readErr := readUpdateStatus(path, job.JobID)
	require.NoError(t, readErr)
	require.Equal(t, ReleaseStatusPrepared, queued.Status)
	require.Equal(t, "expire", startedAction)
	state, stateErr := newReleaseLedgerStoreWithArtifactRoots(ledgerRoot, ledgerRoot, ledgerRoot).ReadState()
	require.NoError(t, stateErr)
	require.Equal(t, job.JobID, state.ActiveOperationID)
}

func TestCustomReleasePrepareQueuesExpiredActiveOperationSettlement(t *testing.T) {
	runtimeRoot := configureCustomReleaseTestRuntime(t)
	var startedAction string
	previousStart := customReleaseStartScript
	customReleaseStartScript = func(_ string, action string, _ string) (func() error, error) {
		startedAction = action
		return func() error { return nil }, nil
	}
	t.Cleanup(func() { customReleaseStartScript = previousStart })
	ledgerRoot := t.TempDir()
	current := releaseLedgerTestRecord(ledgerRoot, "release-current", 4, "2026-07-23T08:00:00Z")
	target := releaseLedgerTestRecord(ledgerRoot, "release-v101", 1, "2026-07-20T08:00:00Z")
	other := releaseLedgerTestRecord(ledgerRoot, "release-v102", 2, "2026-07-21T08:00:00Z")
	writeReleaseLedgerFixture(t, ledgerRoot, ReleaseLedgerState{SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 4, UpdatedAt: "2026-07-23T08:00:00Z"}, current, target, other)
	configureCustomReleaseLedgerTestRoot(t, ledgerRoot)
	svc := NewUpdateService(nil, nil, "0.1.163", "release")

	job, err := svc.PrepareRollback(context.Background(), target.ReleaseID)
	require.NoError(t, err)
	job.Status = ReleaseStatusPrepared
	job.ExpiresAt = time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	require.NoError(t, writeUpdateStatus(filepath.Join(runtimeRoot, "release-ledger", "operations", job.JobID+".json"), job))
	writeReleaseLedgerJSON(t, filepath.Join(ledgerRoot, "state.json"), ReleaseLedgerState{
		SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 4,
		ActiveOperationID: job.JobID, UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	_, err = svc.PrepareRollback(context.Background(), other.ReleaseID)
	require.ErrorIs(t, err, ErrUpdateInProgress)
	require.Equal(t, "expire", startedAction)
	state, stateErr := newReleaseLedgerStoreWithArtifactRoots(ledgerRoot, ledgerRoot, ledgerRoot).ReadState()
	require.NoError(t, stateErr)
	require.Equal(t, job.JobID, state.ActiveOperationID)
}

func TestCustomReleasePrepareRefusesNoneAndDocsOnlyUpdates(t *testing.T) {
	tests := []struct {
		name       string
		customHead string
		files      []ChangedFile
	}{
		{name: "none", customHead: strings.Repeat("b", 40)},
		{name: "docs only", customHead: strings.Repeat("d", 40), files: []ChangedFile{{Filename: "docs/guide.md"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeRoot := configureCustomReleaseTestRuntime(t)
			ledgerRoot := t.TempDir()
			current := releaseLedgerTestRecord(ledgerRoot, "release-current", 4, "2026-07-23T08:00:00Z")
			writeReleaseLedgerFixture(t, ledgerRoot, ReleaseLedgerState{SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 4, UpdatedAt: "2026-07-23T08:00:00Z"}, current)
			configureCustomReleaseLedgerTestRoot(t, ledgerRoot)
			client := &customReleaseGitHubClientStub{
				release:      &GitHubRelease{TagName: current.OfficialVersion},
				customHead:   &GitRef{SHA: test.customHead},
				compareFiles: test.files,
				tagCommits:   map[string]string{current.OfficialVersion: current.OfficialCommit},
			}

			_, err := NewUpdateService(nil, client, "0.1.163", "release").PrepareUpdate(context.Background())
			require.ErrorIs(t, err, ErrNoUpdateAvailable)
			_, statErr := os.Stat(filepath.Join(runtimeRoot, "release-current-job-id"))
			require.ErrorIs(t, statErr, os.ErrNotExist)
		})
	}
}

func TestCustomReleasePrepareRejectsCorruptCurrentOperationPointer(t *testing.T) {
	runtimeRoot := configureCustomReleaseTestRuntime(t)
	ledgerRoot := t.TempDir()
	current := releaseLedgerTestRecord(ledgerRoot, "release-current", 4, "2026-07-23T08:00:00Z")
	target := releaseLedgerTestRecord(ledgerRoot, "release-v101", 1, "2026-07-20T08:00:00Z")
	writeReleaseLedgerFixture(t, ledgerRoot, ReleaseLedgerState{SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 4, UpdatedAt: "2026-07-23T08:00:00Z"}, current, target)
	configureCustomReleaseLedgerTestRoot(t, ledgerRoot)
	require.NoError(t, os.WriteFile(filepath.Join(runtimeRoot, "release-current-job-id"), []byte("../invalid\n"), 0644))

	_, err := NewUpdateService(nil, nil, "0.1.163", "release").PrepareRollback(context.Background(), target.ReleaseID)
	require.ErrorIs(t, err, ErrReleaseOperationInconsistent)
}
