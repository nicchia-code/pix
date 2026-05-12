package pibox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type VMState struct {
	Backend   string `json:"backend"`
	Image     string `json:"image"`
	SSHPort   int    `json:"ssh_port,omitempty"`
	PID       int    `json:"pid,omitempty"`
	BaseImage string `json:"base_image,omitempty"`
	Disk      string `json:"disk,omitempty"`
	Seed      string `json:"seed,omitempty"`
}

type VM struct {
	runner commandRunner
}

func newVM(r commandRunner) VM {
	return VM{runner: r}
}

func (v VM) Init(ctx context.Context) (string, error) {
	root, err := ensureStateTree()
	if err != nil {
		return "", err
	}
	if err := v.ensureSSHKey(ctx, root); err != nil {
		return "", err
	}
	statePath := filepath.Join(root, "vm", "default", "state.json")
	if _, err := os.Stat(statePath); errors.Is(err, os.ErrNotExist) {
		state := VMState{
			Backend:   recommendedBackend(),
			Image:     imageName(),
			BaseImage: filepath.Join(root, "images", baseImageFile()),
			Disk:      filepath.Join(root, "vm", "default", "disk.qcow2"),
			Seed:      filepath.Join(root, "vm", "default", "seed.iso"),
		}
		if err := writeVMState(statePath, state); err != nil {
			return "", err
		}
	}
	if runtime.GOOS == "linux" {
		if err := v.ensureQEMU(ctx, root, statePath); err != nil {
			return "", err
		}
	}
	return root, nil
}

func (v VM) ensureReady(ctx context.Context) (*SSH, error) {
	root, err := v.Init(ctx)
	if err != nil {
		return nil, err
	}
	state, err := readVMState(filepath.Join(root, "vm", "default", "state.json"))
	if err != nil {
		return nil, err
	}
	if state.SSHPort == 0 {
		return nil, userError("Backend VM non configurato.\n\nSu Linux installa qemu-img, qemu-system e cloud-localds, poi esegui:\n  pibox image update\n  pibox init")
	}
	ssh := &SSH{
		runner:        v.runner,
		port:          state.SSHPort,
		keyPath:       filepath.Join(root, "vm", "default", "ssh", "id_ed25519"),
		knownHostPath: filepath.Join(root, "vm", "default", "ssh", "known_hosts"),
	}
	if err := ssh.Run(ctx, "", "true"); err != nil {
		return nil, fmt.Errorf("SSH VM non raggiungibile su 127.0.0.1:%d: %w", state.SSHPort, err)
	}
	return ssh, nil
}

