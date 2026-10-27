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
  Snapshot,
  TaskProgress,
  TaskResult,
  VerifyRequest,
} from "./types";

type Listener = (payload: unknown) => void;
const listeners = new Map<string, Set<Listener>>();

export const mockEvents = {
  on<T>(name: string, callback: (payload: T) => void): () => void {
    const set = listeners.get(name) ?? new Set<Listener>();
    set.add(callback as Listener);
    listeners.set(name, set);
    return () => set.delete(callback as Listener);
  },
  emit<T>(name: string, payload: T) {
    listeners.get(name)?.forEach((listener) => listener(payload));
  },
};

const params = new URLSearchParams(typeof window !== "undefined" ? window.location.search : "");
const connectedByDefault = params.get("github") === "1";

const iso = (date: Date) => date.toISOString();
const now = new Date();

function makeSnapshot(id: string, createdAt: string, size: number, encrypted: boolean): Snapshot {
  return {
    id,
    createdAt,
    kind: "manual",
    size,
    sha256: "a".repeat(64),
    encrypted,
    desktopIncluded: true,
    fileCount: 68214,
    totalBytes: 103.2 * 1024 * 1024,
    compression: "balanced",
    location: "local",
    source: "local",
    status: "ready",
    localPath: `D:\\DSHBackup\\backups\\dsh-backup-${id}.7z`,
    manifestPath: `D:\\DSHBackup\\backups\\dsh-backup-${id}.manifest.json`,
  };
}

const initialSnapshots: Snapshot[] = [
  makeSnapshot("20261026-184200", iso(new Date(now.getTime() - 2 * 86400000)), 64.3 * 1024 * 1024, false),
  makeSnapshot("20261025-161500", iso(new Date(now.getTime() - 3 * 86400000)), 63.8 * 1024 * 1024, false),
  makeSnapshot("20261024-210700", iso(new Date(now.getTime() - 4 * 86400000)), 62.9 * 1024 * 1024, false),
];

const baseConfig: Config = {
  schemaVersion: 1,
  onboardingComplete: params.get("onboarding") !== "1",
  mode: "local",
  backupDir: "D:\\DSHBackup\\backups",
  includeDesktop: true,
  encryptionLocal: false,
  compression: { level: "balanced", dictionaryMb: 0, threads: 0, extraArgs: [] },
  schedule: { kind: "manual", time: "03:00", weekday: 1 },
  retention: { local: 7, cloud: 7 },
  preRestoreSnapshot: true,
  verifyAfterBackup: true,
  github: {
    clientId: "",
    login: connectedByDefault ? "preview-user" : "",
    avatarUrl: "",
    owner: connectedByDefault ? "preview-user" : "",
    repo: connectedByDefault ? "dsh-backups" : "",
    connectedAt: connectedByDefault ? iso(new Date()) : "",
    autoUpload: true,
  },
  proxy: { mode: "system", url: "http://127.0.0.1:7897", username: "", bypassLocal: true, passwordSet: false },
  lastScheduledAt: "",
};

