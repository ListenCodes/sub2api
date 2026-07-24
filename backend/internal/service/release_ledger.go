package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const releaseLedgerSchemaVersion = 1

var (
	ErrLedgerInconsistent = infraerrors.InternalServer("LEDGER_INCONSISTENT", "release ledger is inconsistent")
	releaseIDPattern      = regexp.MustCompile(`^release-[A-Za-z0-9-]+$`)
	operationIDPattern    = regexp.MustCompile(`^(update|rollback)-[A-Za-z0-9-]+$`)
	versionPattern        = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	commitPattern         = regexp.MustCompile(`^[0-9a-f]{40}$`)
	digestPattern         = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	hashPattern           = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type ReleaseLedgerState struct {
	SchemaVersion          int    `json:"schema_version"`
	CurrentReleaseID       string `json:"current_release_id"`
	CustomVersionHighWater int    `json:"custom_version_high_water"`
	ActiveOperationID      string `json:"active_operation_id,omitempty"`
	UpdatedAt              string `json:"updated_at"`
}

type ReleaseRecord struct {
	SchemaVersion         int    `json:"schema_version"`
	ReleaseID             string `json:"release_id"`
	OfficialVersion       string `json:"official_version"`
	OfficialCommit        string `json:"official_commit"`
	CustomVersion         string `json:"custom_version"`
	CustomVersionSequence int    `json:"custom_version_sequence"`
	CustomCommit          string `json:"custom_commit"`
	MainDigest            string `json:"main_digest"`
	ExtensionsDigest      string `json:"extensions_digest"`
	BaseComposeSHA256     string `json:"base_compose_sha256"`
	CustomComposeSHA256   string `json:"custom_compose_sha256"`
	RenderedComposeSHA256 string `json:"rendered_compose_sha256"`
	EnvSHA256             string `json:"env_sha256"`
	BackupDir             string `json:"backup_dir"`
	BackupManifestSHA256  string `json:"backup_manifest_sha256"`
	PublishedAt           string `json:"published_at"`
	SourceKind            string `json:"source_kind"`
	OperationID           string `json:"operation_id"`
}

type releaseLedgerStore struct {
	root         string
	artifactRoot string
}

func newReleaseLedgerStoreWithArtifactRoot(root, artifactRoot string) *releaseLedgerStore {
	cleanRoot := filepath.Clean(root)
	return &releaseLedgerStore{root: cleanRoot, artifactRoot: filepath.Clean(artifactRoot)}
}

func (s *releaseLedgerStore) ReadState() (*ReleaseLedgerState, error) {
	var state ReleaseLedgerState
	if err := readReleaseLedgerJSON(filepath.Join(s.root, "state.json"), &state); err != nil {
		return nil, ledgerInconsistent("read state", err)
	}
	if state.SchemaVersion != releaseLedgerSchemaVersion ||
		!validReleaseID(state.CurrentReleaseID) ||
		state.CustomVersionHighWater < 0 ||
		(state.ActiveOperationID != "" && !operationIDPattern.MatchString(state.ActiveOperationID)) ||
		!validRFC3339(state.UpdatedAt) {
		return nil, ledgerInconsistent("invalid state", nil)
	}
	return &state, nil
}

func (s *releaseLedgerStore) ClearActiveOperation(operationID string) error {
	if !operationIDPattern.MatchString(operationID) {
		return ledgerInconsistent("invalid operation id", nil)
	}
	state, err := s.ReadState()
	if err != nil {
		return err
	}
	if state.ActiveOperationID != operationID {
		return ledgerInconsistent("active operation does not match", nil)
	}
	state.ActiveOperationID = ""
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return writeReleaseLedgerJSONAtomic(filepath.Join(s.root, "state.json"), state)
}

func (s *releaseLedgerStore) CurrentRelease() (*ReleaseRecord, error) {
	state, err := s.ReadState()
	if err != nil {
		return nil, err
	}
	return s.currentReleaseFromState(state)
}

func (s *releaseLedgerStore) currentReleaseFromState(state *ReleaseLedgerState) (*ReleaseRecord, error) {
	if state == nil {
		return nil, ledgerInconsistent("state is required", nil)
	}
	record, err := s.readRecord(state.CurrentReleaseID)
	if err != nil {
		return nil, err
	}
	if err := s.validateRecord(record, state.CustomVersionHighWater); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *releaseLedgerStore) ListRollbackReleases(limit int) ([]ReleaseRecord, error) {
	state, err := s.ReadState()
	if err != nil {
		return nil, err
	}
	if _, err := s.CurrentRelease(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(s.root, "releases"))
	if err != nil {
		return nil, ledgerInconsistent("read releases", err)
	}

	records := make([]ReleaseRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		releaseID := strings.TrimSuffix(entry.Name(), ".json")
		record, err := s.readRecord(releaseID)
		if err != nil {
			return nil, err
		}
		if record.ReleaseID != releaseID {
			return nil, ledgerInconsistent("release filename does not match record", nil)
		}
		if err := s.validateRecord(record, state.CustomVersionHighWater); err != nil {
			return nil, err
		}
		if record.ReleaseID == state.CurrentReleaseID || !s.rollbackArtifactsAvailable(record) {
			continue
		}
		records = append(records, *record)
	}

	sort.Slice(records, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339, records[i].PublishedAt)
		right, _ := time.Parse(time.RFC3339, records[j].PublishedAt)
		if left.Equal(right) {
			return records[i].ReleaseID > records[j].ReleaseID
		}
		return left.After(right)
	})
	if limit <= 0 {
		return []ReleaseRecord{}, nil
	}
	if len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

