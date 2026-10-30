<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useModalFocus } from "../lib/modalFocus";
import { askConfirm, isBusy, pickDirectory, snapshots, startRestore, store } from "../store";
import { errorMessage, formatBytes, formatDate, isAbsoluteDirectory, restoreBlockReason, scopeLabel, statusLabel } from "../lib/format";
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
const locked = computed(() => submitting.value || browsing.value);
const busy = computed(() => isBusy.value);
function close() { if (!locked.value) store.modals.restore = false; }
useModalFocus(card, close);
const selected = computed(() => snapshots.value.find((snapshot) => snapshot.id === selectedId.value));
const defaultTarget = computed(() => store.boot?.dsh.sources.find((source) => source.name === "dsh")?.path ?? "");
const target = computed(() => targetMode.value === "custom" ? customTarget.value.trim() : defaultTarget.value);
const desktopAvailable = computed(() => targetMode.value === "default" && !!selected.value?.desktopIncluded);
const desktopTarget = computed(() => store.boot?.dsh.sources.filter((source) => source.name !== "dsh").map((source) => source.path).join("；") || "系统桌面登录态目录");
const blocked = computed(() => restoreBlockReason(selected.value, store.boot?.key));
const archiveName = computed(() => selected.value?.localPath?.split(/[\\/]/).pop() || (selected.value ? `dsh-backup-${selected.value.id}.7z` : "选择一份归档"));
const targetError = computed(() => {
  if (!isAbsoluteDirectory(target.value)) return "请选择有效的 Windows 绝对目录。";
  if (targetMode.value === "custom" && selected.value?.scope?.length && !selected.value.scope.includes("dsh")) return "该归档不包含 DSH 核心数据，不能恢复到其他目录。";
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
      <div class="modal-head">
        <div>
          <h2 id="restoreTitle">恢复归档</h2>
          <p class="modal-sub">{{ archiveName }}</p>
        </div>
        <button class="icon-btn modal-close" type="button" :disabled="locked" aria-label="关闭" @click="close"><svg class="icon"><use href="#i-x"></use></svg></button>
      </div>
      <div class="modal-body">
        <div class="callout warn" style="margin: 0 0 12px">
          <svg class="icon"><use href="#i-alert"></use></svg>
          <span>恢复会覆盖目标中的现有数据。请先关闭 DSH；{{ store.boot?.dsh.running ? "当前检测到 DSH 正在运行。" : "恢复完成后重新启动 DSH。" }}</span>
        </div>

        <fieldset class="form-fieldset" :disabled="locked || busy">
          <div class="field-block" style="margin-top: 0">
            <label class="field-label" for="restoreSnapshot">来源归档</label>
            <select id="restoreSnapshot" v-model="selectedId" autofocus class="text-input select-input"><option v-for="snapshot in snapshots" :key="snapshot.id" :value="snapshot.id">{{ formatDate(snapshot.createdAt) }} · {{ formatBytes(snapshot.size) }} · {{ statusLabel(snapshot.status) }}</option></select>
            <p v-if="selected" class="hint">{{ selected.encrypted ? "已加密，需使用对应本地私钥。" : "未加密归档。" }}</p>
          </div>

          <div v-if="selected" class="summary-grid">
            <div class="summary-cell"><span class="k">归档大小</span><span class="v">{{ formatBytes(selected.size) }}</span></div>
            <div class="summary-cell"><span class="k">创建时间</span><span class="v">{{ formatDate(selected.createdAt) }}</span></div>
            <div class="summary-cell"><span class="k">备份范围</span><span class="v">{{ scopeLabel(selected) }}</span></div>
            <div class="summary-cell"><span class="k">完整性</span><span class="v" :style="{ color: selected.status === 'ready' ? 'var(--green)' : selected.status === 'size-mismatch' ? 'var(--amber)' : 'var(--red)' }">{{ statusLabel(selected.status) }}</span></div>
          </div>

          <div class="field-block">
            <div class="field-label">恢复方式</div>
            <div class="radio-list" role="radiogroup" aria-label="恢复方式">
              <label class="radio-card" :class="{ 'is-active': targetMode === 'default' }">
                <input v-model="targetMode" type="radio" name="restore-mode" value="default" />
                <span class="radio-dot"></span>
                <span class="radio-copy"><strong>覆盖当前 DSH 数据</strong><span>恢复到当前数据目录；若归档包含桌面登录态，可一并恢复。</span></span>
              </label>
              <label class="radio-card" :class="{ 'is-active': targetMode === 'custom' }">
                <input v-model="targetMode" type="radio" name="restore-mode" value="custom" />
                <span class="radio-dot"></span>
                <span class="radio-copy"><strong>恢复到其他目录</strong><span>不影响当前数据；自定义目录不会包含系统桌面登录态。</span></span>
              </label>
            </div>
            <div v-if="targetMode === 'custom'" class="path-input target-input">
              <input v-model="customTarget" class="text-input mono" placeholder="选择用于接收 DSH 核心数据的目录" aria-label="其他恢复目录" />
              <button class="btn btn-secondary btn-small" type="button" @click="browseTarget">浏览</button>
            </div>
            <p v-if="targetMode === 'custom'" class="hint">目标目录中即将覆盖的文件会被替换；其他文件会保留。</p>
          </div>

          <div class="field-block">
            <div class="field-label">恢复内容</div>
            <div class="check-row"><span class="check-fixed" aria-hidden="true">✓</span><span><strong>DSH 核心数据</strong><small>必选</small></span></div>
            <label class="check-row" :class="{ disabled: !desktopAvailable }"><input v-model="includeDesktop" type="checkbox" :disabled="!desktopAvailable" /><span><strong>桌面登录态</strong><small>{{ targetMode === "custom" ? "恢复到其他目录时禁用，不修改系统桌面登录态。" : selected?.desktopIncluded ? "恢复到系统桌面登录态目录。" : "此备份不包含桌面登录态。" }}</small></span></label>
          </div>

          <label class="check-row"><input v-model="preRestore" type="checkbox" /><span><strong>恢复前先备份即将覆盖的现有数据</strong><small>建议保持开启；创建一份恢复前快照以保护此次覆盖目标。</small></span></label>

          <div class="restore-summary">
            <div class="summary-row"><span>目标目录</span><strong class="mono path-wrap">{{ target || "尚未选择" }}</strong></div>
            <div class="summary-row"><span>影响内容</span><strong>DSH 核心数据{{ desktopAvailable && includeDesktop ? " + 桌面登录态" : "；不修改系统桌面登录态" }}</strong></div>
            <div v-if="desktopAvailable && includeDesktop" class="summary-row"><span>桌面目录</span><strong class="mono path-wrap">{{ desktopTarget }}</strong></div>
            <div class="summary-row"><span>覆盖保护</span><strong>{{ preRestore ? "先备份即将覆盖的现有数据" : "未开启；本次覆盖没有预恢复备份保护" }}</strong></div>
          </div>
        </fieldset>
      </div>
      <div class="modal-bottom">
        <p v-if="busy" class="hint" role="status">有任务正在进行；恢复提交已暂停，仍可关闭此窗口。</p>
        <p v-if="blocked || (targetMode === 'custom' && targetError)" class="inline-error" role="alert">{{ blocked || targetError }}</p>
        <p v-if="failure" class="inline-error" role="alert">{{ failure }}</p>
        <div class="modal-footer">
          <button class="btn btn-ghost" type="button" :disabled="locked" @click="close">取消</button>
          <button class="btn btn-danger" type="button" :disabled="locked || busy || !!blocked || !!targetError" @click="submit">{{ submitting || store.startingTask ? "正在启动…" : "开始恢复" }}</button>
        </div>
      </div>
    </div>
  </div>
</template>
