//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCustomReleaseUpdateFingerprint(t *testing.T) {
	base := &CustomReleaseInfo{
		DetectionComplete:     true,
		HasUpdate:             true,
		UpdateKind:            UpdateKindCombined,
		TargetOfficialVersion: "v0.1.169",
		TargetOfficialCommit:  strings.Repeat("a", 40),
		TargetCustomCommit:    strings.Repeat("b", 40),
	}
	fingerprint := customReleaseUpdateFingerprint(base)
	require.Regexp(t, `^[0-9a-f]{64}$`, fingerprint)
	require.Equal(t, "9252a51c27e073d6c72fd49f4ad29fa1056b680b5936c2ad50d92ae5431e7a4f", fingerprint)
	require.Equal(t, fingerprint, customReleaseUpdateFingerprint(base))

	for name, mutate := range map[string]func(*CustomReleaseInfo){
		"kind":             func(info *CustomReleaseInfo) { info.UpdateKind = UpdateKindOfficial },
		"official version": func(info *CustomReleaseInfo) { info.TargetOfficialVersion = "v0.1.170" },
		"official commit":  func(info *CustomReleaseInfo) { info.TargetOfficialCommit = strings.Repeat("c", 40) },
		"custom commit":    func(info *CustomReleaseInfo) { info.TargetCustomCommit = strings.Repeat("d", 40) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := *base
			mutate(&changed)
			require.NotEqual(t, fingerprint, customReleaseUpdateFingerprint(&changed))
		})
	}

	for name, mutate := range map[string]func(*CustomReleaseInfo){
		"incomplete": func(info *CustomReleaseInfo) { info.DetectionComplete = false },
		"no update":  func(info *CustomReleaseInfo) { info.HasUpdate = false },
		"none":       func(info *CustomReleaseInfo) { info.UpdateKind = UpdateKindNone },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := *base
			mutate(&invalid)
			require.Empty(t, customReleaseUpdateFingerprint(&invalid))
		})
	}
	require.Empty(t, customReleaseUpdateFingerprint(nil))
}

func TestCustomReleaseNoticePersistsPerAdmin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notice-state.json")
	t.Setenv("SUB2API_RELEASE_NOTICE_STATE_FILE", path)
	fingerprint := strings.Repeat("a", 64)

	unread, err := customReleaseNoticeUnread(41, fingerprint)
	require.NoError(t, err)
	require.True(t, unread)
	require.NoError(t, markCustomReleaseNoticeRead(41, fingerprint))

	unread, err = customReleaseNoticeUnread(41, fingerprint)
	require.NoError(t, err)
	require.False(t, unread)
	other, err := customReleaseNoticeUnread(42, fingerprint)
	require.NoError(t, err)
	require.True(t, other)

	service := NewUpdateService(nil, nil, "", "")
	unread, err = service.CustomReleaseNoticeUnread(context.Background(), 41, fingerprint)
	require.NoError(t, err)
	require.False(t, unread)
	require.NoError(t, service.MarkCustomReleaseNoticeRead(context.Background(), 42, fingerprint))
	other, err = customReleaseNoticeUnread(42, fingerprint)
	require.NoError(t, err)
	require.False(t, other)
}

func TestCustomReleaseNoticeValidatesInputsAndState(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	t.Setenv("SUB2API_RELEASE_NOTICE_STATE_FILE", filepath.Join(t.TempDir(), "notice-state.json"))

	for _, userID := range []int64{-1, 0} {
		_, err := customReleaseNoticeUnread(userID, fingerprint)
		require.Error(t, err)
		require.Error(t, markCustomReleaseNoticeRead(userID, fingerprint))
	}
	for _, invalid := range []string{"", strings.Repeat("a", 63), strings.Repeat("A", 64), strings.Repeat("z", 64)} {
		_, err := customReleaseNoticeUnread(1, invalid)
		require.Error(t, err)
		require.Error(t, markCustomReleaseNoticeRead(1, invalid))
	}

	require.NoError(t, os.WriteFile(customReleaseNoticeStatePath(), []byte("{"), 0600))
	unread, err := customReleaseNoticeUnread(1, fingerprint)
	require.Error(t, err)
	require.True(t, unread)
	require.Error(t, markCustomReleaseNoticeRead(1, fingerprint))
}

func TestCustomReleaseNoticeRejectsSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not consistently available on Windows")
	}
	root := t.TempDir()
	target := filepath.Join(root, "target.json")
	statePath := filepath.Join(root, "notice-state.json")
	require.NoError(t, os.WriteFile(target, []byte(`{"schema_version":1,"admins":{}}`), 0600))
	require.NoError(t, os.Symlink(target, statePath))
	t.Setenv("SUB2API_RELEASE_NOTICE_STATE_FILE", statePath)

	unread, err := customReleaseNoticeUnread(1, strings.Repeat("a", 64))
	require.Error(t, err)
	require.True(t, unread)
	require.Error(t, markCustomReleaseNoticeRead(1, strings.Repeat("a", 64)))
}

func TestCustomReleaseNoticeWritesPrivateAtomicState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "notice-state.json")
	t.Setenv("SUB2API_RELEASE_NOTICE_STATE_FILE", path)
	require.NoError(t, markCustomReleaseNoticeRead(1, strings.Repeat("a", 64)))

	var state customReleaseNoticeState
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &state))
	require.Equal(t, customReleaseNoticeSchemaVersion, state.SchemaVersion)
	require.Len(t, state.Admins, 1)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	}

	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blocker, []byte("blocked"), 0600))
	t.Setenv("SUB2API_RELEASE_NOTICE_STATE_FILE", filepath.Join(blocker, "notice-state.json"))
	require.Error(t, markCustomReleaseNoticeRead(1, strings.Repeat("b", 64)))
}

func TestCustomReleaseNoticePrunesOldestAdminDeterministically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notice-state.json")
	t.Setenv("SUB2API_RELEASE_NOTICE_STATE_FILE", path)
	readAt := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC).Format(time.RFC3339)
	state := customReleaseNoticeState{
		SchemaVersion: customReleaseNoticeSchemaVersion,
		Admins:        make(map[string]customReleaseNoticeAdminState, customReleaseNoticeMaxAdmins),
	}
	for userID := 1; userID <= customReleaseNoticeMaxAdmins; userID++ {
		state.Admins[fmt.Sprintf("%d", userID)] = customReleaseNoticeAdminState{
			LastReadFingerprint: strings.Repeat("a", 64),
			ReadAt:              readAt,
		}
	}
	raw, err := json.Marshal(state)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0600))

	require.NoError(t, markCustomReleaseNoticeRead(int64(customReleaseNoticeMaxAdmins+1), strings.Repeat("b", 64)))
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	var persisted customReleaseNoticeState
	require.NoError(t, json.Unmarshal(raw, &persisted))
	require.Len(t, persisted.Admins, customReleaseNoticeMaxAdmins)
	require.NotContains(t, persisted.Admins, "1")
	require.Contains(t, persisted.Admins, "2")
	require.Contains(t, persisted.Admins, fmt.Sprintf("%d", customReleaseNoticeMaxAdmins+1))
}