const initialBootstrap: Bootstrap = {
  config: baseConfig,
  app: {
    version: "1.0.0",
    buildTime: "2026-10-26T19:00:00+08:00",
    platform: "windows/amd64",
    writable: true,
    portable: true,
    warnings: [],
    sevenZip: { path: "tools\\7za.exe", version: "7-Zip (a) 26.05 (x64)", sha256: "7ddf0a3bc451e166679296be630d6f0573eb386159c04184761c43020c484291", ok: true },
    sidecar: { path: "dsh-backup-core.exe", version: "1.0.0", ok: true },
  },
  paths: {
    appDir: "D:\\DSHBackup",
    dataDir: "D:\\DSHBackup\\data",
    configDir: "D:\\DSHBackup\\data\\config",
    logsDir: "D:\\DSHBackup\\data\\logs",
    keysDir: "D:\\DSHBackup\\data\\keys",
    backupDir: "D:\\DSHBackup\\backups",
    defaultBackupDir: "D:\\DSHBackup\\backups",
  },
  dsh: {
    running: false,
    evidence: [],
    checkedAt: iso(new Date()),
    sources: [
      { name: "dsh", label: "DSH 核心数据", path: "C:\\Users\\23500\\.dsh", exists: true, files: 65411, bytes: 100.5 * 1024 * 1024 },
      { name: "desktop-state", label: "桌面登录态", path: "C:\\Users\\23500\\AppData\\Roaming\\@deepseek-ai\\dsh-desktop", exists: true, files: 2803, bytes: 2.7 * 1024 * 1024 },
    ],
  },
  localSnapshots: initialSnapshots,
  cloudSnapshots: [],
  latest: initialSnapshots[0],
  key: { exists: false, keyId: "", publicKey: "", privatePath: "D:\\DSHBackup\\data\\keys\\private.key" },
  github: {
    connected: connectedByDefault,
    login: connectedByDefault ? "preview-user" : "",
    avatarUrl: "",
    repo: connectedByDefault ? "preview-user/dsh-backups" : "",
    clientId: "",
    clientIdConfigured: false,
    tokenPresent: connectedByDefault,
    autoUpload: true,
    connectedAt: connectedByDefault ? iso(new Date()) : "",
  },
  disk: { path: "D:\\DSHBackup\\backups", free: 186 * 1024 * 1024 * 1024, total: 512 * 1024 * 1024 * 1024 },
  schedule: { kind: "manual", time: "03:00", weekday: 1, nextRun: "", lastRun: "" },
  proxyPassword: false,
  warnings: [],
  logsPath: "D:\\DSHBackup\\data\\logs",
};

const state = {
  bootstrap: initialBootstrap,
  backupTimer: null as ReturnType<typeof setInterval> | null,
  currentTask: null as TaskProgress | null,
  githubTimer: null as ReturnType<typeof setTimeout> | null,
  repos: [
    { id: 1, name: "dsh-backups", fullName: "preview-user/dsh-backups", private: true, defaultBranch: "main", htmlUrl: "https://github.com/preview-user/dsh-backups", owner: "preview-user", canPush: true, updatedAt: iso(new Date()) },
    { id: 2, name: "lab-backups", fullName: "preview-user/lab-backups", private: true, defaultBranch: "main", htmlUrl: "https://github.com/preview-user/lab-backups", owner: "preview-user", canPush: true, updatedAt: iso(new Date(Date.now() - 86400000)) },
  ] as GitHubRepo[],
};

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function delay(ms = 120) {
  return new Promise<void>((resolve) => setTimeout(resolve, ms));
}

function emitTask(progress: TaskProgress) {
  state.currentTask = progress;
  mockEvents.emit("task:progress", progress);
}

function emitResult(result: TaskResult) {
  state.currentTask = null;
  mockEvents.emit("task:result", result);
}

function taskId(prefix: string) {
  return `${prefix}-${Date.now()}`;
}

function currentSnapshots(): Snapshot[] {
  const local = state.bootstrap.localSnapshots;
  const cloud = state.bootstrap.cloudSnapshots;
  const map = new Map<string, Snapshot>();
  local.forEach((snapshot) => map.set(snapshot.id, clone(snapshot)));
  cloud.forEach((snapshot) => {
    const existing = map.get(snapshot.id);
    if (existing) {
      existing.location = "both";
      existing.source = "both";
      Object.assign(existing, {
        cloudReleaseId: snapshot.cloudReleaseId,
        cloudTag: snapshot.cloudTag,
        cloudName: snapshot.cloudName,
        cloudAssetId: snapshot.cloudAssetId,
        cloudManifestAssetId: snapshot.cloudManifestAssetId,
        cloudHtmlUrl: snapshot.cloudHtmlUrl,
        cloudSize: snapshot.cloudSize,
      });
    } else {
      map.set(snapshot.id, clone(snapshot));
    }
  });
  return Array.from(map.values()).sort((a, b) => b.createdAt.localeCompare(a.createdAt));
}

