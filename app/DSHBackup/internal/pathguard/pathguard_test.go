//go:build windows

package pathguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestContainsRejectsJunctionAlias(t *testing.T) {
	base := t.TempDir()
	inside := filepath.Join(base, "inside")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", alias, inside).CombinedOutput()
	if err != nil {
		t.Fatalf("junction fixture: %v %s", err, output)
	}
	resolved, err := ResolveTarget(alias)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("alias=%s resolved=%s inside=%s", alias, resolved, inside)
	contained, err := Contains(filepath.Join(alias, "child"), inside)
	if err != nil {
		t.Fatal(err)
	}
	if !contained {
		t.Fatalf("junction child was not recognized inside its target; resolved=%s", resolved)
	}
	if _, err := Overlaps(outside, alias); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityDistinguishesDirectories(t *testing.T) {
	base := t.TempDir()
	one := filepath.Join(base, "one")
	two := filepath.Join(base, "two")
	for _, dir := range []string{one, two} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	first, err := Identity(one)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Identity(two)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("different directories shared identity %s", first)
	}
}
