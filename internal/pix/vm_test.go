package pix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSSHArgsPassSingleRemoteCommand(t *testing.T) {
	ssh := &SSH{port: 1234, keyPath: "/tmp/key", knownHostPath: "/tmp/known_hosts"}
	args := ssh.args("mkdir -p '/var/lib/pix/repos/example/worktree'")
	last := args[len(args)-1]
	if !strings.HasPrefix(last, "sh -lc ") {
		t.Fatalf("last ssh arg = %q, want sh -lc command", last)
	}
	if strings.Contains(last, "sh -lc mkdir") {
		t.Fatalf("remote command is not shell-quoted: %q", last)
	}
}

func TestSSHInteractiveArgsRequestTTY(t *testing.T) {
	ssh := &SSH{port: 1234, keyPath: "/tmp/key", knownHostPath: "/tmp/known_hosts"}
	args := ssh.interactiveArgs("pi")
	if args[0] != "-tt" {
		t.Fatalf("first interactive arg = %q, want -tt", args[0])
	}
}

func TestGitSSHCommandUsesManagedKeyAndKnownHosts(t *testing.T) {
	ssh := &SSH{port: 1234, keyPath: "/tmp/key with spaces", knownHostPath: "/tmp/known hosts"}
	cmd := ssh.GitSSHCommand()
	for _, want := range []string{
		"ssh",
		"-i '/tmp/key with spaces'",
		"IdentitiesOnly=yes",
		"IdentityAgent=none",
		"ForwardAgent=no",
		"UserKnownHostsFile='/tmp/known hosts'",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("git ssh command %q missing %q", cmd, want)
		}
	}
}

func TestDefaultDiskFile(t *testing.T) {
	if got := defaultDiskFile("apple-virtualization"); got != "disk.raw" {
		t.Fatalf("darwin disk file = %q, want disk.raw", got)
	}
	if got := defaultDiskFile("qemu"); got != "disk.qcow2" {
		t.Fatalf("qemu disk file = %q, want disk.qcow2", got)
	}
}

func TestIsWindowsUNCPath(t *testing.T) {
	if !isWindowsUNCPath(`\\wsl.localhost\Ubuntu\home\luca\.pix\images\rootfs.tar.gz`) {
		t.Fatal("expected WSL UNC path to be detected")
	}
	if isWindowsUNCPath(`C:\Users\User01\AppData\Local\pix\images\rootfs.tar.gz`) {
		t.Fatal("did not expect local Windows path to be detected as UNC")
	}
}

func TestWindowsPathDir(t *testing.T) {
	got, err := windowsPathDir(`C:\Users\User01\AppData\Local\pix\wsl\pix-default`)
	if err != nil {
		t.Fatal(err)
	}
	want := `C:\Users\User01\AppData\Local\pix\wsl`
	if got != want {
		t.Fatalf("dir = %q, want %q", got, want)
	}
}

func TestParseSizeBytes(t *testing.T) {
	got, err := parseSizeBytes("40G")
	if err != nil {
		t.Fatal(err)
	}
	if got != 40<<30 {
		t.Fatalf("size = %d, want %d", got, int64(40<<30))
	}
}

func TestParseSHA256SUMS(t *testing.T) {
	data := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa *other.img\n" +
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb  noble-server-cloudimg-amd64.img\n")
	got, ok := parseSHA256SUMS(data, "noble-server-cloudimg-amd64.img")
	if !ok {
		t.Fatal("checksum not found")
	}
	want := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if got != want {
		t.Fatalf("checksum = %q, want %q", got, want)
	}
}

func TestVerifyFileSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFileSHA256(path, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"); err != nil {
		t.Fatal(err)
	}
	if err := verifyFileSHA256(path, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Fatal("expected checksum error")
	}
}
