package manifest

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"DSHBackup/internal/scope"
)

const (
	// SchemaVersion 2 起，一份快照按备份域拆成多个归档，每份归档有自己的清单。
	// 1 是旧的"一快照一归档"格式，仍然可读可恢复。
	SchemaVersion  = 2
	KindManual     = "manual"
	KindScheduled  = "scheduled"
	KindPreRestore = "pre-restore"
	// DomainFull 用于恢复前快照：它按目标目录整体暂存，不按域拆分。
	DomainFull = "full"
)

// legacySchemaVersions 是仍可读取的清单版本。
var legacySchemaVersions = map[int]bool{1: true}

type FileStats struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

type ArchiveInfo struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type EncryptionInfo struct {
	Enabled         bool   `json:"enabled"`
	Algorithm       string `json:"algorithm"`
	KeyID           string `json:"keyId"`
	WrappedPassword string `json:"wrappedPassword"`
}

type CompressorInfo struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Args    []string `json:"args"`
}

type Manifest struct {
	SchemaVersion   int             `json:"schemaVersion"`
	ID              string          `json:"id"`
	Domain          string          `json:"domain,omitempty"`
	CreatedAt       string          `json:"createdAt"`
	Kind            string          `json:"kind"`
	AppVersion      string          `json:"appVersion"`
	Hostname        string          `json:"hostname,omitempty"`
	Scope           []string        `json:"scope"`
	DesktopIncluded bool            `json:"desktopIncluded"`
	Files           FileStats       `json:"files"`
	Archive         ArchiveInfo     `json:"archive"`
	Encryption      *EncryptionInfo `json:"encryption,omitempty"`
	Compressor      CompressorInfo  `json:"compressor"`
	CaptureStatus   string          `json:"captureStatus,omitempty"`
}

// Part 是快照里某个域的归档及其状态。
type Part struct {
	Domain       string `json:"domain"`
	Label        string `json:"label"`
	ArchiveName  string `json:"archiveName"`
	LocalPath    string `json:"localPath"`
	ManifestPath string `json:"manifestPath"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	Files        int64  `json:"files"`
	Bytes        int64  `json:"bytes"`
	Status       string `json:"status"`
}

type Snapshot struct {
	ID              string   `json:"id"`
	CreatedAt       string   `json:"createdAt"`
	Kind            string   `json:"kind"`
	Size            int64    `json:"size"`
	SHA256          string   `json:"sha256"`
	Encrypted       bool     `json:"encrypted"`
	KeyID           string   `json:"keyId,omitempty"`
	DesktopIncluded bool     `json:"desktopIncluded"`
	FileCount       int64    `json:"fileCount"`
	TotalBytes      int64    `json:"totalBytes"`
	Compression     string   `json:"compression,omitempty"`
	CaptureStatus   string   `json:"captureStatus,omitempty"`
	Scope           []string `json:"scope,omitempty"`
	Status          string   `json:"status"`
	Parts           []Part   `json:"parts,omitempty"`
	LocalPath       string   `json:"localPath,omitempty"`
	ManifestPath    string   `json:"manifestPath,omitempty"`
	ManifestAliases []string `json:"manifestAliases,omitempty"`
}

var idPattern = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}$`)

func NewID(now time.Time) string {
	return now.Format("20060102-150405")
}

func ArchiveName(id string) string {
	return "dsh-backup-" + id + ".7z"
}

// DomainArchiveName 是 v2 的命名：域名进文件名，因此淘汰与找回都能按域进行。
func DomainArchiveName(id, domain string) string {
	if domain == "" {
		return ArchiveName(id)
	}
	return "dsh-backup-" + id + "-" + domain + ".7z"
}

// expectedArchiveName 按清单版本还原应有的归档名。
func expectedArchiveName(value Manifest) string {
	if value.SchemaVersion == SchemaVersion {
		return DomainArchiveName(value.ID, value.Domain)
	}
	return ArchiveName(value.ID)
}

// domainRank 固定域的展示与聚合顺序；未知域排在最后。
func domainRank(domain string) int {
	for index, name := range scope.Names() {
		if string(name) == domain {
			return index
		}
	}
	return len(scope.Names())
}

func ManifestPath(archivePath string) string {
	ext := filepath.Ext(archivePath)
	if ext == "" {
		return archivePath + ".manifest.json"
	}
	return strings.TrimSuffix(archivePath, ext) + ".manifest.json"
}

