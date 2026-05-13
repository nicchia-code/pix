//go:build linux

package pix

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func (v VM) ensureWSL2Appliance(ctx context.Context, root, statePath string) error {
	if !isWSL() {
		return userError("Il backend wsl2-appliance è disponibile solo dentro WSL2.")
	}
	if err := requireWSLInteropTool("wsl.exe"); err != nil {
		return err
	}
	if err := requireWSLInteropTool("wslpath"); err != nil {
		return err
	}

	state, err := readVMState(statePath)
	if err != nil {
		return err
	}
	state = normalizeVMState(root, state)
	if state.Backend != "wsl2-appliance" {
		return nil
	}
	state.Image = "ubuntu-24.04-lts-wsl-rootfs"
	if state.WSLDistro == "" {
		state.WSLDistro = defaultWSLDistro
	}
	if state.WSLRootFS == "" {
		state.WSLRootFS = filepath.Join(root, "images", wslRootFSFile())
	}
	if state.WSLInstallLocation == "" {
		loc, err := defaultWSLInstallLocation(ctx, v.runner, state.WSLDistro)
		if err != nil {
			return err
		}
		state.WSLInstallLocation = loc
	}

	exists, err := wslDistroExists(ctx, v.runner, state.WSLDistro)
	if err != nil {
		return err
	}
	if !exists {
		if _, err := os.Stat(state.WSLRootFS); os.IsNotExist(err) {
			path, err := downloadWSLRootFS(ctx)
			if err != nil {
				return err
			}
			state.WSLRootFS = path
		}
		rootfsWin, err := wslpathWindows(ctx, v.runner, state.WSLRootFS)
		if err != nil {
			return err
		}
		if err := ensureWindowsDir(ctx, v.runner, filepath.Dir(state.WSLInstallLocation)); err != nil {
			return err
		}
		_, _, err = v.runner.Run(ctx, "", nil, "wsl.exe", "--import", state.WSLDistro, state.WSLInstallLocation, rootfsWin, "--version", "2")
		if err != nil {
			return fmt.Errorf("import distro WSL pix: %w", err)
		}
	}

	if err := hardenWSLAppliance(ctx, v.runner, state.WSLDistro); err != nil {
		return err
	}
	if err := installWSLSSHKey(ctx, v.runner, root, state.WSLDistro); err != nil {
		return err
	}
	if err := writeVMState(statePath, state); err != nil {
		return err
	}
	if err := startWSLSSHD(ctx, v.runner, state.WSLDistro); err != nil {
		return err
	}
	host, err := wslDistroIP(ctx, v.runner, state.WSLDistro)
	if err != nil {
		return err
	}
	state.SSHHost = host
	state.SSHPort = 22
	if err := writeVMState(statePath, state); err != nil {
		return err
	}
	ssh := v.sshForState(root, state)
	return waitForSSH(ctx, ssh, 60*time.Second)
}

func requireWSLInteropTool(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return userError(fmt.Sprintf("Manca `%s` nel PATH. pix su WSL richiede interop Windows nella distro di controllo per creare l'appliance dedicata; l'appliance pix avrà invece automount e interop disabilitati.", name))
	}
	return nil
}

func wslDistroExists(ctx context.Context, r commandRunner, distro string) (bool, error) {
	stdout, _, err := r.Run(ctx, "", nil, "wsl.exe", "--list", "--quiet")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(normalizeWindowsText(string(stdout)), "\n") {
		if strings.TrimSpace(line) == distro {
			return true, nil
		}
	}
	return false, nil
}

func hardenWSLAppliance(ctx context.Context, r commandRunner, distro string) error {
	script := `set -eu
cat >/etc/wsl.conf <<'EOF'
[automount]
enabled=false

[interop]
enabled=false
appendWindowsPath=false

[user]
default=root
EOF
mkdir -p /var/lib/pix/repos /root/.ssh /run/sshd
chmod 700 /root/.ssh
if ! command -v sshd >/dev/null 2>&1 || ! command -v git >/dev/null 2>&1 || ! command -v curl >/dev/null 2>&1; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get install -y ca-certificates curl git openssh-server tar
fi
sed -i 's/^#\?PasswordAuthentication .*/PasswordAuthentication no/' /etc/ssh/sshd_config
sed -i 's/^#\?KbdInteractiveAuthentication .*/KbdInteractiveAuthentication no/' /etc/ssh/sshd_config
sed -i 's/^#\?PermitRootLogin .*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
sed -i 's/^#\?AllowAgentForwarding .*/AllowAgentForwarding no/' /etc/ssh/sshd_config
sed -i 's/^#\?AllowTcpForwarding .*/AllowTcpForwarding no/' /etc/ssh/sshd_config
printf '\nexport PATH="/root/.local/share/pi-node/current/bin:/root/.local/bin:/root/.pi/bin:$PATH"\n' >/etc/profile.d/pix-pi.sh
`
	if _, _, err := r.Run(ctx, "", nil, "wsl.exe", "-d", distro, "--user", "root", "--", "sh", "-lc", script); err != nil {
		return fmt.Errorf("configurazione appliance WSL pix: %w", err)
	}
	_, _, _ = r.Run(ctx, "", nil, "wsl.exe", "--terminate", distro)
	return nil
}

