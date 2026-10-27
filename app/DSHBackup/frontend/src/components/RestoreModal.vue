<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { closeModals, pickDirectory, showToast, snapshots, startRestore, store } from "../store";
import { formatBytes, formatDate, sourceLabel } from "../lib/format";

const selectedId = ref(store.restorePreselect || snapshots.value[0]?.id || "");
const targetMode = ref<"default" | "custom">("default");
const customTarget = ref("");
const preRestore = ref(store.boot?.config.preRestoreSnapshot ?? true);
const includeDesktop = ref(store.boot?.config.includeDesktop ?? true);

const selected = computed(() => snapshots.value.find((snapshot) => snapshot.id === selectedId.value));
const defaultTarget = computed(() => store.boot?.dsh.sources.find((source) => source.name === "dsh")?.path ?? "%USERPROFILE%\\.dsh");
const desktopAvailable = computed(() => !!selected.value?.desktopIncluded);
const dshRunning = computed(() => !!store.boot?.dsh.running);

watch(selected, (snapshot) => {
  if (!snapshot) return;
  includeDesktop.value = !!(store.boot?.config.includeDesktop && snapshot.desktopIncluded);
  if (snapshot.location === "cloud" || !snapshot.localPath) {
    preRestore.value = true;
  }
});

async function browseTarget() {
  const dir = await pickDirectory(customTarget.value || defaultTarget.value);
  if (dir) customTarget.value = dir;
}

async function submit() {
  const snapshot = selected.value;
  if (!snapshot) {
    showToast("请选择要恢复的快照", "warning");
    return;
  }
  if (targetMode.value === "custom" && !customTarget.value.trim()) {
    showToast("请选择自定义恢复目录", "warning");
    return;
  }
  await startRestore(
    snapshot.id,
    snapshot.location,
    includeDesktop.value,
    preRestore.value,
    targetMode.value === "custom" ? customTarget.value.trim() : "",
    false,
  );
}
</script>

<template>
  <div class="overlay modal open" @click.self="closeModals()">
    <div class="modal-card restore-modal">
      <div class="modal-head">
        <div>
          <span class="eyebrow">危险操作</span>
          <h2>恢复备份</h2>
        </div>
        <button class="icon-btn modal-close" aria-label="关闭" @click="closeModals()">×</button>
      </div>
      <div class="modal-body">
        <label class="field-label" for="restoreSnapshot">选择备份</label>
        <select id="restoreSnapshot" v-model="selectedId" class="text-input select-input">
          <option v-for="snapshot in snapshots" :key="snapshot.id" :value="snapshot.id">
            {{ formatDate(snapshot.createdAt) }} · {{ sourceLabel(snapshot.location) }} · {{ formatBytes(snapshot.size) }}
          </option>
        </select>
        <p v-if="selected" class="modal-hint snapshot-meta">
          {{ selected.encrypted ? "归档已加密，将使用本地私钥解密。" : "归档未加密。" }}
          <template v-if="selected.location === 'cloud' || !selected.localPath">需要从 GitHub 下载后恢复。</template>
        </p>

        <div class="field-block">
          <div class="field-label">恢复内容</div>
          <label class="check-row">
            <input type="checkbox" checked aria-disabled="true" tabindex="-1" @click.prevent />
            <span><strong>DSH 核心数据</strong><small>凭据、会话、附件、插件、用量</small></span>
          </label>
          <label class="check-row" :class="{ disabled: !desktopAvailable }">
            <input v-model="includeDesktop" type="checkbox" :disabled="!desktopAvailable" />
            <span><strong>桌面登录态</strong><small>Cookies、Local Storage、窗口偏好</small></span>
          </label>
          <p v-if="!desktopAvailable && selected" class="hint">该快照没有包含桌面登录态。</p>
        </div>

        <div class="field-block">
          <div class="field-label">恢复到</div>
          <div class="inline-options">
            <label class="radio-inline"><input v-model="targetMode" type="radio" value="default" /> 默认位置</label>
            <label class="radio-inline"><input v-model="targetMode" type="radio" value="custom" /> 自定义目录</label>
          </div>
          <div v-if="targetMode === 'default'" class="static-value target-hint">
            <span class="mono">{{ defaultTarget }}</span>
          </div>
          <div v-else class="path-input target-input">
            <input v-model="customTarget" class="text-input mono" placeholder="选择要恢复到的 .dsh 目录" />
            <button class="btn btn-secondary btn-small" @click="browseTarget">浏览</button>
          </div>
          <p v-if="targetMode === 'custom'" class="hint">桌面登录态仍恢复到系统默认位置；自定义目录只影响 .dsh 核心数据。</p>
        </div>

        <label class="check-row">
          <input v-model="preRestore" type="checkbox" />
          <span><strong>恢复前先备份当前数据</strong><small>推荐保持开启；会先创建一份“恢复前快照”。</small></span>
        </label>

        <div class="restore-warning">
          <span class="alert-icon">!</span>
          <p>
            这会覆盖现有 .dsh 和可选的桌面登录态。请先关闭 DSH；恢复完成后需要重新启动 DSH。
            <template v-if="dshRunning"> 当前检测到 DSH 正在运行。</template>
          </p>
        </div>
      </div>
      <div class="modal-footer">
        <button class="btn btn-ghost" @click="closeModals()">取消</button>
        <button class="btn btn-danger" :disabled="!selected || !!store.task" @click="submit">
          {{ store.task ? "任务进行中…" : "开始恢复" }}
        </button>
      </div>
    </div>
  </div>
</template>
