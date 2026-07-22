//go:build unit

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateServiceCacheStub struct {
	data string
}

func (s *updateServiceCacheStub) GetUpdateInfo(context.Context) (string, error) {
	if s.data == "" {
		return "", errors.New("cache miss")
	}
	return s.data, nil
}

func (s *updateServiceCacheStub) SetUpdateInfo(_ context.Context, data string, _ time.Duration) error {
	s.data = data
	return nil
}

type updateServiceGitHubClientStub struct {
	release        *GitHubRelease
	recentReleases []*GitHubRelease
	recentErr      error
	customHead     *GitRef
	customErr      error
	compareFiles   []ChangedFile
	compareErr     error
	tagCommits     map[string]string
	tagErr         error
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(context.Context, string) (*GitHubRelease, error) {
	return s.release, nil
}

func (s *updateServiceGitHubClientStub) FetchRecentReleases(context.Context, string, int) ([]*GitHubRelease, error) {
	return s.recentReleases, s.recentErr
}

func (s *updateServiceGitHubClientStub) FetchCustomReleaseHead(context.Context, string, string) (*GitRef, error) {
	return s.customHead, s.customErr
}

func (s *updateServiceGitHubClientStub) CompareCommits(context.Context, string, string, string) ([]ChangedFile, error) {
	return s.compareFiles, s.compareErr
}

func (s *updateServiceGitHubClientStub) FetchRefCommit(_ context.Context, _ string, ref string) (string, error) {
	if s.tagErr != nil {
		return "", s.tagErr
	}
	return s.tagCommits[ref], nil
}

func (s *updateServiceGitHubClientStub) DownloadFile(context.Context, string, string, int64) error {
	panic("DownloadFile should not be called when no update is available")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	panic("FetchChecksumFile should not be called when no update is available")
}

func TestUpdateServicePerformUpdateQueuesEvenWhenBinaryVersionIsCurrent(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "sync-trigger.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0755))
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.132",
				Name:    "v0.1.132",
			},
		},
		"0.1.132",
		"release",
	)
	svc.scriptPath = scriptPath
	svc.jobsDir = filepath.Join(tmpDir, "release-jobs")
	svc.jobIDPath = filepath.Join(tmpDir, "release-current-job-id")
	var triggeredJobID string
	svc.startScript = func(_ string, _ string, jobID string) (func() error, error) {
		triggeredJobID = jobID
		return func() error { return nil }, nil
	}

	job, err := svc.PerformUpdate(context.Background())

	require.NoError(t, err)
	require.NotNil(t, job)
	require.Equal(t, UpdateStatusCheckingUpdates, job.Status)
	require.Equal(t, job.JobID, triggeredJobID)
}

func TestUpdateServicePrepareUpdateCreatesPrepareJob(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "sync-trigger.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0755))
	svc := NewUpdateService(&updateServiceCacheStub{}, &updateServiceGitHubClientStub{}, "0.1.163", "release")
	svc.scriptPath = scriptPath
	svc.jobsDir = filepath.Join(tmpDir, "release-jobs")
	svc.jobIDPath = filepath.Join(tmpDir, "release-current-job-id")
	var action string
	var triggeredJobID string
	svc.startScript = func(_ string, gotAction string, jobID string) (func() error, error) {
		action = gotAction
		triggeredJobID = jobID
		return func() error { return nil }, nil
	}

	job, err := svc.PrepareUpdate(context.Background())

	require.NoError(t, err)
	require.Equal(t, UpdateActionPrepare, job.Action)
	require.Equal(t, UpdateStatusCheckingUpdates, job.Status)
	require.Equal(t, UpdateActionPrepare, action)
	require.Equal(t, job.JobID, triggeredJobID)
}

