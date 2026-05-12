package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"pibox/internal/pibox"
)

func Main(args []string, stdout, stderr io.Writer) int {
	app := pibox.NewApp(stdout, stderr)
	if err := app.Run(context.Background(), args); err != nil {
		var exitErr pibox.ExitError
		if errors.As(err, &exitErr) {
			fmt.Fprintln(stderr, exitErr.Error())
			return exitErr.Code
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
