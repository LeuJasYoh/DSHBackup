package main

import (
	"os"
	"path/filepath"
	"testing"

	"DSHBackup/internal/scope"
)

func TestStatSourceCountsWholeSkippedSubtrees(t *testing.T) {
	root := t.TempDir()
	write := func(relative string, content string) {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".credentials.yaml", "key\n")
	write("sessions/a.jsonl", "session-record")
	write("logs/old/day-1.log", "aaaaaaaa")
	write("logs/old/deep/day-2.log", "bbbbbbbb")
	write("dsh-runtimes/dsh-primary-runtime/dependencies/node/bin/node.exe", "cccccccc")
	write("storages/session_projcache/sessions/s.json", "dddddddd")

	info := statSource("dsh", "DSH 核心数据", root, scope.DSH(scope.Selection{Plugins: true, Runtime: false}))
	if !info.Exists {
		t.Fatalf("预览应认为目录存在：%s", info.Error)
	}
	if info.Files != 2 || info.Bytes != int64(len("key\n")+len("session-record")) {
		t.Fatalf("计入归档的统计不符：%+v", info)
	}
	// 被排除的子树必须整棵计入跳过量，不能只算到顶层那层。
	if info.SkippedFiles != 4 {
		t.Fatalf("跳过文件数应为 4（两层日志 + 运行环境 + 投影缓存），实际 %d", info.SkippedFiles)
	}
	if info.SkippedBytes != int64(4*8) {
		t.Fatalf("跳过字节数应为 32，实际 %d", info.SkippedBytes)
	}
	domains := map[scope.Domain]int64{}
	for _, stat := range info.Domains {
		domains[stat.Domain] = stat.Bytes
	}
	if len(info.Domains) != 2 || domains[scope.DomainSessions] != int64(len("session-record")) {
		t.Fatalf("域统计不符：%+v", info.Domains)
	}
	if _, ok := domains[scope.DomainRuntime]; ok {
		t.Fatalf("运行环境已关闭，不应出现在域统计里：%+v", info.Domains)
	}
}

func TestStatSourceReportsRuntimeWhenIncluded(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dsh-runtimes", "dsh-primary-runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dsh-runtimes", "dsh-primary-runtime", "runtime.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".credentials.yaml"), []byte("key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info := statSource("dsh", "DSH 核心数据", root, scope.DSH(scope.Selection{Plugins: true, Runtime: true}))
	var found bool
	for _, stat := range info.Domains {
		if stat.Domain == scope.DomainRuntime {
			found = true
		}
	}
	if !found {
		t.Fatalf("打开运行环境后应统计到该域：%+v", info.Domains)
	}
	if info.SkippedFiles != 0 {
		t.Fatalf("两域都开时不应有跳过项：%+v", info)
	}
}
