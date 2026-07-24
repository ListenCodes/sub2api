package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	defaultProductionReleaseStatePath = "/app/data/release-state.json"
	defaultReleaseLedgerRoot          = "/app/data/release-ledger"
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
	ReleaseID              string       `json:"release_id,omitempty"`
	CurrentOfficialVersion string       `json:"current_official_version,omitempty"`
	CurrentCustomVersion   string       `json:"current_custom_version,omitempty"`
	TargetOfficialVersion  string       `json:"target_official_version,omitempty"`
	TargetCustomVersion    string       `json:"target_custom_version,omitempty"`
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
	customReleaseMu                 sync.Mutex
	customReleaseStartScript        = startCustomReleaseScript
	ErrUpdateDetectionIncomplete    = infraerrors.Conflict("UPDATE_DETECTION_INCOMPLETE", "release detection is incomplete; retry before preparing")
	ErrRollbackReleaseInvalid       = infraerrors.BadRequest("ROLLBACK_RELEASE_INVALID", "rollback release is not eligible")
	ErrReleaseOperationInconsistent = infraerrors.InternalServer("RELEASE_OPERATION_INCONSISTENT", "release operation state is inconsistent")
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
	return customReleaseEnv("SUB2API_RELEASE_OPERATIONS_DIR", filepath.Join(customReleaseLedgerRoot(), "operations"))
}

func customReleaseLegacyJobsDir() string {
	return customReleaseEnv("SUB2API_LEGACY_RELEASE_JOBS_DIR", defaultUpdateJobsDir)
}

