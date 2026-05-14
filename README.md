# pix

`pix` is a Go CLI that runs Pi inside a persistent Linux VM isolated from the host machine.

## At a glance

`pix` gives Pi a place where it can work freely without giving it direct access to the machine you actually develop on.

The idea is simple: instead of letting Pi operate inside the host repository, `pix` copies the project into a VM, lets Pi work there, and brings the result back through Git. You still get a familiar CLI workflow, but with a much clearer safety boundary.

In one sentence: `pix` makes it practical to let Pi do a lot inside a disposable machine, while letting it touch far less of the host.

## The decisions that matter

The easiest way to understand how `pix` works is not to walk through every implementation detail. It is to look at the design choices that shape the project.

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

## Security model

Pi is not assumed to be intentionally malicious, but it is allowed to make mistakes.

Inside the VM, these mistakes are acceptable:

```bash
rm -rf /
apt install ...
modify /etc
break a VM-side worktree
break a VM-side bridge.git
break installed toolchains
```

Those mistakes must stay inside the VM.

Pi must not be able to:

- read the host home directory;
- mount host directories;
- read host `.env` files;
- read host API keys;
- access host `~/.ssh`;
- use the host `ssh-agent`;
- modify host files directly;
- push through a host Git remote or host credentials;
- depend on host secrets to work.

Pi may:

- run as `root` inside the VM;
- access the internet from inside the VM;
- modify any file inside the VM;
- install SDKs, package managers, toolchains, and caches;
- modify VM-side copies of repositories;
- commit and push to the internal VM bridge repository.

The key promise is VM-to-host isolation. `pix` does not promise isolation between different repositories inside the same VM.

## Architecture

Each machine has one global persistent VM. All registered repositories live inside that VM:

```text
/var/lib/pix/
  repos/
    <repo_id>/
      worktree/
      bridge.git/
```

For each repository:

- `worktree/` is the editable copy Pi works on;
- `bridge.git/` is a bare Git repository used as the bridge back to the host;
- the VM worktree remote named `origin` points to the internal `bridge.git`, not to GitHub, GitLab, the host filesystem, or an external remote.

On the host, `pix` keeps global state under `~/.pix/`, for example:

```text
~/.pix/
  images/
    base-lts.img
  vm/
    default/
      disk.qcow2
      state.json
      ssh/
        id_ed25519
        id_ed25519.pub
        known_hosts
  logs/
```

For each registered local checkout, `pix` stores non-versioned metadata in:

```text
.git/pix/config.json
```

That file maps the host checkout to its VM-side repository. It is local metadata and should not be committed.

## Git bridge model

Git is the only supported bridge between host and VM.

Host to VM:

```bash
pix sync --from-host
```

Pi inside the VM:

```bash
cd /var/lib/pix/repos/<repo_id>/worktree
git add -A
git commit -m "..."
git push origin pi-result
```

VM to host:

```bash
pix sync
```

`pix sync` does not create commits for Pi. It does not run `git add`, `git commit`, or `git push` inside the VM. Pi is responsible for committing and pushing to the internal bridge.

The official bridge branch is:

```text
pi-result
```

If a sync from the VM brings back the wrong result, rollback is normal Git work on the host:

```bash
git reset --hard HEAD~1
git reflog
git reset --hard <sha>
```

## Main commands

### `pix install`

Initializes or verifies host-side state, prepares the right VM backend for the platform, generates the SSH key managed by `pix`, downloads the Ubuntu LTS base image when needed, creates the persistent disk when needed, configures guest SSH, and verifies that the VM is reachable.

The command is intended to be idempotent. Running it multiple times should be safe.

### `pix sync --from-host`

Registers the current repository if needed and synchronizes the host project into the VM.

The current implementation copies Git-tracked files and also untracked, non-ignored files from the host working tree. This is intentionally documented here because it is more permissive than the stricter committed/tracked-only model the project may move toward.

If the VM worktree contains unexported changes, `pix` requires `--force` before overwriting it:

