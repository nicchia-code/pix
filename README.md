# pix

`pix` e una CLI in Go che esegue Pi dentro una VM Linux persistente e isolata dall'host.

## In breve

`pix` serve a dare a Pi un posto dove lavorare con liberta, senza dargli accesso diretto alla macchina su cui stai davvero sviluppando.

L'idea e semplice: invece di far lavorare Pi nel repository host, `pix` copia il progetto dentro una VM, lascia che Pi operi li dentro e riporta fuori solo i risultati tramite Git. In questo modo il progetto resta utilizzabile come una normale CLI, ma il confine di sicurezza e molto piu chiaro.

Se dovessi riassumerlo in una frase: `pix` prova a rendere comodo un workflow in cui Pi puo fare molto dentro una macchina sacrificabile, ma puo toccare molto meno dell'host.

## Le scelte che contano

Per spiegare come funziona `pix` senza trasformare il README in un diario di implementazione, conviene guardare le decisioni chiave.

### Il repository host non viene montato nella VM

Problema: se Pi lavora direttamente sul filesystem host, un errore o un comando troppo aggressivo puo toccare file, configurazioni e secret che non fanno parte del progetto.

Scelta: `pix` non monta il repo host nella VM. Copia invece il repository dentro un worktree interno alla VM e tratta quella copia come unica area modificabile da Pi.

Effetto: la superficie di rischio si riduce molto, perche tra host e VM non c'e scrittura diretta sul progetto reale.

### Le modifiche tornano indietro tramite Git

Problema: anche se la VM e isolata, serve comunque un modo affidabile per riportare fuori il lavoro svolto da Pi.

Scelta: dentro la VM `pix` mantiene un bare repository di bridge. Pi salva li i commit prodotti nella sessione e l'host li importa con `pix sync`.

Effetto: il passaggio VM -> host resta esplicito, ispezionabile e coerente con strumenti che gli sviluppatori conoscono gia.

### La VM e persistente, ma sacrificabile

Problema: ricreare ogni volta l'ambiente completo sarebbe lento; tenere tutto sull'host sarebbe comodo ma troppo permissivo.

Scelta: `pix` usa una VM persistente per macchina e utente, dentro cui vivono toolchain, cache e copie dei repository registrati.

Effetto: le sessioni restano rapide da riprendere, ma se qualcosa si rompe si puo sempre azzerare tutto con `pix vm reset --yes` senza toccare i repository host.

### Pi gira con molta liberta, ma nel posto giusto

Problema: limitare troppo Pi lo rende poco utile; lasciarlo libero sull'host lo rende troppo rischioso.

Scelta: Pi gira come `root` dentro la VM, con accesso internet e con un contesto preparato dalla CLI, ma senza mount del filesystem host, senza `~/.ssh` host e senza `ssh-agent` host.

Effetto: `pix` sposta la domanda da "come blocco ogni singolo comando?" a "in quale ambiente gli permetto di eseguirli?".

## Flusso tipico

```bash
pix install
pix sync --from-host
pix new
pix sync
```

In pratica:

1. `pix install` prepara stato locale, immagine e accesso alla VM;
2. `pix sync --from-host` copia il repository host nella VM;
3. `pix new` avvia Pi nel worktree VM;
4. `pix sync` importa nell'host i commit prodotti dentro la VM.

## Cosa promette il progetto

`pix` prova a garantire queste proprieta:

- una sola VM persistente per macchina/utente;
- Pi gira come `root` dentro la VM;
- la VM ha internet libero;
- la VM non monta il filesystem host;
- la VM non riceve secret host, `~/.ssh` host o `ssh-agent` host;
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
