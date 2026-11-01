package scope

import (
	"reflect"
	"testing"
)

func TestDomainOfClassifiesByReinstallability(t *testing.T) {
	cases := []struct {
		path string
		want Domain
	}{
		{"dsh/.credentials.yaml", DomainCore},
		{"dsh/.dshw-usage.json", DomainCore},
		{"dsh/storages/workspace.json", DomainCore},
		{"dsh/profiles/desktop/package.json", DomainCore},
		{"dsh/whale-roles/roles.json", DomainCore},
		{"dsh/sessions/ws/session.v4.jsonl.zstd", DomainSessions},
		{"dsh/attachments/v1/objects/ab/abcd", DomainSessions},
		{"dsh/profiles/desktop/node_modules/pkg/index.js", DomainPlugins},
		{"dsh/profiles/node_modules/pkg/index.js", DomainPlugins},
		{"dsh/dsh-runtimes/dsh-primary-runtime/dependencies/node/bin/node.exe", DomainRuntime},
		// 运行环境优先：它整棵子树都同进同出。
		{"dsh/dsh-runtimes/x/node_modules/y.js", DomainRuntime},
		// 插件树里的 dsh-runtimes 目录仍属插件域。
		{"dsh/profiles/desktop/node_modules/pkg/dsh-runtimes/z", DomainPlugins},
		{"desktop-state/Network/Cookies", DomainCore},
		{"dsh", DomainCore},
	}
	for _, testCase := range cases {
		if got := DomainOf(testCase.path); got != testCase.want {
			t.Errorf("DomainOf(%q) = %s，期望 %s", testCase.path, got, testCase.want)
		}
	}
}

func TestDSHExcludesFollowSelection(t *testing.T) {
	all := DSH(Selection{Plugins: true, Runtime: true})
	if !contains(all.Prefix, "storages/session_projcache") || !contains(all.Name, "__pycache__") {
		t.Fatalf("派生缓存应恒定排除，实际 %+v", all)
	}
	for _, name := range []string{"logs", "cache", "electron-user-data"} {
		if !contains(all.Top, name) {
			t.Errorf("顶层应排除 %s", name)
		}
	}
	if contains(all.Prefix, "dsh-runtimes") || contains(all.Name, "node_modules") {
		t.Errorf("两个域都打开时不应有额外排除，实际 %+v", all)
	}

	defaults := DSH(Selection{Plugins: true, Runtime: false})
	if !contains(defaults.Prefix, "dsh-runtimes") {
		t.Error("默认应跳过运行环境")
	}
	if contains(defaults.Name, "node_modules") {
		t.Error("默认不应跳过插件依赖")
	}

	lean := DSH(Selection{Plugins: false, Runtime: false})
	if !contains(lean.Name, "node_modules") || !contains(lean.Prefix, "dsh-runtimes") {
		t.Errorf("两域都关时都应排除，实际 %+v", lean)
	}
	// 返回的清单必须是副本，调用方改动不能污染下一次备份。
	lean.Top = append(lean.Top, "unrelated")
	if contains(DSH(Selection{}).Top, "unrelated") {
		t.Error("DSH 返回了共享的排除切片")
	}
}

func TestSkippedMatchesSidecarSemantics(t *testing.T) {
	excludes := DSH(Selection{Plugins: true, Runtime: false})
	cases := []struct {
		relative string
		want     bool
	}{
		{"logs", true},
		{"cache", true},
		{"cache/attachments/request-images/ab/cd", true},
		{"logs/old/desh.log", true},
		{"electron-user-data/next-updates", true},
		{"storages/session_projcache/sessions/s.json", true},
		{"storages/session_projcache", true},
		{"storages/session_projcache-backup/x.json", false},
		{"sessions/storages/session_projcache/x.json", false},
		{"dsh-runtimes/dsh-primary-runtime/runtime.json", true},
		{"dsh-runtimes/dsh-primary-runtime/dependencies/node/bin/node.exe", true},
		{".dshw-usage.json.before-recharge-fix.bak", true},
		{"profiles/desktop/node_modules/pkg/__pycache__/a.pyc", true},
		{"profiles/desktop/package.json", false},
		{"sessions/ws/a.jsonl", false},
		{"storages/workspace.json", false},
	}
	for _, testCase := range cases {
		if got := excludes.Skipped(testCase.relative); got != testCase.want {
			t.Errorf("Skipped(%q) = %v，期望 %v", testCase.relative, got, testCase.want)
		}
	}
	// 反斜杠同样成立（sidecar 会先归一化，预览走 Windows 原生分隔符）。
	if !excludes.Skipped(`storages\session_projcache\s.json`) {
		t.Error("反斜杠路径应被前缀排除")
	}
}

func TestStatsGroupsAndOrders(t *testing.T) {
	entries := []Entry{
		{Path: "dsh/.credentials.yaml", Size: 10},
		{Path: "dsh/storages", Folder: true},
		{Path: "dsh/sessions/a.jsonl", Size: 40},
		{Path: "dsh/attachments/c.bin", Size: 5},
		{Path: "dsh/profiles/desktop/node_modules/p/index.js", Size: 100},
		{Path: "desktop-state/Network/Cookies", Size: 7},
	}
	want := []Stat{
		{Domain: DomainCore, Files: 2, Bytes: 17},
		{Domain: DomainSessions, Files: 2, Bytes: 45},
		{Domain: DomainPlugins, Files: 1, Bytes: 100},
	}
	got := Stats(entries)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Stats 结果不符：\n got %+v\nwant %+v", got, want)
	}
	if Stats(nil) != nil {
		t.Error("空条目应返回 nil，避免清单里出现空域数组")
	}
}

func TestDomainLabelsAndReinstallable(t *testing.T) {
	if DomainRuntime.Reinstallable() != true || DomainCore.Reinstallable() != false {
		t.Error("可再生标记错误")
	}
	for _, name := range Names() {
		if name.Label() == string(name) && name != "" {
			t.Errorf("域 %s 缺少界面标签", name)
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
