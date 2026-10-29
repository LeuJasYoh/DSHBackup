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
)

const (
	SchemaVersion  = 1
	KindManual     = "manual"
	KindScheduled  = "scheduled"
	KindPreRestore = "pre-restore"
)

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
}

type Snapshot struct {
	ID              string `json:"id"`
	CreatedAt       string `json:"createdAt"`
	Kind            string `json:"kind"`
	Size            int64  `json:"size"`
	SHA256          string `json:"sha256"`
	Encrypted       bool   `json:"encrypted"`
	KeyID           string `json:"keyId,omitempty"`
	DesktopIncluded bool   `json:"desktopIncluded"`
	FileCount       int64  `json:"fileCount"`
	TotalBytes      int64  `json:"totalBytes"`
	Compression     string `json:"compression,omitempty"`
	Status          string `json:"status"`
	LocalPath       string `json:"localPath,omitempty"`
	ManifestPath    string `json:"manifestPath,omitempty"`
}

var idPattern = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}$`)

func NewID(now time.Time) string {
	return now.Format("20060102-150405")
}

func ArchiveName(id string) string {
	return "dsh-backup-" + id + ".7z"
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
	if value.SchemaVersion != SchemaVersion {
		return fmt.Errorf("不支持的 manifest 版本：%d", value.SchemaVersion)
	}
	if !idPattern.MatchString(value.ID) {
		return fmt.Errorf("manifest ID 无效：%s", value.ID)
	}
	if value.Archive.Name != ArchiveName(value.ID) {
		return fmt.Errorf("manifest 归档名与 ID 不一致")
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

func Scan(dir string) ([]Snapshot, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var snapshots []Snapshot
	var warnings []string
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
		archivePath := filepath.Join(dir, value.Archive.Name)
		info, err := os.Stat(archivePath)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s 的归档文件不存在", entry.Name()))
		}
		snapshot := FromManifest(value, archivePath, manifestPath)
		if err != nil {
			snapshot.Status = "missing"
			snapshot.Size = 0
		} else {
			snapshot.Size = info.Size()
			if info.Size() != value.Archive.Size {
				snapshot.Status = "size-mismatch"
			}
		}
		snapshots = append(snapshots, snapshot)
	}
	Sort(snapshots)
	return snapshots, warnings, nil
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
		Status:          "ready",
		LocalPath:       archivePath,
		ManifestPath:    manifestPath,
	}
}

func Sort(snapshots []Snapshot) {
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].CreatedAt > snapshots[j].CreatedAt
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
	case level >= 1 && level <= 3:
		return "fast"
	case level >= 4 && level <= 7:
		return "balanced"
	case level >= 8:
		return "max"
	default:
		return ""
	}
}
