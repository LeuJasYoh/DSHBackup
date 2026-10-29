package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestValidateDefaults(t *testing.T) {
	cfg := Default(t.TempDir())
	if cfg.Theme != ThemeSystem {
		t.Fatalf("default theme should follow the system: %s", cfg.Theme)
	}
	if !cfg.UpdateCheck {
		t.Fatal("default config should check for updates")
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
}

func TestValidateRejectsUnsafe(t *testing.T) {
	cfg := Default(t.TempDir())
	cfg.BackupDir = "relative"
	if err := Validate(cfg); err == nil {
		t.Fatal("relative backup dir should be rejected")
	}
	cfg = Default(t.TempDir())
	cfg.Theme = "neon"
	if err := Validate(cfg); err == nil {
		t.Fatal("unknown theme should be rejected")
	}
	cfg = Default(t.TempDir())
	cfg.Compression.ExtraArgs = []string{"-psecret"}
	if err := Validate(cfg); err == nil {
		t.Fatal("dangerous 7z arg should be rejected")
	}
	cfg = Default(t.TempDir())
	cfg.Schedule.Time = "25:00"
	if err := Validate(cfg); err == nil {
		t.Fatal("invalid schedule time should be rejected")
	}
}

func TestNextRun(t *testing.T) {
	cfg := Default(t.TempDir())
	cfg.Schedule = Schedule{Kind: ScheduleDaily, Time: "03:00", Weekday: 1}
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 26, 2, 0, 0, 0, loc)
	next, ok := cfg.NextRun(now)
	if !ok || next.Hour() != 3 || next.Day() != 26 {
		t.Fatalf("unexpected daily next run: %v %v", next, ok)
	}
	now = time.Date(2026, 10, 26, 4, 0, 0, 0, loc)
	next, ok = cfg.NextRun(now)
	if !ok || next.Day() != 27 {
		t.Fatalf("daily next run should roll to tomorrow: %v", next)
	}
	cfg.Schedule = Schedule{Kind: ScheduleWeekly, Time: "03:00", Weekday: int(time.Monday)}
	now = time.Date(2026, 10, 26, 4, 0, 0, 0, loc) // Monday
	next, ok = cfg.NextRun(now)
	if !ok || next.Weekday() != time.Monday || next.Day() != 2 {
		t.Fatalf("weekly next run should be next Monday: %v", next)
	}
}

func TestLoadWithoutVisualPreferencesDefaultsToSystem(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw, err := json.Marshal(map[string]any{
		"schemaVersion":      1,
		"onboardingComplete": true,
		"backupDir":          filepath.Join(dir, "backups"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path, filepath.Join(dir, "default-backups"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Theme != ThemeSystem || !loaded.UpdateCheck {
		t.Fatalf("legacy config should default to system theme and update checks: %+v", loaded)
	}
}

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := Default(filepath.Join(dir, "backups"))
	cfg.OnboardingComplete = true
	cfg.Retention.Local = 9
	cfg.Theme = ThemeLight
	cfg.UpdateCheck = false
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path, filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.OnboardingComplete || loaded.Retention.Local != 9 || loaded.Theme != ThemeLight || loaded.UpdateCheck {
		t.Fatalf("unexpected loaded config: %+v", loaded)
	}
}

func TestLegacyConfigLoadsAndSavesLocalFields(t *testing.T) {
	for _, mode := range []string{"local", "cloud", "hybrid"} {
		for _, encrypted := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "/plain", true: "/encrypted"}[encrypted], func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "config.json")
				want := Default(filepath.Join(dir, "old-backups"))
				want.OnboardingComplete = true
				want.IncludeDesktop = false
				want.EncryptionLocal = encrypted
				want.Compression = Compression{Level: "max", DictionaryMB: 64, Threads: 2, ExtraArgs: []string{"-mqs=on"}}
				want.Schedule = Schedule{Kind: ScheduleWeekly, Time: "21:35", Weekday: 4}
				want.Retention.Local = 12
				want.PreRestoreSnapshot = false
				want.VerifyAfterBackup = false
				want.LastScheduledAt = "2026-10-22T21:35:00+08:00"
				raw, err := json.Marshal(want)
				if err != nil {
					t.Fatal(err)
				}
				var legacy map[string]any
				if err := json.Unmarshal(raw, &legacy); err != nil {
					t.Fatal(err)
				}
				legacy["mode"] = mode
				legacy["github"] = map[string]any{"clientId": "old-id", "owner": "old-owner", "repo": "old-repo", "autoUpload": true}
				legacy["proxy"] = map[string]any{"mode": "manual", "url": "invalid-old-address", "passwordSet": true}
				legacy["retention"].(map[string]any)["cloud"] = -1
				raw, err = json.Marshal(legacy)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, raw, 0o600); err != nil {
					t.Fatal(err)
				}
				got, err := Load(path, filepath.Join(dir, "new-default"))
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("local settings changed during migration: got %+v, want %+v", got, want)
				}
				if err := Save(path, got); err != nil {
					t.Fatal(err)
				}
				raw, err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var saved map[string]json.RawMessage
				if err := json.Unmarshal(raw, &saved); err != nil {
					t.Fatal(err)
				}
				for _, field := range []string{"mode", "github", "proxy"} {
					if _, exists := saved[field]; exists {
						t.Fatalf("obsolete field %q was saved", field)
					}
				}
				var retention map[string]int
				if err := json.Unmarshal(saved["retention"], &retention); err != nil {
					t.Fatal(err)
				}
				if len(retention) != 1 || retention["local"] != want.Retention.Local {
					t.Fatalf("unexpected retention: %v", retention)
				}
				reloaded, err := Load(path, "unused")
				if err != nil || !reflect.DeepEqual(reloaded, want) {
					t.Fatalf("saved local settings changed: %+v, %v", reloaded, err)
				}
			})
		}
	}
}
