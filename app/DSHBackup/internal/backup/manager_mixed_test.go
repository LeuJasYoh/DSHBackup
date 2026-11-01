//go:build windows

package backup_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"DSHBackup/internal/backup"
	"DSHBackup/internal/manifest"
	"DSHBackup/internal/scope"
)

// TestMixedLegacyAndDomainSnapshots 覆盖真实升级场景：备份目录里同时存在
// 1.3.x 的单归档快照和 1.4.0 的分域快照，两者都要能列出、校验和恢复。
func TestMixedLegacyAndDomainSnapshots(t *testing.T) {
	client, paths, manager := setupDomainsFixture(t)
	cfg := fixtureConfig(paths)

	// 手工造一份 v1 快照（单归档、无域字段）。
	legacyID := "20261030-201210"
	legacyArchive := filepath.Join(cfg.BackupDir, manifest.ArchiveName(legacyID))
	if err := os.WriteFile(legacyArchive, []byte("legacy-archive-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := manifest.Manifest{
		SchemaVersion: 1,
		ID:            legacyID,
		CreatedAt:     "2026-10-30T20:12:10+08:00",
		Kind:          manifest.KindManual,
		AppVersion:    "1.3.1",
		Scope:         []string{"dsh"},
		Files:         manifest.FileStats{Files: 488, Bytes: 120307482},
		Archive: manifest.ArchiveInfo{
			Name:   manifest.ArchiveName(legacyID),
			Size:   int64(len("legacy-archive-bytes")),
			SHA256: strings.Repeat("c", 64),
		},
		Compressor:    manifest.CompressorInfo{Name: "7-Zip", Version: "test", Args: []string{"-mx=0"}},
		CaptureStatus: "complete",
	}
	if err := manifest.Write(manifest.ManifestPath(legacyArchive), legacy); err != nil {
		t.Fatalf("写入 v1 清单失败：%v", err)
	}

	current, err := manager.Backup(context.Background(), backup.BackupInput{
		TaskID: "mixed", Config: cfg, Kind: manifest.KindManual, Force: true,
	})
	if err != nil {
		t.Fatalf("v2 备份失败：%v", err)
	}

	listed, warnings, err := manager.ListLocal(cfg.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) > 0 {
		t.Fatalf("混合目录不该有告警：%v", warnings)
	}
	if len(listed) != 2 {
		t.Fatalf("应列出 v1 与 v2 两份快照，实际 %d：%+v", len(listed), listed)
	}
	var legacyFound, currentFound bool
	for _, snapshot := range listed {
		switch snapshot.ID {
		case legacyID:
			legacyFound = true
			if len(snapshot.Parts) != 0 {
				t.Fatalf("v1 快照不该有域列表：%+v", snapshot.Parts)
			}
			if snapshot.Status != "ready" || snapshot.Size != int64(len("legacy-archive-bytes")) {
				t.Fatalf("v1 快照状态不符：%+v", snapshot)
			}
		case current.Snapshot.ID:
			currentFound = true
			if len(snapshot.Parts) != len(current.Snapshot.Parts) {
				t.Fatalf("v2 快照域份数不符：%+v", snapshot.Parts)
			}
		}
	}
	if !legacyFound || !currentFound {
		t.Fatalf("混合列表缺项：legacy=%v current=%v", legacyFound, currentFound)
	}

	// 手动删除整份 v2 快照：所有域归档与清单必须一起进回收站。
	deleted, err := backup.MoveToTrash(cfg.BackupDir, current.Snapshot, backup.TrashReasonManual, 7, "test")
	if err != nil {
		t.Fatalf("整份移入回收站失败：%v", err)
	}
	if len(deleted.Parts) != len(current.Snapshot.Parts) {
		t.Fatalf("回收站记录的分份数不符：%+v", deleted.Parts)
	}
	for _, path := range current.ArchivePaths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("归档仍在备份目录：%s", path)
		}
	}
	remaining, _, err := manager.ListLocal(cfg.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != legacyID {
		t.Fatalf("删除 v2 后应只剩 v1：%+v", remaining)
	}
	trash, err := backup.ListTrash(cfg.BackupDir, true)
	if err != nil {
		t.Fatal(err)
	}
	if trash.Count != 1 || trash.Items[0].Domain != "" || len(trash.Items[0].Parts) != len(current.Snapshot.Parts) {
		t.Fatalf("回收站列表未按整份快照记录：%+v", trash.Items)
	}
	if !trash.Items[0].Restorable {
		t.Fatalf("整份条目应可找回：%s", trash.Items[0].BlockedReason)
	}
	if err := backup.RestoreTrashItem(cfg.BackupDir, current.Snapshot.ID); err != nil {
		t.Fatalf("找回整份快照失败：%v", err)
	}
	restored, _, err := manager.ListLocal(cfg.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 2 {
		t.Fatalf("找回后应重新出现两份快照：%+v", restored)
	}
	for _, path := range current.ArchivePaths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("找回后缺少归档 %s：%v", path, err)
		}
	}
	_ = client
}

