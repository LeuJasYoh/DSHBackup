<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { api } from "../lib/bridge";
import {
  clearProxyPassword as clearStoredProxyPassword,
  createRepo,
  disconnectGitHub,
  exportDiagnostics,
  listRepos,
  openBackupFolder,
  openDataFolder,
  openExternal,
  openGitHubModal,
  openGitHubRepo,
  openKeysFolder,
  openLogsFolder,
  pickDirectory,
  saveConfig as storeSaveConfig,
  selectRepo,
  showToast,
  store,
  testProxy,
} from "../store";
import {
  compressionLabel,
  errorMessage,
  formatBytes,
  formatDate,
  scheduleLabel,
  weekdayLabel,
} from "../lib/format";
import type { CompressionLevel, Config, GitHubRepo, Mode, ProxyTestResult, SettingsTab } from "../lib/types";

const tabs: Array<{ id: SettingsTab; label: string }> = [
  { id: "backup", label: "备份" },
  { id: "cloud", label: "云端" },
  { id: "network", label: "网络" },
  { id: "security", label: "安全" },
  { id: "general", label: "常规" },
  { id: "about", label: "关于" },
];

const boot = computed(() => store.boot!);
const draft = ref<Config | null>(null);
const extraArgsText = ref("");
const proxyPassword = ref("");
const proxyPasswordTouched = ref(false);
const proxyTesting = ref(false);
const proxyTestResult = ref<ProxyTestResult | null>(null);
const repoQuery = ref("");
const repoList = ref<GitHubRepo[]>([]);
const repoLoading = ref(false);
const repoInput = ref("");
const newRepoName = ref("");
const repoCreating = ref(false);
const logTail = ref("");
const logLoading = ref(false);

const compressionHints: Record<CompressionLevel, string> = {
  fast: "备份更快，归档体积更大。",
  balanced: "速度与体积平衡，推荐。",
  max: "体积最小，压缩时间和 CPU 占用更高。",
};

const extraArgPatterns = [
  /^-mx=[0-9]$/,
  /^-md=(16|32|64|128|256|512|1024)m$/i,
  /^-mmt=([1-9]|[1-5][0-9]|6[0-4])$/,
  /^-mfb=([5-9]|[1-9][0-9]|1[0-9]{2}|2[0-6][0-9]|27[0-3])$/,
  /^-ms=(on|off)$/,
  /^-mqs=(on|off)$/,
];

function cloneConfig(config: Config): Config {
  return JSON.parse(JSON.stringify(config)) as Config;
}

function comparable(config: Config): string {
  const copy = cloneConfig(config);
  copy.proxy.passwordSet = false;
  copy.lastScheduledAt = "";
  copy.github.login = "";
  copy.github.avatarUrl = "";
  copy.github.connectedAt = "";
  return JSON.stringify(copy);
}

const dirty = computed(() => {
  if (!draft.value) return false;
  return comparable(draft.value) !== comparable(store.boot!.config);
});

function syncDraft() {
  const config = store.boot?.config;
  if (!config) return;
  draft.value = cloneConfig(config);
  extraArgsText.value = draft.value.compression.extraArgs.join(" ");
  repoInput.value = config.github.owner && config.github.repo ? `${config.github.owner}/${config.github.repo}` : "";
  proxyPassword.value = "";
  proxyPasswordTouched.value = false;
  proxyTestResult.value = null;
}

watch(
  () => store.boot?.config,
  () => {
    if (!draft.value || !dirty.value) syncDraft();
  },
  { deep: true },
);

onMounted(syncDraft);

function setTab(tab: SettingsTab) {
  store.settingsTab = tab;
}

function setMode(mode: Mode) {
  if (!draft.value) return;
  draft.value.mode = mode;
  if (mode !== "local" && !store.boot?.github.connected) {
    showToast("请先连接 GitHub 私有仓库", "warning");
    openGitHubModal();
  }
}

function cancelChanges() {
  syncDraft();
  showToast("已撤销未保存的修改");
}

