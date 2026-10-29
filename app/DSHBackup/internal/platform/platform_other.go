//go:build !windows

package platform

import "errors"

type DiskStatus struct {
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
}

func DiskFree(_ string) (DiskStatus, error) {
	return DiskStatus{}, errors.New("磁盘空间查询仅支持 Windows")
}

func IsDSHRunning(_ string) (bool, []string, error) {
	return false, nil, nil
}
