package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"DSHBackup/internal/apppath"
	"DSHBackup/internal/backup"
	"DSHBackup/internal/config"
	"DSHBackup/internal/logging"
	"DSHBackup/internal/manifest"
	"DSHBackup/internal/platform"
	"DSHBackup/internal/sidecar"
)

var (
	Version   = "1.1.0"
	BuildTime = "dev"
)

const expected7zSHA256 = "7ddf0a3bc451e166679296be630d6f0573eb386159c04184761c43020c484291"
const runningPrefix = "DSH_RUNNING:"

type activeTask struct {
	ID     string
	Kind   string
	cancel context.CancelFunc
}

type App struct {
	ctx   context.Context
	paths apppath.Paths

	logger  *logging.Logger
	sidecar *sidecar.Client

	cfg        config.Config
	cfgMu      sync.RWMutex
	cfgWriteMu sync.Mutex

	sevenZipPath    string
	sevenZipVersion string
	sevenZipSHA     string
	sevenZipOK      bool
	sevenZipError   string
	sidecarPath     string
	sidecarError    string
	initWarnings    []string

	taskMu sync.Mutex
	active *activeTask

	schedulerStop chan struct{}
	schedulerDone chan struct{}
	stopOnce      sync.Once

	sourceMu sync.Mutex
	sources  []SourceInfo
	sourceAt time.Time
}

func NewApp(paths apppath.Paths) *App {
	return &App{paths: paths}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.schedulerStop = make(chan struct{})
	a.schedulerDone = make(chan struct{})
	a.addWarning(a.paths.Ensure())

	logger, err := logging.New(a.paths.LogsDir)
	if err != nil {
		a.initWarnings = append(a.initWarnings, "日志初始化失败："+err.Error())
	} else {
		a.logger = logger
	}
	a.addWarning(a.paths.CleanTemp())

	cfg, loadErr := config.Load(a.paths.ConfigFile(), a.paths.DefaultBackupDir)
	if loadErr != nil {
		a.initWarnings = append(a.initWarnings, "配置加载失败，已使用默认值："+loadErr.Error())
	}
	a.cfg = cfg

	a.sidecarPath = a.paths.LocateSidecar()
	if a.sidecarPath == "" {
		a.sidecarError = "找不到 Rust sidecar（dsh-backup-core.exe）"
		a.initWarnings = append(a.initWarnings, a.sidecarError)
	} else {
		client, err := sidecar.Start(a.sidecarPath, a.logger)
		if err != nil {
			a.sidecarError = err.Error()
			a.initWarnings = append(a.initWarnings, a.sidecarError)
		} else {
			a.sidecar = client
		}
	}

	a.sevenZipPath = a.paths.LocateSevenZip()
	if a.sevenZipPath == "" {
		a.sevenZipError = "找不到内置的 7za.exe"
		a.sevenZipOK = false
		a.initWarnings = append(a.initWarnings, a.sevenZipError)
	} else if a.sidecar != nil {
		checkCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		check, checkErr := a.sidecar.Check7z(checkCtx, a.sevenZipPath)
		cancel()
		if checkErr != nil {
			a.sevenZipError = "7za.exe 检查失败：" + checkErr.Error()
			a.initWarnings = append(a.initWarnings, a.sevenZipError)
		} else {
			a.sevenZipVersion = check.Version
			a.sevenZipSHA = check.SHA256
			if !strings.EqualFold(check.SHA256, expected7zSHA256) {
				a.sevenZipError = "内置 7za.exe 的 SHA-256 与发行版不一致，已阻止备份"
				a.initWarnings = append(a.initWarnings, a.sevenZipError)
			} else {
				a.sevenZipOK = true
			}
		}
	}

	if openErr := a.ensureBackupDirInitial(); openErr != nil {
		a.initWarnings = append(a.initWarnings, "本地备份目录不可用："+openErr.Error())
	}
	if a.logger != nil {
		a.logger.Info("app.start", "应用启动", map[string]any{
			"version":  Version,
			"platform": apppath.PlatformName(),
			"appDir":   a.paths.AppDir,
			"sidecar":  a.sidecarVersion(),
			"sevenZip": a.sevenZipVersion,
		})
	}
	go a.schedulerLoop()
}

