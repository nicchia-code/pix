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
	if !strings.Contains(stdout.String(), "pibox init repo") {
		t.Fatalf("help output missing expected command: %s", stdout.String())
	}
}

func TestHelpSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Main([]string{"help", "sync"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "pibox sync --from-host") {
		t.Fatalf("sync help output missing usage: %s", stdout.String())
	}
}
