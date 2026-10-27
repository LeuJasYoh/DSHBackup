# DSH Backup

Windows 便携版 DSH 核心数据备份工具。默认仅本地，云端可选；使用 7-Zip 高压缩与 AES-256 加密，支持 GitHub 私有仓库 Releases。

- 设计文档：[DESIGN.md](DESIGN.md)
- 开发说明：[app/DSHBackup/README.md](app/DSHBackup/README.md)
- 发行版：[Releases](https://github.com/LeuJasYoh/DSHBackup/releases)

## 功能

- 备份 `.dsh` 核心数据：凭据、会话、附件、插件、用量等
- 可选备份 `%APPDATA%\@deepseek-ai\dsh-desktop` 桌面登录态
- 默认仅本地，不要求 GitHub，不发起网络请求
- 7-Zip 26.05 x64，压缩档位与高级参数白名单
- 本地加密可选；云端归档强制 7z AES-256 + 文件头加密
- 每个归档独立随机密码，由本地 age X25519 公钥封装；私钥只在本机
- 本地/云端快照保留、SHA-256 校验、7z 完整性测试
- GitHub Device Flow / PAT、私有仓库 Releases、代理、恢复下载
- 恢复前快照、路径穿越校验、桌面登录态可选恢复
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

- Go：配置、任务、恢复编排、GitHub、代理、计划任务
- Rust：文件暂存、7z 编排、SHA-256、age 密钥封装、解压安全校验
- 7-Zip：固定版本与 SHA-256，随包分发，附 License
- token / 代理密码：Windows 凭据管理器
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
dist\DSHBackup-1.0.0-win64.zip
```

构建脚本会校验 7za SHA-256、构建静态 CRT Rust sidecar、运行 Wails 构建并组装便携目录。

## 测试

```powershell
# Go
cd app\DSHBackup
go test ./...

# Rust
cd app\DSHBackup\sidecar
cargo test
```

已验证：Rust 路径安全/参数白名单/age 封装；Go 配置、manifest、7z、伪 GitHub 服务上传/列表/下载/设备流；真实 DSH 数据备份、校验、恢复、加密备份与解密恢复。

## 安全边界

- 默认本地模式不初始化网络客户端
- 云端归档始终加密；私有仓库不等于端到端加密
- 私钥丢失后加密备份无法恢复；换机恢复需复制 `data\keys`
- 不记录 token、归档密码、私钥、Cookie、API Key、代理密码
- 7z 通过参数数组调用，不经过 shell；只允许白名单高级参数
- 恢复先校验 SHA-256，再解压到受控暂存目录，拒绝路径逃逸和重解析点

## 已知限制

- GitHub Device Flow 需要用户自己的 OAuth App Client ID（启用 Device Flow）；也可使用 fine-grained PAT
- 真实 GitHub 账号的端到端上传需要用户凭据；代码路径已通过伪 GitHub 服务测试
- 计划任务只在应用运行时触发
- 不做项目代码备份、多账号、增量/去重；首版不做 Linux UI
- 不提供自动更新、代码签名、VSS、ACL 保存
- 桌面 Cookie 受 Windows 用户加密限制，换机可能需要重新登录

## 许可

本项目代码为内部构建，未声明开源许可证。随包 7-Zip 使用 LGPL 2.1+（含 unRAR 限制），第三方组件许可证见 [app/DSHBackup/packaging/THIRD_PARTY_NOTICES.txt](app/DSHBackup/packaging/THIRD_PARTY_NOTICES.txt)。
