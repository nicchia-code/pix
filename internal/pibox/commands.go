package pibox

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func (a *App) runInit(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "repo" {
		return a.runInitRepo(ctx, args[1:])
	}
	if len(args) != 0 {
		return userError("Uso: pibox init oppure pibox init repo")
	}
	vm := newVM(osRunner{})
	root, err := vm.Init(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Stato pibox inizializzato in %s\n", root)
	fmt.Fprintf(a.out, "Backend: %s, immagine: %s\n", recommendedBackend(), imageName())
	return nil
}

func (a *App) runInitRepo(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return userError("Uso: pibox init repo")
	}
	r := osRunner{}
	root, err := gitRoot(ctx, r, ".")
	if err != nil {
		return err
	}
	gitDirPath, err := gitDir(ctx, r, root)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(repoConfigPath(gitDirPath)); statErr == nil {
		cfg, err := readRepoConfig(gitDirPath)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.out, "Repo già registrato: %s\n", cfg.RepoID)
		return nil
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	repoID, err := makeRepoID(root)
	if err != nil {
		return err
	}
	cfg := NewRepoConfig(repoID)
	if err := writeRepoConfig(gitDirPath, cfg); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Repo registrato: %s\n", cfg.RepoID)

	vm := newVM(r)
	ssh, err := vm.ensureReady(ctx)
	if err != nil {
		fmt.Fprintln(a.err, "Metadata host creati; setup VM rinviato.")
		return err
	}
	return ensureVMRepo(ctx, ssh, cfg)
}

func (a *App) runSync(ctx context.Context, args []string) error {
	fs := newFlagSet("sync", a.err)
	fromHost := fs.Bool("from-host", false, "")
	force := fs.Bool("force", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !*fromHost {
		return userError("Uso: pibox sync --from-host [--force]")
	}
	r := osRunner{}
	root, cfg, err := loadCurrentRepo(ctx, r)
	if err != nil {
		return err
	}
	if err := requireCleanWorktree(ctx, r, root); err != nil {
		return err
	}
	vm := newVM(r)
	ssh, err := vm.ensureReady(ctx)
	if err != nil {
		return err
	}
	if !*force {
		dirty, err := vmWorktreeDirty(ctx, ssh, cfg)
		if err != nil {
			return err
		}
		if dirty {
			return userError(fmt.Sprintf("Questo comando sovrascriverà la copia del repo dentro la VM.\n\nEventuali modifiche presenti in:\n  %s\n\nandranno perse se non sono già state portate fuori.\n\nUsa:\n  pibox sync --from-host --force\n\nper continuare.", cfg.WorktreePath))
		}
	}
	archive, err := gitArchive(ctx, r, root)
	if err != nil {
		return err
	}
	if err := syncArchiveToVM(ctx, ssh, cfg, archive); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Repo sincronizzato nella VM: %s\n", cfg.WorktreePath)
	return nil
}

func (a *App) runPi(ctx context.Context, args []string) error {
	fs := newFlagSet("run", a.err)
	if err := fs.Parse(args); err != nil {
		return err
	}
	r := osRunner{}
	_, cfg, err := loadCurrentRepo(ctx, r)
	if err != nil {
		return err
	}
	vm := newVM(r)
	ssh, err := vm.ensureReady(ctx)
	if err != nil {
		return err
	}
	if err := syncLocalPiCustomizations(ctx, ssh); err != nil {
		return err
	}
	piArgs := shellQuoteAll(fs.Args())
	script := fmt.Sprintf(`
set -eu
export PATH="/root/.local/share/pi-node/current/bin:/root/.local/bin:/root/.pi/bin:/usr/local/bin:/usr/bin:/bin:$PATH"
if ! command -v pi >/dev/null 2>&1; then
  curl -fsSL https://pi.dev/install.sh | sh
  ln -sf /root/.local/share/pi-node/current/bin/pi /usr/local/bin/pi || true
  export PATH="/root/.local/share/pi-node/current/bin:/root/.local/bin:/root/.pi/bin:/usr/local/bin:/usr/bin:/bin:$PATH"
fi
PI_BIN="$(command -v pi || true)"
if [ -z "$PI_BIN" ] && [ -x /root/.local/share/pi-node/current/bin/pi ]; then PI_BIN=/root/.local/share/pi-node/current/bin/pi; fi
if [ -z "$PI_BIN" ] && [ -x /root/.local/bin/pi ]; then PI_BIN=/root/.local/bin/pi; fi
if [ -z "$PI_BIN" ] && [ -x /root/.pi/bin/pi ]; then PI_BIN=/root/.pi/bin/pi; fi
if [ -z "$PI_BIN" ]; then
  echo "Pi non trovato dopo l'installazione." >&2
  exit 127
fi
cd %s
exec "$PI_BIN" %s
`, shellQuote(cfg.WorktreePath), piArgs)
	return ssh.Interactive(ctx, "", a.in, a.out, a.err, script)
}

func (a *App) runPull(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return userError("Uso: pibox pull")
	}
	r := osRunner{}
	root, cfg, err := loadCurrentRepo(ctx, r)
	if err != nil {
		return err
	}
	vm := newVM(r)
	ssh, err := vm.ensureReady(ctx)
	if err != nil {
		return err
	}
	branch := cfg.DefaultBranch
	if branch == "" {
		branch = resultBranch
	}
	lsRemote, _ := ssh.Output(ctx, "", fmt.Sprintf("git --git-dir=%s rev-parse --verify --quiet refs/heads/%s || true", shellQuote(cfg.BridgePath), shellQuote(branch)))
	if strings.TrimSpace(lsRemote) == "" {
		return userError("Nessun risultato da importare dalla VM.\n\nPi potrebbe non aver ancora committato/pushato nel bridge Git.")
	}
	_, _, err = r.Run(ctx, root, nil, "git", "pull", ssh.PullURL(cfg.BridgePath), branch)
	return err
}

