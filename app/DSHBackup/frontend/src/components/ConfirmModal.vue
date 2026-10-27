<script setup lang="ts">
import { ref } from "vue";
import { closeConfirm, showToast, store } from "../store";
import { errorMessage } from "../lib/format";

const busy = ref(false);

async function confirm() {
  const state = store.modals.confirm;
  if (!state || busy.value) return;
  busy.value = true;
  try {
    await state.onConfirm();
    closeConfirm();
  } catch (error) {
    showToast(errorMessage(error), "error", 6000);
    closeConfirm();
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
    <div class="modal-card confirm-card">
      <div class="modal-head">
        <div>
          <span class="eyebrow">{{ store.modals.confirm.danger ? "请确认" : "提示" }}</span>
          <h2>{{ store.modals.confirm.title }}</h2>
        </div>
        <button class="icon-btn modal-close" aria-label="关闭" @click="cancel">×</button>
      </div>
      <div class="modal-body">
        <p class="modal-lead">{{ store.modals.confirm.message }}</p>
        <div v-if="store.modals.confirm.detail" class="restore-warning">
          <span class="alert-icon">!</span>
          <p>{{ store.modals.confirm.detail }}</p>
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
          {{ busy ? "处理中…" : store.modals.confirm.confirmLabel }}
        </button>
      </div>
    </div>
  </div>
</template>
