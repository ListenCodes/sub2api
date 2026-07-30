package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	defaultCustomReleaseNoticeStatePath = "/app/data/custom-release-notice-state.json"
	customReleaseNoticeSchemaVersion    = 1
	customReleaseNoticeMaxAdmins        = 10_000
	customReleaseNoticeFingerprintLabel = "custom-release-notice-v1"
)

type customReleaseNoticeAdminState struct {
	LastReadFingerprint string `json:"last_read_fingerprint"`
	ReadAt              string `json:"read_at"`
}

type customReleaseNoticeState struct {
	SchemaVersion int                                      `json:"schema_version"`
	Admins        map[string]customReleaseNoticeAdminState `json:"admins"`
}

type customReleaseNoticePruneCandidate struct {
	UserID int64
	Key    string
	ReadAt time.Time
}

var customReleaseNoticeMu sync.Mutex

func customReleaseUpdateFingerprint(info *CustomReleaseInfo) string {
	if info == nil || !info.DetectionComplete || !info.HasUpdate || info.UpdateKind == UpdateKindNone {
		return ""
	}
	hash := sha256.New()
	for _, field := range []string{
		customReleaseNoticeFingerprintLabel,
		info.UpdateKind,
		info.TargetOfficialVersion,
		info.TargetOfficialCommit,
		info.TargetCustomCommit,
	} {
		_, _ = hash.Write([]byte(field))
		_, _ = hash.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func customReleaseNoticeStatePath() string {
	return customReleaseEnv("SUB2API_RELEASE_NOTICE_STATE_FILE", defaultCustomReleaseNoticeStatePath)
}

func (s *UpdateService) CustomReleaseNoticeUnread(ctx context.Context, userID int64, fingerprint string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return true, err
	}
	return customReleaseNoticeUnread(userID, fingerprint)
}

func (s *UpdateService) MarkCustomReleaseNoticeRead(ctx context.Context, userID int64, fingerprint string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return markCustomReleaseNoticeRead(userID, fingerprint)
}

func customReleaseNoticeUnread(userID int64, fingerprint string) (bool, error) {
	if err := validateCustomReleaseNoticeKey(userID, fingerprint); err != nil {
		return true, err
	}
	customReleaseNoticeMu.Lock()
	defer customReleaseNoticeMu.Unlock()

	state, err := readCustomReleaseNoticeState(customReleaseNoticeStatePath())
	if err != nil {
		return true, err
	}
	admin, ok := state.Admins[strconv.FormatInt(userID, 10)]
	return !ok || admin.LastReadFingerprint != fingerprint, nil
}

func markCustomReleaseNoticeRead(userID int64, fingerprint string) error {
	if err := validateCustomReleaseNoticeKey(userID, fingerprint); err != nil {
		return err
	}
	customReleaseNoticeMu.Lock()
	defer customReleaseNoticeMu.Unlock()

	path := customReleaseNoticeStatePath()
	state, err := readCustomReleaseNoticeState(path)
	if err != nil {
		return err
	}
	key := strconv.FormatInt(userID, 10)
	if _, exists := state.Admins[key]; !exists && len(state.Admins) >= customReleaseNoticeMaxAdmins {
		pruneOldestCustomReleaseNoticeAdmin(&state)
	}
	state.Admins[key] = customReleaseNoticeAdminState{
		LastReadFingerprint: fingerprint,
		ReadAt:              time.Now().UTC().Format(time.RFC3339Nano),
	}
	return writeCustomReleaseNoticeState(path, &state)
}

func validateCustomReleaseNoticeKey(userID int64, fingerprint string) error {
	if userID <= 0 {
		return fmt.Errorf("custom release notice user ID must be positive")
	}
	if !hashPattern.MatchString(fingerprint) {
		return fmt.Errorf("custom release notice fingerprint is invalid")
	}
	return nil
}

func readCustomReleaseNoticeState(path string) (customReleaseNoticeState, error) {
	state := customReleaseNoticeState{
		SchemaVersion: customReleaseNoticeSchemaVersion,
		Admins:        make(map[string]customReleaseNoticeAdminState),
	}
	if err := rejectCustomReleaseNoticeSymlink(path); err != nil {
		return state, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, fmt.Errorf("read custom release notice state: %w", err)
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return customReleaseNoticeState{}, fmt.Errorf("decode custom release notice state: %w", err)
	}
	if err := validateCustomReleaseNoticeState(&state); err != nil {
		return customReleaseNoticeState{}, err
	}
	return state, nil
}

func validateCustomReleaseNoticeState(state *customReleaseNoticeState) error {
	if state == nil || state.SchemaVersion != customReleaseNoticeSchemaVersion || state.Admins == nil {
		return fmt.Errorf("custom release notice state schema is invalid")
	}
	if len(state.Admins) > customReleaseNoticeMaxAdmins {
		return fmt.Errorf("custom release notice state exceeds the admin limit")
	}
	for key, admin := range state.Admins {
		userID, err := strconv.ParseInt(key, 10, 64)
		if err != nil || userID <= 0 || strconv.FormatInt(userID, 10) != key {
			return fmt.Errorf("custom release notice state contains an invalid user ID")
		}
		if !hashPattern.MatchString(admin.LastReadFingerprint) {
			return fmt.Errorf("custom release notice state contains an invalid fingerprint")
		}
		if _, err := time.Parse(time.RFC3339, admin.ReadAt); err != nil {
			return fmt.Errorf("custom release notice state contains an invalid read time")
		}
	}
	return nil
}

func pruneOldestCustomReleaseNoticeAdmin(state *customReleaseNoticeState) {
	candidates := make([]customReleaseNoticePruneCandidate, 0, len(state.Admins))
	for key, admin := range state.Admins {
		userID, _ := strconv.ParseInt(key, 10, 64)
		readAt, _ := time.Parse(time.RFC3339, admin.ReadAt)
		candidates = append(candidates, customReleaseNoticePruneCandidate{UserID: userID, Key: key, ReadAt: readAt})
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].ReadAt.Equal(candidates[right].ReadAt) {
			return candidates[left].UserID < candidates[right].UserID
		}
		return candidates[left].ReadAt.Before(candidates[right].ReadAt)
	})
	if len(candidates) > 0 {
		delete(state.Admins, candidates[0].Key)
	}
}

func writeCustomReleaseNoticeState(path string, state *customReleaseNoticeState) error {
	if err := validateCustomReleaseNoticeState(state); err != nil {
		return err
	}
	if err := rejectCustomReleaseNoticeSymlink(path); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return fmt.Errorf("create custom release notice state directory: %w", err)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode custom release notice state: %w", err)
	}
	raw = append(raw, '\n')

	temporary, err := os.CreateTemp(parent, ".custom-release-notice-state-*")
	if err != nil {
		return fmt.Errorf("create custom release notice state temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect custom release notice state temporary file: %w", err)
	}
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write custom release notice state temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync custom release notice state temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close custom release notice state temporary file: %w", err)
	}
	if err := rejectCustomReleaseNoticeSymlink(path); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace custom release notice state: %w", err)
	}
	if runtime.GOOS != "windows" {
		directory, err := os.Open(parent)
		if err != nil {
			return fmt.Errorf("open custom release notice state directory: %w", err)
		}
		syncErr := directory.Sync()
		closeErr := directory.Close()
		if syncErr != nil {
			return fmt.Errorf("sync custom release notice state directory: %w", syncErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close custom release notice state directory: %w", closeErr)
		}
	}
	return nil
}

func rejectCustomReleaseNoticeSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect custom release notice state: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("custom release notice state must not be a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("custom release notice state must be a regular file")
	}
	return nil
}
