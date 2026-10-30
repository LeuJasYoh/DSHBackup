# DSH Backup 后续工作路线图

> 交接对象：接手本项目实现工作的工程师或大模型。
> 写作时间：2026-10-30。写作基线：`452e9a1`（v1.2.3）。
> **2026-10-30 更新**：R1–R4、R6、R7 已按本文档实施并本地构建为 **1.3.0**（未推送、未发布）；
> 第 3 节的表格与第 14.2 节已同步为实际结果。仍在开放的是 **R5 审计遗留边界**（第 8 节）与**发布本身**。
> 前置阅读：[REPAIR_HANDOFF.md](REPAIR_HANDOFF.md)（上一轮审计与修复的完整记录，已完成）。

---

## 0. 这份文档怎么用

1. 先读第 2 节「硬约束」。这一节是红线，任何阶段的实现都不得违反；违反即视为返工。
2. 再读第 3 节「待办总览」，确认你要做的阶段编号（R1…R8）以及它的前置阶段。
3. 进入具体阶段时，只读该阶段的小节。每个阶段都自带：目标、非目标、数据模型、代码落点、边界清单、测试矩阵、验收标准、可独立提交的拆分步骤。
4. 阶段内所有「已确认」条目都是用户已经点头的设计决定，不要再重复询问；所有「待定」条目必须先向用户确认再动手。
5. 每完成一个可提交步骤，就本地提交一次，不要攒到最后。提交信息描述「为什么」，不描述「改了哪个文件」。
6. 不要在文档里写「应该已通过」。**通过与未通过都必须是实测结果**，并附命令与输出摘要。

---

## 1. 当前基线

### 1.1 版本与产物

| 项 | 值 |
| --- | --- |
| 版本 | `1.2.3`（`app/DSHBackup/wails.json` productVersion、`frontend/package.json` version、`build.ps1` 默认值、`app.go` Version 常量） |
| 最近提交 | `452e9a1 Close the audit gaps that could silently lose data`（58 files, +4231/−697） |
| 试用包 | `app/DSHBackup/dist/DSHBackup-1.2.3-win64.zip`，9 个运行文件，已校验 ZIP 内 SHA 与解压后 exe 可启动 |
| 已验证 | `go test ./... -race` 全绿；`cargo test` 10 项全绿；`cargo clippy --locked --all-targets -- -D warnings` 与 `cargo fmt --check` 全绿；`vue-tsc --noEmit && vite build` 全绿 |
| 未做 | 未 push、未 tag、未发 GitHub Release；无真实用户数据参与验证 |

### 1.2 上一轮已闭环的能力（不要再改动语义）

- 恢复提交走「目标旁短命候选文件 + 句柄改名替换」，复制/校验失败不截断旧文件，只读与隐藏属性保留。
- 采集完整性：清单回读与暂存清单逐条比对（文件、大小、空目录），不完整即标记 `captureStatus = partial`，压缩告警时拒绝生成保护快照。
- 密钥能力拆分：公私钥不匹配 → 阻止新建加密备份，但私钥仍可用于解开旧归档（`sidecar/src/keys.rs` 的 `inspect_pair` / `info` / `wrap_password` / `unwrap_password`）。
- 真实路径边界：`internal/pathguard` 用 `GetFinalPathNameByHandle` 解析卷号 + 文件索引，junction / 硬链接 / 写穿全部拦下。
- 取消语义：JSON-RPC 协议版本 2，按 request id 注册取消令牌，令牌先于 worker 生成；sidecar 挂在 kill-on-close 的 Job Object 上。
- 调度语义：应用关闭期间错过的时刻不追补；运行中错过的合并为最近一次；持久化失败即暂停调度。
- 清单语义：重复别名共享一个保留名额；按真实时刻（Instant）排序，不再按字符串排序。
- 压缩：六档 `store/fast/quick/balanced/high/max` ↔ `-mx=0/1/3/5/7/9`，新装机默认 `store`；`-mx` 已从高级参数白名单移除，旧配置里的 `-mx` 由 `Config.migrateLegacyMX()` 迁移成档位。
- 打包：`build.ps1` 在独立 GUID 暂存目录组装，ZIP 只含运行文件，不再删除运行目录里的 `data`/`backups`。

### 1.3 代码地图（速查，行号对应 `452e9a1`）

后端：

| 位置 | 内容 |
| --- | --- |
| `app/DSHBackup/app.go:119` | `startup`：装配 sidecar、7z 预校验、配置加载、`scheduleBase` 设定、`markReady` |
| `app/DSHBackup/app.go:287` | `GetBootstrap`：一次性返回配置 + 归档列表 + 源状态 + 备份目录身份（**重操作**） |
| `app/DSHBackup/app.go:547` | `beginTask`：全局单任务槽，占用期间其他任务直接失败 |
| `app/DSHBackup/app.go:584` | `emitStateChanged(reason)`：前端收到后 350ms 防抖调 `GetBootstrap` |
| `app/DSHBackup/actions_backup.go:260` | `DeleteLocalSnapshot(request)`：当前删除入口，内部先 `awaitReady(30s)` 再 `beginTask("delete")` |
| `app/DSHBackup/internal/backup/manager.go:716` | `DeleteLocal`：`pathguard.Contains` 后 `os.Remove` 归档 + 全部清单别名 |
| `app/DSHBackup/internal/backup/manager.go:985` | `applyLocalRetention`：跳过 `KindPreRestore`、非 `ready`、`partial`，超额的直接 `os.Remove` |
| `app/DSHBackup/internal/manifest/manifest.go:197` | `Scan`：`os.ReadDir` **单层、不递归**，第 210 行跳过目录 → 备份目录内的子目录天然不出现在归档列表 |
| `app/DSHBackup/internal/config/config.go:47` | `Retention{Local int}`；第 164 行默认 7；第 222 行校验 1..99 |
| `app/DSHBackup/internal/apppath/paths.go:110` | `TempTaskDir` + 第 128 行 `.dshbackup-task` 标记；第 132 行 `CleanTemp` 只清带标记的目录 |
| `app/DSHBackup/main.go` | Wails `options.App`：`SingleInstanceLock` + `OnSecondInstanceLaunch` 目前只发一条 warning |
| `app/DSHBackup/internal/sidecar/client.go` | 协议 2、取消宽限 `cancelGrace = 5s`、Job Object 管理 |

前端：

| 位置 | 内容 |
| --- | --- |
| `frontend/src/store.ts:334` | `const UNDO_WINDOW = 6000`（R1 要删掉的东西） |
| `frontend/src/store.ts:348` | `commitPendingDelete`：6 秒后才真正调用后端，失败/有任务时静默丢弃 |
| `frontend/src/store.ts:386` | `deleteSnapshot`：`if (isBusy.value \|\| pendingDelete) return` → 一次只允许一个删除 |
| `frontend/src/store.ts:65` | `snapshots` 计算属性靠 `pendingSnapshotId` 过滤显示（服务端列表仍是事实） |
| `frontend/src/store.ts:69` | `isBusy` 含 `store.deleting` → 删除期间全站按钮禁用 |
| `frontend/src/store.ts:108` | `refresh()` = 整个 `GetBootstrap` 重取 → 用户口中的「整个页面全部刷新」 |
| `frontend/src/store.ts:165` | `api.onStateChanged(() => scheduleRefresh())` |
| `frontend/src/components/SidebarNav.vue:30` | 侧栏导航：总览 / 归档历史 / 设置；第 68 行 `sidebar-spacer`，第 75 行 `github-card` |
| `frontend/src/components/SnapshotTable.vue` | 行内操作：校验 / 恢复 / 打开位置 / 删除；`props.compact` 控制精简列 |
| `frontend/src/components/HelpModal.vue` | 现有帮助：7 个 `<section>`，每段一到两句话（R4 要扩写） |
| `frontend/src/lib/types.ts` | `Page = "dashboard" \| "history" \| "settings"`；`SettingsSection` 14 个字面量（R1/R3 都要扩） |

Wails v2.15.0 关键事实（已在模块缓存里逐个确认，不要凭印象）：

- `pkg/options/options.go:64` 有 `OnBeforeClose func(ctx context.Context) (prevent bool)` → 关闭拦截可用。
- `pkg/runtime/window.go:61/67` 有 `WindowShow` / `WindowHide`，`pkg/runtime/runtime.go:64` 有 `Quit`。
- `pkg/options/options.go:196` 是 `SecondInstanceData{Args, WorkingDirectory}`。
- **托盘不可用**：`internal/menumanager/traymenu.go:109` 定义了 `AddTrayMenu`，但全仓库除自身递归（第 135 行）外没有任何调用点，`pkg/options` 里搜不到任何 `Tray` 字段。结论：Wails v2.15 没有 Windows 托盘后端，托盘必须自己实现。
- `golang.org/x/sys v0.49.0` 已是直接依赖，托盘可用 `windows.NewLazySystemDLL("shell32.dll").NewProc("Shell_NotifyIconW")` 直接挂。

