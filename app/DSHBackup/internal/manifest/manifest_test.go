package manifest

import (
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

func TestReleaseBodyRoundTrip(t *testing.T) {
	value := testManifest()
	body, err := EncodeReleaseBody(value)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeReleaseBody(body)
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

func TestMerge(t *testing.T) {
	local := []Snapshot{{ID: "a", CreatedAt: "2026-10-26T12:00:00+08:00", Location: "local", LocalPath: "x"}}
	cloud := []Snapshot{{ID: "a", CreatedAt: "2026-10-26T12:00:00+08:00", Location: "cloud", CloudReleaseID: 5}}
	merged := MergeLocalAndCloud(local, cloud)
	if len(merged) != 1 || merged[0].Location != "both" || merged[0].CloudReleaseID != 5 || merged[0].LocalPath != "x" {
		t.Fatalf("unexpected merge: %+v", merged)
	}
}