```bash
pix sync --from-host --force
```

### `pix new`

Starts a new Pi session inside the VM worktree for the current repository.

### `pix resume` or `pix`

Resumes a Pi session inside the VM. Running `pix` with no arguments is equivalent to `pix resume`.

### `pix sync`

Imports commits produced by Pi from the VM bridge repository into the host repository.

The host worktree must be clean before syncing from the VM back to the host. If Pi has not committed and pushed anything to the bridge, there is nothing to import.

### `pix ssh`

Opens a root shell inside the VM worktree for the current repository.

### `pix vm reset --yes`

Destroys and recreates the global `pix` VM. It does not touch host repositories, host files, host `.env` files, or host SSH keys, but it removes VM-side toolchains, caches, SDKs, repository copies, bridge repositories, and guest configuration changes.

Reset is always explicit. It must not happen implicitly during install or update.

## Supported platforms and backends

`pix` supports UNIX-like environments:

- macOS;
- Linux;
- Windows through WSL2.

Native Windows, PowerShell, and CMD are not primary targets.

`pix` chooses the VM backend based on the platform:

- macOS: Apple `Virtualization.framework`;
- Linux: QEMU, with KVM when available and software emulation fallback when needed;
- WSL2: a dedicated appliance/distro with additional hardening.

The interface to the VM stays the same across platforms: SSH managed by the CLI.

## SSH rules

The VM is accessed through SSH managed by `pix`.

The intended rules are:

- no user private keys inside the VM;
- no host `ssh-agent` forwarding;
- no host `~/.ssh` mount;
- no password login;
- no root password login;
- no SSH exposure on `0.0.0.0`.

Recommended guest SSH posture:

```sshconfig
PermitRootLogin prohibit-password
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
AllowAgentForwarding no
AllowTcpForwarding no
X11Forwarding no
PermitTunnel no
PermitUserEnvironment no
```

## File sync and secrets

The project is designed so host secrets do not enter the VM.

Do not copy host credentials such as:

- `.env` files;
- API keys from the host environment;
- cloud credentials;
- GitHub or GitLab tokens;
- `~/.ssh`;
- `~/.config/gh`;
- `~/.aws`;
- `~/.docker/config.json`;
- keychain data;
- `ssh-agent` sockets.

If secret access inside the VM is ever needed, it should be designed as a separate, explicit feature.

## Updates

There are three separate update layers:

1. the host CLI binary, `pix`;
2. the base Ubuntu LTS image used for new or reset VMs;
3. Pi inside the VM.

Updating the CLI must not reset, replace, or migrate the existing VM implicitly.

The base image is selected by the code. Existing VMs are not automatically rebuilt when the CLI changes. A new base image only affects newly created or explicitly reset VMs.

## Pi integration

When Pi is launched through `pix`, the CLI:

- installs Pi inside the VM if it is not already available;
- synchronizes selected local Pi customizations into the VM;
- injects a Pi extension that reminds the agent to work only inside the VM and to publish the final result with Git to `origin` on the `pi-result` branch.

In practice, Pi treats the VM worktree as the only editable copy of the project.

## Non-goals

The initial design intentionally does not include:

- API key brokering;
- untracked-file sync as a long-term guarantee;
- `.env` management inside the VM;
- automatic test execution with host secrets;
- isolation between repositories inside the same VM;
- snapshots;
- CLI-level rollback;
- one VM per repository;
- Docker as a required backend;
- native Windows support outside WSL2.

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

- `internal/pix/app.go`: CLI commands and main flows;
- `internal/pix/config.go`: host state and repository metadata;
- `internal/pix/git.go`: repository registration and host/VM sync;
- `internal/pix/vm.go`: VM provisioning, SSH, images, and common backend logic;
- `internal/pix/vm_darwin.go`: macOS backend;
- `internal/pix/vm_wsl_linux.go`: WSL2 backend;
- `internal/pix/pi_local.go`: Pi integration and VM-specific context.