### 1.4 「现在删除」为什么会让人觉得难用（复现点）

用户反馈的三条，全部对应到代码：

1. **「等半天才能再删另一个」** → `store.ts:387` 的 `pendingDelete` 单例 + `isBusy` 含 `store.deleting` + 后端 `beginTask("delete")` 抢全局任务槽。三处任一都能拦住第二次删除。
2. **「等半天才真的删」** → 前端 `UNDO_WINDOW = 6000` 倒计时结束才发 RPC；后端 `DeleteLocalSnapshot` 入口还先 `awaitReady(30s)`、再 `manifest.Scan`、再 `pathguard.Identity`。
3. **「归档页面还会刷新」** → 删除成功后 `commitPendingDelete` 调 `refresh()`，即完整重取 `GetBootstrap`（含源扫描、DSH 进程检测、身份解析）。

R1 的设计目标是把这三条一次性解决：**同卷重命名 = 移入回收站（瞬时），列表做增量更新（不重取全站），删除不占用任务槽且不排他。**

---

## 2. 硬约束（红线）

任何阶段都不得违反；如果实现与约束冲突，停下来问用户，不要自己放宽约束。

1. **纯本地**：不引入云端、不引入账号、不上传任何数据。唯一允许的网络请求仍是启动时只读检查 GitHub 发行版，且可在设置中关闭。
2. **可移植**：所有状态必须在应用目录内（`data/`、`backups/`、`tools/`）。不允许写注册表、不允许写 `%APPDATA%`（WebView2 用户数据目录已是既有例外，保持现状）。
3. **不夸大能力**：不承诺 ACL/权限保留、不承诺事务性回滚、不做 VSS、不做增量去重、不做 DPAPI、不做恢复码。文档、帮助、提示文案都要与代码实际行为一致。
4. **绝不记录敏感内容**：日志、诊断包、错误文案中不得出现归档密码、私钥、Cookie、API Key。
5. **不自动删除无法确认归属的文件**：外部文件只报告，不代客清理（`CleanTemp` 的 `.dshbackup-task` 标记是既有范式，回收站的归属判定要沿用同样强度的证据）。
6. **破坏性操作要能撤销或有明确二段确认**：移入回收站要瞬时（靠可恢复性取代倒计时）；从回收站彻底删除必须显式确认。
7. **一个设置只有一个入口**：不要在两处提供同一控件或同一开关（用户明确要求过）。新增开关时先确认现有页面没有同源入口。
8. **UI 先出静态预览再落实现**：涉及布局改动时，先给出可读的线框/静态稿让用户确认，再写代码。
9. **交付前必须实测**：重新构建并启动 `dist/DSHBackup/DSHBackup.exe` 验证界面与服务行为；只看类型检查/单测通过不算完成。
10. **未获授权不 push / 不打 tag / 不发 Release**：当前只授权了本地提交。

---

## 3. 待办总览与建议顺序

| 编号 | 名称 | 规模 | 前置 | 面向用户价值 | 状态 |
| --- | --- | --- | --- | --- | --- |
| R1 | 回收站 | L | 无 | 极高（当前体验最差的一环） | **已完成**（`e7bb539`，1.3.0） |
| R2 | 自研选择器组件 | M | 无 | 中（视觉一致性） | **已完成**（`95220d3`） |
| R3 | 关闭行为 + 系统托盘 | L | R1 | 高（常驻使用形态） | **已完成**（`a70c3fb`），真机验证三种关闭行为 |
| R4 | 应用内帮助扩写 | M | R1、R2、R3 | 中 | **已完成**（12 节带目录长文档） |
| R5 | 审计遗留边界（W8 余项等） | L | 无 | 低可见、高稳健 | **R5-1/R5-3 已完成（1.3.1）**；R5-2 用户明确跳过；R5-4/5/6 明确不做；R5-7 由集成测试覆盖 |
| R6 | 前端自动化测试基建 | M | R1 | 低可见、高杠杆 | **已完成**（vitest，18 个用例，已接入 build.ps1） |
| R7 | 文档与打包（README 形态等） | S | R1–R4 | 中 | **已完成**（README / packaging / DESIGN 第 29 章） |
| R8 | 版本与发布流程 | S | 全部 | 高 | 版本定为 **1.3.0**，已本地构建；未推送、未发布 |

建议实施顺序（历史记录）：**R1 → R2 → R3 → R6 → R5 → R4 → R7 → R8**。

### 3.1 实施记录（2026-10-30）

1.3.0 的 R1–R4、R6、R7 已在本地提交并构建，提交依次为：
`e7bb539`（回收站）→ `95220d3`（选择器）→ `a70c3fb`（托盘与关闭行为）；
文档与测试随后并入同一分支。**未 push、未打 tag、未发布**。
R5 的遗留边界仍然是开放的，按第 8 节的表逐条处理，不要误以为整个路线图已清空。

---

## 4. R1 回收站

### 4.1 目标与非目标

目标：

- 删除归档 = **瞬时移入回收站**，同卷重命名，不复制字节。
- 允许**连续、多次**删除，不被前一次删除阻塞，不占用备份/恢复任务槽。
- **没有倒计时**，删除后不出现「6 秒内可撤销」这种时间压力。
- 删除后**不整页刷新**：只让归档列表与回收站两处状态增量更新。
- 保留策略淘汰的旧归档**也进回收站**，不是直接 `os.Remove`。
- 回收站项可在保留期内**恢复到当前备份目录**，也可**立即彻底删除**。
- 保留时长是**设置项**，默认 7 天。
- 侧栏新增「回收站」入口，占据现在「设置」的位置；「设置」按钮下移（用户已确认）。

非目标：

- 不做跨目录的全局回收站；不做「还原到原始位置」这类路径记忆（恢复目标恒为当前备份目录）。
- 不做回收站内预览/解包、不做批量选择（第一期只给单条操作 + 清空）。
- 不做倒计时/自动撤销 toast。
- 不引入 Windows 资源管理器回收站，不与其交互。

### 4.2 目录与数据模型

在备份目录内建立隐藏子目录：

```
<backupDir>/
  dsh-backup-2026-10-30T09-12-03-827-a1b2c3.7z
  dsh-backup-...-manifest.json
  .dshbackup-trash/                     ← 隐藏属性，单层
    <snapshotId>/                       ← 每项一个目录，天然避免重名冲突
      dsh-backup-...-827-a1b2c3.7z      ← 原归档文件名
      dsh-backup-...-manifest.json      ← 原清单（含全部别名文件）
      dsh-backup-...manifest-1.json     ← 第二个别名清单
      trash.json                        ← 本项的元数据（新增，不由 manifest 推导）
```

依据（已实测确认）：

- `manifest.Scan` 用 `os.ReadDir` 单层扫描并在 `internal/manifest/manifest.go:210` 跳过目录，因此 `.dshbackup-trash` 里的东西**天然不会**出现在归档列表；不需要给 Scan 加过滤逻辑，但**必须加测试锁死这个前提**（第 4.15 节 T-R1-01），否则将来有人把 Scan 改成递归就会静默双列。
- 回收站与归档同卷 → `os.Rename` 是元数据操作，常数时间，与归档体积无关（这是「瞬间完成」的技术前提）。
- 每项一个子目录，可以直接把整棵子树当作一个操作单元，且 `pathguard.Contains(itemDir, trashRoot)` 就是唯一的路径边界。
- Windows 隐藏属性：`os.MkdirAll` 不会设置 `FILE_ATTRIBUTE_HIDDEN`。需要在 `internal/platform/platform_windows.go` 增加 `HideIfWindows(path string)`（用 `golang.org/x/sys/windows` 的 `SetFileAttributes`），失败只告警不阻断——隐藏只是观感，安全性靠 `Scan` 跳目录 + 归属标记。

### 4.3 `trash.json` 元数据

只存「恢复所需 + 展示所需 + 归属证据」，不复制清单内容：

```json
{
  "schemaVersion": 1,
  "snapshotId": "a1b2c3d4e5f6",
  "reason": "manual",
  "movedAt": "2026-10-30T09:12:03.827+08:00",
  "expiresAt": "2026-11-06T09:12:03.827+08:00",
  "retentionDays": 7,
  "archiveName": "dsh-backup-2026-10-30T09-12-03-827-a1b2c3.7z",
  "archiveSize": 12884901888,
  "archiveSha256": "…",
  "manifestNames": ["dsh-backup-…-manifest.json"],
  "encrypted": true,
  "keyId": "age1…",
  "kind": "manual",
  "captureStatus": "complete",
  "originBackupDir": "D:\\Backups\\DSH",
  "originBackupDirIdentity": "\\??\\Volume{…}\\",
  "appVersion": "1.2.4"
}
```

