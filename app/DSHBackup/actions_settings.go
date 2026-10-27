package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"DSHBackup/internal/config"
	"DSHBackup/internal/creds"
	"DSHBackup/internal/github"
	"DSHBackup/internal/manifest"
)

func (a *App) CompleteOnboarding(input OnboardingInput) error {
	current := a.configSnapshot()
	cfg := current
	cfg.Mode = config.Mode(input.Mode)
	if cfg.Mode == "" {
		cfg.Mode = config.ModeLocal
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
	current := a.configSnapshot()
	cfg := input
	cfg.SchemaVersion = config.SchemaVersion
	cfg.Normalize(a.paths.DefaultBackupDir)
	cfg.OnboardingComplete = current.OnboardingComplete || input.OnboardingComplete
	cfg.LastScheduledAt = current.LastScheduledAt
	cfg.GitHub.Login = current.GitHub.Login
	cfg.GitHub.AvatarURL = current.GitHub.AvatarURL
	if current.GitHub.ConnectedAt != "" {
		cfg.GitHub.ConnectedAt = current.GitHub.ConnectedAt
	}
	if err := a.saveConfigInternal(cfg); err != nil {
		return err
	}
	a.emitStateChanged("settings")
	return nil
}

func (a *App) saveConfigInternal(cfg config.Config) error {
	cfg.SchemaVersion = config.SchemaVersion
	cfg.Normalize(a.paths.DefaultBackupDir)
	if err := config.Validate(cfg); err != nil {
		return err
	}
	if _, err := ensureWritableDir(cfg.BackupDir); err != nil {
		return fmt.Errorf("本地备份目录不可写：%w", err)
	}
	if isPathInside(cfg.BackupDir, a.paths.DSHDataDir()) {
		return errors.New("本地备份目录不能位于 .dsh 目录内")
	}
	if desktop := a.paths.DesktopStateDir(); desktop != "" && isPathInside(cfg.BackupDir, desktop) {
		return errors.New("本地备份目录不能位于桌面登录态目录内")
	}
	if isPathInside(cfg.BackupDir, a.paths.DataDir) {
		return errors.New("本地备份目录不能位于应用 data 目录内；data	emp 会被启动清理")
	}
	if cfg.Mode != config.ModeLocal {
		if !creds.Exists(creds.GitHubTokenTarget) {
			return errors.New("云端模式需要先连接 GitHub 账号")
		}
		if cfg.GitHub.FullRepo() == "" {
			return errors.New("请先选择或创建 GitHub 私有仓库")
		}
	}
	needsEncryption := cfg.EncryptionLocal || cfg.Mode != config.ModeLocal
	if needsEncryption {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := a.ensureEncryptionReady(ctx, cfg, true)
		cancel()
		if err != nil {
			return err
		}
	}
	cfg.Proxy.PasswordSet = creds.Exists(creds.ProxyPasswordTarget)
	if err := config.Save(a.paths.ConfigFile(), cfg); err != nil {
		return fmt.Errorf("保存配置失败：%w", err)
	}
	a.setConfig(cfg)
	a.invalidateCloudCache()
	if a.logger != nil {
		a.logger.Info("config.save", "配置已保存", map[string]any{
			"mode":       cfg.Mode,
			"encryption": cfg.EncryptionLocal,
			"backupDir":  cfg.BackupDir,
			"schedule":   cfg.Schedule.Kind,
		})
	}
	return nil
}

func (a *App) ensureEncryptionReady(ctx context.Context, cfg config.Config, generate bool) error {
	if !cfg.EncryptionLocal && cfg.Mode == config.ModeLocal {
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
	if info.Exists {
		if info.Error != "" {
			return errors.New(info.Error)
		}
		return nil
	}
	if info.Error != "" {
		return errors.New(info.Error)
	}
	snapshots, _, err := manifest.Scan(cfg.BackupDir)
	if err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		if snapshot.Encrypted {
			return errors.New("检测到已有的加密备份，但本地私钥缺失。为避免旧备份永久失效，不会自动生成新密钥；请先恢复 data\\keys 目录")
		}
	}
	if cfg.Mode != config.ModeLocal {
		if err := a.ensureNoEncryptedCloudWithoutKeys(ctx, cfg); err != nil {
			return err
		}
	}
	if !generate {
		return nil
	}
	ensureCtx, ensureCancel := context.WithTimeout(ctx, 10*time.Second)
	_, err = a.sidecar.KeysEnsure(ensureCtx, a.paths.KeysDir)
	ensureCancel()
	if err != nil {
		return fmt.Errorf("生成本地密钥失败：%w", err)
	}
	return nil
}

func (a *App) ensureNoEncryptedCloudWithoutKeys(ctx context.Context, cfg config.Config) error {
	if !creds.Exists(creds.GitHubTokenTarget) || cfg.GitHub.FullRepo() == "" {
		return nil
	}
	client, _, err := a.newGitHubClient(true)
	if err != nil {
		return err
	}
	listCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	snapshots, listErr := client.ListBackups(listCtx, cfg.GitHub.Owner, cfg.GitHub.Repo)
	cancel()
	if listErr != nil {
		return fmt.Errorf("无法确认云端是否已有加密快照，为避免生成新密钥导致旧备份不可恢复，请联网后重试：%w", listErr)
	}
	for _, snapshot := range snapshots {
		if snapshot.Encrypted {
			return errors.New("云端已有加密备份，但本地私钥缺失。为避免旧备份永久失效，不会自动生成新密钥；请先恢复 data\\keys 目录")
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

func (a *App) SaveProxyPassword(password string) error {
	cfg := a.configSnapshot()
	username := cfg.Proxy.Username
	if username == "" {
		username = "proxy"
	}
	if password == "" {
		if err := creds.Delete(creds.ProxyPasswordTarget); err != nil {
			return err
		}
	} else {
		if err := creds.Set(creds.ProxyPasswordTarget, username, password); err != nil {
			return err
		}
	}
	a.emitStateChanged("proxy-password")
	return nil
}

func (a *App) ClearProxyPassword() error {
	if err := creds.Delete(creds.ProxyPasswordTarget); err != nil {
		return err
	}
	a.emitStateChanged("proxy-password")
	return nil
}

func (a *App) TestProxy(input ProxyTestRequest) (ProxyTestResult, error) {
	cfg := a.configSnapshot()
	password := input.Password
	if password == "" {
		password, _ = creds.Get(creds.ProxyPasswordTarget)
	}
	proxyCfg := github.ProxyConfig{
		Mode:        input.Mode,
		URL:         input.URL,
		Username:    input.Username,
		Password:    password,
		BypassLocal: cfg.Proxy.BypassLocal,
	}
	if proxyCfg.Mode == "" {
		proxyCfg.Mode = cfg.Proxy.Mode
	}
	client, err := github.New("", proxyCfg, a.logger)
	if err != nil {
		return ProxyTestResult{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	connectivity, pingErr := client.Ping(ctx)
	result := ProxyTestResult{
		Reachable: connectivity.Reachable,
		Status:    connectivity.Status,
		LatencyMS: connectivity.LatencyMS,
		Message:   connectivity.Message,
	}
	if pingErr != nil {
		result.Message = pingErr.Error()
	}
	return result, nil
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

func (a *App) OpenExternalURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return errors.New("链接无效")
	}
	if parsed.Scheme != "https" {
		return errors.New("只允许打开 HTTPS 链接")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "github.com" && host != "docs.github.com" {
		return errors.New("只允许打开 GitHub 官方链接")
	}
	if a.ctx != nil {
		runtime.BrowserOpenURL(a.ctx, rawURL)
	}
	return nil
}

func (a *App) CopyText(text string) error {
	if len(text) > 4096 {
		return errors.New("文本过长")
	}
	if a.ctx != nil {
		runtime.ClipboardSetText(a.ctx, text)
	}
	return nil
}

func (a *App) OpenGitHubRepo() error {
	cfg := a.configSnapshot()
	if cfg.GitHub.FullRepo() == "" {
		return errors.New("尚未选择仓库")
	}
	return a.OpenExternalURL("https://github.com/" + cfg.GitHub.FullRepo())
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
	cfg.Proxy.PasswordSet = creds.Exists(creds.ProxyPasswordTarget)
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
		"README.txt":     []byte("此诊断包不包含 GitHub token、代理密码、本地私钥、归档密码或 DSH 凭据内容。\n"),
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
