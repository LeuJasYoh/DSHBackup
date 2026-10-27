<script setup lang="ts">
import { openLogsFolder, store } from "../store";
</script>

<template>
  <div class="overlay modal open" @click.self="store.modals.help = false">
    <div class="modal-card help-card">
      <div class="modal-head">
        <div>
          <span class="eyebrow">帮助</span>
          <h2>使用说明</h2>
        </div>
        <button class="icon-btn modal-close" aria-label="关闭" @click="store.modals.help = false">×</button>
      </div>
      <div class="modal-body help-body">
        <section>
          <h3>默认仅本地</h3>
          <p>不登录 GitHub 也能完整备份 DSH 核心数据。默认目录在应用目录的 backups 下；删除整个应用目录会连同备份一起删除，重要备份请另外复制一份。</p>
        </section>
        <section>
          <h3>备份范围</h3>
          <p>只读取 .dsh 核心数据和可选的桌面登录态。不读取项目代码、日志和崩溃缓存；项目数据请使用 Git 远端或独立工具。</p>
        </section>
        <section>
          <h3>加密与私钥</h3>
          <p>每个归档使用随机密码，7z 使用 AES-256 并加密文件头；密码由本地公钥封装。私钥在应用目录的 data\keys 中，丢失后加密备份无法恢复。换机恢复前请复制整个 data\keys 目录。</p>
        </section>
        <section>
          <h3>恢复</h3>
          <p>恢复会覆盖现有 .dsh 和可选的桌面登录态。请先关闭 DSH；默认会先创建一份“恢复前快照”。Cloud 快照需要本机具备对应的私钥。</p>
        </section>
        <section>
          <h3>GitHub 云端</h3>
          <p>云端只使用私有仓库的 Releases，归档始终加密。Device Flow 需要你自己的 OAuth App Client ID（在 GitHub Developer settings 中创建并启用 Device Flow）；也可以使用带 repo 权限的 fine-grained PAT 作为高级兜底。token 保存在 Windows 凭据管理器。</p>
        </section>
        <section>
          <h3>计划备份</h3>
          <p>计划任务只在应用运行时触发；如果到点时 DSH 正在运行，会跳过并提示，避免生成不一致快照。</p>
        </section>
        <div class="help-actions">
          <button class="btn btn-secondary btn-small" @click="openLogsFolder()">打开日志目录</button>
          <button class="btn btn-ghost btn-small" @click="store.modals.help = false">关闭</button>
        </div>
      </div>
    </div>
  </div>
</template>