字段约束：

- `reason`：`manual`（用户点删除）| `retention`（保留策略淘汰）| `prune`（回收站到期清理失败后的补记，第一期可不产生）。**用户要求保留策略淘汰的旧归档也进回收站**，因此 `applyLocalRetention` 不再直接 `os.Remove`。
- `expiresAt` = `movedAt + retentionDays`，其中 `retentionDays` 取**移入那一刻**的配置值。写死进文件，后续用户调大/调小设置不回改历史项的到期时间（避免「把保留期改成 90 天让已经 89 天的项复活」这种反直觉行为）。
- `originBackupDirIdentity` 用 `pathguard.Identity` 得到，是**归属证据**：只有当前备份目录身份与之一致、或当前备份目录本身就等于 `originBackupDir` 时，才允许对该项做自动清理。沿用 `apppath` 的 `.dshbackup-task` 标记思路（`internal/apppath/paths.go:128`）：**没有可信归属标记的目录一律不动。**
- 写入方式：先用 `writeFile` + 读回校验，或复用 `sidecar/src/util.rs` 的候选-改名提交范式在 Go 侧实现等价物。禁止直接 `os.WriteFile` 到最终路径后再假定成功。

### 4.4 归属与发现规则（重要，最容易出错的地方）

场景：用户把备份目录从 `D:\Backups\A` 改成 `E:\Backups\B`，A 里留下了回收站。

规则（**待定 D-1**，下面是最建议的一档）：

1. 应用只在**当前配置的备份目录**内读写回收站。列表、恢复、清空、到期清理都只作用于它。
2. 其他目录里的遗留回收站**不自动删除**（红线 5）。在「设置 → 备份目录」小节显示一条提示：「上次使用的备份目录 `<old>` 中仍有 N 项回收站内容（约 X）未清理。本应用不会自动删除其他目录的内容。」并提供「打开该目录」。
3. 该提示的数据来源：在 `data/` 下记一个 `trash-known.json`（已知创建过的回收站绝对路径 + 身份列表），只增不删；展示时逐项 `Stat`，读不到就标注「不可访问」。

> 需要用户确认的点：是否接受「遗留回收站永不自动删除」（推荐），还是希望「设置里提供一次性清理按钮」（也安全，但要额外确认框）。

### 4.5 移入（=删除）流程

后端新入口（**替换** `DeleteLocalSnapshot` 的语义，或保留旧名并改成移入；见 4.10）：

```
MoveToTrash(request MoveToTrashRequest) error
  MoveToTrashRequest{ SnapshotID, ExpectedRootIdentity, ExpectedBackupDir, ExpectedSHA256 }
```

步骤（顺序即正确性，不要重排）：

1. `awaitReady`（保持现有的短超时，见 4.7 关于 `beginTask` 的说明）。
2. 取配置，`pathguard.Identity(cfg.BackupDir)`；与 `ExpectedRootIdentity` 不符 → 拒绝（沿用 `actions_backup.go:277` 的现有文案逻辑）。
3. `manifest.Scan` 定位快照；`ExpectedSHA256` 与 `Snapshot.SHA256` 不符 → 拒绝；`Status == "conflict"` → 拒绝（现有行为保留，冲突清单必须人工处理）。
4. 边界校验：`pathguard.Contains(snapshot.LocalPath, cfg.BackupDir)` 且 `Contains(ManifestPath, cfg.BackupDir)`。
5. **重解析点检查**：对归档与每个清单文件 `os.Lstat`，Windows 下拒绝 `ReparsePoint` 属性（避免把 junction 当文件移走）。
6. 确保 `.dshbackup-trash/<snapshotId>/` 存在（`MkdirAll`，权限 0o700 语义在 Windows 上由目录继承，不承诺 ACL —— 红线 3）。
7. **目标不存在检查**：若该 `<snapshotId>` 子目录已存在且非空 → 拒绝并提示「该快照已在回收站中」。这是幂等保护，绝不覆盖。
8. 逐个 `os.Rename` 归档与全部清单别名进子目录。
9. 写 `trash.json`，读回校验。
10. **失败即回滚**：如果第 9 步写元数据失败，把已移入的文件原样改名回备份目录（用 no-replace 语义；若原名已被占用则保留在回收站并报错让人工处理，同时返回明确路径）。回滚失败必须返回带路径的错误，不静默。
11. 记日志 `trash.add`，字段只有 id/size/reason，无敏感内容。
12. `emitStateChanged("trash-add")` —— 前端据此做**增量**更新（4.12）。

并发与阻塞：

- **不得调用 `beginTask`**。移入是纯元数据操作，不该阻止备份/恢复，也不该被它们阻止。改为在 App 上加一个 `trashMu sync.Mutex` 串行化回收站自身的写操作即可。
- 允许前端连点多次：后端每次都是独立、幂等、瞬时的事务。
- 移入过程中并发备份：可能让 `applyLocalRetention` 看到刚被移走的快照。处理办法是 `applyLocalRetention` 在删除（现为移入）前先重新 `Scan` 并在移入失败时把错误收进 warnings（现有代码在 `manager.go:1037` 已有「失败即保留清单」的语义，沿用）。

### 4.6 从回收站恢复

```
RestoreFromTrash(trashItemID string) error
```

- 目标目录 = **当前配置的备份目录**（用户已确认：「恢复时恢复到当前备份目录呀，也方便恢复后的使用」）。
- 语义 = 把子目录里的每个文件改回 `backupDir/<原文件名>`，全部成功后删 `trash.json` 再删子目录。
- **绝不覆盖**：改名前逐个检查目标名是否已存在，任一存在即整体拒绝并提示「备份目录中已有同名归档」，回收站项保持不变。第一期不做自动重命名（避免制造第二个身份同一个归档的清单，那会撞上 `manifest.Scan` 的 conflict 逻辑）。
- 全部文件必须用同一批「原名」，即从 `trash.json.manifestNames` + `archiveName` 读取，不重新猜测。
- 跨卷（备份目录被换到别的卷）：`os.Rename` 返回 `ERROR_NOT_SAME_FILE` 时，第一期直接拒绝并提示「回收站与当前备份目录不在同一磁盘分区，无法瞬时恢复。请先把备份目录切回原分区，或在回收站中彻底删除后重新备份。」**不做静默复制回退**——复制一份 12 GB 归档是长任务，会伪装成界面卡死，属于产品事故。

### 4.7 永久删除与清空

- 单条彻底删除：必须走现有 `ConfirmModal`（`store.modals.confirm`），文案要点是「不可恢复；DSH 原数据不受影响」。
- 清空回收站：确认框里显示「N 项，约 X」；实现为逐项移除，任一项失败就停止并把失败项列入返回 warnings，不把整批当原子操作（避免一个被占用的文件让清空永远失败）。
- 移除实现：`pathguard.Contains(itemDir, trashRoot)` → 拒绝 reparse → `os.RemoveAll(itemDir)`。**只允许作用于 `.dshbackup-trash` 直系子项**，任何深度、任何软链都拒绝。

### 4.8 到期清理

- 触发点三处：应用启动 `markReady` 之后一次；每次回收站写操作后一次；`GetBootstrap` 命中时如果发现有已过期项则顺手处理（用 `trashMu` 保护，绝不阻塞 `GetBootstrap` 返回）。
- **不要**新增长周期定时器（应用可能常驻托盘，见 R3；定时器会与任务槽和磁盘 IO 撞上）。若 R3 落地后需要常驻，则改为「每次 `OnBeforeClose` 之外的唤醒事件 + 每小时一次」的低频 timer，并复用调度器的 `reserveScheduledTask` 互斥思路（`app/DSHBackup/scheduler.go`）。
- 清理条件：`now >= expiresAt` **且** `originBackupDirIdentity` 与当前备份目录身份一致（或当前备份目录路径完全等于 `originBackupDir`）。不满足归属条件的项**永不自动删除**（红线 5），只在界面标注「不属于当前备份目录，不会自动清理」。
- 清理失败（文件被占用等）：保留项，标注原因，不打断其他项处理。
- 用户要求：**没有倒计时**。因此界面上不显示「xx 分钟后清除」，只显示「剩余 N 天」；`expiresAt` 精确到日。

### 4.9 与保留策略、备份目录切换的交互

