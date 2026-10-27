<script setup lang="ts">
import { computed, ref } from "vue";
import ProgressCard from "../components/ProgressCard.vue";
import SnapshotTable from "../components/SnapshotTable.vue";
import {
  checkDSH,
  cloudEnabled,
  dismissTaskResult,
  go,
  latestSnapshot,
  localUsage,
  openBackupFolder,
  openExternal,
  openLocalSnapshot,
  openRestore,
  openSettingsTab,
  snapshots,
  startBackup,
  startVerify,
  store,
  deleteSnapshot,
  totalSourceBytes,
  totalSourceFiles,
} from "../store";
import {
  compressionLabel,
  formatBytes,
  formatDate,
  modeContext,
  scheduleLabel,
  truncateMiddle,
} from "../lib/format";

const boot = computed(() => store.boot!);
const config = computed(() => boot.value.config);
const latest = latestSnapshot;
const recent = computed(() => snapshots.value.slice(0, 3));
const showSourceDetails = ref(false);
const showAllWarnings = ref(false);
const task = computed(() => store.task);
const result = computed(() => store.taskResult);
const showResultCard = computed(
  () => !!result.value && !store.taskResultDismissed && (!result.value.success || !!result.value.warnings?.length || !!result.value.cloudError),
);
const diskUsagePercent = computed(() => {
  const total = boot.value.disk.total;
  if (!total) return 0;
  return Math.min(100, Math.max(1, (localUsage.value / total) * 100));
});

function verifyLatest() {
  if (latest.value) void startVerify(latest.value);
}

function openSnapshot(snapshot: typeof latest.value) {
  if (!snapshot) return;
  if (snapshot.localPath) {
    void openLocalSnapshot(snapshot.id);
  } else if (snapshot.cloudHtmlUrl) {
    void openExternal(snapshot.cloudHtmlUrl);
  }
}

function openSettingsBackup() {
  openSettingsTab("backup");
}
</script>

