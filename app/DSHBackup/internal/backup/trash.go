package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"DSHBackup/internal/manifest"
	"DSHBackup/internal/pathguard"
	"DSHBackup/internal/platform"
)

const (
	// TrashDirName lives inside the backup directory so that moving an archive
	// into it is a same-volume rename instead of a copy.
	TrashDirName     = ".dshbackup-trash"
	trashMetaName    = "trash.json"
	trashSchema      = 1
	DefaultTrashDays = 7

	TrashReasonManual    = "manual"
	TrashReasonRetention = "retention"
)

type TrashMeta struct {
	SchemaVersion   int      `json:"schemaVersion"`
	SnapshotID      string   `json:"snapshotId"`
	Reason          string   `json:"reason"`
	MovedAt         string   `json:"movedAt"`
	ExpiresAt       string   `json:"expiresAt"`
	RetentionDays   int      `json:"retentionDays"`
	ArchiveName     string   `json:"archiveName"`
	ArchiveSize     int64    `json:"archiveSize"`
	ArchiveSHA256   string   `json:"archiveSha256"`
	ArchiveMissing  bool     `json:"archiveMissing,omitempty"`
	ManifestNames   []string `json:"manifestNames"`
	Encrypted       bool     `json:"encrypted,omitempty"`
	KeyID           string   `json:"keyId,omitempty"`
	Kind            string   `json:"kind"`
	CaptureStatus   string   `json:"captureStatus,omitempty"`
	OriginBackupDir string   `json:"originBackupDir"`
	OriginRootID    string   `json:"originRootIdentity"`
	AppVersion      string   `json:"appVersion,omitempty"`
}

type TrashItem struct {
	SnapshotID           string `json:"snapshotId"`
	ArchiveName          string `json:"archiveName"`
	Size                 int64  `json:"size"`
	MovedAt              string `json:"movedAt"`
	ExpiresAt            string `json:"expiresAt"`
	DaysLeft             int    `json:"daysLeft"`
	Reason               string `json:"reason"`
	Encrypted            bool   `json:"encrypted"`
	Kind                 string `json:"kind"`
	CaptureStatus        string `json:"captureStatus,omitempty"`
	ArchiveMissing       bool   `json:"archiveMissing"`
	BelongsToCurrentRoot bool   `json:"belongsToCurrentRoot"`
	Restorable           bool   `json:"restorable"`
	NeverExpires         bool   `json:"neverExpires"`
	BlockedReason        string `json:"blockedReason,omitempty"`
}

type TrashList struct {
	Items    []TrashItem `json:"items"`
	Count    int         `json:"count"`
	Bytes    int64       `json:"bytes"`
	Root     string      `json:"root"`
	Warnings []string    `json:"warnings,omitempty"`
}

func TrashRoot(backupDir string) string {
	return filepath.Join(backupDir, TrashDirName)
}

// checkTrashRoot refuses a trash directory that is itself a link, so deletes
// and moves never reach through it into an unrelated location.
func checkTrashRoot(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("回收站路径不是目录")
	}
	reparse, err := pathguard.IsReparsePoint(root)
	if err != nil {
		return fmt.Errorf("无法检查回收站目录：%w", err)
	}
	if reparse {
		return errors.New("回收站目录是链接或重解析点，已拒绝操作")
	}
	return nil
}

func validSnapshotID(id string) error {
	if id == "" || id == "." || id == ".." {
		return errors.New("快照 ID 无效")
	}
	for _, char := range id {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
		case char == '-', char == '_', char == '.':
		default:
			return fmt.Errorf("快照 ID 包含非法字符：%s", id)
		}
	}
	return nil
}

func validFileName(name string) error {
	if name == "" || name == "." || name == ".." {
		return errors.New("回收站记录中的文件名无效")
	}
	if filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("回收站记录中的文件名无效：%s", name)
	}
	return nil
}

