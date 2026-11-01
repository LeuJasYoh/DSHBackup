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
	"sort"
	"strings"
	"time"

	"DSHBackup/internal/apppath"
	"DSHBackup/internal/config"
	"DSHBackup/internal/logging"
	"DSHBackup/internal/manifest"
	"DSHBackup/internal/pathguard"
	"DSHBackup/internal/platform"
	"DSHBackup/internal/scope"
	"DSHBackup/internal/sevenparams"
	"DSHBackup/internal/sidecar"
)

// 排除规则与备份域的定义都在 internal/scope，预览、暂存和恢复共用一张表。

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
	Snapshot     manifest.Snapshot
	Manifest     manifest.Manifest
	Manifests    []manifest.Manifest
	ArchivePath  string
	ArchivePaths []string
	Warnings     []string
}

type RestoreInput struct {
	TaskID             string
	Config             config.Config
	Snapshot           manifest.Snapshot
	Domains            []string
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
		percent := 3 + clamp(event.Percent, 0, 10)/10*11
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

	compressor := sidecar.Compression{
		Level:        sevenparams.LevelMX(input.Config.Compression.Level),
		DictionaryMB: input.Config.Compression.DictionaryMB,
		Threads:      input.Config.Compression.Threads,
		ExtraArgs:    input.Config.Compression.ExtraArgs,
	}
	compressorArgs := sevenparams.BuildArgs(input.Config.Compression.Level, input.Config.Compression.DictionaryMB, input.Config.Compression.Threads, input.Config.Compression.ExtraArgs)
	hostname, _ := os.Hostname()
	created := time.Now().Truncate(time.Second)
	// 恢复前快照按目标整体存档；日常备份按域各成一份归档。只有拆开才能按域
	// 淘汰旧份、按域挑选恢复，也不会为了少留一份插件依赖而重写整个快照。
	works := make([]domainWork, 0, len(stageResult.Domains)+1)
	if protection {
		works = append(works, domainWork{
			name:    manifest.DomainFull,
			dir:     stagingDir,
			files:   stageResult.Files,
			bytes:   stageResult.Bytes,
			entries: stageResult.Entries,
		})
	} else {
		for _, domain := range stageResult.Domains {
			works = append(works, domainWork{
				name:  domain.Name,
				dir:   filepath.Join(stagingDir, domain.Name),
				files: domain.Files,
				bytes: domain.Bytes,
			})
		}
	}
	if len(works) == 0 {
		return BackupResult{}, errors.New("没有可归档的数据")
	}
	sort.SliceStable(works, func(left, right int) bool {
		return domainOrder(works[left].name) < domainOrder(works[right].name)
	})

	domainNames := make([]string, 0, len(works))
	for _, work := range works {
		domainNames = append(domainNames, work.name)
	}
	id, created, err := uniqueID(input.Config.BackupDir, created, domainNames)
	if err != nil {
		return BackupResult{}, err
	}

	encrypted := input.Config.EncryptionLocal
	if encrypted {
		if _, keyErr := m.ensureKeys(ctx, input.Config.BackupDir); keyErr != nil {
			return BackupResult{}, keyErr
		}
	}

	var written []manifest.Manifest
	var archivePaths []string
	// 任一份失败就把本次已落盘的归档与清单一起删掉，避免留下半套快照。
	rollback := func(cause string) (BackupResult, error) {
		for _, value := range written {
			_ = os.Remove(filepath.Join(input.Config.BackupDir, value.Archive.Name))
			_ = os.Remove(manifest.ManifestPath(filepath.Join(input.Config.BackupDir, value.Archive.Name)))
		}
		return BackupResult{}, errors.New(cause)
	}

	completed := int64(0)
	grandTotal := maxInt64(stageResult.Bytes, 1)
	for index, work := range works {
		label := domainLabel(work.name)
		archiveName := manifest.DomainArchiveName(id, work.name)
		outputPath := filepath.Join(taskDir, archiveName)
		password := ""
		var encryption *manifest.EncryptionInfo
		if encrypted {
			password, err = randomPassword()
			if err != nil {
				return BackupResult{}, err
			}
			wrapped, wrappedKeyID, wrapErr := m.client.WrapPassword(ctx, m.paths.KeysDir, password)
			if wrapErr != nil {
				return rollback("封装归档密码失败：" + wrapErr.Error())
			}
			info, keyErr := m.client.KeysInfo(ctx, m.paths.KeysDir)
			if keyErr != nil || !strings.EqualFold(info.KeyID, wrappedKeyID) {
				return rollback("封装密码使用的公钥与当前私钥不匹配，已阻止创建无法恢复的备份")
			}
			encryption = &manifest.EncryptionInfo{
				Enabled:         true,
				Algorithm:       "7z-aes256+age-x25519",
				KeyID:           info.KeyID,
				WrappedPassword: wrapped,
			}
		}

		// 每份归档按自己的字节占比拿到 14→90 之间的一段，段内再分压缩/校验/落盘，
		// 否则多份归档会让进度条来回跳。
		share := float64(maxInt64(work.bytes, 1)) / float64(grandTotal)
		spanStart := 14 + 76*float64(completed)/float64(grandTotal)
		width := 76 * share
		base := spanStart
		message := "压缩 " + label
		if len(works) > 1 {
			message = fmt.Sprintf("压缩 %s（%d/%d）", label, index+1, len(works))
		}
		report("compress", base, message, completed, stageResult.Bytes)
		backupResult, backupErr := m.client.Backup(ctx, input.TaskID, sidecar.BackupRequest{
			SevenZip:    m.sevenZip,
			StagingDir:  work.dir,
			Output:      outputPath,
			Password:    password,
			Compression: compressor,
			TotalBytes:  work.bytes,
		}, func(event sidecar.Progress) {
			percent := spanStart + width*0.6*clamp(event.Percent, 0, 100)/100
			report("compress", percent, label+" "+event.Message, completed+event.BytesDone, stageResult.Bytes)
		})
		if backupErr != nil {
			return BackupResult{}, mapTaskError(backupErr)
		}
		warnings = append(warnings, backupResult.Warnings...)
		if protection && (backupResult.ExitCode != 0 || len(backupResult.Warnings) > 0) {
			return BackupResult{}, errors.New("保护快照压缩未完整完成：" + strings.Join(backupResult.Warnings, "；"))
		}
		if backupResult.Size <= 0 {
			return BackupResult{}, errors.New("压缩结果为空")
		}

		verified := false
		if input.Config.VerifyAfterBackup || protection {
			start := spanStart + width*0.6
			report("verify", start, "校验归档完整性："+label, completed, stageResult.Bytes)
			testResult, testErr := m.client.TestArchive(ctx, input.TaskID, m.sevenZip, outputPath, password, func(event sidecar.Progress) {
				report("verify", start+width*0.35*clamp(event.Percent, 0, 100)/100, event.Message, completed+event.BytesDone, stageResult.Bytes)
			})
			if testErr != nil {
				return BackupResult{}, mapTaskError(testErr)
			}
			warnings = append(warnings, testResult.Warnings...)
			verified = testResult.OK
			if !testResult.OK || (protection && (testResult.ExitCode != 0 || len(testResult.Warnings) > 0)) {
				return rollback(label + " 的完整性校验未通过，未保存")
			}
			if protection {
				listing, listErr := m.client.ListArchive(ctx, m.sevenZip, outputPath, password)
				if listErr != nil {
					return BackupResult{}, fmt.Errorf("读取保护快照清单失败：%w", listErr)
				}
				if verifyErr := verifyProtectionArchive(work.entries, listing.Details); verifyErr != nil {
					return BackupResult{}, fmt.Errorf("保护快照不完整，已中止恢复：%w", verifyErr)
				}
			}
		} else {
			report("verify", spanStart+width*0.95, "已跳过完整性测试", backupResult.Size, backupResult.Size)
		}
		if encrypted {
			unwrapped, unwrapErr := m.client.UnwrapPassword(ctx, m.paths.KeysDir, encryption.WrappedPassword)
			if unwrapErr != nil {
				return rollback(fmt.Sprintf("%s 的新归档密码无法用落盘私钥解开，未保存", label))
			}
			if unwrapped != password {
				return rollback(label + " 的新归档密码与私钥解封结果不一致，未保存")
			}
		}
		if verified && input.Config.VerifyAfterBackup {
			report("verify", spanStart+width*0.95, label+" 校验通过", completed, stageResult.Bytes)
		}

		captureStatus := "complete"
		if incomplete || len(warnings) > 0 {
			captureStatus = "partial"
		}
		value := manifest.Manifest{
			SchemaVersion:   manifest.SchemaVersion,
			ID:              id,
			Domain:          work.name,
			CreatedAt:       created.Format(time.RFC3339),
			Kind:            input.Kind,
			AppVersion:      m.appVersion,
			Hostname:        hostname,
			Scope:           m.rootsUnder(work.dir),
			DesktopIncluded: m.sourceExists(filepath.Join(work.dir, "desktop-state")),
			Files:           manifest.FileStats{Files: work.files, Bytes: work.bytes},
			Archive:         manifest.ArchiveInfo{Name: archiveName, Size: backupResult.Size, SHA256: backupResult.SHA256},
			Encryption:      encryption,
			Compressor:      manifest.CompressorInfo{Name: "7-Zip", Version: m.sevenZipVersion, Args: compressorArgs},
			CaptureStatus:   captureStatus,
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return BackupResult{}, ctxErr
		}
		archivePath := filepath.Join(input.Config.BackupDir, archiveName)
		if free.Free < uint64(backupResult.Size)+16*1024*1024 {
			return rollback(fmt.Sprintf("备份目录空间不足以写入 %s（可用 %s）", archiveName, formatBytes(int64(free.Free))))
		}
		report("save", spanStart+width*0.97, "写入备份目录："+archiveName, backupResult.Size, backupResult.Size)
		if moveErr := moveFile(ctx, outputPath, archivePath); moveErr != nil {
			return rollback("写入本地备份失败：" + moveErr.Error())
		}
		if writeErr := manifest.Write(manifest.ManifestPath(archivePath), value); writeErr != nil {
			_ = os.Remove(archivePath)
			return rollback("写入备份清单失败：" + writeErr.Error())
		}
		written = append(written, value)
		archivePaths = append(archivePaths, archivePath)
		completed = maxInt64(completed+work.bytes, completed)
	}

	snapshot, aggregateWarnings := manifest.SnapshotFromManifests(written, input.Config.BackupDir)
	warnings = append(warnings, aggregateWarnings...)
	if input.Kind != manifest.KindPreRestore {
		if incomplete || len(warnings) > 0 {
			warnings = append(warnings, "本次备份存在未完整的项目，已跳过自动清理旧备份")
		} else {
			report("cleanup", 98, "应用分域保留策略", 0, 0)
			moved, retentionErr := m.applyDomainRetention(ctx, input.Config.BackupDir, input.Config.Retention, id, input.Config.Retention.TrashDays)
			if retentionErr != nil {
				warnings = append(warnings, "本地保留策略执行失败："+retentionErr.Error())
			} else if len(moved) > 0 {
				warnings = append(warnings, "已按保留策略把旧备份移入回收站："+strings.Join(moved, "、"))
			}
		}
	}
	report("done", 100, "备份完成", stageResult.Bytes, stageResult.Bytes)
	if m.logger != nil {
		m.logger.Info("backup.complete", "备份任务完成", map[string]any{
			"task":      input.TaskID,
			"id":        id,
			"archives":  len(written),
			"size":      snapshot.Size,
			"encrypted": encrypted,
			"desktop":   snapshot.DesktopIncluded,
		})
	}
	return BackupResult{
		Snapshot:     snapshot,
		Manifest:     written[0],
		Manifests:    written,
		ArchivePath:  archivePaths[0],
		ArchivePaths: archivePaths,
		Warnings:     dedupeWarnings(warnings),
	}, nil
}

