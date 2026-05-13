package pix

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestTarDirectoryIncludesExtensionFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "markdown-renderer.ts"), []byte("export default {}"), 0o600); err != nil {
		t.Fatal(err)
	}

	data, count, err := tarDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	tr := tar.NewReader(bytes.NewReader(data))
	header, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if header.Name != "markdown-renderer.ts" {
		t.Fatalf("tar entry = %q", header.Name)
	}
	content, err := io.ReadAll(tr)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "export default {}" {
		t.Fatalf("content = %q", content)
	}
}

func TestReadLocalPiPackages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"packages":["npm:pi-web-access"],"defaultModel":"gpt-5.5"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	packages, err := readLocalPiPackages(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || packages[0] != "npm:pi-web-access" {
		t.Fatalf("packages = %#v", packages)
	}
}
