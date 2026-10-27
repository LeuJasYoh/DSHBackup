import { computed, reactive } from "vue";
import { api, copyToClipboard, isPreviewMode } from "./lib/bridge";
import {
  errorMessage,
  mergeSnapshots,
} from "./lib/format";
import type {
  Bootstrap,
  Config,
  DeviceFlowView,
  GitHubAuthEvent,
  GitHubRepo,
  Notice,
  OnboardingInput,
  Page,
  ProxyTestRequest,
  ProxyTestResult,
  SettingsTab,
  Snapshot,
  TaskProgress,
  TaskResult,
} from "./lib/types";

export interface ConfirmState {
  title: string;
  message: string;
  detail?: string;
  confirmLabel: string;
  cancelLabel?: string;
  danger?: boolean;
  onConfirm: () => void | Promise<void>;
}

export interface ToastState {
  text: string;
  type: "info" | "success" | "warning" | "error";
}

const emptyToast: ToastState = { text: "", type: "info" };

export const store = reactive({
  ready: false,
  loading: false,
  boot: null as Bootstrap | null,
  bootError: "",
  page: "dashboard" as Page,
  settingsTab: "backup" as SettingsTab,
  task: null as TaskProgress | null,
  taskStartedAt: 0,
  taskResult: null as TaskResult | null,
  taskResultDismissed: false,
  restorePreselect: "",
  toast: { ...emptyToast } as ToastState,
  toastTimer: null as ReturnType<typeof setTimeout> | null,
  modals: {
    restore: false,
    github: false,
    help: false,
    confirm: null as ConfirmState | null,
  },
  githubAuth: null as GitHubAuthEvent | null,
  githubFlow: null as DeviceFlowView | null,
  githubBusy: false,
  githubAuthStartedAt: 0,
  saving: false,
  refreshing: false,
  cloudRefreshing: false,
  previewMode: isPreviewMode,
  unsubscribes: [] as Array<() => void>,
  refreshTimer: null as ReturnType<typeof setTimeout> | null,
});

export const snapshots = computed(() => {
  if (!store.boot) return [] as Snapshot[];
  return mergeSnapshots(store.boot.localSnapshots, store.boot.cloudSnapshots);
});

export const latestSnapshot = computed(() => snapshots.value[0]);
export const isBusy = computed(() => store.task !== null);
export const cloudEnabled = computed(() => store.boot?.config.mode !== "local");
export const githubConnected = computed(() => !!store.boot?.github.connected);
export const totalSourceBytes = computed(() => {
  if (!store.boot) return 0;
  return store.boot.dsh.sources.reduce((sum, source) => sum + (source.exists ? source.bytes : 0), 0);
});
export const totalSourceFiles = computed(() => {
  if (!store.boot) return 0;
  return store.boot.dsh.sources.reduce((sum, source) => sum + (source.exists ? source.files : 0), 0);
});
export const localUsage = computed(() => {
  if (!store.boot) return 0;
  return store.boot.localSnapshots.reduce((sum, snapshot) => sum + snapshot.size, 0);
});
export const cloudUsage = computed(() => {
  if (!store.boot) return 0;
  return store.boot.cloudSnapshots.reduce((sum, snapshot) => sum + snapshot.size, 0);
});
export const keyReady = computed(() => !!store.boot?.key.exists);

export const pageMeta = computed(() => {
  switch (store.page) {
    case "history":
      return { title: "备份历史", subtitle: "本地与云端快照" };
    case "settings":
      return { title: "设置", subtitle: "备份方式与云端连接" };
    default:
      return { title: "概览", subtitle: "DSH 核心数据备份" };
  }
});

export function showToast(text: string, type: ToastState["type"] = "info", duration = 3200) {
  if (!text) return;
  if (store.toastTimer) clearTimeout(store.toastTimer);
  store.toast = { text, type };
  store.toastTimer = setTimeout(() => {
    store.toast = { ...emptyToast };
    store.toastTimer = null;
  }, duration);
}

export function dismissToast() {
  if (store.toastTimer) clearTimeout(store.toastTimer);
  store.toastTimer = null;
  store.toast = { ...emptyToast };
}

