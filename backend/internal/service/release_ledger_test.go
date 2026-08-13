//go:build unit

package service

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestReleaseLedgerCurrentReleaseReturnsVersionPair(t *testing.T) {
	root := t.TempDir()
	record := releaseLedgerTestRecord(root, "release-current", 4, "2026-07-23T08:00:00Z")
	writeReleaseLedgerFixture(t, root, ReleaseLedgerState{
		SchemaVersion:          1,
		CurrentReleaseID:       record.ReleaseID,
		CustomVersionHighWater: 4,
		UpdatedAt:              "2026-07-23T08:00:00Z",
	}, record)

	artifactRoot := filepath.Join(root, "artifacts")
	current, err := newReleaseLedgerStoreWithArtifactRoots(root, artifactRoot, artifactRoot).CurrentRelease()
	require.NoError(t, err)
	require.Equal(t, "v0.1.163", current.OfficialVersion)
	require.Equal(t, "v1.0.4", current.CustomVersion)
	require.Equal(t, record.ReleaseID, current.ReleaseID)
}

func TestReleaseLedgerAcceptsIdentityConfigurationRelease(t *testing.T) {
	root := t.TempDir()
	record := releaseLedgerTestRecord(root, "release-config-current", 4, "2026-07-23T08:00:00Z")
	record.SourceKind = UpdateKindIdentityConfig
	record.IdentityTransition = IdentityTransitionStage2Admin
	protectedRoot := filepath.Join(root, "protected-artifacts")
	require.NoError(t, os.MkdirAll(protectedRoot, 0o700))
	require.NoError(t, os.Rename(record.BackupDir, filepath.Join(protectedRoot, record.ReleaseID)))
	record.BackupDir = filepath.Join(protectedRoot, record.ReleaseID)
	writeReleaseLedgerFixture(t, root, ReleaseLedgerState{
		SchemaVersion: 1, CurrentReleaseID: record.ReleaseID, CustomVersionHighWater: 9, UpdatedAt: "2026-07-23T08:00:00Z",
	}, record)
	artifactRoot := filepath.Join(root, "artifacts")
	store := newReleaseLedgerStoreWithArtifactRoots(root, artifactRoot, artifactRoot)
	store.protectedRecordArtifactRoot = protectedRoot
	current, err := store.CurrentRelease()
	require.NoError(t, err)
	require.Equal(t, IdentityTransitionStage2Admin, current.IdentityTransition)
	require.True(t, store.rollbackArtifactsAvailable(current))

	record.BackupDir = filepath.Join(artifactRoot, record.ReleaseID)
	writeReleaseLedgerJSON(t, filepath.Join(root, "releases", record.ReleaseID+".json"), record)
	_, err = store.CurrentRelease()
	require.Error(t, err)

	record.BackupDir = filepath.Join(protectedRoot, record.ReleaseID)
	record.IdentityTransition = ""
	writeReleaseLedgerJSON(t, filepath.Join(root, "releases", record.ReleaseID+".json"), record)
	_, err = store.CurrentRelease()
	require.Error(t, err)
}

func TestReleaseLedgerListsLastThreeEligibleReleases(t *testing.T) {
	root := t.TempDir()
	current := releaseLedgerTestRecord(root, "release-current", 4, "2026-07-23T08:00:00Z")
	records := []ReleaseRecord{
		current,
		releaseLedgerTestRecord(root, "release-oldest", 0, "2026-07-19T08:00:00Z"),
		releaseLedgerTestRecord(root, "release-third", 1, "2026-07-20T08:00:00Z"),
		releaseLedgerTestRecord(root, "release-ineligible", 2, "2026-07-21T08:00:00Z"),
		releaseLedgerTestRecord(root, "release-second", 3, "2026-07-22T08:00:00Z"),
	}
	writeReleaseLedgerFixture(t, root, ReleaseLedgerState{
		SchemaVersion:          1,
		CurrentReleaseID:       current.ReleaseID,
		CustomVersionHighWater: 4,
		UpdatedAt:              "2026-07-23T08:00:00Z",
	}, records...)
	require.NoError(t, os.Remove(filepath.Join(records[3].BackupDir, "target", "SHA256SUMS")))

	artifactRoot := filepath.Join(root, "artifacts")
	rollback, err := newReleaseLedgerStoreWithArtifactRoots(root, artifactRoot, artifactRoot).ListRollbackReleases(3, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"release-second", "release-third", "release-oldest"}, []string{
		rollback[0].ReleaseID,
		rollback[1].ReleaseID,
		rollback[2].ReleaseID,
	})
}

