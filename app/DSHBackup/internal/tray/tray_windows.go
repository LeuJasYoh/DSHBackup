//go:build windows

// Package tray implements the Windows notification-area icon. Wails v2 has no
// working tray backend (the only AddTrayMenu helper is never called by any
// platform), so this talks to Shell_NotifyIconW directly.
package tray

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW       = user32.NewProc("RegisterClassExW")
	procUnregisterClassW       = user32.NewProc("UnregisterClassW")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDefWindowProcW         = user32.NewProc("DefWindowProcW")
	procGetMessageW            = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessageW       = user32.NewProc("DispatchMessageW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procDestroyWindow          = user32.NewProc("DestroyWindow")
	procCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	procAppendMenuW            = user32.NewProc("AppendMenuW")
	procDestroyMenu            = user32.NewProc("DestroyMenu")
	procTrackPopupMenuEx       = user32.NewProc("TrackPopupMenuEx")
	procGetCursorPos           = user32.NewProc("GetCursorPos")
	procSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	procPostMessageW           = user32.NewProc("PostMessageW")
	procCreateIconFromResource = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon            = user32.NewProc("DestroyIcon")
	procLoadIconW              = user32.NewProc("LoadIconW")
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	procExtractIconExW         = shell32.NewProc("ExtractIconExW")
	procGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	procShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	wndProcCallback            = syscall.NewCallback(windowProc)
)

const (
	wmApp          = 0x8000
	wmTrayCallback = wmApp + 0x2B0
	wmShutdown     = wmApp + 0x2B1
	wmNull         = 0x0000
	wmRButtonUp    = 0x0205
	wmContextMenu  = 0x007B
	wmLButtonUp    = 0x0202
	ninSelect      = 0x0400
	ninKeySelect   = 0x0401

	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4

	nifMessage = 0x0001
	nifIcon    = 0x0002
	nifTip     = 0x0004

	notifyIconVersion4 = 4

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmNoNotify    = 0x0080

	mfString    = 0x0000
	mfSeparator = 0x0800

	idiApplication = 32512
	lrDefaultColor = 0x0000

	cmdShow         = 1
	cmdBackup       = 2
	cmdOpenFolder   = 3
	cmdExit         = 4
	notificationID  = 1
	maximumTipRunes = 127
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type windowMessage struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Point    struct{ X, Y int32 }
	LPrivate uint32
}

type point struct{ X, Y int32 }

type notifyIconData struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUID            [16]byte
	BalloonIcon     uintptr
}

type Callbacks struct {
	Show             func()
	Backup           func()
	OpenBackupFolder func()
	Exit             func()
}

type Tray struct {
	hwnd           uintptr
	icon           uintptr
	menu           uintptr
	className      *uint16
	instance       uintptr
	taskbarCreated uint32
	callbacks      Callbacks
	closed         atomic.Bool
}

var active *Tray

// New creates the window, registers the icon and pumps messages on one locked
// OS thread. Creating the window elsewhere and pumping on another thread is the
// classic tray bug: the shell posts to the thread that created the window, so
// the callback never reaches a loop running on a different thread.
func New(iconData []byte, tipText string, callbacks Callbacks) (*Tray, error) {
	tray := &Tray{callbacks: callbacks}
	ready := make(chan error, 1)
	go tray.run(iconData, tipText, ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return tray, nil
}

func (t *Tray) run(iconData []byte, tipText string, ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := t.init(iconData, tipText); err != nil {
		ready <- err
		return
	}
	ready <- nil
	var message windowMessage
	for {
		result, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) == -1 || result == 0 {
			return
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}
}

func (t *Tray) init(iconData []byte, tipText string) error {
	instance, _, _ := procGetModuleHandleW.Call(0)
	if instance == 0 {
		return errors.New("无法获取进程模块句柄")
	}
	className, err := windows.UTF16PtrFromString("DSHBackupTrayWindow")
	if err != nil {
		return err
	}
	class := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   wndProcCallback,
		Instance:  instance,
		ClassName: className,
	}
	if result, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class))); result == 0 {
		return fmt.Errorf("注册托盘窗口失败：%v", callErr)
	}
	windowTitle, _ := windows.UTF16PtrFromString("DSH Backup")
	// A hidden top-level window, not HWND_MESSAGE: the shell ignores
	// message-only windows for both the icon and the callback messages.
	hwnd, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowTitle)),
		0, 0, 0, 0, 0,
		0, 0, instance, 0,
	)
	if hwnd == 0 {
		_, _, _ = procUnregisterClassW.Call(uintptr(unsafe.Pointer(className)), instance)
		return fmt.Errorf("创建托盘窗口失败：%v", callErr)
	}
	t.hwnd = hwnd
	t.className = className
	t.instance = instance
	if message, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(mustUTF16("TaskbarCreated")))); message != 0 {
		t.taskbarCreated = uint32(message)
	}
	t.icon = createIcon(iconData)
	t.menu = buildMenu()
	active = t
	if err := t.addIcon(tipText); err != nil {
		active = nil
		t.destroy()
		return err
	}
	return nil
}

