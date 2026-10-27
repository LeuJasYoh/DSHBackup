package github

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"DSHBackup/internal/manifest"
)

type Asset struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	ContentType        string `json:"contentType"`
	URL                string `json:"url"`
	BrowserDownloadURL string `json:"browserDownloadUrl"`
	Digest             string `json:"digest"`
	State              string `json:"state"`
}

type Release struct {
	ID          int64   `json:"id"`
	TagName     string  `json:"tagName"`
	Name        string  `json:"name"`
	Body        string  `json:"body"`
	HTMLURL     string  `json:"htmlUrl"`
	CreatedAt   string  `json:"createdAt"`
	PublishedAt string  `json:"publishedAt"`
	Draft       bool    `json:"draft"`
	Prerelease  bool    `json:"prerelease"`
	UploadURL   string  `json:"uploadUrl"`
	Assets      []Asset `json:"assets"`
}

type releasePayload struct {
	ID          int64          `json:"id"`
	TagName     string         `json:"tag_name"`
	Name        string         `json:"name"`
	Body        string         `json:"body"`
	HTMLURL     string         `json:"html_url"`
	CreatedAt   string         `json:"created_at"`
	PublishedAt string         `json:"published_at"`
	Draft       bool           `json:"draft"`
	Prerelease  bool           `json:"prerelease"`
	UploadURL   string         `json:"upload_url"`
	Assets      []assetPayload `json:"assets"`
}