- `applyLocalRetention`（`internal/backup/manager.go:985`）改为「移入回收站」，函数语义变成 prune。返回值的字段名从 `deleted` 改成 `moved`，备份完成时的提示文案随之改为「已按保留策略把 N 个旧归档移入回收站，7 天内可在回收站找回」。
- 淘汰项同样受回收站保留期约束：**这意味着「本地保留 7 个」不再是硬上限的磁盘占用**。必须让用户看得见这一点（回收站卡片显示占用与项数），否则会引发「为什么磁盘没腾出来」的误解。这是本阶段最重要的产品风险，见第 13 节风险 RK-1。
- 备份目录切换时：`saveConfigInternal`（`actions_settings.go:80`）已有 `pathguard.Contains` 检查。新增：如果旧目录存在回收站且非空，保存成功后返回一条 warning（对应 4.4 规则 2）。
- 磁盘空间预检：备份前 `checkBackupDestination` 已有空间检查。**要把回收站目录计入已占用**——因为它就在同一卷同一目录树里，现有按目录用量统计天然包含，只需确认 UI 上「本地占用」口径写明「含回收站」。当前口径来自 `localUsage`（`frontend/src/store.ts`）与 `app.go` 的 pathsInfo，改动时在标签上说清楚，不要改算法。

### 4.10 Go 层 API 清单

新增/修改的绑定方法（全部在 `app/DSHBackup` 根包，前端通过 `wailsjs` 调用）：

| 方法 | 说明 | 替换关系 |
| --- | --- | --- |
| `MoveToTrash(req MoveToTrashRequest) error` | 4.5 | 取代 `DeleteLocalSnapshot` |
| `ListTrash() (TrashList, error)` | 项、大小、剩余天数、归属状态、合计 | 新增 |
| `RestoreFromTrash(snapshotID string) error` | 4.6 | 新增 |
| `DeleteTrashItem(snapshotID string, confirmToken string) error` | 4.7 | 新增 |
| `EmptyTrash() (TrashPruneResult, error)` | 4.7 | 新增 |
| `PurgeExpiredTrash() error` | 4.8，前端不主动调，启动与写操作内部调用 | 新增 |
| `OpenTrashFolder() error` | 在资源管理器打开回收站目录，沿用 `OpenLocalSnapshot` 的调用方式 | 新增 |

`TrashList` 结构（`api.go`）：

```go
type TrashItem struct {
    SnapshotID    string `json:"snapshotId"`
    ArchiveName   string `json:"archiveName"`
    Size          int64  `json:"size"`
    MovedAt       string `json:"movedAt"`
    ExpiresAt     string `json:"expiresAt"`
    DaysLeft      int    `json:"daysLeft"`
    Reason        string `json:"reason"`
    Encrypted     bool   `json:"encrypted"`
    CaptureStatus string `json:"captureStatus"`
    BelongsToCurrentRoot bool `json:"belongsToCurrentRoot"`
    Restorable    bool   `json:"restorable"`
    BlockedReason string `json:"blockedReason,omitempty"`
}
```

`Bootstrap`（`api.go`，`GetBootstrap` 返回值）需要新增 `Trash TrashList`，但**不要把 `GetBootstrap` 变得更重**：列回收站是同目录的一次 `ReadDir` + 若干小 JSON 读，实测应在毫秒级；如果项数很多，考虑按 4.12 走增量。

### 4.11 配置与迁移

`internal/config/config.go`：

```go
type Retention struct {
    Local     int `json:"local"`
    TrashDays int `json:"trashDays"`   // 新增，默认 7
}
```

- 默认值：`TrashDays = 7`（在 `Default()`，`config.go:89` 附近补）。
- 校验：允许 `1 | 3 | 7 | 30 | 90` 之外的任意 1..365，但 UI 只提供 1/7/30/90 四档 pill（与压缩六档 pill 同风格，用户已确认沿用 pill 分段控件）。0 或负数 → 回落为 7；超过 365 → 拒绝并提示（沿用 `config.go:222` 的写法风格）。
- 迁移：`SchemaVersion` 递增前，先确认现有配置读入时 `TrashDays == 0` 能被补成 7（`Normalize()`/`Load` 里做，不要写迁移脚本）。**这是向后兼容的关键路径，必须有测试**（T-R1-14）。
- 语义变化要在帮助里说明（R4）：保留策略淘汰不再直接删除。

### 4.12 前端交互与状态

删除 `store.ts` 里整套 pending-delete 机制：

- 删：`UNDO_WINDOW`（`store.ts:334`）、`pendingDelete`、`cancelPendingDelete`、`commitPendingDelete`（:348）、`store.pendingSnapshotId` / `pendingRootIdentity`（:52-53）、`snapshots` 的 pending 过滤（:65）、`isBusy` 里的 `store.deleting`（:69）、`SnapshotTable.vue` 上以 `store.pendingSnapshotId` 为条件的 disabled 与 title。
- `deleteSnapshot(snapshot)` 改为：立即 `await api.moveToTrash(...)`，成功后本地从 `store.boot.localSnapshots` 中**乐观移除该项并推入 `store.boot.trash.items`**，然后弹一条无操作按钮的信息 toast：`已移入回收站：<文件名>`（无倒计时、无撤销按钮 —— 找回入口在回收站页面）。
- **不整页刷新**：新增 `applyLocalSnapshotChange(item)` 之类的局部 mutation，禁止在删除路径上调用 `refresh()`。后端 `emitStateChanged("trash-add")` 到达时，前端识别 reason 只做 `api.listTrash()`（轻量）而不做 `GetBootstrap`。为此需要把 `onStateChanged` 的回调从「无脑 scheduleRefresh」改成按 reason 分发（`store.ts:165`）。
- `store.deleting` 语义换成 `store.trashBusy`，只在**彻底删除/清空**这类真需要防重入的操作时置位；普通移入不置位（否则又会「等半天才能删第二个」）。
- 失败处理：移入失败 → 保留该项在列表，弹 error toast，并明确说「归档仍在原处」。这一条必须与实现的回滚逻辑（4.5 步骤 10）一致，不能界面说「仍在原处」而文件其实已经在回收站。

新页面 `frontend/src/views/TrashView.vue`：

- 表头：归档 / 大小 / 移入时间 / 剩余天数 / 来源 / 操作。
- 行操作：找回（恢复到当前备份目录）、打开位置、彻底删除。
- 顶部：合计 N 项、约 X；「清空回收站」；一条固定说明「超过保留期会自动清除；本应用不自动清理不属于当前备份目录的内容」。
- 空状态：插画式留白 + 一句解释（对齐现有空状态样式）。
- 归属异常项：整行降透明度，`BlockedReason` 直接显示，找回按钮 disabled 且 title 说明原因。

### 4.13 侧栏与信息架构

用户已确认的排布（`SidebarNav.vue:30` 起的导航块）：

```
工作台
  总览
  归档历史      (badge = 归档数)
  回收站        (badge = 回收站项数)   ← 放在现在「设置」的位置
  ────────
  设置          ← 下移到导航块底部，与主导航之间留一条分隔
```

实现要点：

- 新增 `Page = "trash"`（`frontend/src/lib/types.ts`），`go("trash")` 走现有路由切换；`App.vue` 里引入 `TrashView`。
- 需要一个 `#i-trash` 之外的「回收站」语义图标；现有 `#i-trash`（`App.vue` 的 symbol 定义区）已经存在，可直接用，但视觉上更像「删除」动作。建议新增一个带盖篓/回收符号的 symbol，`App.vue` 的 `<svg class="svg-defs">` 区补一个 `<symbol id="i-recycle">`。**不要复用 `#i-trash` 作为页面图标 + 同时作为删除按钮图标**，会造成「点页面图标是不是清空」的歧义。
- badge 为 0 时不显示（沿用 `snapshots.length` 的写法）。
- 「设置」按钮下移后，键盘与读屏顺序必须仍是视觉顺序；把 `aria-label` 保留为「设置」。
- 遵循记忆里的规则：**一个入口一件事**。不要在总览页再加一个「回收站」快捷卡片，除非那里只有跳转、没有第二套删除控件。

### 4.14 边界情况清单

