<script setup lang="ts">
import type { Snapshot } from "../lib/types";
import { isBusy, store } from "../store";
import { formatBytes, formatDate, kindLabel, restoreBlockReason, statusLabel } from "../lib/format";

const props = defineProps<{ snapshots: Snapshot[]; compact?: boolean }>();
const emit = defineEmits<{ restore: [Snapshot]; verify: [Snapshot]; open: [Snapshot]; deleteLocal: [Snapshot] }>();
const blocked = (snapshot: Snapshot) => restoreBlockReason(snapshot, store.boot?.key);
</script>

<template>
  <div class="table-card card">
    <table>
      <thead><tr><th scope="col">时间 / 类型</th><th scope="col">大小</th><th scope="col">加密</th><th scope="col">文件状态</th><th scope="col" class="align-right">操作</th></tr></thead>
      <tbody>
        <tr v-for="snapshot in props.snapshots" :key="snapshot.id">
          <td><strong>{{ formatDate(snapshot.createdAt) }}</strong><small class="row-sub">{{ kindLabel(snapshot.kind) }}</small></td>
          <td>{{ formatBytes(snapshot.size) }}</td>
          <td><span class="badge" :class="snapshot.encrypted ? 'success' : 'muted'">{{ snapshot.encrypted ? '已加密' : '未加密' }}</span></td>
          <td class="snapshot-status"><span class="badge" :class="snapshot.status === 'ready' ? 'success' : 'danger'">{{ statusLabel(snapshot.status) }}</span><small v-if="blocked(snapshot)" class="row-sub">{{ blocked(snapshot) }}</small></td>
          <td class="align-right"><span class="table-actions">
            <button class="table-action" :disabled="isBusy || !!blocked(snapshot)" :title="blocked(snapshot) || '恢复这份备份'" @click="emit('restore', snapshot)">恢复</button>
            <button class="table-action" :disabled="isBusy || !!blocked(snapshot)" :title="blocked(snapshot) || '校验归档完整性'" @click="emit('verify', snapshot)">校验</button>
            <button v-if="!props.compact" class="table-action" :disabled="isBusy || !snapshot.localPath || snapshot.status === 'missing'" @click="emit('open', snapshot)">打开</button>
            <button v-if="!props.compact" class="table-action danger-link" :disabled="isBusy" @click="emit('deleteLocal', snapshot)">删除备份</button>
          </span></td>
        </tr>
        <tr v-if="!props.snapshots.length"><td colspan="5" class="table-empty">还没有备份</td></tr>
      </tbody>
    </table>
  </div>
</template>