type assetPayload struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	ContentType        string `json:"content_type"`
	URL                string `json:"url"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
	State              string `json:"state"`
}

func convertRelease(payload releasePayload) Release {
	assets := make([]Asset, 0, len(payload.Assets))
	for _, item := range payload.Assets {
		assets = append(assets, Asset{
			ID:                 item.ID,
			Name:               item.Name,
			Size:               item.Size,
			ContentType:        item.ContentType,
			URL:                item.URL,
			BrowserDownloadURL: item.BrowserDownloadURL,
			Digest:             item.Digest,
			State:              item.State,
		})
	}
	return Release{
		ID:          payload.ID,
		TagName:     payload.TagName,
		Name:        payload.Name,
		Body:        payload.Body,
		HTMLURL:     payload.HTMLURL,
		CreatedAt:   payload.CreatedAt,
		PublishedAt: payload.PublishedAt,
		Draft:       payload.Draft,
		Prerelease:  payload.Prerelease,
		UploadURL:   payload.UploadURL,
		Assets:      assets,
	}
}

type UploadRequest struct {
	Owner         string
	Repo          string
	Tag           string
	Name          string
	Body          string
	ArchivePath   string
	ArchiveName   string
	ArchiveSize   int64
	ArchiveSHA256 string
	ManifestName  string
	ManifestBytes []byte
}

type UploadResult struct {
	ReleaseID       int64
	Tag             string
	HTMLURL         string
	ArchiveAssetID  int64
	ManifestAssetID int64
	ArchiveSize     int64
}

func (c *Client) createRelease(ctx context.Context, owner, repo string, req UploadRequest) (Release, error) {
	body := map[string]any{
		"tag_name":   req.Tag,
		"name":       req.Name,
		"body":       req.Body,
		"draft":      true,
		"prerelease": false,
	}
	path := fmt.Sprintf("/repos/%s/%s/releases", url.PathEscape(owner), url.PathEscape(repo))
	var payload releasePayload
	if err := c.doJSON(ctx, http.MethodPost, path, body, &payload); err != nil {
		return Release{}, err
	}
	return convertRelease(payload), nil
}

func (c *Client) publishRelease(ctx context.Context, owner, repo string, releaseID int64) (Release, error) {
	path := fmt.Sprintf("/repos/%s/%s/releases/%d", url.PathEscape(owner), url.PathEscape(repo), releaseID)
	var payload releasePayload
	if err := c.doJSON(ctx, http.MethodPatch, path, map[string]any{"draft": false}, &payload); err != nil {
		return Release{}, err
	}
	return convertRelease(payload), nil
}

func (c *Client) getRelease(ctx context.Context, owner, repo string, releaseID int64) (Release, error) {
	path := fmt.Sprintf("/repos/%s/%s/releases/%d", url.PathEscape(owner), url.PathEscape(repo), releaseID)
	var payload releasePayload
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return Release{}, err
	}
	return convertRelease(payload), nil
}

func (c *Client) UploadBackup(ctx context.Context, req UploadRequest, progress func(done, total int64)) (UploadResult, error) {
	if req.Owner == "" || req.Repo == "" {
		return UploadResult{}, fmt.Errorf("尚未选择 GitHub 私有仓库")
	}
	if req.ArchivePath == "" {
		return UploadResult{}, fmt.Errorf("归档路径为空")
	}
	stat, err := os.Stat(req.ArchivePath)
	if err != nil {
		return UploadResult{}, fmt.Errorf("无法读取待上传归档：%w", err)
	}
	if req.ArchiveSize == 0 {
		req.ArchiveSize = stat.Size()
	}
	if req.ManifestName == "" {
		req.ManifestName = manifest.ManifestPath(req.ArchiveName)
	}
	total := req.ArchiveSize + int64(len(req.ManifestBytes))

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			if err := sleepContext(ctx, 2*time.Second); err != nil {
				return UploadResult{}, err
			}
		}
		release, err := c.createRelease(ctx, req.Owner, req.Repo, req)
		if err != nil {
			lastErr = err
			if attempt == 0 && shouldRetryUpload(err) {
				continue
			}
			return UploadResult{}, err
		}
		cleanup := func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = c.DeleteBackup(cleanupCtx, req.Owner, req.Repo, release.ID)
		}
		archiveAsset, err := c.uploadFile(ctx, release, req.ArchiveName, req.ArchivePath, req.ArchiveSize, req.ArchiveSHA256, func(done int64) {
			if progress != nil {
				progress(done, total)
			}
		})
		if err != nil {
			cleanup()
			lastErr = err
			if !shouldRetryUpload(err) {
				return UploadResult{}, err
			}
			continue
		}
		manifestAsset, err := c.uploadBytes(ctx, release, req.ManifestName, req.ManifestBytes, func(done int64) {
			if progress != nil {
				progress(req.ArchiveSize+done, total)
			}
		})
		if err != nil {
			cleanup()
			lastErr = err
			if !shouldRetryUpload(err) {
				return UploadResult{}, err
			}
			continue
		}
		verified, err := c.verifyUploaded(ctx, req.Owner, req.Repo, release.ID, req.ArchiveName, req.ManifestName, req.ArchiveSize, int64(len(req.ManifestBytes)), req.ArchiveSHA256)
		if err != nil {
			cleanup()
			lastErr = err
			if !shouldRetryUpload(err) {
				return UploadResult{}, err
			}
			continue
		}
		published, err := c.publishRelease(ctx, req.Owner, req.Repo, release.ID)
		if err != nil {
			cleanup()
			if !shouldRetryUpload(err) {
				return UploadResult{}, err
			}
			lastErr = err
			continue
		}
		if progress != nil {
			progress(total, total)
		}
		return UploadResult{
			ReleaseID:       published.ID,
			Tag:             published.TagName,
			HTMLURL:         published.HTMLURL,
			ArchiveAssetID:  archiveAsset.ID,
			ManifestAssetID: manifestAsset.ID,
			ArchiveSize:     verified,
		}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("上传 GitHub 失败")
	}
	return UploadResult{}, lastErr
}

func shouldRetryUpload(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if apiErr.Status == 429 || apiErr.Status >= 500 {
			return true
		}
		if apiErr.Status == 403 && apiErr.RetryAfter > 0 {
			return true
		}
		return false
	}
	return true
}

func (c *Client) verifyUploaded(ctx context.Context, owner, repo string, releaseID int64, archiveName, manifestName string, archiveSize, manifestSize int64, archiveSHA string) (int64, error) {
	release, err := c.getRelease(ctx, owner, repo, releaseID)
	if err != nil {
		return 0, err
	}
	var archiveAsset *Asset
	var manifestAsset *Asset
	for index := range release.Assets {
		asset := &release.Assets[index]
		switch asset.Name {
		case archiveName:
			archiveAsset = asset
		case manifestName:
			manifestAsset = asset
		}
	}
	if archiveAsset == nil || manifestAsset == nil {
		return 0, fmt.Errorf("GitHub 未返回完整的上传资产")
	}
	if archiveAsset.Size != archiveSize {
		return 0, fmt.Errorf("云端归档大小校验失败：本地 %d 字节，云端 %d 字节", archiveSize, archiveAsset.Size)
	}
	if manifestAsset.Size != manifestSize {
		return 0, fmt.Errorf("云端 manifest 大小校验失败")
	}
	if archiveSHA != "" && archiveAsset.Digest != "" {
		digest := strings.TrimPrefix(archiveAsset.Digest, "sha256:")
		if !strings.EqualFold(digest, archiveSHA) {
			return 0, fmt.Errorf("云端归档 SHA-256 校验失败")
		}
	}
	return archiveAsset.Size, nil
}

func (c *Client) uploadFile(ctx context.Context, release Release, name, path string, size int64, expectedSHA string, report func(done int64)) (Asset, error) {
	file, err := os.Open(path)
	if err != nil {
		return Asset{}, err
	}
	defer file.Close()
	var hasher hash.Hash
	if expectedSHA != "" {
		hasher = sha256.New()
	}
	var reader io.Reader = file
	if hasher != nil {
		reader = io.TeeReader(file, hasher)
	}
	wrapped := &progressReader{reader: reader, total: size, report: func(done, total int64) {
		if report != nil {
			report(done)
		}
	}}
	asset, err := c.uploadStream(ctx, release, name, wrapped, size, report)
	if err != nil {
		return Asset{}, err
	}
	if hasher != nil {
		actual := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(actual, expectedSHA) {
			return Asset{}, fmt.Errorf("上传过程中源文件发生变化，已中止")
		}
	}
	return asset, nil
}

func (c *Client) uploadBytes(ctx context.Context, release Release, name string, data []byte, report func(done int64)) (Asset, error) {
	reader := bytes.NewReader(data)
	wrapped := &progressReader{reader: reader, total: int64(len(data)), report: func(done, total int64) {
		if report != nil {
			report(done)
		}
	}}
	asset, err := c.uploadStream(ctx, release, name, wrapped, int64(len(data)), report)
	if err != nil {
		return Asset{}, err
	}
	return asset, nil
}

func (c *Client) uploadStream(ctx context.Context, release Release, name string, reader io.Reader, size int64, report func(done int64)) (Asset, error) {
	uploadURL := strings.TrimSuffix(release.UploadURL, "{?name,label}")
	uploadURL = strings.TrimSuffix(uploadURL, "{?name}")
	uploadURL += "?name=" + url.QueryEscape(name)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, reader)
	if err != nil {
		return Asset{}, err
	}
	request.ContentLength = size
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("User-Agent", c.UserAgent)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return Asset{}, fmt.Errorf("上传到 GitHub 失败：%w", err)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
	_ = response.Body.Close()
	if readErr != nil {
		return Asset{}, readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Asset{}, c.translateError(response.StatusCode, response.Header, body)
	}
	var payload assetPayload
	if err := jsonUnmarshal(body, &payload); err != nil {
		return Asset{}, fmt.Errorf("解析上传结果失败：%w", err)
	}
	if payload.Size != size {
		return Asset{}, fmt.Errorf("GitHub 报告的资产大小不一致：期望 %d，实际 %d", size, payload.Size)
	}
	return Asset{
		ID:                 payload.ID,
		Name:               payload.Name,
		Size:               payload.Size,
		ContentType:        payload.ContentType,
		URL:                payload.URL,
		BrowserDownloadURL: payload.BrowserDownloadURL,
		Digest:             payload.Digest,
		State:              payload.State,
	}, nil
}

func (c *Client) ListBackups(ctx context.Context, owner, repo string) ([]manifest.Snapshot, error) {
	releases, err := c.listReleases(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	var snapshots []manifest.Snapshot
	for _, release := range releases {
		if release.Draft || !strings.HasPrefix(release.TagName, "backup-") {
			continue
		}
		value, err := manifest.DecodeReleaseBody(release.Body)
		if err != nil {
			if c.logger != nil {
				c.logger.Warn("github.release.skip", "跳过无法识别的 release", map[string]any{"tag": release.TagName, "error": err.Error()})
			}
			continue
		}
		var archiveAsset *Asset
		var manifestAsset *Asset
		manifestName := manifest.ManifestPath(value.Archive.Name)
		for index := range release.Assets {
			asset := &release.Assets[index]
			if asset.Name == value.Archive.Name {
				archiveAsset = asset
			}
			if asset.Name == manifestName {
				manifestAsset = asset
			}
		}
		if archiveAsset == nil {
			continue
		}
		snapshot := manifest.FromManifest(value, "", "")
		snapshot.Location = "cloud"
		snapshot.Source = "cloud"
		snapshot.Status = "ready"
		if archiveAsset.Size != value.Archive.Size {
			snapshot.Status = "size-mismatch"
		}
		snapshot.Size = archiveAsset.Size
		snapshot.CloudSize = archiveAsset.Size
		snapshot.CloudReleaseID = release.ID
		snapshot.CloudTag = release.TagName
		snapshot.CloudName = release.Name
		snapshot.CloudAssetID = archiveAsset.ID
		snapshot.CloudHTMLURL = release.HTMLURL
		if manifestAsset != nil {
			snapshot.CloudManifestID = manifestAsset.ID
		}
		snapshots = append(snapshots, snapshot)
	}
	manifest.Sort(snapshots)
	return snapshots, nil
}

func (c *Client) listReleases(ctx context.Context, owner, repo string) ([]Release, error) {
	var releases []Release
	for page := 1; page <= 3; page++ {
		path := fmt.Sprintf("/repos/%s/%s/releases?per_page=100&page=%d", url.PathEscape(owner), url.PathEscape(repo), page)
		var payload []releasePayload
		if err := c.doJSON(ctx, http.MethodGet, path, nil, &payload); err != nil {
			return nil, err
		}
		for _, item := range payload {
			releases = append(releases, convertRelease(item))
		}
		if len(payload) < 100 {
			break
		}
	}
	sort.Slice(releases, func(i, j int) bool {
		return releases[i].CreatedAt > releases[j].CreatedAt
	})
	return releases, nil
}

type DownloadResult struct {
	ArchivePath  string
	ManifestPath string
	Manifest     manifest.Manifest
	SHA256       string
	Size         int64
}

func (c *Client) DownloadBackup(ctx context.Context, owner, repo string, releaseID int64, destDir string, progress func(done, total int64)) (DownloadResult, error) {
	release, err := c.getRelease(ctx, owner, repo, releaseID)
	if err != nil {
		return DownloadResult{}, err
	}
	value, err := manifest.DecodeReleaseBody(release.Body)
	if err != nil {
		return DownloadResult{}, err
	}
	manifestName := manifest.ManifestPath(value.Archive.Name)
	var archiveAsset *Asset
	var manifestAsset *Asset
	for index := range release.Assets {
		asset := &release.Assets[index]
		if asset.Name == value.Archive.Name {
			archiveAsset = asset
		}
		if asset.Name == manifestName {
			manifestAsset = asset
		}
	}
	if archiveAsset == nil {
		return DownloadResult{}, fmt.Errorf("云端快照缺少归档资产")
	}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return DownloadResult{}, err
	}
	total := archiveAsset.Size
	if manifestAsset != nil {
		total += manifestAsset.Size
	}
	archivePath := filepath.Join(destDir, value.Archive.Name)
	sha, size, err := c.downloadAsset(ctx, *archiveAsset, archivePath, func(done int64) {
		if progress != nil {
			progress(done, total)
		}
	})
	if err != nil {
		return DownloadResult{}, err
	}
	if size != value.Archive.Size {
		return DownloadResult{}, fmt.Errorf("下载的归档大小与 manifest 不一致")
	}
	if !strings.EqualFold(sha, value.Archive.SHA256) {
		return DownloadResult{}, fmt.Errorf("下载的归档 SHA-256 校验失败")
	}
	manifestPath := manifest.ManifestPath(archivePath)
	if manifestAsset != nil {
		offset := size
		_, _, err := c.downloadAsset(ctx, *manifestAsset, manifestPath, func(done int64) {
			if progress != nil {
				progress(offset+done, total)
			}
		})
		if err != nil {
			return DownloadResult{}, err
		}
		if _, err := manifest.Read(manifestPath); err != nil {
			return DownloadResult{}, fmt.Errorf("下载的 manifest 校验失败：%w", err)
		}
	} else {
		raw, err := manifest.Marshal(value)
		if err != nil {
			return DownloadResult{}, err
		}
		if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
			return DownloadResult{}, err
		}
	}
	if progress != nil {
		progress(total, total)
	}
	return DownloadResult{
		ArchivePath:  archivePath,
		ManifestPath: manifestPath,
		Manifest:     value,
		SHA256:       sha,
		Size:         size,
	}, nil
}

func (c *Client) downloadAsset(ctx context.Context, asset Asset, destination string, report func(done int64)) (string, int64, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := sleepContext(ctx, time.Duration(attempt)*time.Second); err != nil {
				return "", 0, err
			}
		}
		sha, size, err := c.downloadAssetOnce(ctx, asset, destination, report)
		if err == nil {
			return sha, size, nil
		}
		lastErr = err
		if errors.Is(err, context.Canceled) {
			return "", 0, err
		}
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status != 429 && apiErr.Status < 500 {
			return "", 0, err
		}
	}
	return "", 0, lastErr
}

func (c *Client) downloadAssetOnce(ctx context.Context, asset Asset, destination string, report func(done int64)) (string, int64, error) {
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	rawURL := asset.URL
	if rawURL == "" {
		rawURL = asset.BrowserDownloadURL
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", c.UserAgent)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", 0, fmt.Errorf("下载云端归档失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1*1024*1024))
		return "", 0, c.translateError(response.StatusCode, response.Header, body)
	}
	hasher := sha256.New()
	total := asset.Size
	if response.ContentLength > 0 {
		total = response.ContentLength
	}
	wrapped := &progressReader{reader: response.Body, total: total, report: func(done, total int64) {
		if report != nil {
			report(done)
		}
	}}
	written, err := io.Copy(io.MultiWriter(file, hasher), wrapped)
	if err != nil {
		return "", 0, fmt.Errorf("写入下载文件失败：%w", err)
	}
	if asset.Size > 0 && written != asset.Size {
		return "", 0, fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", asset.Size, written)
	}
	return hex.EncodeToString(hasher.Sum(nil)), written, nil
}

func (c *Client) DeleteBackup(ctx context.Context, owner, repo string, releaseID int64) error {
	release, err := c.getRelease(ctx, owner, repo, releaseID)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Status == 404 {
			return nil
		}
		return err
	}
	path := fmt.Sprintf("/repos/%s/%s/releases/%d", url.PathEscape(owner), url.PathEscape(repo), release.ID)
	if err := c.doJSON(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return err
	}
	if !release.Draft {
		tagPath := fmt.Sprintf("/repos/%s/%s/git/refs/tags/%s", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(release.TagName))
		if err := c.doJSON(ctx, http.MethodDelete, tagPath, nil, nil); err != nil {
			var apiErr *APIError
			if !errors.As(err, &apiErr) || (apiErr.Status != 404 && apiErr.Status != 422) {
				if c.logger != nil {
					c.logger.Warn("github.tag.delete", "删除标签失败，release 已删除", map[string]any{"tag": release.TagName, "error": err.Error()})
				}
			}
		}
	}
	return nil
}

func (c *Client) ApplyRetention(ctx context.Context, owner, repo string, keep int) ([]string, error) {
	if keep < 1 {
		keep = 1
	}
	releases, err := c.listReleases(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	var backups []Release
	for _, release := range releases {
		if release.Draft || !strings.HasPrefix(release.TagName, "backup-") {
			continue
		}
		if _, err := manifest.DecodeReleaseBody(release.Body); err != nil {
			continue
		}
		backups = append(backups, release)
	}
	if len(backups) <= keep {
		return nil, nil
	}
	deleted := make([]string, 0)
	for index := keep; index < len(backups); index++ {
		release := backups[index]
		if err := c.DeleteBackup(ctx, owner, repo, release.ID); err != nil {
			return deleted, err
		}
		deleted = append(deleted, release.TagName)
	}
	return deleted, nil
}

func jsonUnmarshal(raw []byte, value any) error {
	return json.Unmarshal(raw, value)
}