func (a *App) runVM(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "reset" {
		return userError("Uso: pibox vm reset --yes")
	}
	fs := newFlagSet("vm reset", a.err)
	yes := fs.Bool("yes", false, "")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if !*yes {
		return userError("ATTENZIONE: questo eliminerà tutta la VM pibox.\n\nVerranno eliminati:\n- toolchain installate\n- cache\n- SDK\n- tutti i worktree dentro la VM\n- tutti i bridge.git dentro la VM\n- configurazioni modificate dentro il guest\n\nNon verranno toccati:\n- repo host\n- file host\n- .env host\n- chiavi SSH host\n\nPer continuare:\n  pibox vm reset --yes")
	}
	root, err := stateHome()
	if err != nil {
		return err
	}
	statePath := root + "/vm/default/state.json"
	if state, err := readVMState(statePath); err == nil && state.PID > 0 {
		_ = stopProcess(state.PID)
	}
	if err := os.RemoveAll(root + "/vm/default"); err != nil {
		return err
	}
	vm := newVM(osRunner{})
	if _, err := vm.Init(ctx); err != nil {
		return err
	}
	fmt.Fprintln(a.out, "VM pibox resettata.")
	return nil
}

func (a *App) runImage(ctx context.Context, args []string) error {
	if len(args) != 1 || args[0] != "update" {
		return userError("Uso: pibox image update")
	}
	path, err := downloadBaseImage(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Immagine headless LTS scaricata: %s\n", path)
	return nil
}

func loadCurrentRepo(ctx context.Context, r commandRunner) (string, RepoConfig, error) {
	root, err := gitRoot(ctx, r, ".")
	if err != nil {
		return "", RepoConfig{}, err
	}
	gitDirPath, err := gitDir(ctx, r, root)
	if err != nil {
		return "", RepoConfig{}, err
	}
	cfg, err := readRepoConfig(gitDirPath)
	if err != nil {
		return "", RepoConfig{}, err
	}
	return root, cfg, nil
}

func ensureVMRepo(ctx context.Context, ssh *SSH, cfg RepoConfig) error {
	script := fmt.Sprintf("mkdir -p %s %s && git init --bare %s >/dev/null", shellQuote(cfg.WorktreePath), shellQuote(cfg.BridgePath), shellQuote(cfg.BridgePath))
	return ssh.Run(ctx, "", script)
}

func vmWorktreeDirty(ctx context.Context, ssh *SSH, cfg RepoConfig) (bool, error) {
	script := fmt.Sprintf("if [ ! -d %s/.git ]; then exit 0; fi; cd %s && git status --porcelain", shellQuote(cfg.WorktreePath), shellQuote(cfg.WorktreePath))
	out, err := ssh.Output(ctx, "", script)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func syncArchiveToVM(ctx context.Context, ssh *SSH, cfg RepoConfig, archive []byte) error {
	script := fmt.Sprintf(`
set -eu
rm -rf %[1]s
mkdir -p %[1]s %[2]s
git init --bare %[2]s >/dev/null
tar -xf - -C %[1]s
cd %[1]s
git init -b %[3]s >/dev/null
git config user.name "pibox"
git config user.email "pibox@localhost"
git remote remove origin >/dev/null 2>&1 || true
git remote add origin %[2]s
git add -A
git commit --allow-empty -m "Import from host" >/dev/null
git push -f origin %[3]s >/dev/null
`, shellQuote(cfg.WorktreePath), shellQuote(cfg.BridgePath), shellQuote(resultBranch))
	return ssh.RunWithInput(ctx, "", archive, script)
}

func shellQuoteAll(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
