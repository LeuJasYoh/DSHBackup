package backup

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"DSHBackup/internal/apppath"
	"DSHBackup/internal/config"
	"DSHBackup/internal/logging"
	"DSHBackup/internal/manifest"
	"DSHBackup/internal/platform"
	"DSHBackup/internal/sevenparams"
	"DSHBackup/internal/sidecar"
)

var (
	DSHExcludeTop     = []string{"logs", "cache", "electron-user-data"}
	DSHExcludeRel     = []string{".dshw-usage.json.before-recharge-fix.bak"}
	DesktopExcludeTop = []string{
		"Cache", "Code Cache", "GPUCache", "GrShaderCache", "DawnWebGPUCache",
		"DawnGraphiteCache", "ShaderCache", "Dictionaries", "GPUPersistentCache",
	}
	DesktopExcludeRel = []string{"logs"}
)

type Progress struct {
	Phase      string
	Percent    float64
	Message    string
	BytesDone  int64
	BytesTotal int64
}

type RunningError struct {
	Evidence []string
}

func (e *RunningError) Error() string {
	if len(e.Evidence) == 0 {
		return "DSH 正在运行"
	}
	return "DSH 正在运行：" + strings.Join(e.Evidence, "；")
}

func IsRunning(err error) bool {
	var target *RunningError
	return errors.As(err, &target)
}

type CloudUploadResult struct {
	ReleaseID int64
	Tag       string
	HTMLURL   string
}

type CloudDownloadResult struct {
	ArchivePath  string
	ManifestPath string
	Manifest     manifest.Manifest
	SHA256       string
	Size         int64
}

type Cloud interface {
	UploadBackup(ctx context.Context, value manifest.Manifest, archivePath string, progress func(done, total int64)) (CloudUploadResult, error)
	DownloadBackup(ctx context.Context, snapshot manifest.Snapshot, destDir string, progress func(done, total int64)) (CloudDownloadResult, error)
	ApplyRetention(ctx context.Context, keep int) ([]string, error)
}

type BackupInput struct {
	TaskID       string
	Config       config.Config
	Kind         string
	Force        bool
	Cloud        Cloud
	CloudEnabled bool
	KeepLocal    bool
	OnProgress   func(Progress)
}

type BackupResult struct {
	Snapshot    manifest.Snapshot
	Manifest    manifest.Manifest
	ArchivePath string
	Warnings    []string
	CloudError  string
}

type RestoreInput struct {
	TaskID             string
	Config             config.Config
	Snapshot           manifest.Snapshot
	IncludeDesktop     bool
	PreRestoreSnapshot bool
	TargetDSH          string
	Force              bool
	Cloud              Cloud
	OnProgress         func(Progress)
}

type RestoreResult struct {
	Files              int64
	Bytes              int64
	PreRestoreSnapshot *manifest.Snapshot
	Warnings           []string
	DesktopRestored    bool
	TargetDSH          string
}

type Manager struct {
	paths           apppath.Paths
	client          *sidecar.Client
	logger          *logging.Logger
	sevenZip        string
	sevenZipVersion string
	appVersion      string
}

func NewManager(paths apppath.Paths, client *sidecar.Client, logger *logging.Logger, sevenZip, sevenZipVersion, appVersion string) *Manager {
	return &Manager{
		paths:           paths,
		client:          client,
		logger:          logger,
		sevenZip:        sevenZip,
		sevenZipVersion: sevenZipVersion,
		appVersion:      appVersion,
	}
}

