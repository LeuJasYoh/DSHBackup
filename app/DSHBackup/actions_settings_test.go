package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"DSHBackup/internal/apppath"
	"DSHBackup/internal/config"
	"DSHBackup/internal/logging"
	"DSHBackup/internal/manifest"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	paths := apppath.Paths{
		AppDir:           root,
		DataDir:          filepath.Join(root, "data"),
		ConfigDir:        filepath.Join(root, "data", "config"),
		LogsDir:          filepath.Join(root, "data", "logs"),
		TempDir:          filepath.Join(root, "data", "temp"),
		KeysDir:          filepath.Join(root, "data", "keys"),
		WebViewDir:       filepath.Join(root, "data", "webview2"),
		DefaultBackupDir: filepath.Join(root, "backups"),
		UserHome:         filepath.Join(root, "home"),
		RoamingAppData:   filepath.Join(root, "roaming"),
	}
	return &App{paths: paths, cfg: config.Default(paths.DefaultBackupDir)}
}

func TestSaveConfigProtectsServerFields(t *testing.T) {
	for _, complete := range []bool{false, true} {
		app := newTestApp(t)
		current := app.configSnapshot()
		current.OnboardingComplete = complete
		current.LastScheduledAt = "2026-10-22T21:35:00+08:00"
		app.setConfig(current)
		input := current
		input.SchemaVersion = 99
		input.OnboardingComplete = !complete
		input.LastScheduledAt = "client-value"
		input.Retention.Local = 12
		if err := app.SaveConfig(input); err != nil {
			t.Fatal(err)
		}
		want := current
		want.Retention.Local = 12
		if got := app.configSnapshot(); !reflect.DeepEqual(got, want) {
			t.Fatalf("saved settings differ: got %+v, want %+v", got, want)
		}
		got, err := config.Load(app.paths.ConfigFile(), "unused")
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("persisted settings differ: %+v, %v", got, err)
		}
	}
}