// MoveToTrash renames the archive and every manifest alias into the trash
// directory. The operation is a metadata-only move; it never copies bytes.
func MoveToTrash(backupDir string, snapshot manifest.Snapshot, reason string, retentionDays int, appVersion string) (TrashMeta, error) {
	if err := validSnapshotID(snapshot.ID); err != nil {
		return TrashMeta{}, err
	}
	if strings.TrimSpace(snapshot.LocalPath) == "" || strings.TrimSpace(snapshot.ManifestPath) == "" {
		return TrashMeta{}, errors.New("快照没有本地文件")
	}
	if reason == "" {
		reason = TrashReasonManual
	}
	if retentionDays <= 0 {
		retentionDays = DefaultTrashDays
	}
	archiveName := filepath.Base(snapshot.LocalPath)
	manifestNames := []string{filepath.Base(snapshot.ManifestPath)}
	manifestPaths := []string{snapshot.ManifestPath}
	for _, alias := range snapshot.ManifestAliases {
		if strings.TrimSpace(alias) == "" {
			continue
		}
		inside, err := pathguard.Contains(alias, backupDir)
		if err != nil {
			return TrashMeta{}, fmt.Errorf("无法检查清单位置：%w", err)
		}
		if !inside {
			return TrashMeta{}, errors.New("拒绝移动备份目录之外的清单")
		}
		manifestNames = append(manifestNames, filepath.Base(alias))
		manifestPaths = append(manifestPaths, alias)
	}
	if err := validFileName(archiveName); err != nil {
		return TrashMeta{}, err
	}
	for _, name := range manifestNames {
		if err := validFileName(name); err != nil {
			return TrashMeta{}, err
		}
	}
	inside, err := pathguard.Contains(snapshot.LocalPath, backupDir)
	if err != nil {
		return TrashMeta{}, fmt.Errorf("无法检查归档位置：%w", err)
	}
	if !inside {
		return TrashMeta{}, errors.New("拒绝移动备份目录之外的文件")
	}

	archiveMissing := false
	archiveSize := snapshot.Size
	if info, statErr := os.Lstat(snapshot.LocalPath); statErr != nil {
		if !errors.Is(statErr, os.ErrNotExist) {
			return TrashMeta{}, fmt.Errorf("无法读取归档：%w", statErr)
		}
		archiveMissing = true
		archiveSize = 0
	} else {
		if info.IsDir() {
			return TrashMeta{}, errors.New("归档位置是目录，已拒绝移动")
		}
		reparse, err := pathguard.IsReparsePoint(snapshot.LocalPath)
		if err != nil {
			return TrashMeta{}, fmt.Errorf("无法检查归档类型：%w", err)
		}
		if reparse {
			return TrashMeta{}, errors.New("归档是链接或重解析点，已拒绝移动")
		}
		archiveSize = info.Size()
	}
	for _, manifestPath := range manifestPaths {
		if _, err := os.Lstat(manifestPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return TrashMeta{}, fmt.Errorf("无法读取清单：%w", err)
		}
		reparse, err := pathguard.IsReparsePoint(manifestPath)
		if err != nil {
			return TrashMeta{}, fmt.Errorf("无法检查清单类型：%w", err)
		}
		if reparse {
			return TrashMeta{}, errors.New("清单是链接或重解析点，已拒绝移动")
		}
	}

	root := TrashRoot(backupDir)
	itemDir := filepath.Join(root, snapshot.ID)
	if _, err := os.Lstat(itemDir); err == nil {
		return TrashMeta{}, errors.New("该归档已在回收站中")
	} else if !errors.Is(err, os.ErrNotExist) {
		return TrashMeta{}, fmt.Errorf("无法检查回收站：%w", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return TrashMeta{}, fmt.Errorf("无法创建回收站目录：%w", err)
	}
	if err := checkTrashRoot(root); err != nil {
		return TrashMeta{}, err
	}
	_ = platform.SetHidden(root)
	if err := os.Mkdir(itemDir, 0o755); err != nil {
		return TrashMeta{}, fmt.Errorf("无法创建回收站条目：%w", err)
	}
	_ = platform.SetHidden(itemDir)

	moved := make([][2]string, 0, len(manifestPaths)+1)
	rollback := func() {
		for index := len(moved) - 1; index >= 0; index-- {
			_ = os.Rename(moved[index][1], moved[index][0])
		}
		_ = os.RemoveAll(itemDir)
	}
	if !archiveMissing {
		destination := filepath.Join(itemDir, archiveName)
		if err := os.Rename(snapshot.LocalPath, destination); err != nil {
			_ = os.Remove(itemDir)
			return TrashMeta{}, fmt.Errorf("移入回收站失败：%w", err)
		}
		moved = append(moved, [2]string{snapshot.LocalPath, destination})
	}
	for index, manifestPath := range manifestPaths {
		if _, err := os.Lstat(manifestPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			rollback()
			return TrashMeta{}, fmt.Errorf("无法读取清单：%w", err)
		}
		destination := filepath.Join(itemDir, manifestNames[index])
		if err := os.Rename(manifestPath, destination); err != nil {
			rollback()
			return TrashMeta{}, fmt.Errorf("移入回收站失败：%w", err)
		}
		moved = append(moved, [2]string{manifestPath, destination})
	}

	now := time.Now()
	rootID, _ := pathguard.Identity(backupDir)
	meta := TrashMeta{
		SchemaVersion:   trashSchema,
		SnapshotID:      snapshot.ID,
		Reason:          reason,
		MovedAt:         now.Format(time.RFC3339),
		ExpiresAt:       now.AddDate(0, 0, retentionDays).Format(time.RFC3339),
		RetentionDays:   retentionDays,
		ArchiveName:     archiveName,
		ArchiveSize:     archiveSize,
		ArchiveSHA256:   snapshot.SHA256,
		ArchiveMissing:  archiveMissing,
		ManifestNames:   manifestNames,
		Encrypted:       snapshot.Encrypted,
		KeyID:           snapshot.KeyID,
		Kind:            snapshot.Kind,
		CaptureStatus:   snapshot.CaptureStatus,
		OriginBackupDir: backupDir,
		OriginRootID:    rootID,
		AppVersion:      appVersion,
	}
	if err := writeTrashMeta(itemDir, meta); err != nil {
		rollback()
		return TrashMeta{}, err
	}
	if _, err := readTrashMeta(itemDir); err != nil {
		rollback()
		return TrashMeta{}, fmt.Errorf("回收站记录写入后无法读回：%w", err)
	}
	return meta, nil
}

func writeTrashMeta(itemDir string, meta TrashMeta) error {
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	target := filepath.Join(itemDir, trashMetaName)
	temp := target + ".tmp"
	if err := os.WriteFile(temp, raw, 0o600); err != nil {
		return fmt.Errorf("写入回收站记录失败：%w", err)
	}
	if err := os.Rename(temp, target); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("写入回收站记录失败：%w", err)
	}
	return nil
}

