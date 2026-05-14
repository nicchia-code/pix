# README_AGENTS

Questo file spiega `pix` a un agent che deve leggere o modificare il repository.

## Scopo del progetto

`pix` e una CLI che esegue Pi dentro una VM Linux persistente e isolata dall'host.

Il modello mentale corretto e questo:

- l'host non e il luogo in cui Pi lavora;
- la VM e l'ambiente di lavoro sacrificabile;
- il repo host e una sorgente/destinazione di sync;
- Git e il ponte controllato tra host e VM.

## Invarianti da non rompere

Quando modifichi il codice, preserva queste regole:

- esiste una sola VM persistente per utente/macchina;
- Pi gira come `root` dentro la VM;
- la VM ha internet libero;
- la VM non deve montare filesystem host;
- la VM non deve ricevere secret host, `~/.ssh` host o `ssh-agent` host;
- ogni repo registrato ha un `worktree/` e un `bridge.git/` dentro `/var/lib/pix/repos/<repo_id>/`;
- Pi non deve scrivere direttamente sul repo host;
- `pix sync` senza flag importa dal bridge Git VM verso host;
- `pix vm reset --yes` e sempre esplicito e distruttivo;
- aggiornare la CLI non deve resettare o migrare implicitamente la VM esistente.

Se una modifica semplifica il codice ma indebolisce questi invarianti, va trattata come sospetta.

## Mappa del codice

Entry point:

- `cmd/pix/main.go`: passa gli argomenti a `internal/cli`.
- `internal/cli/cli.go`: converte errori applicativi in exit code CLI.

Core applicativo:

- `internal/pix/app.go`: routing dei comandi (`install`, `sync`, `new`, `resume`, `ssh`, `vm reset`).
- `internal/pix/config.go`: stato host globale e config repo locale in `.git/pix/config.json`.
- `internal/pix/git.go`: rilevamento repo Git, `repo_id`, export file verso la VM.
- `internal/pix/vm.go`: lifecycle VM, SSH, download immagini, QEMU comune.
- `internal/pix/vm_darwin.go`: backend macOS tramite `Virtualization.framework`.
- `internal/pix/vm_wsl_linux.go`: appliance WSL2 dedicata.
- `internal/pix/pi_local.go`: sync di estensioni/settings locali di Pi e injection del contesto `pix`.
- `internal/pix/exec.go`: esecuzione comandi locali/interattivi.
- `internal/pix/errors.go`: errori user-facing con exit code.

Supporto:

- `internal/pix/cloudinit_iso.go`: genera la seed ISO NoCloud per cloud-init.
- `internal/pix/image_convert.go`: conversione qcow2 -> raw per il backend macOS.

## Flussi fondamentali

### 1. Install

`pix install`:

- inizializza `~/.pix/`;
- genera una chiave SSH dedicata;
- sceglie il backend in base alla piattaforma;
- scarica l'immagine base Ubuntu LTS se manca;
- crea o verifica la VM persistente;
- attende che SSH sia raggiungibile;
- sincronizza customizzazioni locali di Pi nella VM.

### 2. Sync host -> VM

`pix sync --from-host`:

- trova la root Git host;
- crea `.git/pix/config.json` se il repo non e registrato;
- assicura esistenza di `worktree` e `bridge.git` nella VM;
- opzionalmente blocca se il worktree VM e dirty e non c'e `--force`;
- aggiorna il bridge Git e il worktree VM.

Nota importante sul comportamento reale: l'implementazione corrente non e strettamente limited a stato committed/tracked. `gitWorktreeTar()` include anche file untracked non ignorati e modifiche tracked non committate. Se cambi questa parte, confronta sempre il comportamento con `SPEC.md`.

### 3. Esecuzione di Pi

`pix new` e `pix resume`:

- caricano la config del repo corrente;
- portano la VM in stato pronto;
- sincronizzano estensioni/settings locali di Pi nella VM;
- installano Pi nella VM se necessario;
- eseguono `pi` dentro il `worktree` VM.

Il branch bridge ufficiale e `pi-result`.

### 4. Sync VM -> host

`pix sync`:

- richiede worktree host pulito;
- verifica che il bridge abbia il branch atteso;
- fa `fetch` dal bridge Git via SSH;
- fa `merge --no-edit` di `FETCH_HEAD`.

Il codice non esegue commit automatici nella VM. Pi deve committare e pushare da solo dentro il guest.

## Stato persistente

Host globale:

```text
~/.pix/
  images/
  vm/
    default/
      state.json
      ssh/
  logs/
```

Repo host registrato:

```text
.git/pix/config.json
```

Repo nella VM:

```text
/var/lib/pix/repos/<repo_id>/
  worktree/
  bridge.git/
```

## Sicurezza: cosa tenere a mente

Il progetto non cerca di confinare Pi all'interno del guest. Pi puo essere `root`, installare pacchetti, cambiare `/etc`, rompere toolchain o rompere il worktree VM. Questo e accettato.

La barriera vera e tra guest e host. Per questo sono sensibili soprattutto le modifiche che toccano:

- SSH e host key handling;
- path host/guest;
- mount, automount, sharing di directory;
- passaggio di environment, secret o agent socket;
- remoti Git che puntano fuori dal bridge interno.

## Divergenze note tra spec e implementazione

La divergenza principale oggi e questa:

- `SPEC.md` raccomanda per la v1 una sync host -> VM basata su working tree host pulito e stato committed/tracked;
- il codice attuale avvisa se l'host e dirty, ma continua;
- il codice attuale copia anche file untracked non ignorati.

Se stai scrivendo documentazione o modificando il comportamento, non assumere che la spec descriva sempre il comportamento reale corrente.

## Come ragionare prima di cambiare qualcosa

Se devi cambiare `pix`, chiediti:

1. La modifica cambia il boundary tra host e VM?
2. Introduce accesso a filesystem host, secret host o credenziali host?
3. Cambia la semantica di `pix sync` o `pix sync --from-host`?
4. Rende meno esplicito un comportamento distruttivo?
5. Cambia il backend scelto o il formato di stato persistente?

Se la risposta e si, la modifica va trattata come architetturale, non come semplice refactor.

## Sequenza di lettura consigliata per un agent

Per capire rapidamente il progetto, leggi in quest'ordine:

1. `SPEC.md`
2. `internal/pix/app.go`
3. `internal/pix/config.go`
4. `internal/pix/git.go`
5. `internal/pix/vm.go`
6. il backend rilevante per piattaforma: `internal/pix/vm_darwin.go` oppure `internal/pix/vm_wsl_linux.go`
7. `internal/pix/pi_local.go`

## Build e test

Comandi principali:

```bash
make build
make test
```

In questo ambiente i test non sono stati eseguiti perche `go` non era disponibile nel `PATH`. Se devi validare modifiche, verifica prima la presenza della toolchain Go.