function validate(): string {
  const config = draft.value;
  if (!config) return "设置尚未加载";
  if (!/^(?:[a-zA-Z]:[\\/]|\\\\)/.test(config.backupDir)) return "本地备份目录必须是绝对路径";
  if (config.retention.local < 1 || config.retention.local > 99) return "本地保留份数必须在 1 到 99 之间";
  if (config.retention.cloud < 1 || config.retention.cloud > 99) return "云端保留份数必须在 1 到 99 之间";
  if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(config.schedule.time)) return "计划时间格式必须为 HH:MM";
  if (config.mode !== "local" && !store.boot?.github.connected) return "云端模式需要先连接 GitHub";
  if (config.mode !== "local" && !store.boot?.github.repo) return "请先在“云端”标签选择私有仓库";
  if (config.proxy.mode === "manual") {
    try {
      const parsed = new URL(config.proxy.url);
      if (!["http:", "https:", "socks5:", "socks5h:"].includes(parsed.protocol)) return "代理协议只支持 HTTP/HTTPS/SOCKS5";
    } catch {
      return "手动代理地址无效";
    }
  }
  for (const arg of config.compression.extraArgs) {
    if (!extraArgPatterns.some((pattern) => pattern.test(arg))) return `不允许的 7z 高级参数：${arg}`;
  }
  return "";
}

async function save() {
  if (!draft.value) return;
  draft.value.compression.extraArgs = extraArgsText.value.split(/\s+/).filter(Boolean);
  const validationError = validate();
  if (validationError) {
    showToast(validationError, "error", 6000);
    return;
  }
  const current = store.boot!.config;
  draft.value.github.owner = current.github.owner;
  draft.value.github.repo = current.github.repo;
  draft.value.github.login = current.github.login;
  draft.value.github.avatarUrl = current.github.avatarUrl;
  draft.value.github.connectedAt = current.github.connectedAt;
  draft.value.github.clientId = current.github.clientId;
  draft.value.proxy.passwordSet = current.proxy.passwordSet;
  draft.value.lastScheduledAt = current.lastScheduledAt;
  try {
    await storeSaveConfig(cloneConfig(draft.value), proxyPasswordTouched.value ? proxyPassword.value : undefined);
    syncDraft();
  } catch {
    // store 已提示错误
  }
}

async function browseBackupDir() {
  if (!draft.value) return;
  const directory = await pickDirectory(draft.value.backupDir);
  if (directory) draft.value.backupDir = directory;
}

function goSecurity() {
  setTab("security");
}

async function refreshRepos() {
  repoLoading.value = true;
  try {
    repoList.value = await listRepos(repoQuery.value);
  } finally {
    repoLoading.value = false;
  }
}

async function useRepo(fullName: string) {
  if (!fullName.trim()) {
    showToast("请输入 owner/repo", "warning");
    return;
  }
  const ok = await selectRepo(fullName.trim());
  if (ok) {
    repoInput.value = fullName.trim();
    repoList.value = [];
  }
}

async function createNewRepo() {
  if (!newRepoName.value.trim()) {
    showToast("请输入新仓库名称", "warning");
    return;
  }
  repoCreating.value = true;
  try {
    const repo = await createRepo(newRepoName.value.trim());
    if (repo) {
      await useRepo(repo.fullName);
      newRepoName.value = "";
    }
  } finally {
    repoCreating.value = false;
  }
}

async function runProxyTest() {
  if (!draft.value || proxyTesting.value) return;
  proxyTesting.value = true;
  try {
    proxyTestResult.value = await testProxy({
      mode: draft.value.proxy.mode,
      url: draft.value.proxy.url,
      username: draft.value.proxy.username,
      password: proxyPassword.value,
    });
  } finally {
    proxyTesting.value = false;
  }
}

async function clearProxyPassword() {
  await clearStoredProxyPassword();
  proxyPassword.value = "";
  proxyPasswordTouched.value = false;
}

async function showLogs() {
  if (logLoading.value) return;
  logLoading.value = true;
  try {
    logTail.value = await api.readLogTail(120);
    if (!logTail.value) logTail.value = "（日志为空）";
  } catch (error) {
    showToast(errorMessage(error), "error");
  } finally {
    logLoading.value = false;
  }
}

