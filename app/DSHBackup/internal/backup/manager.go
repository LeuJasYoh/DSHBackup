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
	"DSHBackup/internal/pathguard"
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
		"Crashpad",
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

type BackupInput struct {
	TaskID     string
	Config     config.Config
	Kind       string
	Force      bool
	OnProgress func(Progress)
}

type BackupResult struct {
	Snapshot    manifest.Snapshot
	Manifest    manifest.Manifest
	ArchivePath string
	Warnings    []string
}

type RestoreInput struct {
	TaskID             string
	Config             config.Config
	Snapshot           manifest.Snapshot
	IncludeDesktop     bool
	PreRestoreSnapshot bool
	TargetDSH          string
	Force              bool
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
	sources, err := m.sources(input.Config)
	if err != nil {
		return BackupResult{}, err
	}
	return m.backup(ctx, input, sources, false)
}

func (m *Manager) backup(ctx context.Context, input BackupInput, sources []sidecar.StageRoot, protection bool) (BackupResult, error) {
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

	taskDir, err := m.paths.TempTaskDir(input.TaskID)
	if err != nil {
		return BackupResult{}, err
	}
	defer os.RemoveAll(taskDir)
	stagingDir := filepath.Join(taskDir, "staging")
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return BackupResult{}, err
	}

	if len(sources) == 0 {
		if protection {
			return BackupResult{}, nil
		}
		return BackupResult{}, errors.New("找不到可备份的 DSH 数据")
	}
	for _, source := range sources {
		if isPathInside(input.Config.BackupDir, source.Source) || isPathInside(m.paths.TempDir, source.Source) {
			return BackupResult{}, errors.New("备份源不能包含备份目录或应用临时目录")
		}
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
	incomplete := !stageResult.Complete || stageResult.Skipped > 0 || len(stageResult.Warnings) > 0
	if protection {
		if stageResult.Skipped > 0 || len(warnings) > 0 {
			return BackupResult{}, errors.New("未能完整保护目标数据，请关闭占用文件的程序后重试：" + strings.Join(warnings, "；"))
		}
		if len(stageResult.Entries) == 0 {
			return BackupResult{}, nil
		}
	} else {
		stagedWarnings, err := m.verifyStaged(stageResult, input.Config.IncludeDesktop)
		if err != nil {
			return BackupResult{}, err
		}
		warnings = append(warnings, stagedWarnings...)
	}

	tempFree, err := platform.DiskFree(taskDir)
	if err != nil {
		return BackupResult{}, fmt.Errorf("检查临时目录空间失败：%w", err)
	}
	required := uint64(stageResult.Bytes) + 32*1024*1024
	if tempFree.Free < required {
		return BackupResult{}, fmt.Errorf("临时目录空间不足：需要至少 %s，可用 %s", formatBytes(int64(required)), formatBytes(int64(tempFree.Free)))
	}
	free, err := platform.DiskFree(input.Config.BackupDir)
	if err != nil {
		return BackupResult{}, fmt.Errorf("检查备份目录空间失败：%w", err)
	}
	if free.Free < required {
		return BackupResult{}, fmt.Errorf("备份目录空间不足：需要至少 %s，可用 %s", formatBytes(int64(required)), formatBytes(int64(free.Free)))
	}

	encrypted := input.Config.EncryptionLocal
	password := ""
	manifestValue := manifest.Manifest{
		SchemaVersion:   manifest.SchemaVersion,
		Kind:            input.Kind,
		AppVersion:      m.appVersion,
		DesktopIncluded: m.sourceExists(filepath.Join(stagingDir, "desktop-state")),
		Files:           manifest.FileStats{Files: stageResult.Files, Bytes: stageResult.Bytes},
		Compressor: manifest.CompressorInfo{
			Name:    "7-Zip",
			Version: m.sevenZipVersion,
			Args:    sevenparams.BuildArgs(input.Config.Compression.Level, input.Config.Compression.DictionaryMB, input.Config.Compression.Threads, input.Config.Compression.ExtraArgs),
		},
	}
	for _, source := range sources {
		if m.sourceExists(filepath.Join(stagingDir, source.Name)) {
			manifestValue.Scope = append(manifestValue.Scope, source.Name)
		}
	}
	hostname, _ := os.Hostname()
	manifestValue.Hostname = hostname
	created := time.Now().Truncate(time.Second)
	id, created, err := uniqueID(input.Config.BackupDir, created)
	if err != nil {
		return BackupResult{}, err
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
		wrapped, wrappedKeyID, err := m.client.WrapPassword(ctx, m.paths.KeysDir, password)
		if err != nil {
			return BackupResult{}, err
		}
		if wrappedKeyID != key.KeyID {
			return BackupResult{}, errors.New("封装密码使用的公钥与当前私钥不匹配，已阻止创建无法恢复的备份")
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
	if protection && (backupResult.ExitCode != 0 || len(backupResult.Warnings) > 0) {
		return BackupResult{}, errors.New("保护快照压缩未完整完成：" + strings.Join(backupResult.Warnings, "；"))
	}
	manifestValue.Archive = manifest.ArchiveInfo{
		Name:   manifest.ArchiveName(manifestValue.ID),
		Size:   backupResult.Size,
		SHA256: backupResult.SHA256,
	}
	if backupResult.Size <= 0 {
		return BackupResult{}, errors.New("压缩结果为空")
	}

	verified := false
	if input.Config.VerifyAfterBackup || protection {
		report("verify", 76.5, "校验归档完整性", 0, backupResult.Size)
		testResult, err := m.client.TestArchive(ctx, input.TaskID, m.sevenZip, outputPath, password, func(event sidecar.Progress) {
			report("verify", 76.5+clamp(event.Percent, 0, 100)*0.095, event.Message, event.BytesDone, event.BytesTotal)
		})
		if err != nil {
			return BackupResult{}, mapTaskError(err)
		}
		warnings = append(warnings, testResult.Warnings...)
		verified = testResult.OK
		if !testResult.OK || (protection && (testResult.ExitCode != 0 || len(testResult.Warnings) > 0)) {
			return BackupResult{}, errors.New("归档完整性校验未通过")
		}
		if protection {
			listing, err := m.client.ListArchive(ctx, m.sevenZip, outputPath, password)
			if err != nil {
				return BackupResult{}, fmt.Errorf("读取保护快照清单失败：%w", err)
			}
			if err := verifyProtectionArchive(stageResult.Entries, listing.Details); err != nil {
				return BackupResult{}, fmt.Errorf("保护快照不完整，已中止恢复：%w", err)
			}
		}
	} else {
		report("verify", 86, "已跳过完整性测试", backupResult.Size, backupResult.Size)
	}

	if encrypted {
		unwrapped, unwrapErr := m.client.UnwrapPassword(ctx, m.paths.KeysDir, manifestValue.Encryption.WrappedPassword)
		if unwrapErr != nil {
			return BackupResult{}, fmt.Errorf("新归档密码无法用落盘私钥解开，未保存：%w", unwrapErr)
		}
		if unwrapped != password {
			return BackupResult{}, errors.New("新归档密码与私钥解封结果不一致，未保存")
		}
	}

	if incomplete || len(warnings) > 0 {
		manifestValue.CaptureStatus = "partial"
	} else {
		manifestValue.CaptureStatus = "complete"
	}
	if err := ctx.Err(); err != nil {
		return BackupResult{}, err
	}
	report("save", 87, "写入备份目录", backupResult.Size, backupResult.Size)
	archivePath := filepath.Join(input.Config.BackupDir, manifestValue.Archive.Name)
	manifestPath := manifest.ManifestPath(archivePath)
	free, err = platform.DiskFree(input.Config.BackupDir)
	if err != nil {
		return BackupResult{}, err
	}
	if free.Free < uint64(backupResult.Size)+16*1024*1024 {
		return BackupResult{}, fmt.Errorf("备份目录空间不足以写入归档（可用 %s）", formatBytes(int64(free.Free)))
	}
	if err := moveFile(ctx, outputPath, archivePath); err != nil {
		return BackupResult{}, fmt.Errorf("写入本地备份失败：%w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(archivePath)
		return BackupResult{}, err
	}
	if err := manifest.Write(manifestPath, manifestValue); err != nil {
		_ = os.Remove(archivePath)
		return BackupResult{}, fmt.Errorf("写入备份清单失败：%w", err)
	}

	snapshot := manifest.FromManifest(manifestValue, archivePath, manifestPath)
	snapshot.Encrypted = manifestValue.Encryption != nil && manifestValue.Encryption.Enabled
	snapshot.Status = "ready"
	if input.Kind != manifest.KindPreRestore {
		if incomplete || len(warnings) > 0 {
			warnings = append(warnings, "本次备份存在未完整的项目，已跳过自动清理旧备份")
		} else {
			report("cleanup", 98, "应用本地保留策略", 0, 0)
			deleted, err := m.applyLocalRetention(ctx, input.Config.BackupDir, input.Config.Retention.Local, manifestValue.ID)
			if err != nil {
				warnings = append(warnings, "本地保留策略执行失败："+err.Error())
			} else if len(deleted) > 0 {
				warnings = append(warnings, "已按保留策略清理本地快照："+strings.Join(deleted, "、"))
			}
		}
	}
	report("done", 100, "备份完成", backupResult.Size, backupResult.Size)
	if m.logger != nil {
		m.logger.Info("backup.complete", "备份任务完成", map[string]any{
			"task":      input.TaskID,
			"id":        manifestValue.ID,
			"size":      backupResult.Size,
			"encrypted": encrypted,
			"verified":  verified,
			"desktop":   manifestValue.DesktopIncluded,
		})
	}
	return BackupResult{
		Snapshot:    snapshot,
		Manifest:    manifestValue,
		ArchivePath: archivePath,
		Warnings:    dedupeWarnings(warnings),
	}, nil
}

func (m *Manager) Restore(ctx context.Context, input RestoreInput) (RestoreResult, error) {
	if input.Snapshot.ID == "" || input.Snapshot.LocalPath == "" {
		return RestoreResult{}, errors.New("未选择可用的本地快照")
	}
	if input.TargetDSH != "" && input.IncludeDesktop {
		return RestoreResult{}, errors.New("恢复到其他目录时不能同时覆盖系统桌面登录态")
	}
	targetDSH := input.TargetDSH
	customTarget := targetDSH != ""
	if targetDSH == "" {
		targetDSH = m.paths.DSHDataDir()
	}
	if !filepath.IsAbs(targetDSH) {
		return RestoreResult{}, errors.New("恢复目标必须是绝对路径")
	}
	targetDSH = filepath.Clean(targetDSH)
	resolvedTarget, err := pathguard.ResolveTarget(targetDSH)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("无法解析恢复目标：%w", err)
	}
	targetDSH = resolvedTarget
	protected := []string{m.paths.AppDir, input.Config.BackupDir, m.paths.DataDir, m.paths.TempDir}
	if customTarget {
		protected = append(protected, m.paths.DSHDataDir(), m.paths.DesktopStateDir())
	}
	for _, root := range protected {
		if root == "" {
			continue
		}
		overlap, err := pathguard.Overlaps(targetDSH, root)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("无法检查恢复目标边界：%w", err)
		}
		if overlap {
			return RestoreResult{}, errors.New("恢复目标不能与当前 DSH 数据、桌面登录态、应用或备份目录重叠")
		}
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

	archivePath := input.Snapshot.LocalPath
	manifestPath := input.Snapshot.ManifestPath
	if manifestPath == "" {
		manifestPath = manifest.ManifestPath(archivePath)
	}
	value, err := manifest.Read(manifestPath)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("读取备份清单失败：%w", err)
	}
	if input.Snapshot.ID != value.ID {
		return RestoreResult{}, errors.New("快照 ID 与备份清单不一致")
	}
	report("precheck", 8, "校验本地归档哈希", 0, value.Archive.Size)
	hash, err := m.client.HashFile(ctx, archivePath)
	if err != nil {
		return RestoreResult{}, err
	}
	if !strings.EqualFold(hash.SHA256, value.Archive.SHA256) {
		return RestoreResult{}, errors.New("归档 SHA-256 校验失败，文件可能已损坏")
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
	required := uint64(0)
	if validation.Bytes > 0 {
		required = uint64(validation.Bytes) + 32*1024*1024
	}
	if required > 0 {
		tempFree, err := platform.DiskFree(taskDir)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("检查恢复暂存空间失败：%w", err)
		}
		if tempFree.Free < required {
			return RestoreResult{}, fmt.Errorf("恢复暂存空间不足：需要至少 %s，可用 %s", formatBytes(int64(required)), formatBytes(int64(tempFree.Free)))
		}
		for _, target := range []string{targetDSH, m.paths.DesktopStateDir()} {
			if target == "" {
				continue
			}
			targetFree, err := platform.DiskFree(target)
			if err != nil {
				return RestoreResult{}, fmt.Errorf("检查恢复目标空间失败：%w", err)
			}
			if targetFree.Free < required {
				return RestoreResult{}, fmt.Errorf("恢复目标空间不足：需要至少 %s，可用 %s", formatBytes(int64(required)), formatBytes(int64(targetFree.Free)))
			}
		}
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

	var targets []sidecar.InstallTarget
	if m.sourceExists(filepath.Join(extractDir, "dsh")) {
		targets = append(targets, sidecar.InstallTarget{Name: "dsh", Path: targetDSH})
	}
	desktopRestored := input.IncludeDesktop && value.DesktopIncluded
	if desktopRestored {
		desktopDir := m.paths.DesktopStateDir()
		if desktopDir == "" || !m.sourceExists(filepath.Join(extractDir, "desktop-state")) {
			return RestoreResult{}, errors.New("桌面登录态的恢复源或目标不可用")
		}
		targets = append(targets, sidecar.InstallTarget{Name: "desktop-state", Path: desktopDir})
	}
	if len(targets) == 0 {
		return RestoreResult{}, errors.New("该快照不包含所选的恢复内容")
	}
	for index := range targets {
		resolved, err := pathguard.ResolveTarget(targets[index].Path)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("无法解析恢复目标：%w", err)
		}
		targets[index].Path = resolved
		for _, root := range []string{m.paths.AppDir, input.Config.BackupDir, m.paths.DataDir, m.paths.TempDir} {
			overlap, err := pathguard.Overlaps(targets[index].Path, root)
			if err != nil {
				return RestoreResult{}, err
			}
			if overlap {
				return RestoreResult{}, errors.New("恢复目标不能与应用或备份目录重叠")
			}
		}
		for other := 0; other < index; other++ {
			overlap, err := pathguard.Overlaps(targets[index].Path, targets[other].Path)
			if err != nil {
				return RestoreResult{}, err
			}
			if overlap {
				return RestoreResult{}, errors.New("多个恢复目标不能互相重叠")
			}
		}
	}
	var preSnapshot *manifest.Snapshot
	if input.PreRestoreSnapshot {
		var sources []sidecar.StageRoot
		for _, target := range targets {
			info, err := os.Stat(target.Path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return RestoreResult{}, fmt.Errorf("无法检查恢复目标：%w", err)
			}
			if !info.IsDir() {
				return RestoreResult{}, errors.New("恢复目标不是目录：" + target.Path)
			}
			sources = append(sources, sidecar.StageRoot{Name: target.Name, Source: target.Path, ExcludeTop: []string{}, ExcludeRel: []string{}})
		}
		report("snapshot", 70, "备份即将覆盖的目标数据", 0, 0)
		preResult, err := m.backup(ctx, BackupInput{
			TaskID: input.TaskID + "-pre",
			Config: input.Config,
			Kind:   manifest.KindPreRestore,
			Force:  true,
			OnProgress: func(progress Progress) {
				report("snapshot", 70+progress.Percent*0.10, "恢复前快照："+progress.Message, progress.BytesDone, progress.BytesTotal)
			},
		}, sources, true)
		if err != nil {
			return RestoreResult{}, fmt.Errorf("创建恢复前快照失败，已中止恢复：%w", err)
		}
		if preResult.Snapshot.ID != "" {
			preSnapshot = &preResult.Snapshot
			warnings = append(warnings, preResult.Warnings...)
		}
	}
	partial := func() RestoreResult {
		return RestoreResult{PreRestoreSnapshot: preSnapshot, DesktopRestored: desktopRestored, TargetDSH: targetDSH}
	}
	if err := ctx.Err(); err != nil {
		return partial(), err
	}
	if running, evidence, err := platform.IsDSHRunning(m.paths.DSHDataDir()); err == nil && running {
		if !input.Force {
			return partial(), &RunningError{Evidence: evidence}
		}
		warnings = append(warnings, "恢复时检测到 DSH 仍在运行，文件覆盖可能不完整")
	}
	report("install", 82, "覆盖恢复文件", 0, 0)
	installResult, err := m.client.InstallStaging(ctx, input.TaskID, extractDir, targets, func(event sidecar.Progress) {
		report("install", 82+clamp(event.Percent, 0, 100)*0.13, event.Message, event.BytesDone, event.BytesTotal)
	})
	if err != nil {
		return partial(), mapTaskError(err)
	}
	warnings = append(warnings, installResult.Warnings...)
	report("verify", 96, "验证恢复结果", 0, 0)
	if err := verifyRestored(entries, targetDSH, desktopRestored, m.paths.DesktopStateDir()); err != nil {
		message := err.Error()
		if preSnapshot != nil {
			message += "；恢复前快照已保留：" + preSnapshot.LocalPath
		}
		return partial(), errors.New(message)
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

func (m *Manager) DeleteLocal(_ context.Context, snapshot manifest.Snapshot, backupDir string) error {
	if snapshot.LocalPath == "" || snapshot.ManifestPath == "" {
		return errors.New("快照没有本地文件")
	}
	inside, err := pathguard.Contains(snapshot.LocalPath, backupDir)
	if err != nil {
		return fmt.Errorf("无法检查归档位置：%w", err)
	}
	if !inside {
		return errors.New("拒绝删除备份目录之外的文件")
	}
	if err := os.Remove(snapshot.LocalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	manifestPaths := append([]string{snapshot.ManifestPath}, snapshot.ManifestAliases...)
	for _, manifestPath := range manifestPaths {
		if err := os.Remove(manifestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("归档已删除，但清理清单失败：%w", err)
		}
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
		if info.CanEncrypt {
			return info, nil
		}
		repaired, err := m.client.KeysEnsure(ctx, m.paths.KeysDir, false)
		if err != nil {
			return sidecar.KeyInfo{}, fmt.Errorf("本地密钥不可用于加密：%w", err)
		}
		if !repaired.CanEncrypt {
			return sidecar.KeyInfo{}, errors.New("本地密钥不可用于加密，请检查 public.key 与 private.key 是否配对")
		}
		return repaired, nil
	}
	if strings.TrimSpace(info.PublicKey) != "" {
		return sidecar.KeyInfo{}, errors.New("检测到公钥但找不到私钥，不会自动生成新密钥；请恢复 data\\keys 目录")
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
	generated, err := m.client.KeysEnsure(ctx, m.paths.KeysDir, true)
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
	for _, source := range []string{m.paths.DSHDataDir(), m.paths.DesktopStateDir(), m.paths.DataDir, m.paths.TempDir} {
		if source == "" {
			continue
		}
		contained, err := pathguard.Contains(input.Config.BackupDir, source)
		if err != nil {
			return fmt.Errorf("无法检查备份目录边界：%w", err)
		}
		if contained {
			return errors.New("备份目录不能位于 DSH 数据或应用 data 目录内")
		}
	}
	if _, err := ensureWritableDir(input.Config.BackupDir); err != nil {
		return fmt.Errorf("本地备份目录不可写：%w", err)
	}
	return nil
}

func (m *Manager) sources(cfg config.Config) ([]sidecar.StageRoot, error) {
	var roots []sidecar.StageRoot
	dshDir := m.paths.DSHDataDir()
	info, err := os.Stat(dshDir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, errors.New("找不到可备份的 DSH 数据：目录不存在 " + dshDir)
	case err != nil:
		return nil, fmt.Errorf("无法读取 DSH 数据目录 %s：%w", dshDir, err)
	case !info.IsDir():
		return nil, errors.New("DSH 数据路径不是目录：" + dshDir)
	default:
		roots = append(roots, sidecar.StageRoot{
			Name:       "dsh",
			Source:     dshDir,
			ExcludeTop: DSHExcludeTop,
			ExcludeRel: DSHExcludeRel,
		})
	}
	if cfg.IncludeDesktop {
		desktopDir := m.paths.DesktopStateDir()
		if desktopDir != "" {
			desktopInfo, desktopErr := os.Stat(desktopDir)
			switch {
			case errors.Is(desktopErr, os.ErrNotExist):
			case desktopErr != nil:
				return nil, fmt.Errorf("无法读取桌面登录态目录 %s：%w", desktopDir, desktopErr)
			case !desktopInfo.IsDir():
				return nil, errors.New("桌面登录态路径不是目录：" + desktopDir)
			default:
				roots = append(roots, sidecar.StageRoot{
					Name:       "desktop-state",
					Source:     desktopDir,
					ExcludeTop: DesktopExcludeTop,
					ExcludeRel: DesktopExcludeRel,
				})
			}
		}
	}
	return roots, nil
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

func verifyProtectionArchive(staged, listed []sidecar.ArchiveEntry) error {
	type fileInfo struct {
		path string
		size int64
	}
	stagedFiles := make(map[string]fileInfo)
	stagedDirs := make(map[string]string)
	for _, entry := range staged {
		key := strings.ToLower(filepath.ToSlash(entry.Path))
		if entry.Folder {
			stagedDirs[key] = entry.Path
		} else {
			stagedFiles[key] = fileInfo{path: entry.Path, size: entry.Size}
		}
	}
	listedFiles := make(map[string]int64)
	listedDirs := make(map[string]bool)
	for _, entry := range listed {
		key := strings.ToLower(strings.TrimRight(filepath.ToSlash(entry.Path), "/"))
		if entry.Folder {
			listedDirs[key] = true
			if _, ok := stagedDirs[key]; !ok {
				return fmt.Errorf("保护快照包含清单外的目录：%s", entry.Path)
			}
			continue
		}
		if _, exists := listedFiles[key]; exists {
			return fmt.Errorf("保护快照包含重复文件：%s", entry.Path)
		}
		listedFiles[key] = entry.Size
	}
	for key, info := range stagedFiles {
		size, ok := listedFiles[key]
		if !ok {
			return fmt.Errorf("保护快照缺少文件：%s", info.path)
		}
		if size != info.size {
			return fmt.Errorf("保护快照文件大小不符：%s（预期 %d，实际 %d）", info.path, info.size, size)
		}
	}
	for key := range listedFiles {
		if _, ok := stagedFiles[key]; !ok {
			return fmt.Errorf("保护快照包含未暂存的文件：%s", key)
		}
	}
	for dirKey, dirPath := range stagedDirs {
		hasChild := false
		for fileKey := range stagedFiles {
			if strings.HasPrefix(fileKey, dirKey+"/") {
				hasChild = true
				break
			}
		}
		if !hasChild {
			for otherDir := range stagedDirs {
				if otherDir != dirKey && strings.HasPrefix(otherDir, dirKey+"/") {
					hasChild = true
					break
				}
			}
		}
		if !hasChild && !listedDirs[dirKey] {
			return fmt.Errorf("保护快照缺少空目录：%s", dirPath)
		}
	}
	return nil
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

func (m *Manager) applyLocalRetention(ctx context.Context, backupDir string, keep int, protectID string) ([]string, error) {
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
		if snapshot.Status != "ready" || snapshot.CaptureStatus == "partial" {
			continue
		}
		candidates = append(candidates, snapshot)
	}
	manifest.Sort(candidates)
	if len(candidates) <= keep {
		return nil, nil
	}

	survivors := make(map[string]bool, keep)
	for index := 0; index < keep && index < len(candidates); index++ {
		survivors[candidates[index].ID] = true
	}
	if protectID != "" && !survivors[protectID] {
		for _, snapshot := range candidates {
			if snapshot.ID == protectID {
				if len(survivors) >= keep {
					var removeID string
					for id := range survivors {
						removeID = id
						break
					}
					delete(survivors, removeID)
				}
				survivors[protectID] = true
				break
			}
		}
	}

	var deleted []string
	for _, snapshot := range candidates {
		if survivors[snapshot.ID] || snapshot.ID == protectID {
			continue
		}
		if err := ctx.Err(); err != nil {
			return deleted, err
		}
		if err := os.Remove(snapshot.LocalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return deleted, fmt.Errorf("删除旧归档 %s 失败，已保留其清单：%w", snapshot.ID, err)
		}
		manifestPaths := append([]string{snapshot.ManifestPath}, snapshot.ManifestAliases...)
		for _, manifestPath := range manifestPaths {
			if err := os.Remove(manifestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return deleted, fmt.Errorf("旧归档 %s 已删除，但清理清单失败：%w", snapshot.ID, err)
			}
		}
		deleted = append(deleted, snapshot.ID)
		if m.logger != nil {
			m.logger.Info("retention.local", "清理旧本地快照", map[string]any{"id": snapshot.ID})
		}
	}
	return deleted, nil
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
		archiveMissing := false
		if _, err := os.Stat(archive); errors.Is(err, os.ErrNotExist) {
			archiveMissing = true
		}
		manifestMissing := false
		if _, err := os.Stat(manifest.ManifestPath(archive)); errors.Is(err, os.ErrNotExist) {
			manifestMissing = true
		}
		if archiveMissing && manifestMissing {
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

func moveFile(ctx context.Context, source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("目标归档已存在，拒绝覆盖")
	} else if !errors.Is(err, os.ErrNotExist) {
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
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	buffer := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			_ = output.Close()
			_ = os.Remove(destination)
			return err
		}
		read, readErr := input.Read(buffer)
		if read > 0 {
			if _, writeErr := output.Write(buffer[:read]); writeErr != nil {
				_ = output.Close()
				_ = os.Remove(destination)
				return writeErr
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = output.Close()
			_ = os.Remove(destination)
			return readErr
		}
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		_ = os.Remove(destination)
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	_ = os.Remove(source)
	return nil
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
