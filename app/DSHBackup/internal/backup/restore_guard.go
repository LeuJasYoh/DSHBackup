package backup

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

const (
	// RestoreRiskPrefix marks an error the UI may turn into a confirmation.
	RestoreRiskPrefix       = "RESTORE_RISK:"
	restoreRatioLimit       = 200.0
	restoreRiskMinimumBytes = 256 << 20
	restoreCandidateLimit   = 20
)

// RestoreRiskError is returned before extraction when the archive claims to
// expand far beyond its on-disk size. It is not a refusal: the caller may
// retry with Force once the user has seen the numbers.
type RestoreRiskError struct {
	Uncompressed int64
	Compressed   int64
	Ratio        float64
}

func (e *RestoreRiskError) Error() string {
	return fmt.Sprintf("%s归档解压后约 %s，是归档大小的 %.0f 倍。请先确认目标磁盘有足够空间，再决定是否继续。",
		RestoreRiskPrefix, formatBytes(e.Uncompressed), e.Ratio)
}

// checkRestoreFootprint only flags implausible ratios for archives large enough
// to matter; small, highly compressible archives are normal.
func checkRestoreFootprint(uncompressed, compressed int64) error {
	if uncompressed < restoreRiskMinimumBytes || compressed <= 0 {
		return nil
	}
	ratio := float64(uncompressed) / float64(compressed)
	if ratio <= restoreRatioLimit {
		return nil
	}
	return &RestoreRiskError{Uncompressed: uncompressed, Compressed: compressed, Ratio: ratio}
}

// FindRestoreCandidates reports target-side `.dshbak-*.tmp` replacement
// candidates left by an abnormal exit. It only reads; nothing is deleted.
func FindRestoreCandidates(root string, limit int) []string {
	if strings.TrimSpace(root) == "" || limit <= 0 {
		return nil
	}
	found := []string{}
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if strings.Contains(name, ".dshbak-") && strings.HasSuffix(name, ".tmp") {
			found = append(found, path)
			if len(found) >= limit {
				return fs.SkipAll
			}
		}
		return nil
	})
	return found
}

// RestoreCandidates scans the directories a restore would actually touch.
func RestoreCandidates(targets []string) []string {
	found := []string{}
	for _, target := range targets {
		found = append(found, FindRestoreCandidates(target, restoreCandidateLimit-len(found))...)
		if len(found) >= restoreCandidateLimit {
			break
		}
	}
	return found
}
