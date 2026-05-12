//go:build darwin

package pibox

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	vz "github.com/Code-Hex/vz/v3"
)

const appleVMMAC = "5a:94:ef:e4:0c:ee"

func (v VM) ensureAppleVirtualization(ctx context.Context, root, statePath string) error {
	state, err := readVMState(statePath)
	if err != nil {
		return err
	}
	state = normalizeVMState(root, state)
	if state.Backend != "apple-virtualization" {
		return nil
	}
	if state.SSHHost == "127.0.0.1" || state.SSHHost == "localhost" {
		state.SSHHost = ""
	}
	if runtime.GOARCH != "arm64" {
		return userError("Il backend macOS di pix supporta per ora solo Apple Silicon (darwin/arm64).")
	}

	vmDir := filepath.Join(root, "vm", "default")
	pidfile := filepath.Join(vmDir, "apple-vz.pid")
	if pid := readPID(pidfile); pid > 0 && state.PID == 0 {
		state.PID = pid
	}
	if state.SSHHost != "" && state.SSHPort != 0 {
		ssh := v.sshForState(root, state)
		if err := ssh.Run(ctx, "", "true"); err == nil {
			return nil
		}
		if processAlive(state.PID) {
			return waitForSSH(ctx, ssh, 120*time.Second)
		}
	}
	if processAlive(state.PID) && state.SSHHost == "" {
		return userError("La VM pix sembra già in esecuzione, ma lo stato non contiene l'IP SSH.\n\nNon avvio una seconda VM. Esegui `pix vm reset --yes` se vuoi ricrearla.")
	}

	if _, err := os.Stat(state.BaseImage); os.IsNotExist(err) {
		path, err := downloadBaseImage(ctx)
		if err != nil {
			return err
		}
		state.BaseImage = path
	}
	if _, err := os.Stat(state.Disk); os.IsNotExist(err) {
		size, err := parseSizeBytes(state.DiskSize)
		if err != nil {
			return err
		}
		if err := convertImageToRaw(state.BaseImage, state.Disk, size); err != nil {
			return fmt.Errorf("creazione disco raw macOS: %w", err)
		}
	}
	if err := v.writeSeed(ctx, root, state); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := startBackground(ctx, v.runner, exe, []string{"__darwin-vm-helper", root, statePath}, pidfile); err != nil {
		return fmt.Errorf("avvio helper Virtualization.framework: %w", err)
	}
	state.PID = readPID(pidfile)
	state.SSHPort = 22
	if err := writeVMState(statePath, state); err != nil {
		return err
	}

	host, err := waitForDarwinGuestIP(ctx, appleVMMAC, filepath.Join(vmDir, "apple-vz-serial.log"), 120*time.Second)
	if err != nil {
		return err
	}
	state.SSHHost = host
	if err := writeVMState(statePath, state); err != nil {
		return err
	}
	ssh := v.sshForState(root, state)
	return waitForSSH(ctx, ssh, 10*time.Minute)
}

func (a *App) runDarwinVMHelper(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return userError("Uso interno: __darwin-vm-helper <root> <state-path>")
	}
	root, statePath := args[0], args[1]
	state, err := readVMState(statePath)
	if err != nil {
		return err
	}
	state = normalizeVMState(root, state)
	return runAppleVirtualizationVM(ctx, root, state)
}

