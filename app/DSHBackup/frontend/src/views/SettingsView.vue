<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import { api } from "../lib/bridge";
import { cloneConfig, compressionShort, configFingerprint, errorMessage, extraArgsError, formatDate, parseExtraArgs, scheduleLabel, validateConfig, weekdayLabel } from "../lib/format";
import { applyTheme } from "../lib/theme";
import { checkForUpdates, isBusy, openBackupFolder, openDataFolder, openKeysFolder, openLogsFolder, openReleasePage, openRepository, pickDirectory, saveConfig, store } from "../store";
import type { CompressionLevel, Config, SettingsSection, Theme } from "../lib/types";

interface RailItem { id: SettingsSection; label: string; child?: boolean; when?: () => boolean }
const railGroups: RailItem[][] = [
  [
    { id: "backup-dir", label: "备份目录" },
    { id: "desktop", label: "包含桌面登录态" },
    { id: "verify", label: "备份后自动校验" },
    { id: "prerestore", label: "恢复前创建快照" },
  ],
  [
    { id: "schedule", label: "计划与保留" },
    { id: "schedule-time", label: "执行时间", child: true, when: () => draft.value?.schedule.kind !== "manual" },
    { id: "schedule-keep", label: "保留份数", child: true },
  ],
  [
    { id: "compress", label: "压缩" },
    { id: "compress-advanced", label: "高级参数", child: true },
  ],
  [
    { id: "encryption", label: "本地加密" },
    { id: "keys", label: "本地密钥" },
    { id: "diagnostics", label: "诊断" },
  ],
  [
    { id: "theme", label: "外观" },
    { id: "update", label: "更新" },
    { id: "runtime", label: "运行环境" },
  ],
];
const visibleGroups = computed(() => railGroups
  .map((group) => group.filter((item) => !item.when || item.when()))
  .filter((group) => group.length > 0));

const themeOptions: Array<{ id: Theme; label: string }> = [
  { id: "system", label: "跟随系统" }, { id: "light", label: "亮色" }, { id: "dark", label: "暗色" },
];
const scheduleOptions = [
  { id: "manual", label: "手动" }, { id: "daily", label: "每天" }, { id: "weekly", label: "每周" },
] as const;

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
const advancedOpen = ref(false);
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
  applyTheme(config.theme, true);
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
watch(() => draft.value?.theme, (theme) => { if (theme) applyTheme(theme); });
watch(() => store.page, (page) => {
  if (!draft.value) return;
  // 主题只做草稿内预览；离开设置页时回到已保存外观，避免“未生效”的草稿在全局生效。
  if (page === "settings") { applyTheme(draft.value.theme); void nextTick(() => jumpTo(store.settingsSection)); }
  else applyTheme(boot.value.config.theme, true);
});

/* 目录导航：滚动高亮 + 点击跳转 */
const docEl = ref<HTMLElement | null>(null);
const activeSection = ref<SettingsSection>("backup-dir");
const reduceMotion = typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;

function sectionEl(id: SettingsSection): HTMLElement | null {
  return docEl.value?.querySelector<HTMLElement>(`#sec-${id}`) ?? null;
}
function offsetInDoc(el: HTMLElement): number {
  const root = docEl.value!;
  return el.getBoundingClientRect().top - root.getBoundingClientRect().top + root.scrollTop;
}
function onDocScroll() {
  if (!docEl.value) return;
  const root = docEl.value;
  const top = root.scrollTop;
  const items = visibleGroups.value.flat();
  // 最后一段永远滚不到顶部，滚到底时直接把它标为当前。
  if (top + root.clientHeight >= root.scrollHeight - 4 && items.length) {
    activeSection.value = items[items.length - 1].id;
    return;
  }
  let best: SettingsSection = "backup-dir";
  for (const item of items) {
    const el = sectionEl(item.id);
    if (el && offsetInDoc(el) - top <= 24) best = item.id;
  }
  activeSection.value = best;
}
function jumpTo(id: SettingsSection) {
  const el = sectionEl(id);
  if (!el || !docEl.value) return;
  docEl.value.scrollTo({ top: Math.max(0, offsetInDoc(el) - 4), behavior: reduceMotion ? "auto" : "smooth" });
  activeSection.value = id;
}
let docObserver: ResizeObserver | null = null;
onMounted(() => {
  if (typeof ResizeObserver !== "function" || !docEl.value) return;
  docObserver = new ResizeObserver(() => onDocScroll());
  docObserver.observe(docEl.value);
});
onUnmounted(() => docObserver?.disconnect());
watch(() => store.settingsSection, (id) => { if (store.page === "settings") void nextTick(() => jumpTo(id)); });

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
function setRetention(delta: number) {
  if (!draft.value) return;
  draft.value.retention.local = Math.min(99, Math.max(1, draft.value.retention.local + delta));
}
</script>

