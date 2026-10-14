package jsonc

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEncodeFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes do not control access on Windows")
	}
	dir := t.TempDir()
	fresh := filepath.Join(dir, "new.jsonc")
	if err := EncodeFile(fresh, map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if got := filePerm(t, fresh); got != 0o600 {
		t.Errorf("new file mode = %v, want 0600", got)
	}
	existing := filepath.Join(dir, "old.jsonc")
	if err := os.WriteFile(existing, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(existing, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EncodeFile(existing, map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if got := filePerm(t, existing); got != 0o644 {
		t.Errorf("existing file mode = %v, want 0644 kept", got)
	}
}

func filePerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}
