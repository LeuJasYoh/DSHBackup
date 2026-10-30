package main

import (
	"context"
	"strconv"
	"time"

	"DSHBackup/internal/config"
	"DSHBackup/internal/manifest"
)

func (a *App) schedulerLoop() {
	defer close(a.schedulerDone)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.schedulerStop:
			return
		case <-ticker.C:
			a.schedulerTick()
		}
	}
}

func (a *App) scheduleBlocked() bool {
	a.schedMu.Lock()
	defer a.schedMu.Unlock()
	return a.schedBlocked
}

func (a *App) setScheduleBlocked(value bool) {
	a.schedMu.Lock()
	a.schedBlocked = value
	a.schedMu.Unlock()
}

func (a *App) schedulerTick() {
	if a.scheduleBlocked() {
		return
	}
	cfg := a.configSnapshot()
	if !cfg.OnboardingComplete || cfg.Schedule.Kind == config.ScheduleManual {
		return
	}
	now := time.Now()
	var last time.Time
	if cfg.LastScheduledAt != "" {
		if parsed, err := time.Parse(time.RFC3339, cfg.LastScheduledAt); err == nil {
			last = parsed
		}
	}
	if last.IsZero() {
		if err := a.persistScheduled(now, "schedule.arm"); err != nil {
			a.blockSchedule(err)
			return
		}
		return
	}
	// Do not replay occurrences missed while the application was closed.
	if !a.scheduleBase.IsZero() && last.Before(a.scheduleBase) {
		last = a.scheduleBase
	}
	due, ok := cfg.DueOccurrence(now, last)
	if !ok {
		return
	}
	running, evidence, err := a.checkDSHRunningForTask()
	if err != nil {
		if saveErr := a.persistScheduled(due, "schedule.detect"); saveErr != nil {
			a.blockSchedule(saveErr)
			return
		}
		a.emitNotice("error", "计划备份未执行：无法检测 DSH 运行状态（"+err.Error()+"）")
		return
	}
	if running {
		if saveErr := a.persistScheduled(due, "schedule.skip"); saveErr != nil {
			a.blockSchedule(saveErr)
			return
		}
		a.emitNotice("warning", "计划备份已跳过：DSH 正在运行（"+joinEvidence(evidence)+"）")
		return
	}

	taskID, ctx, err := a.reserveScheduledTask(due)
	if err != nil {
		return
	}
	if taskID == "" {
		return
	}
	if err := a.persistScheduled(due, "schedule.run"); err != nil {
		a.finishTask(taskID)
		a.blockSchedule(err)
		return
	}
	a.emitNotice("info", "计划备份已开始")
	if err := a.launchBackup(taskID, ctx, false, manifest.KindScheduled); err != nil {
		a.finishTask(taskID)
		a.emitNotice("error", "计划备份未能启动："+err.Error())
		if a.logger != nil {
			a.logger.Warn("schedule.start", "计划备份启动失败", map[string]any{"error": err.Error()})
		}
	}
}

// reserveScheduledTask rechecks that the occurrence is still current and owns
// the task slot before the watermark is persisted.
func (a *App) reserveScheduledTask(occurrence time.Time) (string, context.Context, error) {
	a.taskMu.Lock()
	defer a.taskMu.Unlock()
	if a.active != nil {
		return "", nil, nil
	}
	cfg := a.configSnapshot()
	if !cfg.OnboardingComplete || cfg.Schedule.Kind == config.ScheduleManual {
		return "", nil, nil
	}
	var last time.Time
	if cfg.LastScheduledAt != "" {
		parsed, err := time.Parse(time.RFC3339, cfg.LastScheduledAt)
		if err == nil {
			last = parsed
		}
	}
	if !a.scheduleBase.IsZero() && last.Before(a.scheduleBase) {
		last = a.scheduleBase
	}
	current, ok := cfg.DueOccurrence(time.Now(), last)
	if !ok || !current.Equal(occurrence) {
		return "", nil, nil
	}
	taskID := "backup-" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	ctx, cancel := context.WithCancel(context.Background())
	a.active = &activeTask{ID: taskID, Kind: "backup", cancel: cancel}
	return taskID, ctx, nil
}

func (a *App) persistScheduled(at time.Time, event string) error {
	a.cfgWriteMu.Lock()
	defer a.cfgWriteMu.Unlock()
	cfg := a.configSnapshot()
	cfg.LastScheduledAt = at.Format(time.RFC3339)
	if err := config.Save(a.paths.ConfigFile(), cfg); err != nil {
		if a.logger != nil {
			a.logger.Warn(event, "保存计划运行时间失败", map[string]any{"error": err.Error()})
		}
		return err
	}
	a.setConfig(cfg)
	a.emitStateChanged("schedule")
	if a.logger != nil {
		a.logger.Info(event, "记录计划时刻", map[string]any{"kind": cfg.Schedule.Kind, "occurrence": cfg.LastScheduledAt})
	}
	return nil
}

func (a *App) markScheduled(at time.Time) {
	if err := a.persistScheduled(at, "schedule.run"); err != nil {
		a.blockSchedule(err)
	}
}

func (a *App) blockSchedule(cause error) {
	a.setScheduleBlocked(true)
	a.emitNotice("error", "计划备份已暂停：无法保存计划运行时间；请在设置中重新保存以恢复（"+cause.Error()+"）")
}

func joinEvidence(evidence []string) string {
	result := ""
	for index, value := range evidence {
		if index > 0 {
			result += "；"
		}
		result += value
	}
	if result == "" {
		return "已检测到 DSH 进程"
	}
	return result
}