func (s *releaseLedgerStore) readRecord(releaseID string) (*ReleaseRecord, error) {
	if !validReleaseID(releaseID) {
		return nil, ledgerInconsistent("invalid release id", nil)
	}
	var record ReleaseRecord
	path := filepath.Join(s.root, "releases", releaseID+".json")
	if err := readReleaseLedgerJSON(path, &record); err != nil {
		return nil, ledgerInconsistent("read release record", err)
	}
	return &record, nil
}

func (s *releaseLedgerStore) validateRecord(record *ReleaseRecord, highWater int) error {
	if record == nil ||
		record.SchemaVersion != releaseLedgerSchemaVersion ||
		!validReleaseID(record.ReleaseID) ||
		!versionPattern.MatchString(record.OfficialVersion) ||
		!commitPattern.MatchString(record.OfficialCommit) ||
		record.CustomVersionSequence < 0 ||
		record.CustomVersionSequence > highWater ||
		record.CustomVersion != fmt.Sprintf("v1.0.%d", record.CustomVersionSequence) ||
		!commitPattern.MatchString(record.CustomCommit) ||
		!digestPattern.MatchString(record.MainDigest) ||
		!digestPattern.MatchString(record.ExtensionsDigest) ||
		!hashPattern.MatchString(record.BaseComposeSHA256) ||
		!hashPattern.MatchString(record.CustomComposeSHA256) ||
		!hashPattern.MatchString(record.RenderedComposeSHA256) ||
		!hashPattern.MatchString(record.EnvSHA256) ||
		!hashPattern.MatchString(record.BackupManifestSHA256) ||
		!validRFC3339(record.PublishedAt) ||
		!validReleaseSourceKind(record.SourceKind) ||
		strings.TrimSpace(record.OperationID) == "" {
		return ledgerInconsistent("invalid release record", nil)
	}
	if _, err := s.canonicalArtifactPath(record.BackupDir); err != nil {
		return ledgerInconsistent("invalid backup path", err)
	}
	return nil
}

func (s *releaseLedgerStore) rollbackArtifactsAvailable(record *ReleaseRecord) bool {
	backupDir, err := s.canonicalArtifactPath(record.BackupDir)
	if err != nil {
		return false
	}
	targetPath, err := s.canonicalArtifactPath(filepath.Join(backupDir, "target"))
	if err != nil {
		return false
	}
	target, err := os.Stat(targetPath)
	if err != nil || !target.IsDir() {
		return false
	}
	checksumsPath, err := s.canonicalArtifactPath(filepath.Join(targetPath, "SHA256SUMS"))
	if err != nil {
		return false
	}
	checksums, err := os.Stat(checksumsPath)
	return err == nil && checksums.Mode().IsRegular()
}

func (s *releaseLedgerStore) canonicalArtifactPath(path string) (string, error) {
	root, err := filepath.Abs(s.artifactRoot)
	if err != nil {
		return "", err
	}
	candidate, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(candidate); resolveErr == nil {
		candidate = resolved
	} else if !errors.Is(resolveErr, os.ErrNotExist) {
		return "", resolveErr
	}
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	} else if !errors.Is(resolveErr, os.ErrNotExist) {
		return "", resolveErr
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("path is outside artifact root")
	}
	return candidate, nil
}

func readReleaseLedgerJSON(path string, target any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return err
	}
	return nil
}

func writeReleaseLedgerJSONAtomic(path string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".release-ledger-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0644); err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return syncReleaseDirectory(path)
}

func validReleaseID(value string) bool {
	return value != "." && value != ".." && releaseIDPattern.MatchString(value)
}

func validRFC3339(value string) bool {
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}

func validReleaseSourceKind(value string) bool {
	switch value {
	case "official", "custom", "combined", "bootstrap":
		return true
	default:
		return false
	}
}

func ledgerInconsistent(message string, cause error) error {
	if cause == nil {
		return infraerrors.InternalServer("LEDGER_INCONSISTENT", message)
	}
	return infraerrors.InternalServer("LEDGER_INCONSISTENT", message).WithCause(cause)
}
