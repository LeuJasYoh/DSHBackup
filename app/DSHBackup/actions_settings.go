package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"DSHBackup/internal/config"
	"DSHBackup/internal/manifest"
	"DSHBackup/internal/pathguard"
)

func (a *App) CompleteOnboarding(input OnboardingInput) error {
	if err := a.readyError(); err != nil {
		return err
	}
	a.taskMu.Lock()
	defer a.taskMu.Unlock()
	if a.active != nil {
		return errors.New("任务正在运行，无法修改备份设置；请等待任务结束后重试")
	}
	a.cfgWriteMu.Lock()
	defer a.cfgWriteMu.Unlock()
	cfg := a.configSnapshot()
	if cfg.OnboardingComplete {
		return errors.New("首次设置已完成，请在设置中修改备份选项")
	}
	if input.BackupDir != "" {
		cfg.BackupDir = filepath.Clean(input.BackupDir)
	}
	if input.Compression == "" {
		input.Compression = "balanced"
	}
	cfg.Compression.Level = input.Compression
	cfg.IncludeDesktop = input.IncludeDesktop
	cfg.EncryptionLocal = input.EncryptionLocal
	cfg.OnboardingComplete = true
	if err := a.saveConfigInternal(cfg); err != nil {
		return err
	}
	a.emitStateChanged("onboarding")
	return nil
}

func (a *App) SaveConfig(input config.Config) error {
	if err := a.readyError(); err != nil {
		return err
	}
	a.taskMu.Lock()
	defer a.taskMu.Unlock()
	if a.active != nil {
		return errors.New("任务正在运行，无法修改备份设置；请等待任务结束后重试")
	}
	a.cfgWriteMu.Lock()
	defer a.cfgWriteMu.Unlock()
	current := a.configSnapshot()
	cfg := input
	cfg.OnboardingComplete = current.OnboardingComplete
	cfg.LastScheduledAt = current.LastScheduledAt
	if current.ScheduleChanged(cfg) {
		cfg.LastScheduledAt = time.Now().Format(time.RFC3339)
	}
	if err := a.saveConfigInternal(cfg); err != nil {
		return err
	}
	a.setScheduleBlocked(false)
	a.emitStateChanged("settings")
	return nil
}

// 调用者持有 taskMu 和 cfgWriteMu，防止任务启动或计划写入与设置保存交错。
func (a *App) saveConfigInternal(cfg config.Config) error {
	previousBackupDir := a.configSnapshot().BackupDir
	cfg.SchemaVersion = config.SchemaVersion
	cfg.Normalize(a.paths.DefaultBackupDir)
	if err := config.Validate(cfg); err != nil {
		return err
	}
	for _, root := range []struct {
		path    string
		message string
	}{
		{a.paths.DSHDataDir(), "本地备份目录不能位于 .dsh 目录内"},
		{a.paths.DesktopStateDir(), "本地备份目录不能位于桌面登录态目录内"},
		{a.paths.DataDir, "本地备份目录不能位于应用 data 目录内"},
	} {
		if root.path == "" {
			continue
		}
		contained, err := pathguard.Contains(cfg.BackupDir, root.path)
		if err != nil {
			return fmt.Errorf("无法检查备份目录边界：%w", err)
		}
		if contained {
			return errors.New(root.message)
		}
	}
	if _, err := ensureWritableDir(cfg.BackupDir); err != nil {
		return fmt.Errorf("本地备份目录不可写：%w", err)
	}
	if cfg.EncryptionLocal {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := a.ensureEncryptionReady(ctx, cfg, true)
		cancel()
		if err != nil {
			return err
		}
	}
	if err := config.Save(a.paths.ConfigFile(), cfg); err != nil {
		return fmt.Errorf("保存配置失败：%w", err)
	}
	a.setConfig(cfg)
	if previousBackupDir != "" && !strings.EqualFold(filepath.Clean(previousBackupDir), filepath.Clean(cfg.BackupDir)) {
		a.warnLeftoverTrash(previousBackupDir)
	}
	if a.logger != nil {
		a.logger.Info("config.save", "配置已保存", map[string]any{
			"encryption": cfg.EncryptionLocal,
			"backupDir":  cfg.BackupDir,
			"schedule":   cfg.Schedule.Kind,
		})
	}
	return nil
}