func installWSLSSHKey(ctx context.Context, r commandRunner, root, distro string) error {
	pubKeyPath := filepath.Join(root, "vm", "default", "ssh", "id_ed25519.pub")
	pubKey, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return fmt.Errorf("lettura chiave pubblica pix: %w", err)
	}
	script := `set -eu
mkdir -p /root/.ssh
chmod 700 /root/.ssh
cat >/root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys
`
	_, _, err = r.Run(ctx, "", pubKey, "wsl.exe", "-d", distro, "--user", "root", "--", "sh", "-lc", script)
	if err != nil {
		return fmt.Errorf("installazione chiave SSH appliance WSL pix: %w", err)
	}
	return nil
}

func startWSLSSHD(ctx context.Context, r commandRunner, distro string) error {
	script := `set -eu
if findmnt -rn -o TARGET | grep -Eq '^/mnt(/|$)'; then
  echo "pix appliance WSL non hardenizzata: /mnt risulta montato" >&2
  exit 1
fi
if [ -d /mnt ] && find /mnt -mindepth 1 -maxdepth 1 2>/dev/null | grep -q .; then
  echo "pix appliance WSL non hardenizzata: /mnt contiene filesystem host" >&2
  exit 1
fi
mkdir -p /run/sshd /root/.ssh
chmod 700 /root/.ssh
/usr/sbin/sshd || true
export PATH="/root/.local/share/pi-node/current/bin:/root/.local/bin:/root/.pi/bin:/usr/local/bin:/usr/bin:/bin:$PATH"
if ! command -v pi >/dev/null 2>&1; then
  curl -fsSL https://pi.dev/install.sh | sh
  ln -sf /root/.local/share/pi-node/current/bin/pi /usr/local/bin/pi || true
fi
`
	_, _, err := r.Run(ctx, "", nil, "wsl.exe", "-d", distro, "--user", "root", "--", "sh", "-lc", script)
	if err != nil {
		return fmt.Errorf("avvio sshd appliance WSL pix: %w", err)
	}
	return nil
}

func wslDistroIP(ctx context.Context, r commandRunner, distro string) (string, error) {
	stdout, _, err := r.Run(ctx, "", nil, "wsl.exe", "-d", distro, "--user", "root", "--", "sh", "-lc", "hostname -I | awk '{print $1}'")
	if err != nil {
		return "", err
	}
	host := strings.TrimSpace(string(stdout))
	if host == "" {
		return "", userError("Appliance WSL avviata, ma non riesco a determinare l'IP.")
	}
	return host, nil
}

func defaultWSLInstallLocation(ctx context.Context, r commandRunner, distro string) (string, error) {
	stdout, _, err := r.Run(ctx, "", nil, "cmd.exe", "/c", "echo", "%LOCALAPPDATA%")
	if err != nil {
		return "", userError("Non riesco a leggere %LOCALAPPDATA% via cmd.exe. pix su WSL richiede interop Windows nella distro di controllo per creare l'appliance dedicata.")
	}
	base := strings.TrimSpace(normalizeWindowsText(string(stdout)))
	if base == "" || strings.Contains(base, "%LOCALAPPDATA%") {
		return "", userError("%LOCALAPPDATA% Windows non disponibile per installare l'appliance WSL pix.")
	}
	return base + `\pix\wsl\` + distro, nil
}

func ensureWindowsDir(ctx context.Context, r commandRunner, path string) error {
	cmd := fmt.Sprintf("if not exist %s mkdir %s", cmdQuote(path), cmdQuote(path))
	_, _, err := r.Run(ctx, "", nil, "cmd.exe", "/c", cmd)
	return err
}

func cmdQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func wslpathWindows(ctx context.Context, r commandRunner, path string) (string, error) {
	stdout, _, err := r.Run(ctx, "", nil, "wslpath", "-w", path)
	if err != nil {
		return "", fmt.Errorf("conversione path Windows per %s: %w", path, err)
	}
	return strings.TrimSpace(normalizeWindowsText(string(stdout))), nil
}

func normalizeWindowsText(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func wslRootFSFile() string {
	switch runtime.GOARCH {
	case "arm64":
		return "ubuntu-noble-wsl-arm64-wsl.rootfs.tar.gz"
	default:
		return "ubuntu-noble-wsl-amd64-wsl.rootfs.tar.gz"
	}
}

func wslRootFSURL() (string, error) {
	switch runtime.GOARCH {
	case "amd64", "arm64":
		return "https://cloud-images.ubuntu.com/wsl/releases/24.04/current/" + wslRootFSFile(), nil
	default:
		return "", userError("Architettura non supportata per rootfs WSL Ubuntu LTS: " + runtime.GOARCH)
	}
}

func downloadWSLRootFS(ctx context.Context) (string, error) {
	root, err := ensureStateTree()
	if err != nil {
		return "", err
	}
	url, err := wslRootFSURL()
	if err != nil {
		return "", err
	}
	expected, err := fetchExpectedImageSHA256(ctx, url, wslRootFSFile())
	if err != nil {
		return "", err
	}
	dst := filepath.Join(root, "images", wslRootFSFile())
	tmp := dst + ".tmp"
	if err := downloadURLToFile(ctx, url, tmp); err != nil {
		return "", err
	}
	if err := verifyFileSHA256(tmp, expected); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return dst, nil
}

func unregisterWSLDistro(ctx context.Context, r commandRunner, distro string) error {
	if distro == "" || !isWSL() {
		return nil
	}
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		return nil
	}
	exists, err := wslDistroExists(ctx, r, distro)
	if err != nil || !exists {
		return err
	}
	_, _, err = r.Run(ctx, "", nil, "wsl.exe", "--unregister", distro)
	return err
}