| # | 情况 | 期望行为 |
| --- | --- | --- |
| B-01 | 备份目录在移动硬盘上，拔出后移入 | `pathguard.Identity` 失败 → 拒绝删除并提示目录不可访问，不产生半成品目录 |
| B-02 | 归档被 7z/杀软占用 | `os.Rename` 返回 `ACCESS_DENIED` → 保留原状，提示「文件正被占用」 |
| B-03 | 同一快照连点两次删除 | 第二次 `Scan` 找不到 → 提示「找不到本地快照；请刷新归档列表」，幂等无副作用 |
| B-04 | 回收站里已有同 id 项（异常残留） | 拒绝移入并提示，绝不覆盖（4.5 步骤 7） |
| B-05 | `trash.json` 损坏/缺失 | 列表把该项标为「元数据不可读」，**永不自动删除**；允许人工「彻底删除」但需二次确认并显示实际文件名 |
| B-06 | 备份目录本身被用户手工改名/移动 | 身份不匹配 → 只展示，不自动清理；找回拒绝 |
| B-07 | 回收站目录被用户手工塞入无关文件 | 只处理带 `trash.json` 且归属校验通过的直系子目录；其余一概不动，只在诊断包中记录发现 |
| B-08 | 用户在保留期内手工把 `expiresAt` 改小 | 以文件内容为准，到期即清（不做时间防篡改，属于本地可控环境，红线未涉及） |
| B-09 | 应用正在备份时删除 | 允许，二者不互相阻塞；若保留策略与手工删除撞上，后到者得到「找不到快照」并保留现状 |
| B-10 | 恢复到其他目录（desktop-only 警告路径） | 与回收站无关，行为不变（`RestoreModal` 现有逻辑） |
| B-11 | 卷上空间耗尽导致 `MkdirAll` 失败 | 回滚已移入的文件并返回带路径的错误 |
| B-12 | 备份目录 = 应用目录（默认 `backups/`） | 回收站随之位于应用目录内。**打包与升级说明必须写清**：删除整个应用目录会连同备份与回收站一起删除（`HelpModal.vue` 现有条目已说过备份目录，需补回收站） |
| B-13 | 归档数很大（>200 项）时列回收站 | `ListTrash` 只读 `trash.json`，不读清单、不算 SHA，应 <50ms；若超标改为启动后台填充并让前端增量渲染 |

### 4.15 测试矩阵

Go（新增 `app/DSHBackup/internal/backup/trash_test.go` + `app/DSHBackup/trash_test.go`，Windows 条件用现有 `manager_safety_test.go` 的 build-tag/运行时判据风格）：

| 编号 | 用例 | 断言 |
| --- | --- | --- |
| T-R1-01 | 备份目录内建 `.dshbackup-trash` 后跑 `manifest.Scan` | 归档列表项数与建目录前完全一致（锁死「不递归」这一隐含前提） |
| T-R1-02 | `MoveToTrash` 正常路径 | 归档 + 全部清单别名都在回收站子目录内；备份目录中不再存在；`trash.json` 可读且字段齐 |
| T-R1-03 | 移入时 `ExpectedRootIdentity` 过期 | 返回错误，且备份目录与回收站都无变化（无部分移入） |
| T-R1-04 | 移入时 `ExpectedSHA256` 不符 | 拒绝，文件未动 |
| T-R1-05 | 清单别名存在但归档缺失（`status=missing`） | 拒绝移入并提示「归档不存在」，避免把只剩清单的残骸搬来搬去 |
| T-R1-06 | `trash.json` 写入失败（mock 只读目录或用不存在的父路径） | 已移入文件被完整改名回原位；回收站子目录被清掉；错误含路径 |
| T-R1-07 | 回收站项 `RestoreFromTrash` 到空备份目录 | 归档与清单回到原名；`trash.json` 与子目录消失；`manifest.Scan` 能重新识别该快照 |
| T-R1-08 | 恢复时目标同名文件已存在 | 整体拒绝，回收站项保持完整，未产生第二个清单 |
| T-R1-09 | 跨卷恢复（构造 `ERROR_NOT_SAME_FILE`，可用 tmpfs/RAM disk 或跳过并在测试名标注 skip 原因） | 明确错误，不复制字节 |
| T-R1-10 | `applyLocalRetention` 淘汰 3 项 | 三项进入回收站且带 `reason=retention`；备份目录中不再可见；`Scan` 结果数量正确 |
| T-R1-11 | 到期清理：`expiresAt` 已过且归属匹配 | 项被移除；日志有 `trash.purge` |
| T-R1-12 | 到期清理：归属不匹配 | **文件仍在**，返回 blocked 计数 |
| T-R1-13 | 回收站里被手工放入无关目录 `misc/` | `EmptyTrash` 与到期清理都拒绝触碰 `misc`，错误里列出它的名字 |
| T-R1-14 | 老配置（无 `trashDays` 字段）读入 | 归一化为 7，保存后写出新字段；不改变其他字段 |
| T-R1-15 | `DeleteLocalSnapshot` 旧入口（若保留兼容） | 或直接删除该入口；若保留必须走回收站，不能存在第二条真删除路径 |
| T-R1-16 | reparse：回收站子目录被替换成 junction | 操作拒绝，junction 指向的外部目录未被破坏 |
| T-R1-17 | 并发：10 个 goroutine 连续移入 10 个不同快照 | 全部成功或明确失败，无交叉污染，`trashMu` 生效 |

Rust：回收站不经 sidecar（纯元数据操作），**不需要**改 Rust。若后续要做跨卷复制回退，才复用 `sidecar/src/util.rs` 的 `stable_copy`；第一期明确不做。

前端（等 R6 基建；在那之前用 `frontend/src/lib/mock.ts` + 浏览器手工走查）：

- 连点三次删除 → 三项都进回收站，界面无遮罩、无倒计时、不整页刷新。
- 回收站找回 → 归档重新出现在历史列表，不触发 `GetBootstrap`。
- 彻底删除 → 必须先出现确认框；取消后文件仍在。
- 空回收站 → 空状态文案正确。
- 侧栏顺序与 `aria-current`；badge 数字正确；键盘 Tab 顺序 = 视觉顺序。

### 4.16 验收标准

1. 上述 T-R1-01…T-R1-17 全绿（跨卷那条允许在无法构造环境时以 `t.Skip` + 明确原因 + 手工记录替代，不许假装通过）。
2. `go test ./... -race` 全绿；`vue-tsc --noEmit && vite build` 全绿。
3. 重新构建 `dist/DSHBackup/DSHBackup.exe` 并真实启动，实测：
   - 备份 → 删除 3 个归档（连续点击，无卡顿）→ 打开回收站看到 3 项 → 找回 1 项 → 彻底删除 1 项 → 清空剩余额。
   - 期间「总览」页仍在运行时不出现整站遮罩。
   - 磁盘上确认 `.dshbackup-trash` 有隐藏属性且没有污染归档列表。
4. 保留策略淘汰后，备份完成提示文案明确说明「已移入回收站，N 天内可找回」。
5. 帮助页（R4）与 `README`、`packaging/README.txt` 中关于「删除」的描述同步更新，不得残留「6 秒内可撤销」。

### 4.17 实施拆分（每步一个提交）

1. **R1-a**：`Retention.TrashDays` + 归一化 + 校验 + 测试（T-R1-14）。纯配置，无风险。
2. **R1-b**：`internal/backup/trash.go`：路径解析、`trash.json` 读写、归属校验、move-in/restore/delete/empty 内部函数 + 单元测试（T-R1-02…T-R1-13、T-R1-16、T-R1-17）。
3. **R1-c**：`manifest.Scan` 不递归的回归测试（T-R1-01）—— 便宜且是后续一切的前提，先落。
4. **R1-d**：`app/DSHBackup/api.go` + `trash.go`（根包绑定方法），把 `DeleteLocalSnapshot` 换成 `MoveToTrash`；`actions_backup.go` 保留身份/SHA/冲突校验逻辑。
5. **R1-e**：`applyLocalRetention` 改走移入；备份完成文案调整；跑真实集成测试（`manager_integration_test.go`）。
6. **R1-f**：`Bootstrap.Trash` + `GetBootstrap` 填充；`OpenTrashFolder`。
7. **R1-g**：前端类型/桥接/mock/store：删掉 pending-delete 全家，改成即时移入 + 局部更新；`onStateChanged` 按 reason 分发。
8. **R1-h**：`TrashView.vue` + `Page`/路由 + 侧栏重排 + 回收站 symbol 图标 + 样式（亮/暗两套都要截图对比）。
9. **R1-i**：`SettingsView.vue` 新增「回收站」小节（保留天数 pill + 立即清空 + 占用显示）；`SettingsSection` 加 `"trash"`。
10. **R1-j**：文档同步（`README.md`、`app/DSHBackup/README.md`、`packaging/README.txt`）+ 全量验收 + 升版本号。

---

## 5. R2 自研选择器组件

### 5.1 需求来源

用户原话：点击「字典大小」选择器时，弹出的选择器 UI 与应用设计非常不和谐。根因：WebView2 的原生 `<select>` 弹出层由操作系统绘制，不吃应用 CSS。

现状（已核对，共 3 处原生 `<select>`）：

| 位置 | 用途 |
| --- | --- |
| `frontend/src/views/SettingsView.vue:447` | 压缩字典大小（0/16/32/64/128/256/512/1024 MB） |
| `frontend/src/views/SettingsView.vue:403` | 每周备份的星期选择 |
| `frontend/src/components/RestoreModal.vue:93` | 选择要恢复的归档（`id="restoreSnapshot"`） |

> 记忆里已确认的原则：**六档压缩仍用 pill 分段控件**，不要因为做选择器就把压缩档位改成下拉。

