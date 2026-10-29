<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { api } from "../lib/bridge";
import { cloneConfig, compressionLabel, configFingerprint, errorMessage, extraArgsError, formatDate, parseExtraArgs, scheduleLabel, validateConfig, weekdayLabel } from "../lib/format";
import { isBusy, openBackupFolder, openDataFolder, openKeysFolder, openLogsFolder, pickDirectory, saveConfig, store } from "../store";
import type { CompressionLevel, Config, SettingsTab } from "../lib/types";

const tabs: Array<{ id: SettingsTab; label: string }> = [
  { id: "backup", label: "备份" }, { id: "security", label: "安全" }, { id: "about", label: "关于" },
];
const boot = computed(() => store.boot!);
const draft = ref<Config | null>(null);
const baseline = ref("");
const extraArgsText = ref("");
const failure = ref("");
const savedNote = ref("");
const externalUpdate = ref(false);
const browsing = ref(false);
const logTail = ref("");
const toolBusy = ref(false);
const toolMessage = ref("");
const toolFailure = ref("");
const locked = computed(() => isBusy.value || browsing.value);
const dirty = computed(() => !!draft.value && configFingerprint(draft.value, extraArgsText.value) !== baseline.value);
const argsError = computed(() => extraArgsError(extraArgsText.value));
const validation = computed(() => draft.value ? validateConfig(draft.value, extraArgsText.value) : "设置尚未加载");
const directoryChanged = computed(() => draft.value?.backupDir !== boot.value.config.backupDir);

function syncDraft(config = store.boot?.config) {
  if (!config) return;
  draft.value = cloneConfig(config);
  extraArgsText.value = config.compression.extraArgs.join(" ");
  baseline.value = configFingerprint(draft.value, extraArgsText.value);
  externalUpdate.value = false;
}
watch(() => store.boot?.config, (config, previous) => {
  if (!config) return;
  if (!draft.value || (!dirty.value && !locked.value)) syncDraft(config);
  else if (previous && configFingerprint(config, config.compression.extraArgs.join(" ")) !== configFingerprint(previous, previous.compression.extraArgs.join(" "))) externalUpdate.value = true;
}, { deep: true, immediate: true });
watch(locked, (value) => { if (!value && !dirty.value) syncDraft(); });
watch(dirty, (value) => {
  store.settingsDirty = value;
  if (!value) store.settingsReminder = "";
  else savedNote.value = "";
}, { immediate: true, flush: "sync" });

function discard() {
  if (locked.value) return;
  syncDraft();
  failure.value = "";
  savedNote.value = "已放弃修改，恢复为已保存的设置。";
}
async function save() {
  if (!draft.value || locked.value || !dirty.value) return;
  failure.value = validation.value;
  if (failure.value) return;
  const config = cloneConfig(draft.value);
  config.backupDir = config.backupDir.trim();
  config.compression.extraArgs = parseExtraArgs(extraArgsText.value);
  config.lastScheduledAt = boot.value.config.lastScheduledAt;
  failure.value = "";
  savedNote.value = "";
  try {
    syncDraft(await saveConfig(config));
    savedNote.value = "设置已保存，所有分类的修改均已生效。";
  } catch (error) { failure.value = errorMessage(error); }
}
async function browseBackupDir() {
  if (!draft.value || locked.value) return;
  browsing.value = true;
  try { const directory = await pickDirectory(draft.value.backupDir); if (directory) draft.value.backupDir = directory; }
  catch (error) { failure.value = errorMessage(error); }
  finally { browsing.value = false; }
}
async function showLogs() {
  if (toolBusy.value) return;
  toolBusy.value = true;
  toolFailure.value = "";
  try { logTail.value = await api.readLogTail(120) || "（日志为空）"; }
  catch (error) { toolFailure.value = errorMessage(error); }
  finally { toolBusy.value = false; }
}
async function exportDiagnostics() {
  if (toolBusy.value) return;
  toolBusy.value = true;
  toolFailure.value = "";
  try { toolMessage.value = `诊断包已导出：${await api.exportDiagnostics()}`; }
  catch (error) { toolFailure.value = errorMessage(error); }
  finally { toolBusy.value = false; }
}
</script>