func (m *Manager) Backup(ctx context.Context, input BackupInput) (BackupResult, error) {
	if err := config.Validate(input.Config); err != nil {
		return BackupResult{}, err
	}
	if input.Kind == "" {
		input.Kind = manifest.KindManual
	}
	report := func(phase string, percent float64, message string, done, total int64) {
		if input.OnProgress != nil {
			if percent < 0 {
				percent = 0
			}
			if percent > 100 {
				percent = 100
			}
			input.OnProgress(Progress{
				Phase:      phase,
				Percent:    percent,
				Message:    message,
				BytesDone:  done,
				BytesTotal: total,
			})
		}
	}
	report("precheck", 0.5, "检查备份环境", 0, 0)
	if !input.Force {
		running, evidence, err := platform.IsDSHRunning(m.paths.DSHDataDir())
		if err != nil {
			return BackupResult{}, fmt.Errorf("检测 DSH 运行状态失败：%w", err)
		}
		if running {
			return BackupResult{}, &RunningError{Evidence: evidence}
		}
	}
	if err := m.checkBackupDestination(input); err != nil {
		return BackupResult{}, err
	}
	if input.CloudEnabled && input.Cloud == nil {
		return BackupResult{}, errors.New("云端模式尚未连接 GitHub")
	}

	taskDir, err := m.paths.TempTaskDir(input.TaskID)
	if err != nil {
		return BackupResult{}, err
	}
	defer os.RemoveAll(taskDir)
	stagingDir := filepath.Join(taskDir, "staging")
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return BackupResult{}, err
	}

	sources := m.sources(input.Config)
	if len(sources) == 0 {
		return BackupResult{}, errors.New("找不到可备份的 DSH 数据")
	}
	report("precheck", 2, "扫描源目录", 0, 0)
	stageResult, err := m.client.Stage(ctx, input.TaskID, stagingDir, sources, func(event sidecar.Progress) {
		percent := 3 + (clamp(event.Percent, 0, 10)-2)/8*11
		report("stage", percent, event.Message, event.BytesDone, event.BytesTotal)
	})
	if err != nil {
		return BackupResult{}, mapTaskError(err)
	}
	warnings := append([]string{}, stageResult.Warnings...)
	stagedWarnings, err := m.verifyStaged(stageResult, input.Config.IncludeDesktop)
	if err != nil {
		return BackupResult{}, err
	}
	warnings = append(warnings, stagedWarnings...)

	tempFree, err := platform.DiskFree(taskDir)
	if err != nil {
		return BackupResult{}, fmt.Errorf("检查临时目录空间失败：%w", err)
	}
	required := uint64(stageResult.Bytes) + 32*1024*1024
	if tempFree.Free < required {
		return BackupResult{}, fmt.Errorf("临时目录空间不足：需要至少 %s，可用 %s", formatBytes(int64(required)), formatBytes(int64(tempFree.Free)))
	}
	if input.KeepLocal {
		if _, err := ensureWritableDir(input.Config.BackupDir); err != nil {
			return BackupResult{}, fmt.Errorf("本地备份目录不可写：%w", err)
		}
		free, err := platform.DiskFree(input.Config.BackupDir)
		if err != nil {
			return BackupResult{}, fmt.Errorf("检查备份目录空间失败：%w", err)
		}
		if free.Free < required {
			return BackupResult{}, fmt.Errorf("备份目录空间不足：需要至少 %s，可用 %s", formatBytes(int64(required)), formatBytes(int64(free.Free)))
		}
	}

	encrypted := input.Config.EncryptionLocal || input.CloudEnabled || input.Config.Mode != config.ModeLocal
	password := ""
	manifestValue := manifest.Manifest{
		SchemaVersion:   manifest.SchemaVersion,
		Kind:            input.Kind,
		AppVersion:      m.appVersion,
		DesktopIncluded: input.Config.IncludeDesktop && m.sourceExists(m.paths.DesktopStateDir()),
		Files:           manifest.FileStats{Files: stageResult.Files, Bytes: stageResult.Bytes},
		Compressor: manifest.CompressorInfo{
			Name:    "7-Zip",
			Version: m.sevenZipVersion,
			Args:    sevenparams.BuildArgs(input.Config.Compression.Level, input.Config.Compression.DictionaryMB, input.Config.Compression.Threads, input.Config.Compression.ExtraArgs),
		},
	}
	manifestValue.Scope = []string{"dsh"}
	if manifestValue.DesktopIncluded {
		manifestValue.Scope = append(manifestValue.Scope, "desktop-state")
	}
	hostname, _ := os.Hostname()
	manifestValue.Hostname = hostname
	created := time.Now().Truncate(time.Second)
	id := manifest.NewID(created)
	if input.KeepLocal {
		id, created, err = uniqueID(input.Config.BackupDir, created)
		if err != nil {
			return BackupResult{}, err
		}
	}
	manifestValue.ID = id
	manifestValue.CreatedAt = created.Format(time.RFC3339)
	manifestValue.Archive.Name = manifest.ArchiveName(id)

	if encrypted {
		key, err := m.ensureKeys(ctx, input.Config.BackupDir)
		if err != nil {
			return BackupResult{}, err
		}
		password, err = randomPassword()
		if err != nil {
			return BackupResult{}, err
		}
		wrapped, _, err := m.client.WrapPassword(ctx, m.paths.KeysDir, password)
		if err != nil {
			return BackupResult{}, err
		}
		manifestValue.Encryption = &manifest.EncryptionInfo{
			Enabled:         true,
			Algorithm:       "7z-aes256+age-x25519",
			KeyID:           key.KeyID,
			WrappedPassword: wrapped,
		}
	}

	outputPath := filepath.Join(taskDir, manifestValue.Archive.Name)
	report("compress", 14, "启动 7z 压缩", 0, stageResult.Bytes)
	compression := sidecar.Compression{
		Level:        sevenparams.LevelMX(input.Config.Compression.Level),
		DictionaryMB: input.Config.Compression.DictionaryMB,
		Threads:      input.Config.Compression.Threads,
		ExtraArgs:    input.Config.Compression.ExtraArgs,
	}
	backupResult, err := m.client.Backup(ctx, input.TaskID, sidecar.BackupRequest{
		SevenZip:    m.sevenZip,
		StagingDir:  stagingDir,
		Output:      outputPath,
		Password:    password,
		Compression: compression,
		TotalBytes:  stageResult.Bytes,
	}, func(event sidecar.Progress) {
		percent := 14 + (clamp(event.Percent, 10, 98)-10)/88*62
		report("compress", percent, event.Message, event.BytesDone, event.BytesTotal)
	})
	if err != nil {
		return BackupResult{}, mapTaskError(err)
	}
	warnings = append(warnings, backupResult.Warnings...)
	manifestValue.Archive = manifest.ArchiveInfo{
		Name:   manifest.ArchiveName(manifestValue.ID),
		Size:   backupResult.Size,
		SHA256: backupResult.SHA256,
	}
	if backupResult.Size <= 0 {
		return BackupResult{}, errors.New("压缩结果为空")
	}

	verified := false
	if input.Config.VerifyAfterBackup {
		report("verify", 76.5, "校验归档完整性", 0, backupResult.Size)
		testResult, err := m.client.TestArchive(ctx, input.TaskID, m.sevenZip, outputPath, password, func(event sidecar.Progress) {
			report("verify", 76.5+clamp(event.Percent, 0, 100)*0.095, event.Message, event.BytesDone, event.BytesTotal)
		})
		if err != nil {
			return BackupResult{}, mapTaskError(err)
		}
		warnings = append(warnings, testResult.Warnings...)
		verified = testResult.OK
		if !testResult.OK {
			return BackupResult{}, errors.New("归档完整性校验未通过")
		}
	} else {
		report("verify", 86, "已跳过完整性测试", backupResult.Size, backupResult.Size)
	}

	archivePath := outputPath
	manifestPath := ""
	if input.KeepLocal {
		report("save", 87, "写入备份目录", backupResult.Size, backupResult.Size)
		finalPath := filepath.Join(input.Config.BackupDir, manifestValue.Archive.Name)
		if input.Config.BackupDir != "" {
			if _, err := ensureWritableDir(input.Config.BackupDir); err != nil {
				return BackupResult{}, err
			}
		}
		free, err := platform.DiskFree(input.Config.BackupDir)
		if err != nil {
			return BackupResult{}, err
		}
		if free.Free < uint64(backupResult.Size)+16*1024*1024 {
			return BackupResult{}, fmt.Errorf("备份目录空间不足以写入归档（可用 %s）", formatBytes(int64(free.Free)))
		}
		if err := moveFile(outputPath, finalPath); err != nil {
			return BackupResult{}, fmt.Errorf("写入本地备份失败：%w", err)
		}
		archivePath = finalPath
		manifestPath = manifest.ManifestPath(finalPath)
		raw, err := manifest.Marshal(manifestValue)
		if err != nil {
			_ = os.Remove(finalPath)
			return BackupResult{}, err
		}
		if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
			_ = os.Remove(finalPath)
			return BackupResult{}, fmt.Errorf("写入 manifest 失败：%w", err)
		}
	} else {
		report("save", 87, "归档准备完成", backupResult.Size, backupResult.Size)
	}

	snapshot := manifest.FromManifest(manifestValue, archivePath, manifestPath)
	snapshot.Encrypted = manifestValue.Encryption != nil && manifestValue.Encryption.Enabled
	snapshot.Status = "ready"
	cloudError := ""
	if input.CloudEnabled && input.Cloud != nil {
		report("upload", 88, "上传到 GitHub 私有仓库", 0, backupResult.Size)
		uploadResult, err := input.Cloud.UploadBackup(ctx, manifestValue, archivePath, func(done, total int64) {
			percent := 88.0
			if total > 0 {
				percent = 88 + (float64(done)/float64(total))*8
			}
			report("upload", percent, "上传加密归档", done, total)
		})
		if err != nil {
			if ctx.Err() != nil {
				return BackupResult{}, mapTaskError(err)
			}
			cloudError = err.Error()
			warnings = append(warnings, "云端上传失败："+cloudError)
			if !input.KeepLocal {
				fallbackPath := filepath.Join(input.Config.BackupDir, manifestValue.Archive.Name)
				if _, dirErr := ensureWritableDir(input.Config.BackupDir); dirErr == nil {
					if moveErr := moveFile(archivePath, fallbackPath); moveErr == nil {
						archivePath = fallbackPath
						manifestPath = manifest.ManifestPath(fallbackPath)
						raw, _ := manifest.Marshal(manifestValue)
						if writeErr := os.WriteFile(manifestPath, raw, 0o600); writeErr == nil {
							snapshot = manifest.FromManifest(manifestValue, archivePath, manifestPath)
							snapshot.Status = "ready"
							warnings = append(warnings, "云端失败后已在本机保留一份归档")
						}
					}
				}
			}
			if m.logger != nil {
				m.logger.Warn("backup.upload", "云端上传失败", map[string]any{"task": input.TaskID, "error": cloudError})
			}
		} else {
			snapshot.CloudReleaseID = uploadResult.ReleaseID
			snapshot.CloudTag = uploadResult.Tag
			snapshot.CloudHTMLURL = uploadResult.HTMLURL
			snapshot.CloudSize = backupResult.Size
			if input.KeepLocal {
				snapshot.Location = "both"
				snapshot.Source = "both"
			} else {
				snapshot.Location = "cloud"
				snapshot.Source = "cloud"
				snapshot.LocalPath = ""
				snapshot.ManifestPath = ""
			}
		}
		report("cleanup", 97, "应用云端保留策略", 0, 0)
		if cloudError == "" {
			deleted, retentionErr := input.Cloud.ApplyRetention(ctx, input.Config.Retention.Cloud)
			if retentionErr != nil {
				warnings = append(warnings, "云端保留策略执行失败："+retentionErr.Error())
			} else if len(deleted) > 0 {
				warnings = append(warnings, "已按保留策略清理云端快照："+strings.Join(deleted, "、"))
			}
		}
	}

	if input.KeepLocal {
		report("cleanup", 98, "应用本地保留策略", 0, 0)
		deleted, err := m.applyLocalRetention(input.Config.BackupDir, input.Config.Retention.Local, manifestValue.ID)
		if err != nil {
			warnings = append(warnings, "本地保留策略执行失败："+err.Error())
		} else if len(deleted) > 0 {
			warnings = append(warnings, "已按保留策略清理本地快照："+strings.Join(deleted, "、"))
		}
	}
	report("done", 100, "备份完成", backupResult.Size, backupResult.Size)
	if m.logger != nil {
		m.logger.Info("backup.complete", "备份任务完成", map[string]any{
			"task":       input.TaskID,
			"id":         manifestValue.ID,
			"size":       backupResult.Size,
			"encrypted":  encrypted,
			"cloud":      input.CloudEnabled,
			"verified":   verified,
			"desktop":    manifestValue.DesktopIncluded,
			"cloudError": cloudError,
		})
	}
	return BackupResult{
		Snapshot:    snapshot,
		Manifest:    manifestValue,
		ArchivePath: archivePath,
		Warnings:    dedupeWarnings(warnings),
		CloudError:  cloudError,
	}, nil
}

