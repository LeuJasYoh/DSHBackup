package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testManifest() Manifest {
	return Manifest{
		SchemaVersion: SchemaVersion,
		ID:            "20261026-120000",
		CreatedAt:     time.Now().Format(time.RFC3339),
		Kind:          KindManual,
		AppVersion:    "test",
		Scope:         []string{"dsh", "desktop-state"},
		Files:         FileStats{Files: 12, Bytes: 1024},
		Archive: ArchiveInfo{
			Name:   ArchiveName("20261026-120000"),
			Size:   512,
			SHA256: strings.Repeat("a", 64),
		},
		Encryption: &EncryptionInfo{
			Enabled:         true,
			Algorithm:       "test",
			KeyID:           "key",
			WrappedPassword: "YWJj",
		},
		Compressor: CompressorInfo{Name: "7-Zip", Version: "test", Args: []string{"-mx=5"}},
	}
}

func TestCompressionLevelNames(t *testing.T) {
	cases := map[string]string{
		"-mx=0": "store",
		"-mx=1": "fast",
		"-mx=3": "quick",
		"-mx=5": "balanced",
		"-mx=7": "high",
		"-mx=9": "max",
	}
	for arg, want := range cases {
		if got := compressionLevel([]string{arg}); got != want {
			t.Fatalf("%s mapped to %q, want %q", arg, got, want)
		}
	}
}

func TestLocalManifestRoundTrip(t *testing.T) {
	value := testManifest()
	path := filepath.Join(t.TempDir(), "backup.manifest.json")
	if err := Write(path, value); err != nil {
		t.Fatal(err)
	}
	decoded, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID != value.ID || decoded.Archive.SHA256 != value.Archive.SHA256 {
		t.Fatalf("unexpected decoded manifest: %+v", decoded)
	}
	if decoded.Encryption == nil || !decoded.Encryption.Enabled {
		t.Fatal("encryption info lost")
	}
}

func TestValidateRejectsUnsafeArchiveName(t *testing.T) {
	value := testManifest()
	value.Archive.Name = "../evil.7z"
	if err := Validate(value); err == nil {
		t.Fatal("unsafe archive name should be rejected")
	}
	value = testManifest()
	value.SchemaVersion = 99
	if err := Validate(value); err == nil {
		t.Fatal("unsupported schema should be rejected")
	}
}

func TestScanMergesManifestAliasesAndRejectsConflicts(t *testing.T) {
	dir := t.TempDir()
	value := testManifest()
	archive := filepath.Join(dir, value.Archive.Name)
	if err := os.WriteFile(archive, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	value.Archive.Size = int64(len("archive"))
	value.CaptureStatus = "complete"
	if err := Write(ManifestPath(archive), value); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(dir, "copy.manifest.json")
	if err := Write(copyPath, value); err != nil {
		t.Fatal(err)
	}
	list, warnings, err := Scan(dir)
	if err != nil || len(list) != 1 || len(list[0].ManifestAliases) != 1 || len(warnings) != 0 {
		t.Fatalf("identical manifest duplicate was not merged: %+v %v %v", list, warnings, err)
	}
	conflict := value
	conflict.Archive.SHA256 = strings.Repeat("b", 64)
	if err := Write(filepath.Join(dir, "conflict.manifest.json"), conflict); err != nil {
		t.Fatal(err)
	}
	list, warnings, err = Scan(dir)
	if err != nil || len(list) != 1 || list[0].Status != "conflict" || len(warnings) == 0 {
		t.Fatalf("conflicting manifest was not marked: %+v %v %v", list, warnings, err)
	}
}

func TestSortUsesInstantsAcrossTimeZones(t *testing.T) {
	list := []Snapshot{
		{ID: "older", CreatedAt: "2026-10-30T12:00:00+08:00"},
		{ID: "newer", CreatedAt: "2026-10-30T08:00:00+00:00"},
	}
	Sort(list)
	if list[0].ID != "newer" {
		t.Fatalf("retention order used string time: %+v", list)
	}
}

func TestScanRetainsMissingAndMismatchedArchives(t *testing.T) {
	dir := t.TempDir()
	value := testManifest()
	archive := filepath.Join(dir, value.Archive.Name)
	if err := Write(ManifestPath(archive), value); err != nil {
		t.Fatal(err)
	}
	list, _, err := Scan(dir)
	if err != nil || len(list) != 1 || list[0].Status != "missing" {
		t.Fatalf("missing archive: %+v %v", list, err)
	}
	if err := os.WriteFile(archive, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	list, _, err = Scan(dir)
	if err != nil || len(list) != 1 || list[0].Status != "size-mismatch" {
		t.Fatalf("mismatched archive: %+v %v", list, err)
	}
}
