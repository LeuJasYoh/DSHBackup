package main

import (
	"testing"
	"time"

	"DSHBackup/internal/config"
)

func TestSchedulerDoesNotReplayOccurrenceBeforeStartup(t *testing.T) {
	app := newTestApp(t)
	now := time.Now()
	cfg := app.configSnapshot()
	cfg.OnboardingComplete = true
	cfg.Schedule = config.Schedule{
		Kind:    config.ScheduleDaily,
		Time:    now.Add(-2 * time.Hour).Format("15:04"),
		Weekday: 1,
	}
	cfg.LastScheduledAt = now.AddDate(0, 0, -1).Format(time.RFC3339)
	app.setConfig(cfg)
	app.scheduleBase = now
	app.schedulerTick()
	if got := app.configSnapshot().LastScheduledAt; got != cfg.LastScheduledAt {
		t.Fatalf("missed occurrence was replayed at startup: %s -> %s", cfg.LastScheduledAt, got)
	}
}

func TestSchedulerArmsEmptyWatermark(t *testing.T) {
	app := newTestApp(t)
	cfg := app.configSnapshot()
	cfg.OnboardingComplete = true
	cfg.Schedule = config.Schedule{Kind: config.ScheduleDaily, Time: "03:00", Weekday: 1}
	app.setConfig(cfg)
	app.schedulerTick()
	if app.configSnapshot().LastScheduledAt == "" {
		t.Fatal("scheduler did not arm an empty watermark")
	}
}
