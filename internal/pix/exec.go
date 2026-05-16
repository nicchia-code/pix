package pix

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
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
		output := strings.TrimSpace(stderr.String())
		if out := strings.TrimSpace(stdout.String()); out != "" {
			if output != "" {
				output += "\n"
			}
			output += out
		}
		return stdout.Bytes(), stderr.Bytes(), fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, output)
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}

func runInteractive(ctx context.Context, dir string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return userError("Comando remoto terminato con exit status " + strconv.Itoa(exitErr.ExitCode()) + ".")
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func commandText(ctx context.Context, r commandRunner, dir, name string, args ...string) (string, error) {
	stdout, _, err := r.Run(ctx, dir, nil, name, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(stdout)), nil
}

func conciseCommandError(err error, stdout, stderr []byte) error {
	status := "comando fallito"
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		status = "comando terminato con exit status " + strconv.Itoa(exitErr.ExitCode())
	}

	output := strings.TrimSpace(string(stderr))
	if out := strings.TrimSpace(string(stdout)); out != "" {
		if output != "" {
			output += "\n"
		}
		output += out
	}
	if output == "" {
		return errors.New(status)
	}
	return errors.New(status + "\n" + output)
}