func (m *Manager) Restore(ctx context.Context, input RestoreInput) (RestoreResult, error) {
	if input.Snapshot.ID == "" {
		return RestoreResult{}, errors.New("未选择要恢复的快照")
	}
	report := func(phase string, percent float64, message string, done, total int64) {
		if input.OnProgress != nil {
			input.OnProgress(Progress{Phase: phase, Percent: clamp(percent, 0, 100), Message: message, BytesDone: done, BytesTotal: total})
		}
	}
	report("precheck", 0.5, "检查恢复环境", 0, 0)
	if !input.Force {
		running, evidence, err := platform.IsDSHRunning(m.paths.DSHDataDir())
		if err != nil {
			return RestoreResult{}, fmt.Errorf("检测 DSH 运行状态失败：%w", err)
		}
		if running {
			return RestoreResult{}, &RunningError{Evidence: evidence}
		}
	}
	taskDir, err := m.paths.TempTaskDir("restore-" + input.TaskID)
	if err != nil {
		return RestoreResult{}, err
	}
	defer os.RemoveAll(taskDir)

	var archivePath string
	var manifestPath string
	var value manifest.Manifest
	if input.Snapshot.Location == "cloud" || input.Snapshot.LocalPath == "" {
		if input.Cloud == nil {
			return RestoreResult{}, errors.New("该快照只存在于云端，请先连接 GitHub")
		}
		report("download", 2, "从 GitHub 下载归档", 0, 0)
		download, err := input.Cloud.DownloadBackup(ctx, input.Snapshot, taskDir, func(done, total int64) {
			percent := 2.0
			if total > 0 {
				percent = 2 + (float64(done)/float64(total))*36
			}
			report("download", percent, "下载云端归档", done, total)
		})
		if err != nil {
			return RestoreResult{}, mapTaskError(err)
		}
		archivePath = download.ArchivePath
		manifestPath = download.ManifestPath
		value = download.Manifest
		if input.Snapshot.SHA256 != "" && !strings.EqualFold(download.SHA256, input.Snapshot.SHA256) {
			return RestoreResult{}, errors.New("下载的归档与快照记录的 SHA-256 不一致")
		}
	} else {
		archivePath = input.Snapshot.LocalPath
		manifestPath = input.Snapshot.ManifestPath
		if manifestPath == "" {
			manifestPath = manifest.ManifestPath(archivePath)
		}
		if _, err := os.Stat(archivePath); err != nil {
			return RestoreResult{}, fmt.Errorf("归档不存在：%w", err)
		}
		value, err = manifest.Read(manifestPath)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("读取本地 manifest 失败：%w", err)
		}
		if input.Snapshot.ID != value.ID {
			return RestoreResult{}, errors.New("快照 ID 与 manifest 不一致")
		}
		report("download", 8, "校验本地归档哈希", 0, value.Archive.Size)
		hash, err := m.client.HashFile(ctx, archivePath)
		if err != nil {
			return RestoreResult{}, err
		}
		if !strings.EqualFold(hash.SHA256, value.Archive.SHA256) {
			return RestoreResult{}, errors.New("本地归档 SHA-256 校验失败，文件可能已损坏")
		}
	}
	if value.ID == "" {
		return RestoreResult{}, errors.New("manifest 无效")
	}
	if input.Snapshot.Location == "cloud" {
		// 云端下载的 manifest 与快照记录保持一致性检查。
		if input.Snapshot.SHA256 != "" && !strings.EqualFold(value.Archive.SHA256, input.Snapshot.SHA256) {
			return RestoreResult{}, errors.New("云端 manifest 的 SHA-256 与快照记录不一致")
		}
	}
	password := ""
	if value.Encryption != nil && value.Encryption.Enabled {
		report("decrypt", 40, "读取本地私钥", 0, 0)
		password, err = m.client.UnwrapPassword(ctx, m.paths.KeysDir, value.Encryption.WrappedPassword)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("解密归档失败：%w", err)
		}
	}
	report("extract", 42, "检查归档结构", 0, 0)
	validation, err := m.client.ValidateArchive(ctx, m.sevenZip, archivePath, password)
	if err != nil {
		return RestoreResult{}, mapTaskError(err)
	}
	entries := make(map[string]bool, len(validation.Entries))
	for _, entry := range validation.Entries {
		entries[entry] = true
	}
	extractDir := filepath.Join(taskDir, "extract")
	if err := os.MkdirAll(extractDir, 0o700); err != nil {
		return RestoreResult{}, err
	}
	extractResult, err := m.client.Extract(ctx, input.TaskID, m.sevenZip, archivePath, extractDir, password, func(event sidecar.Progress) {
		report("extract", 42+clamp(event.Percent, 0, 100)*0.28, event.Message, event.BytesDone, event.BytesTotal)
	})
	if err != nil {
		return RestoreResult{}, mapTaskError(err)
	}
	warnings := append([]string{}, extractResult.Warnings...)

	var preSnapshot *manifest.Snapshot
	if input.PreRestoreSnapshot {
		report("snapshot", 70, "创建恢复前快照", 0, 0)
		preResult, err := m.Backup(ctx, BackupInput{
			TaskID:       input.TaskID + "-pre",
			Config:       input.Config,
			Kind:         manifest.KindPreRestore,
			Force:        true,
			KeepLocal:    true,
			CloudEnabled: false,
			OnProgress: func(progress Progress) {
				report("snapshot", 70+progress.Percent*0.10, "恢复前快照："+progress.Message, progress.BytesDone, progress.BytesTotal)
			},
		})
		if err != nil {
			return RestoreResult{}, fmt.Errorf("创建恢复前快照失败，已中止恢复：%w", err)
		}
		preSnapshot = &preResult.Snapshot
		warnings = append(warnings, preResult.Warnings...)
	}

	if running, evidence, err := platform.IsDSHRunning(m.paths.DSHDataDir()); err == nil && running {
		if !input.Force {
			return RestoreResult{}, &RunningError{Evidence: evidence}
		}
		warnings = append(warnings, "恢复时检测到 DSH 仍在运行，文件覆盖可能不完整")
	}

	targetDSH := input.TargetDSH
	if targetDSH == "" {
		targetDSH = m.paths.DSHDataDir()
	}
	if !filepath.IsAbs(targetDSH) {
		return RestoreResult{}, errors.New("恢复目标必须是绝对路径")
	}
	if isPathInside(targetDSH, m.paths.TempDir) {
		return RestoreResult{}, errors.New("恢复目标不能位于临时目录")
	}
	if err := os.MkdirAll(targetDSH, 0o755); err != nil {
		return RestoreResult{}, fmt.Errorf("无法创建恢复目标：%w", err)
	}
	targets := []sidecar.InstallTarget{{Name: "dsh", Path: targetDSH}}
	desktopRestored := false
	if input.IncludeDesktop && value.DesktopIncluded {
		desktopDir := m.paths.DesktopStateDir()
		if desktopDir == "" {
			warnings = append(warnings, "无法确定桌面状态目录，已跳过桌面登录态")
		} else {
			targets = append(targets, sidecar.InstallTarget{Name: "desktop-state", Path: desktopDir})
			desktopRestored = true
		}
	} else if input.IncludeDesktop && !value.DesktopIncluded {
		warnings = append(warnings, "该快照不包含桌面登录态，已跳过")
	}
	report("install", 82, "覆盖恢复文件", 0, 0)
	installResult, err := m.client.InstallStaging(ctx, input.TaskID, extractDir, targets, func(event sidecar.Progress) {
		report("install", 82+clamp(event.Percent, 0, 100)*0.13, event.Message, event.BytesDone, event.BytesTotal)
	})
	if err != nil {
		return RestoreResult{}, mapTaskError(err)
	}
	warnings = append(warnings, installResult.Warnings...)
	report("verify", 96, "验证恢复结果", 0, 0)
	if err := verifyRestored(entries, targetDSH, desktopRestored, m.paths.DesktopStateDir()); err != nil {
		message := err.Error()
		if preSnapshot != nil {
			message += "；恢复前快照已保留：" + preSnapshot.LocalPath
		}
		return RestoreResult{}, errors.New(message)
	}
	report("done", 100, "恢复完成", installResult.Bytes, installResult.Bytes)
	if m.logger != nil {
		m.logger.Info("restore.complete", "恢复任务完成", map[string]any{
			"task":    input.TaskID,
			"id":      value.ID,
			"files":   installResult.Files,
			"desktop": desktopRestored,
		})
	}
	return RestoreResult{
		Files:              installResult.Files,
		Bytes:              installResult.Bytes,
		PreRestoreSnapshot: preSnapshot,
		Warnings:           dedupeWarnings(warnings),
		DesktopRestored:    desktopRestored,
		TargetDSH:          targetDSH,
	}, nil
}

