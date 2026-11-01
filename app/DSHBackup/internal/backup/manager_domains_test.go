//go:build windows

package backup_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"DSHBackup/internal/apppath"
	"DSHBackup/internal/backup"
	"DSHBackup/internal/config"
	"DSHBackup/internal/manifest"
	"DSHBackup/internal/scope"
	"DSHBackup/internal/sidecar"
)

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func testSevenZip() string {
	return filepath.Join(repoRoot(), "..", "..", "tools", "7zip", "7z2605-extra", "x64", "7za.exe")
}

// setupDomainsFixture 起一个临时 home 并铺好四类数据；sidecar 缺失时跳过。
func setupDomainsFixture(t *testing.T) (*sidecar.Client, apppath.Paths, *backup.Manager) {
	t.Helper()
	binary := filepath.Join(repoRoot(), "sidecar", "target", "release", "dsh-backup-core.exe")
	if _, err := os.Stat(binary); err != nil {
		binary = filepath.Join(repoRoot(), "sidecar", "target", "debug", "dsh-backup-core.exe")
	}
	if _, err := os.Stat(binary); err != nil {
		t.Skip("sidecar 未构建")
	}
	if _, err := os.Stat(testSevenZip()); err != nil {
		t.Skip("7za 不存在")
	}
	client, err := sidecar.Start(binary, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	base := t.TempDir()
	home := filepath.Join(base, "home")
	appDir := filepath.Join(base, "app")
	paths := apppath.Paths{
		AppDir:           appDir,
		DataDir:          filepath.Join(appDir, "data"),
		ConfigDir:        filepath.Join(appDir, "data", "config"),
		LogsDir:          filepath.Join(appDir, "data", "logs"),
		TempDir:          filepath.Join(appDir, "data", "temp"),
		KeysDir:          filepath.Join(appDir, "data", "keys"),
		WebViewDir:       filepath.Join(appDir, "data", "webview2"),
		DefaultBackupDir: filepath.Join(appDir, "backups"),
		UserHome:         home,
		RoamingAppData:   filepath.Join(base, "roaming"),
	}
	for _, dir := range []string{paths.DataDir, paths.TempDir, paths.KeysDir, paths.DefaultBackupDir, home, paths.RoamingAppData} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dsh := filepath.Join(home, ".dsh")
	writeFile(t, filepath.Join(dsh, ".credentials.yaml"), "api_key: test-secret\n")
	writeFile(t, filepath.Join(dsh, "sessions", "a.json"), `{"session":1}`)
	writeFile(t, filepath.Join(dsh, "attachments", "c.txt"), "attachment")
	writeFile(t, filepath.Join(dsh, "storages", "workspace.json"), `{"workspace":1}`)
	writeFile(t, filepath.Join(dsh, "storages", "session_projcache", "sessions", "s.json"), `{"cache":1}`)
	writeFile(t, filepath.Join(dsh, "profiles", "desktop", "package.json"), `{"dependencies":{}}`)
	writeFile(t, filepath.Join(dsh, "profiles", "desktop", "node_modules", "pkg", "index.js"), "plugin-code")
	writeFile(t, filepath.Join(dsh, "profiles", "desktop", "node_modules", "pkg", "__pycache__", "stale.pyc"), "bytecode")
	writeFile(t, filepath.Join(dsh, "dsh-runtimes", "dsh-primary-runtime", "dependencies", "node", "bin", "node.exe"), "runtime-blob")

	manager := backup.NewManager(paths, client, nil, testSevenZip(), "7-Zip (test)", "test")
	return client, paths, manager
}

// fixtureConfig 是每种域选择都要重设的公共设置。
func fixtureConfig(paths apppath.Paths) config.Config {
	cfg := config.Default(paths.DefaultBackupDir)
	cfg.OnboardingComplete = true
	cfg.IncludeDesktop = false
	cfg.EncryptionLocal = false
	cfg.VerifyAfterBackup = true
	cfg.BackupDir = paths.DefaultBackupDir
	return cfg
}

func hasPart(parts []manifest.Part, name scope.Domain) bool {
	for _, part := range parts {
		if part.Domain == string(name) {
			return true
		}
	}
	return false
}

func archivePaths(t *testing.T, client *sidecar.Client, archive string) string {
	t.Helper()
	listing, err := client.ListArchive(context.Background(), testSevenZip(), archive, "")
	if err != nil {
		t.Fatalf("读取归档清单失败：%v", err)
	}
	lines := make([]string, 0, len(listing.Details))
	for _, entry := range listing.Details {
		lines = append(lines, strings.ToLower(filepath.ToSlash(entry.Path)))
	}
	return "\n" + strings.Join(lines, "\n") + "\n"
}

func TestDomainSelectionSkipsReinstallableData(t *testing.T) {
	client, paths, manager := setupDomainsFixture(t)
	cfg := fixtureConfig(paths)

	// 默认：插件依赖进备份，运行环境不进。
	if selection := cfg.Domains.Selection(); selection.Plugins != true || selection.Runtime != false {
		t.Fatalf("默认域选择应为插件开、运行环境关，实际 %+v", selection)
	}
	result, err := manager.Backup(context.Background(), backup.BackupInput{
		TaskID: "domains-default", Config: cfg, Kind: manifest.KindManual, Force: true,
	})
	if err != nil {
		t.Fatalf("默认备份失败：%v", err)
	}
	if len(result.ArchivePaths) != 3 {
		t.Fatalf("默认应生成 3 份归档（身份、会话、插件），实际 %d：%v", len(result.ArchivePaths), result.ArchivePaths)
	}
	for _, name := range []scope.Domain{scope.DomainCore, scope.DomainSessions, scope.DomainPlugins} {
		if !hasPart(result.Snapshot.Parts, name) {
			t.Fatalf("快照应包含 %s 域：%+v", name, result.Snapshot.Parts)
		}
	}
	if hasPart(result.Snapshot.Parts, scope.DomainRuntime) {
		t.Fatalf("默认不应备份运行环境：%+v", result.Snapshot.Parts)
	}
	if !strings.Contains(result.ArchivePath, "-core.7z") {
		t.Fatalf("归档名应带域名：%s", result.ArchivePath)
	}
	var joined string
	for _, archive := range result.ArchivePaths {
		joined += archivePaths(t, client, archive)
	}
	for _, forbidden := range []string{"dsh/dsh-runtimes/", "dsh/storages/session_projcache/", "/__pycache__/"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("归档不应包含 %s（实际：%s）", forbidden, joined)
		}
	}
	for _, required := range []string{
		"dsh/profiles/desktop/node_modules/pkg/index.js",
		"dsh/sessions/a.json",
		"dsh/attachments/c.txt",
		"dsh/storages/workspace.json",
	} {
		if !strings.Contains(joined, "\n"+required) {
			t.Fatalf("归档缺少 %s（实际：%s）", required, joined)
		}
	}

	cfg.Domains.SkipPluginDeps = true
	cfg.Domains.IncludeRuntime = true
	second, err := manager.Backup(context.Background(), backup.BackupInput{
		TaskID: "domains-inverted", Config: cfg, Kind: manifest.KindManual, Force: true,
	})
	if err != nil {
		t.Fatalf("反向备份失败：%v", err)
	}
	if hasPart(second.Snapshot.Parts, scope.DomainPlugins) {
		t.Fatalf("关闭插件依赖后不应有插件域：%+v", second.Snapshot.Parts)
	}
	if !hasPart(second.Snapshot.Parts, scope.DomainRuntime) {
		t.Fatalf("打开运行环境后应有运行环境域：%+v", second.Snapshot.Parts)
	}
}