func Write(path string, value Manifest) error {
	if err := Validate(value); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

func Marshal(value Manifest) ([]byte, error) {
	if err := Validate(value); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func Read(path string) (Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var value Manifest
	if err := json.Unmarshal(raw, &value); err != nil {
		return Manifest{}, fmt.Errorf("manifest 格式无效：%w", err)
	}
	if err := Validate(value); err != nil {
		return Manifest{}, err
	}
	return value, nil
}

func Validate(value Manifest) error {
	if value.SchemaVersion != SchemaVersion && !legacySchemaVersions[value.SchemaVersion] {
		return fmt.Errorf("不支持的 manifest 版本：%d", value.SchemaVersion)
	}
	if !idPattern.MatchString(value.ID) {
		return fmt.Errorf("manifest ID 无效：%s", value.ID)
	}
	if value.SchemaVersion == SchemaVersion {
		if !knownDomainName(value.Domain) {
			return fmt.Errorf("未知备份域：%s", value.Domain)
		}
	} else if value.Domain != "" {
		return fmt.Errorf("v%d 清单不应带备份域：%s", value.SchemaVersion, value.Domain)
	}
	if value.Archive.Name != expectedArchiveName(value) {
		return fmt.Errorf("manifest 归档名与 ID 或域不一致")
	}
	if strings.ContainsAny(value.Archive.Name, `\/:`) {
		return errors.New("归档名不能包含路径分隔符")
	}
	if value.Archive.Size < 0 {
		return errors.New("归档大小不能为负数")
	}
	if len(value.Archive.SHA256) != 64 {
		return errors.New("归档 SHA-256 无效")
	}
	if _, err := hex.DecodeString(value.Archive.SHA256); err != nil {
		return errors.New("归档 SHA-256 无效")
	}
	if _, err := time.Parse(time.RFC3339, value.CreatedAt); err != nil {
		return errors.New("备份时间格式无效")
	}
	switch value.Kind {
	case KindManual, KindScheduled, KindPreRestore:
	default:
		return fmt.Errorf("未知备份类型：%s", value.Kind)
	}
	switch value.CaptureStatus {
	case "", "complete", "partial":
	default:
		return fmt.Errorf("未知采集状态：%s", value.CaptureStatus)
	}
	if value.Encryption != nil && value.Encryption.Enabled {
		if value.Encryption.KeyID == "" || value.Encryption.WrappedPassword == "" {
			return errors.New("加密信息不完整")
		}
		if _, err := base64.StdEncoding.DecodeString(value.Encryption.WrappedPassword); err != nil {
			return errors.New("封装的归档密码格式无效")
		}
	}
	return nil
}

// knownDomainName 判断域名是否合法：四个备份域、恢复前快照的 full，
// 以及 v1 清单的空域。
func knownDomainName(domain string) bool {
	if domain == DomainFull {
		return true
	}
	for _, name := range scope.Names() {
		if string(name) == domain {
			return true
		}
	}
	return false
}

// partBundle 是一份已落盘清单及其归档文件状态。
type partBundle struct {
	value        Manifest
	manifestPath string
	archivePath  string
	aliases      []string
	status       string
	size         int64
}

// inspect 按磁盘现状给清单补充状态；归档缺失或大小不符都算异常。
func (p *partBundle) inspect() []string {
	p.status = "ready"
	info, err := os.Stat(p.archivePath)
	if err != nil {
		p.status = "missing"
		p.size = 0
		return []string{fmt.Sprintf("%s 的归档文件不存在", filepath.Base(p.manifestPath))}
	}
	p.size = info.Size()
	if info.Size() != p.value.Archive.Size {
		p.status = "size-mismatch"
	}
	return nil
}

func (p partBundle) part() Part {
	label := "整份归档"
	if p.value.Domain != "" {
		label = scope.Domain(p.value.Domain).Label()
	}
	return Part{
		Domain:       p.value.Domain,
		Label:        label,
		ArchiveName:  p.value.Archive.Name,
		LocalPath:    p.archivePath,
		ManifestPath: p.manifestPath,
		Size:         p.size,
		SHA256:       p.value.Archive.SHA256,
		Files:        p.value.Files.Files,
		Bytes:        p.value.Files.Bytes,
		Status:       p.status,
	}
}

// aggregate 把同一快照 ID 的各份域清单合成一个列表项。字段顺序固定，
// 兼容字段（LocalPath/SHA256 等）指向第一份就绪的清单。
func aggregate(parts []partBundle) (Snapshot, []string) {
	sort.SliceStable(parts, func(left, right int) bool {
		if domainRank(parts[left].value.Domain) != domainRank(parts[right].value.Domain) {
			return domainRank(parts[left].value.Domain) < domainRank(parts[right].value.Domain)
		}
		return parts[left].value.Archive.Name < parts[right].value.Archive.Name
	})
	var warnings []string
	primary := parts[0]
	for index := 1; index < len(parts); index++ {
		if snapshotFieldsConflict(primary.value, parts[index].value) {
			warnings = append(warnings, fmt.Sprintf("%s 与同一快照的其他清单不一致", filepath.Base(parts[index].manifestPath)))
		}
	}
	snapshot := FromManifest(primary.value, primary.archivePath, primary.manifestPath)
	snapshot.ManifestAliases = append([]string{}, primary.aliases...)
	snapshot.Size = 0
	snapshot.FileCount = 0
	snapshot.TotalBytes = 0
	snapshot.Encrypted = false
	snapshot.KeyID = ""
	snapshot.DesktopIncluded = false
	snapshot.Scope = nil
	var listed []Part
	seenScope := map[string]bool{}
	ready := 0
	conflict := false
	for _, part := range parts {
		listed = append(listed, part.part())
		snapshot.Size += part.size
		snapshot.FileCount += part.value.Files.Files
		snapshot.TotalBytes += part.value.Files.Bytes
		if part.value.Encryption != nil && part.value.Encryption.Enabled {
			snapshot.Encrypted = true
			if snapshot.KeyID == "" {
				snapshot.KeyID = part.value.Encryption.KeyID
			}
		}
		snapshot.DesktopIncluded = snapshot.DesktopIncluded || part.value.DesktopIncluded
		for _, name := range part.value.Scope {
			if !seenScope[name] {
				seenScope[name] = true
				snapshot.Scope = append(snapshot.Scope, name)
			}
		}
		switch part.status {
		case "ready":
			ready++
		case "conflict":
			conflict = true
		default:
			warnings = append(warnings, fmt.Sprintf("%s 不可用（%s）", filepath.Base(part.manifestPath), part.status))
		}
	}
	snapshot.Parts = listed
	if len(listed) == 1 && listed[0].Domain == "" {
		snapshot.Parts = nil // v1 单归档不暴露域列表，界面按旧样式展示
	}
	switch {
	case conflict:
		snapshot.Status = "conflict"
	case ready == len(listed):
		snapshot.Status = "ready"
	case ready == 0:
		// 一份都不就绪时，沿用第一份清单的状态，让"归档缺失"和"大小不符"
		// 仍然能区分出来。
		snapshot.Status = listed[0].Status
	default:
		// 部分域可用：仍可恢复就绪的那几份，但列表要标出来。
		snapshot.Status = "partial"
	}
	return snapshot, warnings
}

// snapshotFieldsConflict 比较各份清单必须一致的快照级字段。加密信息按份独立，
// 因此不参与比较。
func snapshotFieldsConflict(left, right Manifest) bool {
	return left.CreatedAt != right.CreatedAt ||
		left.Kind != right.Kind ||
		left.AppVersion != right.AppVersion ||
		left.CaptureStatus != right.CaptureStatus
}

func Scan(dir string) ([]Snapshot, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var warnings []string
	byKey := map[string]*partBundle{}
	bySnapshot := map[string][]*partBundle{}
	var order []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".manifest.json") {
			continue
		}
		manifestPath := filepath.Join(dir, entry.Name())
		value, err := Read(manifestPath)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s：%v", entry.Name(), err))
			continue
		}
		key := value.ID + "\n" + value.Domain
		archivePath := filepath.Join(dir, value.Archive.Name)
		if existing, duplicate := byKey[key]; duplicate {
			if manifestsConflict(existing.value, value) {
				existing.status = "conflict"
				warnings = append(warnings, fmt.Sprintf("%s 与同一归档的另一份清单内容冲突", entry.Name()))
				continue
			}
			preferNew := isStandardManifest(archivePath, manifestPath) && !isStandardManifest(archivePath, existing.manifestPath)
			if preferNew {
				existing.aliases = append(existing.aliases, existing.manifestPath)
				existing.manifestPath = manifestPath
				existing.value = value
			} else {
				existing.aliases = append(existing.aliases, manifestPath)
			}
			continue
		}
		part := &partBundle{value: value, manifestPath: manifestPath, archivePath: archivePath}
		warnings = append(warnings, part.inspect()...)
		byKey[key] = part
		bySnapshot[value.ID] = append(bySnapshot[value.ID], part)
		if len(bySnapshot[value.ID]) == 1 {
			order = append(order, value.ID)
		}
	}
	var snapshots []Snapshot
	for _, id := range order {
		parts := make([]partBundle, 0, len(bySnapshot[id]))
		for _, part := range bySnapshot[id] {
			parts = append(parts, *part)
		}
		snapshot, aggregateWarnings := aggregate(parts)
		warnings = append(warnings, aggregateWarnings...)
		snapshots = append(snapshots, snapshot)
	}
	Sort(snapshots)
	return snapshots, warnings, nil
}