// TestRetentionRespectsDisabledDomain 关掉某个域后，旧快照里该域的归档
// 应被按域淘汰，而不是永远留在目录里。
func TestRetentionRespectsDisabledDomain(t *testing.T) {
	_, paths, manager := setupDomainsFixture(t)
	cfg := fixtureConfig(paths)
	cfg.Domains.IncludeRuntime = true
	first, err := manager.Backup(context.Background(), backup.BackupInput{
		TaskID: "runtime-on-1", Config: cfg, Kind: manifest.KindManual, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	runtimeArchive := filepath.Join(cfg.BackupDir, manifest.DomainArchiveName(first.Snapshot.ID, string(scope.DomainRuntime)))
	if _, err := os.Stat(runtimeArchive); err != nil {
		t.Fatalf("打开运行环境后应生成该域归档：%v", err)
	}
	// 第二份运行环境归档：让该域超出保留数，才能验证淘汰只看本域。
	time.Sleep(1100 * time.Millisecond)
	secondWithRuntime, err := manager.Backup(context.Background(), backup.BackupInput{
		TaskID: "runtime-on-2", Config: cfg, Kind: manifest.KindManual, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	newerArchive := filepath.Join(cfg.BackupDir, manifest.DomainArchiveName(secondWithRuntime.Snapshot.ID, string(scope.DomainRuntime)))
	if _, err := os.Stat(newerArchive); err != nil {
		t.Fatalf("第二份运行环境归档缺失：%v", err)
	}

	// 关掉该域后不再新增，历史份数仍按本域保留数淘汰，不牵连其他域。
	cfg.Retention.Runtime = 1
	cfg.Domains.IncludeRuntime = false
	third, err := manager.Backup(context.Background(), backup.BackupInput{
		TaskID: "runtime-off", Config: cfg, Kind: manifest.KindManual, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(runtimeArchive); !os.IsNotExist(err) {
		t.Fatalf("关闭运行环境后，旧的运行环境归档应被淘汰：%v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.BackupDir, manifest.DomainArchiveName(third.Snapshot.ID, string(scope.DomainRuntime)))); err == nil {
		t.Fatal("关闭运行环境后不该再生成该域归档")
	}
	if _, err := os.Stat(newerArchive); err != nil {
		t.Fatalf("该域最新的一份应留着：%v", err)
	}
	// 其余域不受影响。
	if _, err := os.Stat(filepath.Join(cfg.BackupDir, manifest.DomainArchiveName(first.Snapshot.ID, string(scope.DomainSessions)))); err != nil {
		t.Fatalf("会话域不该被牵连：%v", err)
	}
}

// TestBackupProgressStaysMonotonic 防止分域后进度百分比回退或越界。
func TestBackupProgressStaysMonotonic(t *testing.T) {
	_, paths, manager := setupDomainsFixture(t)
	cfg := fixtureConfig(paths)
	var seen []float64
	if _, err := manager.Backup(context.Background(), backup.BackupInput{
		TaskID: "progress", Config: cfg, Kind: manifest.KindScheduled, Force: true,
		OnProgress: func(progress backup.Progress) {
			seen = append(seen, progress.Percent)
		},
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) < 4 {
		t.Fatalf("进度事件太少：%v", seen)
	}
	previous := -1.0
	for _, percent := range seen {
		if percent < 0 || percent > 100 {
			t.Fatalf("进度越界：%v（全部 %v）", percent, seen)
		}
		// 多份归档之间不允许进度倒退，否则界面进度条会来回跳。
		if percent < previous-1e-9 { // 允许浮点末位噪声
			t.Fatalf("进度回退：%.1f → %.1f（全部 %v）", previous, percent, seen)
		}
		previous = percent
	}
	if seen[len(seen)-1] != 100 {
		t.Fatalf("最后一次进度应为 100，实际 %v", seen[len(seen)-1])
	}
}

// TestSecondBackupDoesNotEvictReferencedDomains 连续两次备份后，
// 每个域各自按份数保留，不能出现"某域的清单还在、归档被别域的淘汰带走"。
func TestSecondBackupDoesNotEvictReferencedDomains(t *testing.T) {
	_, paths, manager := setupDomainsFixture(t)
	cfg := fixtureConfig(paths)
	cfg.Retention.Sessions = 1
	cfg.Retention.Core = 5
	cfg.Retention.Plugins = 5
	var ids []string
	for index := 0; index < 3; index++ {
		time.Sleep(1100 * time.Millisecond)
		result, err := manager.Backup(context.Background(), backup.BackupInput{
			TaskID: "chain-" + string(rune('a'+index)), Config: cfg, Kind: manifest.KindManual, Force: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, result.Snapshot.ID)
	}
	for index, id := range ids {
		coreExists := fileExists(filepath.Join(cfg.BackupDir, manifest.DomainArchiveName(id, string(scope.DomainCore))))
		sessionsExists := fileExists(filepath.Join(cfg.BackupDir, manifest.DomainArchiveName(id, string(scope.DomainSessions))))
		if index < len(ids)-1 && sessionsExists {
			t.Fatalf("第 %d 次备份的会话域应已被淘汰（保留 1 份）", index+1)
		}
		if !coreExists {
			t.Fatalf("第 %d 次备份的身份域不该被牵连淘汰", index+1)
		}
		if _, err := os.Stat(manifest.ManifestPath(filepath.Join(cfg.BackupDir, manifest.DomainArchiveName(id, string(scope.DomainCore))))); err != nil {
			t.Fatalf("身份域清单与归档必须同生同灭：%v", err)
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
