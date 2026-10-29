package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"DSHBackup/internal/backup"
	"DSHBackup/internal/manifest"
	"DSHBackup/internal/platform"
	"DSHBackup/internal/sidecar"
)

func (a *App) StartBackup(force bool) (string, error) {
	return a.startBackup(force, manifest.KindManual)
}

func (a *App) startBackup(force bool, kind string) (string, error) {
	taskID, ctx, err := a.beginTask("backup")
	if err != nil {
		return "", err
	}
	started := false
	defer func() {
		if !started {
			a.finishTask(taskID)
		}
	}()
	cfg := a.configSnapshot()
	if !cfg.OnboardingComplete {
		return "", errors.New("请先完成首次设置")
	}
	if a.sidecar == nil {
		return "", errors.New(a.sidecarError)
	}
	if !a.sevenZipOK {
		return "", errors.New(a.sevenZipError)
	}
	if !force {
		running, evidence, err := a.checkDSHRunningForTask()
		if err != nil {
			return "", err
		}
		if running {
			return "", fmt.Errorf("%s%s", runningPrefix, strings.Join(evidence, "；"))
		}
	}
	if cfg.EncryptionLocal {
		keyCtx, keyCancel := context.WithTimeout(ctx, 40*time.Second)
		keyErr := a.ensureEncryptionReady(keyCtx, cfg, true)
		keyCancel()
		if keyErr != nil {
			return "", keyErr
		}
	}
	manager := backup.NewManager(a.paths, a.sidecar, a.logger, a.sevenZipPath, a.sevenZipVersion, Version)
	input := backup.BackupInput{
		TaskID: taskID,
		Config: cfg,
		Kind:   kind,
		Force:  force,
		OnProgress: func(progress backup.Progress) {
			a.emitProgress(TaskProgress{
				TaskID:     taskID,
				Kind:       "backup",
				Phase:      progress.Phase,
				Percent:    progress.Percent,
				Message:    progress.Message,
				BytesDone:  progress.BytesDone,
				BytesTotal: progress.BytesTotal,
			})
		},
	}
	started = true
	go func() {
		result, err := manager.Backup(ctx, input)
		taskResult := TaskResult{TaskID: taskID, Kind: "backup"}
		if err != nil {
			taskResult.Success = false
			taskResult.Error = a.taskErrorMessage(err)
			taskResult.Message = "备份失败"
			if backup.IsRunning(err) {
				taskResult.Error = a.runningError(err).Error()
			}
		} else {
			snapshot := result.Snapshot
			taskResult.Success = true
			taskResult.Message = "备份完成"
			taskResult.Snapshot = &snapshot
			taskResult.Warnings = result.Warnings
		}
		a.finishTask(taskID)
		a.emitTaskResult(taskResult)
	}()
	return taskID, nil
}

func (a *App) StartRestore(request RestoreRequest) (string, error) {
	taskID, ctx, err := a.beginTask("restore")
	if err != nil {
		return "", err
	}
	started := false
	defer func() {
		if !started {
			a.finishTask(taskID)
		}
	}()
	cfg := a.configSnapshot()
	if !cfg.OnboardingComplete {
		return "", errors.New("请先完成首次设置")
	}
	if a.sidecar == nil {
		return "", errors.New(a.sidecarError)
	}
	if !a.sevenZipOK {
		return "", errors.New(a.sevenZipError)
	}
	snapshot, err := a.resolveSnapshot(request.SnapshotID)
	if err != nil {
		return "", err
	}
	if !request.Force {
		running, evidence, err := a.checkDSHRunningForTask()
		if err != nil {
			return "", err
		}
		if running {
			return "", fmt.Errorf("%s%s", runningPrefix, strings.Join(evidence, "；"))
		}
	}
	if request.PreRestoreSnapshot && cfg.EncryptionLocal {
		keyCtx, keyCancel := context.WithTimeout(ctx, 40*time.Second)
		keyErr := a.ensureEncryptionReady(keyCtx, cfg, true)
		keyCancel()
		if keyErr != nil {
			return "", keyErr
		}
	}
	manager := backup.NewManager(a.paths, a.sidecar, a.logger, a.sevenZipPath, a.sevenZipVersion, Version)
	input := backup.RestoreInput{
		TaskID:             taskID,
		Config:             cfg,
		Snapshot:           snapshot,
		IncludeDesktop:     request.IncludeDesktop,
		PreRestoreSnapshot: request.PreRestoreSnapshot,
		TargetDSH:          request.CustomTarget,
		Force:              request.Force,
		OnProgress: func(progress backup.Progress) {
			a.emitProgress(TaskProgress{
				TaskID:     taskID,
				Kind:       "restore",
				Phase:      progress.Phase,
				Percent:    progress.Percent,
				Message:    progress.Message,
				BytesDone:  progress.BytesDone,
				BytesTotal: progress.BytesTotal,
			})
		},
	}
	started = true
	go func() {
		result, err := manager.Restore(ctx, input)
		taskResult := TaskResult{TaskID: taskID, Kind: "restore"}
		if err != nil {
			taskResult.Success = false
			taskResult.Error = a.taskErrorMessage(err)
			taskResult.Message = "恢复失败"
			if backup.IsRunning(err) {
				taskResult.Error = a.runningError(err).Error()
			}
		} else {
			taskResult.Success = true
			taskResult.Message = "恢复完成"
			taskResult.Warnings = result.Warnings
			taskResult.PreRestoreSnapshot = result.PreRestoreSnapshot
			taskResult.Restore = &RestoreOutcome{
				Files:           result.Files,
				Bytes:           result.Bytes,
				DesktopRestored: result.DesktopRestored,
				TargetDSH:       result.TargetDSH,
			}
		}
		a.finishTask(taskID)
		a.emitTaskResult(taskResult)
	}()
	return taskID, nil
}

