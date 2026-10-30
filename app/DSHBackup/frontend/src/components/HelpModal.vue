<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref } from "vue";
import { openLogsFolder, store } from "../store";
import { useModalFocus } from "../lib/modalFocus";

const card = ref<HTMLElement | null>(null);
const doc = ref<HTMLElement | null>(null);
const active = ref("start");
const sections = [
  { id: "start", label: "快速上手" },
  { id: "scope", label: "备份范围" },
  { id: "compress", label: "压缩" },
  { id: "keys", label: "加密与密钥" },
  { id: "status", label: "归档状态" },
  { id: "trash", label: "回收站" },
  { id: "restore", label: "恢复" },
  { id: "schedule", label: "计划备份" },
  { id: "close", label: "关闭与托盘" },
  { id: "diagnostics", label: "诊断与日志" },
  { id: "limits", label: "已知限制" },
  { id: "update", label: "更新" },
];

function close() { store.modals.help = false; }
useModalFocus(card, close);

function jump(id: string) {
  active.value = id;
  const target = doc.value?.querySelector<HTMLElement>(`#help-${id}`);
  target?.scrollIntoView({ block: "start" });
}
function onScroll() {
  const container = doc.value;
  if (!container) return;
  const top = container.getBoundingClientRect().top;
  let current = sections[0].id;
  for (const section of sections) {
    const element = container.querySelector<HTMLElement>(`#help-${section.id}`);
    if (!element) continue;
    if (element.getBoundingClientRect().top - top <= 28) current = section.id;
  }
  active.value = current;
}
onMounted(() => {
  void nextTick(onScroll);
  doc.value?.addEventListener("scroll", onScroll, { passive: true });
});
onUnmounted(() => doc.value?.removeEventListener("scroll", onScroll));
</script>

