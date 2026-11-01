package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"DSHBackup/internal/scope"
)

// TestRealHomeDomainPreview 用真机数据核对域规则覆盖面：它只读遍历 `%USERPROFILE%\.dsh`，
// 不写任何东西，默认跳过。核对排除规则是否漏掉某个形状的路径时，用
// `DSHBACKUP_REAL_SCAN=1 go test . -run RealHomeDomainPreview -v` 跑一次。
func TestRealHomeDomainPreview(t *testing.T) {
	if os.Getenv("DSHBACKUP_REAL_SCAN") != "1" {
		t.Skip("设置 DSHBACKUP_REAL_SCAN=1 才对真实 home 目录做只读核对")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, ".dsh")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("没有 %s，跳过真机核对", root)
	}
	for _, selection := range []scope.Selection{
		{Plugins: true, Runtime: false},  // 产品默认
		{Plugins: true, Runtime: true},   // 全开
		{Plugins: false, Runtime: false}, // 最省
	} {
		excludes := scope.DSH(selection)
		var included, excluded int64
		var includedFiles, excludedFiles int
		seen := map[scope.Domain]int64{}
		err := filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				t.Logf("遍历告警：%s：%v", current, walkErr)
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			relative, relErr := filepath.Rel(root, current)
			if relErr != nil {
				return nil
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return nil
			}
			slash := filepath.ToSlash(relative)
			if excludes.Skipped(slash) {
				excluded += info.Size()
				excludedFiles++
				return nil
			}
			included += info.Size()
			includedFiles++
			seen[scope.DomainOf("dsh/"+slash)] += info.Size()
			// 关闭的域必须一个字节都不进归档。
			domain := scope.DomainOf("dsh/" + slash)
			if domain == scope.DomainRuntime && !selection.Runtime {
				t.Errorf("运行环境已关闭却被计入：%s", slash)
			}
			if domain == scope.DomainPlugins && !selection.Plugins {
				t.Errorf("插件依赖已关闭却被计入：%s", slash)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		const mib = 1024 * 1024
		var domains []string
		for _, name := range scope.Names() {
			if value, ok := seen[name]; ok {
				domains = append(domains, fmt.Sprintf("%s=%.1f MiB", name, float64(value)/(1024*1024)))
			}
		}
		t.Logf("插件=%v 运行环境=%v：计入 %.1f MiB/%d 文件，跳过 %.1f MiB/%d 文件；%s",
			selection.Plugins, selection.Runtime,
			float64(included)/mib, includedFiles,
			float64(excluded)/mib, excludedFiles,
			strings.Join(domains, " "))
		if included == 0 {
			t.Fatal("真实 home 下没有暂存到任何文件，规则一定过窄")
		}
	}
}
