<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useModalFocus } from "../lib/modalFocus";
import { askConfirm, isBusy, pickDirectory, snapshots, startRestore, store } from "../store";
import { errorMessage, formatBytes, formatDate, isAbsoluteDirectory, restoreBlockReason, statusLabel } from "../lib/format";
import type { RestoreRequest } from "../lib/types";

const selectedId = ref(store.restorePreselect || snapshots.value[0]?.id || "");
const targetMode = ref<"default" | "custom">("default");
const customTarget = ref("");
const preRestore = ref(store.boot?.config.preRestoreSnapshot ?? true);
const includeDesktop = ref(false);
const submitting = ref(false);
const browsing = ref(false);
const failure = ref("");
const card = ref<HTMLElement | null>(null);
const locked = computed(() => submitting.value || browsing.value || isBusy.value);
function close() { if (!locked.value) store.modals.restore = false; }
useModalFocus(card, close);
const selected = computed(() => snapshots.value.find((snapshot) => snapshot.id === selectedId.value));
const defaultTarget = computed(() => store.boot?.dsh.sources.find((source) => source.name === "dsh")?.path ?? "");
const target = computed(() => targetMode.value === "custom" ? customTarget.value.trim() : defaultTarget.value);
const desktopAvailable = computed(() => targetMode.value === "default" && !!selected.value?.desktopIncluded);
const desktopTarget = computed(() => store.boot?.dsh.sources.filter((source) => source.name !== "dsh").map((source) => source.path).join("；") || "系统桌面登录态目录");
const blocked = computed(() => restoreBlockReason(selected.value, store.boot?.key));
const targetError = computed(() => {
  if (!isAbsoluteDirectory(target.value)) return "请选择有效的 Windows 绝对目录。";
  const normalize = (path: string) => path.replaceAll("/", "\\").replace(/\\+$/, "").toLowerCase();
  if (targetMode.value === "custom" && normalize(target.value) === normalize(defaultTarget.value)) return "所选目录是当前数据目录。请选择其他目录，或切换为“覆盖当前数据”。";
  return "";
});
watch(() => selected.value?.id, () => {
  includeDesktop.value = targetMode.value === "default" && !!store.boot?.config.includeDesktop && !!selected.value?.desktopIncluded;
}, { immediate: true });
watch(targetMode, () => { includeDesktop.value = false; });
async function browseTarget() {
  if (locked.value) return;
  browsing.value = true;
  try { const dir = await pickDirectory(customTarget.value || defaultTarget.value); if (dir) customTarget.value = dir; }
  catch (error) { failure.value = errorMessage(error); }
  finally { browsing.value = false; }
}
async function submit() {
  if (locked.value) return;
  failure.value = blocked.value || targetError.value;
  if (failure.value || !selected.value) return;
  submitting.value = true;
  const request: RestoreRequest = {
    snapshotId: selected.value.id,
    includeDesktop: desktopAvailable.value && includeDesktop.value,
    preRestoreSnapshot: preRestore.value,
    customTarget: targetMode.value === "custom" ? target.value : "",
    force: false,
  };
  try { await startRestore(request); }
  catch (error) {
    const message = errorMessage(error);
    if (message.startsWith("DSH_RUNNING:")) {
      failure.value = "DSH 正在运行。建议先关闭 DSH，再重试恢复。";
      askConfirm({
        title: "DSH 正在运行", message: message.slice("DSH_RUNNING:".length),
        detail: "继续恢复可能因文件占用而失败，或被运行中的 DSH 覆盖。建议取消并先关闭 DSH。",
        confirmLabel: "仍然恢复", danger: true,
        onConfirm: () => startRestore({ ...request, force: true }),
      });
    } else failure.value = message;
  } finally { submitting.value = false; }
}
</script>

