<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { completeOnboarding, createRepo, pickDirectory, selectRepo, showToast, store, openGitHubModal } from "../store";
import { compressionLabel, formatBytes, modeLabel } from "../lib/format";
import type { CompressionLevel, Mode } from "../lib/types";

const step = ref(1);
const mode = ref<Mode>("local");
const backupDir = ref(store.boot?.paths.defaultBackupDir || "");
const compression = ref<CompressionLevel>("balanced");
const encryptionLocal = ref(false);
const includeDesktop = ref(true);
const saving = ref(false);
const repoInput = ref("");
const newRepoName = ref("");
const repoBusy = ref(false);

const cloudMode = computed(() => mode.value !== "local");
const connected = computed(() => !!store.boot?.github.connected);
const forcedEncryption = computed(() => cloudMode.value);
const effectiveEncryption = computed(() => forcedEncryption.value || encryptionLocal.value);
const sourceSummary = computed(() => {
  const sources = store.boot?.dsh.sources ?? [];
  return sources.reduce((sum, source) => sum + (source.exists ? source.bytes : 0), 0);
});

watch(
  [connected, () => store.boot?.github.repo],
  ([value]) => {
    if (value && !repoInput.value) {
      const login = store.boot?.github.login || "owner";
      repoInput.value = store.boot?.github.repo || `${login}/dsh-backups`;
    }
  },
  { immediate: true },
);

async function useRepo() {
  if (!repoInput.value.trim()) {
    showToast("请输入 owner/repo", "warning");
    return;
  }
  repoBusy.value = true;
  try {
    await selectRepo(repoInput.value.trim());
  } finally {
    repoBusy.value = false;
  }
}

async function createPrivateRepo() {
  if (!newRepoName.value.trim()) {
    showToast("请输入新仓库名称", "warning");
    return;
  }
  repoBusy.value = true;
  try {
    const repo = await createRepo(newRepoName.value.trim());
    if (repo) {
      await selectRepo(repo.fullName);
      repoInput.value = repo.fullName;
      newRepoName.value = "";
    }
  } finally {
    repoBusy.value = false;
  }
}

const compressionHints: Record<CompressionLevel, string> = {
  fast: "备份更快，归档体积更大。",
  balanced: "速度与体积平衡，推荐。",
  max: "体积最小，压缩时间和 CPU 占用更高。",
};

const modes = [
  {
    value: "local" as Mode,
    title: "仅本地备份",
    desc: "完全离线，不需要 GitHub；之后可以随时添加云端。",
    badge: "推荐",
    muted: false,
  },
  {
    value: "hybrid" as Mode,
    title: "本地 + GitHub",
    desc: "本地保留一份，再加密上传到你的私有仓库。",
    badge: "双副本",
    muted: false,
  },
  {
    value: "cloud" as Mode,
    title: "仅 GitHub",
    desc: "没有本地副本，断网或账号异常时无法恢复。",
    badge: "不推荐",
    muted: true,
  },
];

async function browse() {
  const dir = await pickDirectory(backupDir.value);
  if (dir) backupDir.value = dir;
}

async function next() {
  if (step.value < 3) {
    step.value += 1;
    return;
  }
  if (mode.value !== "local" && !connected.value) {
    showToast("请先连接 GitHub 私有仓库", "warning");
    openGitHubModal();
    return;
  }
  if (mode.value !== "local" && !store.boot?.github.repo) {
    showToast("请先选择或创建一个私有仓库", "warning");
    step.value = 2;
    return;
  }
  await finish();
}

function back() {
  if (step.value > 1) step.value -= 1;
}

async function skip() {
  mode.value = "local";
  encryptionLocal.value = false;
  await finish();
}

