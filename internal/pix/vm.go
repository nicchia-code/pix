package pix

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	Backend            string `json:"backend"`
	Image              string `json:"image"`
	SSHHost            string `json:"ssh_host,omitempty"`
	SSHPort            int    `json:"ssh_port,omitempty"`
	PID                int    `json:"pid,omitempty"`
	BaseImage          string `json:"base_image,omitempty"`
	Disk               string `json:"disk,omitempty"`
	DiskSize           string `json:"disk_size,omitempty"`
	Seed               string `json:"seed,omitempty"`
	WSLDistro          string `json:"wsl_distro,omitempty"`
	WSLInstallLocation string `json:"wsl_install_location,omitempty"`
	WSLRootFS          string `json:"wsl_rootfs,omitempty"`
}

const (
	defaultDiskSize  = "40G"
	defaultWSLDistro = "pix-default"
)

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
		backend := recommendedBackend()
		state := VMState{
			Backend:   backend,
			Image:     imageName(),
			BaseImage: filepath.Join(root, "images", baseImageFile()),
			Disk:      filepath.Join(root, "vm", "default", defaultDiskFile(backend)),
			DiskSize:  defaultDiskSize,
			Seed:      filepath.Join(root, "vm", "default", "seed.iso"),
		}
		if err := writeVMState(statePath, state); err != nil {
			return "", err
		}
	}
	switch runtime.GOOS {
	case "linux":
		state, err := readVMState(statePath)
		if err != nil {
			return "", err
		}
		state = normalizeVMState(root, state)
		switch state.Backend {
		case "wsl2-appliance":
			if err := v.ensureWSL2Appliance(ctx, root, statePath); err != nil {
				return "", err
			}
		default:
			if err := v.ensureQEMU(ctx, root, statePath); err != nil {
				return "", err
			}
		}
	case "darwin":
		if err := v.ensureAppleVirtualization(ctx, root, statePath); err != nil {
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
	if state.SSHHost == "" {
		state.SSHHost = "127.0.0.1"
	}
	if state.SSHPort == 0 {
		return nil, userError("Backend VM non configurato.\n\nEsegui:\n  pix install")
	}
	ssh := v.sshForState(root, state)
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
	pidfile := filepath.Join(root, "vm", "default", "qemu.pid")
	if pid := readPID(pidfile); pid > 0 && state.PID == 0 {
		state.PID = pid
		_ = writeVMState(statePath, state)
	}
	if state.SSHPort != 0 {
		ssh := v.sshForState(root, state)
		if err := ssh.Run(ctx, "", "true"); err == nil {
			return nil
		}
		if processAlive(state.PID) {
			return waitForSSH(ctx, ssh, 90*time.Second)
		}
	}
	if state.SSHPort == 0 && processAlive(state.PID) {
		return userError("La VM pix sembra già in esecuzione, ma lo stato non contiene la porta SSH.\n\nNon avvio una seconda VM. Esegui `pix vm reset --yes` se vuoi ricrearla.")
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
		_, _, err := v.runner.Run(ctx, "", nil, "qemu-img", "create", "-f", "qcow2", "-F", "qcow2", "-b", state.BaseImage, state.Disk, state.DiskSize)
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
		if strings.Contains(err.Error(), "Failed to get \"write\" lock") {
			return userError("Il disco della VM pix è già bloccato da un processo QEMU.\n\nNon avvio una seconda VM. Se la VM è bloccata o lo stato è incoerente, usa:\n  pix vm reset --yes")
		}
		return fmt.Errorf("avvio QEMU: %w", err)
	}
	pid := readPID(pidfile)
	state.SSHPort = port
	state.PID = pid
	if err := writeVMState(statePath, state); err != nil {
		return err
	}
	ssh := v.sshForState(root, state)
	return waitForSSH(ctx, ssh, 90*time.Second)
}

func (v VM) sshForState(root string, state VMState) *SSH {
	host := state.SSHHost
	if host == "" {
		host = "127.0.0.1"
	}
	return &SSH{
		runner:            v.runner,
		host:              host,
		port:              state.SSHPort,
		keyPath:           filepath.Join(root, "vm", "default", "ssh", "id_ed25519"),
		knownHostPath:     filepath.Join(root, "vm", "default", "ssh", "known_hosts"),
		ignoreHostKeyScan: state.Backend == "apple-virtualization",
	}
}

func waitForSSH(ctx context.Context, ssh *SSH, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := ssh.Run(ctx, "", "true"); err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return userError(fmt.Sprintf("VM avviata, ma SSH non è diventato raggiungibile entro %s.", timeout))
}

func startBackground(ctx context.Context, r commandRunner, name string, args []string, pidfile string) error {
	quoted := append([]string{shellQuote(name)}, shellQuoteAll(args))
	logfile := pidfile + ".log"
	script := fmt.Sprintf("nohup %s >%s 2>&1 & echo $! > %s", strings.Join(quoted, " "), shellQuote(logfile), shellQuote(pidfile))
	_, _, err := r.Run(ctx, "", nil, "sh", "-lc", script)
	return err
}

func (v VM) writeSeed(ctx context.Context, root string, state VMState) error {
	dir, err := v.writeCloudInitFiles(root)
	if err != nil {
		return err
	}
	return createCloudInitSeed(ctx, v.runner, state.Seed, dir)
}

func (v VM) writeCloudInitFiles(root string) (string, error) {
	pubKeyPath := filepath.Join(root, "vm", "default", "ssh", "id_ed25519.pub")
	pubKey, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return "", fmt.Errorf("lettura chiave pubblica pix: %w", err)
	}
	dir := filepath.Join(root, "vm", "default", "cloud-init")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
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
  - printf '\nexport PATH="/root/.local/share/pi-node/current/bin:/root/.local/bin:/root/.pi/bin:$PATH"\n' >/etc/profile.d/pix-pi.sh
  - sh -c '(for i in $(seq 1 120); do ip -4 -o addr show scope global | awk '\''{split($4,a,"/"); print "PIX_IP=" a[1]}'\'' | head -n1; sleep 2; done) >/dev/hvc0 2>/dev/null &' || true
  - sh -c '(for i in $(seq 1 120); do ip -4 -o addr show scope global | awk '\''{split($4,a,"/"); print "PIX_IP=" a[1]}'\'' | head -n1; sleep 2; done) >/dev/console 2>/dev/null &' || true
  - mkdir -p /var/lib/pix/repos
  - sed -i 's/^#\?PasswordAuthentication .*/PasswordAuthentication no/' /etc/ssh/sshd_config
  - sed -i 's/^#\?KbdInteractiveAuthentication .*/KbdInteractiveAuthentication no/' /etc/ssh/sshd_config
  - sed -i 's/^#\?PermitRootLogin .*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
  - sed -i 's/^#\?AllowAgentForwarding .*/AllowAgentForwarding no/' /etc/ssh/sshd_config
  - sed -i 's/^#\?AllowTcpForwarding .*/AllowTcpForwarding no/' /etc/ssh/sshd_config
  - systemctl restart ssh || systemctl restart sshd || true
  - curl -fsSL https://pi.dev/install.sh | sh
  - ln -sf /root/.local/share/pi-node/current/bin/pi /usr/local/bin/pi || true
