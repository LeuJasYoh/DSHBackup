<script setup lang="ts">
import { computed, ref } from "vue";
import ProgressCard from "../components/ProgressCard.vue";
import SnapshotTable from "../components/SnapshotTable.vue";
import {
  checkDSH, dismissNotice, dismissTaskResult, go, isBusy, latestSnapshot, localUsage,
  openLocalSnapshot, openRestore, openSettingsTab, snapshots,
  startVerify, store,
} from "../store";
import { formatBytes, formatRelative, restoreBlockReason, scheduleLabel, statusLabel } from "../lib/format";

const showAllWarnings = ref(false);
const boot = computed(() => store.boot!);
const config = computed(() => boot.value.config);
const latest = latestSnapshot;
const recent = computed(() => snapshots.value.slice(0, 4));
const result = computed(() => store.taskResult);
const readyCount = computed(() => snapshots.value.filter((item) => item.status === "ready").length);
const issueCount = computed(() => snapshots.value.length - readyCount.value);
const sourceDsh = computed(() => boot.value.dsh.sources.find((source) => source.name === "dsh"));
const sourceDesktop = computed(() => boot.value.dsh.sources.find((source) => source.name !== "dsh"));
const latestBlocked = computed(() => latest.value ? restoreBlockReason(latest.value, boot.value.key) : "");
const nextNote = computed(() => {
  if (config.value.schedule.kind === "manual") return `手动触发 · 保留 ${config.value.retention.local} 份`;
  return `${scheduleLabel(config.value.schedule.kind, config.value.schedule.time, config.value.schedule.weekday)} · 保留 ${config.value.retention.local} 份`;
});
const nextValue = computed(() => config.value.schedule.kind === "manual" ? "手动" : config.value.schedule.time);
const resultNextStep = computed(() => {
  if (result.value?.kind === "restore") return "请先检查恢复目标和日志，确认数据状态后，再从归档历史重新发起恢复。不要在未检查的情况下反复覆盖。";
  if (result.value?.kind === "verify") return "请检查归档文件、对应私钥及日志。找回完整归档后，可在归档历史重新校验。";
  return "请检查备份目录权限、剩余空间及日志，关闭 DSH 后再点击“立即备份”重试。";
});

function dayKey(date: Date): string {
  return `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`;
}
const weekly = computed(() => {
  const now = new Date();
  const days = Array.from({ length: 7 }, (_, index) => {
    const date = new Date(now.getFullYear(), now.getMonth(), now.getDate() - (6 - index));
    return { key: dayKey(date), label: String(date.getDate()), size: 0, today: index === 6, date };
  });
  const byKey = new Map(days.map((day) => [day.key, day]));
  for (const snapshot of snapshots.value) {
    const date = new Date(snapshot.createdAt);
    if (Number.isNaN(date.getTime())) continue;
    const day = byKey.get(dayKey(date));
    if (day) day.size += snapshot.size;
  }
  const max = Math.max(...days.map((day) => day.size), 1);
  return days.map((day) => ({
    ...day,
    height: `${day.size ? Math.max(8, Math.round(day.size / max * 100)) : 3}%`,
    title: `${day.date.getMonth() + 1}-${day.label} · ${formatBytes(day.size)}`,
  }));
});
</script>

