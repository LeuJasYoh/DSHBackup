//go:build windows

package backup_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"DSHBackup/internal/apppath"
	"DSHBackup/internal/backup"
	"DSHBackup/internal/config"
	"DSHBackup/internal/sidecar"
)

func safetyFixture(t *testing.T) (*backup.Manager, config.Config, apppath.Paths) {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	sidecarPath := filepath.Join(repo, "sidecar", "target", "release", "dsh-backup-core.exe")
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
	t.Cleanup(func() { _ = client.Close() })
	base := t.TempDir()
	appDir := filepath.Join(base, "app")
	home := filepath.Join(base, "home")
	roaming := filepath.Join(base, "roaming")
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
		RoamingAppData:   roaming,
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(paths.DSHDataDir(), ".credentials.yaml"), "api_key: safety\n")
	writeFile(t, filepath.Join(paths.DSHDataDir(), "payload.txt"), "archived-old")
	cfg := config.Default(paths.DefaultBackupDir)
	cfg.OnboardingComplete = true
	cfg.IncludeDesktop = false
	cfg.Compression.Level = "fast"
	return backup.NewManager(paths, client, nil, sevenZip, "audit", "audit"), cfg, paths
}

func safetyBackup(t *testing.T, manager *backup.Manager, cfg config.Config, id string) backup.BackupResult {
	t.Helper()
	result, err := manager.Backup(context.Background(), backup.BackupInput{TaskID: id, Config: cfg, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func createJunction(t *testing.T, link, target string) {
	t.Helper()
	output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("junction fixture: %v %s", err, output)
	}
}

func TestSafetyRestoreRejectsDestinationJunction(t *testing.T) {
	manager, cfg, paths := safetyFixture(t)
	writeFile(t, filepath.Join(paths.DSHDataDir(), "nested", "payload.txt"), "nested-archived")
	saved := safetyBackup(t, manager, cfg, "junction-source")
	target := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(target, "keep.txt"), "keep")
	canary := filepath.Join(outside, "payload.txt")
	writeFile(t, canary, "outside")
	createJunction(t, filepath.Join(target, "nested"), outside)
	_, err := manager.Restore(context.Background(), backup.RestoreInput{
		TaskID: "junction-restore", Config: cfg, Snapshot: saved.Snapshot,
		TargetDSH: target, PreRestoreSnapshot: true, Force: true,
	})
	if err == nil {
		t.Fatal("restore through a destination junction unexpectedly succeeded")
	}
	content, readErr := os.ReadFile(canary)
	if readErr != nil || string(content) != "outside" {
		t.Fatalf("restore escaped through destination junction: %q %v", content, readErr)
	}
}

func TestSafetyRestoreReplacesHardlinkWithoutTouchingOutside(t *testing.T) {
	manager, cfg, _ := safetyFixture(t)
	saved := safetyBackup(t, manager, cfg, "hardlink-source")
	target := t.TempDir()
	outside := t.TempDir()
	canary := filepath.Join(outside, "payload.txt")
	writeFile(t, canary, "outside")
	if err := os.Link(canary, filepath.Join(target, "payload.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Restore(context.Background(), backup.RestoreInput{
		TaskID: "hardlink-restore", Config: cfg, Snapshot: saved.Snapshot,
		TargetDSH: target, PreRestoreSnapshot: true, Force: true,
	}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(canary)
	if err != nil || string(content) != "outside" {
		t.Fatalf("hard-linked outside file changed: %q %v", content, err)
	}
	restored, err := os.ReadFile(filepath.Join(target, "payload.txt"))
	if err != nil || string(restored) != "archived-old" {
		t.Fatalf("target not replaced: %q %v", restored, err)
	}
}

func TestSafetyReadOnlyDestinationIsReplacedSafely(t *testing.T) {
	manager, cfg, _ := safetyFixture(t)
	saved := safetyBackup(t, manager, cfg, "readonly-source")
	target := t.TempDir()
	destination := filepath.Join(target, "payload.txt")
	writeFile(t, destination, "old")
	pointer, _ := windows.UTF16PtrFromString(destination)
	if err := windows.SetFileAttributes(pointer, windows.FILE_ATTRIBUTE_READONLY); err != nil {
		t.Fatal(err)
	}
	defer windows.SetFileAttributes(pointer, windows.FILE_ATTRIBUTE_NORMAL)
	if _, err := manager.Restore(context.Background(), backup.RestoreInput{
		TaskID: "readonly-restore", Config: cfg, Snapshot: saved.Snapshot,
		TargetDSH: target, Force: true,
	}); err != nil {
		t.Fatalf("restoring over a read-only file failed: %v", err)
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "archived-old" {
		t.Fatalf("read-only destination was not replaced: %q %v", content, err)
	}
	attributes, err := windows.GetFileAttributes(pointer)
	if err != nil || attributes&windows.FILE_ATTRIBUTE_READONLY == 0 {
		t.Fatalf("read-only attribute was not preserved: %v %v", attributes, err)
	}
}

func TestSafetyCopyFailureKeepsExistingDestination(t *testing.T) {
	manager, cfg, paths := safetyFixture(t)
	writeFile(t, filepath.Join(paths.DSHDataDir(), "large.bin"), strings.Repeat("p", 2*1024*1024))
	saved := safetyBackup(t, manager, cfg, "copy-failure-source")
	target := t.TempDir()
	destination := filepath.Join(target, "large.bin")
	writeFile(t, destination, "existing")
	handle := windows.InvalidHandle
	overlap := windows.Overlapped{Offset: 512 * 1024}
	locked := false
	release := func() {
		if handle != windows.InvalidHandle {
			_ = windows.UnlockFileEx(handle, 0, 1024*1024, 0, &overlap)
			windows.CloseHandle(handle)
			handle = windows.InvalidHandle
		}
	}
	defer release()
	_, restoreErr := manager.Restore(context.Background(), backup.RestoreInput{
		TaskID: "copy-failure-restore", Config: cfg, Snapshot: saved.Snapshot,
		TargetDSH: target, Force: true,
		OnProgress: func(progress backup.Progress) {
			if progress.Phase != "install" || locked {
				return
			}
			source := filepath.Join(paths.TempDir, "restore-copy-failure-restore", "extract", "dsh", "large.bin")
			pointer, _ := windows.UTF16PtrFromString(source)
			var openErr error
			handle, openErr = windows.CreateFile(pointer, windows.GENERIC_READ|windows.GENERIC_WRITE,
				windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
				nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
			if openErr != nil {
				t.Errorf("open extracted fixture: %v", openErr)
				return
			}
			if err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1024*1024, 0, &overlap); err != nil {
				t.Errorf("lock fixture: %v", err)
				return
			}
			locked = true
		},
	})
	release()
	if !locked || restoreErr == nil {
		t.Fatalf("copy failure was not induced: locked=%v err=%v", locked, restoreErr)
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "existing" {
		t.Fatalf("failed restore destroyed original: %q %v", content, err)
	}
}

func TestSafetyProtectionSnapshotRejectsOmittedFile(t *testing.T) {
	manager, cfg, paths := safetyFixture(t)
	saved := safetyBackup(t, manager, cfg, "protection-source")
	live := filepath.Join(paths.DSHDataDir(), "payload.txt")
	writeFile(t, live, "irreplaceable-current")
	handle := windows.InvalidHandle
	locked := false
	release := func() {
		if handle != windows.InvalidHandle {
			windows.CloseHandle(handle)
			handle = windows.InvalidHandle
		}
	}
	defer release()
	_, restoreErr := manager.Restore(context.Background(), backup.RestoreInput{
		TaskID: "protection-restore", Config: cfg, Snapshot: saved.Snapshot,
		PreRestoreSnapshot: true, Force: true,
		OnProgress: func(progress backup.Progress) {
			if progress.Phase != "snapshot" || progress.Message != "恢复前快照：启动 7z 压缩" || locked {
				return
			}
			path := filepath.Join(paths.TempDir, "protection-restore-pre", "staging", "dsh", "payload.txt")
			pointer, _ := windows.UTF16PtrFromString(path)
			var openErr error
			handle, openErr = windows.CreateFile(pointer, windows.GENERIC_READ, 0, nil,
				windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
			if openErr != nil {
				t.Errorf("lock staged fixture: %v", openErr)
				return
			}
			locked = true
		},
	})
	release()
	if !locked {
		t.Fatal("fixture did not lock the staged payload")
	}
	if restoreErr == nil {
		t.Fatal("restore continued after protection snapshot omitted a file")
	}
	content, err := os.ReadFile(live)
	if err != nil || string(content) != "irreplaceable-current" {
		t.Fatalf("restore overwrote live data despite incomplete protection: %q %v", content, err)
	}
}

func TestSafetyEmptyCoreDirectoryRoundTrip(t *testing.T) {
	manager, cfg, paths := safetyFixture(t)
	if err := os.MkdirAll(filepath.Join(paths.DSHDataDir(), "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	saved := safetyBackup(t, manager, cfg, "empty-roundtrip")
	target := t.TempDir()
	if _, err := manager.Restore(context.Background(), backup.RestoreInput{
		TaskID: "empty-roundtrip-restore", Config: cfg, Snapshot: saved.Snapshot,
		TargetDSH: target, Force: true,
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(target, "sessions"))
	if err != nil || !info.IsDir() {
		t.Fatalf("empty directory was not restored: %v", err)
	}
}
