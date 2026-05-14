# pix

`pix` is a Go CLI that runs Pi inside a persistent Linux VM isolated from the host machine.

## At a glance

`pix` gives Pi a place where it can work freely without giving it direct access to the machine you actually develop on.

The idea is simple: instead of letting Pi operate inside the host repository, `pix` copies the project into a VM, lets Pi work there, and brings the result back through Git. You still get a familiar CLI workflow, but with a much clearer safety boundary.

In one sentence: `pix` makes it practical to let Pi do a lot inside a disposable machine, while letting it touch far less of the host.

## How it works, without the long version

The easiest way to understand `pix` is not to walk through every implementation detail. It is to follow the shape of the workflow: where Pi works, how changes move, and why the host stays out of reach.

### The host repository is not mounted into the VM

Problem: if Pi works directly on the host filesystem, a bad command can reach files, configuration, caches, and secrets that are not part of the task.

Choice: `pix` does not mount the host repository into the VM. Instead, it creates a VM-side working copy and treats that copy as the only place where Pi should make changes.

Result: the risky part of the workflow moves into an isolated environment. Pi can still inspect and modify the project copy, but it is not writing directly to the real host checkout.

### Changes come back through Git

Problem: isolation is useful only if there is still a reliable way to bring the work back to the host.

Choice: each registered repository has a bare Git repository inside the VM. Pi commits to the VM worktree and pushes to that internal bridge. The host imports those commits with `pix sync`.

Result: the VM-to-host boundary stays explicit, inspectable, and based on tooling developers already understand.

### The VM is persistent, but disposable

Problem: recreating a full development environment for every session would be slow, but giving Pi direct access to the host would be too permissive.

Choice: `pix` uses one persistent VM per user/machine. Toolchains, SDKs, caches, and VM-side repository copies live there across sessions.

Result: Pi can reuse expensive setup work, while the whole environment can still be reset with `pix vm reset --yes` if it gets broken.

### Pi gets freedom in the right place

Problem: if Pi is too constrained, it becomes much less useful. If it is unrestricted on the host, it becomes too risky.

Choice: Pi runs as `root` inside the VM, with network access and a prepared context, but without host filesystem mounts, host `~/.ssh`, or host `ssh-agent` forwarding.

Result: the project focuses less on blocking individual commands and more on choosing the right environment in which those commands are allowed to run.

For the full technical decisions, invariants, threat model, sync semantics, SSH rules, and product boundaries, see [`DESIGN.md`](./DESIGN.md).

## Typical workflow

```bash
pix install
pix sync --from-host
pix new
pix sync
```

In practice:

1. `pix install` prepares local state, the VM image, and SSH access;
2. `pix sync --from-host` copies the host repository into the VM;
3. `pix new` starts Pi inside the VM-side worktree;
4. `pix sync` imports the commits produced inside the VM back into the host repository.

Running `pix` with no arguments is equivalent to `pix resume`.

## Main commands

### Setup

#### `pix install`

Initializes or verifies host-side state, prepares the right VM backend for the platform, generates the SSH key managed by `pix`, downloads the Ubuntu LTS base image when needed, creates the persistent disk when needed, configures guest SSH, and verifies that the VM is reachable.

The command is intended to be idempotent. Running it multiple times should be safe.

### Repository sync

`pix` has two sync directions. They are intentionally documented together because they are the boundary between the host checkout and the VM copy.

#### `pix sync --from-host`

Copies the host repository into the VM. If the repository is not registered yet, this command registers it first.

The current implementation copies Git-tracked files and also untracked, non-ignored files from the host working tree. This is intentionally documented here because it is more permissive than the stricter committed/tracked-only model the project may move toward.

If the VM worktree contains unexported changes, `pix` requires `--force` before overwriting it:

```bash
pix sync --from-host --force
```

#### `pix sync`

Imports commits produced by Pi from the VM bridge repository into the host repository.

The host worktree must be clean before syncing from the VM back to the host. If Pi has not committed and pushed anything to the bridge, there is nothing to import.

### Pi sessions

#### `pix new`

Starts a new Pi session inside the VM worktree for the current repository.

#### `pix resume` or `pix`

Resumes a Pi session inside the VM. Running `pix` with no arguments is equivalent to `pix resume`.

#### `pix ssh`

Opens a root shell inside the VM worktree for the current repository.

### VM lifecycle

#### `pix vm reset --yes`

Destroys and recreates the global `pix` VM. It does not touch host repositories, host files, host `.env` files, or host SSH keys, but it removes VM-side toolchains, caches, SDKs, repository copies, bridge repositories, and guest configuration changes.

Reset is always explicit. It must not happen implicitly during install or update.

## Pi integration

When Pi is launched through `pix`, the CLI:

- installs Pi inside the VM if it is not already available;
- synchronizes selected local Pi customizations into the VM;
- injects a Pi extension that reminds the agent to work only inside the VM and to publish the final result with Git to `origin` on the `pi-result` branch.

In practice, Pi treats the VM worktree as the only editable copy of the project.

## Technical design

The technical decisions are documented in [`DESIGN.md`](./DESIGN.md). That file is the source of truth for:

- security and threat model;
- VM/host boundary;
- persistent state layout;
- Git bridge semantics;
- SSH rules;
- file sync and secrets;
- update model;
- non-goals.

## Build and test

Build locally:

```bash
make build
```

Run tests:

```bash
make test
```

A Go toolchain must be available in `PATH`.

## Code to read first

If you want to get oriented quickly, start here:

- `DESIGN.md`: technical decisions, security model, invariants, and product boundaries;
- `internal/pix/app.go`: CLI commands and main flows;
- `internal/pix/config.go`: host state and repository metadata;
- `internal/pix/git.go`: repository registration and host/VM sync;
- `internal/pix/vm.go`: VM provisioning, SSH, images, and common backend logic;
- `internal/pix/vm_darwin.go`: macOS backend;
- `internal/pix/vm_wsl_linux.go`: WSL2 backend;
- `internal/pix/pi_local.go`: Pi integration and VM-specific context.