<template>
  <section class="page dashboard-page" :class="{ active: store.page === 'dashboard' }">
    <div v-if="store.startingTask" class="card notice-card" role="status"><span class="spinner"></span>正在检查并启动任务，请勿重复操作…</div>
    <ProgressCard v-if="store.task" :key="store.task.taskId" />

    <div v-if="result && !store.taskResultDismissed" class="card task-result-card" :class="result.success ? 'success' : 'error'" role="status">
      <div class="task-result-head">
        <div><div class="eyebrow">{{ result.success ? "任务完成" : "任务未完成" }}</div><strong>{{ result.success ? result.message : result.error || result.message }}</strong></div>
        <button class="icon-btn" type="button" aria-label="关闭结果摘要" @click="dismissTaskResult"><svg class="icon"><use href="#i-x"></use></svg></button>
      </div>
      <template v-if="result.success">
        <p v-if="result.kind === 'backup'" class="result-detail path-wrap">备份路径：{{ result.snapshot?.localPath || store.taskContext?.target || "请在归档历史查看" }}</p>
        <p v-else-if="result.kind === 'verify'" class="result-detail path-wrap">校验完成：{{ store.taskContext?.archive || "所选归档" }}</p>
        <template v-else>
          <p class="result-detail path-wrap">恢复目标：{{ result.restore?.targetDsh || store.taskContext?.target || "请查看日志确认" }}</p>
          <p class="hint">{{ result.restore?.desktopRestored ? "已恢复桌面登录态。" : "未恢复系统桌面登录态。" }}请重新启动 DSH。</p>
        </template>
        <p v-if="result.preRestoreSnapshot" class="hint path-wrap">恢复前快照：{{ result.preRestoreSnapshot.localPath || result.preRestoreSnapshot.id }}</p>
      </template>
      <template v-else>
        <p class="result-detail">{{ resultNextStep }}</p>
        <div class="tool-grid"><button class="btn btn-secondary btn-small" @click="openSettingsTab('security')">查看本地日志</button><button v-if="result.kind !== 'backup'" class="btn btn-secondary btn-small" @click="go('history')">返回归档历史</button></div>
      </template>
      <ul v-if="result.warnings?.length" class="warning-list"><li v-for="warning in result.warnings" :key="warning">{{ warning }}</li></ul>
    </div>

    <div v-if="store.notice" class="alert" :class="store.notice.level === 'error' ? 'error-alert' : 'warning-alert'" role="alert">
      <div class="alert-icon">!</div>
      <div class="alert-copy"><strong>需要留意</strong><span>{{ store.notice.message }}</span></div>
      <button class="icon-btn" type="button" aria-label="关闭提示" @click="dismissNotice"><svg class="icon"><use href="#i-x"></use></svg></button>
    </div>
    <div v-if="boot.warnings.length" class="alert warning-alert" role="alert">
      <div class="alert-icon">!</div>
      <div class="alert-copy"><strong>环境提示</strong><span>{{ boot.warnings[0] }}</span><ul v-if="showAllWarnings" class="warning-list"><li v-for="warning in boot.warnings.slice(1)" :key="warning">{{ warning }}</li></ul></div>
      <button v-if="boot.warnings.length > 1" class="btn btn-secondary btn-small" @click="showAllWarnings = !showAllWarnings">{{ showAllWarnings ? "收起" : `全部 ${boot.warnings.length} 条` }}</button>
    </div>
    <div v-if="boot.dsh.running" class="alert" role="alert">
      <div class="alert-icon">!</div>
      <div class="alert-copy"><strong>DSH 正在运行</strong><span>{{ boot.dsh.evidence.join("；") || "备份前关闭 DSH 可避免文件占用，减少快照不一致。" }}</span></div>
      <button class="btn btn-secondary btn-small" @click="checkDSH()">重新检测</button>
    </div>

    <div class="head">
      <div class="head-main">
        <div class="eyebrow"><i class="pip" :class="issueCount ? 'warn' : (latest ? '' : 'idle')"></i>{{ latest ? `上次备份 ${formatRelative(latest.createdAt)} · ${statusLabel(latest.status)}` : "还没有归档" }}</div>
        <h1>{{ snapshots.length }} 份本地归档<span v-if="latest">，最近一次 {{ formatBytes(latest.size) }}</span></h1>
        <p class="sub">
          备份目录 <span class="mono">{{ config.backupDir }}</span><span class="sep">·</span>{{ nextNote }}<span class="sep">·</span>本地加密{{ config.encryptionLocal ? "已开启" : "未开启" }}
        </p>
      </div>
    </div>

    <div class="figures">
      <div class="fig"><div class="v">{{ formatBytes(localUsage) }}</div><div class="k">归档占用</div></div>
      <div class="fig"><div class="v">{{ formatBytes(boot.disk.free) }}</div><div class="k">目标盘剩余</div></div>
      <div class="fig"><div class="v">{{ snapshots.length ? `${readyCount} /${snapshots.length}` : "—" }}</div><div class="k">归档可用</div></div>
      <div class="fig"><div class="v">{{ latest ? latest.fileCount.toLocaleString() : "—" }}</div><div class="k">最近归档文件数</div></div>
      <div class="fig"><div class="v">{{ nextValue }}</div><div class="k">下次计划</div></div>
    </div>

    <div class="dashboard-grid">
      <section class="card backup-card">
        <div class="card-head">
          <div class="card-title">最近归档</div>
          <span class="card-sub">{{ recent.length }} / {{ snapshots.length }} 份 · 按时间倒序</span>
          <button class="text-button" style="margin-left:auto" @click="go('history')">查看全部<svg class="icon" style="width:12px;height:12px"><use href="#i-chevron-right"></use></svg></button>
        </div>
        <SnapshotTable v-if="recent.length" :snapshots="recent" compact @restore="openRestore($event.id)" @verify="startVerify($event)" @open="openLocalSnapshot($event.id)" />
        <div v-else class="empty-inline">
          <h3>还没有归档</h3>
          <p>准备好本地目录后，创建第一份 DSH 数据备份。归档只保存在这台电脑上。</p>
        </div>
        <div class="card-foot">
          <span class="foot-note" :class="{ warn: !!latestBlocked }">
            <svg class="icon"><use :href="latestBlocked ? '#i-alert' : '#i-check'"></use></svg>
            <span class="path-wrap">{{ latestBlocked || (latest ? "归档可用，文件存在且大小符合清单" : "创建第一份备份后这里会显示完整性") }}</span>
          </span>
          <div class="card-actions">
            <button v-if="latest" class="btn btn-secondary btn-small" :disabled="isBusy || !!latestBlocked" @click="startVerify(latest)">校验最近一份</button>
          </div>
        </div>
      </section>

      <section class="card scope-card">
        <div class="card-head">
          <div class="card-title">保护范围</div>
          <button class="text-button" style="margin-left:auto" @click="openSettingsTab('backup')">编辑<svg class="icon" style="width:12px;height:12px"><use href="#i-chevron-right"></use></svg></button>
        </div>
        <div class="rows">
          <div class="rowline">
            <div class="row-main">
              <div class="row-title">DSH 核心数据</div>
              <div class="row-sub mono" :title="sourceDsh?.path">{{ sourceDsh?.path || "未检测到 DSH 数据目录" }}</div>
            </div>
            <span class="row-meta">{{ sourceDsh?.exists ? formatBytes(sourceDsh.bytes) : "不存在" }}</span>
          </div>
          <div class="rowline">
            <div class="row-main">
              <div class="row-title">桌面登录状态</div>
              <div class="row-sub mono" :title="sourceDesktop?.path">{{ sourceDesktop?.path || "未检测到桌面登录状态" }}</div>
            </div>
            <span class="row-meta">{{ !config.includeDesktop ? "未包含" : sourceDesktop?.exists ? formatBytes(sourceDesktop.bytes) : "不存在" }}</span>
          </div>
          <div class="rowline">
            <div class="row-main">
              <div class="row-title">项目代码</div>
              <div class="row-sub">已排除 · 不读取工作区</div>
            </div>
            <span class="row-meta row-dash">—</span>
          </div>
        </div>

        <div v-if="latest" class="chart-block">
          <div class="chart-head"><b>近 7 天归档大小</b><span>MiB</span></div>
          <div class="chart" aria-label="近 7 天归档大小">
            <div v-for="day in weekly" :key="day.key" class="chart-col" :class="{ today: day.today }" :title="day.title">
              <div class="bar-wrap"><i class="bar" :class="{ dim: !day.today, today: day.today }" :style="{ height: day.height }"></i></div>
              <span class="chart-day">{{ day.label }}</span>
            </div>
          </div>
        </div>

        <div class="scope-foot">
          <div class="mini-row"><span>恢复前自动快照</span><span class="mini-state" :class="{ on: config.preRestoreSnapshot }">{{ config.preRestoreSnapshot ? "已开启" : "未开启" }}</span></div>
          <div class="mini-row"><span>备份后自动校验</span><span class="mini-state" :class="{ on: config.verifyAfterBackup }">{{ config.verifyAfterBackup ? "已开启" : "未开启" }}</span></div>
          <div class="mini-row"><span>本地加密</span><span class="mini-state" :class="{ accent: config.encryptionLocal }">{{ config.encryptionLocal ? "已开启" : "未开启" }}</span></div>
        </div>
      </section>
    </div>

    <div class="page-foot">
      <span><svg class="icon"><use href="#i-folder"></use></svg><span class="path-wrap" :title="config.backupDir">备份目录 {{ config.backupDir }} · 可用 {{ formatBytes(boot.disk.free) }}</span></span>
      <span><svg class="icon"><use href="#i-lock"></use></svg>{{ config.encryptionLocal ? "本地加密已开启" : "本地加密未开启" }}<button class="text-button" @click="openSettingsTab('security')">前往设置<svg class="icon" style="width:12px;height:12px"><use href="#i-chevron-right"></use></svg></button></span>
    </div>
  </section>
</template>
