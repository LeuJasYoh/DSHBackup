//go:build !windows

package pathguard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func ResolveTarget(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("路径为空")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	existing := filepath.Clean(absolute)
	var missing []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", err
		}
		missing = append([]string{filepath.Base(existing)}, missing...)
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	for _, part := range missing {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("路径包含非法片段")
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
	if childResolved == parentResolved {
		return true, nil
	}
	relative, err := filepath.Rel(parentResolved, childResolved)
	if err != nil {
		return false, nil
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)), nil
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
	resolved, err := ResolveTarget(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	return resolved + "|" + info.ModTime().String(), nil
}
