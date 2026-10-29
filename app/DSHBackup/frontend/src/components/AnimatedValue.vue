<script setup lang="ts">
import { ref, watch } from "vue";

const props = defineProps<{ value: string; unit?: string }>();
const el = ref<HTMLElement | null>(null);
const shown = ref(props.value);
const reduce = typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
let token = 0;

watch(() => props.value, async (next) => {
  if (next === shown.value) return;
  const node = el.value;
  if (!node || reduce || typeof node.animate !== "function") {
    shown.value = next;
    return;
  }
  const mine = ++token;
  node.getAnimations().forEach((animation) => animation.cancel());
  const out = node.animate(
    [{ opacity: 1, transform: "translateY(0)" }, { opacity: 0, transform: "translateY(-4px)" }],
    { duration: 130, easing: "ease", fill: "forwards" },
  );
  await out.finished.catch(() => {});
  if (mine !== token) return;
  shown.value = next;
  const back = node.animate(
    [{ opacity: 0, transform: "translateY(4px)" }, { opacity: 1, transform: "translateY(0)" }],
    { duration: 170, easing: "cubic-bezier(.22,.61,.36,1)" },
  );
  back.finished.then(() => out.cancel()).catch(() => {});
});
</script>

<template>
  <span ref="el" class="anim-value">{{ shown }}<em v-if="unit">{{ unit }}</em></span>
</template>
