# DSH Backup

Windows 便携版 DSH 核心数据备份工具。备份数据只保存在本机，不上传；可选在启动时只读检查 GitHub 发行版。使用 7-Zip 压缩（含仅存储档），可选 AES-256 加密。

- 设计文档：[DESIGN.md](DESIGN.md)
- 开发说明：[app/DSHBackup/README.md](app/DSHBackup/README.md)
- 发行版：[Releases](https://github.com/LeuJasYoh/DSHBackup/releases)

## 功能

- 备份整个 `.dsh` 目录：凭据 `.credentials.yaml`、用量 `.dshw-usage.json`、登录态与配置 `profiles`/`sessions`/`storages`、附件 `attachments`/`media`、角色与语音 `whale-*` 等；只排除 `logs`、`cache`、`electron-user-data`
- 可选备份 `%APPDATA%\@deepseek-ai\dsh-desktop` 桌面登录态；不读取项目代码与工作区
- 不要求任何账号，备份数据不上传；可在设置中关闭启动时的 GitHub 发行版检查
- 侧栏 GitHub 入口与可选的新版本提示；不会自动下载或安装更新
- 7-Zip 26.05 x64，六档压缩（仅存储 / 极速 / 快速 / 均衡 / 高压缩 / 极限）；新安装默认仅存储，高级参数白名单不再包含 `-mx`
- 本地加密可选：7z AES-256 + 文件头加密，每个归档独立随机密码，由本地 age X25519 公钥封装；私钥只在本机
- 快照保留、SHA-256 校验、7z 完整性测试
- 恢复前自动备份即将被覆盖的目录；「恢复到其他目录」只写入 DSH 核心数据，不触碰系统桌面登录态
- 路径穿越与重解析点校验；恢复使用目标旁短命候选文件替换，复制/校验失败时不截断旧文件
- 计划备份（每天/每周，应用运行时生效；不追补应用关闭期间错过的时刻）
- 界面跟随系统亮/暗，也可在设置里固定；暗色为中性纸墨底纱层，不抬亮度
- 设置页是一整篇可滚动文档，左侧一条目录导轨标出当前小节
- 删除归档 = 瞬时移入备份目录内的回收站（同盘改名，可连续删除），默认可找回 7 天，保留策略淘汰的旧备份也进入回收站
- 关闭行为可选「每次询问 / 直接退出 / 最小化到系统托盘」；留在托盘时计划备份照常运行，再次启动会唤回窗口
- 备份过程逐步列出并显示每步耗时，长路径鼠标移上去可复制
- Wails 2.15 + WebView2 + Vue3/TypeScript，便携版免安装

## 技术栈

```text
Vue3/TS UI → Wails/WebView2
                  │
                  ▼
              Go 业务层
                  │ JSON-RPC/stdio
                  ▼
        Rust sidecar (dsh-backup-core.exe)
                  │ 参数数组
                  ▼
             7za.exe 26.05 (x64)
```

- Go：配置、任务、备份/恢复编排、计划任务
- Rust：文件暂存、7z 编排、SHA-256、age 密钥封装、解压安全校验
- 7-Zip：固定版本与 SHA-256，随包分发，附 License
- 数据目录：便携版应用目录下 `data/` 与 `backups/`

## 构建

要求 Windows 10/11 x64、Go 1.26+、Node 24+、Rust stable MSVC、Visual Studio Build Tools、Wails CLI 2.15，以及 `tools/7zip/7z2605-extra` 中的官方 7-Zip CLI。

```powershell
cd app\DSHBackup
powershell -ExecutionPolicy Bypass -File .\build.ps1
```

输出：

```text
dist\DSHBackup\
  DSHBackup.exe
  dsh-backup-core.exe
  tools\7za.exe
  tools\License.txt
  icon.ico
  README.txt
  THIRD_PARTY_NOTICES.txt
  7-Zip-SOURCE.txt
  BUILD.txt
dist\DSHBackup-<version>-win64.zip
```

构建脚本会校验 7za SHA-256、构建静态 CRT Rust sidecar、运行 Wails 构建，并在**独立干净暂存目录**组装便携版。
ZIP 只含上面 9 个运行文件，不会把配置、密钥或归档打进包；升级已有安装时只更新运行文件，`data` 和 `backups`
仍保留在原便携目录中（脚本不再删除它们；测试过用哨兵文件验证）。

## 测试

```powershell
# Go
cd app\DSHBackup
go test ./...

# Rust
cd app\DSHBackup\sidecar
cargo test

# 前端（先 vue-tsc 类型检查，再 Vite 构建）
cd app\DSHBackup\frontend
npm run build
```

Go 集成测试会在临时目录里跑完整的「加密备份 → 校验 → 恢复到其他目录 → 覆盖恢复」流程，并断言恢复前快照只保护实际被覆盖的目录。
前端还有只在浏览器里生效的开发态 mock（`frontend/src/lib/mock.ts`，生产构建时由 Vite 整块消除），详见
[app/DSHBackup/README.md](app/DSHBackup/README.md)。

## 安全边界

- 备份与恢复不初始化云端客户端，也不存储任何账号凭据；唯一的可选网络请求是启动时通过 GitHub 公共 API 只读检查发行版，可在「设置 → 更新」关闭
- 私钥丢失后加密备份无法恢复；换机恢复需复制 `data\keys`
- 公私钥不配对时阻止新建加密备份，但仍允许用有效私钥尝试恢复旧归档
- 不记录归档密码、私钥、Cookie、API Key
- 7z 在首次执行前校验固定 SHA-256；通过参数数组调用，不经过 shell；只允许白名单高级参数
- 恢复先校验 SHA-256，再解压到受控暂存目录，拒绝路径逃逸、重解析点、目标内链接和文件写穿
- 恢复目标不能与当前 DSH 数据、桌面登录态、应用目录或备份目录重叠；`Force` 不跳过这些检查
- 普通暂存、压缩和日志仍在应用旁；只有恢复提交时的短命候选文件位于目标文件旁，正常完成会清理
- 计划任务只在应用运行时触发；运行中错过的时刻合并为最近一次，应用关闭期间不追补
- 回收站占用备份目录空间，清空或到期后才释放；只管理当前备份目录的回收站，切换目录不会自动清理旧目录

## 已知限制

- 不做项目代码备份、多账号、增量/去重；首版不做 Linux UI
- 不自动安装更新；启用检查时仅提示 GitHub 发行版并可在浏览器查看。不提供代码签名、VSS
- 恢复逐文件提交，不是整树事务；中途失败时已提交文件不会自动回滚。不保存/恢复自定义 ACL，替换文件使用目标目录继承的权限
- 恢复前会按归档声明的总大小预检空间；压缩比超过 200 倍且大于 256 MB 时要求确认。目标目录中的 `.dshbak-*.tmp` 残留只会被报告，不会自动删除
- 断电或强制结束可能留下 `.dshbak-*.tmp` 恢复候选；下次恢复会报告并写进诊断包，但程序不会自动删除无法确认归属的外部文件
- 暂存使用独立复制而不是硬链接，因此需要额外临时空间并增加耗时
- 桌面 Cookie 受 Windows 用户加密限制，换机可能需要重新登录
- 1.0.x 的 GitHub 云端功能已在 1.1.0 移除；旧配置会自动迁移为纯本地，原有本地备份和密钥照常可用

## 许可

本项目代码为内部构建，未声明开源许可证。随包 7-Zip 使用 LGPL 2.1+（含 unRAR 限制），第三方组件许可证见 [app/DSHBackup/packaging/THIRD_PARTY_NOTICES.txt](app/DSHBackup/packaging/THIRD_PARTY_NOTICES.txt)。
