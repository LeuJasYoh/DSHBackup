package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"DSHBackup/internal/sevenparams"
)

const SchemaVersion = 1

type ScheduleKind string

const (
	ScheduleManual ScheduleKind = "manual"
	ScheduleDaily  ScheduleKind = "daily"
	ScheduleWeekly ScheduleKind = "weekly"
)

type Theme string

const (
	ThemeSystem Theme = "system"
	ThemeDark   Theme = "dark"
	ThemeLight  Theme = "light"
)

type Compression struct {
	Level        string   `json:"level"`
	DictionaryMB int      `json:"dictionaryMb"`
	Threads      int      `json:"threads"`
	ExtraArgs    []string `json:"extraArgs"`
}

type Schedule struct {
	Kind    ScheduleKind `json:"kind"`
	Time    string       `json:"time"`
	Weekday int          `json:"weekday"`
}

type Retention struct {
	Local int `json:"local"`
}

type Config struct {
	SchemaVersion      int         `json:"schemaVersion"`
	Theme              Theme       `json:"theme"`
	UpdateCheck        bool        `json:"updateCheck"`
	OnboardingComplete bool        `json:"onboardingComplete"`
	BackupDir          string      `json:"backupDir"`
	IncludeDesktop     bool        `json:"includeDesktop"`
	EncryptionLocal    bool        `json:"encryptionLocal"`
	Compression        Compression `json:"compression"`
	Schedule           Schedule    `json:"schedule"`
	Retention          Retention   `json:"retention"`
	PreRestoreSnapshot bool        `json:"preRestoreSnapshot"`
	VerifyAfterBackup  bool        `json:"verifyAfterBackup"`
	LastScheduledAt    string      `json:"lastScheduledAt"`
}

var timePattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func Default(defaultBackupDir string) Config {
	return Config{
		SchemaVersion:      SchemaVersion,
		Theme:              ThemeSystem,
		UpdateCheck:        true,
		OnboardingComplete: false,
		BackupDir:          defaultBackupDir,
		IncludeDesktop:     true,
		EncryptionLocal:    false,
		Compression: Compression{
			Level:        "balanced",
			DictionaryMB: 0,
			Threads:      0,
			ExtraArgs:    []string{},
		},
		Schedule: Schedule{
			Kind:    ScheduleManual,
			Time:    "03:00",
			Weekday: 1,
		},
		Retention: Retention{
			Local: 7,
		},
		PreRestoreSnapshot: true,
		VerifyAfterBackup:  true,
	}
}

func Load(path, defaultBackupDir string) (Config, error) {
	cfg := Default(defaultBackupDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Default(defaultBackupDir), fmt.Errorf("配置格式损坏：%w", err)
	}
	cfg.Normalize(defaultBackupDir)
	return cfg, nil
}

func (c *Config) Normalize(defaultBackupDir string) {
	if c.SchemaVersion == 0 {
		c.SchemaVersion = SchemaVersion
	}
	if c.Theme != ThemeSystem && c.Theme != ThemeDark && c.Theme != ThemeLight {
		c.Theme = ThemeSystem
	}
	if c.BackupDir == "" {
		c.BackupDir = defaultBackupDir
	}
	if c.Compression.Level == "" {
		c.Compression.Level = "balanced"
	}
	if c.Compression.ExtraArgs == nil {
		c.Compression.ExtraArgs = []string{}
	}
	if c.Schedule.Kind == "" {
		c.Schedule.Kind = ScheduleManual
	}
	if c.Schedule.Time == "" {
		c.Schedule.Time = "03:00"
	}
	if c.Schedule.Weekday < 0 || c.Schedule.Weekday > 6 {
		c.Schedule.Weekday = 1
	}
	if c.Retention.Local == 0 {
		c.Retention.Local = 7
	}
}

func Save(path string, cfg Config) error {
	if err := Validate(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	temp := path + ".tmp"
	if err := os.WriteFile(temp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			_ = os.Remove(temp)
			return err
		}
		if err := os.Rename(temp, path); err != nil {
			_ = os.Remove(temp)
			return err
		}
	}
	return nil
}

