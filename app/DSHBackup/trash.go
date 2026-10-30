package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"DSHBackup/internal/backup"
	"DSHBackup/internal/manifest"
	"DSHBackup/internal/pathguard"
)

type TrashEmptyResult struct {
	Removed int      `json:"removed"`
	Failed  []string `json:"failed"`
}

// MoveToTrash only renames files inside the backup directory. It deliberately
// does not take the single task slot: deleting must stay instant and must not
// block (or be blocked by) a running backup or restore.
func (a *App) MoveToTrash(request MoveToTrashRequest) error {
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := a.awaitReady(waitCtx)
	waitCancel()
	if err != nil {
		return errors.New("应用仍在初始化，请稍后重试")
	}
	if strings.TrimSpace(request.SnapshotID) == "" {
		return errors.New("快照 ID 不能为空")
	}
	a.trashMu.Lock()
	defer a.trashMu.Unlock()
	cfg := a.configSnapshot()
	identity, err := pathguard.Identity(cfg.BackupDir)
	if err != nil {
		return fmt.Errorf("无法确认当前备份目录身份：%w", err)
	}
	if request.ExpectedRootIdentity == "" || request.ExpectedRootIdentity != identity {
		return errors.New("备份目录在选择后已改变，已拒绝删除；请刷新归档列表后重试")
	}
	if request.ExpectedBackupDir != "" && !strings.EqualFold(filepath.Clean(request.ExpectedBackupDir), filepath.Clean(cfg.BackupDir)) {
		return errors.New("备份目录已更改，已拒绝删除；请刷新归档列表后重试")
	}
	snapshots, _, err := manifest.Scan(cfg.BackupDir)
	if err != nil {
		return err
	}
	var target *manifest.Snapshot
	for index := range snapshots {
		if snapshots[index].ID == request.SnapshotID {
			target = &snapshots[index]
			break
		}
	}
	if target == nil {
		return errors.New("找不到本地快照；请刷新归档列表")
	}
	if request.ExpectedSHA256 != "" && !strings.EqualFold(request.ExpectedSHA256, target.SHA256) {
		return errors.New("归档在删除前发生变化，已拒绝删除；请刷新归档列表")
	}
	if target.Status == "conflict" {
		return errors.New("该归档存在冲突的清单，请先人工处理重复清单后再删除")
	}
	if _, err := backup.MoveToTrash(cfg.BackupDir, *target, backup.TrashReasonManual, cfg.Retention.TrashDays, Version); err != nil {
		return err
	}
	if a.logger != nil {
		a.logger.Info("snapshot.trash", "归档移入回收站", map[string]any{"id": request.SnapshotID})
	}
	a.emitStateChanged("snapshot-delete")
	return nil
}

func (a *App) ListTrash() (backup.TrashList, error) {
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
	err := a.awaitReady(waitCtx)
	waitCancel()
	if err != nil {
		return backup.TrashList{}, errors.New("应用仍在初始化，请稍后重试")
	}
	cfg := a.configSnapshot()
	a.trashMu.Lock()
	defer a.trashMu.Unlock()
	return backup.TrashState(cfg.BackupDir)
}

func (a *App) RestoreFromTrash(snapshotID string) error {
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
	err := a.awaitReady(waitCtx)
	waitCancel()
	if err != nil {
		return errors.New("应用仍在初始化，请稍后重试")
	}
	a.trashMu.Lock()
	defer a.trashMu.Unlock()
	cfg := a.configSnapshot()
	if err := backup.RestoreTrashItem(cfg.BackupDir, snapshotID); err != nil {
		return err
	}
	if a.logger != nil {
		a.logger.Info("snapshot.untrash", "从回收站恢复归档", map[string]any{"id": snapshotID})
	}
	a.emitStateChanged("trash-restore")
	return nil
}

func (a *App) DeleteTrashItem(snapshotID string) error {
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
	err := a.awaitReady(waitCtx)
	waitCancel()
	if err != nil {
		return errors.New("应用仍在初始化，请稍后重试")
	}
	a.trashMu.Lock()
	defer a.trashMu.Unlock()
	cfg := a.configSnapshot()
	if err := backup.DeleteTrashItem(cfg.BackupDir, snapshotID); err != nil {
		return err
	}
	if a.logger != nil {
		a.logger.Info("snapshot.trash-delete", "彻底删除回收站条目", map[string]any{"id": snapshotID})
	}
	a.emitStateChanged("trash-delete")
	return nil
}

func (a *App) EmptyTrash() (TrashEmptyResult, error) {
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
	err := a.awaitReady(waitCtx)
	waitCancel()
	if err != nil {
		return TrashEmptyResult{}, errors.New("应用仍在初始化，请稍后重试")
	}
	a.trashMu.Lock()
	defer a.trashMu.Unlock()
	cfg := a.configSnapshot()
	removed, failed, err := backup.EmptyTrash(cfg.BackupDir)
	if err != nil {
		return TrashEmptyResult{}, err
	}
	if a.logger != nil {
		a.logger.Info("snapshot.trash-empty", "清空回收站", map[string]any{"removed": removed, "failed": len(failed)})
	}
	a.emitStateChanged("trash-empty")
	return TrashEmptyResult{Removed: removed, Failed: failed}, nil
}

func (a *App) OpenTrashFolder() error {
	cfg := a.configSnapshot()
	root := backup.TrashRoot(cfg.BackupDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	return openInExplorer(root)
}

func (a *App) purgeTrashOnStartup() {
	cfg := a.configSnapshot()
	a.trashMu.Lock()
	removed, skipped, warnings, err := backup.PurgeExpiredTrash(cfg.BackupDir, time.Now())
	a.trashMu.Unlock()
	if a.logger != nil {
		a.logger.Info("trash.purge", "清理过期回收站内容", map[string]any{"removed": removed, "skipped": skipped, "warnings": len(warnings), "error": errorText(err)})
	}
	if removed > 0 {
		a.emitStateChanged("trash-purge")
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// warnLeftoverTrash reports, but never touches, a trash left behind in a
// retired backup directory. Automatic cleanup only runs where the current
// configuration points.
func (a *App) warnLeftoverTrash(previousDir string) {
	root := backup.TrashRoot(previousDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			count++
		}
	}
	if count == 0 {
		return
	}
	a.emitNotice("warning", fmt.Sprintf("上次使用的备份目录 %s 里还有 %d 项回收站内容。本应用只管理当前备份目录的回收站，不会自动删除它们。", previousDir, count))
}
