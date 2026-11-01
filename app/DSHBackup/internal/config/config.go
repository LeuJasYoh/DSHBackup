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

	"DSHBackup/internal/scope"
	"DSHBackup/internal/sevenparams"
)

const SchemaVersion = 1

type ScheduleKind string

const (
	ScheduleManual ScheduleKind = "manual"
	ScheduleDaily  ScheduleKind = "daily"
	ScheduleWeekly ScheduleKind = "weekly"
)

type CloseBehavior string

const (
	CloseAsk  CloseBehavior = "ask"
	CloseExit CloseBehavior = "exit"
	CloseTray CloseBehavior = "tray"
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

// 每个域的默认保留份数。身份与配置很小可以多留；插件依赖体积最大，且只在
// 装卸插件时变化，留 3 份足够回到任意一次插件状态。
const (
	DefaultKeepCore     = 14
	DefaultKeepSessions = 7
	DefaultKeepPlugins  = 3
	DefaultKeepRuntime  = 1
)

type Retention struct {
	// Local 是 1.3.x 及以前的全局份数。现在只作为旧配置的迁移来源，界面上
	// 不再出现；缺失的分域份数优先取它，再退回默认值。
	Local int `json:"local"`
	// TrashDays is how long the trash keeps anything before auto-cleaning.
	// Zero means the user chose "never"; it is never implied by a missing
	// value because the default is 7.
	TrashDays int `json:"trashDays"`
	Core      int `json:"core"`
	Sessions  int `json:"sessions"`
	Plugins   int `json:"plugins"`
	Runtime   int `json:"runtime"`
}

// Keeps 返回每个域应保留的份数，顺序与 scope.Names() 一致。
func (r Retention) Keeps() map[scope.Domain]int {
	return map[scope.Domain]int{
		scope.DomainCore:     r.Core,
		scope.DomainSessions: r.Sessions,
		scope.DomainPlugins:  r.Plugins,
		scope.DomainRuntime:  r.Runtime,
	}
}

// Domains 记录可取舍的数据域。两个字段的零值就是产品默认：插件依赖进备份、
// 运行环境不进备份，所以旧配置不需要迁移。身份与配置、会话与附件不可关闭。
type Domains struct {
	SkipPluginDeps bool `json:"skipPluginDeps"`
	IncludeRuntime bool `json:"includeRuntime"`
}

func (d Domains) Selection() scope.Selection {
	return scope.Selection{Plugins: !d.SkipPluginDeps, Runtime: d.IncludeRuntime}
}

type Config struct {
	SchemaVersion      int           `json:"schemaVersion"`
	Theme              Theme         `json:"theme"`
	UpdateCheck        bool          `json:"updateCheck"`
	OnboardingComplete bool          `json:"onboardingComplete"`
	BackupDir          string        `json:"backupDir"`
	IncludeDesktop     bool          `json:"includeDesktop"`
	EncryptionLocal    bool          `json:"encryptionLocal"`
	Compression        Compression   `json:"compression"`
	Schedule           Schedule      `json:"schedule"`
	Retention          Retention     `json:"retention"`
	Domains            Domains       `json:"domains"`
	PreRestoreSnapshot bool          `json:"preRestoreSnapshot"`
	VerifyAfterBackup  bool          `json:"verifyAfterBackup"`
	CloseBehavior      CloseBehavior `json:"closeBehavior"`
	LastScheduledAt    string        `json:"lastScheduledAt"`
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
			Level:        "store",
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
			Local:     DefaultKeepSessions,
			TrashDays: 7,
			Core:      DefaultKeepCore,
			Sessions:  DefaultKeepSessions,
			Plugins:   DefaultKeepPlugins,
			Runtime:   DefaultKeepRuntime,
		},
		PreRestoreSnapshot: true,
		VerifyAfterBackup:  true,
		CloseBehavior:      CloseAsk,
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
	cfg.migrateLegacyMX()
	cfg.Normalize(defaultBackupDir)
	return cfg, nil
}

