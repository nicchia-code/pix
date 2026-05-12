package pibox

import (
	"strings"
	"testing"
)

func TestSSHArgsPassSingleRemoteCommand(t *testing.T) {
	ssh := &SSH{port: 1234, keyPath: "/tmp/key", knownHostPath: "/tmp/known_hosts"}
	args := ssh.args("mkdir -p '/var/lib/pibox/repos/example/worktree'")
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