<template>
  <section class="page settings-page" :class="{ active: store.page === 'settings' }">
    <div class="head">
      <div class="head-main">
        <div class="eyebrow"><i class="pip" :class="{ warn: dirty }"></i>{{ dirty ? "有未保存的修改" : "当前设置已保存" }}</div>
        <h1>设置</h1>
        <p class="sub">所有分组共用一份草稿<span class="sep">·</span>保存后写入 <span class="mono">{{ boot.paths.configDir }}\config.json</span></p>
      </div>
    </div>

    <div v-if="draft" class="settings-body">
      <div class="settings-rail">
        <nav class="settings-nav" aria-label="设置目录">
          <div class="rail-title">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true"><path d="M21 5H3M15 12H3M17 19H3"/></svg>
            <span>目录</span>
          </div>
          <div v-for="(group, index) in visibleGroups" :key="index" class="rail-group">
            <a
              v-for="item in group"
              :key="item.id"
              :href="`#sec-${item.id}`"
              :class="{ child: item.child }"
              :data-active="activeSection === item.id"
              @click.prevent="jumpTo(item.id)"
            >{{ item.label }}</a>
          </div>
        </nav>
      </div>

      <div ref="docEl" class="settings-doc" @scroll.passive="onDocScroll">
        <div v-if="externalUpdate" class="callout settings-callout">
          <svg class="icon"><use href="#i-alert"></use></svg>
          <span>已保存的配置有更新。你的草稿未被覆盖；放弃修改可载入最新配置，保存则以当前草稿为准。</span>
        </div>
        <fieldset class="form-fieldset" :disabled="locked">
          <section id="sec-backup-dir" class="set-sec">
            <h2>备份目录</h2>
            <div class="setting-row stacked">
              <div class="setting-copy">
                <div class="setting-desc">删除应用目录前请先移走备份。打开按钮使用已保存目录；更改目录不会迁移旧备份。</div>
              </div>
              <div class="setting-control path-control">
                <input id="backupDir" v-model="draft.backupDir" class="text-input mono" aria-label="备份目录" />
                <button class="btn btn-secondary btn-small" type="button" @click="browseBackupDir">{{ browsing ? "选择中…" : "浏览" }}</button>
                <button class="btn btn-ghost btn-small" type="button" :disabled="directoryChanged" title="打开已保存的备份目录" @click="openBackupFolder()">打开</button>
              </div>
            </div>
          </section>

          <section id="sec-desktop" class="set-sec">
            <h2>包含桌面登录态</h2>
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc">Cookies、Local Storage 和窗口偏好；建议只在同机恢复时开启。</div>
              </div>
              <div class="setting-control">
                <label class="switch" aria-label="包含桌面登录状态"><input v-model="draft.includeDesktop" type="checkbox" /><span class="switch-track"></span></label>
              </div>
            </div>
          </section>

          <section id="sec-verify" class="set-sec">
            <h2>备份后自动校验</h2>
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc">生成归档后运行完整性测试；会增加耗时，建议开启。</div>
              </div>
              <div class="setting-control">
                <label class="switch" aria-label="备份后自动校验"><input v-model="draft.verifyAfterBackup" type="checkbox" /><span class="switch-track"></span></label>
              </div>
            </div>
          </section>

          <section id="sec-prerestore" class="set-sec">
            <h2>恢复前创建快照</h2>
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc">恢复前先保护即将被覆盖的数据，避免误操作无法回退。</div>
              </div>
              <div class="setting-control">
                <label class="switch" aria-label="恢复前自动快照"><input v-model="draft.preRestoreSnapshot" type="checkbox" /><span class="switch-track"></span></label>
              </div>
            </div>
          </section>

          <section id="sec-schedule" class="set-sec">
            <h2>计划与保留</h2>
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc">仅在本应用运行时触发；DSH 正在运行时跳过。已生效：{{ scheduleLabel(boot.config.schedule.kind, boot.config.schedule.time, boot.config.schedule.weekday) }} · 下次：{{ boot.schedule.nextRun ? formatDate(boot.schedule.nextRun) : "未启用" }}。</div>
              </div>
              <div class="setting-control">
                <div class="segmented" role="group" aria-label="计划备份">
                  <button v-for="option in scheduleOptions" :key="option.id" type="button" :class="{ active: draft.schedule.kind === option.id }" :aria-pressed="draft.schedule.kind === option.id" @click="draft.schedule.kind = option.id">{{ option.label }}</button>
                </div>
              </div>
            </div>
          </section>

          <section v-if="draft.schedule.kind !== 'manual'" id="sec-schedule-time" class="set-sec">
            <h2>执行时间</h2>
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc">每天或每周在指定时间检查一次；上次：{{ formatDate(boot.schedule.lastRun) }}。</div>
              </div>
              <div class="setting-control">
                <input v-model="draft.schedule.time" class="text-input time-input" type="time" aria-label="计划备份时间" />
                <select v-if="draft.schedule.kind === 'weekly'" v-model.number="draft.schedule.weekday" class="text-input weekday-select" aria-label="星期"><option v-for="day in 7" :key="day" :value="day - 1">{{ weekdayLabel(day - 1) }}</option></select>
              </div>
            </div>
          </section>

          <section id="sec-schedule-keep" class="set-sec">
            <h2>保留份数</h2>
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc">保留最近 N 份普通备份，更旧的普通备份会被自动删除；恢复前快照不计入自动清理。</div>
              </div>
              <div class="setting-control">
                <div class="stepper">
                  <button type="button" aria-label="减少保留份数" @click="setRetention(-1)">−</button>
                  <input v-model.number="draft.retention.local" type="number" min="1" max="99" aria-label="保留份数" />
                  <button type="button" aria-label="增加保留份数" @click="setRetention(1)">+</button>
                </div>
              </div>
            </div>
          </section>

          <section id="sec-compress" class="set-sec">
            <h2>压缩</h2>
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc">均衡适合日常备份；极限压缩更慢、占用更多内存。</div>
              </div>
              <div class="setting-control">
                <div class="segmented" role="group" aria-label="压缩等级">
                  <button v-for="level in (['fast', 'balanced', 'max'] as CompressionLevel[])" :key="level" type="button" :class="{ active: draft.compression.level === level }" :aria-pressed="draft.compression.level === level" @click="draft.compression.level = level">{{ compressionShort(level) }}</button>
                </div>
              </div>
            </div>
          </section>

          <section id="sec-compress-advanced" class="set-sec">
            <button class="disc-head" type="button" :aria-expanded="advancedOpen" aria-controls="advancedFields" @click="advancedOpen = !advancedOpen">
              <svg class="chev" aria-hidden="true"><use href="#i-chevron-right"></use></svg>
              高级参数
            </button>
            <div id="advancedFields" class="disc-body" :class="{ open: advancedOpen }">
              <div>
                <div class="advanced-grid">
                  <label>字典大小
                    <select v-model.number="draft.compression.dictionaryMb" class="text-input select-input"><option :value="0">自动</option><option v-for="size in [16, 32, 64, 128, 256, 512, 1024]" :key="size" :value="size">{{ size }} MB</option></select>
                  </label>
                  <label>线程数（0 为自动）<input v-model.number="draft.compression.threads" type="number" min="0" max="64" class="text-input" /></label>
                  <label class="advanced-wide">自定义参数<input v-model="extraArgsText" class="text-input mono" placeholder="例如 -mfb=273 -ms=on" :aria-invalid="!!argsError" aria-describedby="argsHint" /></label>
                </div>
                <p id="argsHint" class="hint" :class="{ error: argsError }">{{ argsError || '仅接受 -mx、-md、-mmt、-mfb、-ms、-mqs 白名单参数，不经过 shell。' }}</p>
              </div>
            </div>
          </section>

          <section id="sec-encryption" class="set-sec">
            <h2>本地加密</h2>
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc">默认关闭。开启后自动生成本地密钥；只影响之后新建的归档，不会重新加密已有备份。</div>
              </div>
              <div class="setting-control">
                <label class="switch" aria-label="加密本地归档"><input v-model="draft.encryptionLocal" type="checkbox" /><span class="switch-track"></span></label>
              </div>
            </div>
            <div class="callout warn">
              <svg class="icon"><use href="#i-alert"></use></svg>
              <span>密码由 data\keys 中的 age 私钥封装。私钥丢失后，已加密归档将无法恢复；换机使用请一并复制密钥目录。</span>
            </div>
          </section>

          <section id="sec-keys" class="set-sec">
            <h2>本地密钥</h2>
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc">{{ boot.key.exists ? "age X25519 · 已生成" : "尚未生成；开启加密时自动创建。" }}</div>
                <p v-if="boot.key.exists" class="hint mono path-wrap">{{ boot.key.keyId || boot.paths.keysDir }}</p>
                <p v-if="boot.keyError || boot.key.error" class="hint error">{{ boot.keyError || boot.key.error }}</p>
              </div>
              <div class="setting-control">
                <span v-if="boot.key.exists" class="inline-pill"><svg class="icon"><use href="#i-check-circle"></use></svg>正常</span>
                <span v-else class="status-pill"><i></i>未生成</span>
                <button class="btn btn-secondary btn-small" type="button" @click="openKeysFolder()">打开目录</button>
              </div>
            </div>
            <div class="security-steps">
              <div class="sec-step"><span>01</span><b>随机密码</b><p>每份新归档生成独立密码。</p></div>
              <div class="sec-step"><span>02</span><b>7z 加密</b><p>AES-256 与文件头加密。</p></div>
              <div class="sec-step"><span>03</span><b>私钥封装</b><p>age X25519 封装随机密码。</p></div>
            </div>
          </section>

          <section id="sec-diagnostics" class="set-sec">
            <h2>诊断</h2>
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc">包含版本、配置状态与脱敏日志；不会写入私钥、归档密码或访问令牌。</div>
              </div>
              <div class="setting-control">
                <button class="btn btn-secondary btn-small" type="button" :disabled="toolBusy || isBusy" @click="exportDiagnostics"><svg class="icon"><use href="#i-export"></use></svg>导出诊断</button>
              </div>
            </div>
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc path-wrap">{{ boot.paths.logsDir }}</div>
              </div>
              <div class="setting-control">
                <button class="btn btn-secondary btn-small" type="button" @click="openLogsFolder()"><svg class="icon"><use href="#i-folder"></use></svg>打开目录</button>
                <button class="btn btn-secondary btn-small" type="button" :disabled="toolBusy" @click="showLogs">{{ toolBusy ? "处理中…" : "查看最近日志" }}</button>
              </div>
            </div>
            <p v-if="toolFailure" class="inline-error" role="alert">{{ toolFailure }}</p>
            <p v-if="toolMessage" class="hint path-wrap" role="status">{{ toolMessage }}</p>
            <pre v-if="logTail" class="log-preview">{{ logTail }}</pre>
          </section>

          <section id="sec-theme" class="set-sec">
            <h2>外观</h2>
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc">默认跟随系统；切换后会立即预览，点击底部保存后写入本机配置。</div>
              </div>
              <div class="setting-control">
                <div class="segmented" role="group" aria-label="界面主题">
                  <button v-for="option in themeOptions" :key="option.id" type="button" :class="{ active: draft.theme === option.id }" :aria-pressed="draft.theme === option.id" @click="draft.theme = option.id">{{ option.label }}</button>
                </div>
              </div>
            </div>
          </section>

          <section id="sec-update" class="set-sec">
            <h2>更新</h2>
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc">启动后异步检查一次；不上传备份、密钥或个人信息，可随时关闭。</div>
              </div>
              <div class="setting-control">
                <label class="switch" aria-label="启动时检查更新"><input v-model="draft.updateCheck" type="checkbox" /><span class="switch-track"></span></label>
              </div>
            </div>
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc">当前 v{{ boot.app.version }}<template v-if="store.update?.latestVersion"> · 最新 v{{ store.update.latestVersion }}</template><template v-else-if="store.update?.error"> · 检查失败</template><template v-else> · 尚未检查</template></div>
                <p v-if="store.update?.error" class="hint error">{{ store.update.error }}</p>
                <p v-else-if="store.update?.available" class="hint" style="color: var(--accent-text)">发现新版本，建议查看发行说明。</p>
                <p v-else-if="store.update" class="hint">当前已是最新版本。</p>
              </div>
              <div class="setting-control">
                <button class="btn btn-secondary btn-small" type="button" :disabled="store.updateChecking" @click="checkForUpdates(true)">{{ store.updateChecking ? "检查中…" : "立即检查" }}</button>
                <button v-if="store.update?.available" class="btn btn-primary btn-small" type="button" @click="openReleasePage()">查看发行版</button>
                <button class="btn btn-ghost btn-small" type="button" @click="openRepository()"><svg class="icon github-icon"><use href="#i-github"></use></svg>仓库</button>
              </div>
            </div>
          </section>

          <section id="sec-runtime" class="set-sec">
            <h2>运行环境</h2>
            <p class="set-lede">{{ boot.app.version }} · Windows x64 · 7-Zip 26.05 · 本地优先</p>
            <div class="rows about-rows">
              <div class="rowline">
                <div class="row-main"><div class="row-title">构建时间</div></div>
                <span class="row-meta">{{ formatDate(boot.app.buildTime) }}</span>
              </div>
              <div class="rowline">
                <div class="row-main"><div class="row-title">应用目录</div><div class="row-sub mono path-wrap">{{ boot.paths.appDir }}</div></div>
              </div>
              <div class="rowline">
                <div class="row-main"><div class="row-title">数据目录</div><div class="row-sub mono path-wrap">{{ boot.paths.dataDir }}</div></div>
                <button class="text-button" type="button" @click="openDataFolder()">打开<svg class="icon" style="width: 12px; height: 12px"><use href="#i-external"></use></svg></button>
              </div>
              <div class="rowline">
                <div class="row-main"><div class="row-title">压缩后端</div><div class="row-sub">{{ boot.app.sevenZip.version || "不可用" }}</div></div>
              </div>
              <div class="rowline">
                <div class="row-main"><div class="row-title">Rust sidecar</div><div class="row-sub">{{ boot.app.sidecar.version || "不可用" }}</div></div>
              </div>
              <div class="rowline">
                <div class="row-main"><div class="row-title">第三方声明</div><div class="row-sub">7-Zip LGPL 2.1+ · age MIT/Apache-2.0</div></div>
              </div>
            </div>
            <p v-if="boot.app.sevenZip.error" class="hint error">{{ boot.app.sevenZip.error }}</p>
            <p v-if="boot.app.sidecar.error" class="hint error">{{ boot.app.sidecar.error }}</p>
            <p class="about-notice">更新便携程序前，请保留 data、备份目录和密钥。诊断包包含配置、快照清单、工具版本与脱敏日志，不包含私钥、归档密码或 DSH 凭据内容；分享前请自行检查。</p>
          </section>
        </fieldset>
      </div>
    </div>

    <div class="settings-save-bar">
      <p v-if="failure" class="inline-error" role="alert">{{ failure }}</p>
      <p v-if="dirty && validation" class="hint error" role="status">{{ validation }}。修正后即可保存。</p>
      <div class="settings-footer">
        <span class="hint save-status" role="status">{{ locked ? "操作进行中，草稿已保留；完成后可继续编辑和保存。" : dirty ? "未保存，切换页面会保留草稿但不会生效。" : savedNote || "当前设置已保存" }}</span>
        <button class="btn btn-ghost" type="button" :disabled="locked || !dirty" @click="discard">放弃修改</button>
        <button class="btn btn-primary" type="button" :disabled="locked || !dirty || !!validation" @click="save">{{ store.saving ? "保存中…" : "保存设置" }}</button>
      </div>
    </div>
  </section>
</template>