// Older builds allowed -mx in the advanced arguments, where it silently
// overrode the selected level. Fold the last value into the new preset.
func (c *Config) migrateLegacyMX() {
	var kept []string
	last := -1
	for _, raw := range c.Compression.ExtraArgs {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(strings.ToLower(trimmed), "-mx=") {
			var value int
			if _, err := fmt.Sscanf(strings.ToLower(trimmed), "-mx=%d", &value); err == nil && value >= 0 && value <= 9 {
				last = value
			}
			continue
		}
		kept = append(kept, raw)
	}
	if last >= 0 {
		c.Compression.Level = sevenparams.LevelForMX(last)
	}
	c.Compression.ExtraArgs = kept
	if c.Compression.ExtraArgs == nil {
		c.Compression.ExtraArgs = []string{}
	}
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
		c.Compression.Level = "store"
	}
	if c.Compression.ExtraArgs == nil {
		c.Compression.ExtraArgs = []string{}
	}
	switch c.CloseBehavior {
	case CloseAsk, CloseExit, CloseTray:
	default:
		c.CloseBehavior = CloseAsk
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
	if c.Retention.Core == 0 {
		c.Retention.Core = DefaultKeepCore
	}
	if c.Retention.Sessions == 0 {
		if c.Retention.Local > 0 {
			c.Retention.Sessions = c.Retention.Local
		} else {
			c.Retention.Sessions = DefaultKeepSessions
		}
	}
	if c.Retention.Plugins == 0 {
		c.Retention.Plugins = DefaultKeepPlugins
	}
	if c.Retention.Runtime == 0 {
		c.Retention.Runtime = DefaultKeepRuntime
	}
	// Local 只在旧配置缺份数字段时当迁移来源，之后不再回写：1.3.x 降级读取时
	// 仍能拿到一个合理的全局份数。
	if c.Retention.TrashDays < 0 {
		c.Retention.TrashDays = 7
	}
	if c.Retention.TrashDays > 365 {
		c.Retention.TrashDays = 365
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
	case "store", "fast", "quick", "balanced", "high", "max":
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
	for _, entry := range []struct {
		name  string
		value int
	}{
		{"身份与配置", cfg.Retention.Core},
		{"会话与附件", cfg.Retention.Sessions},
		{"插件依赖", cfg.Retention.Plugins},
		{"运行环境", cfg.Retention.Runtime},
	} {
		if entry.value < 1 || entry.value > 99 {
			return fmt.Errorf("%s 的保留份数必须在 1 到 99 之间", entry.name)
		}
	}
	if cfg.Retention.TrashDays < 0 || cfg.Retention.TrashDays > 365 {
		return errors.New("回收站保留天数必须在 0 到 365 之间（0 表示不自动清理）")
	}
	switch cfg.CloseBehavior {
	case CloseAsk, CloseExit, CloseTray:
	default:
		return fmt.Errorf("未知关闭行为：%s", cfg.CloseBehavior)
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
	if c.Schedule.Kind == ScheduleManual {
		return time.Time{}, false
	}
	if lastRun.IsZero() {
		return c.NextRun(now)
	}
	if lastRun.After(now) {
		return c.nextOccurrenceAfter(lastRun)
	}
	return c.NextRun(now)
}

func (c Config) nextOccurrenceAfter(after time.Time) (time.Time, bool) {
	hour, minute, err := parseClock(c.Schedule.Time)
	if err != nil {
		return time.Time{}, false
	}
	loc := after.Location()
	if c.Schedule.Kind == ScheduleDaily {
		candidate := time.Date(after.Year(), after.Month(), after.Day(), hour, minute, 0, 0, loc)
		if !candidate.After(after) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		return candidate, true
	}
	if c.Schedule.Kind != ScheduleWeekly {
		return time.Time{}, false
	}
	weekday := time.Weekday(c.Schedule.Weekday)
	daysAhead := (int(weekday) - int(after.Weekday()) + 7) % 7
	candidate := time.Date(after.Year(), after.Month(), after.Day(), hour, minute, 0, 0, loc).AddDate(0, 0, daysAhead)
	if !candidate.After(after) {
		candidate = candidate.AddDate(0, 0, 7)
	}
	return candidate, true
}

// DueOccurrence returns the latest scheduled occurrence at or before now that
// has not already been handled. A zero lastRun means the schedule is not armed.
func (c Config) DueOccurrence(now, lastRun time.Time) (time.Time, bool) {
	hour, minute, err := parseClock(c.Schedule.Time)
	if err != nil || c.Schedule.Kind == ScheduleManual || lastRun.IsZero() {
		return time.Time{}, false
	}
	loc := now.Location()
	if c.Schedule.Kind == ScheduleDaily {
		candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc)
		if candidate.After(now) {
			candidate = candidate.AddDate(0, 0, -1)
		}
		if candidate.After(lastRun) {
			return candidate, true
		}
		return time.Time{}, false
	}
	if c.Schedule.Kind != ScheduleWeekly {
		return time.Time{}, false
	}
	weekday := time.Weekday(c.Schedule.Weekday)
	daysBack := (int(now.Weekday()) - int(weekday) + 7) % 7
	candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc).AddDate(0, 0, -daysBack)
	if candidate.After(now) {
		candidate = candidate.AddDate(0, 0, -7)
	}
	if candidate.After(lastRun) {
		return candidate, true
	}
	return time.Time{}, false
}

func (c Config) ScheduleChanged(other Config) bool {
	return c.Schedule.Kind != other.Schedule.Kind ||
		c.Schedule.Time != other.Schedule.Time ||
		c.Schedule.Weekday != other.Schedule.Weekday
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
