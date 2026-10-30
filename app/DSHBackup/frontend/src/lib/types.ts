export type CompressionLevel = "store" | "fast" | "quick" | "balanced" | "high" | "max";
export type ScheduleKind = "manual" | "daily" | "weekly";
export type Theme = "system" | "light" | "dark";
export type CloseBehavior = "ask" | "exit" | "tray";
export type Page = "dashboard" | "history" | "trash" | "settings";
export type SettingsSection =
  | "backup-dir" | "desktop" | "verify" | "prerestore"
  | "schedule" | "schedule-time" | "schedule-keep" | "trash"
  | "compress" | "compress-advanced"
  | "encryption" | "keys" | "diagnostics"
  | "theme" | "update" | "runtime";
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
export interface RetentionConfig { local: number; trashDays: number }
export interface Config {
  schemaVersion: number;
  theme: Theme;
  updateCheck: boolean;
  onboardingComplete: boolean;
  backupDir: string;
  includeDesktop: boolean;
  encryptionLocal: boolean;
  compression: CompressionConfig;
  schedule: ScheduleConfig;
  retention: RetentionConfig;
  preRestoreSnapshot: boolean;
  verifyAfterBackup: boolean;
  closeBehavior: CloseBehavior;
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
  trayAvailable: boolean;
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
  captureStatus?: string;
  scope?: string[];
  status: string;
  localPath?: string;
  manifestPath?: string;
  manifestAliases?: string[];
}
export interface KeyInfo {
  exists: boolean;
  canEncrypt?: boolean;
  generated?: boolean;
  publicKey?: string;
  keyId?: string;
  privatePath?: string;
  error?: string;
  encryptionError?: string;
}
export interface DiskInfo { path: string; free: number; total: number }
export interface ScheduleInfo {
  kind: string;
  time: string;
  weekday: number;
  nextRun: string;
  lastRun: string;
}
export interface UpdateInfo {
  currentVersion: string;
  latestVersion?: string;
  url?: string;
  publishedAt?: string;
  available: boolean;
  checkedAt: string;
  error?: string;
}
export interface Bootstrap {
  config: Config;
  app: AppInfo;
  paths: PathsInfo;
  dsh: DSHStatus;
  localSnapshots: Snapshot[];
  latest?: Snapshot;
  key: KeyInfo;
  keyError?: string;
  disk: DiskInfo;
  schedule: ScheduleInfo;
  warnings: string[];
  logsPath: string;
  backupRootIdentity?: string;
  trash: TrashList;
}
export interface OnboardingInput {
  backupDir: string;
  encryptionLocal: boolean;
  compression: CompressionLevel;
  includeDesktop: boolean;
}
export interface RestoreRequest {
  snapshotId: string;
  includeDesktop: boolean;
  preRestoreSnapshot: boolean;
  customTarget: string;
  force: boolean;
}
export interface VerifyRequest { snapshotId: string }
export interface MoveToTrashRequest {
  snapshotId: string;
  expectedRootIdentity: string;
  expectedBackupDir: string;
  expectedSha256: string;
}
export interface TrashItem {
  snapshotId: string;
  archiveName: string;
  size: number;
  movedAt: string;
  expiresAt: string;
  daysLeft: number;
  reason: string;
  encrypted: boolean;
  kind: string;
  captureStatus?: string;
  archiveMissing: boolean;
  belongsToCurrentRoot: boolean;
  restorable: boolean;
  neverExpires: boolean;
  blockedReason?: string;
}
export interface TrashList {
  items: TrashItem[];
  count: number;
  bytes: number;
  root: string;
  warnings?: string[];
}
export interface TrashEmptyResult {
  removed: number;
  failed: string[];
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
  restore?: RestoreOutcome;
}
export interface ClosePromptPayload { taskRunning: boolean }
export interface Notice {
  level: "info" | "warning" | "error";
  message: string;
  time: string;
}
// Captured at task launch; progress and results must not follow later settings edits.
export interface TaskContext {
  kind: TaskKind;
  archive?: string;
  target?: string;
  encrypted?: boolean;
  verifyAfterBackup?: boolean;
  preRestoreSnapshot?: boolean;
}