func (m *Manager) VerifyLocal(ctx context.Context, taskID string, snapshot manifest.Snapshot, onProgress func(Progress)) error {
	if snapshot.LocalPath == "" {
		return errors.New("快照没有本地归档")
	}
	value, err := manifest.Read(snapshot.ManifestPath)
	if err != nil {
		return err
	}
	report := func(phase string, percent float64, message string) {
		if onProgress != nil {
			onProgress(Progress{Phase: phase, Percent: percent, Message: message, BytesTotal: value.Archive.Size})
		}
	}
	report("verify", 5, "校验归档大小")
	stat, err := os.Stat(snapshot.LocalPath)
	if err != nil {
		return err
	}
	if stat.Size() != value.Archive.Size {
		return errors.New("归档大小与 manifest 不一致")
	}
	report("verify", 12, "计算 SHA-256")
	hash, err := m.client.HashFile(ctx, snapshot.LocalPath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(hash.SHA256, value.Archive.SHA256) {
		return errors.New("归档 SHA-256 校验失败")
	}
	password := ""
	if value.Encryption != nil && value.Encryption.Enabled {
		password, err = m.client.UnwrapPassword(ctx, m.paths.KeysDir, value.Encryption.WrappedPassword)
		if err != nil {
			return fmt.Errorf("无法解密归档密码：%w", err)
		}
	}
	testResult, err := m.client.TestArchive(ctx, taskID, m.sevenZip, snapshot.LocalPath, password, func(event sidecar.Progress) {
		report("verify", 20+clamp(event.Percent, 0, 100)*0.8, event.Message)
	})
	if err != nil {
		return mapTaskError(err)
	}
	if !testResult.OK {
		return errors.New("7z 完整性测试未通过")
	}
	report("done", 100, "验证通过")
	return nil
}

func (m *Manager) VerifyCloud(ctx context.Context, taskID string, snapshot manifest.Snapshot, cloud Cloud, onProgress func(Progress)) error {
	if cloud == nil {
		return errors.New("云端未连接")
	}
	taskDir, err := m.paths.TempTaskDir("verify-" + taskID)
	if err != nil {
		return err
	}
	defer os.RemoveAll(taskDir)
	download, err := cloud.DownloadBackup(ctx, snapshot, taskDir, func(done, total int64) {
		if onProgress != nil {
			percent := 2.0
			if total > 0 {
				percent = 2 + (float64(done)/float64(total))*58
			}
			onProgress(Progress{Phase: "download", Percent: percent, Message: "下载云端归档", BytesDone: done, BytesTotal: total})
		}
	})
	if err != nil {
		return err
	}
	password := ""
	if download.Manifest.Encryption != nil && download.Manifest.Encryption.Enabled {
		password, err = m.client.UnwrapPassword(ctx, m.paths.KeysDir, download.Manifest.Encryption.WrappedPassword)
		if err != nil {
			return err
		}
	}
	if onProgress != nil {
		onProgress(Progress{Phase: "verify", Percent: 65, Message: "测试归档完整性"})
	}
	testResult, err := m.client.TestArchive(ctx, taskID, m.sevenZip, download.ArchivePath, password, func(event sidecar.Progress) {
		if onProgress != nil {
			onProgress(Progress{Phase: "verify", Percent: 65 + clamp(event.Percent, 0, 100)*0.34, Message: event.Message})
		}
	})
	if err != nil {
		return err
	}
	if !testResult.OK {
		return errors.New("云端归档完整性测试未通过")
	}
	if onProgress != nil {
		onProgress(Progress{Phase: "done", Percent: 100, Message: "云端备份验证通过"})
	}
	return nil
}

func (m *Manager) DeleteLocal(_ context.Context, snapshot manifest.Snapshot, backupDir string) error {
	if snapshot.LocalPath == "" || snapshot.ManifestPath == "" {
		return errors.New("快照没有本地文件")
	}
	absoluteDir, err := filepath.Abs(backupDir)
	if err != nil {
		return err
	}
	archive, err := filepath.Abs(snapshot.LocalPath)
	if err != nil {
		return err
	}
	if !isPathInside(archive, absoluteDir) {
		return errors.New("拒绝删除备份目录之外的文件")
	}
	if err := os.Remove(archive); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(snapshot.ManifestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (m *Manager) ListLocal(backupDir string) ([]manifest.Snapshot, []string, error) {
	return manifest.Scan(backupDir)
}

func (m *Manager) ensureKeys(ctx context.Context, backupDir string) (sidecar.KeyInfo, error) {
	info, err := m.client.KeysInfo(ctx, m.paths.KeysDir)
	if err != nil {
		return sidecar.KeyInfo{}, err
	}
	if info.Exists {
		if info.Error != "" {
			return sidecar.KeyInfo{}, errors.New(info.Error)
		}
		return info, nil
	}
	snapshots, _, err := manifest.Scan(backupDir)
	if err != nil {
		return sidecar.KeyInfo{}, err
	}
	for _, snapshot := range snapshots {
		if snapshot.Encrypted {
			return sidecar.KeyInfo{}, errors.New("检测到已有的加密备份，但本地私钥缺失。为避免旧备份永久失效，不会自动生成新密钥；请先恢复 data\\keys 目录")
		}
	}
	generated, err := m.client.KeysEnsure(ctx, m.paths.KeysDir)
	if err != nil {
		return sidecar.KeyInfo{}, err
	}
	if m.logger != nil {
		m.logger.Info("keys.generated", "已生成新的本地密钥对", map[string]any{"keyId": generated.KeyID})
	}
	return generated, nil
}

func (m *Manager) checkBackupDestination(input BackupInput) error {
	if err := m.paths.CheckWritable(); err != nil {
		return fmt.Errorf("应用目录不可写，请把便携版移动到可写目录：%w", err)
	}
	if m.sevenZip == "" {
		return errors.New("找不到内置的 7za.exe")
	}
	if _, err := os.Stat(m.sevenZip); err != nil {
		return fmt.Errorf("7za.exe 不可用：%w", err)
	}
	if input.KeepLocal {
		if _, err := ensureWritableDir(input.Config.BackupDir); err != nil {
			return fmt.Errorf("本地备份目录不可写：%w", err)
		}
		dshDir := m.paths.DSHDataDir()
		if isPathInside(input.Config.BackupDir, dshDir) {
			return errors.New("本地备份目录不能位于 .dsh 目录内")
		}
		desktopDir := m.paths.DesktopStateDir()
		if desktopDir != "" && isPathInside(input.Config.BackupDir, desktopDir) {
			return errors.New("本地备份目录不能位于桌面登录态目录内")
		}
		if isPathInside(input.Config.BackupDir, m.paths.DataDir) {
			return errors.New("本地备份目录不能位于应用 data 目录内")
		}
	}
	return nil
}

func (m *Manager) sources(cfg config.Config) []sidecar.StageRoot {
	var roots []sidecar.StageRoot
	dshDir := m.paths.DSHDataDir()
	if m.sourceExists(dshDir) {
		roots = append(roots, sidecar.StageRoot{
			Name:       "dsh",
			Source:     dshDir,
			ExcludeTop: DSHExcludeTop,
			ExcludeRel: DSHExcludeRel,
		})
	}
	if cfg.IncludeDesktop {
		desktopDir := m.paths.DesktopStateDir()
		if m.sourceExists(desktopDir) {
			roots = append(roots, sidecar.StageRoot{
				Name:       "desktop-state",
				Source:     desktopDir,
				ExcludeTop: DesktopExcludeTop,
				ExcludeRel: DesktopExcludeRel,
			})
		}
	}
	return roots
}

func (m *Manager) sourceExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (m *Manager) verifyStaged(result sidecar.StageResult, includeDesktop bool) ([]string, error) {
	if result.Files == 0 {
		return nil, errors.New("没有暂存到任何文件")
	}
	var warnings []string
	dshSource := m.paths.DSHDataDir()
	staging := result.StagingDir
	for _, relative := range []string{".credentials.yaml", "profiles", "sessions", "storages"} {
		sourcePath := filepath.Join(dshSource, relative)
		if _, err := os.Stat(sourcePath); err != nil {
			continue
		}
		targetPath := filepath.Join(staging, "dsh", relative)
		if _, err := os.Stat(targetPath); err != nil {
			return nil, fmt.Errorf("关键数据未能暂存：%s（文件可能被占用）", relative)
		}
	}
	if sourceSessions := countFiles(filepath.Join(dshSource, "sessions")); sourceSessions > 0 {
		if stagedSessions := countFiles(filepath.Join(staging, "dsh", "sessions")); stagedSessions == 0 {
			warnings = append(warnings, "会话目录存在文件，但没有成功暂存任何会话；请关闭 DSH 后重试")
		}
	}
	if includeDesktop {
		desktop := m.paths.DesktopStateDir()
		if desktop != "" {
			if _, err := os.Stat(filepath.Join(desktop, "Local State")); err == nil {
				if _, err := os.Stat(filepath.Join(staging, "desktop-state", "Local State")); err != nil {
					return nil, errors.New("桌面登录态未能完整暂存：Local State 缺失")
				}
			}
			for _, relative := range []string{"Network/Cookies", "Preferences", "Local Storage", "Session Storage"} {
				if _, err := os.Stat(filepath.Join(desktop, relative)); err == nil {
					if _, err := os.Stat(filepath.Join(staging, "desktop-state", relative)); err != nil {
						warnings = append(warnings, "桌面登录态可能不完整：未能暂存 "+filepath.ToSlash(relative))
					}
				}
			}
		}
	}
	return warnings, nil
}

func countFiles(root string) int64 {
	var count int64
	_ = filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			count++
		}
		return nil
	})
	return count
}

