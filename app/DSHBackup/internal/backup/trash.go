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

// TrashPart 是回收站条目里的一份域归档；整份快照的条目会有多份。
type TrashPart struct {
	Domain         string `json:"domain,omitempty"`
	ArchiveName    string `json:"archiveName"`
	ArchiveSize    int64  `json:"archiveSize"`
	ArchiveSHA256  string `json:"archiveSha256"`
	ManifestName   string `json:"manifestName"`
	ArchiveMissing bool   `json:"archiveMissing,omitempty"`
}

type TrashMeta struct {
	SchemaVersion   int         `json:"schemaVersion"`
	SnapshotID      string      `json:"snapshotId"`
	Domain          string      `json:"domain,omitempty"`
	Parts           []TrashPart `json:"parts,omitempty"`
	Reason          string      `json:"reason"`
	MovedAt         string      `json:"movedAt"`
	ExpiresAt       string      `json:"expiresAt"`
	RetentionDays   int         `json:"retentionDays"`
	ArchiveName     string      `json:"archiveName"`
	ArchiveSize     int64       `json:"archiveSize"`
	ArchiveSHA256   string      `json:"archiveSha256"`
	ArchiveMissing  bool        `json:"archiveMissing,omitempty"`
	ManifestNames   []string    `json:"manifestNames"`
	Encrypted       bool        `json:"encrypted,omitempty"`
	KeyID           string      `json:"keyId,omitempty"`
	Kind            string      `json:"kind"`
	CaptureStatus   string      `json:"captureStatus,omitempty"`
	OriginBackupDir string      `json:"originBackupDir"`
	OriginRootID    string      `json:"originRootIdentity"`
	AppVersion      string      `json:"appVersion,omitempty"`
}