func (v VM) ensureQEMU(ctx context.Context, root, statePath string) error {
	state, err := readVMState(statePath)
	if err != nil {
		return err
	}
	state = normalizeVMState(root, state)
	if state.Backend != "qemu" {
		return nil
	}
	if state.SSHPort != 0 && processAlive(state.PID) {
		return nil
	}
	if err := requireTool("qemu-img"); err != nil {
		return err
	}
	qemuBin, err := qemuSystemBinary()
	if err != nil {
		return err
	}
	if _, err := os.Stat(state.BaseImage); os.IsNotExist(err) {
		path, err := downloadBaseImage(ctx)
		if err != nil {
			return err
		}
		state.BaseImage = path
	}
	if _, err := os.Stat(state.Disk); os.IsNotExist(err) {
		_, _, err := v.runner.Run(ctx, "", nil, "qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", state.BaseImage, state.Disk)
		if err != nil {
			return fmt.Errorf("creazione disco VM: %w", err)
		}
	}
	if err := v.writeSeed(ctx, root, state); err != nil {
		return err
	}
	port, err := freeLocalPort()
	if err != nil {
		return err
	}
	pidfile := filepath.Join(root, "vm", "default", "qemu.pid")
	args := []string{
		"-daemonize",
		"-pidfile", pidfile,
		"-display", "none",
		"-serial", "none",
		"-monitor", "none",
		"-m", "2048",
		"-smp", "2",
		"-drive", "file=" + state.Disk + ",if=virtio,format=qcow2",
		"-drive", "file=" + state.Seed + ",if=virtio,format=raw,readonly=on",
		"-netdev", fmt.Sprintf("user,id=net0,hostfwd=tcp:127.0.0.1:%d-:22", port),
		"-device", "virtio-net-pci,netdev=net0",
		"-no-reboot",
	}
	if kvmAvailable() {
		args = append([]string{"-machine", "accel=kvm:tcg"}, args...)
	} else {
		args = append([]string{"-machine", "accel=tcg"}, args...)
	}
	_, _, err = v.runner.Run(ctx, "", nil, qemuBin, args...)
	if err != nil {
		return fmt.Errorf("avvio QEMU: %w", err)
	}
	pid := readPID(pidfile)
	state.SSHPort = port
	state.PID = pid
	if err := writeVMState(statePath, state); err != nil {
		return err
	}
	ssh := &SSH{
		runner:        v.runner,
		port:          port,
		keyPath:       filepath.Join(root, "vm", "default", "ssh", "id_ed25519"),
		knownHostPath: filepath.Join(root, "vm", "default", "ssh", "known_hosts"),
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if err := ssh.Run(ctx, "", "true"); err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return userError("VM avviata, ma SSH non è diventato raggiungibile entro 90 secondi.")
}

func (v VM) writeSeed(ctx context.Context, root string, state VMState) error {
	pubKeyPath := filepath.Join(root, "vm", "default", "ssh", "id_ed25519.pub")
	pubKey, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return fmt.Errorf("lettura chiave pubblica pibox: %w", err)
	}
	dir := filepath.Join(root, "vm", "default", "cloud-init")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	userData := fmt.Sprintf(`#cloud-config
disable_root: false
ssh_pwauth: false
users:
  - name: root
    lock_passwd: true
    shell: /bin/bash
    ssh_authorized_keys:
      - %s
package_update: true
package_upgrade: false
packages:
  - ca-certificates
  - curl
  - git
  - openssh-server
  - tar
runcmd:
  - mkdir -p /var/lib/pibox/repos
  - sed -i 's/^#\?PasswordAuthentication .*/PasswordAuthentication no/' /etc/ssh/sshd_config
  - sed -i 's/^#\?KbdInteractiveAuthentication .*/KbdInteractiveAuthentication no/' /etc/ssh/sshd_config
  - sed -i 's/^#\?PermitRootLogin .*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
  - sed -i 's/^#\?AllowAgentForwarding .*/AllowAgentForwarding no/' /etc/ssh/sshd_config
  - sed -i 's/^#\?AllowTcpForwarding .*/AllowTcpForwarding no/' /etc/ssh/sshd_config
  - systemctl restart ssh || systemctl restart sshd || true
  - curl -fsSL https://pi.dev/install.sh | sh
`, strings.TrimSpace(string(pubKey)))
	if err := os.WriteFile(filepath.Join(dir, "user-data"), []byte(userData), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "meta-data"), []byte("instance-id: pibox-default\nlocal-hostname: pibox\n"), 0o600); err != nil {
		return err
	}
	return createCloudInitSeed(ctx, v.runner, state.Seed, dir)
}

func imageName() string {
	return "ubuntu-24.04-lts-server-cloudimg-headless"
}

func baseImageFile() string {
	switch runtime.GOARCH {
	case "arm64":
		return "ubuntu-24.04-server-cloudimg-arm64.img"
	default:
		return "ubuntu-24.04-server-cloudimg-amd64.img"
	}
}

func imageURL() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img", nil
	case "arm64":
		return "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-arm64.img", nil
	default:
		return "", userError("Architettura non supportata per l'immagine cloud Ubuntu LTS: " + runtime.GOARCH)
	}
}

func downloadBaseImage(ctx context.Context) (string, error) {
	root, err := ensureStateTree()
	if err != nil {
		return "", err
	}
	url, err := imageURL()
	if err != nil {
		return "", err
	}
	dst := filepath.Join(root, "images", baseImageFile())
	tmp := dst + ".tmp"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download immagine base: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download immagine base: HTTP %s", resp.Status)
	}
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	return dst, nil
}

func requireTool(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return userError(fmt.Sprintf("Manca `%s` nel PATH.", name))
	}
	return nil
}

