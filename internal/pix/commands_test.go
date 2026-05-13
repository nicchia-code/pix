package pix

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncFromVMRequiresCleanHostWorktree(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	gitDir := filepath.Join(root, ".git")
	if err := writeRepoConfig(gitDir, NewRepoConfig("repo-test")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dirty.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}

	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldwd)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	app := NewApp(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	err = app.syncFromVM(context.Background())
	if err == nil {
		t.Fatal("expected dirty worktree error")
	}
	if !strings.Contains(err.Error(), "modifiche non committate") {
		t.Fatalf("error = %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	r := osRunner{}
	if _, _, err := r.Run(context.Background(), dir, nil, "git", args...); err != nil {
		t.Fatal(err)
	}
}