func (a *App) shutdown(_ context.Context) {
	a.stopOnce.Do(func() {
		close(a.schedulerStop)
		select {
		case <-a.schedulerDone:
		case <-time.After(2 * time.Second):
		}
	})
	a.taskMu.Lock()
	active := a.active
	a.taskMu.Unlock()
	if active != nil {
		active.cancel()
		if a.sidecar != nil {
			_ = a.sidecar.Cancel(active.ID)
			_ = a.sidecar.Cancel(active.ID + "-pre")
		}
		time.Sleep(600 * time.Millisecond)
	}
	if a.sidecar != nil {
		_ = a.sidecar.Close()
	}
	if a.logger != nil {
		a.logger.Info("app.stop", "应用退出", nil)
		a.logger.Close()
	}
}

func (a *App) addWarning(err error) {
	if err == nil {
		return
	}
	message := err.Error()
	for _, existing := range a.initWarnings {
		if existing == message {
			return
		}
	}
	a.initWarnings = append(a.initWarnings, message)
}

func (a *App) ensureBackupDirInitial() error {
	cfg := a.configSnapshot()
	if cfg.BackupDir == "" {
		return nil
	}
	return os.MkdirAll(cfg.BackupDir, 0o755)
}

func (a *App) sidecarVersion() string {
	if a.sidecar == nil {
		return ""
	}
	return a.sidecar.Version()
}

func (a *App) configSnapshot() config.Config {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	cfg := a.cfg
	cfg.Compression.ExtraArgs = append([]string{}, a.cfg.Compression.ExtraArgs...)
	return cfg
}

func (a *App) setConfig(cfg config.Config) {
	a.cfgMu.Lock()
	a.cfg = cfg
	a.cfgMu.Unlock()
}

func (a *App) GetBootstrap() (Bootstrap, error) {
	cfg := a.configSnapshot()
	localSnapshots, warnings, scanErr := manifest.Scan(cfg.BackupDir)
	if scanErr != nil {
		warnings = append(warnings, "读取本地备份失败："+scanErr.Error())
	}
	if localSnapshots == nil {
		localSnapshots = []manifest.Snapshot{}
	}
	var latest *manifest.Snapshot
	if len(localSnapshots) > 0 {
		value := localSnapshots[0]
		latest = &value
	}
	keyInfo := sidecar.KeyInfo{}
	keyErr := ""
	if a.sidecar != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		info, err := a.sidecar.KeysInfo(ctx, a.paths.KeysDir)
		cancel()
		if err != nil {
			keyErr = err.Error()
		} else {
			keyInfo = info
			if info.Error != "" {
				keyErr = info.Error
			}
		}
	} else {
		keyErr = a.sidecarError
	}
	dsh := a.checkDSHRunning()
	disk := DiskInfo{Path: cfg.BackupDir}
	if free, err := platform.DiskFree(cfg.BackupDir); err == nil {
		disk.Free = free.Free
		disk.Total = free.Total
	}
	writable := a.paths.CheckWritable() == nil
	if !writable {
		warnings = append(warnings, "应用目录不可写；便携版必须放在可写目录")
	}
	if a.sidecar == nil {
		warnings = append(warnings, a.sidecarError)
	}
	if !a.sevenZipOK {
		warnings = append(warnings, a.sevenZipError)
	}
	warnings = append(warnings, a.initWarnings...)
	warnings = append(warnings, scanWarnings(dsh)...)
	if !keyInfo.Exists {
		hasEncrypted := false
		for _, snapshot := range localSnapshots {
			if snapshot.Encrypted {
				hasEncrypted = true
				break
			}
		}
		if hasEncrypted {
			warnings = append(warnings, "已有加密快照，但本地私钥不可用；请先恢复 data\\keys 目录，不会自动生成替代密钥")
		} else if cfg.EncryptionLocal {
			warnings = append(warnings, "加密已启用，但本地密钥尚未生成；下一次备份前会生成或提示恢复私钥")
		}
	}
	return Bootstrap{
		Config:         cfg,
		App:            a.appInfo(writable),
		Paths:          a.pathsInfo(cfg),
		DSH:            dsh,
		LocalSnapshots: localSnapshots,
		Latest:         latest,
		Key:            keyInfo,
		KeyError:       keyErr,
		Disk:           disk,
		Schedule:       a.scheduleInfo(cfg),
		Warnings:       dedupe(warnings),
		LogsPath:       a.paths.LogsDir,
	}, nil
}

