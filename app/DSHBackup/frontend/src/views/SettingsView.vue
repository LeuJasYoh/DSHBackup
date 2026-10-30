<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import OptionSelect from "../components/OptionSelect.vue";
import { api } from "../lib/bridge";
import { cloneConfig, compressionHint, compressionLevels, compressionShort, configFingerprint, errorMessage, extraArgsError, formatBytes, formatDate, parseExtraArgs, scheduleLabel, validateConfig, weekdayLabel } from "../lib/format";
import { applyTheme } from "../lib/theme";
import { checkForUpdates, isBusy, openBackupFolder, openDataFolder, openKeysFolder, openLogsFolder, openReleasePage, openRepository, openTrashFolder, pickDirectory, saveConfig, store, trashBytes, trashCount } from "../store";
import type { Config, SettingsSection, Theme } from "../lib/types";

interface RailItem { id: SettingsSection; label: string; depth?: 2; when?: () => boolean }
/* 目录是一棵树，不是一组分开的列表：depth 2 挂在上一条 depth 1 下，导轨线在行间拐圆角弯。 */
const railItems: RailItem[] = [
  { id: "backup-dir", label: "备份目录" },
  { id: "desktop", label: "包含桌面登录态" },
  { id: "verify", label: "备份后自动校验" },
  { id: "prerestore", label: "恢复前创建快照" },
  { id: "schedule", label: "计划与保留" },
  { id: "schedule-time", label: "执行时间", depth: 2, when: () => draft.value?.schedule.kind !== "manual" },
  { id: "schedule-keep", label: "保留份数", depth: 2 },
  { id: "trash", label: "回收站", depth: 2 },
  { id: "compress", label: "压缩" },
  { id: "compress-advanced", label: "高级参数", depth: 2 },
  { id: "encryption", label: "本地加密" },
  { id: "keys", label: "本地密钥" },
  { id: "diagnostics", label: "诊断" },
  { id: "theme", label: "外观" },
  { id: "update", label: "更新" },
  { id: "runtime", label: "运行环境" },
];

