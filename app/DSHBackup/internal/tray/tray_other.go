//go:build !windows

package tray

import "errors"

type Callbacks struct {
	Show             func()
	Backup           func()
	OpenBackupFolder func()
	Exit             func()
}

type Tray struct{}

func New(_ []byte, _ string, _ Callbacks) (*Tray, error) {
	return nil, errors.New("系统托盘仅支持 Windows")
}

func (t *Tray) Close()      {}
func (t *Tray) SetTip(string) {}