func Validate(cfg Config) error {
	if cfg.BackupDir == "" {
		return errors.New("本地备份目录不能为空")
	}
	if cfg.Theme != ThemeSystem && cfg.Theme != ThemeDark && cfg.Theme != ThemeLight {
		return fmt.Errorf("未知界面主题：%s", cfg.Theme)
	}
	if !filepath.IsAbs(cfg.BackupDir) {
		return fmt.Errorf("本地备份目录必须是绝对路径：%s", cfg.BackupDir)
	}
	switch cfg.Compression.Level {
	case "fast", "balanced", "max":
	default:
		return fmt.Errorf("未知压缩档位：%s", cfg.Compression.Level)
	}
	if !sevenparams.DictionaryAllowed(cfg.Compression.DictionaryMB) {
		return fmt.Errorf("字典大小不在允许范围：%d", cfg.Compression.DictionaryMB)
	}
	if !sevenparams.ThreadAllowed(cfg.Compression.Threads) {
		return fmt.Errorf("线程数不在允许范围：%d", cfg.Compression.Threads)
	}
	if _, err := sevenparams.ValidateExtra(cfg.Compression.ExtraArgs); err != nil {
		return err
	}
	if cfg.Retention.Local < 1 || cfg.Retention.Local > 99 {
		return errors.New("本地保留份数必须在 1 到 99 之间")
	}
	switch cfg.Schedule.Kind {
	case ScheduleManual, ScheduleDaily, ScheduleWeekly:
	default:
		return fmt.Errorf("未知计划类型：%s", cfg.Schedule.Kind)
	}
	if !timePattern.MatchString(cfg.Schedule.Time) {
		return errors.New("计划时间格式必须为 HH:MM")
	}
	if cfg.Schedule.Weekday < 0 || cfg.Schedule.Weekday > 6 {
		return errors.New("计划星期必须在 0 到 6 之间")
	}
	return nil
}

func (c Config) NextRun(now time.Time) (time.Time, bool) {
	if c.Schedule.Kind == ScheduleManual {
		return time.Time{}, false
	}
	hour, minute, err := parseClock(c.Schedule.Time)
	if err != nil {
		return time.Time{}, false
	}
	loc := now.Location()
	base := now
	if c.Schedule.Kind == ScheduleDaily {
		candidate := time.Date(base.Year(), base.Month(), base.Day(), hour, minute, 0, 0, loc)
		if !candidate.After(now) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		return candidate, true
	}
	weekday := time.Weekday(c.Schedule.Weekday)
	daysAhead := (int(weekday) - int(now.Weekday()) + 7) % 7
	candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc).AddDate(0, 0, daysAhead)
	if !candidate.After(now) {
		candidate = candidate.AddDate(0, 0, 7)
	}
	return candidate, true
}

func (c Config) NextRunAfter(lastRun time.Time) (time.Time, bool) {
	now := time.Now()
	if lastRun.IsZero() {
		return c.NextRun(now)
	}
	hour, minute, err := parseClock(c.Schedule.Time)
	if err != nil || c.Schedule.Kind == ScheduleManual {
		return time.Time{}, false
	}
	loc := lastRun.Location()
	if c.Schedule.Kind == ScheduleDaily {
		candidate := time.Date(lastRun.Year(), lastRun.Month(), lastRun.Day(), hour, minute, 0, 0, loc).AddDate(0, 0, 1)
		if candidate.Before(now.Add(-24 * time.Hour)) {
			candidate, _ = c.NextRun(now)
		}
		return candidate, true
	}
	candidate := lastRun.AddDate(0, 0, 7)
	if candidate.Before(now.Add(-7 * 24 * time.Hour)) {
		return c.NextRun(now)
	}
	return candidate, true
}

func parseClock(value string) (int, int, error) {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return 0, 0, errors.New("时间格式无效")
	}
	var hour, minute int
	if _, err := fmt.Sscanf(parts[0], "%d", &hour); err != nil {
		return 0, 0, err
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &minute); err != nil {
		return 0, 0, err
	}
	return hour, minute, nil
}
