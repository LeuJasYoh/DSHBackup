import { computed, reactive } from "vue";
import { api, copyToClipboard, isPreviewMode } from "./lib/bridge";
import { errorMessage, restoreBlockReason } from "./lib/format";
import { applyTheme, normalizeTheme } from "./lib/theme";
import type { Bootstrap, Config, OnboardingInput, Page, RestoreRequest, SettingsTab, Snapshot, TaskContext, TaskProgress, TaskResult, UpdateInfo } from "./lib/types";

export interface ConfirmState {
  title: string;
  message: string;
  detail?: string;
  confirmLabel: string;
  cancelLabel?: string;
  danger?: boolean;
  onConfirm: () => void | Promise<void>;
}
export interface ToastState { text: string; type: "info" | "success" | "warning" | "error" }
const emptyToast: ToastState = { text: "", type: "info" };
export const store = reactive({
  ready: false,
  boot: null as Bootstrap | null,
  bootError: "",
  page: "dashboard" as Page,
  settingsTab: "backup" as SettingsTab,
  settingsDirty: false,
  settingsReminder: "",
  task: null as TaskProgress | null,
  startingTask: false,
  taskStartedAt: 0,
  taskContext: null as TaskContext | null,
  taskResult: null as TaskResult | null,
  taskResultDismissed: false,
  taskError: "",
  canceling: false,
  restorePreselect: "",
  toast: { ...emptyToast },
  toastTimer: null as ReturnType<typeof setTimeout> | null,
  notice: null as { level: "warning" | "error"; message: string; time: string } | null,
  modals: { restore: false, help: false, confirm: null as ConfirmState | null },
  saving: false,
  refreshing: false,
  previewMode: isPreviewMode,
  update: null as UpdateInfo | null,
  updateChecking: false,
  updateDismissed: false,
  unsubscribes: [] as Array<() => void>,
  refreshTimer: null as ReturnType<typeof setTimeout> | null,
});
export const snapshots = computed(() => [...(store.boot?.localSnapshots ?? [])].sort((a, b) => b.createdAt.localeCompare(a.createdAt)));
export const latestSnapshot = computed(() => snapshots.value[0]);
export const isBusy = computed(() => store.startingTask || !!store.task || store.saving);
export const totalSourceBytes = computed(() => store.boot?.dsh.sources.reduce((sum, source) => sum + (source.exists ? source.bytes : 0), 0) ?? 0);
export const totalSourceFiles = computed(() => store.boot?.dsh.sources.reduce((sum, source) => sum + (source.exists ? source.files : 0), 0) ?? 0);
export const localUsage = computed(() => store.boot?.localSnapshots.reduce((sum, snapshot) => sum + snapshot.size, 0) ?? 0);

export function showToast(text: string, type: ToastState["type"] = "info", duration = 3200) {
  if (!text) return;
  if (store.toastTimer) clearTimeout(store.toastTimer);
  store.toast = { text, type };
  store.toastTimer = setTimeout(dismissToast, duration);
}
export function dismissToast() {
  if (store.toastTimer) clearTimeout(store.toastTimer);
  store.toastTimer = null;
  store.toast = { ...emptyToast };
}
export function dismissNotice() { store.notice = null; }
let refreshSequence = 0;
function normalizeBootstrap(data: Bootstrap): Bootstrap {
  return {
    ...data,
    config: { ...data.config, theme: normalizeTheme(data.config.theme), updateCheck: data.config.updateCheck !== false, compression: { ...data.config.compression, extraArgs: data.config.compression.extraArgs ?? [] } },
    localSnapshots: data.localSnapshots ?? [], warnings: data.warnings ?? [],
    app: { ...data.app, warnings: data.app?.warnings ?? [] },
    dsh: { ...data.dsh, evidence: data.dsh?.evidence ?? [], sources: data.dsh?.sources ?? [] },
  };
}
export async function refresh(quiet = true) {
  const sequence = ++refreshSequence;
  if (!quiet) store.refreshing = true;
  try {
    const data = await api.getBootstrap();
    if (sequence === refreshSequence) {
      store.boot = normalizeBootstrap(data);
      store.bootError = "";
      if (!store.settingsDirty) applyTheme(store.boot.config.theme, true);
    }
  } catch (error) {
    if (sequence === refreshSequence) store.bootError = errorMessage(error);
  } finally {
    if (!quiet) store.refreshing = false;
  }
}
function scheduleRefresh(delay = 350) {
  if (store.refreshTimer) clearTimeout(store.refreshTimer);
  store.refreshTimer = setTimeout(() => { store.refreshTimer = null; void refresh(); }, delay);
}