function refreshDerived() {
  const all = currentSnapshots();
  state.bootstrap.latest = all[0] ?? undefined;
  state.bootstrap.github = {
    ...state.bootstrap.github,
    connected: !!state.bootstrap.config.github.login,
    login: state.bootstrap.config.github.login,
    repo: state.bootstrap.config.github.owner ? `${state.bootstrap.config.github.owner}/${state.bootstrap.config.github.repo}` : "",
    clientId: state.bootstrap.config.github.clientId,
    clientIdConfigured: !!state.bootstrap.config.github.clientId,
    tokenPresent: !!state.bootstrap.config.github.login,
  };
  state.bootstrap.disk.path = state.bootstrap.config.backupDir;
}

function simulateBackup(): string {
  const id = taskId("backup");
  const kind = "backup" as const;
  const cloud = state.bootstrap.config.mode !== "local";
  const encrypted = state.bootstrap.config.encryptionLocal || cloud;
  const steps = [
    { phase: "precheck", percent: 3, message: "检查备份环境" },
    { phase: "stage", percent: 10, message: "暂存 DSH 核心数据" },
    { phase: "compress", percent: 38, message: "7z 正在压缩归档" },
    { phase: "compress", percent: 58, message: "7z 正在压缩归档" },
    { phase: "save", percent: 78, message: "写入本地备份目录" },
    { phase: "verify", percent: 86, message: "校验归档完整性" },
    ...(cloud ? [{ phase: "upload", percent: 94, message: "上传到 GitHub 私有仓库" }] : []),
    { phase: "done", percent: 100, message: "备份完成" },
  ];
  let index = 0;
  if (state.backupTimer) clearInterval(state.backupTimer);
  state.backupTimer = setInterval(() => {
    const step = steps[index];
    emitTask({
      taskId: id,
      kind,
      phase: step.phase,
      percent: step.percent,
      message: step.message,
      bytesDone: (step.percent / 100) * 103.2 * 1024 * 1024,
      bytesTotal: 103.2 * 1024 * 1024,
    });
    index += 1;
    if (index >= steps.length) {
      if (state.backupTimer) clearInterval(state.backupTimer);
      state.backupTimer = null;
      const createdAt = new Date();
      const stamp = createdAt.toISOString().replace(/[-:T]/g, "").slice(0, 14);
      const snapshot: Snapshot = {
        id: stamp,
        createdAt: iso(createdAt),
        kind: "manual",
        size: 64.4 * 1024 * 1024,
        sha256: "b".repeat(64),
        encrypted,
        desktopIncluded: state.bootstrap.config.includeDesktop,
        fileCount: 68214,
        totalBytes: 103.2 * 1024 * 1024,
        compression: state.bootstrap.config.compression.level,
        location: cloud ? (state.bootstrap.config.mode === "cloud" ? "cloud" : "both") : "local",
        source: cloud ? (state.bootstrap.config.mode === "cloud" ? "cloud" : "both") : "local",
        status: "ready",
        localPath: state.bootstrap.config.mode === "cloud" ? undefined : `${state.bootstrap.config.backupDir}\\dsh-backup-${stamp}.7z`,
        manifestPath: state.bootstrap.config.mode === "cloud" ? undefined : `${state.bootstrap.config.backupDir}\\dsh-backup-${stamp}.manifest.json`,
        cloudReleaseId: cloud ? 9000 + Math.floor(Math.random() * 1000) : undefined,
        cloudTag: cloud ? `backup-${stamp}` : undefined,
        cloudHtmlUrl: cloud ? "https://github.com/preview-user/dsh-backups/releases" : undefined,
      };
      if (state.bootstrap.config.mode !== "cloud") {
        state.bootstrap.localSnapshots.unshift(clone(snapshot));
      }
      if (cloud) {
        state.bootstrap.cloudSnapshots.unshift({ ...clone(snapshot), location: "cloud", source: "cloud", localPath: undefined, manifestPath: undefined });
      }
      if (encrypted) {
        state.bootstrap.key = { ...state.bootstrap.key, exists: true, keyId: "mock-key-id", publicKey: "age1mock", privatePath: state.bootstrap.paths.keysDir + "\\private.key" };
      }
      refreshDerived();
      emitResult({
        taskId: id,
        kind,
        success: true,
        message: "备份完成（预览模拟）",
        snapshot,
        warnings: cloud ? ["预览模式：云端上传为模拟结果，不会访问 GitHub。"] : [],
      });
      mockEvents.emit("state:changed", { reason: "backup" });
    }
  }, 420);
  return id;
}