func TestReleaseLedgerExcludesIncompleteRollbackSnapshots(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, ReleaseRecord)
	}{
		{
			name: "missing risk database dump",
			mutate: func(t *testing.T, record ReleaseRecord) {
				require.NoError(t, os.Remove(filepath.Join(record.BackupDir, "risk_control_db.dump")))
			},
		},
		{
			name: "corrupt top-level manifest",
			mutate: func(t *testing.T, record ReleaseRecord) {
				manifest := filepath.Join(record.BackupDir, "SHA256SUMS")
				file, err := os.OpenFile(manifest, os.O_APPEND|os.O_WRONLY, 0)
				require.NoError(t, err)
				_, err = file.WriteString(strings.Repeat("f", 64) + "  missing-file\n")
				require.NoError(t, err)
				require.NoError(t, file.Close())
			},
		},
		{
			name: "compose hash mismatch",
			mutate: func(t *testing.T, record ReleaseRecord) {
				require.NoError(t, os.WriteFile(filepath.Join(record.BackupDir, "target", "docker-compose.yml"), []byte("changed\n"), 0644))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			current := releaseLedgerTestRecord(root, "release-current", 4, "2026-07-23T08:00:00Z")
			valid := releaseLedgerTestRecord(root, "release-valid", 1, "2026-07-20T08:00:00Z")
			invalid := releaseLedgerTestRecord(root, "release-invalid", 3, "2026-07-22T08:00:00Z")
			writeReleaseLedgerFixture(t, root, ReleaseLedgerState{
				SchemaVersion: 1, CurrentReleaseID: current.ReleaseID, CustomVersionHighWater: 4,
				UpdatedAt: "2026-07-23T08:00:00Z",
			}, current, valid, invalid)
			test.mutate(t, invalid)

			artifactRoot := filepath.Join(root, "artifacts")
			rollback, err := newReleaseLedgerStoreWithArtifactRoots(root, artifactRoot, artifactRoot).ListRollbackReleases(3, nil)
			require.NoError(t, err)
			require.Equal(t, []ReleaseRecord{valid}, rollback)
		})
	}
}

func TestReleaseLedgerRejectsInconsistentStateOrRecords(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, string, *ReleaseLedgerState, *ReleaseRecord)
	}{
		{
			name: "missing current record",
			mutate: func(t *testing.T, root string, _ *ReleaseLedgerState, record *ReleaseRecord) {
				require.NoError(t, os.Remove(filepath.Join(root, "releases", record.ReleaseID+".json")))
			},
		},
		{
			name: "malformed custom version",
			mutate: func(_ *testing.T, _ string, _ *ReleaseLedgerState, record *ReleaseRecord) {
				record.CustomVersion = "1.0.4"
			},
		},
		{
			name: "sequence version mismatch",
			mutate: func(_ *testing.T, _ string, _ *ReleaseLedgerState, record *ReleaseRecord) {
				record.CustomVersion = "v1.0.3"
			},
		},
		{
			name: "bad digest",
			mutate: func(_ *testing.T, _ string, _ *ReleaseLedgerState, record *ReleaseRecord) {
				record.MainDigest = "sha256:not-a-digest"
			},
		},
		{
			name: "high water regression",
			mutate: func(_ *testing.T, _ string, state *ReleaseLedgerState, _ *ReleaseRecord) {
				state.CustomVersionHighWater = 3
			},
		},
		{
			name: "artifact path escape",
			mutate: func(_ *testing.T, root string, _ *ReleaseLedgerState, record *ReleaseRecord) {
				record.BackupDir = filepath.Join(root, "..", "outside")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			record := releaseLedgerTestRecord(root, "release-current", 4, "2026-07-23T08:00:00Z")
			state := ReleaseLedgerState{
				SchemaVersion:          1,
				CurrentReleaseID:       record.ReleaseID,
				CustomVersionHighWater: 4,
				UpdatedAt:              "2026-07-23T08:00:00Z",
			}
			writeReleaseLedgerFixture(t, root, state, record)
			test.mutate(t, root, &state, &record)
			if test.name != "missing current record" {
				writeReleaseLedgerFixture(t, root, state, record)
			}

			artifactRoot := filepath.Join(root, "artifacts")
			_, err := newReleaseLedgerStoreWithArtifactRoots(root, artifactRoot, artifactRoot).CurrentRelease()
			require.Error(t, err)
			require.Equal(t, "LEDGER_INCONSISTENT", infraerrors.Reason(err))
		})
	}
}

