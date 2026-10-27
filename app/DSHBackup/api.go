package main

import (
	"DSHBackup/internal/config"
	"DSHBackup/internal/github"
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
	Version   string   `json:"version"`
	BuildTime string   `json:"buildTime"`
	Platform  string   `json:"platform"`
	Writable  bool     `json:"writable"`
	Portable  bool     `json:"portable"`
	Warnings  []string `json:"warnings"`
	SevenZip  ToolInfo `json:"sevenZip"`
	Sidecar   ToolInfo `json:"sidecar"`
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

type GitHubStatus struct {
	Connected          bool   `json:"connected"`
	Login              string `json:"login"`
	Name               string `json:"name"`
	AvatarURL          string `json:"avatarUrl"`
	Repo               string `json:"repo"`
	ClientID           string `json:"clientId"`
	ClientIDConfigured bool   `json:"clientIdConfigured"`
	TokenPresent       bool   `json:"tokenPresent"`
	AutoUpload         bool   `json:"autoUpload"`
	ConnectedAt        string `json:"connectedAt"`
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

type Bootstrap struct {
	Config         config.Config       `json:"config"`
	App            AppInfo             `json:"app"`
	Paths          PathsInfo           `json:"paths"`
	DSH            DSHStatus           `json:"dsh"`
	LocalSnapshots []manifest.Snapshot `json:"localSnapshots"`
	CloudSnapshots []manifest.Snapshot `json:"cloudSnapshots"`
	CloudError     string              `json:"cloudError,omitempty"`
	Latest         *manifest.Snapshot  `json:"latest,omitempty"`
	Key            sidecar.KeyInfo     `json:"key"`
	KeyError       string              `json:"keyError,omitempty"`
	GitHub         GitHubStatus        `json:"github"`
	Disk           DiskInfo            `json:"disk"`
	Schedule       ScheduleInfo        `json:"schedule"`
	ProxyPassword  bool                `json:"proxyPassword"`
	Warnings       []string            `json:"warnings"`
	LogsPath       string              `json:"logsPath"`
}

type OnboardingInput struct {
	Mode            string `json:"mode"`
	BackupDir       string `json:"backupDir"`
	EncryptionLocal bool   `json:"encryptionLocal"`
	Compression     string `json:"compression"`
	IncludeDesktop  bool   `json:"includeDesktop"`
}

type RestoreRequest struct {
	SnapshotID         string `json:"snapshotId"`
	Location           string `json:"location"`
	IncludeDesktop     bool   `json:"includeDesktop"`
	PreRestoreSnapshot bool   `json:"preRestoreSnapshot"`
	CustomTarget       string `json:"customTarget"`
	Force              bool   `json:"force"`
}

type VerifyRequest struct {
	SnapshotID string `json:"snapshotId"`
	Location   string `json:"location"`
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
	CloudError         string             `json:"cloudError,omitempty"`
	Restore            *RestoreOutcome    `json:"restore,omitempty"`
}

type GitHubAuthEvent struct {
	Status          string          `json:"status"`
	UserCode        string          `json:"userCode,omitempty"`
	VerificationURI string          `json:"verificationUri,omitempty"`
	ExpiresAt       string          `json:"expiresAt,omitempty"`
	Message         string          `json:"message,omitempty"`
	Account         *github.Account `json:"account,omitempty"`
}

type DeviceFlowView struct {
	UserCode                string `json:"userCode"`
	VerificationURI         string `json:"verificationUri"`
	VerificationURIComplete string `json:"verificationUriComplete,omitempty"`
	ExpiresIn               int    `json:"expiresIn"`
	Interval                int    `json:"interval"`
	ExpiresAt               string `json:"expiresAt"`
}

type ProxyTestRequest struct {
	Mode     string `json:"mode"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type ProxyTestResult struct {
	Reachable bool   `json:"reachable"`
	Status    int    `json:"status"`
	LatencyMS int64  `json:"latencyMs"`
	Message   string `json:"message"`
}

type Notice struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	Time    string `json:"time"`
}
