package service

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrNoUpdateAvailable         = infraerrors.Conflict("ALREADY_UP_TO_DATE", "no update available; current version is latest")
	ErrRollbackVersionNotAllowed = infraerrors.BadRequest("ROLLBACK_VERSION_NOT_ALLOWED", "version is not in the allowed rollback list")
)

const (
	updateCacheKey = "update_check_cache"
	updateCacheTTL = 1200 // 20 minutes
	githubRepo     = "Wei-Shaw/sub2api"

	// Security: allowed download domains for updates
	allowedDownloadHost = "github.com"
	allowedAssetHost    = "objects.githubusercontent.com"

	// Security: max download size (500MB)
	maxDownloadSize = 500 * 1024 * 1024

	// Rollback: expose at most the 3 most recent versions older than current
	maxRollbackVersions = 3
	// Fetch a few extra releases so filtering (current/newer/prerelease) still leaves enough candidates
	rollbackFetchPageSize             = 15
	defaultProductionReleaseStatePath = "/app/data/release-state.json"
	githubCustomRepo                  = "ListenCodes/sub2api"
)

const (
	UpdateKindNone     = "none"
	UpdateKindOfficial = "official"
	UpdateKindCustom   = "custom"
	UpdateKindCombined = "combined"
	UpdateKindDocsOnly = "docs-only"
)

// UpdateCache defines cache operations for update service
type UpdateCache interface {
	GetUpdateInfo(ctx context.Context) (string, error)
	SetUpdateInfo(ctx context.Context, data string, ttl time.Duration) error
}

// GitHubReleaseClient 获取 GitHub release 信息的接口
type GitHubReleaseClient interface {
	FetchLatestRelease(ctx context.Context, repo string) (*GitHubRelease, error)
	FetchRecentReleases(ctx context.Context, repo string, perPage int) ([]*GitHubRelease, error)
	FetchCustomReleaseHead(ctx context.Context, repo, branch string) (*GitRef, error)
	CompareCommits(ctx context.Context, repo, base, head string) ([]ChangedFile, error)
	FetchRefCommit(ctx context.Context, repo, ref string) (string, error)
	DownloadFile(ctx context.Context, url, dest string, maxSize int64) error
	FetchChecksumFile(ctx context.Context, url string) ([]byte, error)
}

// UpdateService handles software updates
type UpdateService struct {
	cache               UpdateCache
	githubClient        GitHubReleaseClient
	currentVersion      string
	buildType           string // "source" for manual builds, "release" for CI builds
	scriptPath          string
	jobsDir             string
	jobIDPath           string
	productionStatePath string
	startScript         func(string, string, string) (func() error, error)
	performMu           sync.Mutex
}

// NewUpdateService creates a new UpdateService
func NewUpdateService(cache UpdateCache, githubClient GitHubReleaseClient, version, buildType string) *UpdateService {
	return &UpdateService{
		cache:               cache,
		githubClient:        githubClient,
		currentVersion:      version,
		buildType:           buildType,
		scriptPath:          defaultUpdateScriptPath,
		jobsDir:             defaultUpdateJobsDir,
		jobIDPath:           defaultUpdateJobIDPath,
		productionStatePath: defaultProductionReleaseStatePath,
		startScript:         startUpdateScript,
	}
}

// UpdateInfo contains update information
type UpdateInfo struct {
	CurrentVersion         string       `json:"current_version"`
	LatestVersion          string       `json:"latest_version"`
	HasUpdate              bool         `json:"has_update"`
	ReleaseInfo            *ReleaseInfo `json:"release_info,omitempty"`
	Cached                 bool         `json:"cached"`
	Warning                string       `json:"warning,omitempty"`
	BuildType              string       `json:"build_type"` // "source" or "release"
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

// ReleaseInfo contains GitHub release details
type ReleaseInfo struct {
	Name        string  `json:"name"`
	Body        string  `json:"body"`
	PublishedAt string  `json:"published_at"`
	HTMLURL     string  `json:"html_url"`
	Assets      []Asset `json:"assets,omitempty"`
}

// Asset represents a release asset
type Asset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
	Size        int64  `json:"size"`
}

