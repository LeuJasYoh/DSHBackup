//go:build windows

package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"DSHBackup/internal/config"
	"DSHBackup/internal/manifest"
	"DSHBackup/internal/scope"
)

// uniformRetention 让四个域用同一个份数，便于沿用旧的"全局保留 N 份"断言。
func uniformRetention(keep int) config.Retention {
	return config.Retention{Local: keep, TrashDays: 7, Core: keep, Sessions: keep, Plugins: keep, Runtime: keep}
}

// v2Snapshot 写出一份按域拆开的快照：每个域一份归档加一份清单。
func v2Snapshot(t *testing.T, dir, id string, domains ...scope.Domain) manifest.Snapshot {
	t.Helper()
	var values []manifest.Manifest
	for _, domain := range domains {
		archive := filepath.Join(dir, manifest.DomainArchiveName(id, string(domain)))
		if err := os.WriteFile(archive, []byte("archive"), 0o600); err != nil {
			t.Fatal(err)
		}
		value := manifest.Manifest{
			SchemaVersion: manifest.SchemaVersion,
			ID:            id,
			Domain:        string(domain),
			CreatedAt:     "2026-10-30T" + id[9:11] + ":" + id[11:13] + ":" + id[13:15] + "Z",
			Kind:          manifest.KindManual,
			Files:         manifest.FileStats{Files: 3, Bytes: 7},
			Archive: manifest.ArchiveInfo{
				Name:   filepath.Base(archive),
				Size:   int64(len("archive")),
				SHA256: strings.Repeat("a", 64),
			},
		}
		if err := manifest.Write(manifest.ManifestPath(archive), value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	snapshot, warnings := manifest.SnapshotFromManifests(values, dir)
	if len(warnings) > 0 {
		t.Fatalf("聚合快照清单失败：%v", warnings)
	}
	return snapshot
}

func TestRetentionEvictsOnlyTheDomainThatOverflowed(t *testing.T) {
	dir := t.TempDir()
	first := v2Snapshot(t, dir, "20261030-010000", scope.DomainCore, scope.DomainSessions, scope.DomainPlugins)
	second := v2Snapshot(t, dir, "20261030-020000", scope.DomainCore, scope.DomainSessions, scope.DomainPlugins)
	retention := config.Retention{TrashDays: 7, Core: 5, Sessions: 5, Plugins: 1, Runtime: 1}

	manager := &Manager{}
	moved, err := manager.applyDomainRetention(context.Background(), dir, retention, second.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 || !strings.HasPrefix(moved[0], first.ID) || !strings.Contains(moved[0], "插件依赖") {
		t.Fatalf("应只淘汰最旧的那一份插件依赖：%v", moved)
	}
	// 会话与身份域还在保留数内，一份都不该被动。
	for _, domain := range []scope.Domain{scope.DomainCore, scope.DomainSessions} {
		path := filepath.Join(dir, manifest.DomainArchiveName(first.ID, string(domain)))
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s 不该被淘汰：%v", domain, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, manifest.DomainArchiveName(first.ID, string(scope.DomainPlugins)))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("插件依赖归档仍留在备份目录：%v", err)
	}
	// 整份快照仍然可恢复：只少了被淘汰的那个域。
	snapshots, _, err := manifest.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 被淘汰的域不再出现在快照里：恢复时界面不会再提供它，其余域照旧可恢复。
	if len(snapshots) != 2 {
		t.Fatalf("应列出两份快照，实际 %d", len(snapshots))
	}
	for _, snapshot := range snapshots {
		for _, part := range snapshot.Parts {
			if part.Domain == string(scope.DomainPlugins) && snapshot.ID == first.ID {
				t.Fatalf("被淘汰的域仍挂在旧快照上：%+v", snapshot.Parts)
			}
			if part.Status != "ready" {
				t.Fatalf("保留下来的份数都应就绪：%+v", snapshot.Parts)
			}
		}
	}
	if len(snapshots[1].Parts) != 2 {
		t.Fatalf("旧快照应剩身份与会话两份：%+v", snapshots[1].Parts)
	}
	list, err := ListTrash(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if list.Count != 1 || list.Items[0].SnapshotID != first.ID || list.Items[0].Domain != string(scope.DomainPlugins) {
		t.Fatalf("回收站条目应按域记录：%+v", list.Items)
	}
	if list.Items[0].Parts == nil || len(list.Items[0].Parts) != 1 {
		t.Fatalf("回收站条目缺少分份信息：%+v", list.Items[0])
	}
}

func retentionSnapshot(t *testing.T, dir, id string) manifest.Snapshot {
	t.Helper()
	archive := filepath.Join(dir, manifest.ArchiveName(id))
	if err := os.WriteFile(archive, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	value := manifest.Manifest{
		SchemaVersion: 1,
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
	deleted, err := manager.applyDomainRetention(context.Background(), dir, uniformRetention(2), current.ID, 7)
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
	deleted, retentionErr := manager.applyDomainRetention(context.Background(), dir, uniformRetention(1), current.ID, 7)
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
		SchemaVersion: 1,
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
	deleted, err := manager.applyDomainRetention(context.Background(), dir, uniformRetention(1), partialID, 7)
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