export async function refresh(quiet = true) {
  if (!quiet) store.refreshing = true;
  try {
    const data = await api.getBootstrap();
    store.boot = normalizeBootstrap(data);
    store.bootError = "";
  } catch (error) {
    const message = errorMessage(error);
    store.bootError = message;
    if (!quiet) showToast(`刷新失败：${message}`, "error");
  } finally {
    if (!quiet) store.refreshing = false;
  }
}

function normalizeBootstrap(data: Bootstrap): Bootstrap {
  return {
    ...data,
    localSnapshots: data.localSnapshots ?? [],
    cloudSnapshots: data.cloudSnapshots ?? [],
    warnings: data.warnings ?? [],
    app: {
      ...data.app,
      warnings: data.app?.warnings ?? [],
    },
    dsh: {
      ...data.dsh,
      evidence: data.dsh?.evidence ?? [],
      sources: data.dsh?.sources ?? [],
    },
  };
}

function scheduleRefresh(delay = 350) {
  if (store.refreshTimer) clearTimeout(store.refreshTimer);
  store.refreshTimer = setTimeout(() => {
    store.refreshTimer = null;
    void refresh(true);
  }, delay);
}

export async function init() {
  if (store.ready) return;
  if (store.unsubscribes.length === 0) {
    store.unsubscribes.push(
    api.onTaskProgress((progress) => {
      if (!store.task || store.task.taskId !== progress.taskId) {
        store.taskStartedAt = Date.now();
      }
      store.task = progress;
      store.taskResultDismissed = false;
    }),
    api.onTaskResult((result) => {
      store.task = null;
      store.taskStartedAt = 0;
      store.taskResult = result;
      store.taskResultDismissed = false;
      if (result.success) {
        if (result.kind === "backup") {
          showToast(result.cloudError ? "本地备份完成，但云端上传失败" : "备份完成", result.warnings?.length || result.cloudError ? "warning" : "success", 4200);
        } else if (result.kind === "restore") {
          showToast("恢复完成，请重新启动 DSH", "success", 5000);
        } else {
          showToast(result.message || "验证通过", "success");
        }
      } else {
        showToast(result.error || result.message || "任务失败", "error", 6000);
      }
      scheduleRefresh(250);
    }),
    api.onGitHubAuth((event) => {
      store.githubAuth = event;
      if (event.status === "success") {
        store.githubBusy = false;
        store.githubFlow = null;
      } else if (event.status === "error" || event.status === "cancelled" || event.status === "disconnected") {
        store.githubBusy = false;
      }
      if (event.status === "success") scheduleRefresh(200);
    }),
    api.onStateChanged(() => scheduleRefresh()),
    api.onNotice((notice: Notice) => {
      showToast(notice.message, notice.level === "info" ? "info" : notice.level, 5000);
      if (notice.level === "info") scheduleRefresh(600);
    }),
  );
  }
  await refresh(true);
  store.ready = true;
  if (store.boot && !store.boot.config.onboardingComplete) {
    store.page = "dashboard";
  }
  applyPreviewRoute();
}

function applyPreviewRoute() {
  if (!store.previewMode) return;
  const params = new URLSearchParams(window.location.search);
  const preview = params.get("preview");
  if (!preview) return;
  const tab = params.get("tab") as SettingsTab | null;
  const settingsTabs: SettingsTab[] = ["backup", "cloud", "network", "security", "general", "about"];
  if (preview === "history") {
    store.page = "history";
  } else if (preview === "settings" || settingsTabs.includes(preview as SettingsTab)) {
    store.settingsTab = (tab && settingsTabs.includes(tab) ? tab : (settingsTabs.includes(preview as SettingsTab) ? (preview as SettingsTab) : "backup"));
    store.page = "settings";
  } else if (preview === "restore") {
    window.setTimeout(() => openRestore(store.boot ? snapshots.value[0]?.id ?? "" : ""), 100);
  } else if (preview === "github") {
    window.setTimeout(() => openGitHubModal(), 100);
  } else if (preview === "progress") {
    window.setTimeout(() => void startBackup(true), 250);
  } else if (preview === "help") {
    store.modals.help = true;
  }
}