<template>
  <div class="overlay modal open" @click.self="close">
    <div ref="card" class="modal-card restore-modal" role="dialog" aria-modal="true" aria-labelledby="restoreTitle">
      <div class="modal-head"><div><span class="eyebrow">请确认覆盖范围</span><h2 id="restoreTitle">恢复备份</h2></div><button class="icon-btn modal-close" :disabled="locked" aria-label="关闭" @click="close">×</button></div>
      <div class="modal-body">
        <fieldset class="form-fieldset" :disabled="locked">
          <label class="field-label" for="restoreSnapshot">来源备份</label>
          <select id="restoreSnapshot" v-model="selectedId" autofocus class="text-input select-input"><option v-for="snapshot in snapshots" :key="snapshot.id" :value="snapshot.id">{{ formatDate(snapshot.createdAt) }} · {{ formatBytes(snapshot.size) }} · {{ statusLabel(snapshot.status) }}</option></select>
          <p v-if="selected" class="hint">{{ selected.encrypted ? '已加密，需使用对应本地私钥。' : '未加密归档。' }}</p>
          <div class="field-block">
            <div class="field-label">恢复方式</div>
            <div class="inline-options"><label class="radio-inline"><input v-model="targetMode" type="radio" value="default" />覆盖当前数据</label><label class="radio-inline"><input v-model="targetMode" type="radio" value="custom" />恢复到其他目录</label></div>
            <div v-if="targetMode === 'custom'" class="path-input target-input"><input v-model="customTarget" class="text-input mono" placeholder="选择用于接收 DSH 核心数据的目录" aria-label="其他恢复目录" /><button class="btn btn-secondary btn-small" @click="browseTarget">浏览</button></div>
            <p v-if="targetMode === 'custom'" class="hint">此模式只恢复 DSH 核心数据，不恢复系统桌面登录态；目标目录中即将覆盖的文件会被替换。</p>
          </div>
          <div class="field-block">
            <div class="field-label">恢复内容</div>
            <div class="check-row"><span class="check-fixed" aria-hidden="true">&#10003;</span><span><strong>DSH 核心数据</strong><small>凭据、会话、附件、插件、用量</small></span></div>
            <label class="check-row" :class="{ disabled: !desktopAvailable }"><input v-model="includeDesktop" type="checkbox" :disabled="!desktopAvailable" /><span><strong>桌面登录态</strong><small>{{ targetMode === 'custom' ? '恢复到其他目录时禁用，不修改系统桌面登录态。' : selected?.desktopIncluded ? '恢复到系统桌面登录态目录。' : '此备份不包含桌面登录态。' }}</small></span></label>
          </div>
          <label class="check-row"><input v-model="preRestore" type="checkbox" /><span><strong>恢复前先备份即将覆盖的现有数据</strong><small>建议保持开启；创建一份恢复前快照以保护此次覆盖目标。</small></span></label>
        </fieldset>
        <div class="wizard-summary restore-summary" aria-label="恢复影响总结">
          <div class="summary-row"><span>来源归档</span><strong class="mono path-wrap">{{ selected?.localPath || '归档不可用' }}</strong></div>
          <div class="summary-row"><span>目标目录</span><strong class="mono path-wrap">{{ target || '尚未选择' }}</strong></div>
          <div class="summary-row"><span>影响内容</span><strong>DSH 核心数据{{ desktopAvailable && includeDesktop ? ' + 桌面登录态' : '；不修改系统桌面登录态' }}</strong></div>
          <div v-if="desktopAvailable && includeDesktop" class="summary-row"><span>桌面目录</span><strong class="mono path-wrap">{{ desktopTarget }}</strong></div>
          <div class="summary-row"><span>覆盖保护</span><strong>{{ preRestore ? '先备份即将覆盖的现有数据' : '未开启；本次覆盖没有预恢复备份保护' }}</strong></div>
        </div>
        <div class="restore-warning"><span class="alert-icon">!</span><p>恢复会覆盖上述目标中的现有数据。请先关闭 DSH；恢复完成后重新启动。{{ store.boot?.dsh.running ? '当前检测到 DSH 正在运行。' : '' }}</p></div>
      </div>
      <div class="modal-bottom">
        <p v-if="blocked || (targetMode === 'custom' && targetError)" class="inline-error" role="alert">{{ blocked || targetError }}</p>
        <p v-if="failure" class="inline-error" role="alert">{{ failure }}</p>
        <div class="modal-footer"><button class="btn btn-ghost" :disabled="locked" @click="close">取消</button><button class="btn btn-danger" :disabled="locked || !!blocked || !!targetError" @click="submit">{{ submitting || store.startingTask ? '正在启动…' : '开始恢复' }}</button></div>
      </div>
    </div>
  </div>
</template>
