DSH Backup 1.0.0 - 便携版

DSH Backup 只备份 DSH 的核心数据：登录态、API Key、会话、工作区登记、附件、用量，以及可选的桌面端登录态。
不备份项目代码、日志和缓存。

使用方法
--------
1. 将整个 DSHBackup 文件夹解压到可写目录，不要放到 C:\Program Files 等只读目录。
2. 双击 DSHBackup.exe。首次启动按向导选择备份方式和本地目录。
3. 默认只做本地备份，不要求 GitHub 登录，也不会发起网络请求。
4. “立即备份”完成后，归档位于 DSHBackup\backups，另有同名 .manifest.json 记录 SHA-256 和加密信息。

云端备份（可选）
----------------
- 在“设置 → 云端”连接 GitHub，并使用私有仓库的 Releases 存放加密归档。
- Device Flow 需要你自己的 GitHub OAuth App Client ID；也可以在高级选项里使用带 repo 权限的 fine-grained PAT。
- token 和代理密码保存在 Windows 凭据管理器，不写入配置文件或日志。
- 云端归档始终使用 7z AES-256 + 文件头加密。

本地加密与密钥
--------------
- 默认关闭；开启后会在 data\keys 生成一对本地公钥/私钥。
- 每个归档使用独立随机密码，密码由公钥封装后写入 manifest；恢复时自动用私钥解开。
- 私钥丢失或应用目录被删除后，已有加密备份无法恢复，应用不提供恢复密钥。
- 换机恢复必须同时带上 data\keys 目录；只把私有仓库克隆到新机器是不够的。
- 如果要把备份放到共享目录、U 盘或网盘，建议开启本地加密。

恢复
----
- 请先关闭 DSH。默认会先创建一份“恢复前快照”，再覆盖现有 .dsh 和可选的桌面登录态。
- 同机同 Windows 用户恢复桌面 Cookie 成功率最高；换机可能需要重新登录。
- 恢复完成后重新启动 DSH。

目录说明
--------
DSHBackup.exe            主程序
dsh-backup-core.exe      Rust sidecar（底层文件/7z 编排）
tools\7za.exe            随包分发的官方 7-Zip CLI
tools\License.txt        7-Zip 许可证
backups\                 默认本地备份目录（首次备份时创建）
data\config\             非敏感配置
data\keys\               本地公钥/私钥（只在本机，不随云端上传）
data\logs\               运行日志（已脱敏）
data\temp\               任务临时文件（启动时清理）
data\webview2\           WebView2 用户数据

注意：删除整个应用目录会连同默认本地备份和本地私钥一起删除。重要备份请另存到其他磁盘。
更新时请替换程序与 tools，不要删除 data 和 backups。

系统要求
--------
- Windows 10/11 x64
- Microsoft Edge WebView2 Runtime（Windows 11 通常已内置）

隐私与边界
----------
- 默认模式不联网；日志不记录 token、归档密码、私钥、Cookie、API Key 或代理密码。
- 只解压本应用生成的归档，并校验路径穿越和重解析点。
- 不支持项目代码备份、多账号、团队共享、GitHub Enterprise。
- 本工具不提供自动更新；便携版请手动替换新版本。
