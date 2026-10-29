package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	RepositoryURL         = "https://github.com/LeuJasYoh/DSHBackup"
	RepositoryReleases    = RepositoryURL + "/releases"
	latestReleaseEndpoint = "https://api.github.com/repos/LeuJasYoh/DSHBackup/releases/latest"
	updateCacheDuration   = 30 * time.Minute
)

type githubRelease struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
}

func (a *App) OpenRepository() error {
	return a.openExternal(RepositoryURL)
}

func (a *App) OpenReleasePage(rawURL string) error {
	return a.openExternal(safeReleaseURL(rawURL))
}

func (a *App) openExternal(rawURL string) error {
	if a.ctx == nil {
		return errors.New("应用尚未启动")
	}
	runtime.BrowserOpenURL(a.ctx, rawURL)
	return nil
}

func safeReleaseURL(rawURL string) string {
	if rawURL == "" {
		return RepositoryReleases
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") {
		return RepositoryReleases
	}
	if !strings.HasPrefix(parsed.Path, "/LeuJasYoh/DSHBackup/releases") {
		return RepositoryReleases
	}
	return rawURL
}

func (a *App) CheckForUpdates(force bool) UpdateInfo {
	now := time.Now()
	a.updateMu.Lock()
	if !force && !a.lastUpdateAt.IsZero() && now.Sub(a.lastUpdateAt) < updateCacheDuration {
		cached := a.lastUpdate
		a.updateMu.Unlock()
		return cached
	}
	a.updateMu.Unlock()

	info := UpdateInfo{CurrentVersion: Version, CheckedAt: now.Format(time.RFC3339)}
	ctx := context.Background()
	if a.ctx != nil {
		ctx = a.ctx
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	release, err := fetchLatestRelease(checkCtx, &http.Client{Timeout: 8 * time.Second}, latestReleaseEndpoint)
	if err != nil {
		info.Error = friendlyUpdateError(err)
		a.cacheUpdate(info, now)
		return info
	}
	info.LatestVersion = strings.TrimPrefix(release.TagName, "v")
	info.URL = safeReleaseURL(release.HTMLURL)
	info.PublishedAt = release.PublishedAt
	newer, compareErr := isNewerVersion(Version, release.TagName)
	if compareErr != nil {
		info.Error = compareErr.Error()
	} else {
		info.Available = newer
	}
	a.cacheUpdate(info, now)
	return info
}

func (a *App) cacheUpdate(info UpdateInfo, at time.Time) {
	a.updateMu.Lock()
	a.lastUpdate = info
	a.lastUpdateAt = at
	a.updateMu.Unlock()
}

func fetchLatestRelease(ctx context.Context, client *http.Client, endpoint string) (githubRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return githubRelease{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "DSHBackup/"+Version)
	response, err := client.Do(request)
	if err != nil {
		return githubRelease{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return githubRelease{}, errors.New("仓库还没有发行版")
	}
	if response.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("GitHub 返回 HTTP %d", response.StatusCode)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("无法解析 GitHub 发行版信息：%w", err)
	}
	if release.Draft || release.Prerelease {
		return githubRelease{}, errors.New("最新发行版不是正式版本")
	}
	if strings.TrimSpace(release.TagName) == "" {
		return githubRelease{}, errors.New("GitHub 发行版缺少版本号")
	}
	return release, nil
}

func friendlyUpdateError(err error) string {
	if err == nil {
		return ""
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded") {
		return "检查更新超时，请检查网络或在设置中关闭自动检查。"
	}
	return "检查更新失败：" + err.Error()
}

func isNewerVersion(current, latest string) (bool, error) {
	currentParts, err := versionParts(current)
	if err != nil {
		return false, fmt.Errorf("当前版本号无效：%w", err)
	}
	latestParts, err := versionParts(latest)
	if err != nil {
		return false, fmt.Errorf("最新版本号无效：%w", err)
	}
	length := len(currentParts)
	if len(latestParts) > length {
		length = len(latestParts)
	}
	for index := 0; index < length; index++ {
		currentPart := 0
		if index < len(currentParts) {
			currentPart = currentParts[index]
		}
		latestPart := 0
		if index < len(latestParts) {
			latestPart = latestParts[index]
		}
		if latestPart != currentPart {
			return latestPart > currentPart, nil
		}
	}
	return false, nil
}

func versionParts(value string) ([]int, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(strings.TrimPrefix(value, "v"), "V")
	if index := strings.IndexAny(value, "-+"); index >= 0 {
		value = value[:index]
	}
	if value == "" {
		return nil, errors.New("版本号为空")
	}
	rawParts := strings.Split(value, ".")
	parts := make([]int, 0, len(rawParts))
	for _, rawPart := range rawParts {
		part, err := strconv.Atoi(rawPart)
		if err != nil || part < 0 {
			return nil, fmt.Errorf("无法解析版本号 %q", value)
		}
		parts = append(parts, part)
	}
	return parts, nil
}