func (m *Manager) applyLocalRetention(backupDir string, keep int, protectID string) ([]string, error) {
	if keep < 1 {
		keep = 1
	}
	snapshots, _, err := manifest.Scan(backupDir)
	if err != nil {
		return nil, err
	}
	var candidates []manifest.Snapshot
	for _, snapshot := range snapshots {
		if snapshot.Kind == manifest.KindPreRestore {
			continue
		}
		candidates = append(candidates, snapshot)
	}
	manifest.Sort(candidates)
	if len(candidates) <= keep {
		return nil, nil
	}
	protected := false
	for index := 0; index < keep; index++ {
		if candidates[index].ID == protectID {
			protected = true
			break
		}
	}
	if !protected {
		for _, snapshot := range candidates {
			if snapshot.ID == protectID {
				candidates = append([]manifest.Snapshot{snapshot}, removeSnapshot(candidates, protectID)...)
				break
			}
		}
	}
	var deleted []string
	for index := keep; index < len(candidates); index++ {
		snapshot := candidates[index]
		if snapshot.ID == protectID {
			continue
		}
		_ = os.Remove(snapshot.LocalPath)
		_ = os.Remove(snapshot.ManifestPath)
		deleted = append(deleted, snapshot.ID)
		if m.logger != nil {
			m.logger.Info("retention.local", "清理旧本地快照", map[string]any{"id": snapshot.ID})
		}
	}
	return deleted, nil
}