func customReleaseOperationPath(jobID string) (string, error) {
	operationPath, err := updateJobPath(customReleaseJobsDir(), jobID)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(operationPath); err == nil {
		return operationPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	legacyPath, err := updateJobPath(customReleaseLegacyJobsDir(), jobID)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(legacyPath); err == nil {
		return "", ErrLegacySinglePhase
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return operationPath, nil
}

func customReleaseJobIDPath() string {
	return customReleaseEnv("SUB2API_RELEASE_JOB_ID_PATH", defaultUpdateJobIDPath)
}

func customReleaseLedgerRoot() string {
	return customReleaseEnv("SUB2API_RELEASE_LEDGER_ROOT", defaultReleaseLedgerRoot)
}

func customReleaseBackupRoot() string {
	return customReleaseEnv("SUB2API_RELEASE_BACKUP_ROOT", filepath.Join(filepath.Dir(customReleaseLedgerRoot()), "release-backups"))
}

func newCustomReleaseLedgerStore() *releaseLedgerStore {
	return newReleaseLedgerStoreWithArtifactRoot(customReleaseLedgerRoot(), customReleaseBackupRoot())
}

func (s *UpdateService) CheckCustomRelease(ctx context.Context, force bool) (*CustomReleaseInfo, error) {
	_ = force
	info := &CustomReleaseInfo{
		CurrentVersion: s.currentVersion,
		LatestVersion:  s.currentVersion,
		BuildType:      s.buildType,
		UpdateKind:     UpdateKindNone,
	}
	warnings := make([]string, 0, 3)
	ledger := newCustomReleaseLedgerStore()
	state, stateErr := ledger.ReadState()
	var current *ReleaseRecord
	if stateErr != nil {
		warnings = append(warnings, "release ledger: "+stateErr.Error())
	} else {
		current, stateErr = ledger.currentReleaseFromState(state)
		if stateErr != nil {
			warnings = append(warnings, "current release: "+stateErr.Error())
		} else {
			info.DetectionComplete = true
			info.ReleaseID = current.ReleaseID
			info.CurrentOfficialVersion = current.OfficialVersion
			info.CurrentCustomVersion = current.CustomVersion
			info.TargetOfficialVersion = current.OfficialVersion
			info.TargetCustomVersion = current.CustomVersion
			info.CurrentVersion = strings.TrimPrefix(current.OfficialVersion, "v")
			info.LatestVersion = info.CurrentVersion
			info.ProductionCommit = current.CustomCommit
			info.ProductionStableTag = current.OfficialVersion
			info.ProductionStableCommit = current.OfficialCommit
		}
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

	if release != nil && current != nil {
		info.OfficialUpdate = release.TagName != current.OfficialVersion
		if releaseCommit != "" {
			info.OfficialUpdate = info.OfficialUpdate || releaseCommit != current.OfficialCommit
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
		if current == nil {
			warnings = append(warnings, "current release is unavailable; custom update cannot be safely classified")
			info.DetectionComplete = false
		} else if info.TargetCustomCommit != current.CustomCommit {
			info.CustomUpdate = true
			files, compareErr := client.CompareCommits(ctx, githubCustomRepo, current.CustomCommit, info.TargetCustomCommit)
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
	case info.OfficialUpdate && info.CustomUpdate && !info.DocsOnly:
		info.UpdateKind = UpdateKindCombined
	case info.OfficialUpdate:
		info.UpdateKind = UpdateKindOfficial
	case info.DocsOnly:
		info.UpdateKind = UpdateKindDocsOnly
	default:
		info.UpdateKind = UpdateKindCustom
	}
	if current != nil {
		if info.OfficialUpdate && release != nil {
			info.TargetOfficialVersion = release.TagName
			info.LatestVersion = strings.TrimPrefix(release.TagName, "v")
		}
		if info.CustomUpdate && !info.DocsOnly {
			info.TargetCustomVersion = fmt.Sprintf("v1.0.%d", state.CustomVersionHighWater+1)
		} else if info.OfficialUpdate {
			info.TargetCustomVersion = current.CustomVersion
		} else if info.DocsOnly {
			info.TargetCustomVersion = ""
		}
	}
	if len(warnings) > 0 {
		info.Warning = strings.Join(warnings, "; ")
	}
	return info, nil
}

func (s *UpdateService) PrepareUpdate(ctx context.Context) (*UpdateJob, error) {
	return s.queueOperation(ctx, ReleaseOperationUpdate, ReleasePhasePrepare, "")
}

func (s *UpdateService) ApplyUpdate(ctx context.Context, jobID string) (*UpdateJob, error) {
	return s.queueOperation(ctx, ReleaseOperationUpdate, ReleasePhaseApply, jobID)
}

func (s *UpdateService) CurrentRelease(ctx context.Context) (*ReleaseRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return newCustomReleaseLedgerStore().CurrentRelease()
}

func (s *UpdateService) ListRollbackReleases(ctx context.Context) ([]ReleaseRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return newCustomReleaseLedgerStore().ListRollbackReleases(3)
}

func (s *UpdateService) PrepareRollback(ctx context.Context, releaseID string) (*UpdateJob, error) {
	return s.queueOperation(ctx, ReleaseOperationRollback, ReleasePhasePrepare, strings.TrimSpace(releaseID))
}

func (s *UpdateService) ApplyRollback(ctx context.Context, jobID string) (*UpdateJob, error) {
	return s.queueOperation(ctx, ReleaseOperationRollback, ReleasePhaseApply, strings.TrimSpace(jobID))
}

func (s *UpdateService) queueOperation(ctx context.Context, kind, phase, reference string) (*UpdateJob, error) {
	customReleaseMu.Lock()
	defer customReleaseMu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !isValidReleaseOperationKind(kind) || !isValidReleasePhase(phase) {
		return nil, fmt.Errorf("invalid release operation")
	}
	if phase == ReleasePhaseApply {
		return s.applyOperation(ctx, kind, reference)
	}
	if kind == ReleaseOperationRollback && !validReleaseID(reference) {
		return nil, ErrRollbackReleaseInvalid
	}

	jobsDir := customReleaseJobsDir()
	jobIDPath := customReleaseJobIDPath()
	if currentID, readErr := os.ReadFile(jobIDPath); readErr == nil {
		currentID := strings.TrimSpace(string(currentID))
		if currentID == "" {
			return nil, ErrReleaseOperationInconsistent
		}
		currentPath, pathErr := customReleaseOperationPath(currentID)
		if pathErr != nil {
			if errors.Is(pathErr, ErrLegacySinglePhase) {
				return nil, ErrLegacySinglePhase
			}
			return nil, ErrReleaseOperationInconsistent.WithCause(pathErr)
		}
		existing, statusErr := readUpdateStatus(currentPath, currentID)
		if statusErr != nil {
			return nil, ErrReleaseOperationInconsistent.WithCause(statusErr)
		}
		if !IsTerminalUpdateStatus(existing.Status) || existing.Status == ReleaseStatusPrepared {
			if existing.Status == ReleaseStatusPrepared && preparedJobExpired(existing) {
				if err := queuePreparedOperationExpiration(existing); err != nil {
					return nil, err
				}
				return nil, ErrUpdateInProgress
			} else if existing.OperationKind == kind && existing.Action == ReleasePhasePrepare && existing.TargetReleaseID == reference {
				return existing, nil
			} else {
				return nil, ErrUpdateInProgress
			}
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, fmt.Errorf("read current update job id: %w", readErr)
	}

	job, err := s.buildPreparedOperation(ctx, kind, reference)
	if err != nil {
		return nil, err
	}
	scriptPath := customReleaseScriptPath()
	if _, err := os.Stat(scriptPath); err != nil {
		return nil, fmt.Errorf("sync script not found at %s: %w", scriptPath, err)
	}
	jobID, err := newReleaseOperationID(kind)
	if err != nil {
		return nil, err
	}
	startedAt := time.Now().UTC()
	job.JobID = jobID
	job.OperationKind = kind
	job.Action = phase
	job.Timestamp = startedAt
	job.UpdatedAt = startedAt
	job.StartedAt = &startedAt
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
	wait, err := customReleaseStartScript(scriptPath, phase, jobID)
	if err != nil {
		finishedAt := time.Now().UTC()
		_ = setCustomReleaseStatus(jobID, UpdateStatusFailed, "failed to start sync: "+err.Error(), &startedAt, &finishedAt)
		return nil, fmt.Errorf("start upstream sync: %w", err)
	}
	go func() { _ = wait() }()
	return job, nil
}

func (s *UpdateService) buildPreparedOperation(ctx context.Context, kind, targetReleaseID string) (*UpdateJob, error) {
	ledger := newCustomReleaseLedgerStore()
	state, err := ledger.ReadState()
	if err != nil {
		return nil, err
	}
	current, err := ledger.currentReleaseFromState(state)
	if err != nil {
		return nil, err
	}
	job := &UpdateJob{
		BaseReleaseID:          current.ReleaseID,
		CurrentOfficialVersion: current.OfficialVersion,
		CurrentCustomVersion:   current.CustomVersion,
		Message:                "release operation queued",
	}
	if kind == ReleaseOperationUpdate {
		info, err := s.CheckCustomRelease(ctx, true)
		if err != nil {
			return nil, err
		}
		if !info.DetectionComplete {
			return nil, ErrUpdateDetectionIncomplete
		}
		if !info.RuntimeUpdate || info.UpdateKind == UpdateKindNone || info.UpdateKind == UpdateKindDocsOnly {
			return nil, ErrNoUpdateAvailable
		}
		if info.ReleaseID != current.ReleaseID {
			return nil, ErrUpdateDetectionIncomplete
		}
		job.Status = ReleaseStatusResolvingTarget
		job.TargetOfficialVersion = info.TargetOfficialVersion
		job.TargetCustomVersion = info.TargetCustomVersion
		job.TargetCustomCommit = info.TargetCustomCommit
		job.CustomDocsOnly = info.DocsOnly
		job.UpdateKind = info.UpdateKind
		job.ProductionCommit = current.CustomCommit
		job.StableReleaseTag = current.OfficialVersion
		job.StableReleaseCommit = current.OfficialCommit
		job.AdvancesCustomVersion = info.TargetCustomVersion != current.CustomVersion
		return job, nil
	}

	releases, err := ledger.ListRollbackReleases(3)
	if err != nil {
		return nil, err
	}
	for index := range releases {
		target := &releases[index]
		if target.ReleaseID != targetReleaseID {
			continue
		}
		job.Status = ReleaseStatusResolvingSnapshot
		job.TargetReleaseID = target.ReleaseID
		job.TargetOfficialVersion = target.OfficialVersion
		job.TargetCustomVersion = target.CustomVersion
		job.TargetCustomCommit = target.CustomCommit
		job.MainDigest = target.MainDigest
		job.ExtensionsDigest = target.ExtensionsDigest
		return job, nil
	}
	return nil, ErrRollbackReleaseInvalid
}

func (s *UpdateService) applyOperation(ctx context.Context, kind, jobID string) (*UpdateJob, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, ErrUpdateJobIDRequired
	}
	currentID, err := os.ReadFile(customReleaseJobIDPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrUpdateJobNotFound
		}
		return nil, fmt.Errorf("read current update job id: %w", err)
	}
	if strings.TrimSpace(string(currentID)) != jobID {
		return nil, ErrUpdateInProgress
	}
	jobPath, err := customReleaseOperationPath(jobID)
	if err != nil {
		if errors.Is(err, ErrLegacySinglePhase) {
			return nil, ErrLegacySinglePhase
		}
		return nil, ErrUpdateJobNotFound
	}
	job, err := readUpdateStatus(jobPath, jobID)
	if err != nil {
		return nil, err
	}
	if job.OperationKind != kind {
		return nil, ErrUpdateNotPrepared
	}
	if job.Status == ReleaseStatusPrepared {
		if preparedJobExpired(job) {
			if err := queuePreparedOperationExpiration(job); err != nil {
				return nil, err
			}
			return nil, ErrUpdateExpired
		}
		scriptPath := customReleaseScriptPath()
		if _, err := os.Stat(scriptPath); err != nil {
			return nil, fmt.Errorf("sync script not found at %s: %w", scriptPath, err)
		}
		job.Action = ReleasePhaseApply
		job.Status = ReleaseStatusApplyQueued
		job.Message = "release confirmation queued"
		job.UpdatedAt = time.Now().UTC()
		if err := writeUpdateStatus(jobPath, job); err != nil {
			return nil, err
		}
		wait, startErr := customReleaseStartScript(scriptPath, ReleasePhaseApply, jobID)
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
	if job.Action == ReleasePhaseApply && !IsTerminalUpdateStatus(job.Status) {
		return job, nil
	}
	if job.Status == ReleaseStatusSuccess {
		return job, nil
	}
	return nil, ErrUpdateNotPrepared
}

func queuePreparedOperationExpiration(job *UpdateJob) error {
	if job == nil || !operationIDPattern.MatchString(job.JobID) {
		return ErrReleaseOperationInconsistent
	}
	scriptPath := customReleaseScriptPath()
	if _, err := os.Stat(scriptPath); err != nil {
		return fmt.Errorf("sync script not found at %s: %w", scriptPath, err)
	}
	wait, err := customReleaseStartScript(scriptPath, ReleasePhaseExpire, job.JobID)
	if err != nil {
		return fmt.Errorf("start expiration settlement: %w", err)
	}
	if err := wait(); err != nil {
		return fmt.Errorf("queue expiration settlement: %w", err)
	}
	return nil
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