// Buffer events while the start call is pending: Wails can emit progress/result before returning its ID.
type TaskEvent = { progress: TaskProgress } | { result: TaskResult };
let pendingEvents: TaskEvent[] | null = null;
const finishedTaskIds = new Set<string>();
function receiveTaskEvent(event: TaskEvent) {
  if (pendingEvents) { pendingEvents.push(event); return; }
  const payload = "progress" in event ? event.progress : event.result;
  if (finishedTaskIds.has(payload.taskId)) return;
  if (store.task && store.task.taskId !== payload.taskId) return;
  if ("progress" in event) {
    if (!store.task) {
      store.taskStartedAt = Date.now();
      store.taskContext = { kind: payload.kind }; // Scheduled tasks: don't infer options from mutable config.
      store.taskResult = null;
      store.taskError = "";
    }
    store.task = event.progress;
  } else {
    finishedTaskIds.add(payload.taskId);
    if (finishedTaskIds.size > 200) finishedTaskIds.delete(finishedTaskIds.values().next().value!);
    store.task = null;
    store.taskStartedAt = 0;
    store.canceling = false;
    store.taskError = "";
    store.taskResult = event.result;
    store.taskResultDismissed = false;
    showToast(event.result.success ? event.result.message || "任务完成" : event.result.error || event.result.message || "任务失败", event.result.success ? "success" : "error");
    scheduleRefresh(250);
  }
}
export async function init() {
  if (store.ready) return;
  if (!store.unsubscribes.length) store.unsubscribes.push(
    api.onTaskProgress((progress) => receiveTaskEvent({ progress })),
    api.onTaskResult((result) => receiveTaskEvent({ result })),
    api.onStateChanged(() => scheduleRefresh()),
    api.onNotice((notice) => {
      if (notice.level !== "info") store.notice = { ...notice, level: notice.level };
      else { showToast(notice.message); scheduleRefresh(600); }
    }),
  );
  await refresh();
  store.ready = true;
  if (store.boot?.config.updateCheck) void checkForUpdates(false);
  if (import.meta.env.DEV && store.previewMode) {
    const params = new URLSearchParams(window.location.search);
    const route = params.get("preview");
    const tabs: SettingsTab[] = ["backup", "security", "about"];
    if (route === "history") go("history");
    else if (route === "settings" || tabs.includes(route as SettingsTab)) {
      const tab = params.get("tab") ?? route;
      openSettingsTab(tabs.includes(tab as SettingsTab) ? tab as SettingsTab : "backup");
    } else if (route === "restore") openRestore();
    else if (route === "help") openHelp();
  }
}
export async function retryLoad() { await refresh(false); if (store.boot) store.ready = true; }
export function dispose() {
  store.unsubscribes.forEach((off) => off());
  store.unsubscribes = [];
  if (store.refreshTimer) clearTimeout(store.refreshTimer);
  dismissToast();
}
export function go(page: Page) {
  if (store.page === "settings" && page !== "settings" && store.settingsDirty) {
    store.settingsReminder = "设置草稿已保留，尚未生效。返回设置后保存或放弃修改。";
  }
  store.page = page;
  document.getElementById("content")?.scrollTo({ top: 0 });
}
export function openSettingsTab(tab: SettingsTab) { store.settingsTab = tab; go("settings"); }
export function openHelp() { store.modals.help = true; }
export function openRestore(id = "") {
  if (isBusy.value) return;
  store.restorePreselect = id;
  store.modals.restore = true;
}
export function askConfirm(state: ConfirmState) { if (!store.modals.confirm) store.modals.confirm = state; }
export function closeConfirm() { store.modals.confirm = null; }

