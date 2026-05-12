package pibox

import (
	"context"
	"flag"
	"fmt"
	"io"
)

type App struct {
	in  io.Reader
	out io.Writer
	err io.Writer
}

func NewApp(in io.Reader, out, err io.Writer) *App {
	return &App{in: in, out: out, err: err}
}

func (a *App) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return a.usage()
	}

	switch args[0] {
	case "init":
		return a.runInit(ctx, args[1:])
	case "sync":
		return a.runSync(ctx, args[1:])
	case "run":
		return a.runPi(ctx, args[1:])
	case "resume":
		return a.runResume(ctx, args[1:])
	case "vm":
		return a.runVM(ctx, args[1:])
	case "image":
		return a.runImage(ctx, args[1:])
	case "help", "-h", "--help":
		return a.help(args[1:])
	default:
		return userError(fmt.Sprintf("Comando sconosciuto: %s\n\nEsegui:\n  pibox help", args[0]))
	}
}

func (a *App) usage() error {
	fmt.Fprintln(a.out, `pibox - run Pi inside a persistent isolated Linux VM

Usage:
  pibox init
  pibox init repo
  pibox sync --from-host [--force]
  pibox run [-- <pi args...>]
  pibox resume [-- <pi args...>]
  pibox sync
  pibox vm reset --yes
  pibox image update`)
	return nil
}

func (a *App) help(args []string) error {
	if len(args) == 0 {
		return a.usage()
	}
	switch args[0] {
	case "init":
		fmt.Fprintln(a.out, `Usage:
  pibox init
  pibox init repo

init creates or verifies pibox host state and managed SSH keys.
init repo registers the current Git repo in .git/pibox/config.json.`)
	case "sync":
		fmt.Fprintln(a.out, `Usage:
  pibox sync
  pibox sync --from-host [--force]

pibox sync imports committed Pi results from the VM bridge Git repo into the host repo.
pibox sync --from-host copies tracked files from the clean host Git HEAD into the VM worktree.
If the current repo is not registered yet, sync --from-host registers it automatically.
The --from-host direction overwrites the VM-side copy of the current repo.`)
	case "run":
		fmt.Fprintln(a.out, `Usage:
  pibox run [-- <pi args...>]

Runs Pi as root inside the VM worktree for the current registered repo.`)
	case "resume":
		fmt.Fprintln(a.out, `Usage:
  pibox resume [-- <pi args...>]

Runs Pi with --resume as root inside the VM worktree for the current registered repo.`)
	case "vm":
		fmt.Fprintln(a.out, `Usage:
  pibox vm reset --yes

Destroys and recreates the global pibox VM. The host repo is not touched.`)
	case "image":
		fmt.Fprintln(a.out, `Usage:
  pibox image update

Downloads a new headless Linux LTS base image for future VM resets.`)
	default:
		return userError(fmt.Sprintf("Argomento help sconosciuto: %s", args[0]))
	}
	return nil
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}
