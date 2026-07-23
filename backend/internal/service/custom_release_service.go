package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultProductionReleaseStatePath = "/app/data/release-state.json"
	githubCustomRepo                  = "ListenCodes/sub2api"

	UpdateKindNone     = "none"
	UpdateKindOfficial = "official"
	UpdateKindCustom   = "custom"
	UpdateKindCombined = "combined"
	UpdateKindDocsOnly = "docs-only"
)

type customReleaseGitHubClient interface {
	FetchLatestRelease(context.Context, string) (*GitHubRelease, error)
	FetchCustomReleaseHead(context.Context, string, string) (*GitRef, error)
	CompareCommits(context.Context, string, string, string) ([]ChangedFile, error)
	FetchRefCommit(context.Context, string, string) (string, error)
}

type CustomReleaseInfo struct {
	CurrentVersion         string       `json:"current_version"`
	LatestVersion          string       `json:"latest_version"`
	HasUpdate              bool         `json:"has_update"`
	ReleaseInfo            *ReleaseInfo `json:"release_info,omitempty"`
	Cached                 bool         `json:"cached"`
	Warning                string       `json:"warning,omitempty"`
	BuildType              string       `json:"build_type"`
	UpdateKind             string       `json:"update_kind"`
	OfficialUpdate         bool         `json:"official_update"`
	CustomUpdate           bool         `json:"custom_update"`
	DocsOnly               bool         `json:"docs_only"`
	RuntimeUpdate          bool         `json:"runtime_update"`
	DetectionComplete      bool         `json:"detection_complete"`
	ProductionCommit       string       `json:"production_commit,omitempty"`
	ProductionStableTag    string       `json:"production_stable_tag,omitempty"`
	ProductionStableCommit string       `json:"production_stable_commit,omitempty"`
	TargetCustomCommit     string       `json:"target_custom_commit,omitempty"`
	TargetCustomShortSHA   string       `json:"target_custom_short_sha,omitempty"`
	CustomScopeError       string       `json:"custom_scope_error,omitempty"`
}

type ProductionReleaseState struct {
	ProductionCommit    string `json:"production_commit"`
	StableReleaseTag    string `json:"stable_release_tag"`
	StableReleaseCommit string `json:"stable_release_commit"`
}

type GitRef struct {
	SHA string `json:"sha"`
}

type ChangedFile struct {
	Filename string `json:"filename"`
	Status   string `json:"status"`
}

var (
	customReleaseMu          sync.Mutex
	customReleaseStartScript = startCustomReleaseScript
)

