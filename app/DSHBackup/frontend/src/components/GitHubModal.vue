<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import {
  closeGitHubModal,
  connectPAT,
  copyText,
  openExternal,
  startDeviceFlow,
  store,
} from "../store";

const mode = ref<"device" | "pat">("device");
const clientId = ref(store.boot?.github.clientId ?? "");
const pat = ref("");
const error = ref("");
const tick = ref(Date.now());
let timer: ReturnType<typeof setInterval> | null = null;

const waiting = computed(() => store.githubBusy && store.githubAuth?.status !== "success");
const success = computed(() => store.githubAuth?.status === "success");
const failed = computed(() => store.githubAuth?.status === "error");
const flow = computed(() => store.githubFlow);
const remaining = computed(() => {
  const expiresAt = flow.value?.expiresAt || store.githubAuth?.expiresAt;
  if (!expiresAt) return "";
  const difference = Math.max(0, new Date(expiresAt).getTime() - tick.value);
  const minutes = Math.floor(difference / 60000);
  const seconds = Math.floor((difference % 60000) / 1000);
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
});

onMounted(() => {
  timer = setInterval(() => {
    tick.value = Date.now();
  }, 1000);
});

onUnmounted(() => {
  if (timer) clearInterval(timer);
});

async function beginDeviceFlow() {
  error.value = "";
  if (!clientId.value.trim()) {
    error.value = "请填写 OAuth App Client ID。你可以在 GitHub Developer settings 中创建 OAuth App，并勾选 Enable Device Flow。";
    return;
  }
  await startDeviceFlow(clientId.value.trim());
}

async function submitPAT() {
  error.value = "";
  if (!pat.value.trim()) {
    error.value = "请粘贴带 repo 权限的 fine-grained PAT。";
    return;
  }
  const ok = await connectPAT(pat.value.trim());
  if (ok) {
    pat.value = "";
    closeGitHubModal();
  }
}

async function openBrowser() {
  const url = flow.value?.verificationUri || store.githubAuth?.verificationUri || "https://github.com/login/device";
  await openExternal(url);
}

function openDeveloperSettings() {
  void openExternal("https://github.com/settings/developers");
}

function close() {
  closeGitHubModal();
}
</script>

<template>
  <div class="overlay modal open" @click.self="close">
    <div class="modal-card github-modal">
      <div class="modal-head">
        <div>
          <span class="eyebrow">云端 · 可选</span>
          <h2>连接 GitHub 私有仓库</h2>
        </div>
        <button class="icon-btn modal-close" aria-label="关闭" @click="close">×</button>
      </div>

      <div class="modal-body">
        <template v-if="success">
          <div class="github-success">
            <div class="success-mark"><svg class="icon"><use href="#i-check"></use></svg></div>
            <div>
              <strong>GitHub 已连接</strong>
              <p>{{ store.githubAuth?.account?.login || store.boot?.github.login }} · token 已保存到 Windows 凭据管理器</p>
            </div>
          </div>
          <p class="modal-hint">下一步在“云端”设置中选择一个私有仓库；云端归档始终加密。</p>
        </template>

        <template v-else>
          <p class="modal-lead">使用系统浏览器完成 GitHub 官方授权，应用不会接触你的 GitHub 密码。token 只保存在本机凭据管理器。</p>

          <div class="auth-tabs">
            <button :class="{ active: mode === 'device' }" @click="mode = 'device'">Device Flow</button>
            <button :class="{ active: mode === 'pat' }" @click="mode = 'pat'">PAT 高级兜底</button>
          </div>

          <template v-if="mode === 'device'">
            <div class="field-block">
              <label class="field-label" for="oauthClientId">OAuth App Client ID</label>
              <input id="oauthClientId" v-model="clientId" class="text-input mono" placeholder="例如 Iv1.0123456789abcdef" :disabled="waiting" />
              <p class="hint">
                Device Flow 需要你自己的 OAuth App：在
                <button class="text-button inline-link" @click="openDeveloperSettings">GitHub Developer settings</button>
                创建 OAuth App，并勾选 Enable Device Flow。Client ID 不是密钥，可以保存在本机配置中。
              </p>
            </div>

            <div v-if="flow" class="device-flow-box">
              <div class="device-step">
                <span class="step-index">1</span>
                <div>
                  <strong>打开授权页面</strong>
                  <p class="mono">{{ flow.verificationUri }}</p>
                </div>
              </div>
              <div class="device-step">
                <span class="step-index">2</span>
                <div>
                  <strong>输入下面的代码</strong>
                  <div class="code-box"><span>{{ flow.userCode }}</span></div>
                  <div class="device-actions">
                    <button class="btn btn-secondary btn-small" @click="copyText(flow.userCode, '授权码已复制')">复制代码</button>
                    <button class="btn btn-secondary btn-small" @click="openBrowser">打开浏览器</button>
                  </div>
                </div>
              </div>
            </div>

            <div v-if="waiting" class="wait-line">
              <span class="spinner"></span>
              {{ store.githubAuth?.message || "等待你在浏览器中完成授权…" }}
              <span v-if="remaining" class="code-expiry">{{ remaining }}</span>
            </div>

            <div v-if="failed" class="inline-error">
              <svg class="icon"><use href="#i-alert"></use></svg>
              <span>{{ store.githubAuth?.message || "授权失败，请重试。" }}</span>
            </div>
            <div v-if="error" class="inline-error">
              <svg class="icon"><use href="#i-alert"></use></svg>
              <span>{{ error }}</span>
            </div>

            <button v-if="!flow || failed" class="btn btn-primary connect-button" :disabled="waiting" @click="beginDeviceFlow">
              {{ waiting ? "等待授权…" : "开始 Device Flow 授权" }}
            </button>
            <button v-else-if="!waiting" class="btn btn-secondary connect-button" @click="beginDeviceFlow">重新获取授权码</button>
          </template>

          <template v-else>
            <div class="field-block">
              <label class="field-label" for="patInput">Fine-grained PAT</label>
              <input
                id="patInput"
                v-model="pat"
                class="text-input mono"
                type="password"
                autocomplete="off"
                placeholder="github_pat_..."
                :disabled="store.githubBusy"
              />
              <p class="hint">需要对该私有仓库具备 Contents 读写权限；token 只保存在 Windows 凭据管理器，不会写进配置或日志。</p>
            </div>
            <div v-if="error" class="inline-error">
              <svg class="icon"><use href="#i-alert"></use></svg>
              <span>{{ error }}</span>
            </div>
            <button class="btn btn-primary connect-button" :disabled="store.githubBusy" @click="submitPAT">
              {{ store.githubBusy ? "正在验证…" : "使用 PAT 连接" }}
            </button>
          </template>

          <p class="modal-hint">网络不通？可以先关闭此窗口，在“设置 → 网络”中配置代理。</p>
        </template>
      </div>

      <div class="modal-footer">
        <button v-if="success" class="btn btn-primary" @click="close">完成</button>
        <button v-else class="btn btn-ghost" @click="close">取消</button>
      </div>
    </div>
  </div>
</template>
