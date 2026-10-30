<script setup lang="ts">
import { ref } from "vue";
import { useModalFocus } from "../lib/modalFocus";
import { closeDecision, dismissClosePrompt, store } from "../store";

const card = ref<HTMLElement | null>(null);
const remember = ref(true);
const busy = ref(false);
const failure = ref("");
useModalFocus(card, dismissClosePrompt);

async function choose(action: "exit" | "tray" | "cancel") {
  if (busy.value) return;
  if (action === "cancel") { dismissClosePrompt(); return; }
  busy.value = true;
  failure.value = "";
  try {
    await closeDecision(action, remember.value);
  } catch (error) {
    failure.value = error instanceof Error ? error.message : String(error);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div v-if="store.modals.closePrompt" class="overlay modal open" @click.self="choose('cancel')">
    <div ref="card" class="modal-card close-card" role="dialog" aria-modal="true" aria-label="关闭 DSH Backup">
      <div class="modal-head">
        <div>
          <span class="eyebrow">关闭</span>
          <h2>要退出，还是留在托盘？</h2>
        </div>
        <button class="icon-btn modal-close" :disabled="busy" aria-label="取消关闭" @click="choose('cancel')">×</button>
      </div>
      <div class="modal-body">
        <p class="modal-lead">退出会结束应用和正在运行的任务。最小化到系统托盘后应用继续在后台运行，计划备份照常触发。</p>
        <div v-if="store.closePromptTaskRunning" class="restore-warning">
          <span class="alert-icon">!</span>
          <p>现在有任务正在运行。退出会中断它；选择托盘则任务继续。</p>
        </div>
        <p v-if="!store.boot?.app.trayAvailable" class="hint">系统托盘当前不可用，只能退出应用。</p>
        <label class="check-row" style="margin-top: 12px">
          <input v-model="remember" type="checkbox" />
          <span>记住我的选择（以后可在「设置 → 运行环境」修改）</span>
        </label>
        <div v-if="failure" class="inline-error" role="alert">
          <svg class="icon"><use href="#i-alert"></use></svg>
          <div class="inline-error-body"><strong>操作没有完成</strong><p>{{ failure }}</p></div>
        </div>
      </div>
      <div class="modal-footer">
        <button class="btn btn-ghost" :disabled="busy" @click="choose('cancel')">取消</button>
        <button class="btn btn-secondary" :disabled="busy || !store.boot?.app.trayAvailable" @click="choose('tray')">最小化到托盘</button>
        <button class="btn btn-danger" :disabled="busy" @click="choose('exit')">退出</button>
      </div>
    </div>
  </div>
</template>
