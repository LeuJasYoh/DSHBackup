//go:build windows

package pathguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func finalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	pointer, err := windows.UTF16PtrFromString(absolute)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(
		pointer,
		windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, 32768)
	length, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
	if err != nil {
		return "", err
	}
	if length == 0 || int(length) > len(buffer) {
		return "", fmt.Errorf("无法解析最终路径：%s", path)
	}
	resolved := windows.UTF16ToString(buffer[:length])
	if strings.HasPrefix(resolved, `\\?\UNC\`) {
		resolved = `\\` + strings.TrimPrefix(resolved, `\\?\UNC\`)
	} else if strings.HasPrefix(resolved, `\\?\`) {
		resolved = strings.TrimPrefix(resolved, `\\?\`)
	} else if strings.HasPrefix(resolved, `\??\`) {
		resolved = strings.TrimPrefix(resolved, `\??\`)
	}
	return filepath.Clean(resolved), nil
}

func resolveExisting(path string) (string, error) {
	return finalPath(path)
}

func ResolveTarget(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("路径为空")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	if resolved, err := resolveExisting(absolute); err == nil {
		return resolved, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	existing := absolute
	var missing []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("无法解析路径：%s", path)
		}
		missing = append([]string{filepath.Base(existing)}, missing...)
		existing = parent
	}
	resolved, err := resolveExisting(existing)
	if err != nil {
		return "", err
	}
	for _, part := range missing {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, `<>:"|?*`) {
			return "", fmt.Errorf("路径包含非法片段：%s", part)
		}
		resolved = filepath.Join(resolved, part)
	}
	return filepath.Clean(resolved), nil
}

func Contains(child, parent string) (bool, error) {
	childResolved, err := ResolveTarget(child)
	if err != nil {
		return false, err
	}
	parentResolved, err := ResolveTarget(parent)
	if err != nil {
		return false, err
	}
	childResolved = strings.TrimRight(childResolved, `\/`)
	parentResolved = strings.TrimRight(parentResolved, `\/`)
	if strings.EqualFold(childResolved, parentResolved) {
		return true, nil
	}
	prefix := parentResolved + string(filepath.Separator)
	return len(childResolved) > len(prefix) && strings.EqualFold(childResolved[:len(prefix)], prefix), nil
}

func Overlaps(left, right string) (bool, error) {
	leftInRight, err := Contains(left, right)
	if err != nil {
		return false, err
	}
	if leftInRight {
		return true, nil
	}
	return Contains(right, left)
}

func Identity(path string) (string, error) {
	resolved, err := resolveExisting(path)
	if err != nil {
		return "", err
	}
	pointer, err := windows.UTF16PtrFromString(resolved)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(
		pointer,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return "", err
	}
	return fmt.Sprintf("%08x:%08x%08x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
}
