package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"pix/internal/pix"
)

func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	app := pix.NewApp(stdin, stdout, stderr)
	if err := app.Run(context.Background(), args); err != nil {
		var exitErr pix.ExitError
		if errors.As(err, &exitErr) {
			fmt.Fprintln(stderr, exitErr.Error())
			return exitErr.Code
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
