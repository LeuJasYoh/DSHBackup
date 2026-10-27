package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"DSHBackup/internal/backup"
	"DSHBackup/internal/config"
	"DSHBackup/internal/github"
	"DSHBackup/internal/manifest"
	"DSHBackup/internal/platform"
	"DSHBackup/internal/sidecar"
)

type appCloud struct {
	app    *App
	client *github.Client
	owner  string
	repo   string
}

func (c *appCloud) UploadBackup(ctx context.Context, value manifest.Manifest, archivePath string, progress func(done, total int64)) (backup.CloudUploadResult, error) {
	body, err := manifest.EncodeReleaseBody(value)
	if err != nil {
		return backup.CloudUploadResult{}, err
	}
	manifestBytes, err := manifest.Marshal(value)
	if err != nil {
		return backup.CloudUploadResult{}, err
	}
	created, _ := time.Parse(time.RFC3339, value.CreatedAt)
	result, err := c.client.UploadBackup(ctx, github.UploadRequest{
		Owner:         c.owner,
		Repo:          c.repo,
		Tag:           "backup-" + value.ID,
		Name:          "DSH Backup " + created.Local().Format("2006-01-02 15:04:05"),
		Body:          body,
		ArchivePath:   archivePath,
		ArchiveName:   value.Archive.Name,
		ArchiveSize:   value.Archive.Size,
		ArchiveSHA256: value.Archive.SHA256,
		ManifestName:  manifest.ManifestPath(value.Archive.Name),
		ManifestBytes: manifestBytes,
	}, progress)
	if err != nil {
		return backup.CloudUploadResult{}, err
	}
	return backup.CloudUploadResult{
		ReleaseID: result.ReleaseID,
		Tag:       result.Tag,
		HTMLURL:   result.HTMLURL,
	}, nil
}

func (c *appCloud) DownloadBackup(ctx context.Context, snapshot manifest.Snapshot, destDir string, progress func(done, total int64)) (backup.CloudDownloadResult, error) {
	if snapshot.CloudReleaseID == 0 {
		return backup.CloudDownloadResult{}, errors.New("云端快照缺少 release ID")
	}
	result, err := c.client.DownloadBackup(ctx, c.owner, c.repo, snapshot.CloudReleaseID, destDir, progress)
	if err != nil {
		return backup.CloudDownloadResult{}, err
	}
	return backup.CloudDownloadResult{
		ArchivePath:  result.ArchivePath,
		ManifestPath: result.ManifestPath,
		Manifest:     result.Manifest,
		SHA256:       result.SHA256,
		Size:         result.Size,
	}, nil
}

func (c *appCloud) ApplyRetention(ctx context.Context, keep int) ([]string, error) {
	return c.client.ApplyRetention(ctx, c.owner, c.repo, keep)
}

func (a *App) buildCloud() (*appCloud, error) {
	cfg := a.configSnapshot()
	if cfg.GitHub.FullRepo() == "" {
		return nil, errors.New("请先选择或创建 GitHub 私有仓库")
	}
	client, _, err := a.newGitHubClient(true)
	if err != nil {
		return nil, err
	}
	return &appCloud{app: a, client: client, owner: cfg.GitHub.Owner, repo: cfg.GitHub.Repo}, nil
}

func (a *App) StartBackup(force bool) (string, error) {
	return a.startBackup(force, manifest.KindManual)
}

func (a *App) startBackup(force bool, kind string) (string, error) {
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
	if cfg.EncryptionLocal || cfg.Mode != config.ModeLocal {
		keyCtx, keyCancel := context.WithTimeout(context.Background(), 40*time.Second)
		keyErr := a.ensureEncryptionReady(keyCtx, cfg, true)
		keyCancel()
		if keyErr != nil {
			return "", keyErr
		}
	}
	var cloud backup.Cloud
	if cfg.Mode != config.ModeLocal {
		adapter, err := a.buildCloud()
		if err != nil {
			return "", err
		}
		cloud = adapter
	}
	taskID, ctx, err := a.beginTask("backup")
	if err != nil {
		return "", err
	}
	manager := backup.NewManager(a.paths, a.sidecar, a.logger, a.sevenZipPath, a.sevenZipVersion, Version)
	input := backup.BackupInput{
		TaskID:       taskID,
		Config:       cfg,
		Kind:         kind,
		Force:        force,
		Cloud:        cloud,
		CloudEnabled: cfg.Mode != config.ModeLocal,
		KeepLocal:    cfg.Mode != config.ModeCloud,
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
			taskResult.CloudError = result.CloudError
		}
		a.invalidateCloudCache()
		a.finishTask(taskID)
		a.emitTaskResult(taskResult)
	}()
	return taskID, nil
}