export async function retryLoad() {
  await refresh(false);
  if (store.boot) store.ready = true;
}

export function dispose() {
  store.unsubscribes.forEach((unsubscribe) => unsubscribe());
  store.unsubscribes = [];
}

export function go(page: Page) {
  store.page = page;
  const content = document.getElementById("content");
  if (content) content.scrollTop = 0;
}

export function openSettingsTab(tab: SettingsTab) {
  store.settingsTab = tab;
  go("settings");
}

export function closeModals() {
  store.modals.restore = false;
  store.modals.help = false;
  store.modals.confirm = null;
  if (store.modals.github) {
    store.modals.github = false;
    if (store.githubBusy) {
      void api.cancelGitHubAuth().catch(() => {});
      store.githubBusy = false;
    }
    store.githubAuth = null;
    store.githubFlow = null;
  }
}

export function openHelp() {
  store.modals.help = true;
}

export function openRestore(id = "") {
  store.restorePreselect = id;
  store.modals.restore = true;
}

export function openGitHubModal() {
  store.modals.github = true;
  store.githubAuth = null;
  store.githubFlow = null;
}

export function closeGitHubModal() {
  if (store.githubBusy) {
    void api.cancelGitHubAuth().catch(() => {});
  }
  store.modals.github = false;
  store.githubBusy = false;
  store.githubFlow = null;
  store.githubAuth = null;
}

export function askConfirm(state: ConfirmState) {
  store.modals.confirm = state;
}

export function closeConfirm() {
  store.modals.confirm = null;
}

function stripRunning(message: string): string {
  return message.startsWith("DSH_RUNNING:") ? message.slice("DSH_RUNNING:".length) : message;
}

function handleRunning(error: unknown, retry: () => void) {
  const message = errorMessage(error);
  if (message.startsWith("DSH_RUNNING:")) {
    askConfirm({
      title: "DSH 正在运行",
      message: stripRunning(message),
      detail: "运行中的文件可能被占用，备份可能是不一致快照。建议先关闭 DSH；也可以继续。",
      confirmLabel: "仍然继续",
      danger: false,
      onConfirm: retry,
    });
    return true;
  }
  return false;
}

export async function startBackup(force = false) {
  if (isBusy.value) {
    showToast("已有任务正在运行", "warning");
    return;
  }
  try {
    const taskId = await api.startBackup(force);
    store.page = "dashboard";
    store.task = {
      taskId,
      kind: "backup",
      phase: "precheck",
      percent: 0,
      message: "准备备份",
      bytesDone: 0,
      bytesTotal: totalSourceBytes.value,
    };
    store.taskStartedAt = Date.now();
    store.taskResult = null;
    store.taskResultDismissed = false;
  } catch (error) {
    if (handleRunning(error, () => void startBackup(true))) return;
    showToast(errorMessage(error), "error", 6000);
  }
}

export async function startRestore(
  snapshotId: string,
  location: string,
  includeDesktop: boolean,
  preRestoreSnapshot: boolean,
  customTarget: string,
  force = false,
): Promise<boolean> {
  if (isBusy.value) {
    showToast("已有任务正在运行", "warning");
    return false;
  }
  try {
    const taskId = await api.startRestore({
      snapshotId,
      location,
      includeDesktop,
      preRestoreSnapshot,
      customTarget,
      force,
    });
    store.modals.restore = false;
    go("dashboard");
    store.task = {
      taskId,
      kind: "restore",
      phase: "precheck",
      percent: 0,
      message: "准备恢复",
      bytesDone: 0,
      bytesTotal: 0,
    };
    store.taskStartedAt = Date.now();
    store.taskResult = null;
    store.taskResultDismissed = false;
    return true;
  } catch (error) {
    if (handleRunning(error, () => void startRestore(snapshotId, location, includeDesktop, preRestoreSnapshot, customTarget, true))) {
      return false;
    }
    showToast(errorMessage(error), "error", 6000);
    return false;
  }
}

