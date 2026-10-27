import type { Mode, Snapshot } from "./types";

export function formatBytes(value: number | undefined | null): string {
  if (!value || value <= 0) return "0 B";
  if (value < 1024) return `${value} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let size = value / 1024;
  let index = 0;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index += 1;
  }
  return `${size.toFixed(1)} ${units[index]}`;
}

export function formatDate(value?: string): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function formatDateTime(value?: string): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${formatDate(value)}:${pad(date.getSeconds())}`;
}

export function formatRelative(value?: string): string {
  if (!value) return "—";
  const date = new Date(value).getTime();
  if (Number.isNaN(date)) return "—";
  const diff = Date.now() - date;
  if (diff < 0) return formatDate(value);
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return "刚刚";
  if (minutes < 60) return `${minutes} 分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时前`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days} 天前`;
  return formatDate(value);
}

export function compressionLabel(level?: string): string {
  switch (level) {
    case "fast":
      return "快速压缩";
    case "max":
      return "极限压缩";
    case "balanced":
      return "均衡压缩";
    default:
      return "均衡压缩";
  }
}

export function modeLabel(mode: Mode | string): string {
  if (mode === "hybrid") return "本地 + GitHub";
  if (mode === "cloud") return "仅 GitHub";
  return "仅本地";
}

export function modeContext(mode: Mode | string): string {
  if (mode === "hybrid") return "本地 + 云端 · 云端归档始终加密";
  if (mode === "cloud") return "仅云端 · 建议同时保留本地副本";
  return "本地模式 · 不发起网络请求";
}

export function sourceLabel(source: string): string {
  if (source === "both") return "本地 + 云端";
  if (source === "cloud") return "云端";
  return "本地";
}

export function kindLabel(kind: string): string {
  if (kind === "pre-restore") return "恢复前快照";
  if (kind === "scheduled") return "计划备份";
  return "手动备份";
}

export function statusLabel(status: string): string {
  switch (status) {
    case "ready":
      return "成功";
    case "missing":
      return "归档缺失";
    case "size-mismatch":
      return "大小不符";
    case "cloud-only":
      return "仅云端";
    default:
      return status || "未知";
  }
}

export function phaseLabel(phase: string): string {
  const labels: Record<string, string> = {
    precheck: "检查环境",
    stage: "暂存文件",
    compress: "7z 压缩",
    encrypt: "加密归档",
    save: "写入本地",
    upload: "上传云端",
    verify: "校验归档",
    download: "下载归档",
    decrypt: "解密归档",
    extract: "解压归档",
    snapshot: "恢复前快照",
    install: "覆盖恢复",
    cleanup: "清理临时文件",
    cancel: "取消任务",
    done: "完成",
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
  if (kind === "restore") return "正在恢复";
  if (kind === "verify") return "正在验证";
  return "正在备份";
}

export function taskSteps(kind: string, cloud: boolean, keepLocal = true): Array<{ phase: string; label: string }> {
  if (kind === "restore") {
    return [
      { phase: "download", label: "读取归档" },
      { phase: "decrypt", label: "解密" },
      { phase: "extract", label: "解压" },
      { phase: "snapshot", label: "恢复前快照" },
      { phase: "install", label: "写入" },
      { phase: "done", label: "校验" },
    ];
  }
  if (kind === "verify") {
    return [
      { phase: "verify", label: "哈希校验" },
      { phase: "done", label: "7z 测试" },
    ];
  }
  return [
    { phase: "precheck", label: "检查" },
    { phase: "stage", label: "暂存" },
    { phase: "compress", label: "压缩" },
    { phase: "verify", label: "校验" },
    ...(keepLocal ? [{ phase: "save", label: "写入" }] : []),
    ...(cloud ? [{ phase: "upload", label: "上传" }] : []),
    { phase: "done", label: "完成" },
  ];
}

export function mergeSnapshots(local: Snapshot[], cloud: Snapshot[]): Snapshot[] {
  const map = new Map<string, Snapshot>();
  local.forEach((snapshot) => map.set(snapshot.id, { ...snapshot }));
  cloud.forEach((snapshot) => {
    const existing = map.get(snapshot.id);
    if (existing) {
      map.set(snapshot.id, {
        ...existing,
        location: "both",
        source: "both",
        cloudReleaseId: snapshot.cloudReleaseId,
        cloudTag: snapshot.cloudTag,
        cloudName: snapshot.cloudName,
        cloudAssetId: snapshot.cloudAssetId,
        cloudManifestAssetId: snapshot.cloudManifestAssetId,
        cloudHtmlUrl: snapshot.cloudHtmlUrl,
        cloudSize: snapshot.cloudSize,
      });
    } else {
      map.set(snapshot.id, { ...snapshot, location: "cloud", source: "cloud" });
    }
  });
  return Array.from(map.values()).sort((a, b) => b.createdAt.localeCompare(a.createdAt));
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
