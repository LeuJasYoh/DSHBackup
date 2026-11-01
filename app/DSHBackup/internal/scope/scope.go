// Package scope 是"哪些数据算用户数据"的唯一定义处：它把 `.dsh` 内的相对路径
// 归入备份域，并据此推导暂存与归档要跳过的清单。业务层与预览都从这里取值，
// 避免"预览大小"和"实际备份内容"各算一套。
package scope

import (
	"strings"
)

// Domain 是一组可以独立取舍、独立恢复的数据。
type Domain string

const (
	// DomainCore 是不可再生的身份、配置与账本：凭据、桌面登录态、profile 声明、
	// 插件自己写在 home 根的状态、workspace.json、用量账本。
	DomainCore Domain = "core"
	// DomainSessions 是会话日志与其引用的附件字节。两者必须同进同出：会话日志
	// 只保存引用，图片正文在 attachments 里。
	DomainSessions Domain = "sessions"
	// DomainPlugins 是插件依赖本体，可由 pnpm 按 package.json 与 lockfile 重装。
	DomainPlugins Domain = "plugins"
	// DomainRuntime 是应用把自己目录里的 payload 复制到 home 的一份运行环境，
	// 首次使用 Office 类技能时会在本地重新复制，不需要网络。
	DomainRuntime Domain = "runtime"
)

// Names 按备份时的体积从大到小排列，供界面固定顺序展示。
func Names() []Domain {
	return []Domain{DomainCore, DomainSessions, DomainPlugins, DomainRuntime}
}

// Reinstallable 表示该域的数据能在目标机器上重新生成，因此可以从备份里省略。
func (d Domain) Reinstallable() bool {
	return d == DomainPlugins || d == DomainRuntime
}

// Label 是界面与日志里可读的域名。
func (d Domain) Label() string {
	switch d {
	case DomainCore:
		return "身份与配置"
	case DomainSessions:
		return "会话与附件"
	case DomainPlugins:
		return "插件依赖"
	case DomainRuntime:
		return "运行环境"
	}
	return string(d)
}

// Selection 决定可取舍的两个域是否进入本次备份。
type Selection struct {
	Plugins bool
	Runtime bool
}

// Excludes 与 sidecar 的 stage 入参同形：顶层一级目录名、精确相对路径、
// 任意深度的子树前缀、任意深度的路径段名。
type Excludes struct {
	Top    []string
	Rel    []string
	Prefix []string
	Name   []string
}

// dsh 侧恒定排除的派生数据。cache 里的 request-images 与带 fetchedAt 的远端文档
// 都可重新取得；session_projcache 由上游"下一次写入自愈"。
var (
	dshExcludeTop     = []string{"logs", "cache", "electron-user-data"}
	dshExcludeRel     = []string{".dshw-usage.json.before-recharge-fix.bak"}
	dshExcludePrefix  = []string{"storages/session_projcache"}
	dshExcludeName    = []string{"__pycache__"}
	desktopExcludeTop = []string{
		"Cache", "Code Cache", "GPUCache", "GrShaderCache", "DawnWebGPUCache",
		"DawnGraphiteCache", "ShaderCache", "Dictionaries", "GPUPersistentCache",
		"Crashpad",
	}
	desktopExcludeRel = []string{"logs"}
)

// DSH 返回 .dsh 根本次要跳过的清单。
func DSH(selection Selection) Excludes {
	excludes := Excludes{
		Top:    append([]string{}, dshExcludeTop...),
		Rel:    append([]string{}, dshExcludeRel...),
		Prefix: append([]string{}, dshExcludePrefix...),
		Name:   append([]string{}, dshExcludeName...),
	}
	if !selection.Runtime {
		excludes.Prefix = append(excludes.Prefix, "dsh-runtimes")
	}
	if !selection.Plugins {
		excludes.Name = append(excludes.Name, "node_modules")
	}
	return excludes
}

// Desktop 返回桌面登录态根要跳过的清单；它没有可取舍的域。
func Desktop() Excludes {
	return Excludes{
		Top: append([]string{}, desktopExcludeTop...),
		Rel: append([]string{}, desktopExcludeRel...),
	}
}

// DomainOf 归类一条条目路径。path 是归档内的路径，带根名前缀，
// 例如 "dsh/profiles/desktop/node_modules/x/index.js"。
func DomainOf(path string) Domain {
	normalized := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	root, relative, found := strings.Cut(normalized, "/")
	if !found || root != "dsh" {
		return DomainCore
	}
	segments := strings.Split(relative, "/")
	if segments[0] == "dsh-runtimes" {
		return DomainRuntime
	}
	for _, segment := range segments {
		if segment == "node_modules" {
			return DomainPlugins
		}
	}
	if segments[0] == "sessions" || segments[0] == "attachments" {
		return DomainSessions
	}
	return DomainCore
}

// Entry 是暂存结果里的一条记录。
type Entry struct {
	Path   string
	Size   int64
	Folder bool
}

// Stat 是某个域在本次归档里的体量，写进清单供界面与恢复提示使用。
type Stat struct {
	Domain Domain `json:"domain"`
	Files  int64  `json:"files"`
	Bytes  int64  `json:"bytes"`
}

// Stats 汇总每个域的文件数与字节数，顺序固定，缺失的域不出现在结果里。
func Stats(entries []Entry) []Stat {
	counts := make(map[Domain]*Stat, len(Names()))
	for _, entry := range entries {
		if entry.Folder {
			continue
		}
		domain := DomainOf(entry.Path)
		stat, ok := counts[domain]
		if !ok {
			stat = &Stat{Domain: domain}
			counts[domain] = stat
		}
		stat.Files++
		stat.Bytes += entry.Size
	}
	var stats []Stat
	for _, name := range Names() {
		if stat, ok := counts[name]; ok {
			stats = append(stats, *stat)
		}
	}
	return stats
}

// Skipped 判断一条根内相对路径是否应被跳过。顶层排除按首段匹配，因此整棵子树
// 都算跳过：sidecar 靠 filter_entry 在深度一处剪枝，预览则遍历后逐个判定，两者结果一致。
func (e Excludes) Skipped(relative string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(relative, "\\", "/"))
	normalized = strings.Trim(normalized, "/")
	if normalized == "" {
		return false
	}
	first := strings.SplitN(normalized, "/", 2)[0]
	for _, value := range e.Top {
		if strings.ToLower(value) == first {
			return true
		}
	}
	for _, value := range e.Rel {
		if strings.ToLower(value) == normalized {
			return true
		}
	}
	for _, value := range e.Prefix {
		prefix := strings.ToLower(strings.Trim(value, "/"))
		if prefix == "" {
			continue
		}
		if normalized == prefix || strings.HasPrefix(normalized, prefix+"/") {
			return true
		}
	}
	if len(e.Name) > 0 {
		for _, segment := range strings.Split(normalized, "/") {
			for _, value := range e.Name {
				if strings.ToLower(value) == segment {
					return true
				}
			}
		}
	}
	return false
}
