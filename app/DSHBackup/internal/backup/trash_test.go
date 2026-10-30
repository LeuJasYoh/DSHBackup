package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"DSHBackup/internal/apppath"
	"DSHBackup/internal/manifest"
)

func trashFixture(t *testing.T) (apppath.Paths, manifest.Snapshot) {
	t.Helper()
	dir := t.TempDir()
	paths := apppath.Paths{DefaultBackupDir: filepath.Join(dir, "backups"), TempDir: filepath.Join(dir, "temp")}
	if err := os.MkdirAll(paths.DefaultBackupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "20261030-120000"
	archive := filepath.Join(paths.DefaultBackupDir, manifest.ArchiveName(id))
	content := []byte("archive-bytes")
	if err := os.WriteFile(archive, content, 0o600); err != nil {
		t.Fatal(err)
	}
	value := manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		ID:            id,
		CreatedAt:     "2026-10-30T12:00:00Z",
		Kind:          manifest.KindManual,
		Archive: manifest.ArchiveInfo{
			Name:   manifest.ArchiveName(id),
			Size:   int64(len(content)),
			SHA256: strings.Repeat("a", 64),
		},
	}
	manifestPath := manifest.ManifestPath(archive)
	if err := manifest.Write(manifestPath, value); err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.FromManifest(value, archive, manifestPath)
	snapshot.Status = "ready"
	return paths, snapshot
}

func withAlias(t *testing.T, backupDir string, snapshot manifest.Snapshot) manifest.Snapshot {
	t.Helper()
	alias := filepath.Join(backupDir, "dsh-backup-20261030-120000-copy.manifest.json")
	raw, err := os.ReadFile(snapshot.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alias, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot.ManifestAliases = []string{alias}
	return snapshot
}

func TestScanIgnoresTrashDirectory(t *testing.T) {
	paths, snapshot := trashFixture(t)
	before, _, err := manifest.Scan(paths.DefaultBackupDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := MoveToTrash(paths.DefaultBackupDir, snapshot, TrashReasonManual, 7, "test"); err != nil {
		t.Fatal(err)
	}
	after, _, err := manifest.Scan(paths.DefaultBackupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)-1 {
		t.Fatalf("trash directory leaked into the archive list: before=%d after=%d", len(before), len(after))
	}
}

func TestMoveToTrashAndRestoreRoundTrip(t *testing.T) {
	paths, snapshot := trashFixture(t)
	snapshot = withAlias(t, paths.DefaultBackupDir, snapshot)
	meta, err := MoveToTrash(paths.DefaultBackupDir, snapshot, TrashReasonManual, 7, "test")
	if err != nil {
		t.Fatal(err)
	}
	if meta.ArchiveMissing {
		t.Fatal("archive should not be marked missing")
	}
	for _, path := range []string{snapshot.LocalPath, snapshot.ManifestPath, snapshot.ManifestAliases[0]} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("file still in the backup directory: %s", path)
		}
	}
	itemDir := filepath.Join(TrashRoot(paths.DefaultBackupDir), snapshot.ID)
	for _, name := range append([]string{meta.ArchiveName}, meta.ManifestNames...) {
		if _, err := os.Stat(filepath.Join(itemDir, name)); err != nil {
			t.Fatalf("file missing from trash: %s: %v", name, err)
		}
	}
	list, err := ListTrash(paths.DefaultBackupDir, true)
	if err != nil {
		t.Fatal(err)
	}
	if list.Count != 1 || !list.Items[0].Restorable {
		t.Fatalf("unexpected trash list: %+v", list)
	}
	if list.Items[0].DaysLeft != 7 {
		t.Fatalf("expected 7 days left, got %d", list.Items[0].DaysLeft)
	}
	if err := RestoreTrashItem(paths.DefaultBackupDir, snapshot.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{snapshot.LocalPath, snapshot.ManifestPath, snapshot.ManifestAliases[0]} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("file not restored: %s: %v", path, err)
		}
	}
	if _, err := os.Stat(itemDir); !os.IsNotExist(err) {
		t.Fatalf("trash entry should be gone: %v", err)
	}
}

func TestMoveToTrashRejectsForeignSnapshot(t *testing.T) {
	paths, snapshot := trashFixture(t)
	outside := filepath.Join(t.TempDir(), "outside.7z")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot.LocalPath = outside
	if _, err := MoveToTrash(paths.DefaultBackupDir, snapshot, TrashReasonManual, 7, "test"); err == nil {
		t.Fatal("foreign archive accepted")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file touched: %v", err)
	}
}

