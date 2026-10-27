import type {
  Bootstrap,
  Config,
  DeviceFlowView,
  GitHubAuthEvent,
  GitHubRepo,
  GitHubStatus,
  Notice,
  OnboardingInput,
  ProxyTestRequest,
  ProxyTestResult,
  RestoreRequest,
  TaskProgress,
  TaskResult,
  VerifyRequest,
} from "./types";
import { mockApi, mockEvents } from "./mock";

declare global {
  interface Window {
    go?: {
      main?: {
        App?: Record<string, (...args: any[]) => Promise<any>>;
      };
    };
    runtime?: {
      EventsOn?: (eventName: string, callback: (...data: any[]) => void) => () => void;
      EventsOff?: (eventName: string) => void;
      ClipboardSetText?: (text: string) => Promise<boolean>;
    };
  }
}

export const isWailsHost = typeof window !== "undefined" && !!window.runtime;
const mockAllowed = typeof window !== "undefined" && (import.meta.env.DEV || new URLSearchParams(window.location.search).has("mock"));
export const isPreviewMode = !isWailsHost && mockAllowed;

function invoke<T>(method: string, ...args: any[]): Promise<T> {
  if (isPreviewMode) {
    const mock = (mockApi as Record<string, (...params: any[]) => Promise<any>>)[method];
    if (!mock) return Promise.reject(new Error(`预览模式缺少接口：${method}`));
    return mock(...args) as Promise<T>;
  }
  if (!isWailsHost) return Promise.reject(new Error("Wails 宿主运行时未加载，无法访问本地备份功能"));
  const app = window.go?.main?.App;
  if (!app) return Promise.reject(new Error("Wails 后端绑定尚未就绪，请重新打开应用"));
  const fn = app[method];
  if (!fn) return Promise.reject(new Error(`后端接口不可用：${method}`));
  return fn(...args).catch((error: unknown) => {
    const message = typeof error === "string" ? error : error instanceof Error ? error.message : String(error);
    throw new Error(message);
  });
}

function subscribe<T>(eventName: string, callback: (payload: T) => void): () => void {
  if (isPreviewMode) return mockEvents.on(eventName, callback as (payload: unknown) => void);
  if (!window.runtime?.EventsOn) return () => {};
  return window.runtime.EventsOn(eventName, (payload: T) => callback(payload));
}

export const api = {
  getBootstrap: () => invoke<Bootstrap>("GetBootstrap"),
  completeOnboarding: (input: OnboardingInput) => invoke<void>("CompleteOnboarding", input),
  saveConfig: (config: Config) => invoke<void>("SaveConfig", config),
  pickDirectory: (defaultDir: string) => invoke<string>("PickDirectory", defaultDir),
  startBackup: (force: boolean) => invoke<string>("StartBackup", force),
  cancelTask: () => invoke<void>("CancelTask"),
  startRestore: (request: RestoreRequest) => invoke<string>("StartRestore", request),
  startVerify: (request: VerifyRequest) => invoke<string>("StartVerify", request),
  deleteLocalSnapshot: (id: string) => invoke<void>("DeleteLocalSnapshot", id),
  deleteCloudSnapshot: (releaseId: number) => invoke<void>("DeleteCloudSnapshot", releaseId),
  refreshCloudSnapshots: () => invoke<Bootstrap["cloudSnapshots"]>("RefreshCloudSnapshots"),
  checkDSH: () => invoke<Bootstrap["dsh"]>("CheckDSH"),
  saveProxyPassword: (password: string) => invoke<void>("SaveProxyPassword", password),
  clearProxyPassword: () => invoke<void>("ClearProxyPassword"),
  testProxy: (request: ProxyTestRequest) => invoke<ProxyTestResult>("TestProxy", request),
  openBackupFolder: () => invoke<void>("OpenBackupFolder"),
  openKeysFolder: () => invoke<void>("OpenKeysFolder"),
  openLogsFolder: () => invoke<void>("OpenLogsFolder"),
  openDataFolder: () => invoke<void>("OpenDataFolder"),
  openLocalSnapshot: (id: string) => invoke<void>("OpenLocalSnapshot", id),
  openExternalURL: (url: string) => invoke<void>("OpenExternalURL", url),
  copyText: (text: string) => invoke<void>("CopyText", text),
  openGitHubRepo: () => invoke<void>("OpenGitHubRepo"),
  exportDiagnostics: () => invoke<string>("ExportDiagnostics"),
  readLogTail: (lines: number) => invoke<string>("ReadLogTail", lines),
  startGitHubDeviceFlow: (clientId: string) => invoke<DeviceFlowView>("StartGitHubDeviceFlow", clientId),
  connectGitHubPAT: (token: string) => invoke<GitHubStatus>("ConnectGitHubPAT", token),
  disconnectGitHub: () => invoke<void>("DisconnectGitHub"),
  listGitHubRepos: (query: string) => invoke<GitHubRepo[]>("ListGitHubRepos", query),
  createGitHubRepo: (name: string) => invoke<GitHubRepo>("CreateGitHubRepo", name),
  selectGitHubRepo: (fullName: string) => invoke<GitHubRepo>("SelectGitHubRepo", fullName),
  cancelGitHubAuth: () => invoke<void>("CancelGitHubAuth"),
  onTaskProgress: (callback: (payload: TaskProgress) => void) => subscribe<TaskProgress>("task:progress", callback),
  onTaskResult: (callback: (payload: TaskResult) => void) => subscribe<TaskResult>("task:result", callback),
  onGitHubAuth: (callback: (payload: GitHubAuthEvent) => void) => subscribe<GitHubAuthEvent>("github:auth", callback),
  onStateChanged: (callback: (payload: { reason?: string }) => void) => subscribe<{ reason?: string }>("state:changed", callback),
  onNotice: (callback: (payload: Notice) => void) => subscribe<Notice>("app:notice", callback),
};

export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    await api.copyText(text);
    return true;
  } catch {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      return false;
    }
  }
}
