package pibox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureStateTreeUsesPiboxHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PIBOX_HOME", root)

	got, err := ensureStateTree()
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("state root = %q, want %q", got, root)
	}
	for _, path := range []string{
		filepath.Join(root, "images"),
		filepath.Join(root, "vm", "default", "ssh"),
		filepath.Join(root, "logs"),
	} {
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			t.Fatalf("expected directory %s, stat err = %v", path, err)
		}
	}
}

func TestRepoConfigRoundTrip(t *testing.T) {
	gitDir := t.TempDir()
	cfg := NewRepoConfig("app-a1b2c3")

	if err := writeRepoConfig(gitDir, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := readRepoConfig(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != cfg {
		t.Fatalf("config = %#v, want %#v", got, cfg)
	}
}
