<script setup lang="ts">
import { ref } from "vue";
import { useModalFocus } from "../lib/modalFocus";
import { completeOnboarding, pickDirectory, store } from "../store";
import { compressionLabel, compressionShort, errorMessage, isAbsoluteDirectory } from "../lib/format";
import type { CompressionLevel } from "../lib/types";

const card = ref<HTMLElement | null>(null);
useModalFocus(card);
const step = ref(1);
const backupDir = ref(store.boot?.paths.defaultBackupDir || "");
const compression = ref<CompressionLevel>("balanced");
const includeDesktop = ref(true);
const busy = ref(false);
const failure = ref("");
async function browse() {
  if (busy.value) return;
  busy.value = true;
  try { const dir = await pickDirectory(backupDir.value); if (dir) backupDir.value = dir; }
  catch (error) { failure.value = errorMessage(error); }
  finally { busy.value = false; }
}
function next() {
  if (busy.value) return;
  if (!isAbsoluteDirectory(backupDir.value)) { failure.value = "请选择有效的 Windows 绝对目录。"; return; }
  step.value = 2;
}
function useDefaults() {
  if (busy.value) return;
  backupDir.value = store.boot?.paths.defaultBackupDir || "";
  compression.value = "balanced";
  includeDesktop.value = true;
  next();
}
async function finish() {
  if (busy.value) return;
  if (!isAbsoluteDirectory(backupDir.value)) { failure.value = "请选择有效的 Windows 绝对目录。"; step.value = 1; return; }
  busy.value = true;
  failure.value = "";
  try {
    await completeOnboarding({ backupDir: backupDir.value.trim(), compression: compression.value, includeDesktop: includeDesktop.value, encryptionLocal: false });
  } catch (error) { failure.value = errorMessage(error); }
  finally { busy.value = false; }
}
</script>

<template>
  <div class="overlay onboarding open">
    <div ref="card" class="wizard" role="dialog" aria-modal="true" aria-label="首次设置">
      <div class="wizard-top">
        <div class="wizard-brand"><span class="mini-mark"><svg class="whale-mark" aria-hidden="true"><use href="#i-whale"></use></svg></span><span>DSH Backup</span></div>
        <span class="step-count">{{ step }} / 2</span>
      </div>
      <div class="wizard-progress" role="progressbar" :aria-valuenow="step * 50" aria-valuemin="0" aria-valuemax="100" aria-label="设置进度"><span :style="{ width: `${step * 50}%` }"></span></div>
      <div class="wizard-body">
        <fieldset class="form-fieldset" :disabled="busy">
          <template v-if="step === 1">
            <h2>设置本地备份</h2>
            <p class="wizard-lead">只备份 DSH 核心数据，不读取项目代码。选择存放目录，下一步确认。</p>
            <div class="wizard-fields">
              <div class="wizard-field">
                <label for="wizardDir">备份目录</label>
                <div class="path-input"><input id="wizardDir" v-model="backupDir" autofocus class="text-input mono" /><button class="btn btn-secondary btn-small" type="button" @click="browse">浏览</button></div>
                <p class="hint">默认在应用目录下。删除应用目录前，请先移走备份。</p>
              </div>
              <div class="wizard-field">
                <label class="switch-row">
                  <span class="switch-copy">包含桌面登录态</span>
                  <span class="switch"><input v-model="includeDesktop" type="checkbox" aria-label="包含桌面登录态" /><span class="switch-track"></span></span>
                </label>
                <p class="hint">Cookies、Local Storage 和窗口偏好；同机同用户恢复更可靠。</p>
              </div>
              <div class="wizard-field">
                <span class="wizard-label">压缩等级</span>
                <div class="segmented" role="group" aria-label="压缩等级"><button v-for="level in (['fast', 'balanced', 'max'] as CompressionLevel[])" :key="level" type="button" :class="{ active: compression === level }" :aria-pressed="compression === level" @click="compression = level">{{ compressionShort(level) }}</button></div>
                <p class="hint">默认均衡；极限压缩会增加耗时与内存占用。</p>
              </div>
            </div>
          </template>
          <template v-else>
            <h2>确认首次设置</h2>
            <p class="wizard-lead">完成后进入总览。此步骤只保存设置，不会立即执行备份。</p>
            <div class="wizard-summary">
              <div class="summary-row"><span>备份目录</span><strong class="mono path-wrap">{{ backupDir }}</strong></div>
              <div class="summary-row"><span>备份内容</span><strong>DSH 核心数据{{ includeDesktop ? " + 桌面登录态" : "" }}</strong></div>
              <div class="summary-row"><span>压缩</span><strong>{{ compressionLabel(compression) }}</strong></div>
              <div class="summary-row"><span>本地加密</span><strong>关闭</strong></div>
            </div>
          </template>
        </fieldset>
        <p class="summary-note">首次设置默认不加密。备份可能包含敏感数据，可稍后在「设置 → 本地加密」开启本地加密并妥善保管私钥。</p>
      </div>
      <div class="wizard-bottom">
        <p v-if="failure" class="inline-error" role="alert">{{ failure }}</p>
        <div class="wizard-footer">
          <button v-if="step === 1" class="btn btn-ghost" type="button" :disabled="busy" @click="useDefaults">使用默认设置</button>
          <button v-else class="btn btn-secondary" type="button" :disabled="busy" @click="step = 1">上一步</button>
          <button class="btn btn-primary" type="button" :disabled="busy" @click="step === 1 ? next() : finish()">{{ busy ? "处理中…" : step === 1 ? "下一步" : "完成设置" }}</button>
        </div>
      </div>
    </div>
  </div>
</template>