export async function startVerify(snapshot: Snapshot) {
  if (isBusy.value) {
    showToast("已有任务正在运行", "warning");
    return;
  }
  const localUsable = !!snapshot.localPath && snapshot.status === "ready";
  const location = localUsable ? "local" : snapshot.cloudReleaseId ? "cloud" : "local";
  try {
    const taskId = await api.startVerify({ snapshotId: snapshot.id, location });
    go("dashboard");
    store.task = {
      taskId,
      kind: "verify",
      phase: "verify",
      percent: 0,
      message: "准备校验",
      bytesDone: 0,
      bytesTotal: snapshot.size,
    };
    store.taskStartedAt = Date.now();
    store.taskResult = null;
    store.taskResultDismissed = false;
  } catch (error) {
    showToast(errorMessage(error), "error", 6000);
  }
}

export async function cancelTask() {
  try {
    await api.cancelTask();
  } catch (error) {
    showToast(errorMessage(error), "error");
  }
}

export async function saveConfig(config: Config, proxyPassword?: string) {
  store.saving = true;
  try {
    await api.saveConfig(config);
    if (proxyPassword !== undefined) {
      if (proxyPassword === "") {
        await api.clearProxyPassword();
      } else {
        await api.saveProxyPassword(proxyPassword);
      }
    }
    await refresh(true);
    showToast("设置已保存", "success");
  } catch (error) {
    showToast(errorMessage(error), "error", 6000);
    throw error;
  } finally {
    store.saving = false;
  }
}

export async function completeOnboarding(input: OnboardingInput) {
  store.saving = true;
  try {
    await api.completeOnboarding(input);
    await refresh(true);
    showToast("首次设置已完成", "success");
  } catch (error) {
    showToast(errorMessage(error), "error", 6000);
    throw error;
  } finally {
    store.saving = false;
  }
}

export async function checkDSH(notify = true) {
  try {
    const status = await api.checkDSH();
    if (store.boot) store.boot.dsh = status;
    if (notify) {
      showToast(status.running ? "检测到 DSH 正在运行" : "未检测到正在运行的 DSH", status.running ? "warning" : "success");
    }
  } catch (error) {
    showToast(errorMessage(error), "error");
  }
}

export async function refreshCloud() {
  store.cloudRefreshing = true;
  try {
    const list = await api.refreshCloudSnapshots();
    if (store.boot) store.boot.cloudSnapshots = list ?? [];
    await refresh(true);
    showToast(`云端快照已刷新：${list?.length ?? 0} 份`, "success");
  } catch (error) {
    showToast(errorMessage(error), "error", 6000);
  } finally {
    store.cloudRefreshing = false;
  }
}

export function deleteSnapshot(snapshot: Snapshot, where: "local" | "cloud") {
  askConfirm({
    title: where === "local" ? "删除本地快照" : "删除云端快照",
    message:
      where === "local"
        ? `将删除 ${snapshot.id} 的本地归档和 manifest，云端副本不受影响。`
        : `将从 GitHub Releases 删除 ${snapshot.id}，本地副本不受影响。`,
    confirmLabel: "删除",
    danger: true,
    onConfirm: async () => {
      if (where === "local") {
        await api.deleteLocalSnapshot(snapshot.id);
      } else {
        if (!snapshot.cloudReleaseId) throw new Error("该快照没有云端 release");
        await api.deleteCloudSnapshot(snapshot.cloudReleaseId);
      }
      await refresh(true);
      showToast("已删除", "success");
    },
  });
}

export async function openBackupFolder() {
  try {
    await api.openBackupFolder();
  } catch (error) {
    showToast(errorMessage(error), "error");
  }
}

export async function openKeysFolder() {
  try {
    await api.openKeysFolder();
  } catch (error) {
    showToast(errorMessage(error), "error");
  }
}

export async function openLogsFolder() {
  try {
    await api.openLogsFolder();
  } catch (error) {
    showToast(errorMessage(error), "error");
  }
}