func TestSettingsRejectChangesDuringTask(t *testing.T) {
	app := newTestApp(t)
	if err := app.SaveConfig(app.configSnapshot()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(app.paths.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	id, ctx, err := app.beginTask("backup")
	if err != nil {
		t.Fatal(err)
	}
	defer app.finishTask(id)
	input := app.configSnapshot()
	input.BackupDir = filepath.Join(app.paths.AppDir, "changed")
	for name, save := range map[string]func() error{
		"settings": func() error { return app.SaveConfig(input) },
		"onboarding": func() error {
			return app.CompleteOnboarding(OnboardingInput{BackupDir: input.BackupDir})
		},
	} {
		if err := save(); err == nil || !strings.Contains(err.Error(), "任务正在运行") {
			t.Fatalf("%s did not reject active task: %v", name, err)
		}
	}
	if _, err := os.Stat(input.BackupDir); !os.IsNotExist(err) {
		t.Fatalf("rejected setting created the directory: %v", err)
	}
	after, err := os.ReadFile(app.paths.ConfigFile())
	if err != nil || string(before) != string(after) {
		t.Fatalf("rejected setting changed the saved config: %v", err)
	}
	if err := app.CancelTask(); err != nil || ctx.Err() != context.Canceled {
		t.Fatalf("task cancellation failed: %v, %v", err, ctx.Err())
	}
	if err := app.SaveConfig(input); err == nil {
		t.Fatal("settings must remain locked until task completion")
	}
	app.finishTask(id)
	if err := app.SaveConfig(input); err != nil {
		t.Fatalf("settings should unlock after task completion: %v", err)
	}
}

func TestOnboardingIsServerControlled(t *testing.T) {
	app := newTestApp(t)
	input := app.configSnapshot()
	input.OnboardingComplete = true
	if err := app.SaveConfig(input); err != nil {
		t.Fatal(err)
	}
	if app.configSnapshot().OnboardingComplete {
		t.Fatal("SaveConfig must not complete onboarding")
	}
	if err := app.CompleteOnboarding(OnboardingInput{IncludeDesktop: true}); err != nil {
		t.Fatal(err)
	}
	if !app.configSnapshot().OnboardingComplete {
		t.Fatal("onboarding was not completed")
	}
	if err := app.CompleteOnboarding(OnboardingInput{}); err == nil {
		t.Fatal("replayed onboarding must not overwrite saved settings")
	}
}

func TestScheduledWriteDoesNotOverwriteSettings(t *testing.T) {
	app := newTestApp(t)
	at := time.Date(2026, 10, 22, 21, 35, 0, 0, time.UTC)
	input := app.configSnapshot()
	input.Retention.Local = 12
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := app.SaveConfig(input); err != nil {
			t.Error(err)
		}
	}()
	go func() {
		defer wg.Done()
		app.markScheduled(at)
	}()
	wg.Wait()
	got, err := config.Load(app.paths.ConfigFile(), "unused")
	if err != nil {
		t.Fatal(err)
	}
	if got.LastScheduledAt != at.Format(time.RFC3339) || got.Retention.Local != 12 {
		t.Fatalf("concurrent writes lost settings: %+v", got)
	}
	if !reflect.DeepEqual(got, app.configSnapshot()) {
		t.Fatal("saved and in-memory settings differ")
	}
}

func TestRejectedBackupDirectoryHasNoSideEffects(t *testing.T) {
	app := newTestApp(t)
	for _, dir := range []string{app.paths.DSHDataDir(), app.paths.DesktopStateDir(), filepath.Join(app.paths.DataDir, "unsafe")} {
		input := app.configSnapshot()
		input.BackupDir = dir
		if err := app.SaveConfig(input); err == nil {
			t.Fatalf("unsafe directory accepted: %s", dir)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("unsafe directory was created: %s, %v", dir, err)
		}
	}
}

func TestFailedTaskPreflightReleasesSettings(t *testing.T) {
	app := newTestApp(t)
	for _, start := range []func() (string, error){
		func() (string, error) { return app.StartBackup(false) },
		func() (string, error) { return app.StartRestore(RestoreRequest{}) },
		func() (string, error) { return app.StartVerify(VerifyRequest{}) },
	} {
		if _, err := start(); err == nil {
			t.Fatal("expected preflight failure")
		}
		if err := app.SaveConfig(app.configSnapshot()); err != nil {
			t.Fatalf("failed preflight retained task lock: %v", err)
		}
	}
}

func TestResolveLocalSnapshotRequiresReady(t *testing.T) {
	app := newTestApp(t)
	value := manifest.Manifest{
		SchemaVersion: 1,
		ID:            "20261022-213500",
		CreatedAt:     "2026-10-22T21:35:00Z",
		Kind:          manifest.KindManual,
		Archive: manifest.ArchiveInfo{
			Name:   manifest.ArchiveName("20261022-213500"),
			Size:   3,
			SHA256: strings.Repeat("a", 64),
		},
		Encryption: &manifest.EncryptionInfo{Enabled: true, Algorithm: "age", KeyID: "old-key", WrappedPassword: "b2xk"},
	}
	archive := filepath.Join(app.cfg.BackupDir, value.Archive.Name)
	if err := manifest.Write(manifest.ManifestPath(archive), value); err != nil {
		t.Fatal(err)
	}
	if _, err := app.resolveSnapshot(value.ID); err == nil {
		t.Fatal("missing archive accepted")
	}
	if err := os.WriteFile(archive, []byte("bad-size"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.resolveSnapshot(value.ID); err == nil {
		t.Fatal("size mismatch accepted")
	}
	if err := os.WriteFile(archive, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := app.resolveSnapshot(value.ID)
	if err != nil || !got.Encrypted || got.KeyID != "old-key" || got.LocalPath != archive {
		t.Fatalf("old encrypted local snapshot was not preserved: %+v, %v", got, err)
	}
	if _, err := app.resolveSnapshot(" "); err == nil {
		t.Fatal("empty snapshot ID accepted")
	}
	input := app.configSnapshot()
	input.BackupDir = filepath.Join(app.paths.AppDir, "new-backups")
	input.EncryptionLocal = true
	if err := app.ensureNoEncryptedLocalWithoutKeys(input); err == nil || !strings.Contains(err.Error(), "不会自动生成新密钥") {
		t.Fatalf("changing directories bypassed old key protection: %v", err)
	}
	if _, err := os.Stat(app.paths.KeysDir); !os.IsNotExist(err) {
		t.Fatalf("snapshot lookup or key checks generated replacement keys: %v", err)
	}
}

func TestUnreadableManifestBlocksReplacementKeys(t *testing.T) {
	app := newTestApp(t)
	if err := os.MkdirAll(app.cfg.BackupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(app.cfg.BackupDir, "old.manifest.json")
	if err := os.WriteFile(path, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.ensureNoEncryptedLocalWithoutKeys(app.configSnapshot()); err == nil {
		t.Fatal("unreadable old manifest allowed replacement keys")
	}
}

func TestDiagnosticsExcludeKeysAndCredentials(t *testing.T) {
	app := newTestApp(t)
	logger, err := logging.New(app.paths.LogsDir)
	if err != nil {
		t.Fatal(err)
	}
	app.logger = logger
	defer logger.Close()
	for _, dir := range []string{app.paths.KeysDir, app.paths.DSHDataDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "private-content"), []byte("must-not-export"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path, err := app.ExportDiagnostics()
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		entry, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(entry)
		entry.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "must-not-export") || strings.Contains(file.Name, "private-content") {
			t.Fatalf("sensitive data exported in %s", file.Name)
		}
		if file.Name == "config.json" {
			var cfg map[string]json.RawMessage
			if err := json.Unmarshal(raw, &cfg); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"mode", "github", "proxy"} {
				if _, exists := cfg[field]; exists {
					t.Fatalf("obsolete config field exported: %s", field)
				}
			}
		}
	}
}
