# README_AGENTS

This file explains `pix` to an agent that needs to read or modify this repository. Read `DESIGN.md` for the technical decisions and boundaries that replaced the old standalone specification.

## Project purpose

`pix` is a CLI that runs Pi inside a persistent Linux VM isolated from the host.

Use this mental model:

- the host is not where Pi works;
- the VM is the disposable work environment;
- the host repository is a sync source and destination;
- Git is the controlled bridge between host and VM.

## Non-negotiable invariants

Preserve these rules when changing the code:

- there is one persistent VM per user/machine;
- Pi runs as `root` inside the VM;
- the VM has unrestricted internet access;
- the VM must not mount host filesystems;
- the VM must not receive host secrets, host `~/.ssh`, or the host `ssh-agent`;
- every registered repository has a `worktree/` and a `bridge.git/` under `/var/lib/pix/repos/<repo_id>/`;
- Pi must not write directly to the host checkout;
- `pix sync` without flags imports from the VM Git bridge into the host repository;
- `pix sync --from-host` is destructive on the VM-side copy and must keep that explicit;
- `pix vm reset --yes` is explicit and destructive;
- updating the CLI must not implicitly reset, replace, or migrate the existing VM.

If a change simplifies the code but weakens these invariants, treat it as suspicious.

## Code map

Entry point:

- `cmd/pix/main.go`: passes CLI arguments to `internal/cli`.
- `internal/cli/cli.go`: converts application errors into CLI exit codes.

Core application:

- `internal/pix/app.go`: command routing (`install`, `sync`, `new`, `resume`, `ssh`, `vm reset`).
- `internal/pix/config.go`: host-side global state and local repository config in `.git/pix/config.json`.
- `internal/pix/git.go`: Git root detection, `repo_id` generation, file export to the VM.
- `internal/pix/vm.go`: VM lifecycle, SSH, image download, shared QEMU logic.
- `internal/pix/vm_darwin.go`: macOS backend through `Virtualization.framework`.
- `internal/pix/vm_wsl_linux.go`: dedicated WSL2 appliance backend.
- `internal/pix/pi_local.go`: Pi extensions/settings sync and `pix` context injection.
- `internal/pix/exec.go`: local and interactive command execution.
- `internal/pix/errors.go`: user-facing errors with exit codes.

Support:

- `internal/pix/cloudinit_iso.go`: creates the NoCloud seed ISO for cloud-init.
- `internal/pix/image_convert.go`: converts qcow2 to raw for the macOS backend.

## Core flows

### 1. Install

`pix install`:

- initializes `~/.pix/`;
- generates a dedicated SSH key;
- chooses the backend for the current platform;
- downloads the Ubuntu LTS base image if missing;
- creates or verifies the persistent VM;
- waits for SSH to become reachable;
- syncs local Pi customizations into the VM.

### 2. Host to VM sync

`pix sync --from-host`:

- finds the host Git root;
- creates `.git/pix/config.json` if the repo is not registered;
- ensures `worktree/` and `bridge.git/` exist in the VM;
- blocks if the VM worktree is dirty and `--force` is not provided;
- updates the VM bridge and VM worktree.

Important current behavior: `gitWorktreeTar()` includes untracked, non-ignored files and uncommitted tracked changes. Do not assume host-to-VM sync is committed/tracked-only unless the implementation is changed deliberately.

### 3. Pi execution

`pix new` and `pix resume`:

- load the current repository config;
- bring the VM to a ready state;
- sync Pi extensions/settings into the VM;
- install Pi in the VM if needed;
- run `pi` inside the VM `worktree/`.

The official bridge branch is `pi-result`.

### 4. VM to host sync

`pix sync`:

- requires a clean host worktree;
- checks that the bridge has the expected branch;
- fetches from the bridge Git repository over SSH;
- merges `FETCH_HEAD` with `--no-edit`.

The code must not automatically commit VM changes. Pi must commit and push from inside the guest.

## Persistent state

Host global state:

```text
~/.pix/
  images/
  vm/
    default/
      state.json
      ssh/
  logs/
```

Registered host repository:

```text
.git/pix/config.json
```

Repository inside the VM:

```text
/var/lib/pix/repos/<repo_id>/
  worktree/
  bridge.git/
```

## Security checklist

The project does not try to restrict Pi heavily inside the guest. Pi may run as `root`, install packages, change `/etc`, break toolchains, or break a VM-side worktree. That is acceptable.

The real boundary is between guest and host. Be careful with changes touching:

- SSH options and host key handling;
- host/guest path handling;
- mounts, automounts, or directory sharing;
- environment forwarding;
- secret forwarding;
- agent sockets;
- Git remotes that point outside the internal VM bridge;
- reset or update behavior.

Pi must not gain access to:

- host `.env` files;
- host API keys;
- host `~/.ssh`;
- host cloud credentials;
- host GitHub/GitLab credentials;
- host Docker credentials;
- host keychains;
- the host `ssh-agent`.

## Product boundaries

Do not add these without treating the change as architectural:

- API key brokering;
- `.env` handling inside the VM;
- automatic test execution using host secrets;
- repository-to-repository isolation inside the same VM;
- snapshots;
- CLI-level rollback;
- one VM per repository;
- Docker as a required backend;
- native Windows support outside WSL2.

## How to reason before changing code

Ask these questions before changing `pix`:

1. Does this change the host/VM boundary?
2. Does it introduce access to host filesystems, host secrets, or host credentials?
3. Does it change `pix sync` or `pix sync --from-host` semantics?
4. Does it make a destructive action less explicit?
5. Does it change backend selection or persistent state format?
6. Does it make VM reset, CLI update, or base image update behavior implicit?

If the answer is yes, treat the change as architectural rather than a simple refactor.

## Reading order for agents

To understand the project quickly, read in this order:

1. `README.md`
2. `DESIGN.md`
3. `internal/pix/app.go`
4. `internal/pix/config.go`
5. `internal/pix/git.go`
6. `internal/pix/vm.go`
7. the relevant backend file: `internal/pix/vm_darwin.go` or `internal/pix/vm_wsl_linux.go`
8. `internal/pix/pi_local.go`

## Build and test

Main commands:

```bash
make build
make test
```

A Go toolchain must be available in `PATH`.