func readTrashMeta(itemDir string) (TrashMeta, error) {
	raw, err := os.ReadFile(filepath.Join(itemDir, trashMetaName))
	if err != nil {
		return TrashMeta{}, errors.New("回收站记录缺失")
	}
	var meta TrashMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return TrashMeta{}, errors.New("回收站记录损坏")
	}
	if meta.SchemaVersion != trashSchema {
		return TrashMeta{}, errors.New("回收站记录版本不受支持")
	}
	if err := validSnapshotID(meta.SnapshotID); err != nil {
		return TrashMeta{}, err
	}
	if err := validFileName(meta.ArchiveName); err != nil {
		return TrashMeta{}, err
	}
	for _, name := range meta.ManifestNames {
		if err := validFileName(name); err != nil {
			return TrashMeta{}, err
		}
	}
	return meta, nil
}

func trashItemFromMeta(itemDir string, meta TrashMeta, backupDir, currentRootID string, now time.Time, autoClean bool) TrashItem {
	item := TrashItem{
		SnapshotID:     meta.SnapshotID,
		ArchiveName:    meta.ArchiveName,
		Size:           meta.ArchiveSize,
		MovedAt:        meta.MovedAt,
		ExpiresAt:      meta.ExpiresAt,
		Reason:         meta.Reason,
		Encrypted:      meta.Encrypted,
		Kind:           meta.Kind,
		CaptureStatus:  meta.CaptureStatus,
		ArchiveMissing: meta.ArchiveMissing,
	}
	item.BelongsToCurrentRoot = strings.EqualFold(meta.OriginRootID, currentRootID) ||
		strings.EqualFold(filepath.Clean(meta.OriginBackupDir), filepath.Clean(backupDir))
	item.NeverExpires = !autoClean
	if expires, err := time.Parse(time.RFC3339, meta.ExpiresAt); err == nil {
		item.DaysLeft = int(math.Ceil(expires.Sub(now).Hours() / 24))
		if item.DaysLeft < 0 {
			item.DaysLeft = 0
		}
	}
	archivePath := filepath.Join(itemDir, meta.ArchiveName)
	if !meta.ArchiveMissing {
		if info, err := os.Stat(archivePath); err == nil {
			item.Size = info.Size()
		} else {
			item.ArchiveMissing = true
		}
	}
	switch {
	case !item.BelongsToCurrentRoot:
		item.BlockedReason = "该备份来自其他备份目录，不会自动清理，也不能恢复到当前目录"
	case item.ArchiveMissing:
		item.BlockedReason = "归档文件已缺失，只能彻底删除"
	case !sameVolume(meta.OriginRootID, currentRootID):
		item.BlockedReason = "回收站与当前备份目录不在同一磁盘分区，无法瞬时恢复"
	}
	item.Restorable = item.BlockedReason == ""
	return item
}