func (a *App) StartRestore(request RestoreRequest) (string, error) {
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
	snapshot, err := a.resolveSnapshot(request.SnapshotID, request.Location)
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
	if request.PreRestoreSnapshot && (cfg.EncryptionLocal || cfg.Mode != config.ModeLocal) {
		keyCtx, keyCancel := context.WithTimeout(context.Background(), 40*time.Second)
		keyErr := a.ensureEncryptionReady(keyCtx, cfg, true)
		keyCancel()
		if keyErr != nil {
			return "", keyErr
		}
	}
	var cloud backup.Cloud
	if snapshot.Location == "cloud" || snapshot.LocalPath == "" {
		adapter, err := a.buildCloud()
		if err != nil {
			return "", err
		}
		cloud = adapter
	}
	taskID, ctx, err := a.beginTask("restore")
	if err != nil {
		return "", err
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
		Cloud:              cloud,
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
	if a.sidecar == nil {
		return "", errors.New(a.sidecarError)
	}
	if !a.sevenZipOK {
		return "", errors.New(a.sevenZipError)
	}
	snapshot, err := a.resolveSnapshot(request.SnapshotID, request.Location)
	if err != nil {
		return "", err
	}
	var cloud backup.Cloud
	if snapshot.Location == "cloud" || snapshot.LocalPath == "" {
		adapter, err := a.buildCloud()
		if err != nil {
			return "", err
		}
		cloud = adapter
	}
	taskID, ctx, err := a.beginTask("verify")
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
	go func() {
		var verifyErr error
		if snapshot.Location == "cloud" || snapshot.LocalPath == "" {
			verifyErr = manager.VerifyCloud(ctx, taskID, snapshot, cloud, onProgress)
		} else {
			verifyErr = manager.VerifyLocal(ctx, taskID, snapshot, onProgress)
		}
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
	if err := manager.DeleteLocal(context.Background(), *target, cfg.BackupDir); err != nil {
		return err
	}
	if a.logger != nil {
		a.logger.Info("snapshot.delete", "删除本地快照", map[string]any{"id": id})
	}
	a.invalidateCloudCache()
	a.emitStateChanged("snapshot-delete")
	return nil
}

func (a *App) DeleteCloudSnapshot(releaseID int64) error {
	if releaseID == 0 {
		return errors.New("release ID 无效")
	}
	adapter, err := a.buildCloud()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := adapter.client.DeleteBackup(ctx, adapter.owner, adapter.repo, releaseID); err != nil {
		return err
	}
	if a.logger != nil {
		a.logger.Info("snapshot.delete", "删除云端快照", map[string]any{"releaseId": releaseID})
	}
	a.invalidateCloudCache()
	a.emitStateChanged("snapshot-delete")
	return nil
}

func (a *App) resolveSnapshot(id, location string) (manifest.Snapshot, error) {
	if strings.TrimSpace(id) == "" {
		return manifest.Snapshot{}, errors.New("快照 ID 不能为空")
	}
	cfg := a.configSnapshot()
	if location == "" || location == "local" || location == "both" {
		local, _, err := manifest.Scan(cfg.BackupDir)
		if err == nil {
			for _, snapshot := range local {
				if snapshot.ID == id && snapshot.Status == "ready" {
					return snapshot, nil
				}
			}
		}
	}
	if location == "local" {
		return manifest.Snapshot{}, errors.New("本地快照不存在、文件缺失或大小不符，无法恢复")
	}
	cloud, errText := a.getCloudSnapshots(true)
	if errText != "" {
		return manifest.Snapshot{}, errors.New(errText)
	}
	for _, snapshot := range cloud {
		if snapshot.ID == id {
			if location == "cloud" || location == "both" || location == "" {
				return snapshot, nil
			}
		}
	}
	return manifest.Snapshot{}, errors.New("找不到该快照")
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

func (a *App) CloudSnapshotByRelease(releaseID int64) (manifest.Snapshot, error) {
	list, errText := a.getCloudSnapshots(false)
	if errText != "" {
		return manifest.Snapshot{}, errors.New(errText)
	}
	for _, snapshot := range list {
		if snapshot.CloudReleaseID == releaseID {
			return snapshot, nil
		}
	}
	return manifest.Snapshot{}, errors.New("找不到云端快照")
}