`, strings.TrimSpace(string(pubKey)))
	if err := os.WriteFile(filepath.Join(dir, "user-data"), []byte(userData), 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "meta-data"), []byte("instance-id: pix-default\nlocal-hostname: pix\n"), 0o600); err != nil {
		return "", err
	}
	return dir, nil
}

func imageName() string {
	return "ubuntu-24.04-lts-server-cloudimg-headless"
}

func baseImageFile() string {
	switch runtime.GOARCH {
	case "arm64":
		return "noble-server-cloudimg-arm64.img"
	default:
		return "noble-server-cloudimg-amd64.img"
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
	expected, err := fetchExpectedImageSHA256(ctx, url, baseImageFile())
	if err != nil {
		return "", err
	}
	dst := filepath.Join(root, "images", baseImageFile())
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

func downloadURLToFile(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download immagine base: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download immagine base: HTTP %s", resp.Status)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func fetchExpectedImageSHA256(ctx context.Context, imageURL, filename string) (string, error) {
	sumsURL := imageURL[:strings.LastIndex(imageURL, "/")+1] + "SHA256SUMS"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sumsURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download checksum immagine base: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download checksum immagine base: HTTP %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	expected, ok := parseSHA256SUMS(data, filename)
	if !ok {
		return "", fmt.Errorf("checksum immagine base non trovato in %s", sumsURL)
	}
	return expected, nil
}

func parseSHA256SUMS(data []byte, filename string) (string, bool) {
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		fields := bytes.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(string(fields[1]), "*")
		if filepath.Base(name) == filename && len(fields[0]) == sha256.Size*2 {
			return string(fields[0]), true
		}
	}
	return "", false
}

func verifyFileSHA256(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("checksum immagine base non valido: got %s, want %s", got, expected)
	}
	return nil
}

func requireTool(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return userError(fmt.Sprintf("Manca `%s` nel PATH.", name))
	}
	return nil
}

func createCloudInitSeed(ctx context.Context, r commandRunner, seedPath, dir string) error {
	return createNoCloudISO(seedPath, filepath.Join(dir, "user-data"), filepath.Join(dir, "meta-data"))
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
	_, _, err := v.runner.Run(ctx, "", nil, "ssh-keygen", "-t", "ed25519", "-N", "", "-f", keyPath, "-C", "pix-managed")
	if err != nil {
		return fmt.Errorf("generazione chiave SSH gestita da pix: %w", err)
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
		state.Disk = filepath.Join(root, "vm", "default", defaultDiskFile(state.Backend))
	}
	if state.DiskSize == "" {
		state.DiskSize = defaultDiskSize
	}
	if state.Seed == "" {
		state.Seed = filepath.Join(root, "vm", "default", "seed.iso")
	}
	return state
}

func defaultDiskFile(backend string) string {
	if backend == "apple-virtualization" {
		return "disk.raw"
	}
	return "disk.qcow2"
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
	runner            commandRunner
	host              string
	port              int
	keyPath           string
	knownHostPath     string
	ignoreHostKeyScan bool
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

func (s *SSH) Interactive(ctx context.Context, dir string, stdin io.Reader, stdout, stderr io.Writer, script string) error {
	return runInteractive(ctx, dir, stdin, stdout, stderr, "ssh", s.interactiveArgs(script)...)
}

func (s *SSH) PullURL(path string) string {
	return fmt.Sprintf("ssh://root@%s:%d%s", s.targetHost(), s.port, path)
}

func (s *SSH) targetHost() string {
	if s.host != "" {
		return s.host
	}
	return "127.0.0.1"
}

func (s *SSH) GitSSHCommand() string {
	knownHosts := shellQuote(s.knownHostPath)
	strictHostKeyChecking := "accept-new"
	if s.ignoreHostKeyScan {
		knownHosts = "/dev/null"
		strictHostKeyChecking = "no"
	}
	return strings.Join([]string{
		"ssh",
		"-i", shellQuote(s.keyPath),
		"-o", "IdentitiesOnly=yes",
		"-o", "IdentityAgent=none",
		"-o", "ForwardAgent=no",
		"-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no",
		"-o", "StrictHostKeyChecking=" + strictHostKeyChecking,
		"-o", "UserKnownHostsFile=" + knownHosts,
	}, " ")
}

func (s *SSH) hostKeyArgs() []string {
	if s.ignoreHostKeyScan {
		return []string{"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null"}
	}
	return []string{"-o", "StrictHostKeyChecking=accept-new", "-o", "UserKnownHostsFile=" + s.knownHostPath}
}

func (s *SSH) args(script string) []string {
	args := []string{
		"-p", strconv.Itoa(s.port),
		"-i", s.keyPath,
		"-o", "IdentitiesOnly=yes",
		"-o", "IdentityAgent=none",
		"-o", "ForwardAgent=no",
		"-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no",
	}
	args = append(args, s.hostKeyArgs()...)
	args = append(args,
		"root@"+s.targetHost(),
		"sh -lc "+shellQuote(script),
	)
	return args
}

func (s *SSH) interactiveArgs(script string) []string {
	args := []string{"-tt"}
	args = append(args, s.args(script)...)
	return args
}