// SnapshotFromManifests 聚合刚写完的各份清单，让备份任务立刻能返回完整快照，
// 不必重扫整个备份目录。
func SnapshotFromManifests(values []Manifest, dir string) (Snapshot, []string) {
	if len(values) == 0 {
		return Snapshot{}, nil
	}
	parts := make([]partBundle, 0, len(values))
	var warnings []string
	for _, value := range values {
		part := partBundle{
			value:        value,
			manifestPath: ManifestPath(filepath.Join(dir, value.Archive.Name)),
			archivePath:  filepath.Join(dir, value.Archive.Name),
		}
		warnings = append(warnings, part.inspect()...)
		parts = append(parts, part)
	}
	return aggregate(parts)
}

func isStandardManifest(archivePath, manifestPath string) bool {
	return filepath.Base(manifestPath) == filepath.Base(ManifestPath(archivePath))
}

func manifestsConflict(left, right Manifest) bool {
	if left.Archive.SHA256 != right.Archive.SHA256 ||
		left.Archive.Size != right.Archive.Size ||
		left.Kind != right.Kind ||
		left.CreatedAt != right.CreatedAt ||
		left.CaptureStatus != right.CaptureStatus {
		return true
	}
	if (left.Encryption == nil) != (right.Encryption == nil) {
		return true
	}
	if left.Encryption != nil && right.Encryption != nil {
		if left.Encryption.Enabled != right.Encryption.Enabled ||
			left.Encryption.KeyID != right.Encryption.KeyID ||
			left.Encryption.WrappedPassword != right.Encryption.WrappedPassword {
			return true
		}
	}
	return false
}

