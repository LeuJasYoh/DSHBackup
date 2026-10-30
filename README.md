# DSH Backup

Windows 便携版 DSH 核心数据备份工具。备份数据只保存在本机，不上传；可选在启动时只读检查 GitHub 发行版。使用 7-Zip 高压缩，可选 AES-256 加密。

- 设计文档：[DESIGN.md](DESIGN.md)
- 开发说明：[app/DSHBackup/README.md](app/DSHBackup/README.md)
- 发行版：[Releases](https://github.com/LeuJasYoh/DSHBackup/releases)

## 功能

- 备份 `.dsh` 核心数据：凭据、会话、附件、插件、用量等
- 可选备份 `%APPDATA%\@deepseek-ai\dsh-desktop` 桌面登录态
- 不要求任何账号，备份数据不上传；可在设置中关闭启动时的 GitHub 发行版检查
- 侧栏 GitHub 入口与可选的新版本提示；不会自动下载或安装更新
- 7-Zip 26.05 x64，压缩档位与高级参数白名单
- 本地加密可选：7z AES-256 + 文件头加密，每个归档独立随机密码，由本地 age X25519 公钥封装；私钥只在本机
- 快照保留、SHA-256 校验、7z 完整性测试
- 恢复前自动备份即将被覆盖的目录；「恢复到其他目录」只写入 DSH 核心数据，不触碰系统桌面登录态
- 路径穿越与重解析点校验
- 计划备份（每天/每周，应用运行时生效）
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
  README.txt
  THIRD_PARTY_NOTICES.txt
dist\DSHBackup-<version>-win64.zip
```

构建脚本会校验 7za SHA-256、构建静态 CRT Rust sidecar、运行 Wails 构建并组装便携目录；组装时保留 `dist\DSHBackup` 下已有的 `data` 和 `backups`。

## 测试

```powershell
# Go
cd app\DSHBackup
go test ./...

# Rust
cd app\DSHBackup\sidecar
cargo test

# 前端
cd app\DSHBackup\frontend
npm run build
```

Go 集成测试会在临时目录里跑完整的「加密备份 → 校验 → 恢复到其他目录 → 覆盖恢复」流程，并断言恢复前快照只保护实际被覆盖的目录。

## 安全边界

- 备份与恢复不初始化云端客户端，也不存储任何账号凭据；唯一的可选网络请求是启动时通过 GitHub 公共 API 只读检查发行版，可在「设置 → 更新」关闭
- 私钥丢失后加密备份无法恢复；换机恢复需复制 `data\keys`
- 不记录归档密码、私钥、Cookie、API Key
- 7z 通过参数数组调用，不经过 shell；只允许白名单高级参数
- 恢复先校验 SHA-256，再解压到受控暂存目录，拒绝路径逃逸和重解析点
- 恢复目标不能是应用目录、备份目录或它们的上级目录

## 已知限制

- 计划任务只在应用运行时触发
- 不做项目代码备份、多账号、增量/去重；首版不做 Linux UI
- 不自动安装更新；启用检查时仅提示 GitHub 发行版并可在浏览器查看。不提供代码签名、VSS、ACL 保存
- 桌面 Cookie 受 Windows 用户加密限制，换机可能需要重新登录
- 1.0.x 的 GitHub 云端功能已在 1.1.0 移除；旧配置会自动迁移为纯本地，原有本地备份和密钥照常可用

## 许可

本项目代码为内部构建，未声明开源许可证。随包 7-Zip 使用 LGPL 2.1+（含 unRAR 限制），第三方组件许可证见 [app/DSHBackup/packaging/THIRD_PARTY_NOTICES.txt](app/DSHBackup/packaging/THIRD_PARTY_NOTICES.txt)。