func TestRestoreKeepsOriginalTimestamps(t *testing.T) {
	_, paths, manager := setupDomainsFixture(t)
	dsh := filepath.Join(paths.UserHome, ".dsh")
	original := time.Unix(1700000000, 0)
	if err := os.Chtimes(filepath.Join(dsh, "sessions", "a.json"), original, original); err != nil {
		t.Fatal(err)
	}

	cfg := fixtureConfig(paths)
	cfg.VerifyAfterBackup = false
	result, err := manager.Backup(context.Background(), backup.BackupInput{
		TaskID: "times", Config: cfg, Kind: manifest.KindManual, Force: true,
	})
	if err != nil {
		t.Fatalf("备份失败：%v", err)
	}

	fresh := time.Unix(1760000000, 0)
	if err := os.Chtimes(filepath.Join(dsh, "sessions", "a.json"), fresh, fresh); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(paths.UserHome, "restored")
	writeFile(t, filepath.Join(target, "sessions", "a.json"), "stale-content")
	if _, err := manager.Restore(context.Background(), backup.RestoreInput{
		TaskID: "times-restore", Config: cfg, Snapshot: result.Snapshot, TargetDSH: target, Force: true,
	}); err != nil {
		t.Fatalf("恢复失败：%v", err)
	}
	info, err := os.Stat(filepath.Join(target, "sessions", "a.json"))
	if err != nil {
		t.Fatal(err)
	}
	if drift := info.ModTime().Sub(original); drift > time.Second || drift < -time.Second {
		t.Fatalf("恢复后的修改时间应回到源文件的 %s，实际 %s", original, info.ModTime())
	}
}

