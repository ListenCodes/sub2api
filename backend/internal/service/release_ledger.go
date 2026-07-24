package service

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
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
	manifestLinePattern   = regexp.MustCompile(`^([0-9a-f]{64})[ \t][ \t*](.+)$`)
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
	if err := rejectReleaseBackupSymlinks(backupDir); err != nil {
		return false
	}
	required := []string{
		"SHA256SUMS", ".env", "docker-compose.yml", "docker-compose.custom.yml", "release-state.json",
		"container-metadata.json", "image-metadata.txt", "rollback-tags.txt", "sub2api_db.dump", "sub2api_db.list",
		"risk_control_db.dump", "risk_control_db.list", "docker-containers.txt", "docker-images.txt",
		"nginx-vhost.path", "origin-cert.path", "origin-key.path", "target/SHA256SUMS", "target/.env",
		"target/docker-compose.yml", "target/docker-compose.custom.yml", "target/rendered-compose.json",
	}
	for _, relative := range required {
		if !regularReleaseBackupFile(filepath.Join(backupDir, filepath.FromSlash(relative)), true) {
			return false
		}
	}
	for _, pathFile := range []string{"nginx-vhost.path", "origin-cert.path", "origin-key.path"} {
		reference, err := firstReleaseBackupLine(filepath.Join(backupDir, pathFile))
		if err != nil {
			return false
		}
		base := path.Base(strings.ReplaceAll(reference, "\\", "/"))
		if base == "." || base == ".." || !regularReleaseBackupFile(filepath.Join(backupDir, base), false) {
			return false
		}
	}
	if err := validateReleaseBackupManifest(filepath.Join(backupDir, "target")); err != nil {
		return false
	}
	if err := validateReleaseBackupManifest(backupDir); err != nil {
		return false
	}
	checks := map[string]string{
		filepath.Join(backupDir, "SHA256SUMS"):                          record.BackupManifestSHA256,
		filepath.Join(backupDir, "target", "docker-compose.yml"):        record.BaseComposeSHA256,
		filepath.Join(backupDir, "target", "docker-compose.custom.yml"): record.CustomComposeSHA256,
		filepath.Join(backupDir, "target", "rendered-compose.json"):     record.RenderedComposeSHA256,
		filepath.Join(backupDir, "target", ".env"):                      record.EnvSHA256,
	}
	for file, expected := range checks {
		actual, err := releaseBackupFileSHA256(file)
		if err != nil || actual != expected {
			return false
		}
	}
	return true
}

func rejectReleaseBackupSymlinks(root string) error {
	return filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("release backup contains a symbolic link")
		}
		return nil
	})
}

func regularReleaseBackupFile(file string, requireContent bool) bool {
	info, err := os.Lstat(file)
	return err == nil && info.Mode().IsRegular() && (!requireContent || info.Size() > 0)
}

func firstReleaseBackupLine(file string) (string, error) {
	handle, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer func() { _ = handle.Close() }()
	scanner := bufio.NewScanner(handle)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("release backup path reference is empty")
	}
	reference := strings.TrimSpace(scanner.Text())
	if reference == "" {
		return "", fmt.Errorf("release backup path reference is empty")
	}
	return reference, nil
}

func validateReleaseBackupManifest(root string) error {
	manifestPath := filepath.Join(root, "SHA256SUMS")
	if !regularReleaseBackupFile(manifestPath, true) {
		return fmt.Errorf("release backup manifest is missing")
	}
	handle, err := os.Open(manifestPath)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	declared := make(map[string]string)
	scanner := bufio.NewScanner(handle)
	for scanner.Scan() {
		matches := manifestLinePattern.FindStringSubmatch(scanner.Text())
		if len(matches) != 3 || !validReleaseBackupRelativePath(matches[2]) {
			return fmt.Errorf("invalid release backup manifest entry")
		}
		if _, exists := declared[matches[2]]; exists {
			return fmt.Errorf("duplicate release backup manifest entry")
		}
		declared[matches[2]] = matches[1]
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	actual := make(map[string]string)
	if err := filepath.WalkDir(root, func(file string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("release backup contains a symbolic link")
		}
		if entry.IsDir() || entry.Name() == "SHA256SUMS" {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("release backup contains a non-regular file")
		}
		relative, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		digest, err := releaseBackupFileSHA256(file)
		if err != nil {
			return err
		}
		actual[relative] = digest
		return nil
	}); err != nil {
		return err
	}
	if len(actual) != len(declared) {
		return fmt.Errorf("release backup manifest coverage mismatch")
	}
	for relative, digest := range actual {
		if declared[relative] != digest {
			return fmt.Errorf("release backup manifest checksum mismatch")
		}
	}
	return nil
}

func validReleaseBackupRelativePath(relative string) bool {
	if relative == "" || strings.HasPrefix(relative, "/") || strings.Contains(relative, "\\") || strings.Contains(relative, "//") || path.Base(relative) == "SHA256SUMS" {
		return false
	}
	if len(relative) >= 3 && relative[1] == ':' && (relative[2] == '/' || relative[2] == '\\') {
		return false
	}
	for _, segment := range strings.Split(relative, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func releaseBackupFileSHA256(file string) (string, error) {
	handle, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer func() { _ = handle.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, handle); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
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