func sameVolume(left, right string) bool {
	trim := func(value string) string {
		if index := strings.IndexByte(value, ':'); index > 0 {
			return value[:index]
		}
		return ""
	}
	leftVolume, rightVolume := trim(left), trim(right)
	if leftVolume == "" || rightVolume == "" {
		return true
	}
	return strings.EqualFold(leftVolume, rightVolume)
}

func ListTrash(backupDir string, autoClean bool) (TrashList, error) {
	list := TrashList{Items: []TrashItem{}, Root: TrashRoot(backupDir)}
	root := list.Root
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return list, nil
		}
		return list, err
	}
	currentRootID, _ := pathguard.Identity(backupDir)
	now := time.Now()
	for _, entry := range entries {
		if !entry.IsDir() {
			list.Warnings = append(list.Warnings, "回收站中有无法识别的内容："+entry.Name())
			continue
		}
		id := entry.Name()
		if err := validSnapshotID(id); err != nil {
			list.Warnings = append(list.Warnings, "回收站目录名无效："+id)
			continue
		}
		itemDir := filepath.Join(root, id)
		if reparse, err := pathguard.IsReparsePoint(itemDir); err == nil && reparse {
			list.Warnings = append(list.Warnings, "回收站条目是链接或重解析点，已跳过："+id)
			continue
		}
		meta, err := readTrashMeta(itemDir)
		if err != nil {
			// Keep unreadable entries visible so they can be removed by hand,
			// but never treat them as owned content for automatic cleanup.
			list.Items = append(list.Items, TrashItem{SnapshotID: id, BlockedReason: err.Error()})
			continue
		}
		item := trashItemFromMeta(itemDir, meta, backupDir, currentRootID, now, autoClean)
		list.Items = append(list.Items, item)
		list.Bytes += item.Size
	}
	sort.SliceStable(list.Items, func(i, j int) bool {
		return list.Items[i].MovedAt > list.Items[j].MovedAt
	})
	list.Count = len(list.Items)
	return list, nil
}

