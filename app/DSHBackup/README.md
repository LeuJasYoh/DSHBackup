# DSH Backup

Windows 便携版 DSH 核心数据备份工具。备份整个 `.dsh` 目录（凭据、用量、登录态与配置、会话、附件、角色与语音），
只排除 `logs`、`cache`、`electron-user-data`；外加可选的桌面登录态，不读取项目代码。

- 设计文档：`../../DESIGN.md`
- 便携版产物：`../../dist/DSHBackup/`

## 技术栈

- Wails 2.15 + WebView2 + Vue 3 + TypeScript
- Go 业务层：配置、任务、备份/恢复编排、计划任务
- Rust sidecar：文件暂存、7z 进程编排、SHA-256、age 密钥封装、解压路径校验
- 官方 7-Zip 26.05 CLI（x64，随包分发并校验 SHA-256）
- 备份与恢复不联网；可选在启动时只读检查 GitHub 发行版，可在设置中关闭

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
- `dist\DSHBackup-<version>-win64.zip`

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

可用的 mock 查询参数（只有这两个，其余参数不生效）：

- `?onboarding=1`：清空归档列表并回到首启两步引导
- `?update=1`：让只读更新检查返回一个「有新版」的结果，用来验侧栏徽标和顶部提示

界面本身没有 URL 路由，总览 / 归档历史 / 设置靠侧栏切换；设置页是一整篇文档加左侧目录导轨，
跳转用点击目录项，不用查询参数。这些 mock 只在浏览器 dev 模式下生效，生产构建会被整块消除。

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
actions_backup.go        备份/恢复/验证/删除
actions_settings.go      配置、密钥安全检查、诊断
scheduler.go             每日/每周计划
internal/                业务与底层适配
frontend/                Vue 3 UI
sidecar/                 Rust JSON-RPC sidecar
packaging/               便携版说明与第三方许可证
```

## 安全边界

- 不初始化网络客户端，不存储账号凭据
- 本地加密可选：7z AES-256 + 文件头加密；每个归档独立随机密码
- 密码由本地 age X25519 公钥封装，私钥保存在 `data\keys`
- 日志脱敏；不记录归档密码、私钥、Cookie、API Key
- 恢复前快照按本次实际覆盖的目录创建；恢复到其他目录不触碰系统桌面登录态
- 7z 通过参数数组调用，不经过 shell；高级参数白名单双重校验
- 恢复先校验 SHA-256，再解压到受控暂存目录；拒绝路径穿越和重解析点
- 私钥丢失后加密备份不可恢复；换机恢复需同时复制 `data\keys`

## 已知限制

- 计划任务只在应用运行时触发，不注册 Windows 计划任务
- 不自动安装更新（可选只读检查 GitHub 发行版并提示）、不提供代码签名、VSS、ACL 保存、增量/去重、Linux UI
- 桌面 Cookie 受 Windows 用户加密限制；换机可能需要重新登录
