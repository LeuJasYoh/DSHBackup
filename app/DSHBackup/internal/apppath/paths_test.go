package apppath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanTempOnlyRemovesMarkedTaskDirectories(t *testing.T) {
	root := t.TempDir()
	paths := Paths{TempDir: filepath.Join(root, "temp")}
	if err := os.MkdirAll(paths.TempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	taskDir, err := paths.TempTaskDir("backup-test")
	if err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(paths.TempDir, "user-file")
	if err := os.MkdirAll(unknown, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unknown, "keep.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := paths.CleanTemp(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
		t.Fatalf("marked task directory was not cleaned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(unknown, "keep.txt")); err != nil {
		t.Fatalf("unknown temp directory was removed: %v", err)
	}
}