func TestRestoreOnlySelectedDomains(t *testing.T) {
	_, paths, manager := setupDomainsFixture(t)
	cfg := fixtureConfig(paths)
	saved, err := manager.Backup(context.Background(), backup.BackupInput{
		TaskID: "subset", Config: cfg, Kind: manifest.KindManual, Force: true,
	})
	if err != nil {
		t.Fatalf("备份失败：%v", err)
	}

	target := filepath.Join(paths.UserHome, "subset-target")
	if _, err := manager.Restore(context.Background(), backup.RestoreInput{
		TaskID: "subset-restore", Config: cfg, Snapshot: saved.Snapshot,
		Domains: []string{string(scope.DomainCore)}, TargetDSH: target, Force: true,
	}); err != nil {
		t.Fatalf("只恢复身份与配置域失败：%v", err)
	}
	for _, relative := range []string{".credentials.yaml", filepath.Join("profiles", "desktop", "package.json"), filepath.Join("storages", "workspace.json")} {
		if _, err := os.Stat(filepath.Join(target, relative)); err != nil {
			t.Fatalf("core 域的文件没有恢复出来 %s：%v", relative, err)
		}
	}
	for _, relative := range []string{
		filepath.Join("sessions", "a.json"),
		filepath.Join("attachments", "c.txt"),
		filepath.Join("profiles", "desktop", "node_modules", "pkg", "index.js"),
	} {
		if _, err := os.Stat(filepath.Join(target, relative)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("未选择的域不该被写入 %s（err=%v）", relative, err)
		}
	}
}

func TestRestoreRejectsMissingDomainSelection(t *testing.T) {
	_, paths, manager := setupDomainsFixture(t)
	cfg := fixtureConfig(paths)
	saved, err := manager.Backup(context.Background(), backup.BackupInput{
		TaskID: "subset-missing", Config: cfg, Kind: manifest.KindManual, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(paths.UserHome, "missing-target")
	if _, err := manager.Restore(context.Background(), backup.RestoreInput{
		TaskID: "subset-missing-restore", Config: cfg, Snapshot: saved.Snapshot,
		Domains: []string{string(scope.DomainRuntime)}, TargetDSH: target, Force: true,
	}); err == nil {
		t.Fatal("请求恢复一个不存在于本快照的域时必须报错，不能静默跳过")
	}
}

// TestBackToBackBackupsGetDistinctIDs 盯住分域后最容易复发的坑：
// ID 是秒级的，查重若只看旧版裸归档名，同一秒内的两次备份会撞名并互相覆盖。
func TestBackToBackBackupsGetDistinctIDs(t *testing.T) {
	_, paths, manager := setupDomainsFixture(t)
	cfg := fixtureConfig(paths)
	first, err := manager.Backup(context.Background(), backup.BackupInput{
		TaskID: "same-second-1", Config: cfg, Kind: manifest.KindManual, Force: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Backup(context.Background(), backup.BackupInput{
		TaskID: "same-second-2", Config: cfg, Kind: manifest.KindManual, Force: true,
	})
	if err != nil {
		t.Fatalf("同一秒内的第二次备份失败（多半是 ID 查重没按域来）：%v", err)
	}
	if first.Snapshot.ID == second.Snapshot.ID {
		t.Fatalf("两次备份拿到了同一个 ID：%s", first.Snapshot.ID)
	}
	if len(second.ArchivePaths) < 2 {
		t.Fatalf("第二次备份应落多份归档，实际 %v", second.ArchivePaths)
	}
	listed, _, err := manager.ListLocal(cfg.BackupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("应列出两份快照：%+v", listed)
	}
}