### 5.2 组件契约

新文件 `frontend/src/components/OptionSelect.vue`：

```ts
props: {
  modelValue: number | string
  options: Array<{ value: number | string; label: string; hint?: string }>
  ariaLabel?: string
  id?: string
  disabled?: boolean
  placeholder?: string
}
emits: { "update:modelValue": (v) => void; change: (v) => void }
```

DOM/可访问性（必须逐条做到，验收会看）：

- 触发器：`<button type="button" role="combobox" aria-haspopup="listbox" aria-expanded>`，显示当前项 label；带 chevron，遵循记忆里「箭头指向结果」的规则（展开时朝上/收起时朝下的语义要与动作一致，与现有 disclosure 控件统一）。
- 弹层：`<div role="listbox">`，子项 `role="option"` + `aria-selected`；用 `aria-activedescendant` 而不是把焦点移进弹层。
- 键盘：`Enter`/`Space`/`ArrowDown` 打开；`ArrowUp`/`ArrowDown` 移动；`Home`/`End` 首末；输入字母做 typeahead（`恢复归档` 的选项是长文件名，typeahead 很实用）；`Enter` 选中；`Esc` 关闭并把焦点还给触发器；`Tab` 关闭并视为选中当前高亮项（不要出现焦点掉出弹窗的“黑洞”）。
- 关闭：点击外部、滚动、窗口 resize、弹层内选中后。
- 焦点：打开时锁定在触发器；组件卸载时清理监听。
- 定位：`position: fixed` + `getBoundingClientRect()` 计算；下方空间不足自动向上翻转；最大高度 320px 内部滚动；不得被父级 `overflow` 裁剪（不要放进 `overflow:auto` 容器里用 `absolute`）。Teleport 到 `document.body`，注意与现有 `.overlay modal` 的 z-index 层级协调（`RestoreModal` 里那处必须仍在模态之上）。
- 样式：只用现有 `--` 设计令牌（纸墨底色、描边、圆角、阴影层级与 `style.css` 中既有 token 对齐）。暗色下不得抬亮度（记忆里已确立的暗色原则）。
- 不引入任何依赖。

### 5.3 迁移与验收

- 三处 `<select>` 全量替换，删除对应的 `.weekday-select` / `.select-input` CSS（先确认没有别处复用同名 class）。
- `RestoreModal` 那处的空列表态（无归档时）：显示 placeholder 且 disabled，保留现有提示文案。
- 与 `:inert="!advancedOpen"` 的现有折叠区配合：高级参数区收起时，内部选择器不可聚焦（沿用 `inert`，不要自己加 `tabindex=-1`）。
- 验收：
  - 亮/暗主题、1080 与 1280 宽度、恢复模态内与设置页内、键盘全路径各截图一次比对。
  - 用 `frontend/src/lib/mock.ts` 的预览模式在浏览器实测（既有工作流），确认弹层不被裁剪、翻转正确。
  - 屏幕阅读器（Narrator 或 NVDA）能读出当前选中项与展开状态；不允许出现「点了没反应」的原生残留。

---

## 6. R3 关闭行为与系统托盘

用户已表达方向，但明确要求「先回答我」，因此**实施前需完成第 6.5 节的两项待决确认**。

### 6.1 已验证的技术前提

- Wails v2.15 提供 `OnBeforeClose func(ctx) (prevent bool)`（`pkg/options/options.go:64`）→ 可以拦下标题栏关闭。
- 提供 `runtime.WindowShow` / `WindowHide` / `WindowMinimise` / `Quit`（`pkg/runtime/window.go:61/67/131`，`pkg/runtime/runtime.go:64`）。
- **托盘没有现成实现**：`internal/menumanager/traymenu.go` 里的 `AddTrayMenu` 在 v2.15 全仓库无调用点，`pkg/options` 无 `Tray*` 字段。必须自建 `Shell_NotifyIcon`。
- `SingleInstanceLock.OnSecondInstanceLaunch(SecondInstanceData)` 已在使用（`main.go`），目前只发一条 warning；隐藏后必须改成「唤起重启的窗口」。

### 6.2 关闭策略配置

```go
// internal/config/config.go
type CloseBehavior string  // "ask" | "exit" | "tray"
CloseBehavior CloseBehavior `json:"closeBehavior"`  // 默认 "ask"
```

- 设置入口：`SettingsSection` 里已有 `"runtime"` 小节，把三档 pill 放在那里。**只放一处**（红线 7）。
- 归一化：未知值 → `ask`。
- 默认 `ask`：首次点关闭必须问（用户明确要求）。

### 6.3 托盘实现草案（新包 `internal/tray`，`//go:build windows`）

1. 建一个 message-only 窗口（`HWND_MESSAGE`）承载回调，或复用 Wails 主窗口的 WndProc（**不可靠，Wails 不导出子类化钩子；建议自建窗口**）。
2. `NOTIFYICONDATAW`（`cbSize` 用 sizeof 结构体本身，`uVersion = NOTIFY_ICON_VER_4`）；`NIF_MESSAGE | NIF_ICON | NIF_TIP`；`uCallbackMessage = WM_APP + 0x2B0`（选一个不与 Wails/WebView2 冲突的私有区间，并在代码里注释选值理由）。
3. 图标：应用内嵌 `icon.ico` 的字节 → `CreateIconFromResourceEx`（不落临时文件，符合可移植约束）。多尺寸（16/24/32）从同一个 .ico 选最接近系统 DPI 的帧。
4. 事件处理：`WM_LBUTTONUP`（或 `NIN_SELECT`）→ 唤主窗口；`WM_RBUTTONUP` / `WM_CONTEXTMENU` → `CreatePopupMenu` + `SetForegroundWindow`（Shell_NotifyIcon 的已知坑：不先前台化菜单不消失）+ `TrackPopupMenuEx`。
5. 菜单项：打开主窗口 / 立即备份 / 打开备份目录 / 退出。菜单句柄生命周期要显式 `DestroyMenu`；回调里**不要**直接执行耗时动作，投给 App 的 goroutine。
6. 从托盘菜单/菜单「退出」触发时：先置 `atomic.Bool` 的 `quitting` 标记，再 `runtime.Quit(ctx)`。**这条是关键**：`Quit` 仍会走 `OnBeforeClose`，没有 bypass 标记会被自己拦下，表现为「点了退出但进程还在」。
7. `OnBeforeClose` 逻辑：

```
if quitting.Load() { return false }        // 放行真正退出
switch cfg.CloseBehavior {
case "exit": quitting.Store(true); go runtime.Quit(ctx); return true  // 自我拦截一次后立即放行
case "tray": hideWindow(); return true
case "ask":  emitClosePrompt(); return true // 前端弹出模态，用户选择后回调后端
}
```

8. 清理：`shutdown` 里 `Shell_NotifyIconW(NIM_DELETE)`；进程被强杀时资源管理器会自己回收幽灵图标，不必做额外保险（但要在帮助里说明）。

### 6.4 交互细节

- **首次关闭询问模态**：三个选项 —— 「直接退出」「最小化到托盘」「这次继续等待（取消关闭）」。加一条「记住我的选择」勾选（默认勾上）。选择结果写入 `CloseBehavior`；不勾则只对本次生效，配置不变。
- 隐藏期间的任务：
  - 用户点「最小化到托盘」时若正在备份/恢复 → 提示「有任务正在运行，关闭会中断它。仍要退出吗？」；选择「隐藏」则允许隐藏并让任务继续。
  - 隐藏状态下任务完成 → 现有 toast 机制仍在工作（WebView 隐藏 ≠ 销毁），但用户看不见。做法：托盘图标加角标（切换 `hIcon` 到「已完成」变体）+ 鼠标 tip 文案更新为「上次备份：…」。**不要**用 `runtime.WindowShow` 强行弹回用户面前，那是打扰。
- 第二次启动实例：改为 `runtime.WindowShow(a.ctx)` + `WindowUnminimise` + 置顶提示；保留现有 warning 文案作为兜底（当窗口本就可见时）。
- 常驻带来的连带需求（与 R1 呼应）：托盘常驻意味着应用可能连续开很多天，因此回收站到期清理不能只在启动时跑一次 —— 见 4.8 的低频 timer 说明。
- 帮助页需要新增「关闭与托盘」小节（R4）。
- `packaging/README.txt` 与发行说明要写「关闭时可选择留在托盘」，并说明「托盘进程仍在时删除应用目录会连带删除备份」。

### 6.5 待用户确认（动手前必须问）