// domainWork 是一份待归档的域子树。
type domainWork struct {
	name    string
	dir     string
	files   int64
	bytes   int64
	entries []sidecar.ArchiveEntry
}

// domainOrder 固定归档与界面里的域顺序；恢复前快照的 full 排在最后。
func domainOrder(name string) int {
	for index, candidate := range scope.Names() {
		if string(candidate) == name {
			return index
		}
	}
	return len(scope.Names())
}

// rootsUnder 列出域目录里实际存在的根，作为该份归档的 scope。
func (m *Manager) rootsUnder(dir string) []string {
	var roots []string
	for _, name := range []string{"dsh", "desktop-state"} {
		if m.sourceExists(filepath.Join(dir, name)) {
			roots = append(roots, name)
		}
	}
	return roots
}

// stagedIn 还原条目在分域暂存树里的位置：staging/<域>/<根>/<相对路径>。
func stagedIn(stagingDir, rootRelative string) string {
	domain := scope.DomainOf(rootRelative)
	return filepath.Join(stagingDir, string(domain), filepath.FromSlash(rootRelative))
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

// restoreRoot 是"某个解压子目录里的某个根 → 目标路径"。
type restoreRoot struct {
	staging string
	name    string
	path    string
}

// installGroup 是一次 InstallStaging 调用：一棵解压树加上它要写入的目标。
type installGroup struct {
	staging string
	targets []sidecar.InstallTarget
}

// groupRoots 按解压目录分组并保持首次出现顺序，InstallStaging 一次只处理一棵树。
func groupRoots(roots []restoreRoot) []installGroup {
	var groups []installGroup
	index := make(map[string]int)
	for _, root := range roots {
		position, exists := index[root.staging]
		if !exists {
			index[root.staging] = len(groups)
			groups = append(groups, installGroup{staging: root.staging})
			position = len(groups) - 1
		}
		groups[position].targets = append(groups[position].targets, sidecar.InstallTarget{Name: root.name, Path: root.path})
	}
	return groups
}

func containsRoot(roots []restoreRoot, name string) bool {
	for _, root := range roots {
		if root.name == name {
			return true
		}
	}
	return false
}

// domainLabel 给界面与进度条用的域名，恢复前快照单独一类。
func domainLabel(name string) string {
	if name == manifest.DomainFull {
		return "快照内容"
	}
	return scope.Domain(name).Label()
}

// partFolder 给出某个域归档的解压子目录名。
func partFolder(part manifest.Part, index int) string {
	if part.Domain != "" {
		return part.Domain
	}
	return fmt.Sprintf("part-%d", index)
}

func (m *Manager) Restore(ctx context.Context, input RestoreInput) (RestoreResult, error) {
	if input.Snapshot.ID == "" {
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

	parts, err := selectParts(input.Snapshot, input.Domains)
	if err != nil {
		return RestoreResult{}, err
	}
	// 先逐份校验哈希与结构，再统一解压：任何一份损坏都不该让整次恢复半途写坏目标。
	report("precheck", 8, "校验本地归档哈希", 0, 0)
	entries := make(map[string]bool)
	passwords := make([]string, len(parts))
	var stagedBytes, archiveBytes int64
	for index, part := range parts {
		value, readErr := manifest.Read(part.ManifestPath)
		if readErr != nil {
			return RestoreResult{}, fmt.Errorf("读取备份清单失败：%w", readErr)
		}
		if value.ID != input.Snapshot.ID || value.Archive.Name != part.ArchiveName {
			return RestoreResult{}, errors.New("快照 ID 与备份清单不一致")
		}
		hash, hashErr := m.client.HashFile(ctx, part.LocalPath)
		if hashErr != nil {
			return RestoreResult{}, hashErr
		}
		if !strings.EqualFold(hash.SHA256, value.Archive.SHA256) {
			return RestoreResult{}, fmt.Errorf("%s 的 SHA-256 校验失败，文件可能已损坏", part.ArchiveName)
		}
		password := ""
		if value.Encryption != nil && value.Encryption.Enabled {
			report("decrypt", 14, "读取本地私钥", 0, 0)
			password, err = m.client.UnwrapPassword(ctx, m.paths.KeysDir, value.Encryption.WrappedPassword)
			if err != nil {
				return RestoreResult{}, fmt.Errorf("解密 %s 失败：%w", part.ArchiveName, err)
			}
		}
		passwords[index] = password
		validation, validationErr := m.client.ValidateArchive(ctx, m.sevenZip, part.LocalPath, password)
		if validationErr != nil {
			return RestoreResult{}, mapTaskError(validationErr)
		}
		stagedBytes += validation.Bytes
		if info, statErr := os.Stat(part.LocalPath); statErr == nil {
			archiveBytes += info.Size()
		}
		for _, entry := range validation.Entries {
			entries[entry] = true
		}
	}
	riskErr := checkRestoreFootprint(stagedBytes, archiveBytes)
	if riskErr != nil && !input.Force {
		return RestoreResult{}, riskErr
	}
	required := uint64(0)
	if stagedBytes > 0 {
		required = uint64(stagedBytes) + 32*1024*1024
	}
	if required > 0 {
		tempFree, freeErr := platform.DiskFree(taskDir)
		if freeErr != nil {
			return RestoreResult{}, fmt.Errorf("检查恢复暂存空间失败：%w", freeErr)
		}
		if tempFree.Free < required {
			return RestoreResult{}, fmt.Errorf("恢复暂存空间不足：需要至少 %s，可用 %s", formatBytes(int64(required)), formatBytes(int64(tempFree.Free)))
		}
		for _, target := range []string{targetDSH, m.paths.DesktopStateDir()} {
			if target == "" {
				continue
			}
			targetFree, targetErr := platform.DiskFree(target)
			if targetErr != nil {
				return RestoreResult{}, fmt.Errorf("检查恢复目标空间失败：%w", targetErr)
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
	var warnings []string
	steps := float64(len(parts))
	// 各域分别解压到自己的子目录：归档内部路径都带 dsh/ 前缀，混在同一层会互相
	// 覆盖，也会撞上"解压目录必须为空"的前置检查。
	desktopDir := m.paths.DesktopStateDir()
	var roots []restoreRoot
	for index, part := range parts {
		label := part.Label
		if label == "" {
			label = "整份归档"
		}
		partDir := filepath.Join(extractDir, partFolder(part, index))
		if err := os.MkdirAll(partDir, 0o700); err != nil {
			return RestoreResult{}, err
		}
		extractResult, extractErr := m.client.Extract(ctx, input.TaskID, m.sevenZip, part.LocalPath, partDir, passwords[index], func(event sidecar.Progress) {
			progress := float64(index) + clamp(event.Percent, 0, 100)/100
			report("extract", 30+progress/steps*28, label+"："+event.Message, event.BytesDone, event.BytesTotal)
		})
		if extractErr != nil {
			return RestoreResult{}, mapTaskError(extractErr)
		}
		warnings = append(warnings, extractResult.Warnings...)
		for _, name := range []string{"dsh", "desktop-state"} {
			if !m.sourceExists(filepath.Join(partDir, name)) {
				continue
			}
			if name == "desktop-state" && !input.IncludeDesktop {
				continue
			}
			target := targetDSH
			if name == "desktop-state" {
				if desktopDir == "" {
					return RestoreResult{}, errors.New("桌面登录态的恢复目标不可用")
				}
				target = desktopDir
			}
			roots = append(roots, restoreRoot{staging: partDir, name: name, path: target})
		}
	}
	if riskErr != nil {
		warnings = append(warnings, "归档压缩比异常，已按用户确认继续恢复")
	}

	desktopRestored := input.IncludeDesktop && containsRoot(roots, "desktop-state")
	if len(roots) == 0 {
		return RestoreResult{}, errors.New("该快照不包含所选的恢复内容")
	}
	// 解析并校验每个目标路径一次；多个域归档写同一目录的不同子树是正常情况。
	resolvedPaths := make(map[string]string, len(roots))
	uniqueTargets := make([]sidecar.InstallTarget, 0, len(roots))
	for _, root := range roots {
		key := strings.ToLower(filepath.Clean(root.path))
		if _, done := resolvedPaths[key]; done {
			continue
		}
		resolved, resolveErr := pathguard.ResolveTarget(root.path)
		if resolveErr != nil {
			return RestoreResult{}, fmt.Errorf("无法解析恢复目标：%w", resolveErr)
		}
		resolvedPaths[key] = resolved
		uniqueTargets = append(uniqueTargets, sidecar.InstallTarget{Name: root.name, Path: resolved})
	}
	for index, target := range uniqueTargets {
		resolvedPaths[strings.ToLower(filepath.Clean(target.Path))] = target.Path
		for _, protected := range []string{m.paths.AppDir, input.Config.BackupDir, m.paths.DataDir, m.paths.TempDir} {
			overlap, overlapErr := pathguard.Overlaps(target.Path, protected)
			if overlapErr != nil {
				return RestoreResult{}, overlapErr
			}
			if overlap {
				return RestoreResult{}, errors.New("恢复目标不能与应用或备份目录重叠")
			}
		}
		for other := 0; other < index; other++ {
			overlap, overlapErr := pathguard.Overlaps(target.Path, uniqueTargets[other].Path)
			if overlapErr != nil {
				return RestoreResult{}, overlapErr
			}
			if overlap {
				return RestoreResult{}, errors.New("多个恢复目标不能互相重叠")
			}
		}
	}
	targets := make([]sidecar.InstallTarget, 0, len(roots))
	for index := range roots {
		roots[index].path = resolvedPaths[strings.ToLower(filepath.Clean(roots[index].path))]
		targets = append(targets, sidecar.InstallTarget{Name: roots[index].name, Path: roots[index].path})
	}
	candidateTargets := make([]string, 0, len(targets))
	for _, target := range targets {
		candidateTargets = append(candidateTargets, target.Path)
	}
	if candidates := RestoreCandidates(candidateTargets); len(candidates) > 0 {
		warnings = append(warnings, fmt.Sprintf("恢复目标中发现 %d 个上次异常退出留下的候选文件（例如 %s）；程序不会自动删除，请确认无用后手动清理", len(candidates), candidates[0]))
	}
	var preSnapshot *manifest.Snapshot
	if input.PreRestoreSnapshot {
		var sources []sidecar.StageRoot
		for _, target := range uniqueTargets {
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
			rootName := target.Name
			if rootName == "dsh" {
				sources = append(sources, dshRoot(target.Path, input.Config.Domains.Selection(), false))
				continue
			}
			desktop := scope.Desktop()
			sources = append(sources, sidecar.StageRoot{
				Name:          rootName,
				Source:        target.Path,
				ExcludeTop:    desktop.Top,
				ExcludeRel:    desktop.Rel,
				ExcludePrefix: []string{},
				ExcludeName:   []string{},
			})
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
	installFiles, installBytes := int64(0), int64(0)
	step := float64(0)
	groups := groupRoots(roots)
	steps = float64(len(groups))
	for _, group := range groups {
		installResult, installErr := m.client.InstallStaging(ctx, input.TaskID, group.staging, group.targets, func(event sidecar.Progress) {
			report("install", 82+(step+clamp(event.Percent, 0, 100)/100)/steps*13, event.Message, event.BytesDone, event.BytesTotal)
		})
		if installErr != nil {
			return partial(), mapTaskError(installErr)
		}
		warnings = append(warnings, installResult.Warnings...)
		installFiles += installResult.Files
		installBytes += installResult.Bytes
		step++
	}
	report("verify", 96, "验证恢复结果", 0, 0)
	if err := verifyRestored(entries, targetDSH, desktopRestored, m.paths.DesktopStateDir()); err != nil {
		message := err.Error()
		if preSnapshot != nil {
			message += "；恢复前快照已保留：" + preSnapshot.LocalPath
		}
		return partial(), errors.New(message)
	}
	report("done", 100, "恢复完成", installBytes, installBytes)
	if m.logger != nil {
		m.logger.Info("restore.complete", "恢复任务完成", map[string]any{
			"task":    input.TaskID,
			"id":      input.Snapshot.ID,
			"files":   installFiles,
			"desktop": desktopRestored,
		})
	}
	return RestoreResult{
		Files:              installFiles,
		Bytes:              installBytes,
		PreRestoreSnapshot: preSnapshot,
		Warnings:           dedupeWarnings(warnings),
		DesktopRestored:    desktopRestored,
		TargetDSH:          targetDSH,
	}, nil
}

func (m *Manager) VerifyLocal(ctx context.Context, taskID string, snapshot manifest.Snapshot, onProgress func(Progress)) error {
	parts, err := selectParts(snapshot, nil)
	if err != nil {
		return err
	}
	steps := float64(len(parts))
	for index, part := range parts {
		value, readErr := manifest.Read(part.ManifestPath)
		if readErr != nil {
			return readErr
		}
		label := part.Label
		if label == "" {
			label = "归档"
		}
		report := func(phase string, within float64, message string) {
			if onProgress != nil {
				onProgress(Progress{Phase: phase, Percent: (float64(index) + within) / steps * 100, Message: message, BytesTotal: part.Size})
			}
		}
		report("verify", 0.1, "校验 "+label+" 大小")
		stat, statErr := os.Stat(part.LocalPath)
		if statErr != nil {
			return statErr
		}
		if stat.Size() != value.Archive.Size {
			return fmt.Errorf("%s 大小与 manifest 不一致", part.ArchiveName)
		}
		report("verify", 0.2, "计算 "+label+" SHA-256")
		hash, hashErr := m.client.HashFile(ctx, part.LocalPath)
		if hashErr != nil {
			return hashErr
		}
		if !strings.EqualFold(hash.SHA256, value.Archive.SHA256) {
			return fmt.Errorf("%s 归档 SHA-256 校验失败", part.ArchiveName)
		}
		password := ""
		if value.Encryption != nil && value.Encryption.Enabled {
			password, err = m.client.UnwrapPassword(ctx, m.paths.KeysDir, value.Encryption.WrappedPassword)
			if err != nil {
				return fmt.Errorf("无法解密归档密码：%w", err)
			}
		}
		testResult, testErr := m.client.TestArchive(ctx, taskID, m.sevenZip, part.LocalPath, password, func(event sidecar.Progress) {
			report("verify", 0.3+clamp(event.Percent, 0, 100)/100*0.6, event.Message)
		})
		if testErr != nil {
			return mapTaskError(testErr)
		}
		if !testResult.OK {
			return fmt.Errorf("%s 的 7z 完整性测试未通过", part.ArchiveName)
		}
	}
	if onProgress != nil {
		onProgress(Progress{Phase: "done", Percent: 100, Message: "验证通过"})
	}
	return nil
}

// selectParts 挑出要恢复的域归档。v1 单归档快照没有域列表，这里合成一份“整档”，
// 让新老格式走同一条恢复路径。
func selectParts(snapshot manifest.Snapshot, requested []string) ([]manifest.Part, error) {
	available := snapshot.Parts
	if len(available) == 0 {
		if snapshot.LocalPath == "" {
			return nil, errors.New("快照没有本地归档")
		}
		manifestPath := snapshot.ManifestPath
		if manifestPath == "" {
			manifestPath = manifest.ManifestPath(snapshot.LocalPath)
		}
		status := snapshot.Status
		if status == "partial" {
			status = "ready"
		}
		available = []manifest.Part{{
			Label:        "整份归档",
			ArchiveName:  filepath.Base(snapshot.LocalPath),
			LocalPath:    snapshot.LocalPath,
			ManifestPath: manifestPath,
			Size:         snapshot.Size,
			SHA256:       snapshot.SHA256,
			Files:        snapshot.FileCount,
			Bytes:        snapshot.TotalBytes,
			Status:       status,
		}}
	}
	if len(requested) == 0 {
		var ready []manifest.Part
		for _, part := range available {
			if part.Status == "ready" {
				ready = append(ready, part)
			}
		}
		if len(ready) == 0 {
			return nil, errors.New("该快照没有可用的归档")
		}
		return ready, nil
	}
	wanted := make(map[string]bool, len(requested))
	for _, name := range requested {
		wanted[name] = true
	}
	var picked []manifest.Part
	for _, part := range available {
		if !wanted[part.Domain] {
			continue
		}
		if part.Status != "ready" {
			return nil, fmt.Errorf("%s 的归档不可用（%s）", part.Label, part.Status)
		}
		picked = append(picked, part)
		delete(wanted, part.Domain)
	}
	for name := range wanted {
		return nil, fmt.Errorf("该快照不包含%s的归档", scope.Domain(name).Label())
	}
	if len(picked) == 0 {
		return nil, errors.New("未选择要恢复的内容")
	}
	return picked, nil
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
		roots = append(roots, dshRoot(dshDir, cfg.Domains.Selection(), true))
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
					Name:          "desktop-state",
					Source:        desktopDir,
					ExcludeTop:    scope.Desktop().Top,
					ExcludeRel:    scope.Desktop().Rel,
					ExcludePrefix: []string{},
					ExcludeName:   []string{},
					// 桌面登录态整体属于 core 域，规则为空但启用域布局。
					DefaultDomain: string(scope.DomainCore),
				})
			}
		}
	}
	return roots, nil
}

// dshRoot 用当前域选择构造 .dsh 根的排除清单。恢复前快照也走这里：跳过的域正是
// "能在目标机重新生成"的域，归档不会写到它们，因此无需保护。
// split 为真时按域分目录落盘（日常备份）；为假时保持扁平整体（恢复前快照）。
func dshRoot(source string, selection scope.Selection, split bool) sidecar.StageRoot {
	excludes := scope.DSH(selection)
	root := sidecar.StageRoot{
		Name:          "dsh",
		Source:        source,
		ExcludeTop:    excludes.Top,
		ExcludeRel:    excludes.Rel,
		ExcludePrefix: excludes.Prefix,
		ExcludeName:   excludes.Name,
	}
	if split {
		for _, rule := range scope.DSHRules() {
			root.DomainRules = append(root.DomainRules, sidecar.DomainRule{
				Name:     string(rule.Name),
				Prefixes: append([]string{}, rule.Prefixes...),
				Segments: append([]string{}, rule.Segments...),
			})
		}
		root.DefaultDomain = string(scope.DefaultDomain)
	}
	return root
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
		targetPath := stagedIn(staging, "dsh/"+relative)
		if _, err := os.Stat(targetPath); err != nil {
			return nil, fmt.Errorf("关键数据未能暂存：%s（文件可能被占用）", relative)
		}
	}
	if sourceSessions := countFiles(filepath.Join(dshSource, "sessions")); sourceSessions > 0 {
		if stagedSessions := countFiles(stagedIn(staging, "dsh/sessions")); stagedSessions == 0 {
			warnings = append(warnings, "会话目录存在文件，但没有成功暂存任何会话；请关闭 DSH 后重试")
		}
	}
	if includeDesktop {
		desktop := m.paths.DesktopStateDir()
		if desktop != "" {
			if _, err := os.Stat(filepath.Join(desktop, "Local State")); err == nil {
				if _, err := os.Stat(stagedIn(staging, "desktop-state/Local State")); err != nil {
					return nil, errors.New("桌面登录态未能完整暂存：Local State 缺失")
				}
			}
			for _, relative := range []string{"Network/Cookies", "Preferences", "Local Storage", "Session Storage"} {
				if _, err := os.Stat(filepath.Join(desktop, relative)); err == nil {
					if _, err := os.Stat(stagedIn(staging, "desktop-state/"+relative)); err != nil {
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

// applyDomainRetention 按域各自保留 N 份，并把超出的那一份移入回收站而不是直接
// 删除，所以保留份数设错时仍能在回收站保留期内找回。
// 必须按域淘汰：插件依赖只在装卸时变化，不该跟着天天变的会话一起被重写。
func (m *Manager) applyDomainRetention(ctx context.Context, backupDir string, retention config.Retention, protectID string, trashDays int) ([]string, error) {
	snapshots, _, err := manifest.Scan(backupDir)
	if err != nil {
		return nil, err
	}
	manifest.Sort(snapshots)
	keeps := retention.Keeps()
	var moved []string
	for _, domain := range scope.Names() {
		keep := keeps[domain]
		if keep < 1 {
			keep = 1
		}
		candidates := collectDomainCandidates(snapshots, string(domain), keep)
		if len(candidates) <= keep {
			continue
		}
		survivors := make(map[string]bool, keep)
		for index := 0; index < keep; index++ {
			survivors[candidates[index].snapshot.ID] = true
		}
		// 本次新建的快照必须活着；必要时挤掉最旧的一份存活者。
		if protectID != "" && !survivors[protectID] {
			for _, item := range candidates {
				if item.snapshot.ID != protectID {
					continue
				}
				if len(survivors) >= keep {
					for index := len(candidates) - 1; index >= 0; index-- {
						id := candidates[index].snapshot.ID
						if id == protectID || !survivors[id] {
							continue
						}
						delete(survivors, id)
						break
					}
				}
				survivors[protectID] = true
				break
			}
		}
		for _, item := range candidates {
			if survivors[item.snapshot.ID] || item.snapshot.ID == protectID {
				continue
			}
			if err := ctx.Err(); err != nil {
				return moved, err
			}
			var moveErr error
			if item.legacy {
				_, moveErr = MoveToTrash(backupDir, item.snapshot, TrashReasonRetention, trashDays, m.appVersion)
			} else {
				_, moveErr = MovePartToTrash(backupDir, item.snapshot, item.part, TrashReasonRetention, trashDays, m.appVersion)
			}
			if moveErr != nil {
				return moved, fmt.Errorf("把旧归档 %s 移入回收站失败：%w", item.snapshot.ID, moveErr)
			}
			label := domain.Label()
			if item.legacy {
				label = "整份"
			}
			moved = append(moved, item.snapshot.ID+"（"+label+"）")
			if m.logger != nil {
				m.logger.Info("retention.local", "旧本地快照移入回收站", map[string]any{"id": item.snapshot.ID, "domain": string(domain)})
			}
		}
	}
	return moved, nil
}

type retentionCandidate struct {
	snapshot manifest.Snapshot
	part     manifest.Part
	legacy   bool
}

// collectDomainCandidates 列出该域还就绪的历史份数，按新→旧排序。
// v1 单归档快照包含所有域，只在第一个域上按“会话”份数参与淘汰，避免同一份
// 旧归档被四个域各算一次。
func collectDomainCandidates(snapshots []manifest.Snapshot, domain string, keep int) []retentionCandidate {
	var candidates []retentionCandidate
	for _, snapshot := range snapshots {
		if snapshot.Kind == manifest.KindPreRestore || snapshot.CaptureStatus == "partial" {
			continue
		}
		if len(snapshot.Parts) == 0 {
			if domain != string(scope.Names()[0]) || snapshot.Status != "ready" {
				continue
			}
			candidates = append(candidates, retentionCandidate{snapshot: snapshot, legacy: true})
			continue
		}
		for _, part := range snapshot.Parts {
			if part.Domain != domain || part.Status != "ready" {
				continue
			}
			candidates = append(candidates, retentionCandidate{snapshot: snapshot, part: part})
		}
	}
	return candidates
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

// uniqueID 生成秒级快照 ID。必须检查该 ID 下每一份可能落盘的名字：分域之后
// 归档名带域名，只查 1.x 的裸名字会漏判，同一秒内的两次备份会互相覆盖。
func uniqueID(dir string, created time.Time, domains []string) (string, time.Time, error) {
	for attempt := 0; attempt < 120; attempt++ {
		id := manifest.NewID(created)
		if !snapshotIDTaken(dir, id, domains) {
			return id, created, nil
		}
		created = created.Add(time.Second)
	}
	return "", created, errors.New("无法生成唯一的备份 ID")
}

func snapshotIDTaken(dir, id string, domains []string) bool {
	names := make([]string, 0, len(domains)+1)
	names = append(names, manifest.ArchiveName(id)) // 旧版单归档的名字也要避开
	for _, domain := range domains {
		names = append(names, manifest.DomainArchiveName(id, domain))
	}
	for _, name := range names {
		archive := filepath.Join(dir, name)
		if _, err := os.Stat(archive); err == nil {
			return true
		}
		if _, err := os.Stat(manifest.ManifestPath(archive)); err == nil {
			return true
		}
	}
	return false
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