func (a *App) ensureEncryptionReady(ctx context.Context, cfg config.Config, generate bool) error {
	if !cfg.EncryptionLocal {
		return nil
	}
	if a.sidecar == nil {
		return errors.New(a.sidecarError)
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	info, err := a.sidecar.KeysInfo(checkCtx, a.paths.KeysDir)
	cancel()
	if err != nil {
		return fmt.Errorf("检查本地密钥失败：%w", err)
	}
	if info.Error != "" {
		return errors.New(info.Error)
	}
	if info.Exists {
		if info.CanEncrypt {
			return nil
		}
		if !generate {
			if info.EncryptionError != "" {
				return errors.New(info.EncryptionError)
			}
			return errors.New("本地密钥不可用于加密")
		}
		ensureCtx, ensureCancel := context.WithTimeout(ctx, 10*time.Second)
		repaired, repairErr := a.sidecar.KeysEnsure(ensureCtx, a.paths.KeysDir, false)
		ensureCancel()
		if repairErr != nil {
			return fmt.Errorf("本地密钥不可用于加密：%w", repairErr)
		}
		if !repaired.CanEncrypt {
			return errors.New("本地密钥不可用于加密，请检查 public.key 与 private.key 是否配对")
		}
		return nil
	}
	if strings.TrimSpace(info.PublicKey) != "" {
		return errors.New("检测到公钥但找不到私钥，不会自动生成新密钥；请恢复 data\\keys 目录")
	}
	if err := a.ensureNoEncryptedLocalWithoutKeys(cfg); err != nil {
		return err
	}
	if !generate {
		return errors.New("本地密钥尚未生成")
	}
	ensureCtx, ensureCancel := context.WithTimeout(ctx, 10*time.Second)
	_, err = a.sidecar.KeysEnsure(ensureCtx, a.paths.KeysDir, true)
	ensureCancel()
	if err != nil {
		return fmt.Errorf("生成本地密钥失败：%w", err)
	}
	return nil
}

func (a *App) ensureNoEncryptedLocalWithoutKeys(cfg config.Config) error {
	for _, dir := range dedupe([]string{cfg.BackupDir, a.configSnapshot().BackupDir, a.paths.DefaultBackupDir}) {
		snapshots, warnings, err := manifest.Scan(dir)
		if err != nil {
			return fmt.Errorf("检查已有本地备份失败：%w", err)
		}
		for _, snapshot := range snapshots {
			if snapshot.Encrypted {
				return errors.New("检测到已有的加密备份，但本地私钥缺失。为避免旧备份永久失效，不会自动生成新密钥；请先恢复 data\\keys 目录")
			}
		}
		if len(warnings) > 0 {
			return errors.New("已有本地备份无法完整读取，不能确认旧密钥是否仍需使用；请先检查备份或恢复 data\\keys 目录")
		}
	}
	return nil
}

func (a *App) PickDirectory(defaultDir string) (string, error) {
	if a.ctx == nil {
		return "", errors.New("应用尚未启动")
	}
	if defaultDir == "" {
		defaultDir = a.configSnapshot().BackupDir
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "选择本地备份目录",
		DefaultDirectory: defaultDir,
	})
}

func (a *App) OpenBackupFolder() error {
	cfg := a.configSnapshot()
	if err := os.MkdirAll(cfg.BackupDir, 0o755); err != nil {
		return err
	}
	return openInExplorer(cfg.BackupDir)
}

func (a *App) OpenKeysFolder() error {
	if err := os.MkdirAll(a.paths.KeysDir, 0o700); err != nil {
		return err
	}
	return openInExplorer(a.paths.KeysDir)
}

func (a *App) OpenLogsFolder() error {
	if err := os.MkdirAll(a.paths.LogsDir, 0o755); err != nil {
		return err
	}
	return openInExplorer(a.paths.LogsDir)
}

func (a *App) OpenDataFolder() error {
	if err := os.MkdirAll(a.paths.DataDir, 0o755); err != nil {
		return err
	}
	return openInExplorer(a.paths.DataDir)
}

func (a *App) OpenLocalSnapshot(id string) error {
	snapshots, _, err := manifest.Scan(a.configSnapshot().BackupDir)
	if err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		if snapshot.ID == id && snapshot.LocalPath != "" {
			return openInExplorer("/select," + snapshot.LocalPath)
		}
	}
	return errors.New("找不到本地快照")
}

func (a *App) CopyText(text string) error {
	if len(text) > 4096 {
		return errors.New("文本过长")
	}
	if a.ctx == nil {
		return errors.New("应用尚未启动")
	}
	if err := runtime.ClipboardSetText(a.ctx, text); err != nil {
		return fmt.Errorf("无法写入系统剪贴板：%w", err)
	}
	return nil
}

func (a *App) ReadLogTail(lines int) string {
	if a.logger == nil {
		return ""
	}
	if lines <= 0 || lines > 500 {
		lines = 200
	}
	return a.logger.Tail(lines)
}

func (a *App) ExportDiagnostics() (string, error) {
	if a.logger == nil {
		return "", errors.New("日志系统不可用")
	}
	cfg := a.configSnapshot()
	cfgRaw, _ := json.MarshalIndent(cfg, "", "  ")
	dsh := a.checkDSHRunning()
	sourceRaw, _ := json.MarshalIndent(dsh, "", "  ")
	appRaw, _ := json.MarshalIndent(a.appInfo(a.paths.CheckWritable() == nil), "", "  ")
	snapshots, _, _ := manifest.Scan(cfg.BackupDir)
	snapshotRaw, _ := json.MarshalIndent(snapshots, "", "  ")
	extras := map[string][]byte{
		"config.json":    cfgRaw,
		"app.json":       appRaw,
		"sources.json":   sourceRaw,
		"snapshots.json": snapshotRaw,
		"README.txt":     []byte("此诊断包不包含本地私钥、归档密码或 DSH 凭据内容；日志中的敏感信息会被脱敏。\n"),
	}
	path, err := a.logger.Export(a.paths.LogsDir, extras)
	if err != nil {
		return "", err
	}
	if a.logger != nil {
		a.logger.Info("diagnostics.export", "导出诊断包", map[string]any{"path": path})
	}
	return path, nil
}

func openInExplorer(target string) error {
	cmd := exec.Command("explorer.exe", target)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
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
	return filepath.Abs(dir)
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
