package pix

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureStateTreeUsesPixHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PIX_HOME", root)

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

func TestLoadOrInitRepoConfigCreatesMissingConfig(t *testing.T) {
	root := t.TempDir()
	gitDirPath := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDirPath, 0o700); err != nil {
		t.Fatal(err)
	}

	cfg, initialized, err := loadOrInitRepoConfig(context.Background(), gitDirRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	if !initialized {
		t.Fatal("initialized = false, want true")
	}
	if cfg.RepoID == "" {
		t.Fatal("repo id is empty")
	}
	if _, err := os.Stat(repoConfigPath(gitDirPath)); err != nil {
		t.Fatalf("expected repo config to be written: %v", err)
	}

	again, initialized, err := loadOrInitRepoConfig(context.Background(), gitDirRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	if initialized {
		t.Fatal("initialized = true on second load, want false")
	}
	if again != cfg {
		t.Fatalf("config = %#v, want %#v", again, cfg)
	}
}

type gitDirRunner struct{}

func (gitDirRunner) Run(ctx context.Context, dir string, input []byte, name string, args ...string) ([]byte, []byte, error) {
	return []byte(".git\n"), nil, nil
}