export async function openDataFolder() {
  try {
    await api.openDataFolder();
  } catch (error) {
    showToast(errorMessage(error), "error");
  }
}

export async function openLocalSnapshot(id: string) {
  try {
    await api.openLocalSnapshot(id);
  } catch (error) {
    showToast(errorMessage(error), "error");
  }
}

export async function openExternal(url: string) {
  try {
    await api.openExternalURL(url);
  } catch (error) {
    showToast(errorMessage(error), "error");
  }
}

export async function copyText(text: string, label = "已复制") {
  const ok = await copyToClipboard(text);
  showToast(ok ? label : `复制失败：${text}`, ok ? "success" : "error");
}

export async function pickDirectory(defaultDir: string): Promise<string> {
  try {
    return await api.pickDirectory(defaultDir);
  } catch (error) {
    showToast(errorMessage(error), "error");
    return "";
  }
}

export async function testProxy(request: ProxyTestRequest): Promise<ProxyTestResult> {
  try {
    return await api.testProxy(request);
  } catch (error) {
    return { reachable: false, status: 0, latencyMs: 0, message: errorMessage(error) };
  }
}

export async function clearProxyPassword() {
  try {
    await api.clearProxyPassword();
    await refresh(true);
    showToast("代理密码已从凭据管理器清除", "success");
  } catch (error) {
    showToast(errorMessage(error), "error");
  }
}

export async function exportDiagnostics() {
  try {
    const path = await api.exportDiagnostics();
    showToast(`诊断包已导出：${path}`, "success", 6000);
  } catch (error) {
    showToast(errorMessage(error), "error");
  }
}

export async function startDeviceFlow(clientId: string) {
  store.githubBusy = true;
  store.githubAuth = null;
  try {
    const flow = await api.startGitHubDeviceFlow(clientId);
    store.githubFlow = flow;
    store.githubAuthStartedAt = Date.now();
  } catch (error) {
    store.githubBusy = false;
    showToast(errorMessage(error), "error", 7000);
  }
}

export function cancelDeviceFlow() {
  store.githubBusy = false;
  store.githubFlow = null;
  store.githubAuth = null;
  void api.cancelGitHubAuth().catch(() => {});
}

export async function connectPAT(token: string) {
  store.githubBusy = true;
  try {
    await api.connectGitHubPAT(token);
    await refresh(true);
    showToast("GitHub 已连接", "success");
    store.githubBusy = false;
    store.githubAuth = null;
    return true;
  } catch (error) {
    store.githubBusy = false;
    showToast(errorMessage(error), "error", 7000);
    return false;
  }
}

export function disconnectGitHub() {
  askConfirm({
    title: "断开 GitHub 连接",
    message: "将删除 Windows 凭据管理器中的 GitHub token；如果当前是云端模式，会切换为仅本地。",
    confirmLabel: "断开连接",
    danger: true,
    onConfirm: async () => {
      await api.disconnectGitHub();
      await refresh(true);
      showToast("已断开 GitHub 连接", "success");
    },
  });
}

export async function listRepos(query: string): Promise<GitHubRepo[]> {
  try {
    return await api.listGitHubRepos(query);
  } catch (error) {
    showToast(errorMessage(error), "error", 6000);
    return [];
  }
}

export async function createRepo(name: string): Promise<GitHubRepo | null> {
  try {
    const repo = await api.createGitHubRepo(name);
    showToast(`已创建私有仓库 ${repo.fullName}`, "success");
    return repo;
  } catch (error) {
    showToast(errorMessage(error), "error", 6000);
    return null;
  }
}

export async function selectRepo(fullName: string): Promise<boolean> {
  try {
    await api.selectGitHubRepo(fullName);
    await refresh(true);
    showToast("仓库已保存", "success");
    return true;
  } catch (error) {
    showToast(errorMessage(error), "error", 6000);
    return false;
  }
}

export async function openGitHubRepo() {
  try {
    await api.openGitHubRepo();
  } catch (error) {
    showToast(errorMessage(error), "error");
  }
}

export function dismissTaskResult() {
  store.taskResultDismissed = true;
}
