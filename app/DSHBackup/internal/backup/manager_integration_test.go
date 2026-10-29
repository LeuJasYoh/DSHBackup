//go:build windows

package backup_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"DSHBackup/internal/apppath"
	"DSHBackup/internal/backup"
	"DSHBackup/internal/config"
	"DSHBackup/internal/manifest"
	"DSHBackup/internal/sidecar"
)

func TestEncryptedBackupVerifyRestore(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	sidecarPath := filepath.Join(repo, "sidecar", "target", "release", "dsh-backup-core.exe")
	if _, err := os.Stat(sidecarPath); err != nil {
		sidecarPath = filepath.Join(repo, "sidecar", "target", "debug", "dsh-backup-core.exe")
	}
	sevenZip := filepath.Join(repo, "..", "..", "tools", "7zip", "7z2605-extra", "x64", "7za.exe")
	if _, err := os.Stat(sidecarPath); err != nil {
		t.Skip("sidecar 未构建")
	}
	if _, err := os.Stat(sevenZip); err != nil {
		t.Skip("7za 不存在")
	}
	client, err := sidecar.Start(sidecarPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	base := t.TempDir()
	home := filepath.Join(base, "home")
	appData := filepath.Join(base, "roaming")
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
		RoamingAppData:   appData,
	}
	for _, dir := range []string{paths.DataDir, paths.ConfigDir, paths.LogsDir, paths.TempDir, paths.DefaultBackupDir, home, appData} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dsh := filepath.Join(home, ".dsh")
	writeFile(t, filepath.Join(dsh, ".credentials.yaml"), "api_key: test-secret\n")
	writeFile(t, filepath.Join(dsh, "profiles", "default", "config.json"), `{"profile":"test"}`)
	writeFile(t, filepath.Join(dsh, "sessions", "a.json"), `{"session":1}`)
	writeFile(t, filepath.Join(dsh, "storages", "b.json"), `{"storage":1}`)
	writeFile(t, filepath.Join(dsh, "attachments", "c.txt"), "attachment")
	writeFile(t, filepath.Join(dsh, "logs", "ignored.log"), "should not be archived")
	writeFile(t, filepath.Join(dsh, "cache", "ignored.bin"), "should not be archived")
	writeFile(t, filepath.Join(dsh, ".dshw-usage.json"), `{"usage":1}`)
	desktop := filepath.Join(appData, "@deepseek-ai", "dsh-desktop")
	writeFile(t, filepath.Join(desktop, "Local State"), `{"os_crypt":{}}`)
	writeFile(t, filepath.Join(desktop, "Preferences"), `{"window":{}}`)
	writeFile(t, filepath.Join(desktop, "Network", "Cookies"), "cookie-data")
	writeFile(t, filepath.Join(desktop, "Cache", "ignored.bin"), "cache-data")

	cfg := config.Default(paths.DefaultBackupDir)
	cfg.OnboardingComplete = true
	cfg.IncludeDesktop = true
	cfg.EncryptionLocal = true
	cfg.VerifyAfterBackup = true
	cfg.Retention.Local = 7
	cfg.BackupDir = paths.DefaultBackupDir

	manager := backup.NewManager(paths, client, nil, sevenZip, "7-Zip (test)", "test")
	var phases []string
	result, err := manager.Backup(context.Background(), backup.BackupInput{
		TaskID: "test-backup",
		Config: cfg,
		Kind:   manifest.KindManual,
		Force:  true,
		OnProgress: func(progress backup.Progress) {
			if len(phases) == 0 || phases[len(phases)-1] != progress.Phase {
				phases = append(phases, progress.Phase)
			}
		},
	})
	if err != nil {
		t.Fatalf("backup failed: %v", err)
	}
	if !result.Snapshot.Encrypted {
		t.Fatal("snapshot should be encrypted")
	}
	if _, err := os.Stat(result.ArchivePath); err != nil {
		t.Fatalf("archive missing: %v", err)
	}
	if _, err := os.Stat(result.Snapshot.ManifestPath); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	if result.Manifest.Encryption == nil || !result.Manifest.Encryption.Enabled {
		t.Fatal("manifest encryption missing")
	}
	if result.Manifest.DesktopIncluded != true {
		t.Fatal("desktop should be included")
	}
	if result.Snapshot.KeyID == "" {
		t.Fatal("key id missing")
	}
	if err := manager.VerifyLocal(context.Background(), "test-verify", result.Snapshot, nil); err != nil {
		t.Fatalf("verify failed: %v", err)
	}

	writeFile(t, filepath.Join(desktop, "Local State"), `{"changed":true}`)
	customTarget := filepath.Join(base, "restore-target")
	writeFile(t, filepath.Join(customTarget, "old.txt"), "existing target data")
	if _, err := manager.Restore(context.Background(), backup.RestoreInput{
		TaskID: "test-restore-invalid", Config: cfg, Snapshot: result.Snapshot, IncludeDesktop: true, TargetDSH: customTarget, Force: true,
	}); err == nil {
		t.Fatal("custom target must not restore the system desktop state")
	}
	restore, err := manager.Restore(context.Background(), backup.RestoreInput{
		TaskID:             "test-restore",
		Config:             cfg,
		Snapshot:           result.Snapshot,
		IncludeDesktop:     false,
		PreRestoreSnapshot: true,
		TargetDSH:          customTarget,
		Force:              true,
	})
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	if restore.PreRestoreSnapshot == nil {
		t.Fatal("pre-restore snapshot missing")
	}
	preManifest, err := manifest.Read(restore.PreRestoreSnapshot.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(preManifest.Scope) != 1 || preManifest.Scope[0] != "dsh" || preManifest.DesktopIncluded {
		t.Fatalf("pre-restore snapshot should protect only the custom target: %+v", preManifest)
	}
	content, err := os.ReadFile(filepath.Join(customTarget, ".credentials.yaml"))
	if err != nil || string(content) != "api_key: test-secret\n" {
		t.Fatalf("restored credentials mismatch: %v %q", err, string(content))
	}
	desktopContent, err := os.ReadFile(filepath.Join(desktop, "Local State"))
	if err != nil || string(desktopContent) != `{"changed":true}` {
		t.Fatalf("desktop Local State must stay untouched: %v %q", err, string(desktopContent))
	}
	if _, err := os.Stat(filepath.Join(customTarget, "logs", "ignored.log")); !os.IsNotExist(err) {
		t.Fatal("excluded logs should not be restored")
	}
	if _, err := os.Stat(filepath.Join(customTarget, "sessions", "a.json")); err != nil {
		t.Fatal("session missing after restore")
	}
	if _, err := os.Stat(filepath.Join(customTarget, "old.txt")); err != nil {
		t.Fatal("restore must not delete unrelated files in the target")
	}

	writeFile(t, filepath.Join(dsh, ".credentials.yaml"), "api_key: changed\n")
	defaultRestore, err := manager.Restore(context.Background(), backup.RestoreInput{
		TaskID:             "test-restore-default",
		Config:             cfg,
		Snapshot:           result.Snapshot,
		IncludeDesktop:     true,
		PreRestoreSnapshot: true,
		Force:              true,
	})
	if err != nil {
		t.Fatalf("default restore failed: %v", err)
	}
	if !defaultRestore.DesktopRestored {
		t.Fatal("default restore should include desktop state")
	}
	defaultPre, err := manifest.Read(defaultRestore.PreRestoreSnapshot.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !defaultPre.DesktopIncluded {
		t.Fatal("pre-restore snapshot must protect the desktop state being overwritten")
	}
	restoredDesktop, err := os.ReadFile(filepath.Join(desktop, "Local State"))
	if err != nil || string(restoredDesktop) != `{"os_crypt":{}}` {
		t.Fatalf("desktop Local State not restored: %v %q", err, string(restoredDesktop))
	}

	deleteTarget, _, err := manifest.Scan(paths.DefaultBackupDir)
	if err != nil {
		t.Fatal(err)
	}
	var manual *manifest.Snapshot
	for index := range deleteTarget {
		if deleteTarget[index].Kind == manifest.KindManual {
			manual = &deleteTarget[index]
			break
		}
	}
	if manual == nil {
		t.Fatal("manual snapshot not found")
	}
	if err := manager.DeleteLocal(context.Background(), *manual, paths.DefaultBackupDir); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := os.Stat(manual.LocalPath); !os.IsNotExist(err) {
		t.Fatal("archive should be deleted")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