<template>
  <section class="page settings-page" :class="{ active: store.page === 'settings' }">
    <div class="settings-lead">
      <p>所有分类共用一份草稿，点击底部保存后统一生效。</p>
      <span v-if="dirty" class="dirty-note">有未保存的修改</span>
    </div>
    <div class="settings-tabs" aria-label="设置分类">
      <button v-for="tab in tabs" :key="tab.id" type="button" class="settings-tab" :class="{ active: store.settingsTab === tab.id }" :aria-pressed="store.settingsTab === tab.id" @click="store.settingsTab = tab.id">{{ tab.label }}</button>
    </div>
    <div v-if="draft" class="settings-scroll">
      <p v-if="externalUpdate" class="hint">已保存的配置有更新。你的草稿未被覆盖；放弃修改可载入最新配置，保存则以当前草稿为准。</p>
      <fieldset class="form-fieldset" :disabled="locked">
        <div v-show="store.settingsTab === 'backup'" class="card settings-card">
          <div class="form-row">
            <label class="form-label" for="backupDir">备份目录</label>
            <div class="form-control">
              <div class="path-input">
                <input id="backupDir" v-model="draft.backupDir" class="text-input mono" />
                <button class="btn btn-secondary btn-small" @click="browseBackupDir">{{ browsing ? "选择中…" : "浏览" }}</button>
                <button class="btn btn-ghost btn-small" :disabled="directoryChanged" title="打开已保存的备份目录" @click="openBackupFolder()">打开</button>
              </div>
              <p class="hint">打开的是已保存目录；目录有未保存修改时不可打开。更改目录不会迁移旧备份。</p>
              <p class="hint">默认位于应用目录下，删除应用目录前请移走备份。</p>
            </div>
          </div>
          <div class="form-row">
            <div class="form-label">备份范围</div>
            <div class="form-control">
              <div class="check-row"><span class="check-fixed" aria-hidden="true">&#10003;</span><span><strong>DSH 核心数据（必选）</strong><small>凭据、会话、附件、插件、用量</small></span></div>
              <label class="check-row"><input v-model="draft.includeDesktop" type="checkbox" /><span><strong>包含桌面登录态</strong><small>Cookies、Local Storage、窗口偏好</small></span></label>
              <p class="hint">项目代码、日志和崩溃缓存不在备份范围内。</p>
            </div>
          </div>
          <div class="form-row">
            <div class="form-label">压缩</div>
            <div class="form-control">
              <div class="segmented" role="group" aria-label="压缩等级">
                <button v-for="level in (['fast', 'balanced', 'max'] as CompressionLevel[])" :key="level" :class="{ active: draft.compression.level === level }" :aria-pressed="draft.compression.level === level" @click="draft.compression.level = level">{{ compressionLabel(level) }}</button>
              </div>
              <p class="hint">均衡适合日常备份；极限压缩更慢、占用更多内存。</p>
              <details class="advanced-block">
                <summary>高级压缩参数</summary>
                <div class="advanced-grid">
                  <label>字典大小
                    <select v-model.number="draft.compression.dictionaryMb" class="text-input select-input"><option :value="0">自动</option><option v-for="size in [16, 32, 64, 128, 256, 512, 1024]" :key="size" :value="size">{{ size }} MB</option></select>
                  </label>
                  <label>线程数（0 为自动）<input v-model.number="draft.compression.threads" type="number" min="0" max="64" class="text-input" /></label>
                  <label class="advanced-wide">自定义参数<input v-model="extraArgsText" class="text-input mono" placeholder="例如 -mfb=273 -ms=on" :aria-invalid="!!argsError" aria-describedby="argsHint" /></label>
                </div>
                <p id="argsHint" class="hint" :class="{ error: argsError }">{{ argsError || '仅接受 -mx、-md、-mmt、-mfb、-ms、-mqs 白名单参数，不经过 shell。' }}</p>
              </details>
            </div>
          </div>
          <div class="form-row">
            <div class="form-label">计划备份</div>
            <div class="form-control">
              <div class="inline-options">
                <label class="radio-inline"><input v-model="draft.schedule.kind" type="radio" value="manual" />手动</label>
                <label class="radio-inline"><input v-model="draft.schedule.kind" type="radio" value="daily" />每天</label>
                <label class="radio-inline"><input v-model="draft.schedule.kind" type="radio" value="weekly" />每周</label>
              </div>
              <div v-if="draft.schedule.kind !== 'manual'" class="inline-field schedule-fields">
                <input v-model="draft.schedule.time" class="text-input time-input" type="time" aria-label="计划备份时间" />
                <select v-if="draft.schedule.kind === 'weekly'" v-model.number="draft.schedule.weekday" class="text-input weekday-select" aria-label="星期"><option v-for="day in 7" :key="day" :value="day - 1">{{ weekdayLabel(day - 1) }}</option></select>
              </div>
              <p class="hint">仅在本应用运行时触发；DSH 正在运行时跳过。</p>
              <p class="hint">已生效：{{ scheduleLabel(boot.config.schedule.kind, boot.config.schedule.time, boot.config.schedule.weekday) }} · 下次：{{ formatDate(boot.schedule.nextRun) }} · 上次：{{ formatDate(boot.schedule.lastRun) }}</p>
            </div>
          </div>
          <div class="form-row">
            <label class="form-label" for="retention">保留份数</label>
            <div class="form-control">
              <div class="inline-field"><input id="retention" v-model.number="draft.retention.local" class="text-input compact" type="number" min="1" max="99" />份</div>
              <p class="hint">保留最近 N 份普通备份，更旧的普通备份会被自动删除；恢复前快照不计入自动清理。</p>
            </div>
          </div>
          <div class="form-row">
            <div class="form-label">恢复保护</div>
            <div class="form-control"><label class="check-row"><input v-model="draft.preRestoreSnapshot" type="checkbox" />默认在恢复前备份即将覆盖的现有数据</label><p class="hint">每次恢复时仍可确认是否启用。</p></div>
          </div>
          <div class="form-row">
            <div class="form-label">备份后校验</div>
            <div class="form-control"><label class="check-row"><input v-model="draft.verifyAfterBackup" type="checkbox" />生成归档后运行完整性测试</label><p class="hint">会增加耗时，建议开启。历史列表的“归档可用”不等于已通过完整性校验。</p></div>
          </div>
        </div>
        <div v-show="store.settingsTab === 'security'" class="card settings-card">
          <div class="form-row">
            <div class="form-label">本地加密</div>
            <div class="form-control">
              <label class="switch-row"><span class="switch-copy">加密本地归档</span><span class="switch"><input v-model="draft.encryptionLocal" type="checkbox" /><span class="switch-track"></span></span></label>
              <p class="hint">默认关闭。开启后自动生成本地密钥；只影响之后新建的归档，不会重新加密已有备份。</p>
              <div class="restore-warning"><p>私钥丢失后，加密备份无法恢复。请另存整个 data\keys 目录，并与备份分开保管。</p></div>
            </div>
          </div>
          <div class="form-row">
            <div class="form-label">本地密钥</div>
            <div class="form-control">
              <div class="static-value"><span class="badge" :class="boot.key.exists ? 'success' : 'muted'">{{ boot.key.exists ? '已生成' : '未生成' }}</span><span v-if="boot.key.keyId" class="mono">{{ boot.key.keyId }}</span><button class="btn btn-secondary btn-small" @click="openKeysFolder()">打开密钥目录</button></div>
              <p class="hint mono">{{ boot.paths.keysDir }}</p><p v-if="boot.keyError || boot.key.error" class="hint error">{{ boot.keyError || boot.key.error }}</p>
              <p class="hint">换机时需复制对应密钥目录。应用不能找回丢失的私钥。</p>
            </div>
          </div>
          <div class="form-row"><div class="form-label">加密参数</div><div class="form-control"><div class="static-value"><span class="badge muted">AES-256</span><span class="badge muted">文件头加密</span></div><p class="hint">每个归档使用独立随机密码，由公钥封装、私钥解开。密码不写入日志。</p></div></div>
        </div>
      </fieldset>
      <div v-show="store.settingsTab === 'about'">
        <div class="card about-card"><div class="about-mark"><img src="/icon.svg" alt="" /></div><div><h2>DSH Backup</h2><p class="about-version">v{{ boot.app.version }} · {{ boot.app.platform }} · 便携版</p></div></div>
        <div class="card settings-card">
          <div class="about-row"><span>构建时间</span><strong>{{ formatDate(boot.app.buildTime) }}</strong></div>
          <div class="about-row"><span>应用目录</span><strong class="mono">{{ boot.paths.appDir }}</strong></div>
          <div class="about-row"><span>数据目录</span><strong class="mono">{{ boot.paths.dataDir }}</strong><button class="btn btn-secondary btn-small" @click="openDataFolder()">打开</button></div>
          <div class="about-row"><span>压缩后端</span><strong>{{ boot.app.sevenZip.version || '不可用' }}</strong></div>
          <p v-if="boot.app.sevenZip.error" class="hint error">{{ boot.app.sevenZip.error }}</p>
          <div class="about-row"><span>Rust sidecar</span><strong>{{ boot.app.sidecar.version || '不可用' }}</strong></div>
          <p v-if="boot.app.sidecar.error" class="hint error">{{ boot.app.sidecar.error }}</p>
          <div class="about-row"><span>许可证</span><span>7-Zip LGPL 2.1+ · age MIT/Apache-2.0</span></div>
          <div class="tool-grid"><button class="btn btn-secondary btn-small" :disabled="toolBusy || isBusy" @click="exportDiagnostics">导出诊断包</button><button class="btn btn-secondary btn-small" @click="openLogsFolder()">打开日志目录</button><button class="btn btn-secondary btn-small" :disabled="toolBusy" @click="showLogs">{{ toolBusy ? '处理中…' : '查看最近日志' }}</button></div>
          <p v-if="toolFailure" class="inline-error" role="alert">{{ toolFailure }}</p><p v-if="toolMessage" class="hint path-wrap" role="status">{{ toolMessage }}</p>
          <pre v-if="logTail" class="log-preview">{{ logTail }}</pre>
          <div class="about-notice">诊断包包含配置、快照清单、工具版本与脱敏日志，不包含私钥、归档密码或 DSH 凭据内容。分享前请自行检查。更新便携程序前，请保留 data、备份目录和密钥。</div>
        </div>
      </div>
    </div>
    <div class="settings-save-bar">
      <p v-if="failure" class="inline-error" role="alert">{{ failure }}</p>
      <p v-if="dirty && validation" class="hint error" role="status">{{ validation }}。修正后即可保存。</p>
      <div class="settings-footer">
        <span class="hint save-status" role="status">{{ locked ? '操作进行中，草稿已保留；完成后可继续编辑和保存。' : dirty ? '未保存，切换页面会保留草稿但不会生效。' : savedNote || '当前设置已保存' }}</span>
        <button class="btn btn-ghost" :disabled="locked || !dirty" @click="discard">放弃修改</button>
        <button class="btn btn-primary" :disabled="locked || !dirty || !!validation" @click="save">{{ store.saving ? '保存中…' : '保存设置' }}</button>
      </div>
    </div>
  </section>
</template>