type TrashItem struct {
	SnapshotID           string      `json:"snapshotId"`
	ArchiveName          string      `json:"archiveName"`
	Size                 int64       `json:"size"`
	MovedAt              string      `json:"movedAt"`
	ExpiresAt            string      `json:"expiresAt"`
	DaysLeft             int         `json:"daysLeft"`
	Reason               string      `json:"reason"`
	Encrypted            bool        `json:"encrypted"`
	Kind                 string      `json:"kind"`
	CaptureStatus        string      `json:"captureStatus,omitempty"`
	ArchiveMissing       bool        `json:"archiveMissing"`
	Domain               string      `json:"domain,omitempty"`
	Parts                []TrashPart `json:"parts,omitempty"`
	BelongsToCurrentRoot bool        `json:"belongsToCurrentRoot"`
	Restorable           bool        `json:"restorable"`
	NeverExpires         bool        `json:"neverExpires"`
	BlockedReason        string      `json:"blockedReason,omitempty"`
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

// snapshotParts 返回快照的各份归档；v1 单归档快照合成一份没有域名的整档。
func snapshotParts(snapshot manifest.Snapshot) []manifest.Part {
	if len(snapshot.Parts) > 0 {
		return append([]manifest.Part{}, snapshot.Parts...)
	}
	if strings.TrimSpace(snapshot.LocalPath) == "" {
		return nil
	}
	manifestPath := snapshot.ManifestPath
	if manifestPath == "" {
		manifestPath = manifest.ManifestPath(snapshot.LocalPath)
	}
	names := []string{filepath.Base(manifestPath)}
	for _, alias := range snapshot.ManifestAliases {
		if strings.TrimSpace(alias) != "" {
			names = append(names, filepath.Base(alias))
		}
	}
	return []manifest.Part{{
		ArchiveName:  filepath.Base(snapshot.LocalPath),
		LocalPath:    snapshot.LocalPath,
		ManifestPath: manifestPath,
		Size:         snapshot.Size,
		SHA256:       snapshot.SHA256,
	}}
}

// MoveToTrash 把一份快照的全部归档与清单移进回收站。这是纯改名操作，
// 同卷进行，因此瞬时且可回退。
func MoveToTrash(backupDir string, snapshot manifest.Snapshot, reason string, retentionDays int, appVersion string) (TrashMeta, error) {
	return moveSnapshotToTrash(backupDir, snapshot.ID, "", snapshot, reason, retentionDays, appVersion, snapshotParts(snapshot))
}

// MovePartToTrash 只把某个域的那一份移进回收站，用于按域淘汰：
// 会话天天下新份，插件依赖几周才变一次，两者不该共用一个保留数。
func MovePartToTrash(backupDir string, snapshot manifest.Snapshot, part manifest.Part, reason string, retentionDays int, appVersion string) (TrashMeta, error) {
	if strings.TrimSpace(part.Domain) == "" {
		return TrashMeta{}, errors.New("回收站条目缺少备份域")
	}
	key := snapshot.ID + "-" + part.Domain
	return moveSnapshotToTrash(backupDir, key, part.Domain, snapshot, reason, retentionDays, appVersion, []manifest.Part{part})
}

func moveSnapshotToTrash(backupDir, itemKey, domain string, snapshot manifest.Snapshot, reason string, retentionDays int, appVersion string, parts []manifest.Part) (TrashMeta, error) {
	if err := validSnapshotID(itemKey); err != nil {
		return TrashMeta{}, err
	}
	if len(parts) == 0 {
		return TrashMeta{}, errors.New("快照没有本地文件")
	}
	if reason == "" {
		reason = TrashReasonManual
	}
	if retentionDays <= 0 {
		retentionDays = DefaultTrashDays
	}
	for _, part := range parts {
		if err := validFileName(part.ArchiveName); err != nil {
			return TrashMeta{}, err
		}
		if err := validFileName(filepath.Base(part.ManifestPath)); err != nil {
			return TrashMeta{}, err
		}
		for _, alias := range snapshot.ManifestAliases {
			if strings.TrimSpace(alias) == "" {
				continue
			}
			contained, aliasErr := pathguard.Contains(alias, backupDir)
			if aliasErr != nil {
				return TrashMeta{}, fmt.Errorf("无法检查清单位置：%w", aliasErr)
			}
			if !contained {
				return TrashMeta{}, errors.New("拒绝移动备份目录之外的清单")
			}
		}
	}
	for _, part := range parts {
		contained, partErr := pathguard.Contains(part.LocalPath, backupDir)
		if partErr != nil {
			return TrashMeta{}, fmt.Errorf("无法检查归档位置：%w", partErr)
		}
		if !contained {
			return TrashMeta{}, errors.New("拒绝移动备份目录之外的文件")
		}
	}

	// 先记录每个源文件的状态：缺失的归档也要能进回收站，但标成不可恢复。
	type pending struct {
		part    TrashPart
		sources []string
		names   []string
	}
	// aliasNames 记录别名单清；恢复时要一并移回，避免回收站里留下无名残渣。
	var aliasNames []string

	queue := make([]pending, 0, len(parts))
	trashParts := make([]TrashPart, 0, len(parts))
	for _, part := range parts {
		entry := TrashPart{
			Domain:        part.Domain,
			ArchiveName:   part.ArchiveName,
			ArchiveSize:   part.Size,
			ArchiveSHA256: part.SHA256,
			ManifestName:  filepath.Base(part.ManifestPath),
		}
		item := pending{names: []string{entry.ArchiveName, entry.ManifestName}}
		item.sources = []string{part.LocalPath, part.ManifestPath}
		if info, statErr := os.Lstat(part.LocalPath); statErr != nil {
			if !errors.Is(statErr, os.ErrNotExist) {
				return TrashMeta{}, fmt.Errorf("无法读取归档：%w", statErr)
			}
			entry.ArchiveMissing = true
			entry.ArchiveSize = 0
		} else {
			if info.IsDir() {
				return TrashMeta{}, errors.New("归档位置是目录，已拒绝移动")
			}
			if reparse, reparseErr := pathguard.IsReparsePoint(part.LocalPath); reparseErr == nil && reparse {
				return TrashMeta{}, errors.New("归档是链接或重解析点，已拒绝移动")
			}
			entry.ArchiveSize = info.Size()
		}
		if _, statErr := os.Lstat(part.ManifestPath); statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return TrashMeta{}, fmt.Errorf("无法读取清单：%w", statErr)
		}
		for _, alias := range partManifestAliases(part, snapshot) {
			aliasNames = append(aliasNames, filepath.Base(alias))
		}
		for _, alias := range snapshot.ManifestAliases {
			if strings.TrimSpace(alias) == "" || filepath.Base(alias) == entry.ManifestName {
				continue
			}
			item.names = append(item.names, filepath.Base(alias))
			item.sources = append(item.sources, alias)
		}
		queue = append(queue, item)
		trashParts = append(trashParts, entry)
	}

	root := TrashRoot(backupDir)
	itemDir := filepath.Join(root, itemKey)
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

	moved := make([][2]string, 0, len(queue)*2)
	rollback := func() {
		for index := len(moved) - 1; index >= 0; index-- {
			_ = os.Rename(moved[index][1], moved[index][0])
		}
		_ = os.RemoveAll(itemDir)
	}
	for _, item := range queue {
		for index, source := range item.sources {
			if _, statErr := os.Lstat(source); statErr != nil {
				if errors.Is(statErr, os.ErrNotExist) {
					continue
				}
				rollback()
				return TrashMeta{}, fmt.Errorf("无法读取待移动文件：%w", statErr)
			}
			if reparse, reparseErr := pathguard.IsReparsePoint(source); reparseErr == nil && reparse {
				rollback()
				return TrashMeta{}, errors.New("待移动文件是链接或重解析点，已拒绝移动")
			}
			destination := filepath.Join(itemDir, item.names[index])
			if moveErr := os.Rename(source, destination); moveErr != nil {
				rollback()
				return TrashMeta{}, fmt.Errorf("移入回收站失败：%w", moveErr)
			}
			moved = append(moved, [2]string{source, destination})
		}
	}

	now := time.Now()
	rootID, _ := pathguard.Identity(backupDir)
	meta := TrashMeta{
		SchemaVersion:   trashSchema,
		SnapshotID:      snapshot.ID,
		Domain:          domain,
		Parts:           trashParts,
		Reason:          reason,
		MovedAt:         now.Format(time.RFC3339),
		ExpiresAt:       now.AddDate(0, 0, retentionDays).Format(time.RFC3339),
		RetentionDays:   retentionDays,
		ArchiveName:     trashParts[0].ArchiveName,
		ArchiveSize:     0,
		ArchiveSHA256:   trashParts[0].ArchiveSHA256,
		ArchiveMissing:  len(trashParts) == 1 && trashParts[0].ArchiveMissing,
		ManifestNames:   append(manifestNames(trashParts), aliasNames...),
		Encrypted:       snapshot.Encrypted,
		KeyID:           snapshot.KeyID,
		Kind:            snapshot.Kind,
		CaptureStatus:   snapshot.CaptureStatus,
		OriginBackupDir: backupDir,
		OriginRootID:    rootID,
		AppVersion:      appVersion,
	}
	for _, part := range trashParts {
		meta.ArchiveSize += part.ArchiveSize
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

// partManifestAliases 返回属于该份归档、但不在主清单名里的别名单清。
func partManifestAliases(part manifest.Part, snapshot manifest.Snapshot) []string {
	var aliases []string
	for _, alias := range snapshot.ManifestAliases {
		if strings.TrimSpace(alias) == "" || filepath.Base(alias) == filepath.Base(part.ManifestPath) {
			continue
		}
		aliases = append(aliases, alias)
	}
	return aliases
}

func manifestNames(parts []TrashPart) []string {
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.ManifestName != "" {
			names = append(names, part.ManifestName)
		}
	}
	return names
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
	for _, part := range meta.Parts {
		if err := validFileName(part.ArchiveName); err != nil {
			return TrashMeta{}, err
		}
		if part.ManifestName != "" {
			if err := validFileName(part.ManifestName); err != nil {
				return TrashMeta{}, err
			}
		}
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
	parts := trashPartsOf(meta)
	item.Parts = parts
	item.Domain = meta.Domain
	item.Size = 0
	missing := 0
	for index, part := range parts {
		if info, err := os.Stat(filepath.Join(itemDir, part.ArchiveName)); err == nil {
			parts[index].ArchiveMissing = false
			parts[index].ArchiveSize = info.Size()
			item.Size += info.Size()
		} else {
			parts[index].ArchiveMissing = true
			missing++
		}
	}
	item.Parts = parts
	if missing == len(parts) {
		item.ArchiveMissing = true
	}
	switch {
	case !item.BelongsToCurrentRoot:
		item.BlockedReason = "该备份来自其他备份目录，不会自动清理，也不能恢复到当前目录"
	case item.ArchiveMissing:
		item.BlockedReason = "归档文件已缺失，只能彻底删除"
	case item.partialMissing():
		item.BlockedReason = "该快照缺少部分域的归档，恢复后数据不完整；请重新备份"
	case !sameVolume(meta.OriginRootID, currentRootID):
		item.BlockedReason = "回收站与当前备份目录不在同一磁盘分区，无法瞬时恢复"
	}
	item.Restorable = item.BlockedReason == ""
	return item
}

// partialMissing 标记"部分域还在、部分域已经丢了"的回收站条目。
func (i TrashItem) partialMissing() bool {
	missing := 0
	for _, part := range i.Parts {
		if part.ArchiveMissing {
			missing++
		}
	}
	return missing > 0 && missing < len(i.Parts)
}

// trashPartsOf 兼容只有单份归档字段的旧回收站记录。
func trashPartsOf(meta TrashMeta) []TrashPart {
	if len(meta.Parts) > 0 {
		return append([]TrashPart{}, meta.Parts...)
	}
	return []TrashPart{{
		ArchiveName:    meta.ArchiveName,
		ArchiveSize:    meta.ArchiveSize,
		ArchiveSHA256:  meta.ArchiveSHA256,
		ManifestName:   firstOf(meta.ManifestNames),
		ArchiveMissing: meta.ArchiveMissing,
	}}
}

func firstOf(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
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
	var names []string
	for _, part := range trashPartsOf(meta) {
		names = append(names, part.ArchiveName)
	}
	// 清单可能有多份（别名），一律按记录里的名字整体移回，避免回收站里留残渣。
	for _, name := range meta.ManifestNames {
		if name != "" {
			names = append(names, name)
		}
	}
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