// GitHubRelease represents GitHub API response
type GitHubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	PublishedAt string        `json:"published_at"`
	HTMLURL     string        `json:"html_url"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	Assets      []GitHubAsset `json:"assets"`
}

// RollbackVersion describes a release version the system can roll back to
type RollbackVersion struct {
	Version     string `json:"version"` // without "v" prefix, e.g. "0.1.146"
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
}

type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// CheckUpdate checks for available updates
func (s *UpdateService) CheckUpdate(ctx context.Context, force bool) (*UpdateInfo, error) {
	_ = force // production state and custom refs are intentionally never served from cache
	info := &UpdateInfo{
		CurrentVersion:    s.currentVersion,
		LatestVersion:     s.currentVersion,
		BuildType:         s.buildType,
		UpdateKind:        UpdateKindNone,
		DetectionComplete: true,
	}
	warnings := make([]string, 0, 3)
	state, stateErr := readProductionReleaseState(s.productionStatePath)
	if stateErr != nil {
		warnings = append(warnings, "production state: "+stateErr.Error())
		info.DetectionComplete = false
	} else if state != nil {
		info.ProductionCommit = state.ProductionCommit
		info.ProductionStableTag = state.StableReleaseTag
		info.ProductionStableCommit = state.StableReleaseCommit
	}

	var release *GitHubRelease
	var releaseCommit string
	if s.githubClient == nil {
		warnings = append(warnings, "official release probe unavailable")
		info.DetectionComplete = false
	} else if fetched, err := s.githubClient.FetchLatestRelease(ctx, githubRepo); err != nil {
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
			info.ReleaseInfo = releaseInfoFromGitHubRelease(release)
		}
		if release != nil {
			if commit, err := s.githubClient.FetchRefCommit(ctx, githubRepo, release.TagName); err != nil {
				warnings = append(warnings, "stable commit probe: "+err.Error())
				info.DetectionComplete = false
			} else {
				releaseCommit = commit
			}
		}
	}

	if release != nil {
		stableChanged := false
		if state != nil && state.StableReleaseTag != "" {
			stableChanged = release.TagName != state.StableReleaseTag
			if releaseCommit != "" && state.StableReleaseCommit != "" {
				stableChanged = stableChanged || releaseCommit != state.StableReleaseCommit
			}
		} else {
			stableChanged = compareVersions(s.currentVersion, strings.TrimPrefix(release.TagName, "v")) < 0
		}
		info.OfficialUpdate = stableChanged
	}

	var customHead *GitRef
	if s.githubClient == nil {
		info.DetectionComplete = false
	} else if fetched, err := s.githubClient.FetchCustomReleaseHead(ctx, githubCustomRepo, "custom-release"); err != nil {
		warnings = append(warnings, "custom branch probe: "+err.Error())
		info.DetectionComplete = false
	} else {
		customHead = fetched
		if customHead == nil || !isFullCommitSHA(customHead.SHA) {
			warnings = append(warnings, "custom branch probe returned no commit")
			info.DetectionComplete = false
		} else {
			info.TargetCustomCommit = strings.TrimSpace(customHead.SHA)
			info.TargetCustomShortSHA = info.TargetCustomCommit[:8]
		}
		if info.TargetCustomCommit == "" {
			// A missing custom ref cannot be classified as none or docs-only.
		} else if state == nil || strings.TrimSpace(state.ProductionCommit) == "" {
			warnings = append(warnings, "production commit is unavailable; custom update cannot be safely classified")
			info.DetectionComplete = false
		} else if info.TargetCustomCommit != state.ProductionCommit {
			info.CustomUpdate = true
			files, err := s.githubClient.CompareCommits(ctx, githubCustomRepo, state.ProductionCommit, info.TargetCustomCommit)
			if err != nil {
				info.CustomScopeError = err.Error()
				warnings = append(warnings, "custom scope probe: "+err.Error())
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
	s.saveToCache(ctx, info)
	return info, nil
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
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
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

func releaseInfoFromGitHubRelease(release *GitHubRelease) *ReleaseInfo {
	if release == nil {
		return nil
	}
	assets := make([]Asset, len(release.Assets))
	for i, asset := range release.Assets {
		assets[i] = Asset{Name: asset.Name, DownloadURL: asset.BrowserDownloadURL, Size: asset.Size}
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

// PerformUpdate starts the conflict-gated upstream sync/publish job and returns
// before the host-side workflow completes.
func (s *UpdateService) PerformUpdate(ctx context.Context) (*UpdateJob, error) {
	return s.PrepareUpdate(ctx)
}

// PrepareUpdate queues only the non-mutating preparation phase.
func (s *UpdateService) PrepareUpdate(ctx context.Context) (*UpdateJob, error) {
	return s.queueUpdate(ctx, UpdateActionPrepare)
}

func (s *UpdateService) queueUpdate(ctx context.Context, action string) (*UpdateJob, error) {
	s.performMu.Lock()
	defer s.performMu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if currentID, readErr := os.ReadFile(s.jobIDPath); readErr == nil {
		currentID := strings.TrimSpace(string(currentID))
		if currentID != "" {
			if currentPath, pathErr := updateJobPath(s.jobsDir, currentID); pathErr == nil {
				if existing, statusErr := readUpdateStatus(currentPath, currentID); statusErr == nil && !IsPollingSettledUpdateStatus(existing.Status) {
					return nil, ErrUpdateInProgress
				}
				if existing, statusErr := readUpdateStatus(currentPath, currentID); statusErr == nil && existing.Status == UpdateStatusPrepared && !preparedJobExpired(existing) {
					return nil, ErrUpdateInProgress
				}
			}
		}
	} else if !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("read current update job id: %w", readErr)
	}
	if _, err := os.Stat(s.scriptPath); err != nil {
		return nil, fmt.Errorf("sync script not found at %s: %w", s.scriptPath, err)
	}

	jobID, err := newUpdateJobID()
	if err != nil {
		return nil, err
	}
	startedAt := time.Now().UTC()
	job := &UpdateJob{
		JobID:       jobID,
		Action:      action,
		Status:      UpdateStatusCheckingUpdates,
		Message:     "release job queued",
		NeedRestart: false,
		Published:   false,
		Timestamp:   startedAt,
		UpdatedAt:   startedAt,
		StartedAt:   &startedAt,
	}
	jobPath, err := updateJobPath(s.jobsDir, jobID)
	if err != nil {
		return nil, err
	}
	if err := writeUpdateStatus(jobPath, job); err != nil {
		return nil, err
	}
	if err := writeCurrentUpdateJobID(s.jobIDPath, jobID); err != nil {
		return nil, fmt.Errorf("write update job id: %w", err)
	}

	wait, err := s.startScript(s.scriptPath, action, jobID)
	if err != nil {
		finishedAt := time.Now().UTC()
		_ = s.setUpdateStatus(jobID, UpdateStatusFailed, "failed to start sync: "+err.Error(), &startedAt, &finishedAt)
		return nil, fmt.Errorf("start upstream sync: %w", err)
	}
	go func() {
		_ = wait()
	}()

	return job, nil
}

// ApplyUpdate queues the explicit production switch for a prepared job.
func (s *UpdateService) ApplyUpdate(ctx context.Context, jobID string) (*UpdateJob, error) {
	s.performMu.Lock()
	defer s.performMu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, ErrUpdateJobIDRequired
	}
	jobPath, err := updateJobPath(s.jobsDir, jobID)
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
		wait, startErr := s.startScript(s.scriptPath, UpdateActionApply, jobID)
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
	if IsTerminalUpdateStatus(job.Status) && job.Status == UpdateStatusSuccess {
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

func startUpdateScript(scriptPath, action, jobID string) (func() error, error) {
	cmd := exec.Command("/bin/sh", scriptPath, action, jobID)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Wait, nil
}

// applyReleaseAssets downloads the platform archive from the given release assets,
// verifies its checksum, and atomically swaps the running binary.
// Shared by PerformUpdate (latest) and RollbackToVersion (specific older version).
func (s *UpdateService) applyReleaseAssets(ctx context.Context, releaseAssets []Asset) error {
	// Find matching archive and checksum for current platform
	archiveName := s.getArchiveName()
	var downloadURL string
	var checksumURL string

	for _, asset := range releaseAssets {
		if strings.Contains(asset.Name, archiveName) && !strings.HasSuffix(asset.Name, ".txt") {
			downloadURL = asset.DownloadURL
		}
		if asset.Name == "checksums.txt" {
			checksumURL = asset.DownloadURL
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no compatible release found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// SECURITY: Validate download URL is from trusted domain
	if err := validateDownloadURL(downloadURL); err != nil {
		return fmt.Errorf("invalid download URL: %w", err)
	}
	if checksumURL != "" {
		if err := validateDownloadURL(checksumURL); err != nil {
			return fmt.Errorf("invalid checksum URL: %w", err)
		}
	}

	// Get current executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	exeDir := filepath.Dir(exePath)

	// Create temp directory in the SAME directory as executable
	// This ensures os.Rename is atomic (same filesystem)
	tempDir, err := os.MkdirTemp(exeDir, ".sub2api-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Download archive
	archivePath := filepath.Join(tempDir, filepath.Base(downloadURL))
	if err := s.downloadFile(ctx, downloadURL, archivePath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Verify checksum if available
	if checksumURL != "" {
		if err := s.verifyChecksum(ctx, archivePath, checksumURL); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	// Extract binary from archive
	newBinaryPath := filepath.Join(tempDir, "sub2api")
	if err := s.extractBinary(archivePath, newBinaryPath); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// Set executable permission before replacement
	if err := os.Chmod(newBinaryPath, 0755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}

	// Atomic replacement using rename pattern:
	// 1. Rename current -> backup (atomic on Unix)
	// 2. Rename new -> current (atomic on Unix, same filesystem)
	// If step 2 fails, restore backup
	backupPath := exePath + ".backup"

	// Remove old backup if exists
	_ = os.Remove(backupPath)

	// Step 1: Move current binary to backup
	if err := os.Rename(exePath, backupPath); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	// Step 2: Move new binary to target location (atomic, same filesystem)
	if err := os.Rename(newBinaryPath, exePath); err != nil {
		// Restore backup on failure
		if restoreErr := os.Rename(backupPath, exePath); restoreErr != nil {
			return fmt.Errorf("replace failed and restore failed: %w (restore error: %v)", err, restoreErr)
		}
		return fmt.Errorf("replace failed (restored backup): %w", err)
	}

	// Success - backup file is kept for rollback capability
	// It will be cleaned up on next successful update
	return nil
}

// Rollback restores the previous version
func (s *UpdateService) Rollback() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	backupFile := exePath + ".backup"
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		return fmt.Errorf("no backup found")
	}

	// Replace current with backup
	if err := os.Rename(backupFile, exePath); err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	return nil
}

// ListRollbackVersions returns up to maxRollbackVersions release versions that are
// strictly older than the current version (the current version itself is excluded),
// newest first. Draft and prerelease entries are skipped.
func (s *UpdateService) ListRollbackVersions(ctx context.Context) ([]RollbackVersion, error) {
	releases, err := s.fetchRollbackCandidates(ctx)
	if err != nil {
		return nil, err
	}

	versions := make([]RollbackVersion, 0, len(releases))
	for _, r := range releases {
		versions = append(versions, RollbackVersion{
			Version:     strings.TrimPrefix(r.TagName, "v"),
			PublishedAt: r.PublishedAt,
			HTMLURL:     r.HTMLURL,
		})
	}
	return versions, nil
}

// RollbackToVersion downloads and installs a specific older version.
// The target must be one of the versions returned by ListRollbackVersions;
// anything else (including the current version) is rejected.
func (s *UpdateService) RollbackToVersion(ctx context.Context, version string) error {
	target := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if target == "" {
		return ErrRollbackVersionNotAllowed
	}

	releases, err := s.fetchRollbackCandidates(ctx)
	if err != nil {
		return err
	}

	var match *GitHubRelease
	for _, r := range releases {
		if strings.TrimPrefix(r.TagName, "v") == target {
			match = r
			break
		}
	}
	if match == nil {
		return ErrRollbackVersionNotAllowed
	}

	assets := make([]Asset, len(match.Assets))
	for i, a := range match.Assets {
		assets[i] = Asset{
			Name:        a.Name,
			DownloadURL: a.BrowserDownloadURL,
			Size:        a.Size,
		}
	}

	return s.applyReleaseAssets(ctx, assets)
}

// fetchRollbackCandidates fetches recent releases and keeps the newest
// maxRollbackVersions entries strictly older than the current version.
func (s *UpdateService) fetchRollbackCandidates(ctx context.Context) ([]*GitHubRelease, error) {
	releases, err := s.githubClient.FetchRecentReleases(ctx, githubRepo, rollbackFetchPageSize)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(releases))
	candidates := make([]*GitHubRelease, 0, maxRollbackVersions)
	for _, r := range releases {
		if r == nil || r.Draft || r.Prerelease {
			continue
		}
		v := strings.TrimPrefix(r.TagName, "v")
		if v == "" || seen[v] {
			continue
		}
		// Only versions strictly older than current (also excludes current itself)
		if compareVersions(v, s.currentVersion) >= 0 {
			continue
		}
		seen[v] = true
		candidates = append(candidates, r)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return compareVersions(
			strings.TrimPrefix(candidates[i].TagName, "v"),
			strings.TrimPrefix(candidates[j].TagName, "v"),
		) > 0
	})

	if len(candidates) > maxRollbackVersions {
		candidates = candidates[:maxRollbackVersions]
	}
	return candidates, nil
}

func (s *UpdateService) downloadFile(ctx context.Context, downloadURL, dest string) error {
	return s.githubClient.DownloadFile(ctx, downloadURL, dest, maxDownloadSize)
}

func (s *UpdateService) getArchiveName() string {
	osName := runtime.GOOS
	arch := runtime.GOARCH
	return fmt.Sprintf("%s_%s", osName, arch)
}

// validateDownloadURL checks if the URL is from an allowed domain
// SECURITY: This prevents SSRF and ensures downloads only come from trusted GitHub domains
func validateDownloadURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Must be HTTPS
	if parsedURL.Scheme != "https" {
		return fmt.Errorf("only HTTPS URLs are allowed")
	}

	// Check against allowed hosts
	host := parsedURL.Host
	// GitHub release URLs can be from github.com or objects.githubusercontent.com
	if host != allowedDownloadHost &&
		!strings.HasSuffix(host, "."+allowedDownloadHost) &&
		host != allowedAssetHost &&
		!strings.HasSuffix(host, "."+allowedAssetHost) {
		return fmt.Errorf("download from untrusted host: %s", host)
	}

	return nil
}

func (s *UpdateService) verifyChecksum(ctx context.Context, filePath, checksumURL string) error {
	// Download checksums file
	checksumData, err := s.githubClient.FetchChecksumFile(ctx, checksumURL)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}

	// Calculate file hash
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actualHash := hex.EncodeToString(h.Sum(nil))

	// Find expected hash in checksums file
	fileName := filepath.Base(filePath)
	scanner := bufio.NewScanner(strings.NewReader(string(checksumData)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == fileName {
			if parts[0] == actualHash {
				return nil
			}
			return fmt.Errorf("checksum mismatch: expected %s, got %s", parts[0], actualHash)
		}
	}

	return fmt.Errorf("checksum not found for %s", fileName)
}

func (s *UpdateService) extractBinary(archivePath, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var reader io.Reader = f

	// Handle gzip compression
	if strings.HasSuffix(archivePath, ".gz") || strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz") {
		gzr, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer func() { _ = gzr.Close() }()
		reader = gzr
	}

	// Handle tar archive
	if strings.Contains(archivePath, ".tar") {
		tr := tar.NewReader(reader)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}

			// SECURITY: Prevent Zip Slip / Path Traversal attack
			// Only allow files with safe base names, no directory traversal
			baseName := filepath.Base(hdr.Name)

			// Check for path traversal attempts
			if strings.Contains(hdr.Name, "..") {
				return fmt.Errorf("path traversal attempt detected: %s", hdr.Name)
			}

			// Validate the entry is a regular file
			if hdr.Typeflag != tar.TypeReg {
				continue // Skip directories and special files
			}

			// Only extract the specific binary we need
			if baseName == "sub2api" || baseName == "sub2api.exe" {
				// Additional security: limit file size (max 500MB)
				const maxBinarySize = 500 * 1024 * 1024
				if hdr.Size > maxBinarySize {
					return fmt.Errorf("binary too large: %d bytes (max %d)", hdr.Size, maxBinarySize)
				}

				out, err := os.Create(destPath)
				if err != nil {
					return err
				}

				// Use LimitReader to prevent decompression bombs
				limited := io.LimitReader(tr, maxBinarySize)
				if _, err := io.Copy(out, limited); err != nil {
					_ = out.Close()
					return err
				}
				if err := out.Close(); err != nil {
					return err
				}
				return nil
			}
		}
		return fmt.Errorf("binary not found in archive")
	}

	// Direct copy for non-tar files (with size limit)
	const maxBinarySize = 500 * 1024 * 1024
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}

	limited := io.LimitReader(reader, maxBinarySize)
	if _, err := io.Copy(out, limited); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func (s *UpdateService) saveToCache(ctx context.Context, info *UpdateInfo) {
	if s.cache == nil {
		return
	}
	cacheData := struct {
		Latest      string       `json:"latest"`
		ReleaseInfo *ReleaseInfo `json:"release_info"`
		Timestamp   int64        `json:"timestamp"`
	}{
		Latest:      info.LatestVersion,
		ReleaseInfo: info.ReleaseInfo,
		Timestamp:   time.Now().Unix(),
	}

	data, _ := json.Marshal(cacheData)
	_ = s.cache.SetUpdateInfo(ctx, string(data), time.Duration(updateCacheTTL)*time.Second)
}

// compareVersions compares two semantic versions
func compareVersions(current, latest string) int {
	currentParts := parseVersion(current)
	latestParts := parseVersion(latest)

	for i := 0; i < 3; i++ {
		if currentParts[i] < latestParts[i] {
			return -1
		}
		if currentParts[i] > latestParts[i] {
			return 1
		}
	}
	return 0
}

func parseVersion(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	result := [3]int{0, 0, 0}
	for i := 0; i < len(parts) && i < 3; i++ {
		if parsed, err := strconv.Atoi(parts[i]); err == nil {
			result[i] = parsed
		}
	}
	return result
}