func releaseLedgerTestRecord(root, releaseID string, sequence int, publishedAt string) ReleaseRecord {
	record := ReleaseRecord{
		SchemaVersion:         1,
		ReleaseID:             releaseID,
		OfficialVersion:       "v0.1.163",
		OfficialCommit:        strings.Repeat("a", 40),
		CustomVersion:         fmt.Sprintf("v1.0.%d", sequence),
		CustomVersionSequence: sequence,
		CustomCommit:          strings.Repeat("b", 40),
		MainDigest:            "sha256:" + strings.Repeat("1", 64),
		ExtensionsDigest:      "sha256:" + strings.Repeat("2", 64),
		BaseComposeSHA256:     strings.Repeat("3", 64),
		CustomComposeSHA256:   strings.Repeat("4", 64),
		RenderedComposeSHA256: strings.Repeat("5", 64),
		EnvSHA256:             strings.Repeat("6", 64),
		BackupDir:             filepath.Join(root, "artifacts", releaseID),
		BackupManifestSHA256:  strings.Repeat("7", 64),
		PublishedAt:           publishedAt,
		SourceKind:            "custom",
		OperationID:           "update-" + releaseID,
	}
	if err := writeReleaseLedgerBackupFixture(&record); err != nil {
		panic(err)
	}
	return record
}

func writeReleaseLedgerFixture(t *testing.T, root string, state ReleaseLedgerState, records ...ReleaseRecord) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "releases"), 0755))
	writeReleaseLedgerJSON(t, filepath.Join(root, "state.json"), state)
	for _, record := range records {
		writeReleaseLedgerJSON(t, filepath.Join(root, "releases", record.ReleaseID+".json"), record)
	}
}

func writeReleaseLedgerBackupFixture(record *ReleaseRecord) error {
	files := map[string]string{
		".env": "root env\n", "docker-compose.yml": "root compose\n", "docker-compose.custom.yml": "root custom compose\n",
		"release-state.json": "{}\n", "container-metadata.json": "{}\n", "image-metadata.txt": "images\n",
		"rollback-tags.txt": "tags\n", "sub2api_db.dump": "main dump\n", "sub2api_db.list": "main list\n",
		"risk_control_db.dump": "risk dump\n", "risk_control_db.list": "risk list\n", "docker-containers.txt": "containers\n",
		"docker-images.txt": "images\n", "nginx-vhost.path": "/etc/nginx/nginx.conf\n", "origin-cert.path": "/etc/ssl/origin.crt\n",
		"origin-key.path": "/etc/ssl/origin.key\n", "nginx.conf": "nginx\n", "origin.crt": "cert\n", "origin.key": "key\n",
		"target/.env": "target env\n", "target/docker-compose.yml": "target compose\n",
		"target/docker-compose.custom.yml": "target custom compose\n", "target/rendered-compose.json": "{}\n",
	}
	for relative, content := range files {
		path := filepath.Join(record.BackupDir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return err
		}
	}
	if err := writeTestSHA256Manifest(filepath.Join(record.BackupDir, "target")); err != nil {
		return err
	}
	if err := writeTestSHA256Manifest(record.BackupDir); err != nil {
		return err
	}
	record.BaseComposeSHA256 = testFileSHA256(filepath.Join(record.BackupDir, "target", "docker-compose.yml"))
	record.CustomComposeSHA256 = testFileSHA256(filepath.Join(record.BackupDir, "target", "docker-compose.custom.yml"))
	record.RenderedComposeSHA256 = testFileSHA256(filepath.Join(record.BackupDir, "target", "rendered-compose.json"))
	record.EnvSHA256 = testFileSHA256(filepath.Join(record.BackupDir, "target", ".env"))
	record.BackupManifestSHA256 = testFileSHA256(filepath.Join(record.BackupDir, "SHA256SUMS"))
	return nil
}

func writeTestSHA256Manifest(root string) error {
	paths := make([]string, 0)
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() == "SHA256SUMS" {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(relative))
		return nil
	}); err != nil {
		return err
	}
	sort.Strings(paths)
	manifest, err := os.Create(filepath.Join(root, "SHA256SUMS"))
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(manifest)
	for _, relative := range paths {
		if _, err := fmt.Fprintf(writer, "%s  ./%s\n", testFileSHA256(filepath.Join(root, filepath.FromSlash(relative))), relative); err != nil {
			_ = manifest.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		_ = manifest.Close()
		return err
	}
	return manifest.Close()
}

func testFileSHA256(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func writeReleaseLedgerJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, raw, 0644))
}
