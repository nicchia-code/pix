package pix

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
		return a.runResume(ctx, nil)
	}

	switch args[0] {
	case "install":
		return a.runInstall(ctx, args[1:])
	case "sync":
		return a.runSync(ctx, args[1:])
	case "new":
		return a.runNew(ctx, args[1:])
	case "resume":
		return a.runResume(ctx, args[1:])
	case "ssh":
		return a.runSSH(ctx, args[1:])
	case "vm":
		return a.runVM(ctx, args[1:])
	case "__darwin-vm-helper":
		return a.runDarwinVMHelper(ctx, args[1:])
	case "help", "-h", "--help":
		return a.help(args[1:])
	default:
		return userError(fmt.Sprintf("Comando sconosciuto: %s\n\nEsegui:\n  pix help", args[0]))
	}
}

func (a *App) usage() error {
	fmt.Fprintln(a.out, `pix - run Pi inside a persistent isolated Linux VM

Usage:
  pix install
  pix sync --from-host [--force]
  pix new [-- <pi args...>]
  pix resume [-- <pi args...>]
  pix                 # alias for pix resume
  pix ssh
  pix sync
  pix vm reset --yes`)
	return nil
}

func (a *App) help(args []string) error {
	if len(args) == 0 {
		return a.usage()
	}
	switch args[0] {
	case "install":
		fmt.Fprintln(a.out, `Usage:
  pix install

install creates or verifies pix host state, the managed VM, and managed SSH keys.`)
	case "sync":
		fmt.Fprintln(a.out, `Usage:
  pix sync
  pix sync --from-host [--force]

pix sync imports committed Pi results from the VM bridge Git repo into the host repo.
pix sync --from-host copies the current host working tree into the VM worktree.
If the repo has a .pixcontext file, matching files are copied even when ignored by Git.
If the host has uncommitted changes, sync --from-host warns but continues.
If the current repo is not registered yet, sync --from-host registers it automatically.
The --from-host direction overwrites the VM-side copy of the current repo.`)
	case "new":
		fmt.Fprintln(a.out, `Usage:
  pix new [-- <pi args...>]

Starts a new Pi session as root inside the VM worktree for the current registered repo.`)
	case "resume":
		fmt.Fprintln(a.out, `Usage:
  pix resume [-- <pi args...>]

Runs Pi with --resume as root inside the VM worktree for the current registered repo.
Calling pix without arguments is equivalent to pix resume.`)
	case "ssh":
		fmt.Fprintln(a.out, `Usage:
  pix ssh

Opens an interactive root shell inside the VM worktree for the current registered repo.`)
	case "vm":
		fmt.Fprintln(a.out, `Usage:
  pix vm reset --yes

Destroys and recreates the global pix VM. The host repo is not touched.`)
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
