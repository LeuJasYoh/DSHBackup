import type { CompressionLevel, Config, KeyInfo, Snapshot, TaskContext } from "./types";

export function formatBytes(value: number | undefined | null): string {
  if (!value || value <= 0) return "0 B";
  if (value < 1024) return `${value} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let size = value / 1024;
  let index = 0;
  while (size >= 1024 && index < units.length - 1) { size /= 1024; index += 1; }
  return `${size.toFixed(1)} ${units[index]}`;
}
export function formatDate(value?: string): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}
export function formatTime(value?: string): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${pad(date.getHours())}:${pad(date.getMinutes())}`;
}
export function formatRelative(value?: string): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const now = new Date();
  const start = (input: Date) => new Date(input.getFullYear(), input.getMonth(), input.getDate()).getTime();
  const days = Math.round((start(now) - start(date)) / 86_400_000);
  const pad = (part: number) => String(part).padStart(2, "0");
  const time = `${pad(date.getHours())}:${pad(date.getMinutes())}`;
  if (days === 0) return `今天 ${time}`;
  if (days === 1) return `昨天 ${time}`;
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${time}`;
}
export function kindShort(kind: string): string {
  if (kind === "scheduled") return "计划";
  if (kind === "pre-restore") return "恢复前";
  return "手动";
}
export const compressionLevels: CompressionLevel[] = ["store", "fast", "quick", "balanced", "high", "max"];
const compressionNames: Record<CompressionLevel, string> = {
  store: "仅存储",
  fast: "极速",
  quick: "快速",
  balanced: "均衡",
  high: "高压缩",
  max: "极限",
};
export function compressionLabel(level?: string): string {
  if (!level || !(level in compressionNames)) return "未记录";
  return level === "store" ? "仅存储" : `${compressionNames[level as CompressionLevel]}压缩`;
}
export function compressionShort(level?: string): string {
  return level && level in compressionNames ? compressionNames[level as CompressionLevel] : "未记录";
}
export function compressionHint(level?: string): string {
  switch (level) {
    case "store": return "不压缩，速度最快；归档最大，适合已压缩文件或本地 SSD。";
    case "fast": return "7z 最快压缩，速度快，体积略小于仅存储。";
    case "quick": return "快速压缩，速度与体积之间的轻量折中。";
    case "balanced": return "均衡压缩，日常备份的体积与耗时折中。";
    case "high": return "更高压缩率，耗时和内存高于均衡。";
    case "max": return "最高压缩率，耗时和内存最高。";
    default: return "请选择压缩档位。";
  }
}
export function kindLabel(kind: string): string {
  if (kind === "pre-restore") return "恢复前快照";
  if (kind === "scheduled") return "计划备份";
  return "手动备份";
}
export function scopeLabel(snapshot?: Snapshot): string {
  if (!snapshot) return "未知";
  if (snapshot.scope?.length) {
    const hasDsh = snapshot.scope.includes("dsh");
    const hasDesktop = snapshot.scope.includes("desktop-state");
    if (hasDsh && hasDesktop) return "DSH + 桌面";
    if (hasDesktop) return "仅桌面";
    if (hasDsh) return "仅 DSH";
  }
  return snapshot.desktopIncluded ? "DSH + 桌面" : "仅 DSH";
}
export function statusLabel(status: string): string {
  if (status === "ready") return "归档可用";
  if (status === "missing") return "归档缺失";
  if (status === "size-mismatch") return "大小不符";
  if (status === "conflict") return "清单冲突";
  return status || "状态未知";
}
export function archiveBlockReason(snapshot?: Snapshot): string {
  if (!snapshot) return "请选择备份";
  if (snapshot.status === "missing" || !snapshot.localPath) return "归档文件缺失，请找回原归档后刷新列表。";
  if (snapshot.status === "size-mismatch") return "归档大小与清单不符，请找回完整归档；不能恢复此备份。";
  if (snapshot.status !== "ready") return "归档状态异常，请检查文件并刷新列表。";
  return "";
}
export function restoreBlockReason(snapshot?: Snapshot, key?: KeyInfo): string {
  const archiveError = archiveBlockReason(snapshot);
  if (archiveError) return archiveError;
  if (snapshot?.encrypted) {
    if (!key?.exists || key.error) return "缺少可用的本地私钥，请先找回对应的 data\\keys 目录。";
    if (snapshot.keyId && snapshot.keyId !== key.keyId) return "本地私钥与此备份不匹配，请先找回该归档对应的密钥。";
  }
  return "";
}
export function phaseLabel(phase: string): string {
  const labels: Record<string, string> = {
    precheck: "检查环境", stage: "暂存文件", compress: "7z 压缩", encrypt: "加密归档",
    save: "写入本地", verify: "校验归档", decrypt: "解密归档", extract: "解压归档",
    snapshot: "恢复前快照", install: "覆盖恢复", cleanup: "清理临时文件", cancel: "取消任务", done: "等待任务确认",
  };
  return labels[phase] ?? phase;
}
export function weekdayLabel(day: number): string {
  return ["周日", "周一", "周二", "周三", "周四", "周五", "周六"][day] ?? "周一";
}
export function scheduleLabel(kind: string, time: string, weekday: number): string {
  if (kind === "daily") return `每天 ${time}`;
  if (kind === "weekly") return `每${weekdayLabel(weekday)} ${time}`;
  return "手动备份";
}
export function taskTitle(kind: string): string {
  return kind === "restore" ? "正在恢复" : kind === "verify" ? "正在校验" : "正在备份";
}
export function taskSteps(kind: string, context?: TaskContext | null): Array<{ phase: string; label: string }> {
  if (kind === "restore") return [
    { phase: "precheck", label: "检查" },
    ...(context?.encrypted ? [{ phase: "decrypt", label: "解密" }] : []),
    { phase: "extract", label: "解压" },
    ...(context?.preRestoreSnapshot ? [{ phase: "snapshot", label: "保护现有数据" }] : []),
    { phase: "install", label: "写入" }, { phase: "done", label: "收尾" },
  ];
  if (kind === "verify") return [{ phase: "verify", label: "完整性测试" }, { phase: "done", label: "收尾" }];
  return [
    { phase: "precheck", label: "检查" }, { phase: "stage", label: "暂存" }, { phase: "compress", label: "压缩" },
    ...(context?.verifyAfterBackup ? [{ phase: "verify", label: "校验" }] : []),
    { phase: "save", label: "写入" }, { phase: "done", label: "收尾" },
  ];
}
export function truncateMiddle(value: string, max = 68): string {
  if (value.length <= max) return value;
  const half = Math.floor((max - 1) / 2);
  return `${value.slice(0, half)}…${value.slice(value.length - half)}`;
}
export function errorMessage(error: unknown): string {
  if (typeof error === "string") return error;
  if (error instanceof Error) return error.message;
  return String(error ?? "未知错误");
}
export function cloneConfig(config: Config): Config { return JSON.parse(JSON.stringify(config)); }
export function configFingerprint(config: Config, extraArgsText: string): string {
  // lastScheduledAt is runtime state, not an editable preference.
  return JSON.stringify({ ...config, lastScheduledAt: "", extraArgsText });
}
export function isAbsoluteDirectory(path: string): boolean {
  return /^(?:[a-zA-Z]:[\\/]|\\\\[^\\/]+[\\/][^\\/]+)/.test(path.trim()) && !/[<>"|?*\x00-\x1f]/.test(path);
}
export function parseExtraArgs(text: string): string[] { return text.trim().split(/\s+/).filter(Boolean); }
export function extraArgsError(text: string): string {
  const patterns = [
    /^-md=(16|32|64|128|256|512|1024)m$/i,
    /^-mmt=([1-9]|[1-5][0-9]|6[0-4])$/, /^-mfb=([5-9]|[1-9][0-9]|1[0-9]{2}|2[0-6][0-9]|27[0-3])$/,
    /^-ms=(on|off)$/, /^-mqs=(on|off)$/,
  ];
  const args = parseExtraArgs(text);
  if (args.length > 12) return "高级参数最多 12 个";
  const invalid = args.find((arg) => !patterns.some((pattern) => pattern.test(arg)));
  return invalid ? `不允许的 7z 高级参数：${invalid}` : "";
}
export function validateConfig(config: Config, extraArgsText: string): string {
  if (!isAbsoluteDirectory(config.backupDir)) return "备份目录必须是有效的 Windows 绝对路径";
  if (!["system", "dark", "light"].includes(config.theme)) return "请选择有效的界面主题";
  if (!compressionLevels.includes(config.compression.level)) return "请选择有效的压缩档位";
  if (!Number.isInteger(config.retention.local) || config.retention.local < 1 || config.retention.local > 99) return "保留份数必须是 1 到 99 的整数";
  if (!Number.isInteger(config.retention.trashDays) || config.retention.trashDays < 1 || config.retention.trashDays > 365) return "回收站保留天数必须在 1 到 365 之间";
  if (config.schedule.kind !== "manual" && !/^([01]\d|2[0-3]):[0-5]\d$/.test(config.schedule.time)) return "计划时间格式必须为 HH:MM";
  if (!Number.isInteger(config.schedule.weekday) || config.schedule.weekday < 0 || config.schedule.weekday > 6) return "请选择有效的星期";
  if (![0, 16, 32, 64, 128, 256, 512, 1024].includes(config.compression.dictionaryMb)) return "请选择有效的字典大小";
  if (!Number.isInteger(config.compression.threads) || config.compression.threads < 0 || config.compression.threads > 64) return "线程数必须是 0 到 64 的整数";
  return extraArgsError(extraArgsText);
}
