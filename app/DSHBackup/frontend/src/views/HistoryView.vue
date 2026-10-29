<script setup lang="ts">
import SnapshotTable from "../components/SnapshotTable.vue";
import { deleteSnapshot, isBusy, openLocalSnapshot, openRestore, refresh, snapshots, startVerify, store } from "../store";
</script>

<template>
  <section class="page" :class="{ active: store.page === 'history' }">
    <div class="page-actions"><div class="context-label"><span class="dot"></span><span>共 {{ snapshots.length }} 份本地备份</span></div><button class="btn btn-secondary" :disabled="isBusy || store.refreshing" @click="refresh(false)"><span v-if="store.refreshing" class="spinner"></span><svg v-else class="icon"><use href="#i-refresh"></use></svg>{{ store.refreshing ? '刷新中…' : '刷新' }}</button></div>
    <SnapshotTable v-if="snapshots.length" :snapshots="snapshots" @restore="openRestore($event.id)" @verify="startVerify($event)" @open="openLocalSnapshot($event.id)" @delete-local="deleteSnapshot($event)" />
    <div v-else class="card empty-state"><div class="empty-icon"><svg class="icon-lg"><use href="#i-history"></use></svg></div><h2>还没有备份历史</h2><p>完成一次本地备份后，归档会出现在这里。</p></div>
    <div class="card note-card"><div class="note-icon"><svg class="icon"><use href="#i-shield"></use></svg></div><div><strong>归档可用不代表已经校验</strong><p>“归档可用”表示文件存在且大小符合清单。可使用“校验”检查完整性。缺失或异常的记录会保留，便于找回归档；删除备份不会删除 DSH 原数据。</p></div></div>
  </section>
</template>
