package pix

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestNoArgsDoesNotShowUsage(t *testing.T) {
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldwd)
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}

	app := NewApp(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	err = app.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error outside an initialized repo")
	}
	if strings.Contains(err.Error(), "Usage:") {
		t.Fatalf("no-args should run resume, not show usage: %v", err)
	}
}
