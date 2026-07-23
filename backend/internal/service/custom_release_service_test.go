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
	statePath := filepath.Join(t.TempDir(), "release-state.json")
	require.NoError(t, os.WriteFile(statePath, []byte(`{"production_commit":"`+productionCommit+`","stable_release_tag":"v0.1.163","stable_release_commit":"`+stableCommit+`"}`), 0644))
	t.Setenv("SUB2API_PRODUCTION_RELEASE_STATE_PATH", statePath)

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

	client.compareFiles = []ChangedFile{{Filename: "docs/guide.md", Status: "modified"}}
	info, err = svc.CheckCustomRelease(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, UpdateKindDocsOnly, info.UpdateKind)
	require.True(t, info.DocsOnly)
	require.False(t, info.RuntimeUpdate)
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
