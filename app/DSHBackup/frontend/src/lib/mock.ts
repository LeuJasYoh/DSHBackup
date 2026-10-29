import { cloneConfig, errorMessage, isAbsoluteDirectory, restoreBlockReason, validateConfig } from "./format";
import type { Bootstrap, Config, Notice, OnboardingInput, RestoreRequest, Snapshot, TaskKind, TaskProgress, TaskResult, UpdateInfo, VerifyRequest } from "./types";

type Listener = (payload: unknown) => void;
const listeners = new Map<string, Set<Listener>>();
export const mockEvents = {
  on<T>(name: string, callback: (payload: T) => void): () => void {
    const set = listeners.get(name) ?? new Set<Listener>();
    set.add(callback as Listener);
    listeners.set(name, set);
    return () => { set.delete(callback as Listener); };
  },
  emit<T>(name: string, payload: T) { listeners.get(name)?.forEach((listener) => listener(payload)); },
};
const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value));
const delay = (ms = 80) => new Promise<void>((resolve) => setTimeout(resolve, ms));
const params = new URLSearchParams(typeof window !== "undefined" ? window.location.search : "");
const root = "D:\\DSHBackup";
const baseConfig: Config = {
  schemaVersion: 1, theme: "system", updateCheck: true, onboardingComplete: params.get("onboarding") !== "1", backupDir: `${root}\\backups`,
  includeDesktop: true, encryptionLocal: false,
  compression: { level: "balanced", dictionaryMb: 0, threads: 0, extraArgs: [] },
  schedule: { kind: "manual", time: "03:00", weekday: 1 }, retention: { local: 7 },
  preRestoreSnapshot: true, verifyAfterBackup: true, lastScheduledAt: "",
};
function makeSnapshot(id: string, date: Date, config: Config): Snapshot {
  return {
    id, createdAt: date.toISOString(), kind: "manual", size: 64.3 * 1024 * 1024, sha256: "a".repeat(64),
    encrypted: config.encryptionLocal, keyId: config.encryptionLocal ? "mock-key-id" : undefined,
    desktopIncluded: config.includeDesktop, fileCount: 68214, totalBytes: 103.2 * 1024 * 1024,
    compression: config.compression.level, status: "ready",
    localPath: `${config.backupDir}\\dsh-backup-${id}.7z`, manifestPath: `${config.backupDir}\\dsh-backup-${id}.manifest.json`,
  };
}
const bootstrap: Bootstrap = {
  config: baseConfig,
  app: {
    version: "1.1.0", buildTime: "2026-10-26T19:00:00+08:00", platform: "windows/amd64", writable: true, portable: true, warnings: [],
    sevenZip: { path: "tools\\7za.exe", version: "7-Zip (a) 26.05 (x64)", ok: true },
    sidecar: { path: "dsh-backup-core.exe", version: "1.1.0", ok: true },
  },
  paths: { appDir: root, dataDir: `${root}\\data`, configDir: `${root}\\data\\config`, logsDir: `${root}\\data\\logs`, keysDir: `${root}\\data\\keys`, backupDir: baseConfig.backupDir, defaultBackupDir: baseConfig.backupDir },
  dsh: {
    running: false, evidence: [], checkedAt: new Date().toISOString(),
    sources: [
      { name: "dsh", label: "DSH 核心数据", path: "C:\\Users\\Preview\\.dsh", exists: true, files: 65411, bytes: 100.5 * 1024 * 1024 },
      { name: "desktop-state", label: "桌面登录态", path: "C:\\Users\\Preview\\AppData\\Roaming\\@deepseek-ai\\dsh-desktop", exists: true, files: 2803, bytes: 2.7 * 1024 * 1024 },
    ],
  },
  localSnapshots: params.get("onboarding") === "1" ? [] : [1, 2, 3].map((day) => makeSnapshot(`preview-${day}`, new Date(Date.now() - day * 86400000), baseConfig)),
  key: { exists: false, privatePath: `${root}\\data\\keys\\private.key` },
  disk: { path: baseConfig.backupDir, free: 186 * 1024 ** 3, total: 512 * 1024 ** 3 },
  schedule: { kind: "manual", time: "03:00", weekday: 1, nextRun: "", lastRun: "" }, warnings: [], logsPath: `${root}\\data\\logs`,
};
// Dev tests may replace mockApi methods or mutate this state. No file/credential operations occur here.
export const mockState = { bootstrap, currentTask: null as TaskProgress | null, starting: false };
let timer: ReturnType<typeof setInterval> | null = null;
function refreshDerived() {
  bootstrap.latest = [...bootstrap.localSnapshots].sort((a, b) => b.createdAt.localeCompare(a.createdAt))[0];
  bootstrap.paths.backupDir = bootstrap.config.backupDir;
  bootstrap.disk.path = bootstrap.config.backupDir;
  bootstrap.schedule = { ...bootstrap.schedule, ...bootstrap.config.schedule, lastRun: bootstrap.config.lastScheduledAt };
  if (bootstrap.config.schedule.kind === "manual") bootstrap.schedule.nextRun = "";
  else {
    const next = new Date();
    const [hour, minute] = bootstrap.config.schedule.time.split(":").map(Number);
    next.setHours(hour, minute, 0, 0);
    if (next.getTime() <= Date.now()) next.setDate(next.getDate() + 1);
    if (bootstrap.config.schedule.kind === "weekly") next.setDate(next.getDate() + (bootstrap.config.schedule.weekday - next.getDay() + 7) % 7);
    bootstrap.schedule.nextRun = next.toISOString();
  }
}
function ensureKey() {
  if (bootstrap.config.encryptionLocal && !bootstrap.key.exists) bootstrap.key = { exists: true, keyId: "mock-key-id", privatePath: `${root}\\data\\keys\\private.key` };
}
function ensureIdle() { if (mockState.currentTask || mockState.starting) throw new Error("已有任务正在运行"); }
function checkRunning(force: boolean) { if (bootstrap.dsh.running && !force) throw new Error("DSH_RUNNING:进程 DSH.exe 正在运行"); }
function snapshotFor(id: string) {
  const snapshot = bootstrap.localSnapshots.find((item) => item.id === id);
  const blocked = restoreBlockReason(snapshot, bootstrap.key);
  if (blocked) throw new Error(blocked);
  return snapshot!;
}
interface Job { phases: string[]; finish: () => Omit<TaskResult, "taskId" | "kind"> }
async function startJob(kind: TaskKind, create: () => Job): Promise<string> {
  ensureIdle();
  mockState.starting = true;
  try {
    await delay();
    const job = create();
    const taskId = `${kind}-${Date.now()}`;
    let index = 0;
    const emit = () => {
      const percent = Math.round(index / job.phases.length * 100);
      const progress: TaskProgress = { taskId, kind, phase: job.phases[index], percent, message: "开发预览模拟任务，不读写真实数据", bytesDone: percent, bytesTotal: 100 };
      mockState.currentTask = progress;
      mockEvents.emit("task:progress", progress);
    };
    emit();
    timer = setInterval(() => {
      index += 1;
      if (index < job.phases.length) { emit(); return; }
      if (timer) clearInterval(timer);
      timer = null;
      mockState.currentTask = null;
      try { mockEvents.emit("task:result", { taskId, kind, ...job.finish() }); }
      catch (error) { mockEvents.emit("task:result", { taskId, kind, success: false, message: "模拟任务失败", error: errorMessage(error) }); }
      mockEvents.emit("state:changed", { reason: kind });
    }, 220);
    return taskId;
  } finally { mockState.starting = false; }
}
export const mockApi = {
  async GetBootstrap(): Promise<Bootstrap> { await delay(); refreshDerived(); return clone(bootstrap); },
  async CompleteOnboarding(input: OnboardingInput): Promise<void> {
    ensureIdle();
    if (!isAbsoluteDirectory(input.backupDir)) throw new Error("请选择绝对目录");
    await delay();
    bootstrap.config = { ...bootstrap.config, backupDir: input.backupDir.trim(), includeDesktop: input.includeDesktop, encryptionLocal: input.encryptionLocal, compression: { ...bootstrap.config.compression, level: input.compression }, onboardingComplete: true };
    ensureKey(); refreshDerived(); mockEvents.emit("state:changed", { reason: "onboarding" });
  },
  async SaveConfig(config: Config): Promise<void> {
    ensureIdle();
    const validation = validateConfig(config, config.compression.extraArgs.join(" "));
    if (validation) throw new Error(validation);
    await delay(150);
    bootstrap.config = cloneConfig(config);
    bootstrap.config.backupDir = config.backupDir.trim();
    ensureKey(); refreshDerived(); mockEvents.emit("state:changed", { reason: "settings" });
  },
  async PickDirectory(defaultDir: string): Promise<string> { await delay(); return defaultDir || baseConfig.backupDir; },
  async StartBackup(force: boolean): Promise<string> {
    return startJob("backup", () => {
      checkRunning(force);
      const config = cloneConfig(bootstrap.config);
      return {
        phases: ["precheck", "stage", "compress", ...(config.verifyAfterBackup ? ["verify"] : []), "save"],
        finish: () => {
          const snapshot = makeSnapshot(String(Date.now()), new Date(), config);
          bootstrap.localSnapshots.unshift(snapshot);
          let count = 0;
          bootstrap.localSnapshots = bootstrap.localSnapshots.filter((item) => item.kind === "pre-restore" || ++count <= config.retention.local);
          return { success: true, message: "备份完成（开发模拟）", snapshot };
        },
      };
    });
  },
  async StartRestore(request: RestoreRequest): Promise<string> {
    return startJob("restore", () => {
      checkRunning(request.force);
      const snapshot = snapshotFor(request.snapshotId);
      const input = { ...request };
      if (input.customTarget && (!isAbsoluteDirectory(input.customTarget) || input.includeDesktop)) throw new Error("其他目录必须是绝对路径，且不能恢复系统桌面登录态");
      if (input.includeDesktop && !snapshot.desktopIncluded) throw new Error("归档不包含桌面登录态");
      const config = cloneConfig(bootstrap.config);
      return {
        phases: ["precheck", ...(snapshot.encrypted ? ["decrypt"] : []), "extract", ...(input.preRestoreSnapshot ? ["snapshot"] : []), "install"],
        finish: () => {
          let preRestoreSnapshot: Snapshot | undefined;
          if (input.preRestoreSnapshot) {
            preRestoreSnapshot = { ...makeSnapshot(`pre-${Date.now()}`, new Date(), config), kind: "pre-restore", desktopIncluded: input.includeDesktop };
            bootstrap.localSnapshots.unshift(preRestoreSnapshot);
          }
          return { success: true, message: "恢复完成（开发模拟）", preRestoreSnapshot, restore: { files: snapshot.fileCount, bytes: snapshot.totalBytes, desktopRestored: input.includeDesktop, targetDsh: input.customTarget || bootstrap.dsh.sources[0].path } };
        },
      };
    });
  },
  async StartVerify(request: VerifyRequest): Promise<string> {
    return startJob("verify", () => { snapshotFor(request.snapshotId); return { phases: ["verify", "verify"], finish: () => ({ success: true, message: "校验完成（开发模拟）" }) }; });
  },
  async CancelTask(): Promise<void> {
    if (timer) clearInterval(timer);
    timer = null;
    const task = mockState.currentTask;
    mockState.currentTask = null;
    if (task) mockEvents.emit("task:result", { taskId: task.taskId, kind: task.kind, success: false, message: "任务已取消", error: "任务已取消" });
  },
  async DeleteLocalSnapshot(id: string): Promise<void> {
    ensureIdle(); await delay(); bootstrap.localSnapshots = bootstrap.localSnapshots.filter((item) => item.id !== id);
    refreshDerived(); mockEvents.emit("state:changed", { reason: "snapshot-delete" });
  },
  async CheckDSH() { await delay(); return clone(bootstrap.dsh); },
  async OpenBackupFolder(): Promise<void> { await delay(); },
  async OpenKeysFolder(): Promise<void> { await delay(); },
  async OpenLogsFolder(): Promise<void> { await delay(); },
  async OpenDataFolder(): Promise<void> { await delay(); },
  async OpenLocalSnapshot(id: string): Promise<void> { if (!bootstrap.localSnapshots.some((s) => s.id === id && s.status !== "missing")) throw new Error("归档缺失"); },
  async CopyText(text: string): Promise<void> { await navigator.clipboard.writeText(text); },
  async ExportDiagnostics(): Promise<string> { await delay(); return `${root}\\data\\logs\\diagnostics-preview.zip`; },
  async ReadLogTail(_lines: number): Promise<string> { return '{"level":"info","message":"开发模拟日志，未读写真实文件"}'; },
  async CheckForUpdates(_force: boolean): Promise<UpdateInfo> {
    await delay();
    const currentVersion = bootstrap.app.version;
    if (params.get("update") === "1") {
      return {
        currentVersion, latestVersion: "1.2.0", available: true, checkedAt: new Date().toISOString(),
        url: "https://github.com/LeuJasYoh/DSHBackup/releases/tag/v1.2.0", publishedAt: "2026-10-30T10:00:00Z",
      };
    }
    return { currentVersion, latestVersion: currentVersion, available: false, checkedAt: new Date().toISOString(), url: "https://github.com/LeuJasYoh/DSHBackup/releases" };
  },
  async OpenRepository(): Promise<void> { await delay(); },
  async OpenReleasePage(_url: string): Promise<void> { await delay(); },
};
export function mockEmitNotice(notice: Notice) { mockEvents.emit("app:notice", notice); }
