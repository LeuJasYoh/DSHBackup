package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestValidateDefaults(t *testing.T) {
	cfg := Default("D:\\Backups")
	if err := Validate(cfg); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
	cfg.Mode = "cloud"
	cfg.GitHub.Owner = "o"
	cfg.GitHub.Repo = "r"
	if err := Validate(cfg); err != nil {
		t.Fatalf("cloud config should be valid: %v", err)
	}
}

func TestValidateRejectsUnsafe(t *testing.T) {
	cfg := Default("D:\\Backups")
	cfg.BackupDir = "relative"
	if err := Validate(cfg); err == nil {
		t.Fatal("relative backup dir should be rejected")
	}
	cfg = Default("D:\\Backups")
	cfg.Compression.ExtraArgs = []string{"-psecret"}
	if err := Validate(cfg); err == nil {
		t.Fatal("dangerous 7z arg should be rejected")
	}
	cfg = Default("D:\\Backups")
	cfg.Schedule.Time = "25:00"
	if err := Validate(cfg); err == nil {
		t.Fatal("invalid schedule time should be rejected")
	}
}

func TestNextRun(t *testing.T) {
	cfg := Default("D:\\Backups")
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

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := Default(filepath.Join(dir, "backups"))
	cfg.OnboardingComplete = true
	cfg.Retention.Local = 9
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path, filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.OnboardingComplete || loaded.Retention.Local != 9 {
		t.Fatalf("unexpected loaded config: %+v", loaded)
	}
}
