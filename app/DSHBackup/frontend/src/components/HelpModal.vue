<script setup lang="ts">
import { ref } from "vue";
import { openLogsFolder, store } from "../store";
import { useModalFocus } from "../lib/modalFocus";
const card = ref<HTMLElement | null>(null);
function close() { store.modals.help = false; }
useModalFocus(card, close);
</script>

<template>
  <div class="overlay modal open" @click.self="close">
    <div ref="card" class="modal-card help-card" role="dialog" aria-modal="true" aria-label="使用说明">
      <div class="modal-head"><div><span class="eyebrow">帮助</span><h2>使用说明</h2></div><button class="icon-btn modal-close" aria-label="关闭" @click="close">×</button></div>
      <div class="modal-body help-body">
        <section><h3>本地备份</h3><p>默认目录在应用目录的 backups 下。删除整个应用目录会连同备份一起删除，重要备份请另外复制一份。</p></section>
        <section><h3>备份范围</h3><p>只读取 DSH 核心数据和可选桌面登录态。不读取项目代码、日志和崩溃转储；项目代码请使用独立工具备份。</p></section>
        <section><h3>加密与私钥</h3><p>在「设置 → 安全」开启本地加密，保存后对新备份生效。归档使用随机密码、AES-256 和文件头加密，密码由本地公钥封装。私钥丢失后无法恢复；换机需复制对应的整个 data\keys 目录。</p></section>
        <section><h3>恢复范围</h3><p>默认覆盖当前 DSH 核心数据，可选择恢复桌面登录态。「恢复到其他目录」只写入指定的核心数据目录，不修改系统桌面登录态。推荐开启恢复前备份，保护即将覆盖的现有数据。请先关闭 DSH，完成后重新启动。</p></section>
        <section><h3>归档状态与删除</h3><p>“归档可用”表示文件存在且大小符合清单，不代表已经校验。可在备份历史运行完整性校验。缺失、损坏或缺少私钥时不能恢复。删除的是备份归档及清单，不是 DSH 原数据。</p></section>
        <section><h3>计划备份</h3><p>计划任务只在本应用运行时触发；到点时若 DSH 正在运行会跳过，并留下提示。设置中的所有修改都需点击「保存设置」后生效。</p></section>
      </div>
      <div class="modal-footer"><button class="btn btn-secondary" @click="openLogsFolder()">打开日志目录</button><button class="btn btn-ghost" @click="close">关闭</button></div>
    </div>
  </div>
</template>