func customReleaseEnv(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func customReleaseScriptPath() string {
	return customReleaseEnv("SUB2API_RELEASE_SCRIPT_PATH", defaultUpdateScriptPath)
}

func customReleaseJobsDir() string {
	return customReleaseEnv("SUB2API_RELEASE_JOBS_DIR", defaultUpdateJobsDir)
}

func customReleaseJobIDPath() string {
	return customReleaseEnv("SUB2API_RELEASE_JOB_ID_PATH", defaultUpdateJobIDPath)
}

func customReleaseStatePath() string {
	return customReleaseEnv("SUB2API_PRODUCTION_RELEASE_STATE_PATH", defaultProductionReleaseStatePath)
}

func (s *UpdateService) CheckCustomRelease(ctx context.Context, force bool) (*CustomReleaseInfo, error) {
	_ = force
	info := &CustomReleaseInfo{
		CurrentVersion:    s.currentVersion,
		LatestVersion:     s.currentVersion,
		BuildType:         s.buildType,
		UpdateKind:        UpdateKindNone,
		DetectionComplete: true,
	}
	warnings := make([]string, 0, 3)
	state, stateErr := readProductionReleaseState(customReleaseStatePath())
	if stateErr != nil {
		warnings = append(warnings, "production state: "+stateErr.Error())
		info.DetectionComplete = false
	} else if state != nil {
		info.ProductionCommit = state.ProductionCommit
		info.ProductionStableTag = state.StableReleaseTag
		info.ProductionStableCommit = state.StableReleaseCommit
	}

	client, ok := s.githubClient.(customReleaseGitHubClient)
	if !ok || client == nil {
		info.DetectionComplete = false
		info.Warning = "custom release probe unavailable"
		return info, nil
	}

	var release *GitHubRelease
	var releaseCommit string
	if fetched, err := client.FetchLatestRelease(ctx, githubRepo); err != nil {
		warnings = append(warnings, "official release probe: "+err.Error())
		info.DetectionComplete = false
	} else {
		release = fetched
		if release == nil || release.Draft || release.Prerelease || strings.TrimSpace(release.TagName) == "" {
			warnings = append(warnings, "official release probe returned no stable Release")
			info.DetectionComplete = false
			release = nil
		} else {
			info.LatestVersion = strings.TrimPrefix(release.TagName, "v")
			info.ReleaseInfo = customReleaseInfoFromGitHubRelease(release)
		}
		if release != nil {
			if commit, err := client.FetchRefCommit(ctx, githubRepo, release.TagName); err != nil {
				warnings = append(warnings, "stable commit probe: "+err.Error())
				info.DetectionComplete = false
			} else {
				releaseCommit = commit
			}
		}
	}

	if release != nil {
		if state != nil && state.StableReleaseTag != "" {
			info.OfficialUpdate = release.TagName != state.StableReleaseTag
			if releaseCommit != "" && state.StableReleaseCommit != "" {
				info.OfficialUpdate = info.OfficialUpdate || releaseCommit != state.StableReleaseCommit
			}
		} else {
			info.OfficialUpdate = compareVersions(s.currentVersion, strings.TrimPrefix(release.TagName, "v")) < 0
		}
	}

	customHead, err := client.FetchCustomReleaseHead(ctx, githubCustomRepo, "custom-release")
	if err != nil {
		warnings = append(warnings, "custom branch probe: "+err.Error())
		info.DetectionComplete = false
	} else if customHead == nil || !isFullCommitSHA(customHead.SHA) {
		warnings = append(warnings, "custom branch probe returned no commit")
		info.DetectionComplete = false
	} else {
		info.TargetCustomCommit = strings.TrimSpace(customHead.SHA)
		info.TargetCustomShortSHA = info.TargetCustomCommit[:8]
		if state == nil || strings.TrimSpace(state.ProductionCommit) == "" {
			warnings = append(warnings, "production commit is unavailable; custom update cannot be safely classified")
			info.DetectionComplete = false
		} else if info.TargetCustomCommit != state.ProductionCommit {
			info.CustomUpdate = true
			files, compareErr := client.CompareCommits(ctx, githubCustomRepo, state.ProductionCommit, info.TargetCustomCommit)
			if compareErr != nil {
				info.CustomScopeError = compareErr.Error()
				warnings = append(warnings, "custom scope probe: "+compareErr.Error())
				info.DetectionComplete = false
			} else {
				info.DocsOnly = len(files) > 0 && allDocumentationFiles(files)
			}
		}
	}

	info.RuntimeUpdate = info.OfficialUpdate || (info.CustomUpdate && !info.DocsOnly)
	info.HasUpdate = info.OfficialUpdate || info.CustomUpdate
	switch {
	case !info.HasUpdate:
		info.UpdateKind = UpdateKindNone
	case info.OfficialUpdate && info.CustomUpdate:
		info.UpdateKind = UpdateKindCombined
	case info.OfficialUpdate:
		info.UpdateKind = UpdateKindOfficial
	case info.DocsOnly:
		info.UpdateKind = UpdateKindDocsOnly
	default:
		info.UpdateKind = UpdateKindCustom
	}
	if len(warnings) > 0 {
		info.Warning = strings.Join(warnings, "; ")
	}
	return info, nil
}

func (s *UpdateService) PrepareUpdate(ctx context.Context) (*UpdateJob, error) {
	return s.queueCustomRelease(ctx, UpdateActionPrepare)
}

func (s *UpdateService) queueCustomRelease(ctx context.Context, action string) (*UpdateJob, error) {
	customReleaseMu.Lock()
	defer customReleaseMu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	jobsDir := customReleaseJobsDir()
	jobIDPath := customReleaseJobIDPath()
	if currentID, readErr := os.ReadFile(jobIDPath); readErr == nil {
		currentID := strings.TrimSpace(string(currentID))
		if currentPath, pathErr := updateJobPath(jobsDir, currentID); currentID != "" && pathErr == nil {
			if existing, statusErr := readUpdateStatus(currentPath, currentID); statusErr == nil && !IsPollingSettledUpdateStatus(existing.Status) {
				return nil, ErrUpdateInProgress
			}
			if existing, statusErr := readUpdateStatus(currentPath, currentID); statusErr == nil && existing.Status == UpdateStatusPrepared && !preparedJobExpired(existing) {
				return nil, ErrUpdateInProgress
			}
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, fmt.Errorf("read current update job id: %w", readErr)
	}

	scriptPath := customReleaseScriptPath()
	if _, err := os.Stat(scriptPath); err != nil {
		return nil, fmt.Errorf("sync script not found at %s: %w", scriptPath, err)
	}
	jobID, err := newUpdateJobID()
	if err != nil {
		return nil, err
	}
	startedAt := time.Now().UTC()
	job := &UpdateJob{JobID: jobID, Action: action, Status: UpdateStatusCheckingUpdates, Message: "release job queued", Timestamp: startedAt, UpdatedAt: startedAt, StartedAt: &startedAt}
	jobPath, err := updateJobPath(jobsDir, jobID)
	if err != nil {
		return nil, err
	}
	if err := writeUpdateStatus(jobPath, job); err != nil {
		return nil, err
	}
	if err := writeCurrentUpdateJobID(jobIDPath, jobID); err != nil {
		return nil, fmt.Errorf("write update job id: %w", err)
	}
	wait, err := customReleaseStartScript(scriptPath, action, jobID)
	if err != nil {
		finishedAt := time.Now().UTC()
		_ = setCustomReleaseStatus(jobID, UpdateStatusFailed, "failed to start sync: "+err.Error(), &startedAt, &finishedAt)
		return nil, fmt.Errorf("start upstream sync: %w", err)
	}
	go func() { _ = wait() }()
	return job, nil
}

func (s *UpdateService) ApplyUpdate(ctx context.Context, jobID string) (*UpdateJob, error) {
	customReleaseMu.Lock()
	defer customReleaseMu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, ErrUpdateJobIDRequired
	}
	jobPath, err := updateJobPath(customReleaseJobsDir(), jobID)
	if err != nil {
		return nil, ErrUpdateJobNotFound
	}
	job, err := readUpdateStatus(jobPath, jobID)
	if err != nil {
		return nil, err
	}
	if job.Status == UpdateStatusPrepared {
		if preparedJobExpired(job) {
			job.Status = UpdateStatusExpired
			job.Message = "prepared update expired; prepare again"
			job.UpdatedAt = time.Now().UTC()
			_ = writeUpdateStatus(jobPath, job)
			return nil, ErrUpdateExpired
		}
		job.Action = UpdateActionApply
		job.Status = UpdateStatusApplyQueued
		job.Message = "update confirmation queued"
		job.UpdatedAt = time.Now().UTC()
		if err := writeUpdateStatus(jobPath, job); err != nil {
			return nil, err
		}
		wait, startErr := customReleaseStartScript(customReleaseScriptPath(), UpdateActionApply, jobID)
		if startErr != nil {
			job.Status = UpdateStatusFailed
			job.Message = "failed to start apply: " + startErr.Error()
			job.UpdatedAt = time.Now().UTC()
			_ = writeUpdateStatus(jobPath, job)
			return nil, fmt.Errorf("start apply: %w", startErr)
		}
		go func() { _ = wait() }()
		return job, nil
	}
	if job.Action == UpdateActionApply && !IsTerminalUpdateStatus(job.Status) {
		return job, nil
	}
	if job.Status == UpdateStatusSuccess {
		return job, nil
	}
	return nil, ErrUpdateNotPrepared
}

func preparedJobExpired(job *UpdateJob) bool {
	if job == nil || strings.TrimSpace(job.ExpiresAt) == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339, job.ExpiresAt)
	return err == nil && !time.Now().UTC().Before(expiresAt)
}

func startCustomReleaseScript(scriptPath, action, jobID string) (func() error, error) {
	cmd := exec.Command("/bin/sh", scriptPath, action, jobID)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Wait, nil
}

func isFullCommitSHA(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func readProductionReleaseState(path string) (*ProductionReleaseState, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state ProductionReleaseState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("decode release state: %w", err)
	}
	return &state, nil
}

func customReleaseInfoFromGitHubRelease(release *GitHubRelease) *ReleaseInfo {
	if release == nil {
		return nil
	}
	assets := make([]Asset, len(release.Assets))
	for index, asset := range release.Assets {
		assets[index] = Asset{Name: asset.Name, DownloadURL: asset.BrowserDownloadURL, Size: asset.Size}
	}
	return &ReleaseInfo{Name: release.Name, Body: release.Body, PublishedAt: release.PublishedAt, HTMLURL: release.HTMLURL, Assets: assets}
}

func allDocumentationFiles(files []ChangedFile) bool {
	if len(files) == 0 {
		return false
	}
	for _, file := range files {
		name := strings.TrimSpace(file.Filename)
		base := filepath.Base(name)
		if !strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".mdx") && base != ".gitignore" && base != "AGENTS.md" {
			return false
		}
	}
	return true
}