func removeSnapshot(snapshots []manifest.Snapshot, id string) []manifest.Snapshot {
	result := make([]manifest.Snapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.ID != id {
			result = append(result, snapshot)
		}
	}
	return result
}

func verifyRestored(entries map[string]bool, targetDSH string, desktopRestored bool, desktopDir string) error {
	critical := []string{".credentials.yaml"}
	for _, relative := range critical {
		key := "dsh/" + strings.ReplaceAll(relative, "\\", "/")
		if entries[key] {
			if _, err := os.Stat(filepath.Join(targetDSH, relative)); err != nil {
				return fmt.Errorf("恢复后校验失败：%s 不存在", relative)
			}
		}
	}
	if entries["dsh/profiles"] || hasPrefix(entries, "dsh/profiles/") {
		if _, err := os.Stat(filepath.Join(targetDSH, "profiles")); err != nil {
			return errors.New("恢复后校验失败：profiles 目录不存在")
		}
	}
	if entries["dsh/sessions"] || hasPrefix(entries, "dsh/sessions/") {
		info, err := os.Stat(filepath.Join(targetDSH, "sessions"))
		if err != nil || !info.IsDir() {
			return errors.New("恢复后校验失败：sessions 目录不存在")
		}
	}
	if desktopRestored && desktopDir != "" {
		if entries["desktop-state/Local State"] {
			if _, err := os.Stat(filepath.Join(desktopDir, "Local State")); err != nil {
				return errors.New("恢复后校验失败：桌面端 Local State 不存在")
			}
		}
		if entries["desktop-state/Preferences"] {
			if _, err := os.Stat(filepath.Join(desktopDir, "Preferences")); err != nil {
				return errors.New("恢复后校验失败：桌面端 Preferences 不存在")
			}
		}
	}
	return nil
}

