package pix

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMakeRepoID(t *testing.T) {
	id, err := makeRepoID("/tmp/My App")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "my-app-") {
		t.Fatalf("repo id = %q", id)
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote("a'b")
	want := `'a'"'"'b'`
	if got != want {
		t.Fatalf("quote = %q, want %q", got, want)
	}
}

func TestGitWorktreeTarIncludesPixContextMatches(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	writeFile(t, root, ".gitignore", "secrets/\n*.env\n.pixcontext\n")
	writeFile(t, root, ".pixcontext", "secrets/\n*.env\n!skip.env\n")
	writeFile(t, root, "visible.txt", "visible")
	writeFile(t, root, "secrets/context.txt", "context")
	writeFile(t, root, ".env", "included")
	writeFile(t, root, "skip.env", "not included")

	data, err := gitWorktreeTar(context.Background(), osRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	paths := tarPaths(t, data)

	for _, want := range []string{".gitignore", ".pixcontext", "visible.txt", "secrets/context.txt", ".env"} {
		if !paths[want] {
			t.Fatalf("tar missing %q; paths = %#v", want, paths)
		}
	}
	if paths["skip.env"] {
		t.Fatalf("tar unexpectedly contains negated .pixcontext path skip.env; paths = %#v", paths)
	}
}

func TestGitWorktreeTarDeduplicatesPixContextMatches(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	writeFile(t, root, ".pixcontext", "visible.txt\n")
	writeFile(t, root, "visible.txt", "visible")

	data, err := gitWorktreeTar(context.Background(), osRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == "visible.txt" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("visible.txt appears %d times, want 1", count)
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func tarPaths(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	paths := map[string]bool{}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		paths[hdr.Name] = true
	}
	return paths
}
