<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { cancelTask, store } from "../store";
import { formatBytes, phaseLabel, taskSteps, taskTitle } from "../lib/format";

const task = computed(() => store.task!);
const steps = computed(() => taskSteps(task.value.kind, store.taskContext));
const percent = computed(() => Math.min(100, Math.max(0, Math.round(task.value.percent))));
const currentIndex = ref(0);

// 阶段只前进不后退：Go 会报一些不属于任何步骤的中间态，
// 直接按名字匹配会让进度条在第 90 多秒突然跳回第一步。
watch(
  [() => task.value?.phase, () => task.value?.percent, steps],
  () => {
    if (!task.value) return;
    const phase = task.value.phase === "cleanup" ? "done" : task.value.phase;
    if (phase === "cancel") {
      currentIndex.value = -1;
      return;
    }
    const index = steps.value.findIndex((step) => step.phase === phase);
    if (index >= 0) {
      currentIndex.value = Math.max(currentIndex.value, index);
    } else if (task.value.percent >= 100) {
      currentIndex.value = steps.value.length - 1;
    }
  },
  { immediate: true },
);

// 每一步的耗时由界面自己计时；后端只给阶段，不给每步用时。
const enteredAt = ref<Record<number, number>>({});
const finishedAt = ref<Record<number, number>>({});
const now = ref(Date.now());
let ticker: ReturnType<typeof setInterval> | null = null;
watch(currentIndex, (index, previous) => {
  const prev = previous ?? -1;
  if (prev >= 0 && prev !== index && enteredAt.value[prev] && !finishedAt.value[prev]) {
    finishedAt.value[prev] = Date.now();
  }
  if (index >= 0 && !enteredAt.value[index]) enteredAt.value[index] = Date.now();
}, { immediate: true });
onMounted(() => { ticker = setInterval(() => { now.value = Date.now(); }, 500); });
onUnmounted(() => { if (ticker) clearInterval(ticker); });

function duration(ms: number): string {
  if (ms < 1000) return "1 秒内";
  const seconds = Math.round(ms / 1000);
  if (seconds < 60) return `${seconds} 秒`;
  return `${Math.floor(seconds / 60)} 分 ${String(seconds % 60).padStart(2, "0")} 秒`;
}
function elapsedFor(index: number): string {
  const start = enteredAt.value[index];
  if (!start) return "";
  const end = finishedAt.value[index] ?? (index === currentIndex.value ? now.value : 0);
  return end ? duration(end - start) : "";
}
const eta = computed(() => {
  if (task.value.percent >= 99.5) return "等待任务确认";
  if (!store.taskStartedAt) return "预计剩余 —";
  const elapsed = (Date.now() - store.taskStartedAt) / 1000;
  if (elapsed < 2 || task.value.percent < 2) return "预计剩余 —";
  const remaining = Math.max(1, Math.round((elapsed / task.value.percent) * (100 - task.value.percent)));
  if (remaining >= 60) return `预计剩余 ${Math.floor(remaining / 60)} 分 ${remaining % 60} 秒`;
  return `预计剩余 ${remaining} 秒`;
});
const canceling = computed(() => store.canceling || task.value.phase === "cancel");
const stepsWithState = computed(() =>
  steps.value.map((step, index) => ({
    ...step,
    state: percent.value >= 100 || index < currentIndex.value ? "done" : index === currentIndex.value ? "current" : "",
    elapsed: elapsedFor(index),
  })),
);
</script>

<template>
  <div class="card progress-card">
    <div class="progress-head">
      <div>
        <div class="eyebrow">
          <span class="dot"></span>
          {{ canceling ? "正在取消" : taskTitle(task.kind) }}
        </div>
        <h2>{{ canceling ? "等待当前步骤停止" : phaseLabel(task.phase) }}</h2>
      </div>
      <strong class="progress-percent">{{ percent }}%</strong>
    </div>
    <div
      class="progress-track"
      role="progressbar"
      :aria-valuenow="percent"
      aria-valuemin="0"
      aria-valuemax="100"
      :aria-label="`${taskTitle(task.kind)}进度`"
    >
      <span :style="{ width: `${percent}%` }"></span>
    </div>
    <div class="progress-meta">
      <span>
        <template v-if="task.bytesTotal > 0">{{ formatBytes(task.bytesDone) }} / {{ formatBytes(task.bytesTotal) }}</template>
        <template v-else>{{ task.message }}</template>
      </span>
      <span aria-live="polite">{{ eta }}</span>
    </div>

    <div class="trace">
      <div
        v-for="(step, index) in stepsWithState"
        :key="step.phase"
        class="tstep"
        :class="[step.state, { open: step.state === 'current' && !!task.message }]"
      >
        <div class="tstep-row">
          <span class="tstep-dot"></span>
          {{ step.label }}
          <span class="tstep-ms">{{ step.elapsed }}</span>
        </div>
        <div class="tstep-body">
          <div>
            <div class="tstep-detail"><span>{{ task.message }}</span></div>
          </div>
        </div>
      </div>
    </div>

    <p v-if="store.taskError" class="inline-error" role="alert">{{ store.taskError }}</p>
    <div class="progress-actions">
      <button class="btn btn-secondary btn-small" :disabled="canceling || store.startingTask" @click="cancelTask">
        {{ canceling ? "正在取消…" : "取消任务" }}
      </button>
    </div>
  </div>
</template>
