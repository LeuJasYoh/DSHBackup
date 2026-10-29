package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    bool
		wantErr bool
	}{
		{current: "1.1.0", latest: "1.2.0", want: true},
		{current: "1.1.0", latest: "v1.1.0", want: false},
		{current: "1.1.0", latest: "1.1.1", want: true},
		{current: "1.9.0", latest: "1.10.0", want: true},
		{current: "2.0.0", latest: "1.99.99", want: false},
		{current: "1.1.0", latest: "1.1.0-beta.1", want: false},
		{current: "1.1.0", latest: "1.1", want: false},
		{current: "1.1.0", latest: "", wantErr: true},
	}
	for _, test := range tests {
		got, err := isNewerVersion(test.current, test.latest)
		if test.wantErr {
			if err == nil {
				t.Fatalf("%s -> %s should fail", test.current, test.latest)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s -> %s: %v", test.current, test.latest, err)
		}
		if got != test.want {
			t.Fatalf("%s -> %s: got %v, want %v", test.current, test.latest, got, test.want)
		}
	}
}

func TestSafeReleaseURL(t *testing.T) {
	if got := safeReleaseURL("https://github.com/LeuJasYoh/DSHBackup/releases/tag/v1.2.0"); got != "https://github.com/LeuJasYoh/DSHBackup/releases/tag/v1.2.0" {
		t.Fatalf("unexpected safe URL: %s", got)
	}
	for _, raw := range []string{"https://evil.example/releases", "javascript:alert(1)", "https://github.com/other/repo/releases", ""} {
		if got := safeReleaseURL(raw); got != RepositoryReleases {
			t.Fatalf("%q should fall back to the repository releases page, got %s", raw, got)
		}
	}
}

func TestFetchLatestRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("User-Agent") == "" {
			t.Error("missing User-Agent")
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"tag_name":"v1.2.0","html_url":"https://github.com/LeuJasYoh/DSHBackup/releases/tag/v1.2.0","published_at":"2026-10-30T10:00:00Z"}`))
	}))
	defer server.Close()
	release, err := fetchLatestRelease(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if release.TagName != "v1.2.0" || !strings.Contains(release.HTMLURL, "/releases/tag/v1.2.0") {
		t.Fatalf("unexpected release: %+v", release)
	}
	newer, err := isNewerVersion("1.1.0", release.TagName)
	if err != nil || !newer {
		t.Fatalf("expected newer release, got %v, %v", newer, err)
	}
}

func TestFetchLatestReleaseErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.NotFound(response, request)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := fetchLatestRelease(ctx, server.Client(), server.URL); err == nil {
		t.Fatal("expected an error for a missing release")
	}
}