func buildMenu() uintptr {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return 0
	}
	appendItem(menu, mfString, cmdShow, "打开 DSH Backup")
	appendItem(menu, mfString, cmdBackup, "立即备份")
	appendItem(menu, mfString|mfSeparator, 0, "")
	appendItem(menu, mfString, cmdOpenFolder, "打开备份目录")
	appendItem(menu, mfString|mfSeparator, 0, "")
	appendItem(menu, mfString, cmdExit, "退出")
	return menu
}

func appendItem(menu uintptr, flags uintptr, id uintptr, label string) {
	text, err := windows.UTF16PtrFromString(label)
	if err != nil {
		text, _ = windows.UTF16PtrFromString("")
	}
	_, _, _ = procAppendMenuW.Call(menu, flags, id, uintptr(unsafe.Pointer(text)))
}

func createIcon(data []byte) uintptr {
	// Prefer the icon embedded in the executable: it is the same bitmap the
	// shell shows for the app and avoids PNG-frame parsing differences.
	if executable, err := os.Executable(); err == nil {
		path, err := windows.UTF16PtrFromString(executable)
		if err == nil {
			var large, small uintptr
			if count, _, _ := procExtractIconExW.Call(uintptr(unsafe.Pointer(path)), 0, uintptr(unsafe.Pointer(&large)), uintptr(unsafe.Pointer(&small)), 1); count > 0 {
				if large != 0 {
					_, _, _ = procDestroyIcon.Call(large)
				}
				if small != 0 {
					return small
				}
			}
		}
	}
	if len(data) > 0 {
		icon, _, _ := procCreateIconFromResource.Call(
			uintptr(unsafe.Pointer(&data[0])),
			uintptr(len(data)),
			1, // fIcon
			0x00030000,
			32, 32,
			lrDefaultColor,
		)
		if icon != 0 {
			return icon
		}
	}
	icon, _, _ := procLoadIconW.Call(0, idiApplication)
	return icon
}

func (t *Tray) addIcon(tipText string) error {
	data := notifyIconData{
		Size:            uint32(unsafe.Sizeof(notifyIconData{})),
		Wnd:             t.hwnd,
		ID:              notificationID,
		Flags:           nifMessage | nifIcon | nifTip,
		CallbackMessage: wmTrayCallback,
		Icon:            t.icon,
	}
	copyTip(&data.Tip, tipText)
	if result, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&data))); result == 0 {
		return errors.New("无法创建系统托盘图标")
	}
	version := notifyIconData{
		Size:    uint32(unsafe.Sizeof(notifyIconData{})),
		Wnd:     t.hwnd,
		ID:      notificationID,
		Version: notifyIconVersion4,
	}
	_, _, _ = procShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(&version)))
	return nil
}

func copyTip(destination *[128]uint16, text string) {
	source, err := windows.UTF16FromString(text)
	if err != nil || len(source) == 0 {
		destination[0] = 0
		return
	}
	source = source[:len(source)-1] // drop the trailing NUL
	if len(source) > maximumTipRunes {
		source = source[:maximumTipRunes]
	}
	copy(destination[:], source)
	destination[len(source)] = 0
}

// SetTip updates the hover text without touching the icon or the menu.
func (t *Tray) SetTip(text string) {
	if t == nil || t.closed.Load() || t.hwnd == 0 {
		return
	}
	data := notifyIconData{
		Size:  uint32(unsafe.Sizeof(notifyIconData{})),
		Wnd:   t.hwnd,
		ID:    notificationID,
		Flags: nifTip,
	}
	copyTip(&data.Tip, text)
	_, _, _ = procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&data)))
}