const themeOptions: Array<{ id: Theme; label: string }> = [
  { id: "system", label: "跟随系统" }, { id: "light", label: "亮色" }, { id: "dark", label: "暗色" },
];
const weekdayOptions = Array.from({ length: 7 }, (_, day) => ({ value: day, label: weekdayLabel(day) }));
const dictionaryOptions = [{ value: 0, label: "自动" }, ...[16, 32, 64, 128, 256, 512, 1024].map((size) => ({ value: size, label: `${size} MB` }))];
const scheduleOptions = [
  { id: "manual", label: "手动" }, { id: "daily", label: "每天" }, { id: "weekly", label: "每周" },
] as const;
const closeOptions = [
  { id: "ask", label: "每次询问" }, { id: "exit", label: "直接退出" }, { id: "tray", label: "最小化到托盘" },
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

/* 目录导航：一条连续的树形导轨 + 滑动的当前区段，滚动高亮与点击跳转共用 .content 一个滚动容器 */
const pageEl = ref<HTMLElement | null>(null);
const railEl = ref<HTMLElement | null>(null);
const activeRange = ref<[number, number]>([0, 0]);
const dotLeadTop = ref(false);
const reduceMotion = typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
const ROW = 32;
const RAIL_X = { 1: 8.5, 2: 16.5 } as const;

const visibleItems = computed(() => railItems.filter((item) => !item.when || item.when()));
const railRows = computed(() => visibleItems.value.length * ROW);
const railX = (item: RailItem) => RAIL_X[item.depth === 2 ? 2 : 1];
/* 每两段之间画一条 12px 的三次曲线：同深度时控制点不偏移，看起来就是直线；换深度时自然拐进子列。 */
const railPath = computed(() => {
  const items = visibleItems.value;
  if (!items.length) return "";
  const parts = [`M ${railX(items[0])} 0`];
  for (let i = 0; i < items.length - 1; i += 1) {
    const b = (i + 1) * ROW;
    const a = railX(items[i]);
    const c = railX(items[i + 1]);
    parts.push(`L ${a} ${b - 12} C ${a} ${b - 4} ${c} ${b - 8} ${c} ${b + 12}`);
  }
  parts.push(`L ${railX(items[items.length - 1])} ${items.length * ROW}`);
  return parts.join(" ");
});
const isActive = (index: number) => index >= activeRange.value[0] && index <= activeRange.value[1];
/* 高亮窗口覆盖「当前落在阅读带里的全部标题」，所以短小节会连成一段，而不是一格一格的点。
   上下各让出 6px 行内边距，括号正好框住文字本身，而不是压在行缝上。 */
const trackTop = computed(() => activeRange.value[0] * ROW + 6);
const trackBottom = computed(() => activeRange.value[1] * ROW + ROW - 6);
const markStyle = computed(() => ({
  height: `${railRows.value}px`,
  "--tt": `${trackTop.value}px`,
  "--tb": `${trackBottom.value}px`,
}));
/* 圆点始终走在行进方向的前端：向下滚停在区段下沿，向上滚翻到区段上沿。 */
const dotStyle = computed(() => {
  const items = visibleItems.value;
  const lead = dotLeadTop.value ? items[activeRange.value[0]] : items[activeRange.value[1]];
  const y = dotLeadTop.value ? trackTop.value : trackBottom.value;
  return {
    transform: `translate(${(lead ? railX(lead) : RAIL_X[1]) - 2}px, ${y - 2}px)`,
    opacity: lead ? 1 : 0,
  };
});

const scroller = ref<HTMLElement | null>(null);
const railTop = ref(0);
const railScrolls = ref(false);
let offsets: number[] = [];
let railBox = { client: 0, full: 0 };
let band = 320;

/* 参考页的阅读带是整个视口，因为它每节平均 600px 高；我们每节只有 130px 左右，
   照搬会把半条目录都点亮。所以按「中位节高 × 1.5」换算，让它同样落在 1–2 节、偶尔 3 节。 */
function fitBand(clientHeight: number) {
  const gaps: number[] = [];
  for (let i = 1; i < offsets.length; i += 1) gaps.push(offsets[i] - offsets[i - 1]);
  gaps.sort((a, c) => a - c);
  const median = gaps.length ? gaps[Math.floor(gaps.length / 2)] : 320;
  band = Math.max(160, Math.min(median * 1.5, clientHeight - 32));
}

/** 只在挂载、尺寸变化和目录项增减时量一次，滚动过程中绝不回读布局。 */
function measure() {
  const root = scroller.value;
  const items = visibleItems.value;
  if (!root) return;
  const bar = root.querySelector<HTMLElement>(".topbar");
  railTop.value = bar ? Math.ceil(bar.getBoundingClientRect().height) : 0;
  const base = root.getBoundingClientRect().top - root.scrollTop;
  offsets = items.map((item) => {
    const el = document.getElementById(`sec-${item.id}`);
    return el ? el.getBoundingClientRect().top - base : 0;
  });
  railBox = { client: railEl.value?.clientHeight ?? 0, full: railEl.value?.scrollHeight ?? 0 };
  railScrolls.value = railBox.full > railBox.client + 1;
  fitBand(root.clientHeight);
}

/* 阅读带：从顶栏下沿往下量 band。参考页实测其下边界在 [H-43, H-14) 之间，取 H-32 作上限。 */
function spy() {
  const root = scroller.value;
  const items = visibleItems.value;
  if (!root || !items.length || !offsets.length) return;
  const bandTop = root.scrollTop + railTop.value;
  const bandBottom = Math.min(bandTop + band, root.scrollTop + root.clientHeight - 32);
  let first = -1;
  let last = -1;
  for (let i = 0; i < offsets.length; i += 1) {
    if (offsets[i] >= bandTop && offsets[i] <= bandBottom) {
      if (first < 0) first = i;
      last = i;
    }
  }
  if (first < 0) {
    // 带内一个标题都没有（当前小节特别长）：沿用最后一个越过的，别让导轨整段空掉。
    for (let i = 0; i < offsets.length; i += 1) if (offsets[i] <= bandTop) last = i;
    first = Math.max(0, last);
  }
  // 触底时最后一节的标题可能已经划出阅读带，但它确实是眼前这段内容。
  if (root.scrollTop + root.clientHeight >= root.scrollHeight - 4) last = items.length - 1;
  const previous = activeRange.value;
  // 区间往前挪是向下读，往后缩是向上读；区间没变就沿用上一次的方向。
  dotLeadTop.value = previous[0] > first
    || previous[1] > last
    || (previous[0] === first && previous[1] === last && dotLeadTop.value);
  if (previous[0] !== first || previous[1] !== last) activeRange.value = [first, last];
  keepActiveVisible();
}

/* 反向联动：文档滚到哪，目录就把自己滚到哪，让当前行始终在导轨里居中。 */
function keepActiveVisible() {
  const rail = railEl.value;
  if (!rail || !railScrolls.value) return;
  const target = Math.max(0, Math.min(
    activeRange.value[0] * ROW - (railBox.client - ROW) / 2,
    railBox.full - railBox.client,
  ));
  if (Math.abs(rail.scrollTop - target) > 0.5) rail.scrollTop = target;
}

let frame = 0;
function onScroll() {
  if (frame) return;
  frame = requestAnimationFrame(() => { frame = 0; spy(); });
}

function jumpTo(id: SettingsSection) {
  const root = scroller.value;
  if (!root) return;
  measure();
  const index = visibleItems.value.findIndex((item) => item.id === id);
  if (index < 0) return;
  // 不预设 active：让滚动监听带着指示器一路滑过去，抢先赋值会出现跳到目标再被拽回来的二次抖动。
  root.scrollTo({ top: Math.max(0, offsets[index] - railTop.value - 8), behavior: reduceMotion ? "auto" : "smooth" });
}

let layoutObserver: ResizeObserver | null = null;
onMounted(() => {
  scroller.value = pageEl.value?.closest(".content") as HTMLElement | null;
  scroller.value?.addEventListener("scroll", onScroll, { passive: true });
  if (typeof ResizeObserver === "function" && pageEl.value) {
    layoutObserver = new ResizeObserver(() => { measure(); spy(); });
    layoutObserver.observe(pageEl.value);
    if (scroller.value) layoutObserver.observe(scroller.value);
  }
  measure();
  spy();
});
onUnmounted(() => {
  if (frame) cancelAnimationFrame(frame);
  scroller.value?.removeEventListener("scroll", onScroll);
  layoutObserver?.disconnect();
});
watch(visibleItems, () => { measure(); spy(); });
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
  <section ref="pageEl" class="page settings-page" :class="{ active: store.page === 'settings' }">
    <div class="head">
      <div class="head-main">
        <div class="eyebrow"><i class="pip" :class="{ warn: dirty }"></i>{{ dirty ? "有未保存的修改" : "当前设置已保存" }}</div>
        <h1>设置</h1>
        <p class="sub">所有分组共用一份草稿<span class="sep">·</span>保存后写入 <span class="mono">{{ boot.paths.configDir }}\config.json</span></p>
      </div>
    </div>

    <div v-if="draft" class="settings-body" :style="{ '--rail-top': railTop + 'px' }">
      <div class="settings-rail">
        <nav ref="railEl" class="settings-nav" aria-label="设置目录" :data-scrollable="railScrolls">
          <div class="rail-list">
            <svg class="rail-tree rail-tree-base" :style="{ height: railRows + 'px' }" :viewBox="`0 0 25 ${railRows}`" aria-hidden="true"><path :d="railPath" /></svg>
            <svg class="rail-tree rail-tree-mark" :style="markStyle" :viewBox="`0 0 25 ${railRows}`" aria-hidden="true"><path :d="railPath" /></svg>
            <i class="rail-dot" :style="dotStyle" aria-hidden="true"></i>
            <a
              v-for="(item, index) in visibleItems"
              :key="item.id"
              class="rail-item"
              :href="`#sec-${item.id}`"
              :data-depth="item.depth === 2 ? 2 : 1"
              :data-active="isActive(index)"
              @click.prevent="jumpTo(item.id)"
            >{{ item.label }}</a>
          </div>
        </nav>
      </div>

      <div class="settings-doc">
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
                <OptionSelect v-if="draft.schedule.kind === 'weekly'" class="weekday-select" :model-value="draft.schedule.weekday" :options="weekdayOptions" aria-label="星期" @update:model-value="draft.schedule.weekday = Number($event)" />
              </div>
            </div>
          </section>

          <section id="sec-schedule-keep" class="set-sec">
            <h2>保留份数</h2>
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc">保留最近 N 份普通备份，更旧的普通备份会被移入回收站；恢复前快照不计入自动清理。</div>
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

          <section id="sec-trash" class="set-sec">
            <h2>回收站</h2>
            <div class="setting-row stacked">
              <div class="setting-copy">
                <div class="setting-desc">删除的归档先移到备份目录内的回收站，可以随时找回；超过保留天数后自动彻底删除。当前 {{ trashCount }} 项 · {{ formatBytes(trashBytes) }}。</div>
              </div>
              <div class="setting-control">
                <div class="segmented" role="group" aria-label="回收站保留天数">
                  <button v-for="days in [1, 7, 30, 90]" :key="days" type="button" :class="{ active: draft.retention.trashDays === days }" :aria-pressed="draft.retention.trashDays === days" @click="draft.retention.trashDays = days">{{ days }} 天</button>
                </div>
                <button class="btn btn-ghost btn-small" type="button" @click="openTrashFolder()">打开回收站目录</button>
              </div>
            </div>
          </section>

          <section id="sec-compress" class="set-sec">
            <h2>压缩</h2>
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc">{{ compressionHint(draft.compression.level) }}</div>
              </div>
              <div class="setting-control">
                <div class="segmented compression-levels" role="group" aria-label="压缩等级">
                  <button v-for="level in compressionLevels" :key="level" type="button" :class="{ active: draft.compression.level === level }" :aria-pressed="draft.compression.level === level" @click="draft.compression.level = level">{{ compressionShort(level) }}</button>
                </div>
              </div>
            </div>
          </section>

          <section id="sec-compress-advanced" class="set-sec">
            <button class="disc-head" type="button" :aria-expanded="advancedOpen" aria-controls="advancedFields" @click="advancedOpen = !advancedOpen">
              <svg class="chev" aria-hidden="true"><use href="#i-chevron-right"></use></svg>
              高级参数
            </button>
            <div id="advancedFields" class="disc-body" :class="{ open: advancedOpen }" :inert="!advancedOpen" :aria-hidden="!advancedOpen">
              <div>
                <div class="advanced-grid">
                  <label>字典大小
                    <OptionSelect :model-value="draft.compression.dictionaryMb" :options="dictionaryOptions" aria-label="字典大小" @update:model-value="draft.compression.dictionaryMb = Number($event)" />
                  </label>
                  <label>线程数（0 为自动）<input v-model.number="draft.compression.threads" type="number" min="0" max="64" class="text-input" /></label>
                  <label class="advanced-wide">自定义参数<input v-model="extraArgsText" class="text-input mono" placeholder="例如 -mfb=273 -ms=on" :aria-invalid="!!argsError" aria-describedby="argsHint" /></label>
                </div>
                <p id="argsHint" class="hint" :class="{ error: argsError }">{{ argsError || '仅接受 -md、-mmt、-mfb、-ms、-mqs 白名单参数，不经过 shell；压缩率请用上方档位选择。' }}</p>
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
            <div class="setting-row">
              <div class="setting-copy">
                <div class="setting-desc">点标题栏关闭按钮时的行为。最小化到托盘后应用仍在后台运行，计划备份照常触发；可在托盘菜单里退出。</div>
                <div v-if="!boot.app.trayAvailable" class="hint">当前系统托盘不可用。</div>
              </div>
              <div class="setting-control">
                <div class="segmented" role="group" aria-label="关闭行为">
                  <button v-for="option in closeOptions" :key="option.id" type="button" :class="{ active: draft.closeBehavior === option.id }" :aria-pressed="draft.closeBehavior === option.id" :disabled="option.id === 'tray' && !boot.app.trayAvailable" @click="draft.closeBehavior = option.id">{{ option.label }}</button>
                </div>
              </div>
            </div>
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
