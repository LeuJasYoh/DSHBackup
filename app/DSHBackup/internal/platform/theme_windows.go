//go:build windows

package platform

import "golang.org/x/sys/windows/registry"

// UsesLightTheme 读取 Windows 的“应用默认模式”，用于在首帧之前把窗口底色设成
// 与系统一致的颜色，避免暗色系统上启动时闪一下亮色。
func UsesLightTheme() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return true
	}
	defer key.Close()
	value, _, err := key.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return true
	}
	return value != 0
}