| 编号 | 问题 | 建议 |
| --- | --- | --- |
| D-2 | 常驻托盘后是否要「开机自动启动并隐藏」？ | 建议第一期不做。开机自启要么写注册表/启动文件夹（违反可移植约束），要么注册 Windows 计划任务（与既有「不注册计划任务」的克制一致）。若用户坚持，需明确写入 `HKCU\...\Run` 并在设置里提供「清除启动项」，同时把它列进「本机状态」文档 |
| D-3 | 隐藏状态下 DSH 正在运行时又启动本应用（用户想恢复数据）怎么办？ | 建议：唤现有窗口，若现有窗口被设为隐藏且配置了「保持隐藏」则短暂唤出。必须实测 Wails `WindowShow` 对已隐藏窗口是否可靠 |

### 6.6 验收

- 三种关闭行为 × 「有无任务运行」× 「记住/不记住」全矩阵手工走查。
- 托盘左键/右键/菜单四项、退出后进程真正消失（任务管理器确认 PID 消失、sidecar 子进程随 Job Object 一起消失——这条已有实现，见 `internal/sidecar/process_windows.go`）。
- 隐藏 24 小时不退出：内存不持续增长（WebView2 常驻的已知开销要在文档里如实写）。
- 二次启动唤窗实测。

---

## 7. R4 应用内帮助扩写

### 7.1 结论（与用户讨论已达成）

- 发行包继续携带 `packaging/README.txt`：离线、任何机器、任何编辑器都能读，这是它存在的唯一理由。
- 富文本、带层级、带跳转的说明放在**应用内帮助**。用户问「那是不是意味着还要做应用内帮助页面」，答案是：**扩写现有 `HelpModal.vue`，不新建页面**（红线 7：一个入口一件事；侧栏/顶栏不重复放帮助入口）。

### 7.2 结构

`HelpModal.vue` 现在是 7 个短 `<section>`（见 `frontend/src/components/HelpModal.vue`）。改造为：

- 左侧一条小节导轨（复用设置页已有的 rail 视觉语言，`bc6497b` 那套「导轨跟随阅读方向」的实现），右侧长文档；移动端/窄宽退化为单列。
- 内容清单（每条都要与代码实际行为一致，写完逐条对照代码复核）：
  1. 快速上手：首次引导两步、备份目录在哪、第一次备份怎么点。
  2. 备份范围：读什么、不读什么（项目代码不在内，明确说用什么工具）。
  3. 压缩六档：每档对应的取舍、为什么默认「仅存储」、恢复前快照也用仅存储、高级参数白名单不再含 `-mx`。
  4. 加密与密钥：随机密码 + AES-256 + 文件头加密；密码由本地公钥封装；**私钥丢失不可恢复**；换机要复制整个 `data\keys`；公私钥不匹配时的表现（阻止新建，旧归档仍可解）。
  5. 归档状态：`ready` / `size-mismatch` / `missing` / `conflict` / `partial` 各自含义与「可用 ≠ 已校验」。
  6. 回收站（R1 落地后）：删除即移入；保留天数；保留策略淘汰的也进来；找回回到当前备份目录；不自动清理他人目录的内容；**磁盘占用不会立刻下降**。
  7. 恢复：默认覆盖当前核心数据；可选恢复桌面登录态；「恢复到其他目录」不碰系统登录态；恢复前自动备份；逐文件提交不是整树事务、中途失败不回滚（红线 3，不许隐瞒）；完成后重启 DSH。
  8. 计划备份：只在应用运行时触发；不追补关闭期间错过的时刻；运行中撞上会跳过并提示。
  9. 关闭与托盘（R3 落地后）：三种行为、如何改、隐藏时任务继续、退出的正确姿势。
  10. 诊断：日志在哪、导出诊断包会包含什么（以及**不包含**什么）、`.dshbak-*.tmp` 残留怎么处理。
  11. 已知限制：把 `README.md`「已知限制」小节原样搬进来（不做项目代码备份、无增量去重、无 ACL 保存、无代码签名、桌面 Cookie 换机需重登录、1.0.x 云端功能已移除）。
  12. 更新：只读检查 GitHub 发行版、不自动下载安装、可在设置关闭。
- 底部保留「打开日志目录」，但帮助里**不再**放第二处设置跳转控件，只做纯文字指引（红线 7）。

### 7.3 验收

- 每一条都能指出对应代码位置（自检方法：文档句子 → grep 实现）。做不到的一律删掉或改写。
- 暗/亮主题、1080 宽度下导轨与正文不重叠；键盘 Tab 顺序、`Esc` 关闭、焦点收敛在模态内（沿用 `useModalFocus`）。
- R1/R3 落地后，`README.md`、`app/DSHBackup/README.md`、`packaging/README.txt`、帮助四处关于「删除/关闭」的描述必须完全一致（历史上这里漂移过：`README.md` 曾写着「6 秒内可在底部撤销」而实现已改）。

---

## 8. R5 审计遗留边界（上一轮未做完的）

来自 `REPAIR_HANDOFF.md` 的 W8 余项与已知限制，按投入产出排序。R5-1/R5-3 已在 1.3.1 完成；
R5-2 因用户没有可用的 exFAT/SMB 介质而明确跳过，不为它编写未经验证的结论。

| 编号 | 事项 | 说明 | 建议做法 |
| --- | --- | --- | --- |
| R5-1 | 恶意/超大归档的资源隔离 | **已完成（1.3.1）**：`validate_archive` 的声明总量 ≥256 MB 且压缩比 >200 时返回 `RESTORE_RISK:`，前端确认后以 `force=true` 重试；空间预检保留 | `internal/backup/restore_guard.go`、`manager.Restore` |
| R5-2 | 跨介质矩阵 | exFAT/FAT32/SMB/网络盘/BitLocker 挂载盘未系统验证；隐藏属性、只读属性、重命名语义在非 NTFS 上不同 | 至少手工跑 exFAT(U 盘) + SMB(家用 NAS) 两条：备份→删除→回收站→找回→恢复 全流程，记录差异并落到帮助「已知限制」 |
| R5-3 | 候选文件残留上报 | **已完成（1.3.1）**：恢复准备扫描目标目录并加入 warnings；诊断包发现候选时写入 `restore-candidates.txt`；只报告不删除 | `backup.FindRestoreCandidates`、`ExportDiagnostics`、集成测试断言 |
| R5-4 | Bootstrap 的活动任务快照/修订号 | `GetBootstrap` 与事件流之间没有 revision，理论上存在事件先于状态落地的窗口（现有 `pendingEvents` 缓冲已缓解大部分） | 给 `Bootstrap` 加 `revision int64`，前端丢弃 revision 更低的快照；属于加固，不是 bug 修复 |
| R5-5 | overlapped pipe 传输 | sidecar stdio 目前是同步读写 + 取消宽限，极端大响应会占住 reader goroutine | 只在出现实测瓶颈时再做；不要为了「看起来更专业」重写 IPC |
| R5-6 | ACL 保留 | 明确**不做**，只写文档 | 保持现状，别在 UI 或帮助里出现「保留权限」字样 |
| R5-7 | 真实恢复回归 | 集成测试目前覆盖「加密备份→校验」为主，真实恢复到隔离目录的端到端在隔离 Windows 环境里跑 | 写一个 `TestRestoreIntoIsolatedTargets`：DSH 目录 / 桌面登录态 / 自定义目标三种，断言保护快照与逐文件提交结果 |

---

## 9. R6 前端自动化测试基建

现状（已核对）：`frontend/package.json` 只有 `dev` / `build` / `preview` 三个脚本，**没有任何测试工具**，也没有 `test` 脚本。

建议（保持最小）：

1. `vitest` + `@vue/test-utils`，专测纯逻辑与组件行为，不测像素。
2. 第一批用例（覆盖 R1/R2 的高风险逻辑）：
   - `store.ts`：连续三次移入 → 列表长度、回收站长度、无遮罩、`refresh()` 调用次数为 0（用 spy 断言「不整页刷新」这条产品要求可回归）。
   - `store.ts`：移入失败 → 项回到原位置、toast 为 error。
   - `format.ts`：六档标签、`scopeLabel`、状态文案、高级参数计数上限、`-mx` 不在白名单。
   - `OptionSelect.vue`：键盘全路径（打开/上下/Home/End/typeahead/Enter/Esc）、`aria-selected`/`aria-activedescendant` 正确。
   - `TrashView.vue`：blocked 项找回按钮 disabled 且有 title。
3. E2E 暂不上 CI（本地无 GitHub Actions 需求）；沿用既有 `mock.ts` + 浏览器预览的手工走查流程，并把走查清单固化成文档（本路线图 4.15 的前端小节就是清单）。
4. 把 `npm run test` 加进 `build.ps1` 的验收链，与 `vue-tsc` 同级，缺步即失败（`build.ps1` 已经对 npm/cargo/Wails 检查退出码，沿用）。

验收：`npm run test` 绿；用例数 ≥ 20；至少有一条会因「删除路径调用了整站刷新」而失败的守卫测试。

