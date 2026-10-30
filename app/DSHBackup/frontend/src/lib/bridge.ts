import type { Bootstrap, Config, MoveToTrashRequest, Notice, OnboardingInput, RestoreRequest, TaskProgress, TaskResult, TrashEmptyResult, TrashList, UpdateInfo, VerifyRequest } from "./types";

declare global {
  interface Window {
    go?: { main?: { App?: Record<string, (...args: any[]) => Promise<any>> } };
    runtime?: {
      EventsOn?: (eventName: string, callback: (...data: any[]) => void) => () => void;
      ClipboardSetText?: (text: string) => Promise<boolean>;
    };
  }
}

export const isWailsHost = typeof window !== "undefined" && !!window.runtime;
export const isPreviewMode = import.meta.env.DEV && !isWailsHost;
// Vite eliminates this import in production. Query parameters can never enable mock data.
const preview = import.meta.env.DEV && !isWailsHost ? import("./mock") : null;

async function invoke<T>(method: string, ...args: any[]): Promise<T> {
  if (preview) {
    const { mockApi } = await preview;
    const mock = (mockApi as Record<string, (...params: any[]) => Promise<any>>)[method];
    if (!mock) throw new Error(`预览模式缺少接口：${method}`);
    return mock(...args) as Promise<T>;
  }
  if (!isWailsHost) throw new Error("Wails 宿主运行时未加载，无法访问本地备份功能");
  const app = window.go?.main?.App;
  if (!app) throw new Error("Wails 后端绑定尚未就绪，请重新打开应用");
  const fn = app[method];
  if (!fn) throw new Error(`后端接口不可用：${method}`);
  try {
    return await fn(...args);
  } catch (error) {
    throw new Error(typeof error === "string" ? error : error instanceof Error ? error.message : String(error));
  }
}

function subscribe<T>(eventName: string, callback: (payload: T) => void): () => void {
  if (preview) {
    let disposed = false;
    let off: (() => void) | undefined;
    void preview.then(({ mockEvents }) => {
      if (!disposed) off = mockEvents.on(eventName, callback);
    });
    return () => { disposed = true; off?.(); };
  }
  return window.runtime?.EventsOn?.(eventName, (payload: T) => callback(payload)) ?? (() => {});
}

export const api = {
  getBootstrap: () => invoke<Bootstrap>("GetBootstrap"),
  completeOnboarding: (input: OnboardingInput) => invoke<void>("CompleteOnboarding", input),
  saveConfig: (config: Config) => invoke<void>("SaveConfig", config),
  pickDirectory: (defaultDir: string) => invoke<string>("PickDirectory", defaultDir),
  startBackup: (force: boolean) => invoke<string>("StartBackup", force),
  cancelTask: (taskId: string) => invoke<void>("CancelTask", taskId),
  startRestore: (request: RestoreRequest) => invoke<string>("StartRestore", request),
  startVerify: (request: VerifyRequest) => invoke<string>("StartVerify", request),
  moveToTrash: (request: MoveToTrashRequest) => invoke<void>("MoveToTrash", request),
  listTrash: () => invoke<TrashList>("ListTrash"),
  restoreFromTrash: (snapshotId: string) => invoke<void>("RestoreFromTrash", snapshotId),
  deleteTrashItem: (snapshotId: string) => invoke<void>("DeleteTrashItem", snapshotId),
  emptyTrash: () => invoke<TrashEmptyResult>("EmptyTrash"),
  openTrashFolder: () => invoke<void>("OpenTrashFolder"),
  checkDSH: () => invoke<Bootstrap["dsh"]>("CheckDSH"),
  openBackupFolder: () => invoke<void>("OpenBackupFolder"),
  openKeysFolder: () => invoke<void>("OpenKeysFolder"),
  openLogsFolder: () => invoke<void>("OpenLogsFolder"),
  openDataFolder: () => invoke<void>("OpenDataFolder"),
  openLocalSnapshot: (id: string) => invoke<void>("OpenLocalSnapshot", id),
  copyText: (text: string) => invoke<void>("CopyText", text),
  exportDiagnostics: () => invoke<string>("ExportDiagnostics"),
  readLogTail: (lines: number) => invoke<string>("ReadLogTail", lines),
  checkForUpdates: (force: boolean) => invoke<UpdateInfo>("CheckForUpdates", force),
  openRepository: () => invoke<void>("OpenRepository"),
  openReleasePage: (url: string) => invoke<void>("OpenReleasePage", url),
  onTaskProgress: (callback: (payload: TaskProgress) => void) => subscribe("task:progress", callback),
  onTaskResult: (callback: (payload: TaskResult) => void) => subscribe("task:result", callback),
  onStateChanged: (callback: (payload: { reason?: string }) => void) => subscribe("state:changed", callback),
  onNotice: (callback: (payload: Notice) => void) => subscribe("app:notice", callback),
};

export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    await api.copyText(text);
    return true;
  } catch {
    try { await navigator.clipboard.writeText(text); return true; } catch { return false; }
  }
}
