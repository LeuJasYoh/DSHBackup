<script setup lang="ts">
import { computed, ref } from "vue";
import ProgressCard from "../components/ProgressCard.vue";
import SnapshotTable from "../components/SnapshotTable.vue";
import { checkDSH, dismissNotice, dismissTaskResult, go, isBusy, latestSnapshot, localUsage, openBackupFolder, openLocalSnapshot, openRestore, openSettingsTab, snapshots, startBackup, startVerify, store, totalSourceBytes, totalSourceFiles } from "../store";
import { compressionLabel, formatBytes, formatDate, restoreBlockReason, scheduleLabel, statusLabel } from "../lib/format";

const boot = computed(() => store.boot!);
const config = computed(() => boot.value.config);
const latest = latestSnapshot;
const recent = computed(() => snapshots.value.slice(0, 3));
const showSourceDetails = ref(false);
const showAllWarnings = ref(false);
const result = computed(() => store.taskResult);
const diskUsagePercent = computed(() => boot.value.disk.total ? Math.min(100, localUsage.value / boot.value.disk.total * 100) : 0);
const resultNextStep = computed(() => {
  if (result.value?.kind === 'restore') return '请先检查恢复目标和日志，确认数据状态后，再从备份历史重新发起恢复。不要在未检查的情况下反复覆盖。';
  if (result.value?.kind === 'verify') return '请检查归档文件、对应私钥及日志。找回完整归档后，可在备份历史重新校验。';
  return '请检查备份目录权限、剩余空间及日志，关闭 DSH 后再点击“立即备份”重试。';
});
</script>