<template>
  <div class="overlay modal open" @click.self="close">
    <div ref="card" class="modal-card help-card" role="dialog" aria-modal="true" aria-label="使用说明">
      <div class="modal-head">
        <div><span class="eyebrow">帮助</span><h2>使用说明</h2></div>
        <button class="icon-btn modal-close" aria-label="关闭" @click="close">×</button>
      </div>
      <div class="modal-body help-body">
        <nav class="help-rail" aria-label="帮助目录">
          <button
            v-for="section in sections"
            :key="section.id"
            type="button"
            class="help-rail-item"
            :class="{ active: active === section.id }"
            :aria-current="active === section.id ? 'true' : undefined"
            @click="jump(section.id)"
          >{{ section.label }}</button>
        </nav>
        <div ref="doc" class="help-doc">
          <section id="help-start">
            <h3>快速上手</h3>
            <p>首次启动会引导两步：选择备份目录、决定是否本地加密。之后在「总览」点「立即备份」生成第一份归档；恢复在「归档历史」里选一份备份点恢复。</p>
            <p>所有数据都留在本机：备份、清单和密钥都在应用目录或你指定的备份目录内，不需要账号，也不会上传。</p>
          </section>

          <section id="help-scope">
            <h3>备份范围</h3>
            <p>核心数据是 <span class="mono">%USERPROFILE%\.dsh</span>（跳过 logs、cache 和 electron 用户数据）；桌面登录态可选，包含 Cookies、Local Storage 和窗口偏好。</p>
            <p>不读取项目代码、工作区和崩溃转储。项目代码请用 Git 或其他工具单独备份。</p>
          </section>

          <section id="help-compress">
            <h3>压缩</h3>
            <p>六档固定映射 7-Zip 的 <span class="mono">-mx</span>：仅存储 0、极速 1、快速 3、均衡 5、高压缩 7、极限 9。新安装默认「仅存储」：100 MB 左右的 DSH 数据在 SSD 上几乎瞬间完成，代价是归档更大。</p>
            <p>恢复前快照跟随当前档位。「仅存储」仍是 7z 容器，保留 CRC 与加密能力，不是裸复制。高级参数只接受 <span class="mono">-md</span>、<span class="mono">-mmt</span>、<span class="mono">-mfb</span>、<span class="mono">-ms</span>、<span class="mono">-mqs</span>，不再接受 <span class="mono">-mx</span>。</p>
          </section>

          <section id="help-keys">
            <h3>加密与密钥</h3>
            <p>开启后每个归档使用独立随机密码，经 7z AES-256 与文件头加密；密码由本机 age X25519 公钥封装，私钥保存在 <span class="mono">data\keys</span>。</p>
            <p>私钥丢失后加密备份无法恢复。换机时请复制整个 <span class="mono">data\keys</span> 目录。公私钥不配对时会阻止新建加密备份，但有效的旧私钥仍可用于解开旧归档。</p>
          </section>

          <section id="help-status">
            <h3>归档状态</h3>
            <p>「归档可用」只表示文件存在且大小符合清单，不代表已经校验。需要确认时，在归档历史里运行完整性校验。</p>
            <p>大小不符、缺失、清单冲突的归档会保留在列表里便于找回，但不能恢复；采集不完整的归档（partial）不会被保留策略计入，也不会挤掉完整备份。</p>
          </section>

          <section id="help-trash">
            <h3>回收站</h3>
            <p>删除归档只是把它同盘移动到 <span class="mono">&lt;备份目录&gt;\.dshbackup-trash</span>，瞬间完成，可以连续删除，不影响正在运行的备份。保留策略淘汰的旧备份也会进入这里。</p>
            <p>默认保留 {{ store.boot?.config.retention.trashDays ?? 7 }} 天（可在设置改为 1 / 7 / 30 / 90 天），到期自动彻底删除；也可以手动清空。找回会把归档放回当前备份目录，不覆盖同名文件；回收站与当前备份目录不在同一分区时不允许瞬时找回。</p>
            <p>空间要等清空或到期后才释放。本应用只管理当前备份目录的回收站，切换目录后旧目录里的内容不会被自动删除。</p>
          </section>

          <section id="help-restore">
            <h3>恢复</h3>
            <p>默认覆盖当前 DSH 数据，可选择一并恢复桌面登录态；恢复前快照默认开启，用来保护即将被覆盖的数据。「恢复到其他目录」只写核心数据，不修改系统桌面登录态。</p>
            <p>恢复前请先关闭 DSH，完成后重新启动。恢复是逐文件提交，不是整树事务：中途失败时已经写入的文件不会自动回滚。</p>
          </section>

          <section id="help-schedule">
            <h3>计划备份</h3>
            <p>计划只在应用运行时触发；到点时若 DSH 正在运行会跳过并留下提示。应用关闭期间错过的时刻不会补跑。</p>
            <p>设置页的所有修改都要点「保存设置」才生效，离开设置页时会提示尚未保存。</p>
          </section>

          <section id="help-close">
            <h3>关闭与托盘</h3>
            <p>点标题栏关闭按钮可以选择：每次询问、直接退出、最小化到系统托盘。首次询问时可以记住选择，之后在「设置 → 运行环境」修改。</p>
            <p>留在托盘时应用继续运行，计划备份照常触发；托盘菜单可以打开窗口、立即备份、打开备份目录或退出。再次双击程序会唤回已经隐藏的窗口。</p>
          </section>

          <section id="help-diagnostics">
            <h3>诊断与日志</h3>
            <p>日志在应用目录的 <span class="mono">data\logs</span>。导出诊断包包含配置、快照清单、工具版本与脱敏日志，不包含私钥、归档密码或 DSH 凭据内容；分享前请自行检查。</p>
            <p>极端退出可能在恢复目标旁留下 <span class="mono">.dshbak-*.tmp</span> 候选文件。程序不会自动删除无法确认归属的外部文件，请按错误信息人工检查后再处理。</p>
          </section>

          <section id="help-limits">
            <h3>已知限制</h3>
            <p>不备份项目代码，不做增量/去重，不保存自定义 ACL，不提供代码签名或 VSS。恢复逐文件提交，不是整树事务。桌面 Cookie 受 Windows 用户加密限制，换机可能需要重新登录。1.0.x 的云端功能已在 1.1.0 移除，旧配置会自动迁移为纯本地。</p>
          </section>

          <section id="help-update">
            <h3>更新</h3>
            <p>启动时可只读检查 GitHub 发行版，仅提示新版本并在浏览器打开，不自动下载或安装；可在「设置 → 更新」关闭。更新便携程序前，请保留 <span class="mono">data</span>、备份目录和密钥。</p>
          </section>
        </div>
      </div>
      <div class="modal-footer">
        <button class="btn btn-secondary" @click="openLogsFolder()">打开日志目录</button>
        <button class="btn btn-ghost" @click="close">关闭</button>
      </div>
    </div>
  </div>
</template>
