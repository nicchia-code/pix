package pibox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type commandRunner interface {
	Run(ctx context.Context, dir string, input []byte, name string, args ...string) ([]byte, []byte, error)
}

type osRunner struct{}

func (osRunner) Run(ctx context.Context, dir string, input []byte, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}

func commandText(ctx context.Context, r commandRunner, dir, name string, args ...string) (string, error) {
	stdout, _, err := r.Run(ctx, dir, nil, name, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(stdout)), nil
}
