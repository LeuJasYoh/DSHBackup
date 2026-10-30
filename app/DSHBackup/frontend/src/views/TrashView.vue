<script setup lang="ts">
import { computed } from "vue";
import { askDeleteTrashItem, askEmptyTrash, openTrashFolder, restoreTrashItem, store, trashBytes, trashCount, trashItems } from "../store";
import { formatBytes, formatDate, formatRelative, kindShort } from "../lib/format";
import type { TrashItem } from "../lib/types";

const items = computed(() => [...trashItems.value].sort((a, b) => Date.parse(b.movedAt) - Date.parse(a.movedAt)));
const usable = computed(() => items.value.filter((item) => item.restorable).length);
const reasonLabel = (item: TrashItem) => item.reason === "retention" ? "保留策略" : "手动删除";
const remaining = (item: TrashItem) => item.daysLeft <= 0 ? "即将清理" : `${item.daysLeft} 天`;
const displayName = (item: TrashItem) => item.archiveName || `dsh-backup-${item.snapshotId}.7z`;
</script>

<template>
  <section class="page trash-page" :class="{ active: store.page === 'trash' }">
    <div class="head">
      <div class="head-main">
        <div class="eyebrow"><i class="pip" :class="trashCount ? (usable < trashCount ? 'warn' : '') : 'idle'"></i>{{ trashCount ? `${trashCount} 项在回收站，${usable} 项可找回` : "回收站是空的" }}</div>
        <h1>回收站</h1>
        <p class="sub">
          {{ trashCount }} 项 · 共 {{ formatBytes(trashBytes) }}<span class="sep">·</span>保留 {{ store.boot?.config.retention.trashDays ?? 7 }} 天，到期自动彻底删除<span class="sep">·</span>目录 <span class="mono">{{ store.boot?.trash.root }}</span>
        </p>
      </div>
      <div class="head-act">
        <button class="btn btn-ghost btn-small" type="button" @click="openTrashFolder()">打开目录</button>
        <button class="btn btn-danger btn-small" type="button" :disabled="!trashCount || store.trashBusy" @click="askEmptyTrash()">清空回收站</button>
      </div>
    </div>

    <section v-if="items.length" class="card table-card" style="margin-top: 20px">
      <div class="table-head">
        <div>
          <div class="card-title">已删除的备份</div>
          <div class="card-sub">按移入时间倒序 · 找回会恢复到当前备份目录</div>
        </div>
      </div>
      <div class="table-wrap">
        <table class="snapshot-table">
          <thead>
            <tr>
              <th scope="col">归档</th>
              <th scope="col">来源</th>
              <th scope="col">大小</th>
              <th scope="col">剩余</th>
              <th scope="col" style="text-align: right">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in items" :key="item.snapshotId">
              <td class="archive-cell">
                <div class="file-line">
                  <span class="file-name" :title="displayName(item)">{{ displayName(item) }}</span>
                  <svg v-if="item.encrypted" class="icon file-lock" aria-label="已加密" role="img"><use href="#i-lock"></use></svg>
                </div>
                <div class="file-sub" :title="formatDate(item.movedAt)">{{ formatRelative(item.movedAt) }} · {{ kindShort(item.kind) }}</div>
              </td>
              <td>
                <span class="status-pill" :class="item.reason === 'retention' ? 'warning' : ''"><i></i>{{ reasonLabel(item) }}</span>
              </td>
              <td class="size-cell">{{ formatBytes(item.size) }}</td>
              <td>
                <span v-if="item.blockedReason" class="blocked-note" :title="item.blockedReason">{{ item.blockedReason }}</span>
                <span v-else>{{ remaining(item) }}</span>
              </td>
              <td>
                <div class="table-actions">
                  <button class="table-action" type="button" :disabled="store.trashBusy || !item.restorable" :title="item.blockedReason || '找回这份备份'" aria-label="找回归档" @click="restoreTrashItem(item)">
                    <svg class="icon"><use href="#i-restore"></use></svg>
                  </button>
                  <button class="table-action danger-link" type="button" :disabled="store.trashBusy" title="彻底删除，无法恢复" aria-label="彻底删除" @click="askDeleteTrashItem(item)">
                    <svg class="icon"><use href="#i-trash"></use></svg>
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="table-foot">
        <span>回收站在备份目录内，删除只是同盘移动，不会复制归档</span>
        <span>{{ trashCount }} 项 · {{ formatBytes(trashBytes) }}</span>
      </div>
    </section>

    <div v-else class="card empty-state">
      <div class="empty-icon"><svg class="icon-lg"><use href="#i-recycle"></use></svg></div>
      <h2>回收站还是空的</h2>
      <p>删除归档、或让保留策略淘汰旧备份后，它们会先来到这里，到期才真正删除。</p>
    </div>

    <p class="page-note">
      <svg class="icon"><use href="#i-shield"></use></svg>
      <span>回收站占用的是备份目录里的空间，清空或到期后才会释放。本应用只管理当前备份目录的回收站；切换到其他备份目录后，旧目录里的回收站不会被自动删除，也不会显示在这里。</span>
    </p>
  </section>
</template>