func (a *App) appInfo(writable bool) AppInfo {
	return AppInfo{
		Version:   Version,
		BuildTime: BuildTime,
		Platform:  apppath.PlatformName(),
		Writable:  writable,
		Portable:  true,
		Warnings:  append([]string{}, a.initWarnings...),
		SevenZip: ToolInfo{
			Path:    a.sevenZipPath,
			Version: a.sevenZipVersion,
			SHA256:  a.sevenZipSHA,
			OK:      a.sevenZipOK,
			Error:   a.sevenZipError,
		},
		Sidecar: ToolInfo{
			Path:    a.sidecarPath,
			Version: a.sidecarVersion(),
			OK:      a.sidecar != nil,
			Error:   a.sidecarError,
		},
	}
}

func (a *App) pathsInfo(cfg config.Config) PathsInfo {
	return PathsInfo{
		AppDir:           a.paths.AppDir,
		DataDir:          a.paths.DataDir,
		ConfigDir:        a.paths.ConfigDir,
		LogsDir:          a.paths.LogsDir,
		KeysDir:          a.paths.KeysDir,
		BackupDir:        cfg.BackupDir,
		DefaultBackupDir: a.paths.DefaultBackupDir,
	}
}

func (a *App) scheduleInfo(cfg config.Config) ScheduleInfo {
	info := ScheduleInfo{
		Kind:    string(cfg.Schedule.Kind),
		Time:    cfg.Schedule.Time,
		Weekday: cfg.Schedule.Weekday,
		LastRun: cfg.LastScheduledAt,
	}
	var last time.Time
	if cfg.LastScheduledAt != "" {
		if parsed, err := time.Parse(time.RFC3339, cfg.LastScheduledAt); err == nil {
			last = parsed
		}
	}
	if next, ok := cfg.NextRunAfter(last); ok {
		info.NextRun = next.Format(time.RFC3339)
	}
	return info
}

func (a *App) checkDSHRunning() DSHStatus {
	status := DSHStatus{Sources: a.sourceInfo(), Evidence: []string{}, CheckedAt: time.Now().Format(time.RFC3339)}
	running, evidence, err := platform.IsDSHRunning(a.paths.DSHDataDir())
	if err != nil {
		status.Evidence = []string{"检测进程失败：" + err.Error()}
	} else {
		status.Running = running
		status.Evidence = evidence
	}
	return status
}

func (a *App) sourceInfo() []SourceInfo {
	a.sourceMu.Lock()
	defer a.sourceMu.Unlock()
	if len(a.sources) > 0 && time.Since(a.sourceAt) < 30*time.Second {
		return append([]SourceInfo{}, a.sources...)
	}
	sources := []SourceInfo{
		statSource("dsh", "DSH 核心数据", a.paths.DSHDataDir(), backup.DSHExcludeTop, backup.DSHExcludeRel),
		statSource("desktop-state", "桌面登录态", a.paths.DesktopStateDir(), backup.DesktopExcludeTop, backup.DesktopExcludeRel),
	}
	a.sources = sources
	a.sourceAt = time.Now()
	return append([]SourceInfo{}, sources...)
}

