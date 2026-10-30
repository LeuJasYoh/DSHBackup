package apppath

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Paths struct {
	AppDir           string
	DataDir          string
	ConfigDir        string
	LogsDir          string
	TempDir          string
	KeysDir          string
	WebViewDir       string
	DefaultBackupDir string
	UserHome         string
	RoamingAppData   string
}

func Resolve() (Paths, error) {
	appDir := strings.TrimSpace(os.Getenv("DSHBACKUP_APP_DIR"))
	if appDir == "" {
		exe, err := os.Executable()
		if err != nil {
			return Paths{}, err
		}
		resolved, err := filepath.EvalSymlinks(exe)
		if err == nil {
			exe = resolved
		}
		appDir = filepath.Dir(exe)
	}
	appDir, err := filepath.Abs(appDir)
	if err != nil {
		return Paths{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	roaming, _ := os.UserConfigDir()
	dataDir := filepath.Join(appDir, "data")
	return Paths{
		AppDir:           appDir,
		DataDir:          dataDir,
		ConfigDir:        filepath.Join(dataDir, "config"),
		LogsDir:          filepath.Join(dataDir, "logs"),
		TempDir:          filepath.Join(dataDir, "temp"),
		KeysDir:          filepath.Join(dataDir, "keys"),
		WebViewDir:       filepath.Join(dataDir, "webview2"),
		DefaultBackupDir: filepath.Join(appDir, "backups"),
		UserHome:         home,
		RoamingAppData:   roaming,
	}, nil
}

func (p Paths) Ensure() error {
	dirs := []string{p.DataDir, p.ConfigDir, p.LogsDir, p.TempDir, p.BackupDir()}
	var problems []string
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "；"))
	}
	return nil
}

func (p Paths) ConfigFile() string {
	return filepath.Join(p.ConfigDir, "config.json")
}

func (p Paths) BackupDir() string {
	return p.DefaultBackupDir
}

func (p Paths) DSHDataDir() string {
	return filepath.Join(p.UserHome, ".dsh")
}

func (p Paths) DesktopStateDir() string {
	if p.RoamingAppData == "" {
		return ""
	}
	return filepath.Join(p.RoamingAppData, "@deepseek-ai", "dsh-desktop")
}

func (p Paths) CheckWritable() error {
	for _, dir := range []string{p.AppDir, p.DataDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		probe, err := os.CreateTemp(dir, ".dshbackup-write-test-*")
		if err != nil {
			return err
		}
		name := probe.Name()
		_ = probe.Close()
		_ = os.Remove(name)
	}
	return nil
}

func (p Paths) TempTaskDir(taskID string) (string, error) {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, taskID)
	dir := filepath.Join(p.TempDir, safe)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	marker := filepath.Join(dir, tempTaskMarker)
	if err := os.WriteFile(marker, []byte("DSHBackup task directory\n"), 0o600); err != nil {
		return "", err
	}
	return dir, nil
}

const tempTaskMarker = ".dshbackup-task"

// CleanTemp removes only directories explicitly marked as task-owned. Unknown
// files and directories from older versions are left for manual inspection.
func (p Paths) CleanTemp() error {
	entries, err := os.ReadDir(p.TempDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var problems []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(p.TempDir, entry.Name())
		if _, err := os.Stat(filepath.Join(path, tempTaskMarker)); err != nil {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "；"))
	}
	return nil
}

func (p Paths) LocateSevenZip() string {
	if value := strings.TrimSpace(os.Getenv("DSHBACKUP_7ZA")); value != "" {
		if info, err := os.Stat(value); err == nil && !info.IsDir() {
			return value
		}
	}
	candidates := []string{
		filepath.Join(p.AppDir, "tools", "7za.exe"),
		filepath.Join(p.AppDir, "7za.exe"),
	}
	dir := p.AppDir
	for depth := 0; depth < 6; depth++ {
		candidates = append(candidates,
			filepath.Join(dir, "tools", "7zip", "7z2605-extra", "x64", "7za.exe"),
			filepath.Join(dir, "tools", "7zip", "7z2605-extra", "7za.exe"),
		)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func (p Paths) LocateSidecar() string {
	if value := strings.TrimSpace(os.Getenv("DSHBACKUP_SIDECAR")); value != "" {
		if info, err := os.Stat(value); err == nil && !info.IsDir() {
			return value
		}
	}
	candidates := []string{
		filepath.Join(p.AppDir, "dsh-backup-core.exe"),
		filepath.Join(p.AppDir, "bin", "dsh-backup-core.exe"),
	}
	dir := p.AppDir
	for depth := 0; depth < 6; depth++ {
		candidates = append(candidates,
			filepath.Join(dir, "sidecar", "target", "release", "dsh-backup-core.exe"),
			filepath.Join(dir, "sidecar", "target", "debug", "dsh-backup-core.exe"),
			filepath.Join(dir, "app", "DSHBackup", "sidecar", "target", "release", "dsh-backup-core.exe"),
			filepath.Join(dir, "app", "DSHBackup", "sidecar", "target", "debug", "dsh-backup-core.exe"),
		)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func PlatformName() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