func runAppleVirtualizationVM(ctx context.Context, root string, state VMState) error {
	vmDir := filepath.Join(root, "vm", "default")
	efiPath := filepath.Join(vmDir, "efi-variable-store")
	var variableStore *vz.EFIVariableStore
	var err error
	if _, statErr := os.Stat(efiPath); os.IsNotExist(statErr) {
		variableStore, err = vz.NewEFIVariableStore(efiPath, vz.WithCreatingEFIVariableStore())
	} else {
		variableStore, err = vz.NewEFIVariableStore(efiPath)
	}
	if err != nil {
		return fmt.Errorf("EFI variable store: %w", err)
	}
	bootloader, err := vz.NewEFIBootLoader(vz.WithEFIVariableStore(variableStore))
	if err != nil {
		return fmt.Errorf("EFI bootloader: %w", err)
	}
	config, err := vz.NewVirtualMachineConfiguration(bootloader, 2, 2048*1024*1024)
	if err != nil {
		return err
	}

	rootDisk, err := virtioBlock(state.Disk, false)
	if err != nil {
		return err
	}
	seedDisk, err := virtioBlock(state.Seed, true)
	if err != nil {
		return err
	}
	config.SetStorageDevicesVirtualMachineConfiguration([]vz.StorageDeviceConfiguration{rootDisk, seedDisk})

	entropy, err := vz.NewVirtioEntropyDeviceConfiguration()
	if err != nil {
		return err
	}
	config.SetEntropyDevicesVirtualMachineConfiguration([]*vz.VirtioEntropyDeviceConfiguration{entropy})

	nat, err := vz.NewNATNetworkDeviceAttachment()
	if err != nil {
		return err
	}
	netdev, err := vz.NewVirtioNetworkDeviceConfiguration(nat)
	if err != nil {
		return err
	}
	hw, err := net.ParseMAC(appleVMMAC)
	if err != nil {
		return err
	}
	mac, err := vz.NewMACAddress(hw)
	if err != nil {
		return err
	}
	netdev.SetMACAddress(mac)
	config.SetNetworkDevicesVirtualMachineConfiguration([]*vz.VirtioNetworkDeviceConfiguration{netdev})

	serialAttachment, err := vz.NewFileSerialPortAttachment(filepath.Join(vmDir, "apple-vz-serial.log"), true)
	if err != nil {
		return err
	}
	serial, err := vz.NewVirtioConsoleDeviceSerialPortConfiguration(serialAttachment)
	if err != nil {
		return err
	}
	config.SetSerialPortsVirtualMachineConfiguration([]*vz.VirtioConsoleDeviceSerialPortConfiguration{serial})

	if ok, err := config.Validate(); !ok || err != nil {
		return fmt.Errorf("config VM macOS non valida: %w", err)
	}
	machine, err := vz.NewVirtualMachine(config)
	if err != nil {
		return err
	}
	if err := machine.Start(); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			_, _ = machine.RequestStop()
			return ctx.Err()
		case state := <-machine.StateChangedNotify():
			if state == vz.VirtualMachineStateStopped || state == vz.VirtualMachineStateError {
				return fmt.Errorf("VM macOS terminata: %v", state)
			}
		}
	}
}

func virtioBlock(path string, readOnly bool) (*vz.VirtioBlockDeviceConfiguration, error) {
	attachment, err := vz.NewDiskImageStorageDeviceAttachment(path, readOnly)
	if err != nil {
		return nil, err
	}
	return vz.NewVirtioBlockDeviceConfiguration(attachment)
}

func waitForDarwinGuestIP(ctx context.Context, mac, serialLog string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ip, err := darwinGuestIP(mac); err == nil && ip != "" {
			return ip, nil
		}
		if ip, err := darwinGuestIPFromSerial(serialLog); err == nil && ip != "" {
			return ip, nil
		}
		if tcpPortOpen(ctx, "192.168.64.2", 22, time.Second) {
			return "192.168.64.2", nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return "", userError("VM avviata, ma non riesco a trovare l'IP assegnato dal NAT macOS.\n\nControlla i log:\n  " + serialLog + "\n  /var/db/dhcpd_leases")
}

func tcpPortOpen(ctx context.Context, host string, port int, timeout time.Duration) bool {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func darwinGuestIPFromSerial(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	re := regexp.MustCompile(`PIBOX_IP=([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)`)
	matches := re.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		return "", nil
	}
	return matches[len(matches)-1][1], nil
}

func darwinGuestIP(mac string) (string, error) {
	data, err := os.ReadFile("/var/db/dhcpd_leases")
	if err != nil {
		return "", err
	}
	return parseDarwinDHCPLeases(string(data), mac), nil
}

func parseDarwinDHCPLeases(data, mac string) string {
	want := strings.ToLower(strings.ReplaceAll(mac, ":", ""))
	blocks := strings.Split(data, "{")
	ipRe := regexp.MustCompile(`ip_address=([^;\n]+)`)
	hwRe := regexp.MustCompile(`hw_address=([^;\n]+)`)
	for _, block := range blocks {
		hw := hwRe.FindStringSubmatch(block)
		if len(hw) != 2 {
			continue
		}
		got := strings.ToLower(hw[1])
		got = strings.TrimPrefix(got, "1,")
		got = strings.ReplaceAll(got, ":", "")
		if got != want {
			continue
		}
		ip := ipRe.FindStringSubmatch(block)
		if len(ip) == 2 {
			return strings.TrimSpace(ip[1])
		}
	}
	return ""
}
