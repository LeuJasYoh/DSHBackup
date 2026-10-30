<script setup lang="ts">
import { dismissToast, store } from "../store";

function runAction() {
  const action = store.toast.action;
  dismissToast(true);
  action?.run();
}
function dismiss() {
  if (store.toast.action) return;
  dismissToast();
}
</script>

<template>
  <div
    class="toast"
    :class="[store.toast.type, { show: !!store.toast.text }]"
    role="status"
    aria-live="polite"
    @click="dismiss"
  >
    <span class="toast-text">{{ store.toast.text }}</span>
    <button v-if="store.toast.action" class="toast-action" type="button" @click.stop="runAction">{{ store.toast.action.label }}</button>
  </div>
</template>
