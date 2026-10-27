package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"DSHBackup/internal/manifest"
)

type fakeGitHub struct {
	mu       sync.Mutex
	server   *httptest.Server
	releases map[int64]*fakeRelease
	assets   map[int64][]byte
	nextID   int64
}

type fakeRelease struct {
	id      int64
	tag     string
	name    string
	body    string
	draft   bool
	created string
	htmlURL string
	assets  []fakeAsset
}

type fakeAsset struct {
	id     int64
	name   string
	size   int64
	digest string
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	fake := &fakeGitHub{
		releases: map[int64]*fakeRelease{},
		assets:   map[int64][]byte{},
		nextID:   100,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/releases", func(writer http.ResponseWriter, request *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		switch request.Method {
		case http.MethodPost:
			var payload map[string]any
			_ = json.NewDecoder(request.Body).Decode(&payload)
			id := fake.nextID
			fake.nextID++
			release := &fakeRelease{
				id:      id,
				tag:     fmt.Sprint(payload["tag_name"]),
				name:    fmt.Sprint(payload["name"]),
				body:    fmt.Sprint(payload["body"]),
				draft:   true,
				created: time.Now().UTC().Format(time.RFC3339),
				htmlURL: fmt.Sprintf("https://example.invalid/releases/%d", id),
			}
			fake.releases[id] = release
			writeJSON(writer, http.StatusCreated, fake.releasePayload(release))
		case http.MethodGet:
			all := make([]map[string]any, 0, len(fake.releases))
			for _, release := range fake.releases {
				if release.draft {
					continue
				}
				all = append(all, fake.releasePayload(release))
			}
			writeJSON(writer, http.StatusOK, all)
		default:
			writer.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/repos/o/r/releases/", func(writer http.ResponseWriter, request *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		parts := strings.Split(strings.TrimPrefix(request.URL.Path, "/repos/o/r/releases/"), "/")
		idStr := parts[0]
		var id int64
		fmt.Sscan(idStr, &id)
		release := fake.releases[id]
		if release == nil {
			writeJSON(writer, http.StatusNotFound, map[string]any{"message": "Not Found"})
			return
		}
		if len(parts) == 1 {
			switch request.Method {
			case http.MethodGet:
				writeJSON(writer, http.StatusOK, fake.releasePayload(release))
			case http.MethodPatch:
				var payload map[string]any
				_ = json.NewDecoder(request.Body).Decode(&payload)
				if draft, ok := payload["draft"].(bool); ok {
					release.draft = draft
				}
				writeJSON(writer, http.StatusOK, fake.releasePayload(release))
			case http.MethodDelete:
				delete(fake.releases, id)
				writer.WriteHeader(http.StatusNoContent)
			default:
				writer.WriteHeader(http.StatusMethodNotAllowed)
			}
			return
		}
		if len(parts) >= 2 && parts[1] == "assets" && request.Method == http.MethodPost {
			data, _ := io.ReadAll(request.Body)
			name := request.URL.Query().Get("name")
			assetID := fake.nextID
			fake.nextID++
			hasher := sha256.Sum256(data)
			fake.assets[assetID] = data
			release.assets = append(release.assets, fakeAsset{
				id:     assetID,
				name:   name,
				size:   int64(len(data)),
				digest: hex.EncodeToString(hasher[:]),
			})
			writeJSON(writer, http.StatusCreated, fake.assetPayload(release.assets[len(release.assets)-1]))
			return
		}
		writeJSON(writer, http.StatusNotFound, map[string]any{"message": "Not Found"})
	})
	mux.HandleFunc("/assets/", func(writer http.ResponseWriter, request *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		var id int64
		fmt.Sscan(strings.TrimPrefix(request.URL.Path, "/assets/"), &id)
		data, ok := fake.assets[id]
		if !ok {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = writer.Write(data)
	})
	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeGitHub) releasePayload(release *fakeRelease) map[string]any {
	assets := make([]map[string]any, 0, len(release.assets))
	for _, asset := range release.assets {
		assets = append(assets, f.assetPayload(asset))
	}
	return map[string]any{
		"id":           release.id,
		"tag_name":     release.tag,
		"name":         release.name,
		"body":         release.body,
		"html_url":     release.htmlURL,
		"created_at":   release.created,
		"published_at": release.created,
		"draft":        release.draft,
		"prerelease":   false,
		"upload_url":   fmt.Sprintf("%s/repos/o/r/releases/%d/assets{?name,label}", f.server.URL, release.id),
		"assets":       assets,
	}
}

