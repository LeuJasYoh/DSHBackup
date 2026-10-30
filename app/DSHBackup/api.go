package main

import (
	"DSHBackup/internal/backup"
	"DSHBackup/internal/config"
	"DSHBackup/internal/manifest"
	"DSHBackup/internal/sidecar"
)

type ToolInfo struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	SHA256  string `json:"sha256,omitempty"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

type PathsInfo struct {
	AppDir           string `json:"appDir"`
	DataDir          string `json:"dataDir"`
	ConfigDir        string `json:"configDir"`
	LogsDir          string `json:"logsDir"`
	KeysDir          string `json:"keysDir"`
	BackupDir        string `json:"backupDir"`
	DefaultBackupDir string `json:"defaultBackupDir"`
}

type AppInfo struct {
	Version       string   `json:"version"`
	BuildTime     string   `json:"buildTime"`
	Platform      string   `json:"platform"`
	Writable      bool     `json:"writable"`
	Portable      bool     `json:"portable"`
	TrayAvailable bool     `json:"trayAvailable"`
	Warnings      []string `json:"warnings"`
	SevenZip      ToolInfo `json:"sevenZip"`
	Sidecar       ToolInfo `json:"sidecar"`
}

type SourceInfo struct {
	Name   string `json:"name"`
	Label  string `json:"label"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Files  int64  `json:"files"`
	Bytes  int64  `json:"bytes"`
	Error  string `json:"error,omitempty"`
}

type DSHStatus struct {
	Running   bool         `json:"running"`
	Evidence  []string     `json:"evidence"`
	Sources   []SourceInfo `json:"sources"`
	CheckedAt string       `json:"checkedAt"`
}

type DiskInfo struct {
	Path  string `json:"path"`
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
}

type ScheduleInfo struct {
	Kind    string `json:"kind"`
	Time    string `json:"time"`
	Weekday int    `json:"weekday"`
	NextRun string `json:"nextRun"`
	LastRun string `json:"lastRun"`
}

type UpdateInfo struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion,omitempty"`
	URL            string `json:"url,omitempty"`
	PublishedAt    string `json:"publishedAt,omitempty"`
	Available      bool   `json:"available"`
	CheckedAt      string `json:"checkedAt"`
	Error          string `json:"error,omitempty"`
}

type Bootstrap struct {
	Config             config.Config       `json:"config"`
	App                AppInfo             `json:"app"`
	Paths              PathsInfo           `json:"paths"`
	DSH                DSHStatus           `json:"dsh"`
	LocalSnapshots     []manifest.Snapshot `json:"localSnapshots"`
	Latest             *manifest.Snapshot  `json:"latest,omitempty"`
	Key                sidecar.KeyInfo     `json:"key"`
	KeyError           string              `json:"keyError,omitempty"`
	Disk               DiskInfo            `json:"disk"`
	Schedule           ScheduleInfo        `json:"schedule"`
	Warnings           []string            `json:"warnings"`
	LogsPath           string              `json:"logsPath"`
	BackupRootIdentity string              `json:"backupRootIdentity"`
	Trash              backup.TrashList    `json:"trash"`
}

type MoveToTrashRequest struct {
	SnapshotID           string `json:"snapshotId"`
	ExpectedRootIdentity string `json:"expectedRootIdentity"`
	ExpectedBackupDir    string `json:"expectedBackupDir"`
	ExpectedSHA256       string `json:"expectedSha256"`
}

type OnboardingInput struct {
	BackupDir       string `json:"backupDir"`
	EncryptionLocal bool   `json:"encryptionLocal"`
	Compression     string `json:"compression"`
	IncludeDesktop  bool   `json:"includeDesktop"`
}

type RestoreRequest struct {
	SnapshotID         string `json:"snapshotId"`
	IncludeDesktop     bool   `json:"includeDesktop"`
	PreRestoreSnapshot bool   `json:"preRestoreSnapshot"`
	CustomTarget       string `json:"customTarget"`
	Force              bool   `json:"force"`
}

type VerifyRequest struct {
	SnapshotID string `json:"snapshotId"`
}

type TaskProgress struct {
	TaskID     string  `json:"taskId"`
	Kind       string  `json:"kind"`
	Phase      string  `json:"phase"`
	Percent    float64 `json:"percent"`
	Message    string  `json:"message"`
	BytesDone  int64   `json:"bytesDone"`
	BytesTotal int64   `json:"bytesTotal"`
}

type RestoreOutcome struct {
	Files           int64  `json:"files"`
	Bytes           int64  `json:"bytes"`
	DesktopRestored bool   `json:"desktopRestored"`
	TargetDSH       string `json:"targetDsh"`
}

type TaskResult struct {
	TaskID             string             `json:"taskId"`
	Kind               string             `json:"kind"`
	Success            bool               `json:"success"`
	Message            string             `json:"message"`
	Error              string             `json:"error,omitempty"`
	Warnings           []string           `json:"warnings,omitempty"`
	Snapshot           *manifest.Snapshot `json:"snapshot,omitempty"`
	PreRestoreSnapshot *manifest.Snapshot `json:"preRestoreSnapshot,omitempty"`
	Restore            *RestoreOutcome    `json:"restore,omitempty"`
}

type Notice struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	Time    string `json:"time"`
}