func TestUpdateServiceCheckUpdateClassifiesUnifiedSources(t *testing.T) {
	productionCommit := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	customCommit := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	stableCommit := "cccccccccccccccccccccccccccccccccccccccc"
	statePath := filepath.Join(t.TempDir(), "release-state.json")
	require.NoError(t, os.WriteFile(statePath, []byte(`{"production_commit":"`+productionCommit+`","stable_release_tag":"v0.1.163","stable_release_commit":"`+stableCommit+`"}`), 0644))
	newService := func(files []ChangedFile) *UpdateService {
		svc := NewUpdateService(&updateServiceCacheStub{}, &updateServiceGitHubClientStub{
			release:      &GitHubRelease{TagName: "v0.1.163"},
			customHead:   &GitRef{SHA: customCommit},
			compareFiles: files,
			tagCommits:   map[string]string{"v0.1.163": stableCommit},
		}, "0.1.163", "release")
		svc.productionStatePath = statePath
		return svc
	}

	info, err := newService(nil).CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, UpdateKindCustom, info.UpdateKind)
	require.Equal(t, customCommit, info.TargetCustomCommit)
	require.Equal(t, "bbbbbbbb", info.TargetCustomShortSHA)

	info, err = newService([]ChangedFile{{Filename: "docs/guide.md", Status: "modified"}}).CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, UpdateKindDocsOnly, info.UpdateKind)
	require.True(t, info.DocsOnly)

	customOnly := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	svc := NewUpdateService(&updateServiceCacheStub{}, &updateServiceGitHubClientStub{
		release:    &GitHubRelease{TagName: "v0.1.164"},
		customHead: &GitRef{SHA: customOnly},
		tagCommits: map[string]string{"v0.1.164": "dddddddddddddddddddddddddddddddddddddddd"},
	}, "0.1.163", "release")
	svc.productionStatePath = statePath
	info, err = svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, UpdateKindOfficial, info.UpdateKind)

	svc.githubClient = &updateServiceGitHubClientStub{
		release:      &GitHubRelease{TagName: "v0.1.164"},
		customHead:   &GitRef{SHA: customCommit},
		compareFiles: []ChangedFile{{Filename: "backend/main.go", Status: "modified"}},
		tagCommits:   map[string]string{"v0.1.164": "dddddddddddddddddddddddddddddddddddddddd"},
	}
	info, err = svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, UpdateKindCombined, info.UpdateKind)
}

func TestUpdateServiceCheckUpdateDoesNotClaimNoneWhenCustomProbeFails(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "release-state.json")
	require.NoError(t, os.WriteFile(statePath, []byte(`{"production_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","stable_release_tag":"v0.1.163","stable_release_commit":"cccccccccccccccccccccccccccccccccccccccc"}`), 0644))
	svc := NewUpdateService(&updateServiceCacheStub{}, &updateServiceGitHubClientStub{
		release:    &GitHubRelease{TagName: "v0.1.163"},
		customErr:  errors.New("GitHub ref unavailable"),
		tagCommits: map[string]string{"v0.1.163": "cccccccccccccccccccccccccccccccccccccccc"},
	}, "0.1.163", "release")
	svc.productionStatePath = statePath

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.False(t, info.HasUpdate)
	require.Contains(t, info.Warning, "custom")
	require.False(t, info.DetectionComplete)
}

func TestUpdateServiceCheckUpdateTreatsEmptyCustomHeadAsIncomplete(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "release-state.json")
	require.NoError(t, os.WriteFile(statePath, []byte(`{"production_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","stable_release_tag":"v0.1.163","stable_release_commit":"cccccccccccccccccccccccccccccccccccccccc"}`), 0644))
	svc := NewUpdateService(&updateServiceCacheStub{}, &updateServiceGitHubClientStub{
		release:    &GitHubRelease{TagName: "v0.1.163"},
		customHead: nil,
		tagCommits: map[string]string{"v0.1.163": "cccccccccccccccccccccccccccccccccccccccc"},
	}, "0.1.163", "release")
	svc.productionStatePath = statePath

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.False(t, info.DetectionComplete)
	require.Contains(t, info.Warning, "custom branch probe returned no commit")
}

func TestUpdateServiceApplyUpdateUsesPreparedJobAndSameID(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "sync-trigger.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0755))
	svc := NewUpdateService(nil, nil, "0.1.163", "release")
	svc.scriptPath = scriptPath
	svc.jobsDir = filepath.Join(tmpDir, "release-jobs")
	svc.jobIDPath = filepath.Join(tmpDir, "release-current-job-id")
	require.NoError(t, os.MkdirAll(svc.jobsDir, 0755))
	preparedAt := time.Now().UTC()
	require.NoError(t, writeUpdateStatus(filepath.Join(svc.jobsDir, "update-prepared.json"), &UpdateJob{
		JobID:      "update-prepared",
		Action:     UpdateActionPrepare,
		Status:     UpdateStatusPrepared,
		PreparedAt: preparedAt.Format(time.RFC3339),
		ExpiresAt:  preparedAt.Add(15 * time.Minute).Format(time.RFC3339),
	}))
	var action string
	svc.startScript = func(_ string, gotAction string, jobID string) (func() error, error) {
		action = gotAction
		require.Equal(t, "update-prepared", jobID)
		return func() error { return nil }, nil
	}

	job, err := svc.ApplyUpdate(context.Background(), "update-prepared")

	require.NoError(t, err)
	require.Equal(t, "update-prepared", job.JobID)
	require.Equal(t, UpdateActionApply, job.Action)
	require.Equal(t, UpdateStatusApplyQueued, job.Status)
	require.Equal(t, UpdateActionApply, action)
}

