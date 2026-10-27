//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type DiskStatus struct {
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
}

func DiskFree(at string) (DiskStatus, error) {
	existing := at
	for {
		if _, err := os.Stat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		existing = parent
	}
	volume := filepath.VolumeName(existing)
	if volume == "" {
		return DiskStatus{}, errors.New("无法确定磁盘卷")
	}
	root := volume + `\`
	pointer, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return DiskStatus{}, err
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(pointer, &free, &total, &totalFree); err != nil {
		return DiskStatus{}, err
	}
	return DiskStatus{Free: free, Total: total}, nil
}

func IsDSHRunning(dshDir string) (bool, []string, error) {
	var evidence []string
	names, err := processNames()
	if err != nil {
		return false, nil, err
	}
	for _, name := range names {
		lower := strings.ToLower(name)
		if lower == "dsh.exe" || lower == "dsh-desktop.exe" || lower == "dshdesktop.exe" || lower == "dsh next.exe" {
			evidence = append(evidence, fmt.Sprintf("进程 %s 正在运行", name))
		}
	}
	if dshDir != "" {
		credentialPath := filepath.Join(dshDir, ".credentials.yaml")
		if _, err := os.Stat(credentialPath); err == nil {
			pointer, convErr := windows.UTF16PtrFromString(credentialPath)
			if convErr == nil {
				handle, openErr := windows.CreateFile(
					pointer,
					windows.GENERIC_READ,
					0,
					nil,
					windows.OPEN_EXISTING,
					windows.FILE_ATTRIBUTE_NORMAL,
					0,
				)
				if openErr != nil {
					if errors.Is(openErr, windows.ERROR_SHARING_VIOLATION) {
						evidence = append(evidence, "凭据文件被其他进程占用")
					}
				} else if handle != windows.InvalidHandle {
					_ = windows.CloseHandle(handle)
				}
			}
		}
	}
	return len(evidence) > 0, evidence, nil
}

func processNames() ([]string, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}
	var names []string
	for {
		names = append(names, windows.UTF16ToString(entry.ExeFile[:]))
		err := windows.Process32Next(snapshot, &entry)
		if err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return names, err
		}
	}
	return names, nil
}

func SystemProxy() (string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()
	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil && !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return "", err
	}
	if enabled == 0 {
		return "", nil
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return "", nil
		}
		return "", err
	}
	server = strings.TrimSpace(server)
	if server == "" {
		return "", nil
	}
	if strings.Contains(server, "=") {
		parts := strings.Split(server, ";")
		chosen := ""
		for _, part := range parts {
			part = strings.TrimSpace(part)
			lower := strings.ToLower(part)
			if strings.HasPrefix(lower, "https=") {
				chosen = strings.TrimSpace(part[len("https="):])
				break
			}
			if chosen == "" && (strings.HasPrefix(lower, "http=") || strings.HasPrefix(lower, "socks=")) {
				chosen = strings.TrimSpace(part[strings.Index(part, "=")+1:])
			}
		}
		server = chosen
	}
	if server == "" {
		return "", nil
	}
	if !strings.Contains(server, "://") {
		server = "http://" + server
	}
	return server, nil
}
