# DSH Backup 修复方案与模型交接文档

> 基准：`cdada4c`，应用版本 1.2.1；审查与方案日期：2026-10-30。
> **状态：主要修复已按本文实施，验证结果见第 22 节；剩余边界见 22.3。** 本文不是发布授权，也不授权操作真实用户数据、提交、推送或发布。
> 本文可单独交给下一位实现者；代码路径相对仓库根目录，行号对应上述基准，实施时应重新定位函数。

## 0. 交接摘要

目标不是重写项目，而是让现有备份工具在失败、取消、目录别名、密钥异常和重复操作下仍然保持可解释、可恢复的行为。

建议顺序：**先固化失败测试 → 修运行生命周期与密钥 → 修路径和文件提交 → 修快照完整性及保留策略 → 修调度和界面状态 → 补兼容性、打包与验收。** 不应先修好自动调度，就让尚不安全的备份/清理链路自动运行。

### 已有证据

- 原有 Go 25 个顶层测试，包括真实 Rust/7z 的临时目录集成测试，在 `go test -race -count=1 ./...` 下通过。
- Rust 五项测试、Clippy、前端类型检查及生产构建通过。
- Go 跨包语句覆盖率为 51.6%；`schedulerTick`、`NextRunAfter`、启动流程等关键路径原本未被覆盖。
- 额外 22 个测试函数、23 个场景断言暴露问题，另有浏览器开发态 mock 的交互复现。它们不是 23 个互不相关的缺陷。
- 已复现：目标 junction/硬链接越界、复制失败丢原文件、保护快照漏文件仍继续覆盖、密钥错配导致事后无法解密、备份别名进入 temp 后被清理、计划不触发、取消语义错误、保留策略错误、启动数据竞争、Unicode 崩溃等。
- npm 官方漏洞检查与 Go `govulncheck` 未报告已知漏洞；没有据此宣称 Rust 依赖、7z 本体或业务代码已获得完整安全证明。

### 完成定义

“臻于完美”在本轮应转化为可验收条件：本文的发布阻断用例通过、原有正常功能无回退、实际桌面程序经过隔离验证、已知边界如实说明。不能承诺零缺陷、整树事务一致或任意故障下自动恢复。

## 1. 不得破坏的产品约束

1. 保留 Windows 便携版、Wails/WebView2、Vue/TypeScript、Go、Rust 和官方外部 7z 的架构。
2. 备份数据本地保存，不恢复云端、账号、上传、代理等已删除功能；可选只读发行版检查继续保留。
3. 常规配置、密钥、日志、暂存和压缩工作区仍位于应用旁；默认备份目录仍是应用旁的 `backups`。
4. **已获用户明确同意的例外：恢复提交时，可以在目标文件旁创建短命替换候选文件。** 写完校验后提交；正常结束清理，异常残留报告。该许可不包括搬迁整个运行工作区，也不授权恢复真实数据。
5. 保留现有归档、manifest schema 1、age 密钥和已有配置的可读性；不静默转换或重加密历史备份。
6. 本地加密继续可选且默认关闭。不要加入新口令、DPAPI、恢复密钥或云端托管。
7. 恢复保持覆盖合并语义：只更新归档涉及的对象，不删除目标内无关文件。
8. 不在本轮增加 VSS、整树自动回滚、增量/去重、Windows 后台服务或关闭应用后的计划执行。
9. 不修改当前 UI 视觉语言、设置目录导轨及单一设置入口；只改行为和必要状态表达。
10. 威胁边界包含损坏/异常归档、普通目录别名和既有链接；不宣称抵御持续修改路径的同用户主动攻击者。

### 三个必须分开的保证

| 保证 | 本轮要求 |
| --- | --- |
| 单文件提交前失败 | 读取、复制、校验、候选准备失败时，旧目标内容及属性不变 |
| 整个恢复中途失败 | 已提交的文件可能已经更新；停止后续写入，保留已经创建的保护快照并报告实际/不确定的影响；关闭保护或无保护对象时不能承诺存在回退归档 |
| 断电、远端 SMB 断连、内核 I/O 不返回 | 不承诺自动回滚或结果确定；保留恢复证据，不把未知结果描述为“完全未修改” |

## 2. 问题索引与责任位置

标记：**R** = 已有动态复现；**B** = 浏览器 mock 复现；**S** = 静态确认或尚需专门环境验证；**H** = 为修复闭环追加的设计加固，不冒充已有现场故障。

