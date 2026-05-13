package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Main([]string{"help"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "pix install") {
		t.Fatalf("help output missing expected command: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "pix init") {
		t.Fatalf("help output contains removed pix init command: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "pix new") {
		t.Fatalf("help output missing pix new command: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "pix run") {
		t.Fatalf("help output contains removed pix run command: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "image update") {
		t.Fatalf("help output contains removed image update command: %s", stdout.String())
	}
}

func TestHelpInstallSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Main([]string{"help", "install"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "pix install") {
		t.Fatalf("install help output missing usage: %s", stdout.String())
	}
}

func TestInitCommandRemoved(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Main([]string{"init"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("init command succeeded unexpectedly, stdout = %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Comando sconosciuto: init") {
		t.Fatalf("init command error mismatch: %s", stderr.String())
	}
}

func TestHelpSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Main([]string{"help", "sync"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "pix sync") || !strings.Contains(stdout.String(), "pix sync --from-host") {
		t.Fatalf("sync help output missing usage: %s", stdout.String())
	}
}

func TestHelpNewSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Main([]string{"help", "new"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "pix new") {
		t.Fatalf("new help output missing usage: %s", stdout.String())
	}
}