func mustUTF16(value string) *uint16 {
	pointer, err := windows.UTF16PtrFromString(value)
	if err != nil {
		return nil
	}
	return pointer
}

func (t *Tray) iconData() *notifyIconData {
	data := notifyIconData{
		Size:            uint32(unsafe.Sizeof(notifyIconData{})),
		Wnd:             t.hwnd,
		ID:              notificationID,
		Flags:           nifMessage | nifIcon | nifTip,
		CallbackMessage: wmTrayCallback,
		Icon:            t.icon,
	}
	copyTip(&data.Tip, "DSH Backup")
	return &data
}

func windowProc(hwnd, message, wparam, lparam uintptr) uintptr {
	tray := active
	if tray == nil {
		result, _, _ := procDefWindowProcW.Call(hwnd, message, wparam, lparam)
		return result
	}
	if tray.taskbarCreated != 0 && message == uintptr(tray.taskbarCreated) {
		// Explorer restarted and dropped every icon; put ours back.
		_, _, _ = procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(tray.iconData())))
		return 0
	}
	if message == wmShutdown && hwnd == tray.hwnd {
		tray.destroy()
		return 0
	}
	if message == wmTrayCallback && hwnd == tray.hwnd {
		event := uint32(lparam) & 0xFFFF
		switch event {
		case wmRButtonUp, wmContextMenu:
			tray.showMenu()
			return 0
		case wmLButtonUp, ninSelect, ninKeySelect:
			tray.dispatch(tray.callbacks.Show)
			return 0
		}
	}
	result, _, _ := procDefWindowProcW.Call(hwnd, message, wparam, lparam)
	return result
}

func (t *Tray) dispatch(action func()) {
	if action == nil {
		return
	}
	go action()
}

func (t *Tray) showMenu() {
	var cursor point
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	_, _, _ = procSetForegroundWindow.Call(t.hwnd)
	command, _, _ := procTrackPopupMenuEx.Call(
		t.menu,
		tpmReturnCmd|tpmRightButton|tpmNoNotify,
		uintptr(cursor.X),
		uintptr(cursor.Y),
		t.hwnd,
		0,
	)
	// Documented workaround: without this the menu can stay on screen.
	_, _, _ = procPostMessageW.Call(t.hwnd, wmNull, 0, 0)
	switch command {
	case cmdShow:
		t.dispatch(t.callbacks.Show)
	case cmdBackup:
		t.dispatch(t.callbacks.Backup)
	case cmdOpenFolder:
		t.dispatch(t.callbacks.OpenBackupFolder)
	case cmdExit:
		t.dispatch(t.callbacks.Exit)
	}
}

// destroy runs on the tray thread, either during init failure or from the
// shutdown message, so every Win32 handle is released by its owning thread.
func (t *Tray) destroy() {
	data := notifyIconData{
		Size: uint32(unsafe.Sizeof(notifyIconData{})),
		Wnd:  t.hwnd,
		ID:   notificationID,
	}
	_, _, _ = procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
	if t.menu != 0 {
		_, _, _ = procDestroyMenu.Call(t.menu)
		t.menu = 0
	}
	if t.icon != 0 {
		_, _, _ = procDestroyIcon.Call(t.icon)
		t.icon = 0
	}
	if t.hwnd != 0 {
		_, _, _ = procDestroyWindow.Call(t.hwnd)
		t.hwnd = 0
	}
	if t.className != nil && t.instance != 0 {
		_, _, _ = procUnregisterClassW.Call(uintptr(unsafe.Pointer(t.className)), t.instance)
		t.className = nil
	}
	if active == t {
		active = nil
	}
	_, _, _ = procPostQuitMessage.Call(0)
}

// Close asks the tray thread to remove the icon and stop its loop. Safe to
// call twice and from any goroutine.
func (t *Tray) Close() {
	if t == nil || !t.closed.CompareAndSwap(false, true) {
		return
	}
	if t.hwnd != 0 {
		_, _, _ = procPostMessageW.Call(t.hwnd, wmShutdown, 0, 0)
	}
}
