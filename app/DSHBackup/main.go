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
)

//go:embed all:frontend/dist
var assets embed.FS

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
	err = wails.Run(&options.App{
		Title:            "DSH Backup",
		Width:            1280,
		Height:           820,
		MinWidth:         1080,
		MinHeight:        700,
		DisableResize:    false,
		Frameless:        false,
		StartHidden:      false,
		BackgroundColour: &options.RGBA{R: 246, G: 247, B: 249, A: 255},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "DSHBackup-9f0b4d40-6a0a-4f6d-9c6b-2b5f31a8e7c1",
			OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
				app.emitNotice("warning", "DSH Backup 已经在运行")
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