async function finish() {
  if (saving.value) return;
  if (!backupDir.value.trim()) {
    showToast("请选择本地备份目录", "warning");
    step.value = 2;
    return;
  }
  saving.value = true;
  try {
    await completeOnboarding({
      mode: mode.value,
      backupDir: backupDir.value.trim(),
      encryptionLocal: effectiveEncryption.value,
      compression: compression.value,
      includeDesktop: includeDesktop.value,
    });
  } catch {
    // store 已提示错误，保留向导让用户修正。
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <div class="overlay onboarding open">
    <div class="wizard">
      <div class="wizard-top">
        <div class="wizard-brand">
          <span class="mini-mark"><img src="/icon.svg" alt="" /></span>
          <span>DSH Backup</span>
        </div>
        <span class="step-count">{{ step }} / 3</span>
      </div>
      <div class="wizard-progress"><span :style="{ width: `${(step / 3) * 100}%` }"></span></div>

      <div class="wizard-body">
        <template v-if="step === 1">
          <h2>欢迎使用 DSH Backup</h2>
          <p class="wizard-lead">只备份 DSH 的核心数据，不读取项目代码。先选择备份方式，后续可以在设置中修改。</p>
          <div class="choice-list">
            <label
              v-for="item in modes"
              :key="item.value"
              class="choice-card"
              :class="{ selected: mode === item.value }"
            >
              <input v-model="mode" type="radio" name="wizardMode" :value="item.value" />
              <div class="choice-title">{{ item.title }}</div>
              <span class="choice-badge" :class="{ muted: item.muted }">{{ item.badge }}</span>
              <div class="choice-desc">{{ item.desc }}</div>
            </label>
          </div>
          <div class="wizard-note">
            <span class="dot"></span>
            <span>当前可备份约 {{ formatBytes(sourceSummary) }} 的 DSH 数据；项目代码永远不在范围内。</span>
          </div>
        </template>

        <template v-else-if="step === 2">
          <h2>设置本地备份</h2>
          <p class="wizard-lead">这些选项稍后都可以在设置中修改。默认不加密，适合只在本机使用的备份。</p>
          <div class="wizard-fields">
            <div class="wizard-field">
              <label for="wizardDir">本地备份目录</label>
              <div class="path-input">
                <input id="wizardDir" v-model="backupDir" class="text-input mono" />
                <button class="btn btn-secondary btn-small" @click="browse">浏览</button>
              </div>
              <p class="hint">便携版默认在应用目录下；删除应用目录前请先移走备份。</p>
            </div>

            <div class="wizard-field">
              <span class="wizard-label">压缩等级</span>
              <div class="segmented">
                <button v-for="level in (['fast', 'balanced', 'max'] as CompressionLevel[])" :key="level" :class="{ active: compression === level }" @click="compression = level">
                  {{ level === "fast" ? "快速" : level === "max" ? "极限" : "均衡" }}
                </button>
              </div>
              <p class="hint">当前：{{ compressionLabel(compression) }} · {{ compressionHints[compression] }}</p>
            </div>

            <div class="wizard-field">
              <label class="switch-row">
                <span class="switch-copy">
                  包含桌面登录态
                  <span class="info-icon" tabindex="0" data-tip="包含 Electron 桌面端的 Cookies、Local Storage 和偏好；同机同用户恢复成功率最高。">i</span>
                </span>
                <span class="switch">
                  <input v-model="includeDesktop" type="checkbox" />
                  <span class="switch-track"></span>
                </span>
              </label>
              <p class="hint">只影响登录态与界面状态，不影响 DSH 的会话和 API Key。</p>
            </div>

            <div class="wizard-field">
              <label class="switch-row">
                <span class="switch-copy">
                  加密本地备份
                  <span class="info-icon" tabindex="0" data-tip="开启后在应用目录生成一对本地密钥；每个归档使用随机密码加密，密码由公钥封装。私钥丢失后无法恢复。">i</span>
                </span>
                <span class="switch">
                  <input
                    :checked="effectiveEncryption"
                    type="checkbox"
                    :disabled="forcedEncryption"
                    @change="encryptionLocal = ($event.target as HTMLInputElement).checked"
                  />
                  <span class="switch-track"></span>
                </span>
              </label>
              <p class="hint">
                {{ forcedEncryption ? "云端模式强制加密，本地和云端共用同一份加密归档。" : "默认关闭；如果备份会放 U 盘或网盘，建议开启。" }}
              </p>
            </div>

            <div v-if="cloudMode" class="wizard-connect-row">
              <span>{{ connected ? "GitHub 已连接；还需要选择一个私有仓库。" : "云端模式需要先连接 GitHub。" }}</span>
              <span v-if="connected" class="badge success">已连接</span>
              <button v-else class="btn btn-secondary btn-small" @click="openGitHubModal">连接 GitHub</button>
            </div>

            <div v-if="cloudMode && connected" class="wizard-field repo-setup">
              <label for="wizardRepo">私有仓库</label>
              <div class="path-input">
                <input id="wizardRepo" v-model="repoInput" class="text-input mono" placeholder="owner/repo" />
                <button class="btn btn-secondary btn-small" :disabled="repoBusy" @click="useRepo">
                  {{ repoBusy ? "校验中…" : "使用" }}
                </button>
              </div>
              <div class="path-input repo-create">
                <input v-model="newRepoName" class="text-input mono" placeholder="或新建私有仓库名称" />
                <button class="btn btn-secondary btn-small" :disabled="repoBusy" @click="createPrivateRepo">创建并选择</button>
              </div>
              <p class="hint">
                当前：{{ store.boot?.github.repo || "尚未选择" }}。仓库必须是私有的，并且当前账号有写入权限。
              </p>
            </div>
          </div>
        </template>

        <template v-else>
          <h2>准备就绪</h2>
          <p class="wizard-lead">确认下面的设置，进入主界面后就可以创建第一份备份。</p>
          <div class="wizard-summary">
            <div class="summary-row"><span>备份方式</span><strong>{{ modeLabel(mode) }}</strong></div>
            <div class="summary-row"><span>本地目录</span><strong class="mono">{{ backupDir }}</strong></div>
            <div class="summary-row"><span>压缩等级</span><strong>{{ compressionLabel(compression) }}</strong></div>
            <div class="summary-row"><span>桌面登录态</span><strong>{{ includeDesktop ? "包含" : "不包含" }}</strong></div>
            <div class="summary-row"><span>本地加密</span><strong>{{ effectiveEncryption ? "开启" : "关闭" }}</strong></div>
            <div class="summary-row"><span>云端</span><strong>{{ mode === "local" ? "不连接" : connected ? `GitHub · ${store.boot?.github.repo || "待选择仓库"}` : "尚未连接" }}</strong></div>
          </div>
          <div class="summary-note">
            <span class="dot"></span>
            <span>{{ mode === "local" ? "仅本地模式不会发起网络请求，也不会要求 GitHub 登录。" : "云端归档始终加密；本地是否加密按你的选择执行。" }}</span>
          </div>
        </template>
      </div>

      <div class="wizard-footer">
        <button class="btn btn-ghost" :disabled="saving" @click="skip">稍后设置</button>
        <div class="wizard-actions">
          <button class="btn btn-secondary" :disabled="saving" :style="{ visibility: step === 1 ? 'hidden' : 'visible' }" @click="back">上一步</button>
          <button class="btn btn-primary" :disabled="saving" @click="next">
            {{ saving ? "正在保存…" : step === 3 ? "完成设置" : "下一步" }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
