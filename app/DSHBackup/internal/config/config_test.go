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
	if cfg.Compression.Level != "store" {
		t.Fatalf("default compression should be store, got %s", cfg.Compression.Level)
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

func TestDueOccurrenceHandlesFirstAndStaleWatermarks(t *testing.T) {
	cfg := Default(t.TempDir())
	cfg.Schedule = Schedule{Kind: ScheduleDaily, Time: "03:00", Weekday: 1}
	loc := time.FixedZone("CST", 8*3600)
	last := time.Date(2026, 10, 29, 3, 0, 0, 0, loc)
	now := time.Date(2026, 10, 30, 10, 0, 0, 0, loc)
	due, ok := cfg.DueOccurrence(now, last)
	if !ok || due.Day() != 30 || due.Hour() != 3 {
		t.Fatalf("latest missed occurrence was not selected: %v %v", due, ok)
	}
	if _, ok := cfg.DueOccurrence(now, now); ok {
		t.Fatal("already handled occurrence became due again")
	}
	if _, ok := cfg.DueOccurrence(now, time.Time{}); ok {
		t.Fatal("an unarmed schedule must not run immediately")
	}
}

func TestWeeklyDueUsesConfiguredWeekday(t *testing.T) {
	cfg := Default(t.TempDir())
	cfg.Schedule = Schedule{Kind: ScheduleWeekly, Time: "03:00", Weekday: int(time.Friday)}
	loc := time.FixedZone("CST", 8*3600)
	last := time.Date(2026, 10, 24, 3, 0, 0, 0, loc) // previous Saturday
	now := time.Date(2026, 10, 30, 10, 0, 0, 0, loc) // Friday
	due, ok := cfg.DueOccurrence(now, last)
	if !ok || due.Weekday() != time.Friday || due.Day() != 30 {
		t.Fatalf("weekly due did not use configured Friday: %v %v", due, ok)
	}
	next, ok := cfg.NextRunAfter(last)
	if !ok || next.Weekday() != time.Friday || !next.After(now) {
		t.Fatalf("weekly next run did not use configured Friday: %v %v", next, ok)
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

func TestLegacyMXMigratesToStorePreset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := Default(filepath.Join(dir, "backups"))
	cfg.Compression = Compression{Level: "balanced", ExtraArgs: []string{"-mfb=64", "-mx=0"}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path, filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Compression.Level != "store" {
		t.Fatalf("-mx=0 did not migrate to store: %s", loaded.Compression.Level)
	}
	if len(loaded.Compression.ExtraArgs) != 1 || loaded.Compression.ExtraArgs[0] != "-mfb=64" {
		t.Fatalf("legacy -mx was not removed: %v", loaded.Compression.ExtraArgs)
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
				// Pre-1.3 configs had no trash retention field at all.
				delete(legacy["retention"].(map[string]any), "trashDays")
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
				if len(retention) != 2 || retention["local"] != want.Retention.Local || retention["trashDays"] != 7 {
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

func TestTrashRetentionDaysNormalize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cases := []struct {
		raw  string
		want int
	}{
		{`{"retention":{"local":7}}`, 7},
		{`{"retention":{"local":7,"trashDays":0}}`, 7},
		{`{"retention":{"local":7,"trashDays":-3}}`, 7},
		{`{"retention":{"local":7,"trashDays":30}}`, 30},
		{`{"retention":{"local":7,"trashDays":9000}}`, 365},
	}
	for _, test := range cases {
		if err := os.WriteFile(path, []byte(test.raw), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path, filepath.Join(dir, "fallback"))
		if err != nil {
			t.Fatalf("%s: %v", test.raw, err)
		}
		if cfg.Retention.TrashDays != test.want {
			t.Fatalf("%s: trash days = %d, want %d", test.raw, cfg.Retention.TrashDays, test.want)
		}
	}
}
