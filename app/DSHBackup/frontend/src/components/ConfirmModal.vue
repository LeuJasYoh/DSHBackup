<script setup lang="ts">
import { ref } from "vue";
import { useModalFocus } from "../lib/modalFocus";
import { closeConfirm, store } from "../store";
import { errorMessage } from "../lib/format";

const card = ref<HTMLElement | null>(null);
const busy = ref(false);
useModalFocus(card, cancel);
const failure = ref("");

async function confirm() {
  const state = store.modals.confirm;
  if (!state || busy.value) return;
  busy.value = true;
  failure.value = "";
  try {
    await state.onConfirm();
    if (store.modals.confirm === state) closeConfirm();
  } catch (error) {
    // 失败时窗口保持打开：toast 6 秒后就没了，用户需要看到原因并重试或撤销
    failure.value = errorMessage(error);
  } finally {
    busy.value = false;
  }
}

function cancel() {
  if (busy.value) return;
  closeConfirm();
}
</script>

<template>
  <div v-if="store.modals.confirm" class="overlay modal open" @click.self="cancel">
    <div ref="card" class="modal-card confirm-card" role="dialog" aria-modal="true" :aria-label="store.modals.confirm.title">
      <div class="modal-head">
        <div>
          <span class="eyebrow">{{ store.modals.confirm.danger ? "请确认" : "提示" }}</span>
          <h2>{{ store.modals.confirm.title }}</h2>
        </div>
        <button class="icon-btn modal-close" :disabled="busy" aria-label="关闭" @click="cancel">×</button>
      </div>
      <div class="modal-body">
        <p class="modal-lead">{{ store.modals.confirm.message }}</p>
        <div v-if="store.modals.confirm.detail" class="restore-warning">
          <span class="alert-icon">!</span>
          <p>{{ store.modals.confirm.detail }}</p>
        </div>
        <div v-if="failure" class="inline-error" role="alert">
          <svg class="icon"><use href="#i-alert"></use></svg>
          <div class="inline-error-body">
            <strong>操作没有完成</strong>
            <p>{{ failure }}</p>
          </div>
        </div>
      </div>
      <div class="modal-footer">
        <button class="btn btn-ghost" :disabled="busy" @click="cancel">
          {{ store.modals.confirm.cancelLabel || "取消" }}
        </button>
        <button
          class="btn"
          :class="store.modals.confirm.danger ? 'btn-danger' : 'btn-primary'"
          :disabled="busy"
          @click="confirm"
        >
          {{ busy ? "处理中…" : failure ? "重试" : store.modals.confirm.confirmLabel }}
        </button>
      </div>
    </div>
  </div>
</template>
