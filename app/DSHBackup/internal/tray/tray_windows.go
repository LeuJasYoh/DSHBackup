//go:build windows

// Package tray implements the Windows notification-area icon. Wails v2 has no
// working tray backend (the only AddTrayMenu helper is never called by any
// platform), so this talks to Shell_NotifyIconW directly.
package tray

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
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
	procGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	procShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	wndProcCallback            = syscall.NewCallback(windowProc)
)

const (
	wmApp          = 0x8000
	wmTrayCallback = wmApp + 0x2B0
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

	hwndMessage = ^uintptr(0) - 2 // (HWND)-3, the message-only window parent

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
	hwnd      uintptr
	icon      uintptr
	menu      uintptr
	className *uint16
	callbacks Callbacks

	mu     sync.Mutex
	closed bool
}

var active *Tray

// New creates the hidden window and adds the notification-area icon. The
// message loop runs on its own locked OS thread.
func New(iconData []byte, tipText string, callbacks Callbacks) (*Tray, error) {
	instance, _, _ := procGetModuleHandleW.Call(0)
	if instance == 0 {
		return nil, errors.New("无法获取进程模块句柄")
	}
	className, err := windows.UTF16PtrFromString("DSHBackupTrayWindow")
	if err != nil {
		return nil, err
	}
	class := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   wndProcCallback,
		Instance:  instance,
		ClassName: className,
	}
	if result, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class))); result == 0 {
		return nil, fmt.Errorf("注册托盘窗口失败：%v", callErr)
	}
	windowTitle, _ := windows.UTF16PtrFromString("DSH Backup")
	hwnd, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowTitle)),
		0, 0, 0, 0, 0,
		hwndMessage, 0, instance, 0,
	)
	if hwnd == 0 {
		_, _, _ = procUnregisterClassW.Call(uintptr(unsafe.Pointer(className)), instance)
		return nil, fmt.Errorf("创建托盘窗口失败：%v", callErr)
	}

	tray := &Tray{hwnd: hwnd, className: className, callbacks: callbacks}
	tray.icon = createIcon(iconData)
	tray.menu = buildMenu()
	active = tray
	if err := tray.addIcon(tipText); err != nil {
		tray.destroy()
		return nil, err
	}
	go tray.messageLoop()
	return tray, nil
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
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed {
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

func (t *Tray) messageLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
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

func windowProc(hwnd, message, wparam, lparam uintptr) uintptr {
	tray := active
	if tray != nil && message == wmTrayCallback && hwnd == tray.hwnd {
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

func (t *Tray) destroy() {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	t.mu.Unlock()
	data := notifyIconData{
		Size: uint32(unsafe.Sizeof(notifyIconData{})),
		Wnd:  t.hwnd,
		ID:   notificationID,
	}
	_, _, _ = procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
	if t.menu != 0 {
		_, _, _ = procDestroyMenu.Call(t.menu)
	}
	if t.icon != 0 {
		_, _, _ = procDestroyIcon.Call(t.icon)
	}
	if t.hwnd != 0 {
		_, _, _ = procDestroyWindow.Call(t.hwnd)
	}
	if t.className != nil {
		instance, _, _ := procGetModuleHandleW.Call(0)
		_, _, _ = procUnregisterClassW.Call(uintptr(unsafe.Pointer(t.className)), instance)
	}
	if active == t {
		active = nil
	}
}

// Close removes the icon and the hidden window. Safe to call twice.
func (t *Tray) Close() {
	if t == nil {
		return
	}
	t.destroy()
}