func (a *App) StartVerify(request VerifyRequest) (string, error) {
	taskID, ctx, err := a.beginTask("verify")
	if err != nil {
		return "", err
	}
	started := false
	defer func() {
		if !started {
			a.finishTask(taskID)
		}
	}()
	if a.sidecar == nil {
		return "", errors.New(a.sidecarError)
	}
	if !a.sevenZipOK {
		return "", errors.New(a.sevenZipError)
	}
	snapshot, err := a.resolveSnapshot(request.SnapshotID)
	if err != nil {
		return "", err
	}
	manager := backup.NewManager(a.paths, a.sidecar, a.logger, a.sevenZipPath, a.sevenZipVersion, Version)
	onProgress := func(progress backup.Progress) {
		a.emitProgress(TaskProgress{
			TaskID:     taskID,
			Kind:       "verify",
			Phase:      progress.Phase,
			Percent:    progress.Percent,
			Message:    progress.Message,
			BytesDone:  progress.BytesDone,
			BytesTotal: progress.BytesTotal,
		})
	}
	started = true
	go func() {
		verifyErr := manager.VerifyLocal(ctx, taskID, snapshot, onProgress)
		taskResult := TaskResult{TaskID: taskID, Kind: "verify"}
		if verifyErr != nil {
			taskResult.Success = false
			taskResult.Message = "验证失败"
			taskResult.Error = a.taskErrorMessage(verifyErr)
		} else {
			taskResult.Success = true
			taskResult.Message = "验证通过：归档完整且可解密"
		}
		a.finishTask(taskID)
		a.emitTaskResult(taskResult)
	}()
	return taskID, nil
}

func (a *App) DeleteLocalSnapshot(id string) error {
	taskID, ctx, err := a.beginTask("delete")
	if err != nil {
		return err
	}
	defer a.finishTask(taskID)
	cfg := a.configSnapshot()
	snapshots, _, err := manifest.Scan(cfg.BackupDir)
	if err != nil {
		return err
	}
	var target *manifest.Snapshot
	for index := range snapshots {
		if snapshots[index].ID == id {
			target = &snapshots[index]
			break
		}
	}
	if target == nil {
		return errors.New("找不到本地快照")
	}
	if a.sidecar == nil {
		return errors.New(a.sidecarError)
	}
	manager := backup.NewManager(a.paths, a.sidecar, a.logger, a.sevenZipPath, a.sevenZipVersion, Version)
	if err := manager.DeleteLocal(ctx, *target, cfg.BackupDir); err != nil {
		return err
	}
	if a.logger != nil {
		a.logger.Info("snapshot.delete", "删除本地快照", map[string]any{"id": id})
	}
	a.finishTask(taskID)
	a.emitStateChanged("snapshot-delete")
	return nil
}

func (a *App) resolveSnapshot(id string) (manifest.Snapshot, error) {
	if strings.TrimSpace(id) == "" {
		return manifest.Snapshot{}, errors.New("快照 ID 不能为空")
	}
	local, _, err := manifest.Scan(a.configSnapshot().BackupDir)
	if err != nil {
		return manifest.Snapshot{}, fmt.Errorf("读取本地快照失败：%w", err)
	}
	for _, snapshot := range local {
		if snapshot.ID == id {
			if snapshot.Status != "ready" {
				return manifest.Snapshot{}, errors.New("本地快照文件缺失或大小不符，无法恢复或验证")
			}
			return snapshot, nil
		}
	}
	return manifest.Snapshot{}, errors.New("找不到本地快照")
}

func (a *App) checkDSHRunningForTask() (bool, []string, error) {
	return platform.IsDSHRunning(a.paths.DSHDataDir())
}

func (a *App) taskErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) || sidecar.IsCancelled(err) {
		return "任务已取消"
	}
	if backup.IsRunning(err) {
		return a.runningError(err).Error()
	}
	return err.Error()
}
