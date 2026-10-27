<script setup lang="ts">
import { computed } from "vue";
import { cancelTask, cloudEnabled, store } from "../store";
import { formatBytes, phaseLabel, taskSteps, taskTitle } from "../lib/format";

const task = computed(() => store.task!);
const steps = computed(() => taskSteps(task.value.kind, cloudEnabled.value, store.boot?.config.mode !== "cloud"));
const percent = computed(() => Math.min(100, Math.max(0, Math.round(task.value.percent))));
const currentIndex = computed(() => {
  const aliases: Record<string, string> = { encrypt: "compress", cleanup: "done" };
  const phase = aliases[task.value.phase] ?? task.value.phase;
  if (phase === "cancel") return -1;
  const index = steps.value.findIndex((step) => step.phase === phase);
  if (index >= 0) return index;
  if (task.value.percent >= 100) return steps.value.length - 1;
  return 0;
});
const eta = computed(() => {
  if (task.value.percent >= 99.5) return "完成";
  if (!store.taskStartedAt) return "预计剩余 —";
  const elapsed = (Date.now() - store.taskStartedAt) / 1000;
  if (elapsed < 2 || task.value.percent < 2) return "预计剩余 —";
  const remaining = Math.max(1, Math.round((elapsed / task.value.percent) * (100 - task.value.percent)));
  if (remaining >= 60) return `预计剩余 ${Math.floor(remaining / 60)} 分 ${remaining % 60} 秒`;
  return `预计剩余 ${remaining} 秒`;
});
const canceling = computed(() => task.value.phase === "cancel");
const stepsWithState = computed(() =>
  steps.value.map((step, index) => ({
    ...step,
    state: percent.value >= 100 || index < currentIndex.value ? "done" : index === currentIndex.value ? "current" : "",
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
    <div class="progress-track"><span :style="{ width: `${percent}%` }"></span></div>
    <div class="progress-meta">
      <span>
        <template v-if="task.bytesTotal > 0">
          {{ formatBytes(task.bytesDone) }} / {{ formatBytes(task.bytesTotal) }}
        </template>
        <template v-else>{{ task.message }}</template>
      </span>
      <span>{{ eta }}</span>
    </div>
    <div class="step-row">
      <span v-for="step in stepsWithState" :key="step.phase" class="progress-step" :class="step.state">
        <span class="step-dot">{{ step.state === "done" ? "✓" : step.state === "current" ? "·" : "" }}</span>
        {{ step.label }}
      </span>
    </div>
    <p v-if="task.message" class="progress-message">{{ task.message }}</p>
    <div class="progress-actions">
      <button class="btn btn-secondary" :disabled="canceling" @click="cancelTask">
        {{ canceling ? "正在取消…" : "取消" }}
      </button>
    </div>
  </div>
</template>
