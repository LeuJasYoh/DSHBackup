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