export const mockApi = {
  async GetBootstrap(): Promise<Bootstrap> {
    await delay(80);
    refreshDerived();
    return clone(state.bootstrap);
  },
  async CompleteOnboarding(input: OnboardingInput): Promise<void> {
    await delay(120);
    const config = state.bootstrap.config;
    config.mode = input.mode;
    config.backupDir = input.backupDir;
    config.encryptionLocal = input.encryptionLocal;
    config.compression.level = input.compression;
    config.includeDesktop = input.includeDesktop;
    config.onboardingComplete = true;
    if (config.encryptionLocal || config.mode !== "local") {
      state.bootstrap.key = { exists: true, keyId: "mock-key-id", publicKey: "age1mock", privatePath: state.bootstrap.paths.keysDir + "\\private.key" };
    }
    refreshDerived();
    mockEvents.emit("state:changed", { reason: "onboarding" });
  },
  async SaveConfig(config: Config): Promise<void> {
    await delay(150);
    state.bootstrap.config = clone(config);
    if (config.encryptionLocal || config.mode !== "local") {
      state.bootstrap.key = { ...state.bootstrap.key, exists: true, keyId: state.bootstrap.key.keyId || "mock-key-id" };
    }
    refreshDerived();
    mockEvents.emit("state:changed", { reason: "settings" });
  },
  async PickDirectory(defaultDir: string): Promise<string> {
    await delay(80);
    return defaultDir || "D:\\DSHBackup\\backups";
  },
  async StartBackup(_force: boolean): Promise<string> {
    await delay(80);
    if (state.bootstrap.dsh.running) throw new Error("DSH_RUNNING:进程 DSH.exe 正在运行");
    return simulateBackup();
  },
  async CancelTask(): Promise<void> {
    if (state.backupTimer) clearInterval(state.backupTimer);
    state.backupTimer = null;
    if (state.currentTask) {
      emitResult({ taskId: state.currentTask.taskId, kind: state.currentTask.kind, success: false, message: "任务已取消", error: "任务已取消" });
    }
  },
  async StartRestore(request: RestoreRequest): Promise<string> {
    await delay(80);
    const id = taskId("restore");
    const kind = "restore" as const;
    const steps = [
      { phase: "download", percent: 18, message: "读取备份归档" },
      { phase: "decrypt", percent: 30, message: "解密归档" },
      { phase: "extract", percent: 52, message: "解压到受控暂存目录" },
      { phase: "snapshot", percent: 70, message: "创建恢复前快照" },
      { phase: "install", percent: 90, message: "覆盖恢复文件" },
      { phase: "done", percent: 100, message: "恢复完成" },
    ];
    let index = 0;
    if (state.backupTimer) clearInterval(state.backupTimer);
    state.backupTimer = setInterval(() => {
      const step = steps[index];
      emitTask({ taskId: id, kind, phase: step.phase, percent: step.percent, message: step.message, bytesDone: step.percent, bytesTotal: 100 });
      index += 1;
      if (index >= steps.length) {
        if (state.backupTimer) clearInterval(state.backupTimer);
        state.backupTimer = null;
        emitResult({
          taskId: id,
          kind,
          success: true,
          message: "恢复完成（预览模拟）",
          restore: {
            files: 68214,
            bytes: 103.2 * 1024 * 1024,
            desktopRestored: request.includeDesktop,
            targetDsh: request.customTarget || "C:\\Users\\23500\\.dsh",
          },
        });
        mockEvents.emit("state:changed", { reason: "restore" });
      }
    }, 400);
    return id;
  },
  async StartVerify(_request: VerifyRequest): Promise<string> {
    await delay(80);
    const id = taskId("verify");
    const kind = "verify" as const;
    emitTask({ taskId: id, kind, phase: "verify", percent: 15, message: "计算 SHA-256", bytesDone: 15, bytesTotal: 100 });
    setTimeout(() => {
      emitTask({ taskId: id, kind, phase: "verify", percent: 75, message: "7z 完整性测试", bytesDone: 75, bytesTotal: 100 });
    }, 450);
    setTimeout(() => {
      emitResult({ taskId: id, kind, success: true, message: "验证通过（预览模拟）" });
    }, 1100);
    return id;
  },
  async DeleteLocalSnapshot(id: string): Promise<void> {
    await delay(150);
    state.bootstrap.localSnapshots = state.bootstrap.localSnapshots.filter((snapshot) => snapshot.id !== id);
    refreshDerived();
    mockEvents.emit("state:changed", { reason: "snapshot-delete" });
  },
  async DeleteCloudSnapshot(releaseId: number): Promise<void> {
    await delay(150);
    state.bootstrap.cloudSnapshots = state.bootstrap.cloudSnapshots.filter((snapshot) => snapshot.cloudReleaseId !== releaseId);
    refreshDerived();
    mockEvents.emit("state:changed", { reason: "snapshot-delete" });
  },
  async RefreshCloudSnapshots(): Promise<Snapshot[]> {
    await delay(500);
    return clone(state.bootstrap.cloudSnapshots);
  },
  async CheckDSH() {
    await delay(100);
    state.bootstrap.dsh.running = false;
    state.bootstrap.dsh.evidence = [];
    return clone(state.bootstrap.dsh);
  },
  async SaveProxyPassword(password: string): Promise<void> {
    state.bootstrap.config.proxy.passwordSet = password.length > 0;
    state.bootstrap.proxyPassword = password.length > 0;
  },
  async ClearProxyPassword(): Promise<void> {
    state.bootstrap.config.proxy.passwordSet = false;
    state.bootstrap.proxyPassword = false;
  },
  async TestProxy(_request: ProxyTestRequest): Promise<ProxyTestResult> {
    await delay(650);
    return { reachable: true, status: 200, latencyMs: 48, message: "GitHub API 可访问（预览模拟）" };
  },
  async OpenBackupFolder(): Promise<void> { await delay(50); },
  async OpenKeysFolder(): Promise<void> { await delay(50); },
  async OpenLogsFolder(): Promise<void> { await delay(50); },
  async OpenDataFolder(): Promise<void> { await delay(50); },
  async OpenLocalSnapshot(_id: string): Promise<void> { await delay(50); },
  async OpenExternalURL(_url: string): Promise<void> { await delay(50); },
  async CopyText(_text: string): Promise<void> { await delay(20); },
  async OpenGitHubRepo(): Promise<void> { await delay(50); },
  async ExportDiagnostics(): Promise<string> {
    await delay(400);
    return "D:\\DSHBackup\\data\\logs\\dsh-backup-diagnostics-preview.zip";
  },
  async ReadLogTail(_lines: number): Promise<string> {
    return '{"time":"2026-10-26T19:00:00+08:00","level":"info","event":"app.start","message":"预览模式日志"}';
  },
  async StartGitHubDeviceFlow(clientId: string): Promise<DeviceFlowView> {
    await delay(300);
    if (!clientId.trim()) throw new Error("请先填写 GitHub OAuth App 的 Client ID；也可以在高级选项中使用 PAT");
    state.bootstrap.config.github.clientId = clientId.trim();
    const expiresAt = new Date(Date.now() + 900000);
    const view: DeviceFlowView = {
      userCode: "7GQ4-2K9M",
      verificationUri: "https://github.com/login/device",
      expiresIn: 900,
      interval: 5,
      expiresAt: expiresAt.toISOString(),
    };
    const waiting: GitHubAuthEvent = {
      status: "waiting",
      userCode: view.userCode,
      verificationUri: view.verificationUri,
      expiresAt: view.expiresAt,
      message: "等待你在浏览器中完成授权（预览模拟）",
    };
    mockEvents.emit("github:auth", waiting);
    if (state.githubTimer) clearTimeout(state.githubTimer);
    state.githubTimer = setTimeout(() => {
      const config = state.bootstrap.config;
      config.github.login = "preview-user";
      config.github.avatarUrl = "";
      config.github.connectedAt = iso(new Date());
      refreshDerived();
      const success: GitHubAuthEvent = {
        status: "success",
        message: "GitHub 已连接（预览模拟）",
        account: { login: "preview-user", name: "Preview User", avatarUrl: "", htmlUrl: "https://github.com/preview-user" },
      };
      mockEvents.emit("github:auth", success);
      mockEvents.emit("state:changed", { reason: "github" });
    }, 3600);
    return view;
  },
  async ConnectGitHubPAT(token: string): Promise<GitHubStatus> {
    await delay(500);
    if (!token.trim()) throw new Error("PAT 不能为空");
    const config = state.bootstrap.config;
    config.github.login = "preview-user";
    config.github.avatarUrl = "";
    config.github.connectedAt = iso(new Date());
    refreshDerived();
    mockEvents.emit("state:changed", { reason: "github" });
    return clone(state.bootstrap.github);
  },
  async DisconnectGitHub(): Promise<void> {
    await delay(150);
    state.bootstrap.config.github.login = "";
    state.bootstrap.config.github.avatarUrl = "";
    state.bootstrap.config.github.connectedAt = "";
    state.bootstrap.config.mode = "local";
    refreshDerived();
    mockEvents.emit("state:changed", { reason: "github" });
  },
  async ListGitHubRepos(query: string): Promise<GitHubRepo[]> {
    await delay(450);
    const lower = query.toLowerCase();
    return clone(state.repos.filter((repo) => !lower || repo.fullName.toLowerCase().includes(lower)));
  },
  async CreateGitHubRepo(name: string): Promise<GitHubRepo> {
    await delay(500);
    const repo: GitHubRepo = {
      id: Date.now(),
      name,
      fullName: `preview-user/${name}`,
      private: true,
      defaultBranch: "main",
      htmlUrl: `https://github.com/preview-user/${name}`,
      owner: "preview-user",
      canPush: true,
      updatedAt: iso(new Date()),
    };
    state.repos.push(repo);
    return clone(repo);
  },
  async SelectGitHubRepo(fullName: string): Promise<GitHubRepo> {
    await delay(350);
    const repo = state.repos.find((item) => item.fullName === fullName);
    if (!repo) throw new Error("找不到该仓库");
    state.bootstrap.config.github.owner = repo.owner;
    state.bootstrap.config.github.repo = repo.name;
    refreshDerived();
    mockEvents.emit("state:changed", { reason: "github" });
    return clone(repo);
  },
  async CancelGitHubAuth(): Promise<void> {
    if (state.githubTimer) clearTimeout(state.githubTimer);
    state.githubTimer = null;
    mockEvents.emit("github:auth", { status: "cancelled", message: "已取消 GitHub 授权" });
  },
};

export function mockEmitNotice(notice: Notice) {
  mockEvents.emit("app:notice", notice);
}
