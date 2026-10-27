# DSH Backup

Windows 便携版 DSH 核心数据备份工具。只备份 DSH 的凭据、会话、附件、插件和用量，以及可选的桌面登录态；不读取项目代码。

- 设计文档：`../../DESIGN.md`
- 便携版产物：`../../dist/DSHBackup/`

## 技术栈

- Wails 2.15 + WebView2 + Vue 3 + TypeScript
- Go 业务层：配置、任务、恢复编排、GitHub、代理、计划任务
- Rust sidecar：文件暂存、7z 进程编排、SHA-256、age 密钥封装、解压路径校验
- 官方 7-Zip 26.05 CLI（x64，随包分发并校验 SHA-256）
- GitHub Device Flow / PAT；私有仓库 Releases 存放加密归档

## 构建要求

- Windows 10/11 x64
- Go 1.26+
- Node.js 24+ / npm
- Rust stable MSVC + Visual Studio Build Tools（C++ 桌面开发）
- Wails CLI 2.15：`go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- 7-Zip 26.05 Extra 解压到 `../../tools/7zip/7z2605-extra`

## 构建便携版

```powershell
cd app\DSHBackup
powershell -ExecutionPolicy Bypass -File .\build.ps1
```

输出：

- `dist\DSHBackup\DSHBackup.exe`
- `dist\DSHBackup\dsh-backup-core.exe`
- `dist\DSHBackup\tools\7za.exe`
- `dist\DSHBackup-1.0.0-win64.zip`

便携版目录自带 README、第三方许可证和 7-Zip 来源说明。

## 开发

```powershell
cd app\DSHBackup
wails dev
```

浏览器联调（可调用真实 Go 后端）：

```powershell
wails dev -browser
# 然后打开 http://localhost:34115
```

前端模拟数据预览（不启动 Go）：

```powershell
cd frontend
npm install
npm run dev
# 打开 Vite 地址；无 Wails runtime 时自动使用 mock
```

可用预览路由：

- `?onboarding=1`：首启
- `?preview=settings&tab=security`
- `?preview=history`
- `?preview=restore`
- `?preview=github`
- `?preview=progress`

这些路由只在浏览器 mock 模式下生效。

## 测试

```powershell
# Go
go test ./internal/...

# Rust
cd sidecar
cargo test
```

Go 集成测试需要先构建 `sidecar\target\debug\dsh-backup-core.exe` 并存在 `..\..\tools\7zip\7z2605-extra\x64\7za.exe`；缺失时测试会跳过。

## 代码结构

```text
main.go                  Wails 入口、WebView2 数据目录、单实例
app.go                   启动、Bootstrap、任务状态
api.go                   Wails DTO
actions_backup.go        备份/恢复/验证/删除/云端适配
actions_github.go        Device Flow、PAT、仓库选择
actions_settings.go      配置、密钥安全检查、代理、诊断
scheduler.go             每日/每周计划
internal/                业务与底层适配
frontend/                Vue 3 UI
sidecar/                 Rust JSON-RPC sidecar
packaging/               便携版说明与第三方许可证
```

## 安全边界

- 默认仅本地，不初始化网络客户端
- 云端归档强制 7z AES-256 + 文件头加密；每个归档独立随机密码
- 密码由本地 age X25519 公钥封装，私钥保存在 `data\keys`，不上传
- token / 代理密码保存在 Windows 凭据管理器
- 日志脱敏；不记录 token、归档密码、私钥、Cookie、API Key
- 7z 通过参数数组调用，不经过 shell；高级参数白名单双重校验
- 恢复先校验 SHA-256，再解压到受控暂存目录；拒绝路径穿越和重解析点
- 私钥丢失后加密备份不可恢复；换机恢复需同时复制 `data\keys`

## 已知限制

- Device Flow 需要用户自己的 GitHub OAuth App Client ID（启用 Device Flow）；也可使用 fine-grained PAT
- 计划任务只在应用运行时触发，不注册 Windows 计划任务
- 不提供自动更新、代码签名、VSS、ACL 保存、增量/去重、Linux UI
- 桌面 Cookie 受 Windows 用户加密限制；换机可能需要重新登录
