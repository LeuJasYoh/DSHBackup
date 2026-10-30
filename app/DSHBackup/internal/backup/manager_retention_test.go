//go:build windows

package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"DSHBackup/internal/manifest"
)

func retentionSnapshot(t *testing.T, dir, id string) manifest.Snapshot {
	t.Helper()
	archive := filepath.Join(dir, manifest.ArchiveName(id))
	if err := os.WriteFile(archive, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	value := manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		ID:            id,
		CreatedAt:     "2026-10-30T" + id[9:11] + ":" + id[11:13] + ":" + id[13:15] + "Z",
		Kind:          manifest.KindManual,
		Archive: manifest.ArchiveInfo{
			Name:   manifest.ArchiveName(id),
			Size:   int64(len("archive")),
			SHA256: strings.Repeat("a", 64),
		},
	}
	if err := manifest.Write(manifest.ManifestPath(archive), value); err != nil {
		t.Fatal(err)
	}
	return manifest.FromManifest(value, archive, manifest.ManifestPath(archive))
}

func TestRetentionMergesDuplicateManifestAliases(t *testing.T) {
	dir := t.TempDir()
	old := retentionSnapshot(t, dir, "20261030-010000")
	current := retentionSnapshot(t, dir, "20261030-020000")
	raw, err := os.ReadFile(old.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "copy.manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{}
	deleted, err := manager.applyLocalRetention(context.Background(), dir, 2, current.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 {
		t.Fatalf("duplicate alias consumed a retention slot: %v", deleted)
	}
	if _, err := os.Stat(old.LocalPath); err != nil {
		t.Fatalf("retained archive was deleted: %v", err)
	}
}

func TestRetentionKeepsManifestWhenArchiveDeleteFails(t *testing.T) {
	dir := t.TempDir()
	old := retentionSnapshot(t, dir, "20261030-010000")
	current := retentionSnapshot(t, dir, "20261030-020000")
	pointer, _ := windows.UTF16PtrFromString(old.LocalPath)
	handle, err := windows.CreateFile(pointer, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	manager := &Manager{}
	deleted, retentionErr := manager.applyLocalRetention(context.Background(), dir, 1, current.ID, 7)
	if retentionErr == nil {
		t.Fatal("locked archive deletion was reported as successful")
	}
	if _, err := os.Stat(old.ManifestPath); err != nil {
		t.Fatalf("manifest was removed while archive deletion failed: %v (deleted=%v)", err, deleted)
	}
}

func TestRetentionDoesNotCountPartialBackups(t *testing.T) {
	dir := t.TempDir()
	complete := retentionSnapshot(t, dir, "20261030-010000")
	partialID := "20261030-020000"
	partialArchive := filepath.Join(dir, manifest.ArchiveName(partialID))
	if err := os.WriteFile(partialArchive, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	value := manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		ID:            partialID,
		CreatedAt:     "2026-10-30T02:00:00Z",
		Kind:          manifest.KindManual,
		CaptureStatus: "partial",
		Archive: manifest.ArchiveInfo{
			Name:   manifest.ArchiveName(partialID),
			Size:   int64(len("partial")),
			SHA256: strings.Repeat("b", 64),
		},
	}
	if err := manifest.Write(manifest.ManifestPath(partialArchive), value); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{}
	deleted, err := manager.applyLocalRetention(context.Background(), dir, 1, partialID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 {
		t.Fatalf("partial backup displaced a complete backup: %v", deleted)
	}
	if _, err := os.Stat(complete.LocalPath); err != nil {
		t.Fatalf("complete backup was deleted: %v", err)
	}
}