| 编号 | 问题 | 证据级别 | 主要位置 | 工作包 |
| --- | --- | --- | --- | --- |
| F01 | 恢复目标 junction/硬链接使外部文件被覆盖 | R | [staging.rs:297](app/DSHBackup/sidecar/src/staging.rs#L297)、[util.rs:50](app/DSHBackup/sidecar/src/util.rs#L50) | W2 |
| F02 | 复制报错后原目标文件消失 | R | [util.rs:54](app/DSHBackup/sidecar/src/util.rs#L54) | W2 |
| F03 | 保护快照压缩漏文件后仍放行恢复 | R | [manager.go:186](app/DSHBackup/internal/backup/manager.go#L186)、[manager.go:290](app/DSHBackup/internal/backup/manager.go#L290) | W3 |
| F04 | 公私钥错配仍生成事后不可解密的备份 | R | [keys.rs:104](app/DSHBackup/sidecar/src/keys.rs#L104)、[manager.go:256](app/DSHBackup/internal/backup/manager.go#L256) | W1 |
| F05 | 备份目录别名实际位于 temp，启动清理会删除它 | R | [manager.go:667](app/DSHBackup/internal/backup/manager.go#L667)、[paths.go:124](app/DSHBackup/internal/apppath/paths.go#L124) | W2/W4 |
| F06 | 自定义恢复路径追加 `\.` 后仍覆盖当前数据 | R+B | [RestoreModal.vue:27](app/DSHBackup/frontend/src/components/RestoreModal.vue#L27)、[manager.go:374](app/DSHBackup/internal/backup/manager.go#L374) | W2/W7 |
| F07 | 首次/过期计划不断推到未来，周计划忽略新设置 | R | [config.go:239](app/DSHBackup/internal/config/config.go#L239)、[scheduler.go:24](app/DSHBackup/scheduler.go#L24) | W6 |
| F08 | 已取消请求仍发送、取消不等待远端结束 | R；实际写入交叠由调用链确认 | [client.go:261](app/DSHBackup/internal/sidecar/client.go#L261)、[main.rs:50](app/DSHBackup/sidecar/src/main.rs#L50) | W4 |
| F09 | 管道写入阻塞绕过 RPC 超时 | R（受控传输） | [client.go:280](app/DSHBackup/internal/sidecar/client.go#L280) | W4 |
| F10 | 保存前取消仍发布归档并删除旧快照 | R | [manager.go:318](app/DSHBackup/internal/backup/manager.go#L318) | W4/W5 |
| F11 | 重复清单使自动保留删除本应保留的归档 | R | [manifest.go:198](app/DSHBackup/internal/manifest/manifest.go#L198)、[manager.go:772](app/DSHBackup/internal/backup/manager.go#L772) | W5 |
| F12 | 归档删除失败仍删清单并报告成功 | R | [manager.go:799](app/DSHBackup/internal/backup/manager.go#L799) | W5 |
| F13 | 跨时区按字符串排序，最新与保留判断错误 | R | [manifest.go:253](app/DSHBackup/internal/manifest/manifest.go#L253)、[store.ts:49](app/DSHBackup/frontend/src/store.ts#L49) | W5/W7 |
| F14 | 延迟删除时切换备份目录可能删错位置 | B+S（后端目录解析） | [store.ts:313](app/DSHBackup/frontend/src/store.ts#L313)、[actions_backup.go:243](app/DSHBackup/actions_backup.go#L243) | W7 |
| F15 | 通知挤掉撤销，连续删除提前提交，刷新撤销产生重复项 | B；连续删除提前提交由代码确认 | [store.ts:56](app/DSHBackup/frontend/src/store.ts#L56)、[store.ts:298](app/DSHBackup/frontend/src/store.ts#L298) | W7 |
| F16 | 启动配置发布与读取存在数据竞争，健康状态不随退出变化 | R（竞态）+S（健康状态） | [app.go:79](app/DSHBackup/app.go#L79)、[app.go:301](app/DSHBackup/app.go#L301) | W4 |
| F17 | 中文 stderr 按字节截断导致 release sidecar abort | R | [sevenzip.rs:630](app/DSHBackup/sidecar/src/sevenzip.rs#L630) | W8 |
| F18 | 7z 哈希不符被发现前已经执行了程序 | R（无害替身） | [sevenzip.rs:81](app/DSHBackup/sidecar/src/sevenzip.rs#L81)、[app.go:119](app/DSHBackup/app.go#L119) | W1 |
| F19 | 空核心目录导致备份失败，空目录未被归档 | R | [staging.rs:137](app/DSHBackup/sidecar/src/staging.rs#L137)、[manager.go:718](app/DSHBackup/internal/backup/manager.go#L718) | W3 |
| F20 | 已有隐藏文件无法覆盖；合法中文文件名被误拒 | R | [util.rs:54](app/DSHBackup/sidecar/src/util.rs#L54)、[pathsafe.rs:66](app/DSHBackup/sidecar/src/pathsafe.rs#L66) | W2/W8 |
| F21 | 无关后台任务使已打开的恢复弹窗无法关闭 | B | [RestoreModal.vue:17](app/DSHBackup/frontend/src/components/RestoreModal.vue#L17) | W7 |
| F22 | 源检查错误当不存在，遍历错误被静默丢弃 | S | [manager.go:703](app/DSHBackup/internal/backup/manager.go#L703)、[staging.rs:303](app/DSHBackup/sidecar/src/staging.rs#L303) | W3/W8 |
| F23 | 空间查询拿错卷；恢复缺少展开/提交空间预检 | S | [platform_windows.go:21](app/DSHBackup/internal/platform/platform_windows.go#L21)、[manager.go:437](app/DSHBackup/internal/backup/manager.go#L437) | W2/W3/W8 |
| F24 | 停机未确认工作及子进程结束；立即取消可能先于注册 | S | [app.go:152](app/DSHBackup/app.go#L152)、[main.rs:72](app/DSHBackup/sidecar/src/main.rs#L72) | W4 |
| F25 | 计划判断、标记与占用任务之间有竞争窗口 | S | [scheduler.go:39](app/DSHBackup/scheduler.go#L39) | W6 |
| F26 | 归档名称冲突、数字解析、资源边界校验不足 | S | [sevenzip.rs:389](app/DSHBackup/sidecar/src/sevenzip.rs#L389)、[pathsafe.rs:48](app/DSHBackup/sidecar/src/pathsafe.rs#L48) | W8 |
| F27 | 恢复失败结果丢失已创建的保护快照信息 | S | [manager.go:501](app/DSHBackup/internal/backup/manager.go#L501)、[actions_backup.go:165](app/DSHBackup/actions_backup.go#L165) | W2/W4/W7 |
| F28 | 折叠控件仍可聚焦、单选分组、复制成功误报、参数数量不一致 | B+S | [SettingsView.vue:438](app/DSHBackup/frontend/src/views/SettingsView.vue#L438)、[CopyButton.vue:9](app/DSHBackup/frontend/src/components/CopyButton.vue#L9)、[format.ts:132](app/DSHBackup/frontend/src/lib/format.ts#L132) | W9 |
| F29 | 更新链接前缀校验可被路径规范化绕过仓库范围 | S | [updates.go:49](app/DSHBackup/updates.go#L49) | W9 |
| F30 | 打包清理输出目录内 data/backups；模板安装包不等于便携交付 | S，前者是已明示行为 | [build.ps1:79](app/DSHBackup/build.ps1#L79)、[project.nsi:108](app/DSHBackup/build/windows/installer/project.nsi#L108) | W10 |
| F31 | 配置/密钥落盘、两文件发布中断需要明确失败协议 | H | [config.go:143](app/DSHBackup/internal/config/config.go#L143)、[util.rs:99](app/DSHBackup/sidecar/src/util.rs#L99)、[manager.go:910](app/DSHBackup/internal/backup/manager.go#L910) | W1/W5/W9 |

## 3. 最小设计决策

下表是推荐实现，而不是让执行模型自由扩大范围。

| 领域 | 推荐决策 | 不采用的捷径 |
| --- | --- | --- |
| 文件替换 | 目标旁独立候选，校验完成后同卷名称替换 | 先删旧文件、截断覆写、改旧硬链接属性 |
| 路径 | 解析选定根的物理位置；拒绝待写路径下的重解析点 | 只做 `Clean`、小写字符串前缀比较 |
| 保护快照 | 独立复制、完整 inventory、严格压缩/回读核对 | 仅靠 `7z t` 或“没有抛错” |
| 密钥 | 区分可解密与可新建加密备份 | 公钥异常就禁止有效私钥恢复；自动换私钥 |
| 取消 | 取消请求与终止确认分开；结束前不释放任务 | 超时后丢弃 channel、任由远端继续 |
| 调度 | 稳定名义时刻；应用关闭期间不追补，运行中延迟合并一次 | 每次 tick 重算严格未来、失败每 30 秒重试 |
| 删除撤销 | 一次一个待删除项，撤销状态独立于普通 toast | 连点时提前提交前一个、用整份 bootstrap 保存乐观副本 |
| 自动保留 | 唯一归档集合、实际时刻排序、成功删除才计数 | 对原始清单行截取后逐行删文件 |
| 兼容 | 旧格式继续读；新增字段可选，未知状态不伪装为完整 | 改写历史文件或强制重新备份才能打开 |
| 异常 | 可以多保留恢复证据、明确失败 | 为“收尾干净”删除唯一恢复证据 |

### 允许的最小结构调整

- 需要时新增 Windows 专用路径/替换 helper，保留现有 `pathsafe.rs` 的归档路径规则。
- 同一语言只保留一套物理路径检查；Go 与 Rust 共享测试向量和错误含义，不各自发明规则。
- Go 用已有 `golang.org/x/sys/windows`；Rust 若需要 Win32 API，可新增直接 `windows-sys` 依赖并限制 features、更新锁文件。
- 前端增加真实测试工具：与现有 Vue/Vite/Node 兼容的 Vitest、Vue Test Utils、DOM 环境及 Playwright；版本由实施时核对并锁定。
- 不要求引入新的归档解析/压缩库、数据库、任务框架、通用服务容器或整树事务日志系统。
- 若专项测试证明 7z 文本列表歧义无法在现有边界内安全拒绝，应登记为阻断/范围决策，再评估结构化解析；不得直接引入第二套压缩栈或宣称任意恶意归档已安全。

## 4. W0：先把复现转成可持续测试

### 修改范围

沿用已有 [Go 集成测试](app/DSHBackup/internal/backup/manager_integration_test.go)、[设置测试](app/DSHBackup/actions_settings_test.go) 和 Rust inline tests。缺少位置时再新增 `*_test.go`、Rust 模块测试及前端 `*.spec.ts`。

### 实施要求

1. 先记录基准 SHA、工具版本、原有测试结果和未提交改动；不覆盖用户改动。
2. 每个修复先加入会在旧代码上失败的具体用例，再修改实现，最后补正常对照。
3. 测试 helper 必须把应用目录、home、roaming、源、备份和恢复目标全部放在独立临时根下。
4. 不把当前用户的 `.dsh`、桌面 Cookies、真实密钥作为 fixture。
5. 使用屏障/事件、虚拟时钟、受控 subprocess，而不是靠随机时序或大文件把机器拖慢。
6. 对路径拒绝断言明确错误类别及外部哨兵不变，不能“任意报错就算修复”。
7. 检查内容、属性、归档及清单、任务是否仍活动、子进程是否退出；不要只断言返回值。
8. 集成测试必须使用刚构建的 release sidecar；缺二进制导致的 skip 不能算发布验收通过。
9. 在 `internal/backup` 同包测试保留算法等非导出函数，现有 `backup_test` 外部集成测试继续验证公开流程。不要仅为测试导出内部实现。

### 原始复现资料

原审查机器仍有 `C:\Users\23500\AppData\Local\Temp\dsh-audit-31e22c60\`，包含 `backup_audit_test.go`、`host_audit_test.go`、`ipc_audit_test.go`、`manifest_audit_test.go`、`startup_audit_test.go`、`probe_audit_test.go`、`sevenzip_probe.go`、`overlay.json` 和覆盖率输出。

这些文件**不是仓库的一部分，不能作为另一台机器的前置条件**。其中有本机绝对路径、临时覆盖映射和探索性断言。应迁移测试意图，不要照搬脚本：移除硬编码路径、收紧错误断言、补正常对照、正确回收异步任务。

## 5. W1：密钥与工具执行完整性

### 5.1 密钥能力分开表达

当前 `keys_info` 只由私钥推导公钥，而 `wrap_password` 读取磁盘公钥，两个来源可能不同。修复权威逻辑放在 Rust；Go 不应自行推导 age 密钥。

推荐 `KeyInfo` 继续保留 `exists/keyId/publicKey/error`，但明确：

- `exists` 表示有效私钥存在；`error` 用于私钥不可读/无效。
- 新增 `canEncrypt` 与 `encryptionError`，表示磁盘公钥是否存在并与私钥配对。
- 公钥缺失或错配不能把仍有效的私钥说成“不可解密”。
- `keys_info` 保持只读；生成/重建只能在显式的 ensure 操作中发生。

| 磁盘状态 | 允许恢复旧归档 | 允许新建加密备份 | ensure 行为 |
| --- | --- | --- | --- |
| 正常配对 | 实际 unwrap 成功则允许 | 允许 | 校验后返回，不能换身份 |
| 私钥有效、公钥缺失 | 允许尝试解密 | 重建公钥后允许 | 从原私钥推导，原子写入公钥 |
| 私钥 A、公钥 B | 允许使用 A 尝试解密 | 拒绝 | 明确配对错误，不自动覆盖任何一方 |
| 仅公钥/私钥损坏 | 拒绝缺少有效私钥的恢复 | 拒绝 | 不自动生成替代私钥 |
| 两者都不存在 | 无法解密旧加密归档 | 通过历史备份检查后才可生成 | 显式 `allowCreate`，默认禁止新身份 |

Go 两处入口必须共用“检查历史备份后才允许生成”的规则：目标备份目录、当前配置目录、默认目录；扫描错误或不明清单不能等同于“没有旧加密备份”。Rust ensure 增加明确的创建许可，避免检查后文件意外缺失时自动生成新身份。

`wrap_password` 使用已验证的配对信息；Go 比较其返回的 key ID 与准备阶段 key ID，不能再忽略。新建加密备份发布前，用落盘私钥把已封装密码解开并比对；回归再启动独立 client 完成 verify/restore。

**历史错配归档：**仅修正当前公钥不能修复已加密给另一把钥匙的归档。需要实际使用过的对应私钥；不得承诺找回不存在的私钥。清单 key ID 不具备真实性证明，也可能因旧 bug 错记；前端应把 ID 不一致作为警告而不是唯一硬阻断，以真正的 unwrap 成功为准，且必须在任何目标写入前完成。

同步修改 [actions_settings.go:109](app/DSHBackup/actions_settings.go#L109)、[manager.go:627](app/DSHBackup/internal/backup/manager.go#L627)、[client.go:368](app/DSHBackup/internal/sidecar/client.go#L368)、[format.ts:70](app/DSHBackup/frontend/src/lib/format.ts#L70) 及 TS DTO。不得让 `key.error` 的旧语义误伤 private-only 恢复。

### 5.2 先校验 7z，再执行

- Go 在执行 `check_7z` 前对所选二进制计算并比较固定 SHA-256；也可由 Rust 接收预期值并在 `run_capture` 前比较，但选择一处权威入口，不能在执行之后才拒绝。
- `DSHBACKUP_7ZA` 指向的程序也不能绕过检查。
- 校验失败不得启动该程序；工具状态显示不可用。
- 不把哈希校验说成代码签名或抵御同用户持续替换攻击。
- 密钥落盘与配置写入不得使用“先删除旧文件，再尝试 rename”的通用所谓原子写法；采用经过 W2 验证的替换路径，新私钥使用 create-new，既有私钥永不覆盖生成。

**验收：**错配、缺公钥、缺私钥、损坏私钥、旧标识错记、fresh-client 解密、保存失败保留旧密钥；错误哈希无害探针必须没有执行标记。

## 6. W2：真实路径边界与单文件安全提交

### 6.1 统一物理路径契约

当前字符串包含 helper 不能作为文件系统安全边界。Go 设置保存、启动配置检查、任务预检与 Rust 实际安装均执行同一政策。

1. 只接受用户意义上的绝对盘符路径或 UNC 文件路径；拒绝设备命名空间、ADS 和非法组件。内部 Win32 长路径前缀由程序生成，不直接信任用户提供的设备路径。
2. 规范化 `.`/`..`，随后解析真实位置，而不是以规范化代替真实位置检查。
3. 已存在路径通过 Win32 句柄解析最终路径；本地卷统一卷标识，按路径组件、Windows 大小写规则比较。必要时用文件 ID 识别已有别名/短文件名。
4. 新目标找到最近存在祖先，解析后附加经过校验的剩余组件。只有明确的不存在可以向上回退，权限/I/O 错误直接失败。
5. 允许选定根及祖先是合法的目录重定向，但必须先解析为实际根并进行边界检查；不要无理由禁止用户目录重定向。
6. 在解析后的根下，对本次会写入的每个后代目录/叶子使用不跟随链接的检查；重解析点、类型冲突、别名冲突均拒绝。再次写入前复核。
7. 安装阶段不修改计划之外的文件内容与属性；保护快照可以读取其明确保护范围内的文件，但遇到无法安全表达的链接应中止告知，不能默默省略后声称保护完整。
8. UNC/网络别名不能确定是否相同时，应拒绝危险覆盖并提示使用可验证的实际路径，不能把“无法判断”当作不相交。

### 路径政策

| 操作 | 必须拒绝 |
| --- | --- |
| 普通备份目的地 | 等于或位于源、应用 data/temp 内；临时工作区也不能位于源内。目的地仅是源的上级、实际归档不会进入源时不机械拒绝；默认应用旁 backups 仍允许 |
| 默认恢复 | 与应用/备份/任务区冲突；所有实际恢复目标彼此重叠 |
| 自定义恢复 | 与当前 `.dsh`、系统桌面态、应用或备份目录相等或祖孙重叠；包括 `\.`, `..`, junction 和大小写/短名别名 |
| 文件安装 | 选定根下待写路径中的任何 reparse、目录/文件冲突、会影响无关对象的名称别名 |
| 删除备份 | 不能从前端接收任意文件路径后直接删除；必须在当前已验证目录中解析稳定身份 |

`Force` 只表示用户同意 DSH 正在运行这一风险，不能豁免路径、密钥、完整性或权限检查。传给 Rust 的 `targetMode/protectedRoots` 是需要重新验证的参数，不是 `safe=true` 通行证。

### 6.2 候选文件提交协议

新增小型 Windows 文件替换 helper，替代恢复中直接调用 `std::fs::copy(src, dst)`。

1. 在第一次提交前扫描完整安装计划，发现路径/类型/能力问题就停止。
2. 在目标文件同目录 create-new 一个唯一候选；名称由程序生成，不能覆盖已有同名文件。记录它属于哪个任务。
3. 分块复制并响应取消；计算长度和 SHA-256，核对经过校验的解压 inventory。
4. 完成候选 flush/sync、时间与必要属性设置。不得先清除旧文件 readonly/hidden/system；硬链接属性也是共享的。
5. 对候选采用目标目录正常继承的权限。已有目标若有定制 DACL，应复制目标 DACL 到候选或明确拒绝无法保持的情形，不能意外放宽权限；这不是把源 ACL 加入备份格式。
6. 再核对目标仍符合预检与保护状态，进入一个短的单文件提交点。预检不存在的文件若此时出现，拒绝覆盖；新文件使用 no-replace，不能因为统一 helper 支持 replace 就覆盖未经选择/保护的新内容。
7. 首选评估并实测 `SetFileInformationByHandle(FileRenameInfoEx)` 的同卷替换，按需要使用 `FILE_RENAME_REPLACE_IF_EXISTS`、`FILE_RENAME_IGNORE_READONLY_ATTRIBUTE`；不启用强行替换打开文件的 POSIX 行为，不绕过 ACL。
8. API、文件系统或共享权限不支持时，明确失败；不能降级为删旧文件、截断写入、跨卷 copy-delete。
9. 成功提交才增加已提交计数。失败保留原目标或报告提交结果不确定，并保留必要候选/保护快照；正常退出清理确认不再需要的自身候选。

**实施前的小型能力验证不可跳过：**在支持的 Windows 10/11、NTFS，以及拟支持的 exFAT/SMB 上验证普通、只读、隐藏、硬链接、共享锁、失败返回的实际结果。不能从 Win32 结构体“最低 Vista”的文档推断所有扩展 flags 都在全部系统/卷上可用；不支持的覆盖必须尽可能在首个文件提交前拒绝。

不要简单写成“换成 ReplaceFileW 就天然原子安全”。微软文档列出其 1176/1177 部分完成状态，`REPLACEFILE_WRITE_THROUGH` 也不受支持。如果最终选择它，必须设计失败状态恢复并专门验收；额外目标旁备份文件不属于已批准的“替换候选”例外，需要再次明确授权，不能当无风险后备路径。

### 6.3 失败与恢复信息

保留现有默认开启、允许关闭的 `PreRestoreSnapshot` 开关；关闭时明确显示“本次无保护快照”。全部目标经确认不存在时可以无保护归档继续，已经存在的空目录则仍是保护内容。预检不存在的目标若随后出现，应停止，不能覆盖未经保护的新内容。

`RestoreResult` 即使伴随 error 也保留已经生成的 `PreRestoreSnapshot`、目标、已提交数量/是否已知、恢复候选位置等信息；[actions_backup.go:165](app/DSHBackup/actions_backup.go#L165) 不能仅在成功分支传这些信息。

- 提交前失败：可以报告尚未替换目标文件。
- 已提交若干文件后失败/取消：报告部分完成，不自动反向覆盖。
- 进程被强制终止、最后一次提交未确认：报告状态不确定，不能以计数 0 暗示无修改。
- UI 常驻展示保护快照位置和下一步检查方式；不只给瞬时 toast。

**验收：**junction 与硬链接外部哨兵内容和属性不变；源区间锁/候选磁盘满/校验失败时旧目标不变；隐藏只读组合；跨卷候选确在目标卷；未知提交结果保留证据；无关文件继续存在。

## 7. W3：源采集、空目录与保护快照闭环

### 7.1 扫描和暂存必须给出完整事实

- 普通备份要求核心 `.dsh` 源存在且可读；缺失/读取错误明确失败，不静默生成仅桌面态的替代品并清理旧核心备份。
- 可选桌面源确实不存在时可告知后省略；读取错误不能装作不存在。保护快照仍只保护本次实际目标，允许仅桌面态的保护范围。
- 现有历史 desktop-only 归档仍可读取，UI 按实际 scope 展示和限制可恢复内容，不删除旧格式文件。
- 扫描结果使用任务内 inventory：规范相对路径、类型、长度，复制后填 SHA-256；包括空目录和空根。
- 去掉恢复/压缩/安全复核中的 `filter_map(Result::ok)` 静默吞错。错误必须说明阶段及路径。
- 普通备份如果明确允许部分文件跳过，要返回警告和 incomplete 状态；这样的结果不能触发自动保留删除。

### 7.2 不再把硬链接当作独立快照

推荐普通备份和保护快照都改为独立复制，避免维护两套“暂存完成”的含义。

- 文件读取期间请求拒绝写入/删除共享的句柄；已存在的写句柄导致明确失败或普通备份的已知不完整结果。
- 比对复制前后文件身份、长度和修改信息，检查复制错误。
- 临时源与原文件必须是不同文件对象；暂存后原文件变化不能改变暂存内容。
- 成本是暂存磁盘与 I/O 增加，因此先扫描计算空间，再复制；不能沿用硬链接时期的空间估算。
- 这仍不是整树同一时刻快照，也不能保证运行中数据库事务一致。保留关闭 DSH 的建议。

空目录应被显式创建并参与归档清单。向 7z 提交目录时要明确递归规则，不能让压缩器额外收进 inventory 之外的内容。通过回读测试验证，不凭参数名称推断行为。

### 7.3 保护快照严格链路

以下链路仅在保护开关开启且存在保护对象时执行；不应错误阻止恢复到经确认全新的目录，也不能把用户关闭保护改成强制开启。已存在的空根仍走保护链路。

```text
解析实际目标 → 完整扫描/独立暂存 → 压缩 exit 0 → 列表核对
→ 7z test exit 0 → 解压保护归档到独立校验目录 → 比对 inventory 和文件哈希
→ 确认保护归档发布成功 → 复核目标仍匹配保护内容 → 才允许安装
```

- 保护链路任何扫描/复制/压缩/测试/回读警告、跳过、缺失、多出或内容不符都停止恢复。
- 不再把 `Files == 0` 直接等同于没有保护内容；空目录也是内容。
- 归档是否存在、是否可解密、是否完整、是否包含全部预期文件分别验证。
- 每项对应的旧目标在替换前再确认没有在保护后变化；变化则停止，让用户关闭相关程序并重试。
- 普通备份可以尊重 `VerifyAfterBackup=false` 跳过完整回读，但加密密码的落盘解封检查、基本清单一致性和状态如实报告不能跳过；有源缺失/警告不得启动清理。

无需为新备份引入强制逐文件公开 manifest schema，inventory 可以是任务内产物；但新产物必须写入 `captureStatus: complete|partial`。该字段仅对旧文件读取可选，不能省略新 partial 的状态。任何非政策性跳过、采集失败或未解决警告都不能写成 complete；旧文件缺字段按 legacy/unknown 处理，不伪造历史验证结论。若不实现持久化状态，则部分采集必须直接失败、不发布。

**验收：**锁住已暂存 payload 使 7z exit 1、但 `7z t` 通过，仍必须阻止安装；同大小内容变化也应被哈希发现；空目录往返；不可读源明确失败；独立暂存不随源变化；失败不清理旧核心备份；partial 发布后重启再备份，partial 仍不顶替完整归档的保留名额；关闭保护、全新目标、已有空根各自走正确分支。

## 8. W4：任务、RPC、启动与停机

### 8.1 状态与所有权

| 对象 | 状态/职责 |
| --- | --- |
| App | starting → ready/degraded → stopping → stopped |
| 根任务 | reserved → running → draining → terminal；cancelRequested 是独立锁存状态 |
| 根任务结果 | succeeded / failed / cancelled / aborted；终止未确认时不是 terminal |
| Go App | 唯一任务槽、固定配置快照、完成信号、最终结果 |
| Manager | 自己创建的临时文件、提交点、保护快照与部分结果 |
| RPC client | 每个已发送请求、writer、decoder、终止与 reaper |
| Rust | 每个操作的 token、worker、7z 子进程与输出 reader |

任务 ID 改为会话唯一 ID 加计数器或随机 ID，避免毫秒碰撞。`CancelTask` 接收预期 task ID，并发重复取消幂等；旧界面的取消不能取消下一项任务。

只有在远端操作结束或其进程树确认终止、manager 收尾完成、结果已经记录后，才清空任务槽。`taskMu`、`cfgMu` 和生命周期短锁不得跨越文件 I/O、RPC、进程或 UI 等待；`cfgWriteMu` 专门用于串行配置落盘，可以覆盖写入，但持有期间不得反向获取 `taskMu`。

设置保存与 onboarding 也要拆为“短锁检查并保留互斥操作 → 释放短锁后目录/密钥/落盘 → 释放写锁后结束保留”。这段保留阻止任务抢跑，但不是一项伪装的备份进度任务。停机必须等待它收尾，不能在设置仍写盘时关闭客户端或日志。

### 8.2 用 request ID 取消操作

- Go/Rust 同步升级内部协议版本；ping 验证版本，不适配混装旧协议。
- Rust 输入循环先注册 request ID 对应 token，再启动 worker；取消 RPC 自己有独立 ID，参数引用目标 request ID。
- `taskId` 仍用于 UI 根任务归属，不能作为不同阶段操作唯一身份；保护快照不再靠两次猜测 `id`/`id-pre` 取消。
- 所有耗时 RPC 都纳入追踪，包括 hash/list/check/key 操作，不仅是有进度的 RPC。
- ACK 只说明取消请求已收到。原请求的终态响应必须在工作、子进程和 reader 结束之后发出。
- 已完成请求返回明确 already-finished；去重与移除必须针对本请求，不能移除其他 worker 的注册。

### 8.3 传输不再提前遗弃工作

- 未发送且 context 已取消：从待发送集合移除，不发送。
- 已发送后取消：保留 pending，发控制请求，等待原响应或受监督的进程树终止。
- 采用一个由 client 所有的 writer 和有界队列；入队可取消，写前复查状态，控制消息不能越过它所取消的请求。
- **最小可行方案保留 stdio：**真实管道写入阻塞超过宽限时，由独立 supervisor 终止拥有读端的 sidecar Job，确认退出并回收 writer 后使连接整体失败。接受一次不健康传输导致整个 sidecar 不可用，不自动重放写操作。
- 不为每次 `Write` 启动一个超时后无人回收的 goroutine，也不能假设任意 `io.WriteCloser.Close()` 必然中断写入。
- 要用真实不读 stdin 的测试进程验证上述终止可使写入结束。若支持环境不能满足，再实现 overlapped 管道和 `CancelIoEx`；该 API 请求取消后仍必须等待 I/O completion，不能提前释放缓冲区。
- 部分 JSON 帧、协议损坏、EOF 均令连接不可再用。EOF 表示不健康，不单独证明全部后代进程已停止。
- 进度可合并并丢弃旧进度，终态响应不能丢；避免 UI 回调阻塞 stdout decoder。

初始建议：普通取消宽限 5 秒，超限执行进程树终止并进入 aborted。这个时间是策略参数，不是“5 秒内任何内核 I/O 都能停止”的保证。若仍无法确认终止，保持 draining 并提示，不清理工作区、不允许新的写任务。

### 8.4 Windows 子进程所有权

- Go 创建不可被子进程继承的 Job handle，设置 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`，不允许 breakaway。
- 把 Rust 纳入 Job 后，才发送任何可启动 7z 的请求；Rust 在此之前不得创建后代。分配失败则启动失败，不能降级为失管运行。
- 所有 7z 默认继承 Job；只有一个 reaper 调用等待，避免多处 `cmd.Wait()`。
- 强制结束后确认 Job 中活动进程归零，而不只看 Rust 主进程退出；不能仅依赖不保证送达的 completion-port 通知。
- Rust 的 capture/streaming 共用监督规则，所有退出分支 kill/wait/join；hash/copy 在块边界检查取消。
- EOF 停止收新任务，取消并 join 已注册 worker。不能直接让 main 返回遗留 7z。

### 8.5 启动与停机

- `NewApp` 创建全部生命周期 channel/锁与初始状态；startup 在局部构造结果，统一同步发布，配置用 `setConfig`。
- 先读配置并校验真实路径，再决定临时清理；检测已有备份别名进入 temp 时停止清理并报警，避免“修复版本第一次启动就删旧备份”。
- 用一个轻量、非阻塞的 readiness 快照接口，或在 bootstrap 中明确初始化状态；选择一种统一方式。推荐独立 `GetReadiness`，初始化中不触发目录扫描和 sidecar 查询。
- UI 先订阅再查询；starting 显示初始化状态，不出现可提交的空白配置/首次引导。有限重试只处理错过事件，不无限忙等。
- degraded 可以展示配置/诊断，但任务入口根据实际能力阻止危险操作；sidecar EOF 立即更新健康状态。
- Bootstrap 增加当前任务/最后结果快照与递增 revision；刷新页面不应丢失正在运行任务。旧响应或旧事件不能复活已结束任务。
- 停机拒绝新操作 → 停止调度 → 取消初始化/活动工作 → drain → 必要时终止 Job → 等待 manager/client → 关闭日志。
- 对长停机使用非阻塞 `OnBeforeClose` 协调与界面状态，`OnShutdown` 保持幂等兜底。取消/强退未确认时不能声称正常完成。

### 8.6 临时清理所有权

应用旁采用会话唯一目录和程序生成的任务标记；只删除当前任务确定拥有、且不再有 worker 访问的文件。

- 不再遍历 temp 后无条件删除全部内容。
- 保留未知、旧版、异常中断和仍需恢复的目录；报告空间占用，不能“为了通过测试”强删。
- 已完成且明确可清理的目录可以清理；标记只引用本工作区的路径，不能读取任意标记后按其中外部路径删除。
- 目标旁候选路径需记录以便报告残留；应用启动不能凭一个可能失效的路径自动删除外部文件，必须核对任务所有权和文件身份。

### 8.7 提交阶段取消语义

- `save` 进度回调之后、创建目标备份目录内的最终归档输出之前，先检查 context。
- 向 create-new 的最终归档输出复制数据时可取消；在 manifest 完成前它仍是未发布输出。取消只回收确认属于本次的输出，不发布 manifest、不删旧备份。
- 提交闸门与取消请求同步：取消先获得闸门则不提交清单；提交先获得闸门则完成这一小段发布或报告实际失败。
- 已发布成功后收到取消：保留新备份，跳过尚未执行的自动清理，报告“备份已保存，清理已停止”，不能伪装成没有生成备份。
- 自动清理在每个快照删除单元之间检查取消；已开始并完成的删除无法撤销。
- 已发布的保护快照不因后续取消而丢弃，错误结果仍携带其位置。

**验收：**立即取消、排队取消、压缩/列表/hash/copy 取消、保护阶段取消、部分管道帧、writer 阻塞、子进程异常、反复关窗、初始化关窗、旧事件、UI 重载；逐项断言结束后没有继续写入、没有存活后代、没有提前清理。

## 9. W5：清单、发布、保留与删除

### 9.1 扫描形成唯一归档集合

- 一个归档身份是已验证备份根中的 `(ID, archive name)`，不是一个 manifest 文件名对应一份独立备份。
- 相同身份、内容一致的清单副本合并；优先使用标准文件名，没有标准文件名时仍允许可验证的旧别名。
- 同 ID 的哈希、大小、封装密码等实质信息冲突时标记 conflict，禁止自动清理/恢复，不擅自选择某个副本。
- 内部保存清单别名列表；删除同一归档时处理它的已核实清单副本，所有路径仍须位于当前根。
- 缺失、大小不符、冲突、部分采集的记录继续可见，但不能冒充一份健康保留名额。
- 所有时间先解析为实际时刻再排序，稳定的 ID 次序仅作为相同时刻的次排序。前端使用 `Date.parse`，不使用 `localeCompare`。
- `Snapshot` 暴露实际 `scope` 与必要的采集状态；不要把 desktop-only 显示成“DSH + 桌面”。旧缺字段不能随意推断为完整。

### 9.2 发布是有边界的两文件操作

`.7z` 和 `.manifest.json` 无法一起原子提交，采用明确顺序：

1. 源暂存、压缩、校验和一般临时工作仍在应用旁；不增加备份目录内独立 `.partial` 临时文件的授权。目标备份目录内只创建最终归档/清单输出；没有完成清单之前，归档属于未发布的输出，不是一份可用快照。
2. 检查 archive/manifest 两个最终名称均未被占用，以 create-new/no-replace 创建最终 archive 输出。碰撞重选 ID，不能覆盖历史文件。
3. 向该输出分块复制并响应取消；核对长度/SHA-256、sync/close 完成。清单字节在内存或应用旁准备，然后进入 W4 提交闸门，以 create-new 写最终清单并 sync/close。只有完整清单与归档匹配，且本次发布已确认，才进入可用快照集合；同会话扫描不得把正在提交的项提前当成完成。
4. 中途失败只收回确认属于本次的产物。中断的无清单归档、部分清单不参与恢复/保留；保留诊断，不能自动认作 legacy 完整备份。身份或结果不确定时保留并报告，不删除历史文件。
5. 两者发布确认后才计算保留；不得提前清理旧归档腾空间。若后续一定要使用备份目录内额外临时候选，需要另行明确许可，不能借用恢复候选的例外。

这不是两文件原子发布：原始文件视图可能看到未完成输出，应用必须通过完整清单和发布状态识别它；断电耐久性仍按第 1 节边界处理。

保留现有 ID 外观。碰撞重选名称不应伪造 CreatedAt 为未来，实际创建时间与用于避碰的 ID 选择分开。多台机器同时写同一备份目录不作为本轮协调保证；no-replace 仍必须避免相互覆盖。

### 9.3 保留策略

- 保护快照永不进入普通自动清理集合。
- 本次普通备份若采集不完整、存在未解决警告、清单发布失败或扫描存在歧义，跳过自动清理并说明原因。
- 在唯一、结构上可用且包含核心范围的普通备份中选择最近 N 份，同时保护本次新快照。
- 旧归档的“结构可用”仍不等于逐份完成过解密测试；不得在 UI 宣称全部验证。旧 schema 未写采集状态的归档继续兼容。
- 缺失/冲突/部分记录不消耗健康保留名额，也不静默删除；留给显式人工操作。
- 从明确 survivor 集合反推删除集合，绝不能删除仍由 survivor 引用的同一归档。
- 每个删除单元复核当前身份/状态；先删归档，失败就保留清单并报告。归档成功、清单删除失败则报告部分清理，保留 missing 记录。
- 只有实际成功才加入 deleted 列表；不要用 `_ = os.Remove(...)` 生成假成功。
- 遇到意外 I/O/身份变化时停止本次后续清理，宁可暂时多保留。

### 9.4 显式删除绑定用户选择

列表响应由后端提供选择时备份根的物理身份 `backupRootIdentity`，本地可由卷标识与目录 file ID 构成，或采用等价的后端身份令牌。前端不得重新计算它；不能用路径字符串冒充历史物理身份。

修改 Go DTO 与 bridge：请求至少包含 `snapshotId`、`expectedRootIdentity`、用户选择时用于展示的 `expectedBackupDir`、`expectedSHA256`，而不是任意 archive path。

后端取得任务槽后，在当前配置目录内重新解析唯一快照；当前实际根身份与历史身份不同，或 ID/哈希改变，返回 stale-selection，**两个目录都不删除**。不得把 expected 路径重新解析到它的新指向来代替选择时身份：路径不变、junction 改指同 ID/同 SHA 副本也必须拒绝。网络场景无法可靠确认身份时拒绝该删除，而不是弱化为字符串比较。前端只能把这些字段当条件，不能借传参扩大可删除根。

**验收：**重复与冲突清单、非标准文件名旧清单、锁住归档、锁住清单、跨时区、保留 N 与保护快照、缺失/重启后的 partial 记录不顶替可用份数、目标跨卷复制中断、发布撞名、每个提交点取消、目录切换及 junction 改指后删除拒绝。

## 10. W6：计划备份语义与实现

### 推荐产品语义

| 情况 | 行为 |
| --- | --- |
| 首次启用/启动应用 | 武装一个稳定的未来 `nextDue`；不追补应用关闭期间的历史 |
| 到期且空闲 | 占用任务，记录名义 occurrence，再发起备份 |
| 正在执行其他任务 | 不消耗 occurrence；空闲后执行最近一次到期项 |
| 运行中睡眠/恢复，错过多个时刻 | 合并成最近一次，不能堆积连续补跑 |
| 检测到 DSH 正在运行 | 消耗该 occurrence，明确跳过一次 |
| 检测错误、启动失败、备份失败或取消 | 本次已尝试；下个计划或手动重试，不能每 30 秒重复 |
| 计划字段改变 | 服务端重置该计划水位并重新武装；无关设置保存不重置 |
| 标记持久化失败 | 不执行本次写任务，暂停自动调度并提示；需明确恢复，不做高频重试 |
| 时间回拨 | 不重复已处理的 occurrence |

### 具体修改

1. 日历函数接收显式 `now` 与 location，去掉内部隐藏的 `time.Now()`；为某个日期生成名义时刻，再计算前/后 occurrence。
2. 每周按当前星期与时间计算，不使用 `lastRun.AddDate(...,7)` 代替日历规则。
3. App readiness 后及语义设置改变时武装 `nextDue`，tick 不重新把它推向未来。
4. `LastScheduledAt` 保存名义 occurrence，而不是完成时刻；旧字段按历史水位解释，不让旧延迟改变新计划星期。
5. 建议不新增 config schema。语义计划改变时服务端清空水位；手工输入的 `LastScheduledAt` 仍不可信。
6. 在 `taskMu` 下重新检查最新配置和 due、占用根任务并捕获配置，立即释放 `taskMu`；保留状态阻止设置抢跑。随后持 `cfgWriteMu` 保存水位，释放写锁后才启动已保留的备份，或在失败时结束保留。不能持 `taskMu` 等待落盘，也不能二次 `beginTask`。
7. `taskMu/cfgMu` 仅用于短状态操作；`cfgWriteMu` 可覆盖串行落盘，但持有期间不得反向获取 `taskMu`。设置/onboarding 必须采用 W4 相同的短锁保留协议，不得继续在 `taskMu` 下等待目录检查、密钥 RPC 和保存文件。
8. DST 重复时刻只执行首次；不存在的当地时刻跳过当次。日历加一天/周，不以固定 24h/168h 代替。
9. NextRun 展示使用同一武装状态；不得界面显示今天，后端实际永远等明天。

该策略是“每个 occurrence 至多尝试一次”，不是“崩溃情况下恰好成功一次”。标记后、启动前崩溃可能漏一次；没有额外事务日志时必须如实说明，不自动重放不确定任务。

**验收：**空/很旧水位、恰好到点、周计划改星期和时间、daily/weekly/manual 切换、无关设置保存、busy、DSH running、失败、睡眠、重启、时间回拨、DST、标记写失败及手动任务/保存设置抢占。

## 11. W7：前端任务、恢复与六秒撤销

### 11.1 六秒撤销：一次一个待删除项

保持原有无确认弹窗的方向，但消除不可靠的单 toast 状态：

- `boot.localSnapshots` 始终代表最近一次服务端结果，不再用 splice/push 暂存乐观删除。
- `pendingDelete` 是独立状态：列表响应给出的 `backupRootIdentity`、展示路径、ID、expectedSHA、deadline、pending/committing 状态；身份固定在选择时，不能提交前才重新计算。
- 列表、计数、用量从“服务端列表减去当前 pending 身份”派生；刷新仍过滤，撤销只清状态，不插入副本。
- 一次只允许一个 pending；其他删除按钮暂时禁用，并给出原因。不能第二次点击就提前提交第一次。
- 在现有 ToastHost 中为 pending 保留独立的撤销表现，普通通知不得替换它。可以暂缓显示普通 toast，不为此建立通用通知队列。
- 使用单调时间计算六秒窗口；开始 commit 后移除“仍可撤销”的承诺。
- 关闭/卸载、开始其他任务、确认切换备份目录时取消 pending。后台计划进度到达也取消尚未提交的 pending。
- commit 期间有独立 deleting 状态进入 `isBusy`；请求发生竞态被后端拒绝时，清 pending 并刷新，不手工 push 旧快照。
- 配置切换时前端主动取消只是第一道保护；W5 的后端 expected-directory 检查不可省略。

如果以后用户要求多项并行撤销，再设计逐项队列。本轮不要悄悄改为批量删除，也不要复制出多个同类全局控制入口。

### 11.2 恢复与状态

- 区分 `canSubmit` 和 `canDismiss`：无关任务只禁提交，不应把用户锁在恢复弹窗里；本弹窗请求实际启动后交由任务页展示。
- 自定义路径的文字规范化只为即时提示，安全判断必须由后端 W2 完成。
- scope 来自真实清单，旧 desktop-only 归档不能标成核心必选；自定义恢复不包含核心内容时明确不可执行。
- 失败、取消、aborted 也显示保护快照和部分恢复信息。
- Bootstrap 与事件按 revision 和 task ID 合并；旧事件不能清掉新任务，重载后恢复活动任务状态。
- 当前模板中失败结果即使收到保护字段也可能不展示，必须检查 Dashboard 的成功/失败分支，不能只加 DTO。

同步变更 [api.go](app/DSHBackup/api.go)、[types.ts](app/DSHBackup/frontend/src/lib/types.ts)、[bridge.ts](app/DSHBackup/frontend/src/lib/bridge.ts)、[mock.ts](app/DSHBackup/frontend/src/lib/mock.ts)、组件与测试；生产环境仍不能通过 URL 参数启用 mock。

**验收：**A 删除待定→刷新→撤销不重复；无关通知不丢撤销；第二次删除不能缩短第一次六秒；切目录遇同 ID 不删错；后台任务启动可以关闭恢复弹窗；失败保护位置常驻；刷新任务不重启任务。

## 12. W8：7z 解析、资源边界及 Windows 兼容

### 12.1 解析与校验

- 修复 stderr 保留尾部的实现：保留 bytes 后安全解码，或在字符边界切分；单行读取本身也要有上限。不要通过取消 `panic=abort` 掩盖越界切片。
- 数字解析失败应报错，不能 `unwrap_or(0)`；总字节和条目数量使用 checked arithmetic。
- 原始路径不能先 `trim` 再交给本来要拒绝空白的校验器。
- 拒绝同一目标的重复项、大小写冲突、文件/目录冲突、文件作为其他项祖先、根名是文件、非法根、ADS 与 reparse/link 条目；不要 `dedup` 掩盖歧义。
- 空父目录可以从合法子项补齐，但不能掩盖冲突。
- 提取后严格遍历，与接受的清单比对类型、长度、数量和目录；错误不跳过，再计算安装 inventory 哈希。
- 文件组件长度按 UTF-16 code units 及实际文件系统上限，不按 UTF-8 bytes；完整路径也需检查。
- Windows 保留名测试补齐上标数字设备名、控制台别名、带空格 stem；8.3 别名不能命中无关现有文件。

### 12.2 有界处理与空间

建议先使用集中常量而非增加设置 UI：manifest 最大 1 MiB、wrappedPassword base64 最大 64 KiB、解出密码最大 1 KiB、列表单行最大 128 KiB、条目最大 100 万、累计规范路径字节最大 256 MiB。单行预算应覆盖所支持的 UTF-16 长路径转为 UTF-8 后的长度；累计预算防止大量长路径撑满内存。它们是初始防御预算，实施者须对旧归档样本验证；遇上限给出明确错误，不截断后当成功。

- 输出列表应流式解析，不先把全部 stdout 放入一个 String；协议与错误输出同样有界，终态响应不得静默截断为合法 JSON。
- 大 inventory 留在受控任务目录或结构化流中，RPC 只传摘要/引用；不要一边把 IPC 限制为小帧，一边把百万条路径放入单帧。
- 对实际目录调用空间查询，按实际卷归并。扫描后暂存前、压缩前、保护回读前、每个目标候选前分别核查峰值。
- 预算同时计入独立暂存、压缩输出、原归档展开、保护暂存/回读和目标候选；同卷不能各自重复使用全部可用空间。
- 不用“新文件大小减旧文件大小”估算候选需求，因为提交前旧文件仍存在。
- FAT32 单文件限制应在开始写入前给出明确错误。
- 运行中检查余量并可取消；空间预检不是空间预留，更不是任意恶意压缩流的硬配额。严格硬配额需要额外系统隔离，不在本轮默默引入。
- 7z 内存/参数预算根据高级参数覆盖后的有效值评估，不只看压缩档位；本轮先界定错误和终止能力，不随意添加可能杀掉正常压缩的固定 Job 内存限制。

**验收：**长中文 stderr、90 UTF-16 单位合法文件名、非 BMP 字符、确实超长名称、重复/冲突/尾空格/设备名、非法数字/溢出、fake 小预算超限、挂载卷空间差异。危险资源测试用小上限或受控替身，不能填满用户磁盘。

## 13. W9：小问题和写法收口

1. 高级参数折叠时使用 `inert` 或等价措施，从键盘与可访问树移除内部控件；展开后恢复正常。
2. 恢复单选输入使用共同 `name`，验证方向键行为。
3. Go `CopyText` 检查 `runtime.ClipboardSetText` 的实际返回值；前端 `copyText` 返回 boolean，CopyButton 只在成功后显示已复制。保留合理 fallback，但不得伪造成功。
4. 前端同步最多十二个高级参数的规则，补十二/十三边界用例；用相同测试向量约束 Go/Rust/TS，不为几条正则引入代码生成系统。
5. 更新链接用解析后的规范路径段与仓库 releases 路径边界判断，拒绝 userinfo、dot-segment、编码变体、前缀相似路径。仍只允许 HTTPS GitHub；不要声称这是已证明的任意站点跳转漏洞。
6. Config.Load 在规范化后验证；不支持 schema、非法设置明确诊断，不让坏配置启动计划。保存失败保留旧文件；不要先删旧配置。
7. `cargo fmt`、`gofmt` 只在行为修复稳定后整理，避免格式化淹没高风险修改。
8. 修复可到达的忽略错误点，别机械地给每个 `_ =` 加复杂重试。候选清理失败应记录；旧归档删除、配置替换、保护验证失败必须影响结果。
9. Rust 依赖存在未来兼容警告，应单独评估/更新并锁定；不等于当前安全漏洞。不能使用 `npm audit fix --force` 或全量升级解决本轮业务问题。

## 14. W10：安全打包与发布验收

当前脚本删除 `dist/DSHBackup/data` 和 `backups` 是用户曾批准的干净发布行为。保留“发行包不带运行数据”这个目标，但消除重建误删现场的机制。

### 推荐改法

- 每次在构建过程拥有的全新 staging 下组装白名单运行文件，再从 staging 打包。
- 不在旧 `dist/DSHBackup` 里删除 data/backups；若保留该便携目录作为输出，只更新明确的运行文件，运行数据保持原样，ZIP 仍从干净 staging 生成。
- 不把旧目录整体压缩，也不从旧目录复制未知文件；预期九个运行文件的名单作为断言，数量变更须同步许可证/说明。
- 安装依赖、cargo、Wails、压缩命令都显式检查 native exit code；PowerShell 5.1 的 `ErrorActionPreference=Stop` 不会自动把所有 native 非零退出变成异常。
- 锁定 Go/Rust/frontend 匹配产物并校验协议；程序、sidecar、7z 的哈希进入 BUILD 信息。
- 仅清理构建本身新建的 staging；失败保留必要诊断，不删除已有版本 ZIP 和用户目录。
- 当前 NSIS 是模板而非已完整交付的安装模式，不在本轮扩展安装器。文档不得推荐使用未纳入 sidecar/工具及数据保护验证的模板安装器。

### 发布门槛

1. 所有 P1/P2 回归与原有正常链路通过，无关键测试 skip。
2. 双击真正重建的 exe，使用隔离 Windows 用户或完整隔离 home/roaming 的测试环境验证；仅设置 `DSHBACKUP_APP_DIR` **不会**隔离真实 DSH 源。
3. 验证 onboarding、设置保存、普通/加密备份、独立 verify、默认/自定义恢复、取消、撤销、自动计划、异常恢复指引。
4. 验证 UI 后端是真实 Wails，而不只是浏览器 mock；浏览器 mock 用于交互，不能证明文件恢复正确。
5. 在旧输出目录放临时 data/backups 哨兵执行打包测试，哨兵保留，ZIP 不包含它们或密钥、日志、候选文件。
6. 更新 README 的实际行为、测试前提、取消/部分恢复语义和文件系统限制，不重写历史设计记录冒充从来如此。
7. 提交、推送、打标签、发布和操作真实数据均需单独明确授权；本文不是持续发布许可。

## 15. 接口改动清单

以下是拟新增/调整的契约，名字可以小幅调整，但语义不能遗漏。

| 契约 | 要求 |
| --- | --- |
| Rust ping | 校验 protocol 版本，matched bundle 部署；不为混装版本添加兼容分支 |
| 取消 RPC | 目标 request ID；ACK 与原请求 terminal 分离 |
| Wails CancelTask | 显式预期 task ID，幂等、拒绝误取消新任务 |
| readiness | starting/ready/degraded/stopping/stopped，工具错误可诊断 |
| bootstrap/events | 当前任务、最后结果、revision；防止旧响应覆盖新状态 |
| TaskProgress | cancellable/commit 状态，UI 不承诺已越过提交点的操作能回滚 |
| TaskResult/RestoreOutcome | 取消/失败仍携带保护快照；部分完成、结果是否已知、已确认文件数及恢复候选 |
| KeyInfo | 私钥可用与 canEncrypt/encryptionError 分开 |
| Delete request | snapshot ID + expected directory + expected SHA；后端当前配置仍为权限边界 |
| Snapshot | 实际 scope；conflict/partial 等状态有明确阻断规则 |
| Stage/validate/install | 有类型和哈希的任务 inventory；不能只传 files/bytes 后推断全部完成 |
| manifest | 保持 schema 1 可读，可选新增 captureStatus；不重写历史文件 |
| config | 尽量保留 schema 1；LastScheduledAt 由服务端维护名义 occurrence |

任何 DTO 变化需同步 Go、Rust serde、TS types、bridge、mock、UI 错误分支及测试。不要把 `status=ready` 重新解释成“已完整校验”。

## 16. 回归用例矩阵

### 16.1 迁移原 23 个场景

| 用例/原测试函数 | 新实现必须满足 |
| --- | --- |
| FirstScheduledOccurrence/daily | 跨过武装时刻执行一次，不推到明天 |
| FirstScheduledOccurrence/weekly | 跨过武装时刻执行一次，不推到下周 |
| WeeklyScheduleUsesCurrentSettings | 新星期/时间决定下一次，而非旧执行日 |
| MismatchedKeysMustRejectBackup | 备份前明确配对错误；旧有效私钥解密仍可尝试 |
| EmptyCriticalDirectories | 空 sessions/profiles/storages 可备份并恢复 |
| DuplicateManifestRetention | 两个唯一归档保留两份，复制清单不触发误删 |
| RetentionPreservesManifestOnDeleteFailure | 归档被锁时保留清单，返回清理失败/警告 |
| CancelledSaveMustNotDeleteOldBackup | 保存提交点前取消不发布、不清理旧备份 |
| CustomDotTargetMustNotOverwriteLiveData | custom livepath+`\.` 拒绝且真实目标不变 |
| JunctionDestinationNotAutoDeleted | 别名进入 temp 被拒；已有冲突配置启动不清理其数据 |
| ProtectionRejectsCompressionOmission | payload 被跳过，即使 archive test 通过也不安装 |
| RestoreDestinationJunctionCanary | 外部 canary 内容/属性不变 |
| LongChineseFilename | 86 汉字加扩展名正常处理，不按 UTF-8 bytes 拒绝 |
| RestoreExistingHiddenFile | 支持矩阵内正常安全替换，不先破坏旧文件属性 |
| RestoreDestinationHardlinkCanary | 只替换选定目录项，外部硬链接不变 |
| CopyFailurePreservesExistingFile | 区间锁诱发读取失败后，原目标完整存在 |
| SortChronologicalAcrossOffsets | 按实际时刻排序与保留 |
| PreCancelledRPCNotSent | writer 未收到已取消请求 |
| RPCDeadlineIncludesWrite | 受控超时进入终止流程，writer 最终被回收 |
| CancellationWaitsForPeerTermination | 取消 ACK 后仍占用任务直到原工作终止 |
| ConcurrentStartupConfigRead | race detector 无冲突，UI 不拿到混合状态 |
| Unexpected7zMustNotExecute | 哈希错误的无害程序没有执行标记 |
| UnicodeStderrMustNotCrashSidecar | 中文大错误输出可诊断，client 仍健康或按协议正常报错，不 panic |

取消测试需按新契约更新：不能要求 API 在请求取消的瞬间就返回终态；应断言“请求已确认”和“任务已结束”两个时点。

### 16.2 必须补的新测试

- 密钥私有能力与公共能力分开；private-only 恢复；重建公钥仍是原 key ID；历史错记 key ID 可以依实际 unwrap 判断。
- no-replace 发布撞名、已有孤儿 manifest、跨卷 partial、manifest 发布失败及取消闸门两侧。
- 不可读源、遍历失败、同大小内容变化、额外归档项、空根、只有桌面态的历史保护快照。
- 目标只读/隐藏/system/DACL/短名及不同卷能力组合；候选清理失败、文件 sync/rename 失败、提交结果不确定。
- 同步/异步取消、注册屏障、每个耗时 RPC、所有 7z 退出路径、真实不读管道替身、残余后代、重复关窗。
- 自动保留时 conflict、missing、partial、预恢复快照、扫描错误、删除期间文件替换、至少 N 份真正不同的候选。
- scheduler busy/DSH/错误/持久化失败、重启、睡眠、DST、时间回拨、设置与手动任务竞争。
- 删除→刷新→撤销，删除→新通知，连续删除，待删除→切目录同 ID，删除提交→后台任务，卸载前待删除取消。
- Restore 弹窗打开→后台任务进度→Escape 可关闭；键盘 radio、折叠 inert、剪贴板失败、高级参数 12/13。
- 生产 bundle 不含 mock 启用入口；新旧事件排序、页面重载、sidecar 死亡后的健康状态。
- 打包哨兵不丢，ZIP 无用户数据、密钥、临时候选；旧格式样本继续可读。

## 17. 实施顺序、并行边界与检查点

| 批次 | 内容 | 进入下一步条件 |
| --- | --- | --- |
| A | W0 测试基线与 fixture；W1 密钥/7z 独立修复 | 回归可稳定复现，密钥无误换/误阻恢复 |
| B | W4 生命周期基础；并行研究 W2 Windows helper | 取消/终止/初始化测试通过，替换能力有实测 |
| C | W2 接入恢复、W3 独立采集及严格保护 | 外部哨兵与旧文件不被错误路径/复制失败破坏 |
| D | W5 发布/保留；W8 解析、资源和兼容 | 不完整备份不清旧档，发布与清理失败可解释 |
| E | W6 调度；W7/W9 前端与小问题 | 计划不会推动不安全链路，UI 与后端语义一致 |
| F | W10 打包、全链路与真实桌面验收 | 测试/实际程序/发行文件共同通过，等待发布授权 |

- 可以并行处理测试设计、Rust helper、密钥、前端测试工具，但共享 `manager.go`、`app.go`、`store.ts`、DTO 的改动应由一位整合者串行合并。
- 每批提交应只在用户明确要求提交时创建；否则保留可审阅的工作区改动。
- 不为完成所有包而一次性重写大文件。每个修复附“旧行为、新行为、故障用例、兼容变化”。
- 阻断时报告准确 API/环境/失败测试；禁止跳过安全检查、删除测试、放宽断言或调大超时伪造通过。

### 每个工作包交付格式

```text
工作包：Wn
已修问题：Fxx...
修改文件与主要函数：...
新增/修改测试：...
执行命令与真实结果：...
验证环境与二进制来源：...
旧数据/配置兼容结论：...
仍未验证或仍受限：...
是否需要用户决策：...
```

## 18. 验证命令与执行注意

以下是实施后的验证模板，从仓库根目录在 PowerShell 执行。它不会调用当前有删除风险的 `build.ps1`。先确认 Go、Rust MSVC、Node/npm、7z 和运行 Go race 所需 C 工具链可用。

```powershell
$ErrorActionPreference = 'Stop'
$repo = (Get-Location).Path
$app = Join-Path $repo 'app\DSHBackup'
$validation = Join-Path $env:TEMP ('dsh-repair-validation-' + [guid]::NewGuid())
New-Item -ItemType Directory -Path $validation | Out-Null

Push-Location (Join-Path $app 'sidecar')
try {
    cargo build --release --locked
    if ($LASTEXITCODE -ne 0) { throw 'Rust release build failed' }
    cargo test --locked
    if ($LASTEXITCODE -ne 0) { throw 'Rust tests failed' }
    cargo test --release --locked
    if ($LASTEXITCODE -ne 0) { throw 'Rust release tests failed' }
    cargo clippy --locked --all-targets -- -D warnings
    if ($LASTEXITCODE -ne 0) { throw 'Clippy failed' }
    cargo fmt -- --check
    if ($LASTEXITCODE -ne 0) { throw 'Rust formatting failed' }
} finally { Pop-Location }

Push-Location $app
try {
    if (-not (Test-Path 'sidecar\target\release\dsh-backup-core.exe')) {
        throw 'Missing freshly built integration sidecar'
    }
    if (-not (Test-Path '..\..\tools\7zip\7z2605-extra\x64\7za.exe')) {
        throw 'Missing pinned 7z'
    }
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
    go test -race -count=1 -v ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go race/tests failed' }
    $profile = Join-Path $validation 'coverage.out'
    go test -count=1 -coverpkg=./... "-coverprofile=$profile" ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go coverage run failed' }
    go tool cover "-func=$profile"
    if ($LASTEXITCODE -ne 0) { throw 'Go coverage report failed' }
} finally { Pop-Location }

Push-Location (Join-Path $app 'frontend')
try {
    npm ci
    if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
    npm run build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend typecheck/build failed' }
} finally { Pop-Location }

git status --short
git diff --check
if ($LASTEXITCODE -ne 0) { throw 'Whitespace check failed' }
```

- 前端目前只有 dev/build/preview，没有现成 test 脚本。W0 新增后建议约定 `test:unit`（Vitest 单次执行）和 `test:e2e`（Playwright），再加入验收命令；不能把尚不存在的命令写成已验证。
- 发布验证应检查 `go test -json` 的 skip 事件；必要 Windows 文件系统/二进制用例缺环境时必须记为未验收，而不是绿色完成。
- 前端 Vite build 会清理输出目录，可能删除被跟踪的 `frontend/dist/.gitkeep`；记录构建前状态，只恢复由本次构建导致的占位文件变化，不能覆盖用户修改。
- 需要 Wails build 时先单独构建并复制程序到隔离运行环境，确认真实桌面行为。W10 修完并通过哨兵测试前，不把原 `build.ps1` 当常规验证命令。
- 覆盖率用于定位遗漏，不设“达到某个百分比就安全”的替代目标；故障测试和真实产物验证不可省略。

## 19. 回退与失败保全

- 代码回退不等于数据回滚；保留归档、清单、私钥、原配置和恢复证据，不运行 reset/clean 删除它们。
- App、sidecar、frontend 作为匹配版本回退，先停调度并确认所有工作已结束；不能单独回退 RPC 一侧。
- 使用新字段但保持旧格式可读；旧程序会忽略新增字段，因此回退后不继续依赖旧版危险自动清理/调度，默认先用手动检查。
- 若失败发生在目标文件名称提交期间，报告实际可见文件与候选，不自动“重试恢复全部”，避免覆盖已经发生的新变化。
- 用户真实数据修复/找回是独立任务，需要明确源、目标、授权与保护措施；不能顺手用本方案测试覆盖真实目录。

## 20. 官方资料与需谨慎解释的 API

已核对的官方资料：

- [FILE_RENAME_INFO / FileRenameInfoEx](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_rename_info)：结构体与扩展 flags 的使用入口；最低结构体版本不等于所有扩展行为均受支持。
- [FILE_RENAME_INFORMATION flags](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/ntifs/ns-ntifs-_file_rename_information)：替换、忽略只读与权限要求；实现仍须验证用户态 API 和目标卷能力。
- [ReplaceFileW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-replacefilew)：同卷要求、属性/ACL、1175/1176/1177 状态及不支持的 WRITE_THROUGH。
- [Windows Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)：后代继承、nested jobs、kill-on-close；进程退出确认不能只靠普通通知。
- [CancelIoEx](https://learn.microsoft.com/en-us/windows/win32/api/ioapiset/nf-ioapiset-cancelioex)：请求取消不等于 I/O 已结束，必须等待 completion。

这些资料支持设计边界，不构成当前项目在所有 Windows/文件系统组合上的验收证明。

## 21. 可直接交给下一模型的任务说明

> 请先完整阅读本文并确认当前仓库是否仍基于 cdada4c。本文主体是修复方案；第 22 节记录了本轮已实施内容、验证命令和仍存在的边界，不能把“已实施”理解为发布授权。
>
> 在不重写技术栈、不破坏旧归档/密钥/配置、不触碰真实用户数据的前提下，按第 17 节分批执行。一般数据仍在应用旁；用户已批准仅恢复提交候选可以短暂位于目标文件旁。
>
> 先建立第 16 节回归，用失败测试证明旧问题，再修改代码。不要为了“保持接口不变”保留错误语义，也不要通过大重构、弱化断言、吞错误或关闭检查绕过问题。
>
> 每批报告真实改动、命令结果、二进制来源和未覆盖环境；前端必须在浏览器使用，最终桌面行为必须验证重建 exe。需要权限、安装新系统组件、变更发布流程授权、提交/推送/发布或操作真实数据时单独确认。
>
> 最终交付不是一句“测试通过”，而是问题—修复—故障用例的对应表、兼容结论、真实桌面验证、干净发行包证明，以及仍然存在的边界说明。

## 22. 实施状态（2026-10-30）

### 22.1 已完成

| 工作包 | 状态 | 关键落点 |
| --- | --- | --- |
| W1 密钥与工具完整性 | 完成 | `keys_info` 分离私钥可用性与 `canEncrypt`；错配阻止新加密备份；7z 在任何执行前由 Go 校验固定 SHA-256 |
| W2 路径与安全替换 | 完成（ACL 例外见 22.3） | 新增 `internal/pathguard`，用最终路径/目录身份检查；Rust 拒绝目标内重解析点，候选文件写完校验后以 `FileRenameInfoEx` 替换；临时目录只清理带任务标记的自有目录；隐藏/只读/硬链接/junction 回归通过 |
| W3 采集与保护快照 | 完成 | 独立复制、空目录入库、源错误显式失败；保护快照严格 exit 0、清单回读、文件集合/大小/空目录核对；取消/失败保留保护信息 |
| W4 生命周期 | 核心完成（stdio 兜底、重载任务快照未加，见 22.3） | Rust 按 request ID 先注册后启动 worker；Go 取消等待终态并设置宽限；Windows Job Object + KILL_ON_JOB_CLOSE；启动就绪屏障、配置锁、sidecar 健康状态；提交前取消不再发布 |
| W5 清单/保留/删除 | 完成 | 同一 ID 清单合并别名、冲突标记；实际时刻排序；partial 不占保留名额；失败保留清单；删除绑定选择时目录身份 |
| W6 调度 | 完成 | 稳定名义 occurrence、首次武装、运行中合并、关闭期间不追补、计划编辑重置水位、失败暂停 |
| W7 前端状态 | 完成 | 待删除状态独立、刷新不重复、通知不挤掉撤销、目录切换取消、恢复弹窗可关闭、失败显示保护快照 |
| W8 兼容与解析 | 部分完成 | stderr 字节安全截断、UTF-16 长度、严格列表解析、重复/冲突/空目录校验、部分资源上限；完整恶意归档资源隔离仍未完成 |
| W9 小问题 | 完成 | 高级参数 inert、radio name、剪贴板错误、参数数量、更新 URL 规范化 |
| W10 打包 | 完成（本地验证） | 干净 staging 组装、保留 `data`/`backups`、staging 清理；`-SkipZip` 构建通过 |

### 22.2 验证记录

- `go test -race -count=1 ./...`：全部通过，含新增 `manager_safety_test.go` 的 junction、硬链接、只读目标、复制失败、保护快照漏文件、空目录往返，以及 sidecar 取消测试。
- `cargo test --locked`：10 项通过；`cargo clippy --locked --all-targets -- -D warnings`、`cargo fmt -- --check` 通过。
- `npm run build`：`vue-tsc` 与生产构建通过。
- 浏览器开发态：撤销与刷新不产生重复项；无关通知不替换撤销；恢复弹窗在后台任务时可关闭；折叠参数 `inert`；radio 分组生效。
- 原审查的 23 个场景中，安全与行为场景已通过；仅“伪造 writer 在超时后仍阻塞”这一测试替身不能证明真实管道终止，改由真实 sidecar 取消测试覆盖。
- `build.ps1 -SkipZip` 实际构建成功；放入 `dist/DSHBackup/data` 与 `backups` 的哨兵均保留，暂存目录已清理。未覆盖发布 ZIP 的既有 1.2.1 文件。
- 将运行文件复制到临时目录启动重建的 `DSHBackup.exe`，真实 WebView2 首启向导正常显示（1280×820）；未执行备份/恢复，未触碰真实 `.dsh`。

### 22.3 明确剩余边界

1. **ACL**：安全替换目前保留只读/隐藏/系统/归档属性，但候选文件继承目标目录 ACL，不复制原文件自定义 DACL；不宣称 ACL 保存。
2. **候选残留**：强制结束/断电可能在目标旁留下 `.dshbak-*.tmp`；程序不会自动删除无法确认归属的外部文件。
3. **管道兜底**：实现采用 stdio + 超时终止 Job，而不是 overlapped named pipe；真实测试已验证取消，但没有声明所有内核 I/O 都能即时停止。
4. **页面重载**：Bootstrap 尚未携带当前运行任务/结果快照与 revision；关掉前端页面再打开时只能依赖后续事件，不能恢复正在进行的进度卡。数据任务本身仍受后端任务槽保护。
5. **恶意归档资源**：已拒绝重复/冲突/越界及部分上限，但 `7z l -slt` 文本接口、列表内存和磁盘炸弹的完整隔离仍未实现；不宣称可安全处理任意恶意归档。
6. **跨卷/文件系统矩阵**：候选替换依赖同卷 rename；NTFS 已验证，exFAT/FAT32/SMB/挂载卷仍需在目标环境逐项验收。
7. **真实桌面**：本轮覆盖了重建 exe 的首启界面；未覆盖真实 `%USERPROFILE%\.dsh` 的恢复，发布前仍需在隔离环境执行正常/取消/异常完整流程。
8. **版本与发布**：试用构建已升为 1.2.3，生成独立 ZIP；未提交、未推送、未打标签、未创建 GitHub Release，也未覆盖旧版本 ZIP。任何发布动作都需要新的明确授权。

### 22.4 1.2.3 压缩档位补充（2026-10-30）

- 压缩档位扩展为六档：仅存储 `-mx=0`、极速 `-mx=1`、快速 `-mx=3`、均衡 `-mx=5`、高压缩 `-mx=7`、极限 `-mx=9`。
- 新安装默认“仅存储”，已有配置保留原档位；恢复前快照沿用当前档位，因此默认也是仅存储。
- 高级参数白名单移除 `-mx`；旧配置中的 `-mx` 在加载时按最后值迁移到最接近的档位并移除，旧归档不变。
- 设置页与首次引导保持现有胶囊控件，六档在 1080/1280 宽度下均能单行显示；说明文字随档位变化。
- 已通过 Go/Rust/前端构建和真实 sidecar 的“仅存储 + 加密 + 校验 + 恢复”集成路径。
- 已生成 `dist/DSHBackup-1.2.3-win64.zip`（SHA-256 `8f63d18ed968fa6ca470206cab3b38b1aed6a97d2f6e5a0d6266990adedb350f`），并从 ZIP 解压启动重建 exe，首启页显示六档且默认“仅存储”。未提交、推送或发布。
