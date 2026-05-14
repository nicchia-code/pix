# pix

`pix` e una CLI in Go che esegue Pi dentro una VM Linux persistente e isolata dall'host.

L'obiettivo del progetto e semplice: lasciare a Pi piena liberta dentro una macchina sacrificabile, ma impedire che possa leggere o modificare direttamente il filesystem e i secret dell'host.

Flusso tipico:

```bash
pix install
pix sync --from-host
pix new
pix sync
```

## Idea in 30 secondi

Con `pix` il repository non viene montato nella VM. Invece:

1. il repo host viene copiato nella VM;
2. Pi lavora dentro la copia VM come `root`;
3. Pi salva il risultato in un bare repo Git interno alla VM;
4. l'host importa i commit con `pix sync`.

Questo riduce il rischio che errori di Pi danneggino il sistema host.

## Cosa promette il progetto

`pix` prova a garantire queste proprieta:

- una sola VM persistente per macchina/utente;
- Pi gira come `root` dentro la VM;
- la VM ha internet libero;
- la VM non deve montare il filesystem host;
- la VM non deve ricevere secret host, `~/.ssh` host o `ssh-agent` host;
- il passaggio delle modifiche tra host e VM avviene tramite Git bridge, non tramite scrittura diretta sul repo host.

Importante: l'isolamento promesso e tra VM e host, non tra repository diversi dentro la stessa VM.

## Architettura

Ogni macchina ha una VM globale persistente. Dentro la VM vivono tutti i repository registrati:

```text
/var/lib/pix/
  repos/
    <repo_id>/
      worktree/
      bridge.git/
```

Sul lato host `pix` mantiene stato in `~/.pix/`, per esempio:

```text
~/.pix/
  images/
  vm/
    default/
      state.json
      ssh/
  logs/
```

Per ogni clone locale registrato, `pix` salva metadata non versionati in:

```text
.git/pix/config.json
```

## Comandi principali

### `pix install`

Inizializza lo stato host, prepara il backend VM corretto per la piattaforma, genera la chiave SSH gestita da `pix`, scarica l'immagine base Ubuntu LTS se necessario e verifica che la VM sia raggiungibile.

### `pix sync --from-host`

Registra il repo corrente se necessario e sincronizza il progetto host dentro la VM.

Oggi l'implementazione copia nella VM i file Git tracked e anche i file untracked non ignorati presenti nel working tree host. Questo e piu permissivo della direzione raccomandata nella spec, che punta a un flusso piu stretto basato solo su stato committed/tracked.

Se il worktree VM contiene modifiche non esportate, `pix` chiede `--force` prima di sovrascriverlo.

### `pix new`

Avvia una nuova sessione Pi dentro il worktree VM del repo corrente.

### `pix resume` oppure `pix`

Riprende una sessione Pi esistente dentro la VM.

### `pix sync`

Importa nell'host i commit prodotti da Pi nel bridge Git interno alla VM.

Il repo host deve essere pulito prima della sync VM -> host.

### `pix ssh`

Apre una shell root dentro il worktree VM del repo corrente.

### `pix vm reset --yes`

Distrugge e ricrea completamente la VM globale `pix`. Non tocca i repository host, ma elimina toolchain, cache e copie VM dei repo.

## Backend supportati

`pix` sceglie il backend in base alla piattaforma:

- macOS: Apple `Virtualization.framework`;
- Linux: QEMU, con KVM se disponibile;
- WSL2: appliance/distro dedicata con hardening specifico.

L'interfaccia verso la VM rimane uniforme: SSH locale gestito dalla CLI.

## Integrazione con Pi

Quando lanci Pi tramite `pix`, la CLI:

- installa Pi nella VM se non e presente;
- sincronizza alcune customizzazioni locali di Pi nella VM;
- inietta un'estensione che ricorda a Pi di lavorare solo dentro la VM e di pubblicare il risultato con Git verso `origin` sul branch `pi-result`.

In pratica Pi tratta il worktree VM come unica copia modificabile del progetto.

## Build e test

Build locale:

```bash
make build
```

Test:

```bash
make test
```

Serve una toolchain Go disponibile nel `PATH`.

## Stato del progetto

Il repository e gia una buona implementazione della visione descritta in `SPEC.md`, ma ci sono ancora alcuni dettagli da allineare o chiarire. Il punto piu importante oggi e che `pix sync --from-host` e piu permissivo della semantica raccomandata dalla spec.

## Codice da leggere per primo

Se vuoi orientarti rapidamente:

- `SPEC.md`: obiettivi, threat model e invarianti;
- `internal/pix/app.go`: comandi CLI e flussi principali;
- `internal/pix/vm.go`: provisioning VM, SSH, immagini, backend comuni;
- `internal/pix/vm_darwin.go`: backend macOS;
- `internal/pix/vm_wsl_linux.go`: backend WSL2;
- `internal/pix/git.go`: registrazione repo e sincronizzazione host/VM;
- `internal/pix/pi_local.go`: integrazione e prompt/context di Pi.