func RestoreTrashItem(backupDir, snapshotID string) error {
	if err := validSnapshotID(snapshotID); err != nil {
		return err
	}
	root := TrashRoot(backupDir)
	itemDir := filepath.Join(root, snapshotID)
	inside, err := pathguard.Contains(itemDir, root)
	if err != nil {
		return fmt.Errorf("无法检查回收站位置：%w", err)
	}
	if !inside {
		return errors.New("拒绝操作回收站之外的文件")
	}
	if err := checkTrashRoot(root); err != nil {
		return err
	}
	meta, err := readTrashMeta(itemDir)
	if err != nil {
		return err
	}
	currentRootID, err := pathguard.Identity(backupDir)
	if err != nil {
		return fmt.Errorf("无法确认当前备份目录身份：%w", err)
	}
	item := trashItemFromMeta(itemDir, meta, backupDir, currentRootID, time.Now(), true)
	if !item.BelongsToCurrentRoot {
		return errors.New("该项不属于当前备份目录，已拒绝恢复")
	}
	if item.ArchiveMissing {
		return errors.New("归档文件已缺失，无法恢复；只能彻底删除")
	}
	if item.BlockedReason != "" {
		return errors.New(item.BlockedReason)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return fmt.Errorf("无法创建备份目录：%w", err)
	}
	names := append([]string{meta.ArchiveName}, meta.ManifestNames...)
	for _, name := range names {
		source := filepath.Join(itemDir, name)
		if _, err := os.Lstat(source); err != nil {
			return fmt.Errorf("回收站中缺少文件：%s", name)
		}
		reparse, err := pathguard.IsReparsePoint(source)
		if err != nil {
			return fmt.Errorf("无法检查回收站文件：%w", err)
		}
		if reparse {
			return errors.New("回收站中的文件是链接或重解析点，已拒绝恢复")
		}
		target := filepath.Join(backupDir, name)
		if _, err := os.Lstat(target); err == nil {
			return fmt.Errorf("备份目录中已有同名文件：%s；请先处理它再恢复", name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("无法检查目标文件：%w", err)
		}
	}
	var moved [][2]string
	for _, name := range names {
		source := filepath.Join(itemDir, name)
		target := filepath.Join(backupDir, name)
		if err := os.Rename(source, target); err != nil {
			for index := len(moved) - 1; index >= 0; index-- {
				_ = os.Rename(moved[index][1], moved[index][0])
			}
			return fmt.Errorf("恢复失败：%w", err)
		}
		moved = append(moved, [2]string{source, target})
	}
	_ = os.Remove(filepath.Join(itemDir, trashMetaName))
	if err := os.Remove(itemDir); err != nil {
		return fmt.Errorf("归档已恢复到备份目录，但回收站条目清理失败：%w", err)
	}
	return nil
}

func DeleteTrashItem(backupDir, snapshotID string) error {
	if err := validSnapshotID(snapshotID); err != nil {
		return err
	}
	root := filepath.Clean(TrashRoot(backupDir))
	itemDir := filepath.Join(root, snapshotID)
	if !strings.EqualFold(filepath.Dir(itemDir), root) {
		return errors.New("拒绝删除回收站之外的文件")
	}
	if err := checkTrashRoot(root); err != nil {
		return err
	}
	info, err := os.Lstat(itemDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("无法读取回收站条目：%w", err)
	}
	if !info.IsDir() {
		return errors.New("回收站条目不是目录，已拒绝删除")
	}
	reparse, err := pathguard.IsReparsePoint(itemDir)
	if err != nil {
		return fmt.Errorf("无法检查回收站条目：%w", err)
	}
	if reparse {
		return errors.New("回收站条目是链接或重解析点，已拒绝删除")
	}
	if err := os.RemoveAll(itemDir); err != nil {
		return fmt.Errorf("彻底删除失败：%w", err)
	}
	return nil
}

func EmptyTrash(backupDir string) (int, []string, error) {
	root := TrashRoot(backupDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil, nil
		}
		return 0, nil, err
	}
	removed := 0
	var failed []string
	for _, entry := range entries {
		if !entry.IsDir() {
			failed = append(failed, entry.Name()+"：无法识别，未删除")
			continue
		}
		id := entry.Name()
		if err := validSnapshotID(id); err != nil {
			failed = append(failed, id+"：目录名无效，未删除")
			continue
		}
		if _, err := readTrashMeta(filepath.Join(root, id)); err != nil {
			failed = append(failed, fmt.Sprintf("%s：%s，未删除", id, err))
			continue
		}
		if err := DeleteTrashItem(backupDir, id); err != nil {
			failed = append(failed, fmt.Sprintf("%s：%s", id, err))
			continue
		}
		removed++
	}
	return removed, failed, nil
}

// PurgeExpiredTrash removes expired items only when they demonstrably belong to
// the current backup root. Anything else is left alone and reported.
func PurgeExpiredTrash(backupDir string, now time.Time, autoClean bool) (int, int, []string, error) {
	if !autoClean {
		return 0, 0, nil, nil
	}
	list, err := ListTrash(backupDir, autoClean)
	if err != nil {
		return 0, 0, nil, err
	}
	removed, skipped := 0, 0
	var warnings []string
	for _, item := range list.Items {
		if !item.BelongsToCurrentRoot {
			skipped++
			continue
		}
		if item.ExpiresAt == "" {
			continue
		}
		expires, parseErr := time.Parse(time.RFC3339, item.ExpiresAt)
		if parseErr != nil || expires.After(now) {
			continue
		}
		if err := DeleteTrashItem(backupDir, item.SnapshotID); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s：%s", item.SnapshotID, err))
			continue
		}
		removed++
	}
	return removed, skipped, warnings, nil
}

func TrashState(backupDir string, autoClean bool) (TrashList, error) {
	_, _, purgeWarnings, err := PurgeExpiredTrash(backupDir, time.Now(), autoClean)
	list, listErr := ListTrash(backupDir, autoClean)
	if listErr != nil {
		return TrashList{}, listErr
	}
	list.Warnings = append(list.Warnings, purgeWarnings...)
	if err != nil {
		// A purge failure must never hide the list itself.
		list.Warnings = append(list.Warnings, "清理过期回收站内容失败："+err.Error())
	}
	return list, nil
}
