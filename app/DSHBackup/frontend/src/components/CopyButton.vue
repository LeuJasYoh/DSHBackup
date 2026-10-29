<script setup lang="ts">
import { ref } from "vue";
import { copyText } from "../store";

const props = defineProps<{ value: string; label?: string }>();
const done = ref(false);
let timer: ReturnType<typeof setTimeout> | null = null;

async function copy() {
  await copyText(props.value, "已复制到剪贴板");
  done.value = true;
  if (timer) clearTimeout(timer);
  timer = setTimeout(() => { done.value = false; }, 1500);
}
</script>

<template>
  <button
    class="copy-btn"
    type="button"
    :class="{ done }"
    :title="`复制：${value}`"
    :aria-label="`复制 ${value}`"
    @click.stop="copy"
  >{{ done ? "已复制" : label || "复制" }}</button>
</template>