async function launchTask(context: TaskContext, start: () => Promise<string>, bytesTotal = 0) {
  if (isBusy.value) throw new Error("已有操作正在进行，请等待完成后重试。");
  store.startingTask = true;
  store.taskError = "";
  const startedAt = Date.now();
  pendingEvents = [];
  try {
    const taskId = await start();
    store.taskContext = context;
    store.taskStartedAt = startedAt;
    store.taskResult = null;
    store.taskResultDismissed = false;
    store.task = { taskId, kind: context.kind, phase: "precheck", percent: 0, message: "准备任务", bytesDone: 0, bytesTotal };
    const events = pendingEvents;
    pendingEvents = null;
    for (const event of events) {
      const payload = "progress" in event ? event.progress : event.result;
      if (payload.taskId === taskId) receiveTaskEvent(event);
    }
    go("dashboard");
  } catch (error) {
    const events = pendingEvents ?? [];
    pendingEvents = null;
    for (const event of events) receiveTaskEvent(event);
    throw error;
  } finally {
    pendingEvents = null;
    store.startingTask = false;
  }
}
function rememberStartFailure(kind: TaskContext["kind"], error: unknown) {
  store.taskResult = { taskId: "", kind, success: false, message: "任务未能启动", error: errorMessage(error) };
  store.taskResultDismissed = false;
  go("dashboard");
}
export async function startBackup(force = false) {
  if (isBusy.value) return;
  const config = store.boot?.config;
  try {
    await launchTask({ kind: "backup", target: config?.backupDir, encrypted: config?.encryptionLocal, verifyAfterBackup: config?.verifyAfterBackup }, () => api.startBackup(force), totalSourceBytes.value);
  } catch (error) {
    const message = errorMessage(error);
    if (!force && message.startsWith("DSH_RUNNING:")) {
      askConfirm({
        title: "DSH 正在运行", message: message.slice("DSH_RUNNING:".length),
        detail: "建议先关闭 DSH。继续可能产生不一致的备份。", confirmLabel: "仍然备份",
        onConfirm: async () => {
          const current = store.boot?.config;
          await launchTask({ kind: "backup", target: current?.backupDir, encrypted: current?.encryptionLocal, verifyAfterBackup: current?.verifyAfterBackup }, () => api.startBackup(true), totalSourceBytes.value);
        },
      });
    } else rememberStartFailure("backup", error);
  }
}
export async function startRestore(request: RestoreRequest): Promise<void> {
  const snapshot = snapshots.value.find((item) => item.id === request.snapshotId);
  const blocked = restoreBlockReason(snapshot, store.boot?.key);
  if (blocked) throw new Error(blocked);
  if (request.customTarget && request.includeDesktop) throw new Error("恢复到其他目录时不能恢复系统桌面登录态。");
  await launchTask({ kind: "restore", archive: snapshot?.localPath, target: request.customTarget || store.boot?.dsh.sources.find((s) => s.name === "dsh")?.path, encrypted: snapshot?.encrypted, preRestoreSnapshot: request.preRestoreSnapshot }, () => api.startRestore({ ...request }));
  store.modals.restore = false;
}
export async function startVerify(snapshot: Snapshot) {
  if (isBusy.value) return;
  const blocked = restoreBlockReason(snapshot, store.boot?.key);
  if (blocked) { rememberStartFailure("verify", blocked); return; }
  try {
    await launchTask({ kind: "verify", archive: snapshot.localPath }, () => api.startVerify({ snapshotId: snapshot.id }), snapshot.size);
  } catch (error) { rememberStartFailure("verify", error); }
}
export async function cancelTask() {
  if (!store.task || store.canceling || store.startingTask) return;
  store.canceling = true;
  store.taskError = "";
  try { await api.cancelTask(); }
  catch (error) { store.canceling = false; store.taskError = `取消失败：${errorMessage(error)}。可重试取消，或等待任务结束。`; }
}
async function reloadAfterSave(): Promise<Config> {
  // Invalidate older background reads; only the post-save read establishes the normalized baseline.
  ++refreshSequence;
  try {
    const data = normalizeBootstrap(await api.getBootstrap());
    ++refreshSequence;
    store.boot = data;
    store.bootError = "";
    return data.config;
  } catch (error) {
    throw new Error(`设置已写入，但读取保存结果失败：${errorMessage(error)}。草稿已保留，请刷新后确认。`);
  }
}
export async function saveConfig(config: Config): Promise<Config> {
  if (isBusy.value) throw new Error("操作进行中，请等待完成后保存设置。");
  store.saving = true;
  try {
    await api.saveConfig(config);
    const normalized = await reloadAfterSave();
    applyTheme(normalized.theme, true);
    showToast("设置已保存", "success");
    return normalized;
  } finally { store.saving = false; }
}
export async function completeOnboarding(input: OnboardingInput) {
  if (isBusy.value) throw new Error("操作进行中，请稍后重试。");
  store.saving = true;
  try { await api.completeOnboarding(input); const normalized = await reloadAfterSave(); applyTheme(normalized.theme, true); }
  finally { store.saving = false; }
}
export async function checkDSH(notify = true) {
  try {
    const status = await api.checkDSH();
    if (store.boot) store.boot.dsh = status;
    if (notify) showToast(status.running ? "检测到 DSH 正在运行" : "未检测到正在运行的 DSH", status.running ? "warning" : "success");
  } catch (error) { showToast(errorMessage(error), "error"); }
}
export function deleteSnapshot(snapshot: Snapshot) {
  if (isBusy.value) return;
  askConfirm({
    title: "删除这份备份？", message: `将删除备份 ${snapshot.id} 的归档及清单，不会删除或修改 DSH 原数据。`,
    detail: "删除后无法撤销。归档缺失时，仅删除残留的备份清单。", confirmLabel: "删除备份", danger: true,
    onConfirm: async () => {
      if (isBusy.value) throw new Error("任务进行中，请等待完成后删除。");
      await api.deleteLocalSnapshot(snapshot.id);
      await refresh();
      if (store.bootError) throw new Error(`删除请求已完成，但列表刷新失败：${store.bootError}。请关闭此窗口并刷新历史列表。`);
      showToast("备份已删除，原数据不受影响", "success");
    },
  });
}
async function openFolder(action: () => Promise<void>) {
  try { await action(); } catch (error) { showToast(errorMessage(error), "error"); }
}
export function openBackupFolder() { return openFolder(api.openBackupFolder); }
export function openKeysFolder() { return openFolder(api.openKeysFolder); }
export function openLogsFolder() { return openFolder(api.openLogsFolder); }
export function openDataFolder() { return openFolder(api.openDataFolder); }
export function openLocalSnapshot(id: string) { return openFolder(() => api.openLocalSnapshot(id)); }
export async function copyText(text: string, label = "已复制") {
  showToast(await copyToClipboard(text) ? label : "复制失败，请手动复制。", "info");
}
export function pickDirectory(defaultDir: string): Promise<string> { return api.pickDirectory(defaultDir); }
export function dismissTaskResult() { store.taskResultDismissed = true; }
export async function checkForUpdates(manual = false): Promise<void> {
  if (store.updateChecking) return;
  store.updateChecking = true;
  try {
    const info = await api.checkForUpdates(manual);
    store.update = info;
    if (info.available) store.updateDismissed = false;
    if (manual) {
      if (info.error) showToast(info.error, "error");
      else if (info.available) showToast(`发现新版本 v${info.latestVersion}`, "info");
      else showToast(`当前已是最新版本 v${info.currentVersion}`, "success");
    }
  } catch (error) {
    const message = errorMessage(error);
    store.update = { currentVersion: store.boot?.app.version ?? "", checkedAt: new Date().toISOString(), available: false, error: message };
    if (manual) showToast(message, "error");
  } finally {
    store.updateChecking = false;
  }
}
export function dismissUpdate() { store.updateDismissed = true; }
export async function openRepository(): Promise<void> {
  try { await api.openRepository(); } catch (error) { showToast(errorMessage(error), "error"); }
}
export async function openReleasePage(url = ""): Promise<void> {
  try { await api.openReleasePage(url || store.update?.url || ""); } catch (error) { showToast(errorMessage(error), "error"); }
}
