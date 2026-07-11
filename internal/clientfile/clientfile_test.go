package clientfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndRemove(t *testing.T) {
	dir := t.TempDir()
	content := "[Interface]\nPrivateKey = test\n"
	if err := Save(dir, "wg0", "alice", content); err != nil {
		t.Fatal(err)
	}

	got, err := Load([]string{dir}, "wg0", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got != content {
		t.Fatalf("unexpected content: %q", got)
	}

	Remove([]string{dir}, "wg0", "alice")
	if _, err := os.Stat(filepath.Join(dir, "wg0-client-alice.conf")); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, err=%v", err)
	}
}