<template>
  <section class="page" :class="{ active: store.page === 'dashboard' }">
    <div class="page-actions"><div class="context-label"><span class="dot"></span><span>本地备份 · 数据由你保管</span></div><div class="action-row"><button class="btn btn-secondary" :disabled="isBusy || !snapshots.length" @click="openRestore()">恢复</button><button class="btn btn-primary" :disabled="isBusy" @click="startBackup(false)">{{ store.startingTask ? '正在启动…' : store.task?.kind === 'backup' ? '备份中…' : '立即备份' }}</button></div></div>
    <div v-if="store.startingTask" class="card note-card" role="status"><span class="spinner"></span>正在检查并启动任务，请勿重复操作…</div>
    <ProgressCard v-if="store.task" :key="store.task.taskId" />
    <div v-if="result && !store.taskResultDismissed" class="card task-result-card" :class="result.success ? 'success' : 'error'" role="status">
      <div class="task-result-head"><div><div class="eyebrow">{{ result.success ? '任务完成' : '任务未完成' }}</div><strong>{{ result.success ? result.message : result.error || result.message }}</strong></div><button class="icon-btn" aria-label="关闭结果摘要" @click="dismissTaskResult">×</button></div>
      <template v-if="result.success">
        <p v-if="result.kind === 'backup'" class="result-detail path-wrap">备份路径：{{ result.snapshot?.localPath || store.taskContext?.target || '请在备份历史查看归档' }}</p>
        <p v-else-if="result.kind === 'verify'" class="result-detail path-wrap">校验完成：{{ store.taskContext?.archive || '所选归档' }}</p>
        <template v-else><p class="result-detail path-wrap">恢复目标：{{ result.restore?.targetDsh || store.taskContext?.target || '请查看日志确认' }}</p><p class="hint">{{ result.restore?.desktopRestored ? '已恢复桌面登录态。' : '未恢复系统桌面登录态。' }}请重新启动 DSH。</p></template>
        <p v-if="result.preRestoreSnapshot" class="hint path-wrap">恢复前快照：{{ result.preRestoreSnapshot.localPath || result.preRestoreSnapshot.id }}</p>
      </template>
      <template v-else><p class="result-detail">{{ resultNextStep }}</p><div class="tool-grid"><button class="btn btn-secondary btn-small" @click="openSettingsTab('about')">查看本地日志</button><button v-if="result.kind !== 'backup'" class="btn btn-secondary btn-small" @click="go('history')">返回备份历史</button></div></template>
      <ul v-if="result.warnings?.length" class="warning-list"><li v-for="warning in result.warnings" :key="warning">{{ warning }}</li></ul>
    </div>
    <div v-if="store.notice" class="alert" :class="store.notice.level === 'error' ? 'error-alert' : 'warning-alert'" role="alert"><div class="alert-icon">!</div><div class="alert-copy"><strong>需要留意</strong><span>{{ store.notice.message }}</span></div><button class="icon-btn" aria-label="关闭提示" @click="dismissNotice">×</button></div>
    <div v-if="boot.warnings.length" class="alert warning-alert" role="alert"><div class="alert-icon">!</div><div class="alert-copy"><strong>环境提示</strong><span>{{ boot.warnings[0] }}</span><ul v-if="showAllWarnings" class="warning-list"><li v-for="warning in boot.warnings.slice(1)" :key="warning">{{ warning }}</li></ul></div><button v-if="boot.warnings.length > 1" class="btn btn-secondary btn-small" @click="showAllWarnings = !showAllWarnings">{{ showAllWarnings ? '收起' : `全部 ${boot.warnings.length} 条` }}</button></div>
    <div v-if="latest" class="card hero-card">
      <div class="hero-main"><div class="eyebrow"><span class="dot" :class="{ success: latest.status === 'ready' }"></span><span>最近一次备份 · {{ statusLabel(latest.status) }}</span></div><h2>{{ formatDate(latest.createdAt) }}</h2><div class="hero-meta"><span>{{ formatBytes(latest.size) }}</span><span class="meta-sep"></span><span>{{ compressionLabel(latest.compression) }}</span><span class="badge" :class="latest.encrypted ? 'success' : 'muted'">{{ latest.encrypted ? '已加密' : '未加密' }}</span></div><div class="path-line" :title="latest.localPath">{{ latest.localPath || '归档文件缺失' }}</div><p v-if="restoreBlockReason(latest, boot.key)" class="hint error">{{ restoreBlockReason(latest, boot.key) }}</p></div>
      <div class="hero-actions"><button class="btn btn-ghost" :disabled="!latest.localPath" @click="openBackupFolder()">打开目录</button><button class="btn btn-ghost" :disabled="isBusy || !!restoreBlockReason(latest, boot.key)" @click="startVerify(latest)">校验备份</button></div>
    </div>
    <div v-else class="card empty-state"><div class="empty-icon"><svg class="icon-lg"><use href="#i-shield"></use></svg></div><h2>还没有备份</h2><p>准备好本地目录后，创建第一份 DSH 数据备份。</p><button class="btn btn-primary" :disabled="isBusy" @click="startBackup(false)">立即备份</button></div>
    <div v-if="boot.dsh.running" class="alert" role="alert"><div class="alert-icon">!</div><div class="alert-copy"><strong>DSH 正在运行</strong><span>{{ boot.dsh.evidence.join('；') || '备份前关闭 DSH 可避免文件占用，减少快照不一致。' }}</span></div><button class="btn btn-secondary btn-small" @click="checkDSH()">重新检测</button></div>
    <div class="grid two">
      <div class="card stat-card"><div class="stat-head"><h3>可备份内容</h3><span class="badge local">核心数据</span></div><div v-for="source in boot.dsh.sources" :key="source.name" class="stat-row"><span>{{ source.label }}</span><strong>{{ source.exists ? formatBytes(source.bytes) : '不存在' }}</strong></div><div class="stat-foot"><button class="text-button" @click="showSourceDetails = !showSourceDetails">{{ showSourceDetails ? '收起详情' : '查看详情' }}</button><span>约 {{ formatBytes(totalSourceBytes) }} · {{ totalSourceFiles.toLocaleString() }} 个文件</span></div><div v-if="showSourceDetails" class="stat-details"><div v-for="source in boot.dsh.sources" :key="source.name" class="detail-line"><span>{{ source.label }}</span><span class="mono">{{ source.path }}</span></div><p>排除日志、缓存、崩溃缓存；不读取项目代码。桌面登录态是否备份以设置为准。</p></div></div>
      <div class="card stat-card"><div class="stat-head"><h3>存储</h3><span class="badge muted">本地</span></div><div class="stat-row"><span>可用空间</span><strong>{{ formatBytes(boot.disk.free) }}</strong></div><div class="stat-row"><span>本地备份</span><strong>{{ boot.localSnapshots.length }} 份 · {{ formatBytes(localUsage) }}</strong></div><div class="usage-bar"><span :style="{ width: `${diskUsagePercent}%` }"></span></div><div class="stat-foot"><button class="text-button" @click="openSettingsTab('backup')">管理保留份数</button><span>保留 {{ config.retention.local }} 份普通备份</span></div><div class="stat-foot compact-foot"><button class="text-button" @click="openSettingsTab('backup')">计划备份</button><span>{{ scheduleLabel(config.schedule.kind, config.schedule.time, config.schedule.weekday) }}</span></div></div>
    </div>
    <template v-if="recent.length"><div class="section-head"><h3>最近备份</h3><button class="text-button" @click="go('history')">查看全部</button></div><SnapshotTable :snapshots="recent" compact @restore="openRestore($event.id)" @verify="startVerify($event)" @open="openLocalSnapshot($event.id)" /></template>
    <div class="card note-card"><div class="note-icon"><svg class="icon"><use href="#i-shield"></use></svg></div><div><strong>妥善保管备份与密钥</strong><p>建议在另一块本地存储设备上留存副本。开启加密后，请将私钥另行保存；丢失对应私钥将无法恢复加密归档。</p></div></div>
  </section>
</template>