func FromManifest(value Manifest, archivePath, manifestPath string) Snapshot {
	encrypted := value.Encryption != nil && value.Encryption.Enabled
	keyID := ""
	if value.Encryption != nil {
		keyID = value.Encryption.KeyID
	}
	return Snapshot{
		ID:              value.ID,
		CreatedAt:       value.CreatedAt,
		Kind:            value.Kind,
		Size:            value.Archive.Size,
		SHA256:          value.Archive.SHA256,
		Encrypted:       encrypted,
		KeyID:           keyID,
		DesktopIncluded: value.DesktopIncluded,
		FileCount:       value.Files.Files,
		TotalBytes:      value.Files.Bytes,
		Compression:     compressionLevel(value.Compressor.Args),
		CaptureStatus:   value.CaptureStatus,
		Scope:           append([]string{}, value.Scope...),
		Status:          "ready",
		LocalPath:       archivePath,
		ManifestPath:    manifestPath,
	}
}

func Sort(snapshots []Snapshot) {
	sort.SliceStable(snapshots, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339, snapshots[i].CreatedAt)
		right, rightErr := time.Parse(time.RFC3339, snapshots[j].CreatedAt)
		if leftErr == nil && rightErr == nil && !left.Equal(right) {
			return left.After(right)
		}
		if snapshots[i].CreatedAt != snapshots[j].CreatedAt {
			return snapshots[i].CreatedAt > snapshots[j].CreatedAt
		}
		return snapshots[i].ID > snapshots[j].ID
	})
}

// 后出现的 -mx 覆盖前面的，取最后一个才能和 7z 的实际行为一致。
func compressionLevel(args []string) string {
	level := 0
	for _, arg := range args {
		var value int
		if _, err := fmt.Sscanf(strings.ToLower(strings.TrimSpace(arg)), "-mx=%d", &value); err == nil {
			level = value
		}
	}
	switch {
	case level == 0:
		return "store"
	case level == 1:
		return "fast"
	case level >= 2 && level <= 3:
		return "quick"
	case level >= 4 && level <= 6:
		return "balanced"
	case level >= 7 && level <= 8:
		return "high"
	case level >= 9:
		return "max"
	default:
		return ""
	}
}
