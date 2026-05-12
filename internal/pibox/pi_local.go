package pibox

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type piHostSettings struct {
	Packages []string `json:"packages"`
}

func syncLocalPiCustomizations(ctx context.Context, ssh *SSH) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	piDir := filepath.Join(home, ".pi", "agent")
	if _, err := os.Stat(piDir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := syncLocalPiExtensions(ctx, ssh, filepath.Join(piDir, "extensions")); err != nil {
		return err
	}
	packages, err := readLocalPiPackages(filepath.Join(piDir, "settings.json"))
	if err != nil {
		return err
	}
	if len(packages) > 0 {
		if err := writePiPackagesInVM(ctx, ssh, packages); err != nil {
			return err
		}
	}
	return nil
}

func syncLocalPiExtensions(ctx context.Context, ssh *SSH, extensionsDir string) error {
	info, err := os.Stat(extensionsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	archive, count, err := tarDirectory(extensionsDir)
	if err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	script := `
set -eu
mkdir -p /root/.pi/agent/extensions
rm -rf /root/.pi/agent/extensions/*
tar -xf - -C /root/.pi/agent/extensions
`
	return ssh.RunWithInput(ctx, "", archive, script)
}

func tarDirectory(root string) ([]byte, int, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	count := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = rel
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			if _, err := io.Copy(tw, file); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	if err != nil {
		_ = tw.Close()
		return nil, 0, err
	}
	if err := tw.Close(); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), count, nil
}

func readLocalPiPackages(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var settings piHostSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("lettura packages Pi locali: %w", err)
	}
	return settings.Packages, nil
}

func writePiPackagesInVM(ctx context.Context, ssh *SSH, packages []string) error {
	data, err := json.Marshal(struct {
		Packages []string `json:"packages"`
	}{Packages: packages})
	if err != nil {
		return err
	}
	script := `
set -eu
mkdir -p /root/.pi/agent
tmp="$(mktemp)"
cat > "$tmp"
if [ -f /root/.pi/agent/settings.json ]; then
  node - "$tmp" /root/.pi/agent/settings.json <<'NODE'
const fs = require("fs");
const incoming = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
let current = {};
try { current = JSON.parse(fs.readFileSync(process.argv[3], "utf8")); } catch {}
current.packages = incoming.packages || [];
fs.writeFileSync(process.argv[3], JSON.stringify(current, null, 2) + "\n");
NODE
else
  cp "$tmp" /root/.pi/agent/settings.json
fi
rm -f "$tmp"
`
	return ssh.RunWithInput(ctx, "", data, script)
}