<template>
  <section class="page" :class="{ active: store.page === 'dashboard' }">
    <div class="page-actions">
      <div class="context-label">
        <span class="dot"></span>
        <span>{{ modeContext(config.mode) }}</span>
      </div>
      <div class="action-row">
        <button class="btn btn-secondary" :disabled="!!task || snapshots.length === 0" @click="openRestore()">恢复</button>
        <button class="btn btn-primary" :disabled="!!task" @click="startBackup(false)">
          {{ task?.kind === "backup" ? "备份中…" : "立即备份" }}
        </button>
      </div>
    </div>

    <ProgressCard v-if="task" />

    <div v-if="boot.warnings.length" class="alert warning-alert">
      <div class="alert-icon">!</div>
      <div class="alert-copy">
        <strong>环境提示</strong>
        <span>{{ boot.warnings[0] }}</span>
        <ul v-if="showAllWarnings" class="warning-list compact-warnings">
          <li v-for="warning in boot.warnings.slice(1)" :key="warning">{{ warning }}</li>
        </ul>
      </div>
      <button v-if="boot.warnings.length > 1" class="btn btn-secondary btn-small" @click="showAllWarnings = !showAllWarnings">
        {{ showAllWarnings ? "收起" : `全部 ${boot.warnings.length} 条` }}
      </button>
    </div>

    <template v-else>
      <div v-if="latest" class="card hero-card">
        <div class="hero-main">
          <div class="eyebrow">
            <span class="dot success"></span>
            <span>最近一次备份 · {{ latest.status === "ready" || latest.status === "cloud-only" ? "成功" : "需检查" }}</span>
          </div>
          <h2>{{ formatDate(latest.createdAt) }}</h2>
          <div class="hero-meta">
            <span>{{ formatBytes(latest.size) }}</span>
            <span class="meta-sep"></span>
            <span>{{ compressionLabel(latest.compression || config.compression.level) }}</span>
            <span class="badge" :class="latest.encrypted ? 'success' : 'muted'">{{ latest.encrypted ? "已加密" : "未加密" }}</span>
            <span v-if="latest.location !== 'local'" class="badge cloud">云端</span>
          </div>
          <div class="path-line">
            {{ latest.localPath ? truncateMiddle(latest.localPath, 82) : `GitHub Releases · ${latest.cloudTag || "云端快照"}` }}
          </div>
        </div>
        <div class="hero-actions">
          <button class="btn btn-ghost" :disabled="!latest.localPath" @click="openBackupFolder()">打开目录</button>
          <button class="btn btn-ghost" @click="verifyLatest">验证备份</button>
        </div>
      </div>

      <div v-else class="card empty-state">
        <div class="empty-icon">
          <svg class="icon-lg"><use href="#i-shield"></use></svg>
        </div>
        <h2>还没有备份</h2>
        <p>
          {{ config.mode === "local" ? "仅本地模式无需登录 GitHub，也可以随时在设置中连接云端。" : "连接已就绪，现在可以创建第一份备份。" }}
        </p>
        <button class="btn btn-primary" :disabled="!!task" @click="startBackup(false)">立即备份</button>
      </div>
    </template>

    <div v-if="showResultCard" class="card task-result-card" :class="result?.success ? 'warning' : 'error'">
      <div class="task-result-head">
        <div>
          <div class="eyebrow">{{ result?.success ? "任务提示" : "任务未完成" }}</div>
          <strong>{{ result?.success ? result?.message : result?.error || result?.message }}</strong>
        </div>
        <button class="icon-btn" aria-label="关闭" @click="dismissTaskResult">×</button>
      </div>
      <ul v-if="result?.warnings?.length || result?.cloudError" class="warning-list">
        <li v-if="result?.cloudError">云端错误：{{ result.cloudError }}</li>
        <li v-for="warning in result?.warnings" :key="warning">{{ warning }}</li>
      </ul>
    </div>

    <div v-if="boot.dsh.running" class="alert">
      <div class="alert-icon">!</div>
      <div class="alert-copy">
        <strong>DSH 正在运行</strong>
        <span>{{ boot.dsh.evidence.join("；") || "备份前关闭 DSH 可避免文件占用，保证快照一致。" }}</span>
      </div>
      <button class="btn btn-secondary btn-small" @click="checkDSH()">重新检测</button>
    </div>

    <div v-if="boot.cloudError" class="alert cloud-alert">
      <div class="alert-icon">!</div>
      <div class="alert-copy">
        <strong>云端快照暂时不可用</strong>
        <span>{{ boot.cloudError }}。本地备份不受影响。</span>
      </div>
      <button class="btn btn-secondary btn-small" @click="openSettingsTab('cloud')">检查云端</button>
    </div>

    <div class="grid two">
      <div class="card stat-card">
        <div class="stat-head">
          <h3>备份内容</h3>
          <span class="badge local">核心数据</span>
        </div>
        <div v-for="source in boot.dsh.sources" :key="source.name" class="stat-row">
          <span>{{ source.label }}</span>
          <strong>{{ source.exists ? formatBytes(source.bytes) : "不存在" }}</strong>
        </div>
        <div class="stat-foot">
          <button class="text-button" @click="showSourceDetails = !showSourceDetails">
            {{ showSourceDetails ? "收起详情" : "查看详情" }}
          </button>
          <span>约 {{ formatBytes(totalSourceBytes) }} · {{ totalSourceFiles.toLocaleString() }} 个文件</span>
        </div>
        <div v-if="showSourceDetails" class="stat-details">
          <div v-for="source in boot.dsh.sources" :key="source.name" class="detail-line">
            <span>{{ source.name }}</span>
            <span class="mono">{{ source.path }}</span>
          </div>
          <p>排除 logs、cache、崩溃缓存；不读取项目代码。</p>
        </div>
      </div>

      <div class="card stat-card">
        <div class="stat-head">
          <h3>存储</h3>
          <span class="badge muted">本地</span>
        </div>
        <div class="stat-row">
          <span>可用空间</span>
          <strong>{{ formatBytes(boot.disk.free) }}</strong>
        </div>
        <div class="stat-row">
          <span>本地快照</span>
          <strong>{{ boot.localSnapshots.length }} 份 · {{ formatBytes(localUsage) }}</strong>
        </div>
        <div class="usage-bar"><span :style="{ width: `${diskUsagePercent}%` }"></span></div>
        <div class="stat-foot">
          <button class="text-button" @click="openSettingsBackup">管理保留份数</button>
          <span>保留 {{ config.retention.local }} 份</span>
        </div>
        <div class="stat-foot compact-foot">
          <button class="text-button" @click="openSettingsTab('backup')">计划备份</button>
          <span>{{ scheduleLabel(config.schedule.kind, config.schedule.time, config.schedule.weekday) }}</span>
        </div>
      </div>
    </div>

    <template v-if="recent.length">
      <div class="section-head">
        <h3>最近备份</h3>
        <button class="text-button" @click="go('history')">查看全部</button>
      </div>
      <SnapshotTable
        :snapshots="recent"
        compact
        @restore="openRestore($event.id)"
        @verify="startVerify($event)"
        @open="openSnapshot($event)"
      />
    </template>

    <div class="card note-card">
      <div class="note-icon">
        <svg class="icon"><use href="#i-cloud"></use></svg>
      </div>
      <div>
        <strong>{{ cloudEnabled ? "本地与云端共用一份加密归档" : "本地优先，不依赖 GitHub" }}</strong>
        <p>
          {{
            cloudEnabled
              ? "云端归档始终加密；本地是否加密由“安全”设置决定。删除云端快照不会影响本地副本。"
              : "当前不发起任何网络请求。需要异地副本时，可在设置中连接 GitHub 私有仓库。"
          }}
        </p>
      </div>
    </div>
  </section>
</template>
