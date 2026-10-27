export type Mode = "local" | "hybrid" | "cloud";
export type CompressionLevel = "fast" | "balanced" | "max";
export type ScheduleKind = "manual" | "daily" | "weekly";
export type Page = "dashboard" | "history" | "settings";
export type SettingsTab = "backup" | "cloud" | "network" | "security" | "general" | "about";
export type SnapshotSource = "local" | "cloud" | "both";
export type TaskKind = "backup" | "restore" | "verify";

export interface CompressionConfig {
  level: CompressionLevel;
  dictionaryMb: number;
  threads: number;
  extraArgs: string[];
}

export interface ScheduleConfig {
  kind: ScheduleKind;
  time: string;
  weekday: number;
}

export interface RetentionConfig {
  local: number;
  cloud: number;
}

export interface GitHubConfig {
  clientId: string;
  login: string;
  avatarUrl: string;
  owner: string;
  repo: string;
  connectedAt: string;
  autoUpload: boolean;
}

export interface ProxyConfig {
  mode: "system" | "manual" | "off";
  url: string;
  username: string;
  bypassLocal: boolean;
  passwordSet: boolean;
}

export interface Config {
  schemaVersion: number;
  onboardingComplete: boolean;
  mode: Mode;
  backupDir: string;
  includeDesktop: boolean;
  encryptionLocal: boolean;
  compression: CompressionConfig;
  schedule: ScheduleConfig;
  retention: RetentionConfig;
  preRestoreSnapshot: boolean;
  verifyAfterBackup: boolean;
  github: GitHubConfig;
  proxy: ProxyConfig;
  lastScheduledAt: string;
}

export interface ToolInfo {
  path: string;
  version: string;
  sha256?: string;
  ok: boolean;
  error?: string;
}

export interface AppInfo {
  version: string;
  buildTime: string;
  platform: string;
  writable: boolean;
  portable: boolean;
  warnings: string[];
  sevenZip: ToolInfo;
  sidecar: ToolInfo;
}

export interface PathsInfo {
  appDir: string;
  dataDir: string;
  configDir: string;
  logsDir: string;
  keysDir: string;
  backupDir: string;
  defaultBackupDir: string;
}

export interface SourceInfo {
  name: string;
  label: string;
  path: string;
  exists: boolean;
  files: number;
  bytes: number;
  error?: string;
}

export interface DSHStatus {
  running: boolean;
  evidence: string[];
  sources: SourceInfo[];
  checkedAt: string;
}

export interface Snapshot {
  id: string;
  createdAt: string;
  kind: string;
  size: number;
  sha256: string;
  encrypted: boolean;
  keyId?: string;
  desktopIncluded: boolean;
  fileCount: number;
  totalBytes: number;
  compression?: string;
  location: SnapshotSource;
  source: SnapshotSource;
  status: string;
  localPath?: string;
  manifestPath?: string;
  cloudReleaseId?: number;
  cloudTag?: string;
  cloudName?: string;
  cloudAssetId?: number;
  cloudManifestAssetId?: number;
  cloudHtmlUrl?: string;
  cloudSize?: number;
}

export interface KeyInfo {
  exists: boolean;
  generated?: boolean;
  publicKey?: string;
  keyId?: string;
  privatePath?: string;
  error?: string;
}

export interface GitHubStatus {
  connected: boolean;
  login: string;
  name?: string;
  avatarUrl: string;
  repo: string;
  clientId: string;
  clientIdConfigured: boolean;
  tokenPresent: boolean;
  autoUpload: boolean;
  connectedAt: string;
}

export interface DiskInfo {
  path: string;
  free: number;
  total: number;
}

export interface ScheduleInfo {
  kind: string;
  time: string;
  weekday: number;
  nextRun: string;
  lastRun: string;
}

export interface Bootstrap {
  config: Config;
  app: AppInfo;
  paths: PathsInfo;
  dsh: DSHStatus;
  localSnapshots: Snapshot[];
  cloudSnapshots: Snapshot[];
  cloudError?: string;
  latest?: Snapshot;
  key: KeyInfo;
  keyError?: string;
  github: GitHubStatus;
  disk: DiskInfo;
  schedule: ScheduleInfo;
  proxyPassword: boolean;
  warnings: string[];
  logsPath: string;
}

export interface OnboardingInput {
  mode: Mode;
  backupDir: string;
  encryptionLocal: boolean;
  compression: CompressionLevel;
  includeDesktop: boolean;
}

export interface RestoreRequest {
  snapshotId: string;
  location: string;
  includeDesktop: boolean;
  preRestoreSnapshot: boolean;
  customTarget: string;
  force: boolean;
}

export interface VerifyRequest {
  snapshotId: string;
  location: string;
}

export interface TaskProgress {
  taskId: string;
  kind: TaskKind;
  phase: string;
  percent: number;
  message: string;
  bytesDone: number;
  bytesTotal: number;
}

export interface RestoreOutcome {
  files: number;
  bytes: number;
  desktopRestored: boolean;
  targetDsh: string;
}

export interface TaskResult {
  taskId: string;
  kind: TaskKind;
  success: boolean;
  message: string;
  error?: string;
  warnings?: string[];
  snapshot?: Snapshot;
  preRestoreSnapshot?: Snapshot;
  cloudError?: string;
  restore?: RestoreOutcome;
}

export interface GitHubAccount {
  login: string;
  name: string;
  avatarUrl: string;
  htmlUrl: string;
}

export interface GitHubAuthEvent {
  status: "waiting" | "success" | "error" | "cancelled" | "disconnected" | "slow_down";
  userCode?: string;
  verificationUri?: string;
  expiresAt?: string;
  message?: string;
  account?: GitHubAccount;
}

export interface DeviceFlowView {
  userCode: string;
  verificationUri: string;
  verificationUriComplete?: string;
  expiresIn: number;
  interval: number;
  expiresAt: string;
}

export interface GitHubRepo {
  id: number;
  name: string;
  fullName: string;
  private: boolean;
  defaultBranch: string;
  htmlUrl: string;
  owner: string;
  canPush: boolean;
  updatedAt: string;
}

export interface ProxyTestRequest {
  mode: string;
  url: string;
  username: string;
  password: string;
}

export interface ProxyTestResult {
  reachable: boolean;
  status: number;
  latencyMs: number;
  message: string;
}

export interface Notice {
  level: "info" | "warning" | "error";
  message: string;
  time: string;
}
