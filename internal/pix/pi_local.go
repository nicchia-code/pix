package pix

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
		return writePixExtension(ctx, ssh)
	} else if err != nil {
		return err
	}
	if err := syncLocalPiExtensions(ctx, ssh, filepath.Join(piDir, "extensions")); err != nil {
		return err
	}
	if err := writePixExtension(ctx, ssh); err != nil {
		return err
	}
	for _, sub := range []string{"git", "npm"} {
		if err := syncPiAgentDir(ctx, ssh, filepath.Join(piDir, sub), sub); err != nil {
			return err
		}
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

func writePixExtension(ctx context.Context, ssh *SSH) error {
	const extension = `import { execFileSync } from "node:child_process";
import path from "node:path";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";

const PIX_CONTEXT = ` + "`" + `You are running inside pix.

Environment and security context:
- You have root access inside this Linux machine.
- This machine is an isolated, disposable VM managed by pix, not the user's host OS.
- You may freely perform actions inside this VM when they help complete the task: install packages and SDKs, use package managers, edit system configuration, create files, run services, and use caches.
- Do not try to escape the VM, mount or inspect host filesystems, access host secrets, use a host ssh-agent, or otherwise bypass the pix isolation boundary.
- Treat the current repository as the VM-side copy. Make project changes here; pix sync is responsible for moving committed results back to the host.
- When your work is complete, commit and push the result so the host can import it: git add -A && git commit -m "<message>" && git push origin pi-result.` + "`" + `;

type SyncState = { cwd: string; synced: boolean; checkedAt: number };
let syncState: SyncState | undefined;

function git(cwd: string, args: string[]): string {
  return execFileSync("git", ["-C", cwd, ...args], {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "ignore"],
    timeout: 1000,
  }).trim();
}

function isSynced(cwd: string): boolean {
  try {
    if (git(cwd, ["status", "--porcelain"]) !== "") return false;
    const head = git(cwd, ["rev-parse", "HEAD"]);
    const remote = git(cwd, ["ls-remote", "origin", "refs/heads/pi-result"]).split(/\s+/)[0] || "";
    return remote !== "" && head === remote;
  } catch {
    return false;
  }
}

function cachedSync(cwd: string): boolean {
  const now = Date.now();
  if (!syncState || syncState.cwd !== cwd || now - syncState.checkedAt > 1500) {
    syncState = { cwd, synced: isSynced(cwd), checkedAt: now };
  }
  return syncState.synced;
}

function projectName(cwd: string): string {
  if (path.basename(cwd) === "worktree") {
    return path.basename(path.dirname(cwd)).replace(/-[0-9a-f]{6}-[0-9a-f]{6}$/, "");
  }
  return path.basename(cwd) || cwd;
}

function formatTokens(n: number): string {
  if (n < 1000) return String(n);
  if (n < 1_000_000) return (n / 1000).toFixed(n < 10_000 ? 1 : 0) + "k";
  return (n / 1_000_000).toFixed(1) + "M";
}

export default function (pi: ExtensionAPI) {
  pi.on("before_agent_start", async (event) => {
    return { systemPrompt: event.systemPrompt + "\n\n" + PIX_CONTEXT };
  });

  pi.on("session_start", async (_event, ctx) => {
    ctx.ui.setFooter((tui, theme, footerData) => {
      const interval = setInterval(() => tui.requestRender(), 1500);
      const unsubBranch = footerData.onBranchChange(() => tui.requestRender());

      return {
        dispose() {
          clearInterval(interval);
          unsubBranch();
        },
        invalidate() {},
        render(width: number): string[] {
          const cwd = ctx.sessionManager.getCwd();
          const synced = cachedSync(cwd);
          const syncStatus = synced ? theme.fg("success", "SYNC") : theme.fg("error", "OUT OF SYNC");
          const projectLine = truncateToWidth(theme.fg("dim", "  " + projectName(cwd) + "  ") + syncStatus, width, theme.fg("dim", "..."));

          let totalInput = 0;
          let totalOutput = 0;
          let totalCacheRead = 0;
          let totalCacheWrite = 0;
          let totalCost = 0;
          for (const entry of ctx.sessionManager.getEntries()) {
            if (entry.type === "message" && entry.message.role === "assistant") {
              const usage = entry.message.usage;
              totalInput += usage.input;
              totalOutput += usage.output;
              totalCacheRead += usage.cacheRead;
              totalCacheWrite += usage.cacheWrite;
              totalCost += usage.cost.total;
            }
          }

          const statsParts: string[] = [];
          if (totalInput) statsParts.push("↑" + formatTokens(totalInput));
          if (totalOutput) statsParts.push("↓" + formatTokens(totalOutput));
          if (totalCacheRead) statsParts.push("R" + formatTokens(totalCacheRead));
          if (totalCacheWrite) statsParts.push("W" + formatTokens(totalCacheWrite));
          if (totalCost) statsParts.push("$" + totalCost.toFixed(3));

          const contextUsage = ctx.getContextUsage();
          const contextWindow = contextUsage?.contextWindow ?? ctx.model?.contextWindow ?? 0;
          const contextPercentValue = contextUsage?.percent ?? 0;
          const contextDisplay = contextUsage?.percent == null ? "?/" + formatTokens(contextWindow) : contextPercentValue.toFixed(1) + "%/" + formatTokens(contextWindow);
          if (contextWindow) {
            statsParts.push(
              contextPercentValue > 90
                ? theme.fg("error", contextDisplay)
                : contextPercentValue > 70
                  ? theme.fg("warning", contextDisplay)
                  : contextDisplay,
            );
          }

          const contentWidth = Math.max(1, width - 2);
          let statsLeft = statsParts.join(" ");
          if (visibleWidth(statsLeft) > contentWidth) statsLeft = truncateToWidth(statsLeft, contentWidth, "...");

          const branch = footerData.getGitBranch();
          const branchStr = branch ? " (" + branch + ")" : "";
          const rightSide = (ctx.model?.id || "no-model") + branchStr;
          const rightWidth = visibleWidth(rightSide);
          const leftWidth = visibleWidth(statsLeft);
          let statsLine: string;
          if (leftWidth + 2 + rightWidth <= contentWidth) {
            statsLine = statsLeft + " ".repeat(contentWidth - leftWidth - rightWidth) + rightSide;
          } else {
            const availableRight = contentWidth - leftWidth - 2;
            statsLine = availableRight > 0 ? statsLeft + "  " + truncateToWidth(rightSide, availableRight, "") : statsLeft;
          }

          const lines = [projectLine, theme.fg("dim", "  " + statsLine)];
          const extensionStatuses = footerData.getExtensionStatuses();
          if (extensionStatuses.size > 0) {
            lines.push("  " + truncateToWidth(Array.from(extensionStatuses.values()).join(" "), contentWidth, theme.fg("dim", "...")));
          }
          return lines;
        },
      };
    });
  });
}
`
	script := `
set -eu
mkdir -p /root/.pi/agent/extensions
rm -f /root/.pi/agent/extensions/pix-vm-context.ts
cat > /root/.pi/agent/extensions/pix-ext.ts
`
	return ssh.RunWithInput(ctx, "", []byte(extension), script)
}

func syncPiAgentDir(ctx context.Context, ssh *SSH, localDir, name string) error {
	info, err := os.Stat(localDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	archive, count, err := tarDirectory(localDir)
	if err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	vmDir := fmt.Sprintf("/root/.pi/agent/%s", name)
	script := fmt.Sprintf(`
set -eu
rm -rf %[1]s
mkdir -p %[1]s
tar -xf - -C %[1]s
`, shellQuote(vmDir))
	return ssh.RunWithInput(ctx, "", archive, script)
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
if [ -f /root/.pi/agent/settings.json ] && command -v node >/dev/null 2>&1; then
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