---

## 10. R7 文档与打包

| 事项 | 决定 | 说明 |
| --- | --- | --- |
| 发行包内文档形态 | 保持 `README.txt` | 离线可读优先；富样式说明交给应用内帮助（R4）。用户已认同这个结论 |
| 仓库内文档 | 继续 `README.md` / `DESIGN.md` / 本文件 | 面向开发者与接手模型 |
| `build.ps1` | 需要新增：R1 的 `TrashDays` 默认值检查、`npm run test`（R6）、托盘图标资源打包 | 现在组装 9 个运行文件的清单要随资源变化更新，并在脚本里断言 ZIP 成员数与文件名，避免把 `data`/`backups` 打进包 |
| 第三方许可 | `tools/7zip/7z2605-extra/License.txt` 已随包 | 若引入托盘相关依赖（只用 x/sys，不引入新第三方），无需新增许可条目 |
| 升级说明 | 每个小版本写清「替换程序与 tools，保留 data 与 backups」 | 已在 README；R1 后要加一句「旧版本删除的归档不会出现在回收站里，回收站只含本版本移入的项」 |

---

## 11. R8 版本与发布

- **待定 D-4**：下一版本号取 `1.2.4` 还是 `1.3.0`。
  - 事实：R1 引入了新的用户可见能力（回收站页、侧栏结构变化）与**语义变化**（保留策略不再真删）。R3 又改关闭行为。
  - 建议：R1 完成即 `1.3.0`（语义变化，配置新增字段）；R2/R4 并入同一版本；R3 单独 `1.4.0`。
- 版本号落点（必须一次改齐，历史上漏过）：`app/DSHBackup/wails.json` 的 `productVersion`、`frontend/package.json` 的 `version`、`build.ps1` 默认版本、`app.go` 的 `Version` 常量、两份 README 与 `packaging/README.txt` 的升级说明。
- 发布门禁（按顺序，任一失败即停）：
  1. `go vet ./...` → `go test ./... -race`
  2. `cd sidecar && cargo fmt --check && cargo clippy --locked --all-targets -- -D warnings && cargo test`
  3. `cd frontend && npm run test`（R6 之后）→ `npm run build`
  4. `pwsh ./build.ps1`（构建 release sidecar + Wails 打包 + 组 staging + ZIP）
  5. 解压 ZIP 到干净临时目录，核对成员数与文件名，启动 exe，实测核心流程（不是「看起来没问题」）
  6. 用户明确授权后才 push / tag / 建 Release

---

## 12. 全量验收命令（复制可用）

```bash
# 仓库根：D:/System/Desktop/DSH
cd app/DSHBackup

go vet ./... && go test ./... -race

# 集成测试需要 release sidecar 与官方 7za，缺任一项测试会 skip；skip 不算通过
cd sidecar && cargo build --release && cargo fmt --check \
  && cargo clippy --locked --all-targets -- -D warnings && cargo test && cd ..

cd frontend && npm run build   # 内含 vue-tsc --noEmit

pwsh ./build.ps1               # 默认版本 1.2.3，需按 R8 更新
```

---

## 13. 风险登记册与回滚

| 编号 | 风险 | 影响 | 缓解 |
| --- | --- | --- | --- |
| RK-1 | **回收站掩盖真实磁盘占用**：保留策略把淘汰项搬进回收站，用户以为空间已释放 | 高（用户抱怨「磁盘满了但列表已按保留清理」） | 回收站页与总览都要显式显示「回收站占用 X，N 天后自动清除」；帮助与备份完成文案明说；不做「静默直接删」的隐藏路径 |
| RK-2 | 移入与保留策略并发撞上，出现「找不到快照」 | 中 | 幂等处理 + 后到者只告警不失败（4.5）；测试 T-R1-10/T-R1-17 |
| RK-3 | 用户手工改动 `.dshbackup-trash`（删文件、塞文件、改名） | 中 | 归属校验 + 无 `trash.json` 不动 + `pathguard` 边界；T-R1-13 |
| RK-4 | Wails 无 Windows 托盘，自建 message-only 窗口与消息循环可能踩坑（菜单不消失、图标幽灵、DPI 图标模糊） | 中 | 先用一个最小 spike 验证 `Shell_NotifyIcon` + `TrackPopupMenuEx` 四件事能跑通，再谈接进主程序；spike 不过就退回「只做关闭询问，不做托盘」并把结论写进文档 |
| RK-5 | `runtime.Quit` 被自家 `OnBeforeClose` 拦截导致「退不出去」 | 高（进程僵留） | `quitting` bypass 标记 + 单测/手工验收必须覆盖（6.3 第 6 条） |
| RK-6 | 隐藏窗口常驻导致 sidecar 长期存活 | 低（已有 Job Object） | 退出路径必须走 `Close`/`abort`；实测任务管理器无残留 PID |
| RK-7 | 前端去掉 `isBusy` 里 `deleting` 后，删除与保存设置/备份互相干扰的隐性依赖断裂 | 中 | 后端仍是唯一事实来源：身份 + SHA + 冲突三重校验保留；R6 加守卫测试 |
| RK-8 | 文档与实现漂移（已经发生过：README 写 6 秒撤销） | 中 | R4 的「逐条对照代码」自检 + 验收标准第 3 条硬性列出需同步的文件 |

回滚策略：R1 每个子步骤独立提交，`R1-e`（保留策略改移入）单独成一个提交，出问题可以只回退这一条而保留回收站读路径；R3 若 spike 失败，只删 `internal/tray`，配置项退回 `ask|exit` 两档。

---

## 14. 决策记录

### 14.1 已确认（不要再问用户）

1. 六档压缩 + 默认「仅存储」，恢复前快照同样用「仅存储」；UI 继续用 pill 分段控件。
2. 高级参数白名单去掉 `-mx`。
3. 删除 = 瞬时移入回收站，位置在**备份目录内**（同卷，保证瞬时 rename）。
4. 允许多次连续删除；**不做倒计时**；**不做整页刷新**。
5. 保留策略淘汰的旧归档也进回收站。
6. 回收站保留默认 7 天，且是设置项（提供 1/7/30/90 四档）。
7. 找回 = 恢复到当前备份目录。
8. 侧栏：回收站占用「设置」现在的位置，「设置」下移。
9. 选择器组件自研（不用原生 `<select>`）。
10. 关闭行为要提供「直接退出 / 托盘托管」，且首次点关闭要询问。
11. 发行包继续带 `README.txt`（离线可读优先），富说明放应用内帮助。
12. 帮助沿用现有模态，扩写内容，不新建页面、不加第二个入口。

### 14.2 已决（2026-10-30 实施时按建议落地）

| 编号 | 问题 | 决定 | 落地位置 |
| --- | --- | --- | --- |
| D-1 | 旧备份目录里的遗留回收站 | 永不自动删除；切换目录保存成功后弹一条 notice 说明，不提供批量清理入口 | `warnLeftoverTrash`（`trash.go`） |
| D-2 | 开机自动启动并隐藏 | 不做（会写注册表/启动项，违反可移植约束） | — |
| D-3 | 隐藏时再次启动 exe | 唤回并短暂置顶现有窗口 | `OnSecondInstanceLaunch` → `showWindow` |
| D-4 | 版本号 | **1.3.0**（回收站改变删除语义并新增配置字段） | 版本四处 + 文档 |
| D-5 | 单条彻底删除是否确认 | 要；移入不确认，永久删除必须确认 | `askDeleteTrashItem` / `askEmptyTrash` |

新增约束（实施中发现，后续沿用）：

- 托盘不可用时（`tray.New` 失败），`tray` 关闭选项禁用，`beforeClose` 对 `tray` 配置直接放行关闭，
  避免窗口隐藏后无法唤回。
- `runtime.Quit` 会再次进入 `OnBeforeClose`，任何退出路径都必须先置 `quitting` 标记。

---

## 15. 交接检查表

新接手时，按顺序执行：

1. `git log --oneline -3` 确认基线是 `452e9a1`（或其后）。
2. 读本文件第 2 节 + 第 14 节，把「待确认」5 条一次性问完用户，得到答复后把结论并入第 14 节再开工。
3. 跑第 12 节命令，确认基线全绿（尤其别把 skip 当通过）。
4. 按第 4.17 的 R1-a → R1-j 顺序实施，每步一个本地提交，每步跑对应的测试。
5. UI 改动（R1-h、R2、R4）先给静态稿或浏览器预览截图让用户确认，再写实现（记忆里已确立的工作方式）。
6. 不做第 2 节红线未授权的事；不 push、不 tag、不发 Release。
7. 完成一个阶段后更新本文件的「状态」列与 `REPAIR_HANDOFF.md` 第 22 节的实现状态，保持两份文档不互相矛盾。