function openDeveloperSettings() {
  void openExternal("https://github.com/settings/developers");
}

const keyIdShort = computed(() => {
  const keyId = boot.value.key.keyId || "";
  return keyId ? keyId.slice(0, 16) : "";
});
const encryptionForced = computed(() => draft.value?.mode !== "local");
</script>

<template>
  <section class="page" :class="{ active: store.page === 'settings' }">
    <div class="settings-lead">
      <p>默认仅本地备份；云端功能是可选项，不影响本地使用。</p>
      <span v-if="dirty" class="dirty-note"><span class="dirty-dot"></span>有未保存的修改</span>
    </div>

    <div class="settings-tabs" role="tablist" aria-label="设置分类">
      <button
        v-for="tab in tabs"
        :key="tab.id"
        class="settings-tab"
        :class="{ active: store.settingsTab === tab.id }"
        role="tab"
        @click="setTab(tab.id)"
      >
        {{ tab.label }}
      </button>
    </div>

    <template v-if="draft">
      <div v-show="store.settingsTab === 'backup'" class="settings-panel" :class="{ active: store.settingsTab === 'backup' }">
        <div class="card settings-card">
          <div class="form-block">
            <div class="form-row">
              <div class="form-label">备份方式</div>
              <div class="form-control">
                <div class="mode-options">
                  <button type="button" class="mode-option" :class="{ selected: draft.mode === 'local' }" @click="setMode('local')">
                    <span class="mode-option-title">仅本地</span>
                    <span class="mode-option-desc">完全离线</span>
                  </button>
                  <button type="button" class="mode-option" :class="{ selected: draft.mode === 'hybrid' }" @click="setMode('hybrid')">
                    <span class="mode-option-title">本地 + GitHub</span>
                    <span class="mode-option-desc">推荐的双副本</span>
                  </button>
                  <button type="button" class="mode-option" :class="{ selected: draft.mode === 'cloud' }" @click="setMode('cloud')">
                    <span class="mode-option-title">仅 GitHub</span>
                    <span class="mode-option-desc">没有本地副本</span>
                  </button>
                </div>
                <p class="hint">云端模式需要先在“云端”标签连接 GitHub。仅 GitHub 不推荐。</p>
              </div>
            </div>

            <div class="form-row">
              <div class="form-label">本地备份目录</div>
              <div class="form-control">
                <div class="path-input">
                  <input v-model="draft.backupDir" class="text-input mono" />
                  <button class="btn btn-secondary btn-small" @click="browseBackupDir">浏览</button>
                  <button class="btn btn-ghost btn-small" @click="openBackupFolder()">打开</button>
                </div>
                <p class="hint">便携版默认在应用目录下；更新/卸载不得删除此目录，建议另存一份离线副本。</p>
              </div>
            </div>

            <div class="form-row">
              <div class="form-label">备份范围</div>
              <div class="form-control">
                <label class="check-row">
                  <input type="checkbox" checked aria-disabled="true" tabindex="-1" @click.prevent />
                  <span><strong>DSH 核心数据</strong><small>凭据、会话、附件、插件、用量</small></span>
                </label>
                <label class="check-row">
                  <input v-model="draft.includeDesktop" type="checkbox" />
                  <span><strong>桌面登录态</strong><small>Cookies、Local Storage、窗口偏好</small></span>
                </label>
                <p class="hint">项目代码、日志和崩溃缓存不在备份范围内。</p>
              </div>
            </div>

            <div class="form-row">
              <div class="form-label">压缩等级</div>
              <div class="form-control">
                <div class="segmented">
                  <button
                    v-for="level in (['fast', 'balanced', 'max'] as CompressionLevel[])"
                    :key="level"
                    :class="{ active: draft.compression.level === level }"
                    @click="draft.compression.level = level"
                  >
                    {{ level === "fast" ? "快速" : level === "max" ? "极限" : "均衡" }}
                  </button>
                </div>
                <p class="hint">当前：{{ compressionLabel(draft.compression.level) }} · {{ compressionHints[draft.compression.level] }}</p>
                <details class="advanced-block">
                  <summary>高级压缩参数</summary>
                  <div class="advanced-grid">
                    <label>字典大小
                      <select v-model.number="draft.compression.dictionaryMb" class="text-input select-input">
                        <option :value="0">自动</option>
                        <option :value="64">64 MB</option>
                        <option :value="128">128 MB</option>
                        <option :value="256">256 MB</option>
                        <option :value="512">512 MB</option>
                      </select>
                    </label>
                    <label>线程数
                      <select v-model.number="draft.compression.threads" class="text-input select-input">
                        <option :value="0">自动</option>
                        <option :value="1">1</option>
                        <option :value="2">2</option>
                        <option :value="4">4</option>
                        <option :value="8">8</option>
                      </select>
                    </label>
                    <label class="advanced-wide">自定义参数
                      <input v-model="extraArgsText" class="text-input mono" placeholder="例如 -mfb=273 -ms=on" />
                    </label>
                  </div>
                  <p class="hint">参数直接传给 7za；使用参数数组，不经过 shell，非法或危险参数会被拒绝。</p>
                </details>
              </div>
            </div>

            <div class="form-row">
              <div class="form-label">计划备份</div>
              <div class="form-control">
                <div class="inline-options">
                  <label class="radio-inline"><input v-model="draft.schedule.kind" type="radio" value="manual" /> 手动</label>
                  <label class="radio-inline"><input v-model="draft.schedule.kind" type="radio" value="daily" /> 每天</label>
                  <label class="radio-inline"><input v-model="draft.schedule.kind" type="radio" value="weekly" /> 每周</label>
                </div>
                <div v-if="draft.schedule.kind !== 'manual'" class="inline-field schedule-fields">
                  <input v-model="draft.schedule.time" class="text-input compact" type="time" />
                  <select v-if="draft.schedule.kind === 'weekly'" v-model.number="draft.schedule.weekday" class="text-input select-input weekday-select">
                    <option v-for="day in 7" :key="day" :value="day - 1">{{ weekdayLabel(day - 1) }}</option>
                  </select>
                  <span class="hint inline-hint">应用运行时才会触发；DSH 正在运行时会跳过。</span>
                </div>
              </div>
            </div>

            <div class="form-row">
              <div class="form-label">保留份数</div>
              <div class="form-control">
                <div class="inline-field">
                  <input v-model.number="draft.retention.local" class="text-input compact" type="number" min="1" max="99" />
                  <span>份</span>
                  <label class="check-row inline-check"><input v-model="draft.preRestoreSnapshot" type="checkbox" /> 恢复前自动备份当前数据</label>
                </div>
                <p class="hint">按时间保留最近 N 份普通快照；“恢复前快照”不计入自动清理。</p>
              </div>
            </div>

            <div class="form-row">
              <div class="form-label">本地加密</div>
              <div class="form-control">
                <div class="static-value">
                  <span class="badge" :class="draft.encryptionLocal || draft.mode !== 'local' ? 'success' : 'muted'">
                    {{ draft.encryptionLocal || draft.mode !== "local" ? "开启" : "关闭" }}
                  </span>
                  <span>加密设置与本地密钥在“安全”标签中管理。</span>
                  <button class="text-button" @click="goSecurity">前往安全设置</button>
                </div>
                <p class="hint">默认关闭。若备份会放 U 盘、移动硬盘或网盘，建议开启。</p>
              </div>
            </div>

            <div class="form-row">
              <div class="form-label">备份后校验</div>
              <div class="form-control">
                <label class="switch-row">
                  <span class="switch-copy">生成归档后运行 7z 完整性测试</span>
                  <span class="switch">
                    <input v-model="draft.verifyAfterBackup" type="checkbox" />
                    <span class="switch-track"></span>
                  </span>
                </label>
                <p class="hint">会增加一点耗时，但能在写入 manifest 前发现损坏。</p>
              </div>
            </div>
          </div>
        </div>
        <div class="settings-footer">
          <button class="btn btn-ghost" @click="cancelChanges">撤销</button>
          <button class="btn btn-primary" :disabled="store.saving" @click="save">{{ store.saving ? "保存中…" : "保存更改" }}</button>
        </div>
      </div>

      <div v-show="store.settingsTab === 'cloud'" class="settings-panel" :class="{ active: store.settingsTab === 'cloud' }">
        <div v-if="!boot.github.connected" class="card cloud-card">
          <div class="cloud-icon">
            <svg class="icon-lg"><use href="#i-cloud"></use></svg>
          </div>
          <h2>尚未连接 GitHub</h2>
          <p>连接私有仓库后，可以把加密归档上传到 GitHub Releases。连接完全可选，不影响本地备份。</p>
          <button class="btn btn-primary" @click="openGitHubModal()">连接 GitHub</button>
          <p class="hint">使用系统浏览器完成 OAuth 授权；也可以在连接窗口使用 PAT 高级兜底。</p>
        </div>

        <template v-else>
          <div class="card connected-card">
            <div class="connection-row">
              <div class="account-avatar">GH</div>
              <div class="account-copy">
                <strong>GitHub 已连接</strong>
                <span>@{{ boot.github.login }} · token 保存在 Windows 凭据管理器</span>
              </div>
              <span class="badge success">已连接</span>
              <button class="btn btn-ghost btn-small" @click="disconnectGitHub">断开连接</button>
            </div>
          </div>

          <div class="card settings-card">
            <div class="form-row">
              <div class="form-label">私有仓库</div>
              <div class="form-control">
                <div class="static-value repo-current">
                  <span class="mono">{{ boot.github.repo || "尚未选择仓库" }}</span>
                  <span v-if="boot.github.repo" class="badge success">已配置</span>
                  <span v-else class="badge muted">必需</span>
                  <button v-if="boot.github.repo" class="text-button" @click="openGitHubRepo()">在 GitHub 打开</button>
                </div>
                <div class="path-input repo-input">
                  <input v-model="repoInput" class="text-input mono" placeholder="owner/repo" />
                  <button class="btn btn-secondary btn-small" @click="useRepo(repoInput)">使用此仓库</button>
                  <button class="btn btn-secondary btn-small" :disabled="repoLoading" @click="refreshRepos">
                    {{ repoLoading ? "读取中…" : "刷新私有仓库" }}
                  </button>
                </div>
                <p class="hint">只使用 Releases 存放归档；仓库必须是私有仓库，并且当前账号有写入权限。</p>
                <div v-if="repoList.length" class="repo-list">
                  <button v-for="repo in repoList" :key="repo.id" class="repo-row" @click="useRepo(repo.fullName)">
                    <span>
                      <strong>{{ repo.fullName }}</strong>
                      <small>{{ repo.defaultBranch || "无默认分支" }}</small>
                    </span>
                    <span class="badge" :class="repo.fullName === boot.github.repo ? 'success' : 'muted'">
                      {{ repo.fullName === boot.github.repo ? "当前" : "选择" }}
                    </span>
                  </button>
                </div>
                <div class="path-input create-repo-row">
                  <input v-model="newRepoName" class="text-input mono" placeholder="新私有仓库名称" />
                  <button class="btn btn-secondary btn-small" :disabled="repoCreating" @click="createNewRepo">
                    {{ repoCreating ? "创建中…" : "创建并选择" }}
                  </button>
                </div>
              </div>
            </div>

            <div class="form-row">
              <div class="form-label">云端保留</div>
              <div class="form-control">
                <div class="inline-field">
                  <input v-model.number="draft.retention.cloud" class="text-input compact" type="number" min="1" max="99" />
                  <span>份</span>
                  <span class="badge muted">上传成功后自动清理旧快照</span>
                </div>
                <p class="hint">GitHub Releases 单文件需小于 2 GiB；当前核心数据约 {{ formatBytes(store.boot?.dsh.sources.reduce((sum, source) => sum + (source.exists ? source.bytes : 0), 0)) }}。</p>
              </div>
            </div>

            <div class="form-row">
              <div class="form-label">OAuth App</div>
              <div class="form-control">
                <div class="static-value">
                  <span class="mono">{{ boot.github.clientId || "未配置 Client ID" }}</span>
                  <span class="badge" :class="boot.github.clientIdConfigured ? 'success' : 'muted'">
                    {{ boot.github.clientIdConfigured ? "Device Flow 可用" : "仅 PAT" }}
                  </span>
                  <button class="text-button" @click="openDeveloperSettings">GitHub Developer settings</button>
                </div>
                <p class="hint">在连接窗口中填写 Client ID；PAT 作为高级兜底。凭据不会写入日志或配置文件。</p>
              </div>
            </div>

            <div v-if="draft.mode === 'local'" class="cloud-mode-note">
              <span class="alert-icon">!</span>
              <div>
                <strong>当前备份方式仍是仅本地</strong>
                <span>仓库已配置，但不会自动上传。到“备份”标签切换为“本地 + GitHub”后生效。</span>
                <button class="text-button" @click="setTab('backup')">去切换备份方式</button>
              </div>
            </div>
          </div>

          <div class="settings-footer">
            <button class="btn btn-ghost" @click="cancelChanges">撤销</button>
            <button class="btn btn-primary" :disabled="store.saving" @click="save">{{ store.saving ? "保存中…" : "保存更改" }}</button>
          </div>
        </template>
      </div>

      <div v-show="store.settingsTab === 'network'" class="settings-panel" :class="{ active: store.settingsTab === 'network' }">
        <div class="card settings-card">
          <div class="form-row">
            <div class="form-label">代理模式</div>
            <div class="form-control">
              <div class="inline-options proxy-options">
                <label class="radio-inline"><input v-model="draft.proxy.mode" type="radio" value="system" /> 系统代理</label>
                <label class="radio-inline"><input v-model="draft.proxy.mode" type="radio" value="manual" /> 手动</label>
                <label class="radio-inline"><input v-model="draft.proxy.mode" type="radio" value="off" /> 不使用</label>
              </div>
              <p class="hint">只影响 GitHub API、上传和下载；仅本地备份不会初始化网络客户端。</p>
            </div>
          </div>

          <template v-if="draft.proxy.mode === 'manual'">
            <div class="form-row">
              <div class="form-label">手动代理</div>
              <div class="form-control">
                <div class="path-input">
                  <input v-model="draft.proxy.url" class="text-input mono" placeholder="http://127.0.0.1:7897" />
                  <button class="btn btn-secondary btn-small" :disabled="proxyTesting" @click="runProxyTest">
                    {{ proxyTesting ? "测试中…" : "测试连接" }}
                  </button>
                </div>
                <p class="hint" :class="{ success: proxyTestResult?.reachable, error: proxyTestResult && !proxyTestResult.reachable }">
                  <template v-if="proxyTestResult">
                    {{ proxyTestResult.reachable ? "代理连接正常" : "代理连接失败" }} · {{ proxyTestResult.message }}
                    <template v-if="proxyTestResult.reachable"> · {{ proxyTestResult.latencyMs }} ms</template>
                  </template>
                  <template v-else>支持 http、https 和 socks5；GitHub API 与上传都会走同一代理。</template>
                </p>
              </div>
            </div>
            <div class="form-row">
              <div class="form-label">代理认证</div>
              <div class="form-control">
                <div class="path-input proxy-auth">
                  <input v-model="draft.proxy.username" class="text-input" placeholder="用户名（可选）" autocomplete="off" />
                  <input
                    v-model="proxyPassword"
                    class="text-input"
                    type="password"
                    placeholder="密码（保存在凭据管理器）"
                    autocomplete="new-password"
                    @input="proxyPasswordTouched = true"
                  />
                  <button v-if="boot.proxyPassword" class="btn btn-ghost btn-small" @click="clearProxyPassword">清除已存密码</button>
                </div>
                <p class="hint">密码不会写入配置文件；代理测试会使用当前输入的密码或已保存的密码。</p>
              </div>
            </div>
          </template>

          <div class="form-row">
            <div class="form-label">绕过列表</div>
            <div class="form-control">
              <label class="check-row">
                <input v-model="draft.proxy.bypassLocal" type="checkbox" />
                <span><strong>本地地址绕过代理</strong><small>localhost、127.0.0.1 和本地网络</small></span>
              </label>
            </div>
          </div>
        </div>
        <div class="settings-footer">
          <button class="btn btn-ghost" @click="cancelChanges">撤销</button>
          <button class="btn btn-primary" :disabled="store.saving" @click="save">{{ store.saving ? "保存中…" : "保存更改" }}</button>
        </div>
      </div>

      <div v-show="store.settingsTab === 'security'" class="settings-panel" :class="{ active: store.settingsTab === 'security' }">
        <div class="card settings-card">
          <div class="form-row">
            <div class="form-label">本地加密</div>
            <div class="form-control">
              <label class="switch-row">
                <span class="switch-copy">
                  加密本地归档
                  <span class="info-icon" tabindex="0" data-tip="开启后在应用目录生成本地密钥对；每个归档使用随机密码加密，密码由公钥封装，恢复时用私钥解开。私钥文件丢失后无法恢复。">i</span>
                </span>
                <span class="switch">
                  <input
                    :checked="draft.encryptionLocal || encryptionForced"
                    type="checkbox"
                    :disabled="encryptionForced"
                    @change="draft.encryptionLocal = ($event.target as HTMLInputElement).checked"
                  />
                  <span class="switch-track"></span>
                </span>
              </label>
              <p class="hint">
                {{ encryptionForced ? "云端模式强制加密；本地和云端共用同一份加密归档，关闭加密不会生效。" : "默认关闭。开启后会生成本地密钥；加密备份离开本机后受保护，私钥文件丢失则无法恢复。" }}
              </p>
            </div>
          </div>

          <div class="form-row">
            <div class="form-label">云端加密</div>
            <div class="form-control">
              <div class="static-value">
                <span class="badge success">始终开启</span>
                <span>上传 GitHub 前，归档必须先在本地加密；私有仓库不等于端到端加密。</span>
              </div>
            </div>
          </div>

          <div class="form-row">
            <div class="form-label">GitHub token</div>
            <div class="form-control">
              <div class="static-value">
                <span class="badge" :class="boot.github.tokenPresent ? 'success' : 'muted'">{{ boot.github.tokenPresent ? "已保存" : "未连接" }}</span>
                <span>token 保存在 Windows 凭据管理器，不写入应用配置或日志。</span>
              </div>
            </div>
          </div>

          <div class="form-row">
            <div class="form-label">本地密钥</div>
            <div class="form-control">
              <div class="static-value">
                <span class="badge" :class="boot.key.exists ? 'success' : 'muted'">{{ boot.key.exists ? "已生成" : "未生成" }}</span>
                <span v-if="boot.key.exists" class="mono">key id {{ keyIdShort }}…</span>
                <span>密钥对位于 <span class="mono">data\keys</span>；开启加密后自动生成。</span>
                <button class="btn btn-secondary btn-small" @click="openKeysFolder()">打开密钥目录</button>
              </div>
              <p v-if="boot.keyError" class="hint error">{{ boot.keyError }}</p>
              <p class="hint">私钥丢失后加密备份无法恢复；换机恢复需要复制整个 data\keys 目录。应用不提供恢复密钥。</p>
            </div>
          </div>

          <div class="form-row">
            <div class="form-label">7z 加密参数</div>
            <div class="form-control">
              <div class="static-value">
                <span class="badge muted">AES-256</span>
                <span class="badge muted">文件头加密</span>
                <span>每个归档使用独立的随机密码；应用不记录密码到日志。</span>
              </div>
            </div>
          </div>
        </div>
        <div class="settings-footer">
          <button class="btn btn-ghost" @click="cancelChanges">撤销</button>
          <button class="btn btn-primary" :disabled="store.saving" @click="save">{{ store.saving ? "保存中…" : "保存更改" }}</button>
        </div>
      </div>

      <div v-show="store.settingsTab === 'general'" class="settings-panel" :class="{ active: store.settingsTab === 'general' }">
        <div class="card settings-card">
          <div class="form-row">
            <div class="form-label">语言</div>
            <div class="form-control">
              <select class="text-input select-input" disabled>
                <option>简体中文（首版）</option>
              </select>
              <p class="hint">首版只提供简体中文界面。</p>
            </div>
          </div>
          <div class="form-row">
            <div class="form-label">主题</div>
            <div class="form-control">
              <select class="text-input select-input" disabled>
                <option>浅色（首版）</option>
              </select>
              <p class="hint">深色主题不在首版范围内。</p>
            </div>
          </div>
          <div class="form-row">
            <div class="form-label">数据目录</div>
            <div class="form-control">
              <div class="path-input">
                <input class="text-input mono" :value="boot.paths.dataDir" readonly />
                <button class="btn btn-secondary btn-small" @click="openDataFolder()">打开</button>
              </div>
              <p class="hint">临时文件、日志和密钥都放在应用目录；本地备份成品在“备份”标签中单独设置。</p>
            </div>
          </div>
          <div class="form-row">
            <div class="form-label">计划任务</div>
            <div class="form-control">
              <div class="static-value">
                <span class="badge" :class="draft.schedule.kind === 'manual' ? 'muted' : 'success'">
                  {{ scheduleLabel(draft.schedule.kind, draft.schedule.time, draft.schedule.weekday) }}
                </span>
                <span v-if="boot.schedule.nextRun">下次：{{ formatDate(boot.schedule.nextRun) }}</span>
                <span v-else>应用关闭时不会运行计划任务。</span>
              </div>
            </div>
          </div>
          <div class="form-row">
            <div class="form-label">更新</div>
            <div class="form-control">
              <div class="static-value">
                <span class="badge muted">便携版</span>
                <span>请以新版本替换程序文件；更新流程不会删除 data 和 backups。</span>
              </div>
              <p class="hint">首版不提供自动更新和在线更新检查。</p>
            </div>
          </div>
        </div>
      </div>

      <div v-show="store.settingsTab === 'about'" class="settings-panel" :class="{ active: store.settingsTab === 'about' }">
        <div class="card about-card">
          <div class="about-mark"><img src="/icon.svg" alt="" /></div>
          <div>
            <h2>DSH Backup</h2>
            <p class="about-version">v{{ boot.app.version }} · Windows · 便携版</p>
          </div>
          <span class="badge success">运行中</span>
        </div>
        <div class="card settings-card">
          <div class="about-row"><span>构建时间</span><strong>{{ formatDate(boot.app.buildTime) }}</strong></div>
          <div class="about-row"><span>平台</span><strong>{{ boot.app.platform }}</strong></div>
          <div class="about-row"><span>应用目录</span><strong class="mono">{{ boot.paths.appDir }}</strong></div>
          <div class="about-row">
            <span>压缩后端</span>
            <strong>{{ boot.app.sevenZip.version || "不可用" }}</strong>
          </div>
          <div class="about-row">
            <span>Rust sidecar</span>
            <strong>{{ boot.app.sidecar.version || "不可用" }}</strong>
          </div>
          <div class="about-row"><span>备份范围</span><strong>DSH 核心数据 + 可选桌面登录态</strong></div>
          <div class="about-row"><span>云端</span><strong>GitHub 私有仓库 Releases</strong></div>
          <div class="about-row"><span>许可证</span><span>7-Zip LGPL 2.1+ · age MIT/Apache-2.0 · 本项目</span></div>
          <div class="tool-grid">
            <button class="btn btn-secondary btn-small" @click="exportDiagnostics()">导出诊断包</button>
            <button class="btn btn-secondary btn-small" @click="openLogsFolder()">打开日志目录</button>
            <button class="btn btn-secondary btn-small" :disabled="logLoading" @click="showLogs">
              {{ logLoading ? "读取中…" : "查看最近日志" }}
            </button>
          </div>
          <pre v-if="logTail" class="log-preview">{{ logTail }}</pre>
          <div class="about-notice">
            诊断包包含配置、快照清单、工具版本和脱敏日志；不包含 token、代理密码、私钥、归档密码或 DSH 凭据内容。
            7-Zip 使用 LGPL 2.1+ 许可证，随包携带 License.txt；本项目不解压不可信来源的归档。
          </div>
        </div>
      </div>
    </template>
  </section>
</template>
