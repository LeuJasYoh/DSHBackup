package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"DSHBackup/internal/apppath"
	"DSHBackup/internal/platform"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/windows/icon.ico
var trayIconData []byte

// windowBackground 与 style.css 的 --bg 保持一致，避免首帧出现错色。
func windowBackground() *options.RGBA {
	if platform.UsesLightTheme() {
		return &options.RGBA{R: 247, G: 246, B: 244, A: 255}
	}
	return &options.RGBA{R: 11, G: 11, B: 12, A: 255}
}

func main() {
	paths, err := apppath.Resolve()
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法确定应用目录：", err)
		os.Exit(1)
	}
	webviewDir := paths.WebViewDir
	if mkdirErr := os.MkdirAll(webviewDir, 0o755); mkdirErr != nil {
		cacheDir, cacheErr := os.UserCacheDir()
		if cacheErr != nil {
			fmt.Fprintln(os.Stderr, "无法创建 WebView2 数据目录：", mkdirErr)
			os.Exit(1)
		}
		webviewDir = filepath.Join(cacheDir, "DSHBackup", "webview2")
		if err := os.MkdirAll(webviewDir, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "无法创建 WebView2 数据目录：", err)
			os.Exit(1)
		}
	}
	app := NewApp(paths)
	app.trayIcon = trayIconData
	err = wails.Run(&options.App{
		Title:            "DSH Backup",
		Width:            1280,
		Height:           820,
		MinWidth:         1080,
		MinHeight:        700,
		DisableResize:    false,
		Frameless:        false,
		StartHidden:      false,
		BackgroundColour: windowBackground(),
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:     app.startup,
		OnShutdown:    app.shutdown,
		OnBeforeClose: app.beforeClose,
		Bind: []interface{}{
			app,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "DSHBackup-9f0b4d40-6a0a-4f6d-9c6b-2b5f31a8e7c1",
			OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
				app.showWindow()
			},
		},
		EnableDefaultContextMenu: true,
		Windows: &windows.Options{
			WebviewUserDataPath: webviewDir,
			Theme:               windows.SystemDefault,
			DisablePinchZoom:    true,
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "DSH Backup 启动失败：", err)
		os.Exit(1)
	}
}
