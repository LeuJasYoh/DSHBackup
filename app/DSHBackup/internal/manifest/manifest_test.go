package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"DSHBackup/internal/scope"
)

func testManifest() Manifest {
	return Manifest{
		SchemaVersion: SchemaVersion,
		ID:            "20261026-120000",
		Domain:        string(scope.DomainCore),
		CreatedAt:     time.Now().Format(time.RFC3339),
		Kind:          KindManual,
		AppVersion:    "test",
		Scope:         []string{"dsh", "desktop-state"},
		Files:         FileStats{Files: 12, Bytes: 1024},
		Archive: ArchiveInfo{
			Name:   DomainArchiveName("20261026-120000", string(scope.DomainCore)),
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

func writeManifestFile(t *testing.T, dir string, value Manifest) string {
	t.Helper()
	path := filepath.Join(dir, filepath.Base(ManifestPath(filepath.Join(dir, value.Archive.Name))))
	if err := Write(path, value); err != nil {
		t.Fatalf("写入清单失败：%v", err)
	}
	return path
}

func TestScanAggregatesDomainParts(t *testing.T) {
	dir := t.TempDir()
	created := time.Now().Format(time.RFC3339)
	domains := []scope.Domain{scope.DomainSessions, scope.DomainCore} // 故意乱序写入
	for _, name := range domains {
		archive := filepath.Join(dir, DomainArchiveName("20261101-090000", string(name)))
		if err := os.WriteFile(archive, []byte("payload"), 0o600); err != nil {
			t.Fatal(err)
		}
		value := testManifest()
		value.ID = "20261101-090000"
		value.CreatedAt = created
		value.Domain = string(name)
		value.Archive.Name = filepath.Base(archive)
		value.Archive.Size = int64(len("payload"))
		value.Encryption = nil
		writeManifestFile(t, dir, value)
	}
	snapshots, warnings, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) > 0 {
		t.Fatalf("不该有告警：%v", warnings)
	}
	if len(snapshots) != 1 {
		t.Fatalf("两份域清单应聚合成一个快照，实际 %d 项", len(snapshots))
	}
	snapshot := snapshots[0]
	if snapshot.Status != "ready" {
		t.Fatalf("全部就绪时状态应为 ready，实际 %s", snapshot.Status)
	}
	if len(snapshot.Parts) != 2 || snapshot.Parts[0].Domain != string(scope.DomainCore) || snapshot.Parts[1].Domain != string(scope.DomainSessions) {
		t.Fatalf("域顺序应为 core→sessions：%+v", snapshot.Parts)
	}
	if snapshot.Size != 2*int64(len("payload")) {
		t.Fatalf("快照大小应为各份之和，实际 %d", snapshot.Size)
	}
	if snapshot.SHA256 != snapshot.Parts[0].SHA256 || snapshot.LocalPath != snapshot.Parts[0].LocalPath {
		t.Fatal("兼容字段应指向第一份就绪清单")
	}
}

func TestScanKeepsLegacySingleArchive(t *testing.T) {
	dir := t.TempDir()
	value := testManifest()
	value.SchemaVersion = 1
	value.Domain = ""
	value.Archive.Name = ArchiveName(value.ID)
	archive := filepath.Join(dir, value.Archive.Name)
	if err := os.WriteFile(archive, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	value.Archive.Size = int64(len("old"))
	writeManifestFile(t, dir, value)
	snapshots, _, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("旧清单应仍能列出，实际 %d 项", len(snapshots))
	}
	if snapshots[0].Status != "ready" || len(snapshots[0].Parts) != 0 {
		t.Fatalf("v1 快照不该出现域列表：%+v", snapshots[0])
	}
	if !strings.HasSuffix(snapshots[0].LocalPath, "dsh-backup-20261026-120000.7z") {
		t.Fatalf("旧归档名不应被改写：%s", snapshots[0].LocalPath)
	}
}

func TestScanMarksPartialWhenOneDomainMissing(t *testing.T) {
	dir := t.TempDir()
	value := testManifest()
	value.ID = "20261101-093000"
	value.Domain = string(scope.DomainCore)
	value.Archive.Name = DomainArchiveName(value.ID, value.Domain)
	if err := os.WriteFile(filepath.Join(dir, value.Archive.Name), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	value.Archive.Size = 2
	writeManifestFile(t, dir, value)

	ghost := testManifest()
	ghost.ID = value.ID
	ghost.CreatedAt = value.CreatedAt
	ghost.Domain = string(scope.DomainPlugins)
	ghost.Archive.Name = DomainArchiveName(ghost.ID, ghost.Domain)
	writeManifestFile(t, dir, ghost)

	snapshots, _, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 || snapshots[0].Status != "partial" {
		t.Fatalf("缺一份域时应标为 partial：%+v", snapshots)
	}
	if snapshots[0].Size != 2 {
		t.Fatalf("缺失的域不该计大小，实际 %d", snapshots[0].Size)
	}
}

func TestValidateRejectsDomainNameMismatch(t *testing.T) {
	value := testManifest()
	value.Domain = string(scope.DomainRuntime)
	if err := Validate(value); err == nil {
		t.Fatal("归档名与域不一致应被拒绝")
	}
	value = testManifest()
	value.Domain = "made-up"
	value.Archive.Name = DomainArchiveName(value.ID, value.Domain)
	if err := Validate(value); err == nil {
		t.Fatal("未知域应被拒绝")
	}
}
