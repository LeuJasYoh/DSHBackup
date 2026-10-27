<script setup lang="ts">
import { dismissToast, store } from "../store";
</script>

<template>
  <div
    class="toast"
    :class="[store.toast.type, { show: !!store.toast.text }]"
    role="status"
    aria-live="polite"
    @click="dismissToast"
  >
    {{ store.toast.text }}
  </div>
</template>
