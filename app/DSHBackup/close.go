package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"DSHBackup/internal/config"
	"DSHBackup/internal/tray"
)

// beforeClose decides what the title-bar close button does. Returning true
// cancels the close; the tray or the close prompt takes over from there.
func (a *App) beforeClose(ctx context.Context) bool {
	if a.quitting.Load() {
		return false
	}
	// Closing before the UI is ready would leave a window that cannot be
	// dismissed, because the close prompt has nobody to talk to.
	if a.ctx == nil {
		return false
	}
	select {
	case <-a.ready:
	default:
		return false
	}
	switch a.configSnapshot().CloseBehavior {
	case config.CloseExit:
		a.quitting.Store(true)
		go runtime.Quit(ctx)
		return true
	case config.CloseTray:
		if a.tray == nil {
			return false
		}
		a.hideWindow()
		return true
	default:
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "app:close-request", map[string]any{"taskRunning": a.taskRunning()})
		}
		return true
	}
}

func (a *App) taskRunning() bool {
	a.taskMu.Lock()
	defer a.taskMu.Unlock()
	return a.active != nil
}

// CloseDecision applies the choice from the close prompt. remember persists it
// for next time; the immediate action always happens.
func (a *App) CloseDecision(action string, remember bool) error {
	switch action {
	case "exit":
		if remember {
			if err := a.persistCloseBehavior(config.CloseExit); err != nil {
				return err
			}
		}
		a.quitApplication()
		return nil
	case "tray":
		if a.tray == nil {
			return errors.New("系统托盘不可用")
		}
		if remember {
			if err := a.persistCloseBehavior(config.CloseTray); err != nil {
				return err
			}
		}
		a.hideWindow()
		return nil
	case "cancel":
		return nil
	default:
		return errors.New("未知的关闭选择")
	}
}

func (a *App) persistCloseBehavior(behavior config.CloseBehavior) error {
	a.cfgWriteMu.Lock()
	defer a.cfgWriteMu.Unlock()
	cfg := a.configSnapshot()
	if cfg.CloseBehavior == behavior {
		return nil
	}
	cfg.CloseBehavior = behavior
	if err := config.Save(a.paths.ConfigFile(), cfg); err != nil {
		return fmt.Errorf("保存关闭行为失败：%w", err)
	}
	a.setConfig(cfg)
	a.emitStateChanged("settings")
	return nil
}

func (a *App) hideWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowHide(a.ctx)
	if a.tray != nil {
		a.tray.SetTip("DSH Backup 正在后台运行")
	}
}

func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx)
	a.focusWindow()
	if a.tray != nil {
		a.tray.SetTip("DSH Backup")
	}
}

func (a *App) focusWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	go func() {
		time.Sleep(300 * time.Millisecond)
		if a.ctx != nil && !a.quitting.Load() {
			runtime.WindowSetAlwaysOnTop(a.ctx, false)
		}
	}()
}

func (a *App) quitApplication() {
	if a.quitting.CompareAndSwap(false, true) {
		if a.tray != nil {
			a.tray.Close()
		}
		if a.ctx != nil {
			runtime.Quit(a.ctx)
		}
	}
}

func (a *App) logTray(action string) {
	if a.logger != nil {
		a.logger.Info("tray."+action, "托盘菜单操作", nil)
	}
}

func (a *App) startTray() {
	if a.tray != nil || len(a.trayIcon) == 0 {
		return
	}
	created, err := tray.New(a.trayIcon, "DSH Backup", tray.Callbacks{
		Show:             func() { a.logTray("show"); a.showWindow() },
		Backup:           func() { a.logTray("backup"); go func() { _, _ = a.StartBackup(false) }() },
		OpenBackupFolder: func() { a.logTray("open-folder"); _ = a.OpenBackupFolder() },
		Exit:             func() { a.logTray("exit"); a.quitApplication() },
	})
	if err != nil {
		a.initWarnings = append(a.initWarnings, "系统托盘不可用："+err.Error())
		if a.logger != nil {
			a.logger.Info("tray.unavailable", "系统托盘不可用", map[string]any{"error": err.Error()})
		}
		return
	}
	a.tray = created
}