func TestMoveToTrashMissingArchiveOnlyAllowsPermanentDelete(t *testing.T) {
	paths, snapshot := trashFixture(t)
	if err := os.Remove(snapshot.LocalPath); err != nil {
		t.Fatal(err)
	}
	meta, err := MoveToTrash(paths.DefaultBackupDir, snapshot, TrashReasonManual, 7, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !meta.ArchiveMissing {
		t.Fatal("missing archive should be recorded")
	}
	if err := RestoreTrashItem(paths.DefaultBackupDir, snapshot.ID); err == nil {
		t.Fatal("restore of a manifest-only trash entry must fail")
	}
	if err := DeleteTrashItem(paths.DefaultBackupDir, snapshot.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(TrashRoot(paths.DefaultBackupDir), snapshot.ID)); !os.IsNotExist(err) {
		t.Fatal("trash entry should be removed")
	}
}

func TestRestoreTrashRefusesExistingTarget(t *testing.T) {
	paths, snapshot := trashFixture(t)
	if _, err := MoveToTrash(paths.DefaultBackupDir, snapshot, TrashReasonManual, 7, "test"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.DefaultBackupDir, filepath.Base(snapshot.LocalPath)), []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RestoreTrashItem(paths.DefaultBackupDir, snapshot.ID); err == nil {
		t.Fatal("restore over an existing archive must fail")
	}
	itemDir := filepath.Join(TrashRoot(paths.DefaultBackupDir), snapshot.ID)
	if _, err := os.Stat(filepath.Join(itemDir, filepath.Base(snapshot.LocalPath))); err != nil {
		t.Fatalf("trash entry must stay intact: %v", err)
	}
}

func TestRestoreTrashRejectsForeignOrigin(t *testing.T) {
	paths, snapshot := trashFixture(t)
	if _, err := MoveToTrash(paths.DefaultBackupDir, snapshot, TrashReasonManual, 7, "test"); err != nil {
		t.Fatal(err)
	}
	itemDir := filepath.Join(TrashRoot(paths.DefaultBackupDir), snapshot.ID)
	meta, err := readTrashMeta(itemDir)
	if err != nil {
		t.Fatal(err)
	}
	meta.OriginRootID = "deadbeef:0000000000000000"
	meta.OriginBackupDir = `D:\Elsewhere\Backups`
	if err := writeTrashMeta(itemDir, meta); err != nil {
		t.Fatal(err)
	}
	if err := RestoreTrashItem(paths.DefaultBackupDir, snapshot.ID); err == nil {
		t.Fatal("foreign origin accepted")
	}
	list, err := ListTrash(paths.DefaultBackupDir, true)
	if err != nil {
		t.Fatal(err)
	}
	if list.Count != 1 || list.Items[0].BelongsToCurrentRoot || list.Items[0].Restorable {
		t.Fatalf("foreign item should be listed but not restorable: %+v", list.Items)
	}
	if _, err := os.Stat(itemDir); err != nil {
		t.Fatalf("foreign item must not be removed: %v", err)
	}
}

func TestDeleteTrashItemRejectsTraversal(t *testing.T) {
	paths, _ := trashFixture(t)
	outside := filepath.Join(paths.DefaultBackupDir, "keep.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DeleteTrashItem(paths.DefaultBackupDir, "../keep.txt"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file removed: %v", err)
	}
}

func TestEmptyTrashLeavesUnreadableEntries(t *testing.T) {
	paths, snapshot := trashFixture(t)
	if _, err := MoveToTrash(paths.DefaultBackupDir, snapshot, TrashReasonManual, 7, "test"); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(TrashRoot(paths.DefaultBackupDir), "notes")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	removed, failed, err := EmptyTrash(paths.DefaultBackupDir)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("expected one removed entry, got %d", removed)
	}
	if len(failed) != 1 {
		t.Fatalf("expected one skipped entry, got %v", failed)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("unrelated directory must be left alone: %v", err)
	}
}