func statSource(name, label, path string, excludeTop, excludeRel []string) SourceInfo {
	info := SourceInfo{Name: name, Label: label, Path: path}
	if path == "" {
		info.Error = "路径不可用"
		return info
	}
	stat, err := os.Stat(path)
	if err != nil || !stat.IsDir() {
		info.Exists = false
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			info.Error = err.Error()
		}
		return info
	}
	info.Exists = true
	top := make(map[string]bool, len(excludeTop))
	for _, value := range excludeTop {
		top[strings.ToLower(value)] = true
	}
	rel := make(map[string]bool, len(excludeRel))
	for _, value := range excludeRel {
		rel[strings.ToLower(filepath.ToSlash(value))] = true
	}
	_ = filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			info.Error = walkErr.Error()
			return nil
		}
		if current == path {
			return nil
		}
		relative, err := filepath.Rel(path, current)
		if err != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) > 0 && top[strings.ToLower(parts[0])] {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if rel[strings.ToLower(filepath.ToSlash(relative))] {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return nil
		}
		info.Files++
		info.Bytes += fileInfo.Size()
		return nil
	})
	return info
}

func (a *App) invalidateSourceCache() {
	a.sourceMu.Lock()
	a.sourceAt = time.Time{}
	a.sourceMu.Unlock()
}

func (a *App) CheckDSH() DSHStatus {
	return a.checkDSHRunning()
}

func (a *App) beginTask(kind string) (string, context.Context, error) {
	a.taskMu.Lock()
	defer a.taskMu.Unlock()
	if a.active != nil {
		return "", nil, fmt.Errorf("已有任务正在运行：%s", a.active.Kind)
	}
	id := kind + "-" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	ctx, cancel := context.WithCancel(context.Background())
	a.active = &activeTask{ID: id, Kind: kind, cancel: cancel}
	return id, ctx, nil
}

func (a *App) finishTask(taskID string) {
	a.taskMu.Lock()
	if a.active != nil && a.active.ID == taskID {
		a.active.cancel()
		a.active = nil
	}
	a.taskMu.Unlock()
}

func (a *App) emitProgress(event TaskProgress) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "task:progress", event)
	}
}

func (a *App) emitTaskResult(result TaskResult) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "task:result", result)
	}
	a.invalidateSourceCache()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "state:changed", map[string]any{"reason": result.Kind})
	}
}

func (a *App) emitStateChanged(reason string) {
	if a.ctx != nil {
		a.invalidateSourceCache()
		runtime.EventsEmit(a.ctx, "state:changed", map[string]any{"reason": reason})
	}
}

func (a *App) emitNotice(level, message string) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "app:notice", Notice{Level: level, Message: message, Time: time.Now().Format(time.RFC3339)})
	}
}

func (a *App) CancelTask() error {
	a.taskMu.Lock()
	active := a.active
	a.taskMu.Unlock()
	if active == nil {
		return errors.New("当前没有正在运行的任务")
	}
	active.cancel()
	if a.sidecar != nil {
		_ = a.sidecar.Cancel(active.ID)
		_ = a.sidecar.Cancel(active.ID + "-pre")
	}
	a.emitProgress(TaskProgress{TaskID: active.ID, Kind: active.Kind, Phase: "cancel", Percent: 0, Message: "正在取消任务"})
	return nil
}

func (a *App) runningError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	if strings.HasPrefix(message, runningPrefix) {
		return err
	}
	return fmt.Errorf("%s%s", runningPrefix, message)
}

func scanWarnings(dsh DSHStatus) []string {
	var warnings []string
	for _, source := range dsh.Sources {
		if !source.Exists {
			warnings = append(warnings, fmt.Sprintf("%s 不存在：%s", source.Label, source.Path))
		}
	}
	return warnings
}

func dedupe(values []string) []string {
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
