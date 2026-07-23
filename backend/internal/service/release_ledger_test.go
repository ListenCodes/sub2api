//go:build unit

package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	current, err := newReleaseLedgerStoreWithArtifactRoot(root, filepath.Join(root, "artifacts")).CurrentRelease()
	require.NoError(t, err)
	require.Equal(t, "v0.1.163", current.OfficialVersion)
	require.Equal(t, "v1.0.4", current.CustomVersion)
	require.Equal(t, record.ReleaseID, current.ReleaseID)
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

	rollback, err := newReleaseLedgerStoreWithArtifactRoot(root, filepath.Join(root, "artifacts")).ListRollbackReleases(3)
	require.NoError(t, err)
	require.Equal(t, []string{"release-second", "release-third", "release-oldest"}, []string{
		rollback[0].ReleaseID,
		rollback[1].ReleaseID,
		rollback[2].ReleaseID,
	})
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

			_, err := newReleaseLedgerStoreWithArtifactRoot(root, filepath.Join(root, "artifacts")).CurrentRelease()
			require.Error(t, err)
			require.Equal(t, "LEDGER_INCONSISTENT", infraerrors.Reason(err))
		})
	}
}

func releaseLedgerTestRecord(root, releaseID string, sequence int, publishedAt string) ReleaseRecord {
	return ReleaseRecord{
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
}

func writeReleaseLedgerFixture(t *testing.T, root string, state ReleaseLedgerState, records ...ReleaseRecord) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "releases"), 0755))
	writeReleaseLedgerJSON(t, filepath.Join(root, "state.json"), state)
	for _, record := range records {
		writeReleaseLedgerJSON(t, filepath.Join(root, "releases", record.ReleaseID+".json"), record)
		require.NoError(t, os.MkdirAll(filepath.Join(record.BackupDir, "target"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(record.BackupDir, "target", "SHA256SUMS"), []byte("fixture\n"), 0644))
	}
}

func writeReleaseLedgerJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, raw, 0644))
}