func TestPurgeExpiredTrashSkipsForeignOrigin(t *testing.T) {
	paths, snapshot := trashFixture(t)
	if _, err := MoveToTrash(paths.DefaultBackupDir, snapshot, TrashReasonManual, 7, "test"); err != nil {
		t.Fatal(err)
	}
	itemDir := filepath.Join(TrashRoot(paths.DefaultBackupDir), snapshot.ID)
	meta, err := readTrashMeta(itemDir)
	if err != nil {
		t.Fatal(err)
	}
	meta.ExpiresAt = time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	meta.OriginRootID = "deadbeef:0000000000000000"
	meta.OriginBackupDir = `D:\Elsewhere\Backups`
	if err := writeTrashMeta(itemDir, meta); err != nil {
		t.Fatal(err)
	}
	removed, skipped, _, err := PurgeExpiredTrash(paths.DefaultBackupDir, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 || skipped != 1 {
		t.Fatalf("foreign item must be skipped: removed=%d skipped=%d", removed, skipped)
	}
	if _, err := os.Stat(itemDir); err != nil {
		t.Fatalf("foreign item must not be purged: %v", err)
	}
	meta.OriginRootID = ""
	meta.OriginBackupDir = paths.DefaultBackupDir
	if err := writeTrashMeta(itemDir, meta); err != nil {
		t.Fatal(err)
	}
	removed, _, _, err = PurgeExpiredTrash(paths.DefaultBackupDir, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("expired owned item should be purged, removed=%d", removed)
	}
}

func TestApplyLocalRetentionMovesToTrash(t *testing.T) {
	paths, first := trashFixture(t)
	second := first
	second.ID = "20261030-130000"
	second.LocalPath = filepath.Join(paths.DefaultBackupDir, manifest.ArchiveName(second.ID))
	second.ManifestPath = manifest.ManifestPath(second.LocalPath)
	if err := os.WriteFile(second.LocalPath, []byte("second-archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	value := manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		ID:            second.ID,
		CreatedAt:     "2026-10-30T13:00:00Z",
		Kind:          manifest.KindManual,
		Archive: manifest.ArchiveInfo{
			Name:   manifest.ArchiveName(second.ID),
			Size:   int64(len("second-archive")),
			SHA256: strings.Repeat("b", 64),
		},
	}
	if err := manifest.Write(second.ManifestPath, value); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(paths, nil, nil, "", "", "test")
	moved, err := manager.applyLocalRetention(context.Background(), paths.DefaultBackupDir, 1, second.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 || moved[0] != first.ID {
		t.Fatalf("unexpected retention result: %v", moved)
	}
	if _, err := os.Stat(second.LocalPath); err != nil {
		t.Fatalf("protected snapshot should stay: %v", err)
	}
	list, err := ListTrash(paths.DefaultBackupDir, true)
	if err != nil {
		t.Fatal(err)
	}
	if list.Count != 1 || list.Items[0].SnapshotID != first.ID || list.Items[0].Reason != TrashReasonRetention {
		t.Fatalf("retention did not move the old archive into the trash: %+v", list.Items)
	}
}

func TestPurgeCanBeDisabled(t *testing.T) {
	paths, first := trashFixture(t)
	if _, err := MoveToTrash(paths.DefaultBackupDir, first, TrashReasonManual, 7, "test"); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID = "20261030-150000"
	second.LocalPath = filepath.Join(paths.DefaultBackupDir, manifest.ArchiveName(second.ID))
	second.ManifestPath = manifest.ManifestPath(second.LocalPath)
	if err := os.WriteFile(second.LocalPath, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	value := manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		ID:            second.ID,
		CreatedAt:     "2026-10-30T15:00:00Z",
		Kind:          manifest.KindManual,
		Archive: manifest.ArchiveInfo{
			Name:   manifest.ArchiveName(second.ID),
			Size:   int64(len("second")),
			SHA256: strings.Repeat("d", 64),
		},
	}
	if err := manifest.Write(second.ManifestPath, value); err != nil {
		t.Fatal(err)
	}
	if _, err := MoveToTrash(paths.DefaultBackupDir, second, TrashReasonRetention, 7, "test"); err != nil {
		t.Fatal(err)
	}
	root := TrashRoot(paths.DefaultBackupDir)
	for _, id := range []string{first.ID, second.ID} {
		itemDir := filepath.Join(root, id)
		meta, err := readTrashMeta(itemDir)
		if err != nil {
			t.Fatal(err)
		}
		meta.ExpiresAt = time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
		if err := writeTrashMeta(itemDir, meta); err != nil {
			t.Fatal(err)
		}
	}

	list, err := ListTrash(paths.DefaultBackupDir, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list.Items {
		if !item.NeverExpires {
			t.Fatalf("item %s should be marked as never expiring", item.SnapshotID)
		}
	}
	removed, _, _, err := PurgeExpiredTrash(paths.DefaultBackupDir, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatalf("automatic cleaning is off but %d items were removed", removed)
	}
	for _, id := range []string{first.ID, second.ID} {
		if _, err := os.Stat(filepath.Join(root, id)); err != nil {
			t.Fatalf("trash item %s must survive when auto-clean is off: %v", id, err)
		}
	}

	removed, _, _, err = PurgeExpiredTrash(paths.DefaultBackupDir, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("auto-clean should remove both expired items, removed=%d", removed)
	}
}