func TestUpdateServiceApplyUpdateRejectsNonPreparedJob(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewUpdateService(nil, nil, "0.1.163", "release")
	svc.jobsDir = filepath.Join(tmpDir, "release-jobs")
	require.NoError(t, os.MkdirAll(svc.jobsDir, 0755))
	require.NoError(t, writeUpdateStatus(filepath.Join(svc.jobsDir, "update-running.json"), &UpdateJob{
		JobID:  "update-running",
		Action: UpdateActionPrepare,
		Status: UpdateStatusBackingUp,
	}))

	_, err := svc.ApplyUpdate(context.Background(), "update-running")

	require.ErrorIs(t, err, ErrUpdateNotPrepared)
}

func TestUpdateServicePerformUpdateRejectsConcurrentRequests(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "sync-trigger.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0755))
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{},
		"0.1.158",
		"release",
	)
	svc.scriptPath = scriptPath
	svc.jobsDir = filepath.Join(tmpDir, "release-jobs")
	svc.jobIDPath = filepath.Join(tmpDir, "release-current-job-id")

	var starts atomic.Int32
	svc.startScript = func(string, string, string) (func() error, error) {
		starts.Add(1)
		return func() error { return nil }, nil
	}

	const callers = 32
	start := make(chan struct{})
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.PerformUpdate(context.Background())
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	succeeded := 0
	inProgress := 0
	for err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrUpdateInProgress):
			inProgress++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, callers-1, inProgress)
	require.Equal(t, int32(1), starts.Load())
}

func newRollbackTestService(current string, releases []*GitHubRelease) *UpdateService {
	return NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentReleases: releases},
		current,
		"release",
	)
}

func TestUpdateServiceListRollbackVersionsFiltersAndCaps(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148", PublishedAt: "2026-07-09T00:00:00Z"},                       // newer than current: excluded
		{TagName: "v0.1.147", PublishedAt: "2026-07-08T00:00:00Z"},                       // current: excluded
		{TagName: "v0.1.146-rc1", PublishedAt: "2026-07-07T12:00:00Z", Prerelease: true}, // prerelease: excluded
		{TagName: "v0.1.146", PublishedAt: "2026-07-07T00:00:00Z"},
		{TagName: "v0.1.145", PublishedAt: "2026-07-06T00:00:00Z", Draft: true}, // draft: excluded
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"},
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"}, // duplicate: excluded
		{TagName: "v0.1.143", PublishedAt: "2026-07-04T00:00:00Z"},
		{TagName: "v0.1.142", PublishedAt: "2026-07-03T00:00:00Z"}, // beyond cap of 3: excluded
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.144", versions[1].Version)
	require.Equal(t, "0.1.143", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsSortsUnorderedInput(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.144"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.145", versions[1].Version)
	require.Equal(t, "0.1.144", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsEmptyWhenNoneOlder(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.148"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestUpdateServiceListRollbackVersionsPropagatesFetchError(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentErr: errors.New("github unavailable")},
		"0.1.147",
		"release",
	)

	_, err := svc.ListRollbackVersions(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "github unavailable")
}

func TestUpdateServiceRollbackToVersionRejectsDisallowedTargets(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148"},
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
		{TagName: "v0.1.144"},
		{TagName: "v0.1.143"},
		{TagName: "v0.1.142"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	for _, target := range []string{
		"",         // empty
		"0.1.147",  // current version
		"v0.1.147", // current version with prefix
		"0.1.148",  // newer than current
		"0.1.142",  // older than the 3 most recent
		"9.9.9",    // nonexistent
	} {
		err := svc.RollbackToVersion(context.Background(), target)
		require.ErrorIs(t, err, ErrRollbackVersionNotAllowed, "target %q should be rejected", target)
	}
}

func TestUpdateServiceRollbackToVersionAcceptsVPrefix(t *testing.T) {
	// No platform asset in the release: the target passes the allowlist check
	// and fails later at asset lookup, proving the version itself was accepted.
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	err := svc.RollbackToVersion(context.Background(), "v0.1.146")

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrRollbackVersionNotAllowed)
	require.Contains(t, err.Error(), "no compatible release found")
}