func createCloudInitSeed(ctx context.Context, r commandRunner, seedPath, dir string) error {
	userData := filepath.Join(dir, "user-data")
	metaData := filepath.Join(dir, "meta-data")
	if _, err := exec.LookPath("cloud-localds"); err == nil {
		_, _, err := r.Run(ctx, "", nil, "cloud-localds", seedPath, userData, metaData)
		if err != nil {
			return fmt.Errorf("creazione seed cloud-init: %w", err)
		}
		return nil
	}
	if _, err := exec.LookPath("genisoimage"); err == nil {
		_, _, err := r.Run(ctx, "", nil, "genisoimage", "-quiet", "-output", seedPath, "-volid", "cidata", "-joliet", "-rock", "-graft-points", "user-data="+userData, "meta-data="+metaData)
		if err != nil {
			return fmt.Errorf("creazione seed cloud-init: %w", err)
		}
		return nil
	}
	if _, err := exec.LookPath("mkisofs"); err == nil {
		_, _, err := r.Run(ctx, "", nil, "mkisofs", "-quiet", "-output", seedPath, "-volid", "cidata", "-joliet", "-rock", "-graft-points", "user-data="+userData, "meta-data="+metaData)
		if err != nil {
			return fmt.Errorf("creazione seed cloud-init: %w", err)
		}
		return nil
	}
	if _, err := exec.LookPath("xorriso"); err == nil {
		_, _, err := r.Run(ctx, "", nil, "xorriso", "-as", "mkisofs", "-quiet", "-output", seedPath, "-volid", "cidata", "-joliet", "-rock", "-graft-points", "user-data="+userData, "meta-data="+metaData)
		if err != nil {
			return fmt.Errorf("creazione seed cloud-init: %w", err)
		}
		return nil
	}
	return userError("Manca un tool per creare il seed cloud-init: installa cloud-localds, genisoimage, mkisofs o xorriso.")
}

func qemuSystemBinary() (string, error) {
	candidates := []string{"qemu-system-x86_64"}
	if runtime.GOARCH == "arm64" {
		candidates = []string{"qemu-system-aarch64"}
	}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", userError("Manca qemu-system nel PATH.")
}

func kvmAvailable() bool {
	_, err := os.Stat("/dev/kvm")
	return err == nil
}

func freeLocalPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func stopProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	return nil
}

func readPID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

func (v VM) ensureSSHKey(ctx context.Context, root string) error {
	keyPath := filepath.Join(root, "vm", "default", "ssh", "id_ed25519")
	if _, err := os.Stat(keyPath); err == nil {
		return nil
	}
	_, _, err := v.runner.Run(ctx, "", nil, "ssh-keygen", "-t", "ed25519", "-N", "", "-f", keyPath, "-C", "pibox-managed")
	if err != nil {
		return fmt.Errorf("generazione chiave SSH gestita da pibox: %w", err)
	}
	return nil
}

func readVMState(path string) (VMState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return VMState{}, fmt.Errorf("lettura %s: %w", path, err)
	}
	var state VMState
	if err := json.Unmarshal(data, &state); err != nil {
		return VMState{}, fmt.Errorf("state VM non valido %s: %w", path, err)
	}
	return state, nil
}

func normalizeVMState(root string, state VMState) VMState {
	if state.Backend == "" {
		state.Backend = recommendedBackend()
	}
	if state.Image == "" {
		state.Image = imageName()
	}
	if state.BaseImage == "" {
		state.BaseImage = filepath.Join(root, "images", baseImageFile())
	}
	if state.Disk == "" {
		state.Disk = filepath.Join(root, "vm", "default", "disk.qcow2")
	}
	if state.Seed == "" {
		state.Seed = filepath.Join(root, "vm", "default", "seed.iso")
	}
	return state
}

func writeVMState(path string, state VMState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("scrittura %s: %w", path, err)
	}
	return nil
}

func recommendedBackend() string {
	switch runtime.GOOS {
	case "darwin":
		return "apple-virtualization"
	case "linux":
		if isWSL() {
			return "wsl2-appliance"
		}
		return "qemu"
	default:
		return runtime.GOOS
	}
}

func isWSL() bool {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "microsoft")
}

type SSH struct {
	runner        commandRunner
	port          int
	keyPath       string
	knownHostPath string
}

func (s *SSH) Run(ctx context.Context, dir, script string) error {
	_, _, err := s.runner.Run(ctx, dir, nil, "ssh", s.args(script)...)
	return err
}

func (s *SSH) Output(ctx context.Context, dir, script string) (string, error) {
	stdout, _, err := s.runner.Run(ctx, dir, nil, "ssh", s.args(script)...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(stdout)), nil
}

func (s *SSH) RunWithInput(ctx context.Context, dir string, input []byte, script string) error {
	_, _, err := s.runner.Run(ctx, dir, input, "ssh", s.args(script)...)
	return err
}

func (s *SSH) PullURL(path string) string {
	return fmt.Sprintf("ssh://root@127.0.0.1:%d%s", s.port, path)
}

func (s *SSH) args(script string) []string {
	return []string{
		"-p", strconv.Itoa(s.port),
		"-i", s.keyPath,
		"-o", "IdentitiesOnly=yes",
		"-o", "IdentityAgent=none",
		"-o", "ForwardAgent=no",
		"-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=" + s.knownHostPath,
		"root@127.0.0.1",
		"sh -lc " + shellQuote(script),
	}
}