func hasPrefix(set map[string]bool, prefix string) bool {
	for key := range set {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func uniqueID(dir string, created time.Time) (string, time.Time, error) {
	for attempt := 0; attempt < 120; attempt++ {
		id := manifest.NewID(created)
		archive := filepath.Join(dir, manifest.ArchiveName(id))
		if _, err := os.Stat(archive); errors.Is(err, os.ErrNotExist) {
			return id, created, nil
		}
		created = created.Add(time.Second)
	}
	return "", created, errors.New("无法生成唯一的备份 ID")
}

func randomPassword() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("生成归档密码失败：%w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func ensureWritableDir(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("目录为空")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	probe, err := os.CreateTemp(dir, ".dshbackup-write-test-*")
	if err != nil {
		return "", err
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return absolute, nil
}

func moveFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if err := os.Rename(source, destination); err == nil {
		return nil
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		_ = os.Remove(destination)
		return err
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	return os.Remove(source)
}

func isPathInside(path, parent string) bool {
	if path == "" || parent == "" {
		return false
	}
	relative, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func mapTaskError(err error) error {
	if sidecar.IsCancelled(err) {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return err
}

func formatBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	size := float64(value) / 1024
	index := 0
	for size >= 1024 && index < len(units)-1 {
		size /= 1024
		index++
	}
	return fmt.Sprintf("%.1f %s", size, units[index])
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func dedupeWarnings(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
