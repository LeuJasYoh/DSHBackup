package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreFootprintOnlyFlagsImplausibleRatios(t *testing.T) {
	cases := []struct {
		name         string
		uncompressed int64
		compressed   int64
		wantRisk     bool
	}{
		{"normal archive", 300 << 20, 100 << 20, false},
		{"small but compressible", 200 << 20, 1 << 20, false},
		{"large and implausible", 300 << 20, 1 << 20, true},
		{"exactly at the ratio limit", 200 << 20, 1 << 20, false},
		{"zero compressed size", 300 << 20, 0, false},
		{"zero uncompressed size", 0, 1 << 20, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := checkRestoreFootprint(test.uncompressed, test.compressed)
			if test.wantRisk && err == nil {
				t.Fatal("expected a restore risk error")
			}
			if !test.wantRisk && err != nil {
				t.Fatalf("unexpected risk error: %v", err)
			}
			if err != nil && !strings.HasPrefix(err.Error(), RestoreRiskPrefix) {
				t.Fatalf("risk error must carry the UI prefix: %q", err.Error())
			}
		})
	}
}

func TestFindRestoreCandidatesOnlyMatchesCandidateNames(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "profiles", "default")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(nested, ".state.json.dshbak-1234-abcd.tmp")
	for _, path := range []string{
		candidate,
		filepath.Join(root, "plain.tmp"),
		filepath.Join(root, "notes.txt"),
		filepath.Join(root, ".dshbak-readable.json"),
	} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	found := FindRestoreCandidates(root, 10)
	if len(found) != 1 || found[0] != candidate {
		t.Fatalf("unexpected candidates: %v", found)
	}
	if limited := FindRestoreCandidates(root, 0); limited != nil {
		t.Fatalf("zero limit should return nothing: %v", limited)
	}
	if missing := FindRestoreCandidates(filepath.Join(root, "does-not-exist"), 10); len(missing) != 0 {
		t.Fatalf("missing root should return nothing: %v", missing)
	}
}

func TestRestoreCandidatesStopsAtLimit(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	for index := 0; index < restoreCandidateLimit+5; index++ {
		name := filepath.Join(first, strings.Repeat("a", index+1)+".dshbak-1-x.tmp")
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < 3; index++ {
		name := filepath.Join(second, strings.Repeat("b", index+1)+".dshbak-1-x.tmp")
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	found := RestoreCandidates([]string{first, second})
	if len(found) != restoreCandidateLimit {
		t.Fatalf("expected the scan to stop at %d entries, got %d", restoreCandidateLimit, len(found))
	}
}
