package main

import (
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

func (a *App) schedulerTick() {
	cfg := a.configSnapshot()
	if !cfg.OnboardingComplete || cfg.Schedule.Kind == config.ScheduleManual {
		return
	}
	var last time.Time
	if cfg.LastScheduledAt != "" {
		if parsed, err := time.Parse(time.RFC3339, cfg.LastScheduledAt); err == nil {
			last = parsed
		}
	}
	next, ok := cfg.NextRunAfter(last)
	if !ok || time.Now().Before(next) {
		return
	}
	a.taskMu.Lock()
	busy := a.active != nil
	a.taskMu.Unlock()
	if busy {
		return
	}
	running, evidence, err := a.checkDSHRunningForTask()
	if err == nil && running {
		a.markScheduled(time.Now())
		a.emitNotice("warning", "计划备份已跳过：DSH 正在运行（"+joinEvidence(evidence)+"）")
		return
	}
	a.markScheduled(time.Now())
	a.emitNotice("info", "计划备份已开始")
	if _, err := a.startBackup(false, manifest.KindScheduled); err != nil {
		a.emitNotice("error", "计划备份未能启动："+err.Error())
		if a.logger != nil {
			a.logger.Warn("schedule.start", "计划备份启动失败", map[string]any{"error": err.Error()})
		}
	}
}

func (a *App) markScheduled(at time.Time) {
	cfg := a.configSnapshot()
	cfg.LastScheduledAt = at.Format(time.RFC3339)
	if err := config.Save(a.paths.ConfigFile(), cfg); err != nil {
		if a.logger != nil {
			a.logger.Warn("schedule.save", "保存计划运行时间失败", map[string]any{"error": err.Error()})
		}
		a.setConfig(cfg)
		return
	}
	a.setConfig(cfg)
	a.emitStateChanged("schedule")
	if a.logger != nil {
		a.logger.Info("schedule.run", "计划备份触发", map[string]any{"kind": cfg.Schedule.Kind})
	}
}

func joinEvidence(evidence []string) string {
	result := ""
	for index, value := range evidence {
		if index > 0 {
			result += "；"
		}
		result += value
	}
	return result
}