func (f *fakeGitHub) assetPayload(asset fakeAsset) map[string]any {
	return map[string]any{
		"id":                   asset.id,
		"name":                 asset.name,
		"size":                 asset.size,
		"content_type":         "application/octet-stream",
		"url":                  fmt.Sprintf("%s/assets/%d", f.server.URL, asset.id),
		"browser_download_url": fmt.Sprintf("%s/assets/%d", f.server.URL, asset.id),
		"digest":               "sha256:" + asset.digest,
		"state":                "uploaded",
	}
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func newTestClient(t *testing.T, fake *fakeGitHub) *Client {
	t.Helper()
	client, err := New("test-token", ProxyConfig{Mode: "off"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	client.APIBase = fake.server.URL
	client.UploadBase = fake.server.URL
	return client
}

func TestUploadListDownloadBackup(t *testing.T) {
	fake := newFakeGitHub(t)
	client := newTestClient(t, fake)
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "dsh-backup-20261026-120000.7z")
	archiveData := []byte(strings.Repeat("backup-data-", 1000))
	if err := os.WriteFile(archivePath, archiveData, 0o600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(archiveData)
	value := manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		ID:            "20261026-120000",
		CreatedAt:     time.Now().Format(time.RFC3339),
		Kind:          manifest.KindManual,
		AppVersion:    "test",
		Scope:         []string{"dsh"},
		Files:         manifest.FileStats{Files: 1, Bytes: int64(len(archiveData))},
		Archive: manifest.ArchiveInfo{
			Name:   "dsh-backup-20261026-120000.7z",
			Size:   int64(len(archiveData)),
			SHA256: hex.EncodeToString(hash[:]),
		},
		Encryption: &manifest.EncryptionInfo{
			Enabled:         true,
			Algorithm:       "test",
			KeyID:           "key",
			WrappedPassword: "YWJj",
		},
		Compressor: manifest.CompressorInfo{Name: "7-Zip", Version: "test", Args: []string{"-mx=5"}},
	}
	body, err := manifest.EncodeReleaseBody(value)
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := manifest.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	progressValues := []int64{}
	result, err := client.UploadBackup(context.Background(), UploadRequest{
		Owner:         "o",
		Repo:          "r",
		Tag:           "backup-" + value.ID,
		Name:          "Test Backup",
		Body:          body,
		ArchivePath:   archivePath,
		ArchiveName:   value.Archive.Name,
		ArchiveSize:   value.Archive.Size,
		ArchiveSHA256: value.Archive.SHA256,
		ManifestName:  manifest.ManifestPath(value.Archive.Name),
		ManifestBytes: manifestBytes,
	}, func(done, total int64) {
		progressValues = append(progressValues, done)
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ReleaseID == 0 || result.Tag != "backup-"+value.ID {
		t.Fatalf("unexpected upload result: %+v", result)
	}
	if len(progressValues) == 0 {
		t.Fatal("expected upload progress")
	}

	snapshots, err := client.ListBackups(context.Background(), "o", "r")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 || snapshots[0].ID != value.ID || !snapshots[0].Encrypted {
		t.Fatalf("unexpected snapshots: %+v", snapshots)
	}
	if snapshots[0].CloudReleaseID != result.ReleaseID {
		t.Fatal("release id mismatch")
	}

	downloadDir := t.TempDir()
	download, err := client.DownloadBackup(context.Background(), "o", "r", result.ReleaseID, downloadDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	downloaded, err := os.ReadFile(download.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(downloaded) != string(archiveData) {
		t.Fatal("downloaded archive mismatch")
	}
	if download.Manifest.ID != value.ID {
		t.Fatal("downloaded manifest mismatch")
	}
}

func TestDeviceFlow(t *testing.T) {
	var pollCount int
	mux := http.NewServeMux()
	mux.HandleFunc("/device", func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]any{
			"device_code":      "device-1",
			"user_code":        "ABCD-EFGH",
			"verification_uri": "https://github.com/login/device",
			"expires_in":       900,
			"interval":         5,
		})
	})
	mux.HandleFunc("/token", func(writer http.ResponseWriter, request *http.Request) {
		pollCount++
		if pollCount < 2 {
			writeJSON(writer, http.StatusOK, map[string]any{"error": "authorization_pending"})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"access_token": "token-1", "token_type": "bearer", "scope": "repo"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client, err := New("", ProxyConfig{Mode: "off"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	client.DeviceURL = server.URL + "/device"
	client.TokenURL = server.URL + "/token"
	info, err := client.StartDeviceFlow(context.Background(), "client-id")
	if err != nil {
		t.Fatal(err)
	}
	if info.UserCode != "ABCD-EFGH" || info.Interval != 5 {
		t.Fatalf("unexpected flow: %+v", info)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	token, err := client.PollDeviceFlow(ctx, "client-id", info, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "token-1" {
		t.Fatalf("unexpected token: %s", token)
	}
}
