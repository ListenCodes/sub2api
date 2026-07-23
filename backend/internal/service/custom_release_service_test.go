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

	"github.com/stretchr/testify/require"
)

type customReleaseGitHubClientStub struct {
	release      *GitHubRelease
	customHead   *GitRef
	compareFiles []ChangedFile
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
	return s.compareFiles, nil
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
	t.Setenv("SUB2API_RELEASE_JOBS_DIR", filepath.Join(temporary, "release-jobs"))
	t.Setenv("SUB2API_RELEASE_JOB_ID_PATH", filepath.Join(temporary, "release-current-job-id"))
	previousStart := customReleaseStartScript
	customReleaseStartScript = func(string, string, string) (func() error, error) {
		return func() error { return nil }, nil
	}
	t.Cleanup(func() { customReleaseStartScript = previousStart })
	return temporary
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
	t.Setenv("SUB2API_RELEASE_LEDGER_ROOT", root)

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
		{name: "docs only", releaseTag: "v0.1.163", releaseCommit: strings.Repeat("a", 40), customCommit: newCustomCommit, files: []ChangedFile{{Filename: "docs/guide.md"}}, wantKind: UpdateKindDocsOnly, wantOfficial: "v0.1.163", wantCustom: "", wantRuntime: false, wantHasUpdate: true},
		{name: "none", releaseTag: "v0.1.163", releaseCommit: strings.Repeat("a", 40), customCommit: currentCustomCommit, wantKind: UpdateKindNone, wantOfficial: "v0.1.163", wantCustom: "v1.0.4", wantRuntime: false, wantHasUpdate: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			current := releaseLedgerTestRecord(root, "release-current", 4, "2026-07-23T08:00:00Z")
			current.CustomCommit = currentCustomCommit
			writeReleaseLedgerFixture(t, root, ReleaseLedgerState{SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 4, UpdatedAt: "2026-07-23T08:00:00Z"}, current)
			t.Setenv("SUB2API_RELEASE_LEDGER_ROOT", root)
			client := &customReleaseGitHubClientStub{
				release:      &GitHubRelease{TagName: test.releaseTag},
				customHead:   &GitRef{SHA: test.customCommit},
				compareFiles: test.files,
				tagCommits:   map[string]string{test.releaseTag: test.releaseCommit},
			}

			info, err := NewUpdateService(&updateServiceCacheStub{}, client, "0.1.163", "release").CheckCustomRelease(context.Background(), true)
			require.NoError(t, err)
			require.True(t, info.DetectionComplete)
			require.Equal(t, current.ReleaseID, info.ReleaseID)
			require.Equal(t, "v0.1.163", info.CurrentOfficialVersion)
			require.Equal(t, "v1.0.4", info.CurrentCustomVersion)
			require.Equal(t, test.wantOfficial, info.TargetOfficialVersion)
			require.Equal(t, test.wantCustom, info.TargetCustomVersion)
			require.Equal(t, test.wantKind, info.UpdateKind)
			require.Equal(t, test.wantRuntime, info.RuntimeUpdate)
			require.Equal(t, test.wantHasUpdate, info.HasUpdate)
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
	svc := NewUpdateService(nil, &customReleaseGitHubClientStub{}, "0.1.163", "release")

	job, err := svc.PrepareUpdate(context.Background())
	require.NoError(t, err)
	require.Equal(t, UpdateActionPrepare, job.Action)
	require.Equal(t, UpdateStatusCheckingUpdates, job.Status)

	preparedAt := time.Now().UTC()
	job.Action = UpdateActionPrepare
	job.Status = UpdateStatusPrepared
	job.PreparedAt = preparedAt.Format(time.RFC3339)
	job.ExpiresAt = preparedAt.Add(15 * time.Minute).Format(time.RFC3339)
	require.NoError(t, writeUpdateStatus(filepath.Join(temporary, "release-jobs", job.JobID+".json"), job))

	applied, err := svc.ApplyUpdate(context.Background(), job.JobID)
	require.NoError(t, err)
	require.Equal(t, job.JobID, applied.JobID)
	require.Equal(t, UpdateActionApply, applied.Action)
	require.Equal(t, UpdateStatusApplyQueued, applied.Status)
}
