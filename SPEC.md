# SPEC.md — pibox / Pi VM Runner

## 1. Obiettivo

`pibox` è una CLI che permette di eseguire Pi, un coding agent minimalista, dentro una VM Linux persistente e isolata dall'host.

L'obiettivo è dare all'utente un'esperienza semplice:

```bash
pibox init
pibox init repo
pibox sync --from-host
pibox run
pibox pull
```

Pi lavora come `root` dentro la VM, può installare toolchain e modificare il sistema guest, ma non deve poter leggere o modificare direttamente il filesystem host.

La VM è persistente, globale per macchina/utente, e condivisa tra tutti i repo registrati.

---

## 2. Requisiti di piattaforma

Supportare solo ambienti UNIX-like:

- macOS
- Linux
- Windows tramite WSL2

Non è richiesto supporto per Windows nativo, PowerShell o CMD come ambiente primario.

Su Windows, WSL2 è considerato substrate UNIX, non sandbox di sicurezza.

---

## 3. Threat model

Pi non è considerato intenzionalmente malevolo, ma può sbagliare.

Esempi di errori accettabili dentro la VM:

```bash
rm -rf /
apt install ...
modifica /etc
rompe un worktree
rompe un bridge.git
rompe toolchain installate
```

Questi errori devono rimanere confinati nella VM.

### Da impedire

Pi non deve poter:

- leggere la home dell'host;
- montare directory host;
- leggere `.env` host;
- leggere API key host;
- accedere a `~/.ssh` host;
- usare `ssh-agent` host;
- modificare direttamente file host;
- pushare verso un Git remoto host;
- dipendere da secret host per funzionare.

### Accettato

Pi può:

- avere accesso root dentro la VM;
- avere internet libero dentro la VM;
- modificare qualunque file dentro la VM;
- installare SDK, package manager, toolchain, cache;
- modificare i repo copiati dentro la VM;
- committare e pushare verso il bare repo interno alla VM.

---

## 4. Modello architetturale

### 4.1 VM globale

Esiste una sola VM persistente per utente/macchina.

```text
HOST
  pibox
  repo-a/
  repo-b/
  repo-c/

VM globale persistente
  Linux completo
  Pi root
  internet libero
  toolchain/cache condivise
  repo registrati
```

La VM è persistente per evitare reinstallazioni continue di toolchain pesanti, ad esempio Flutter, Android SDK, Node, Python, Gradle, Cargo, Go modules, ecc.

### 4.2 Repo multipli nella stessa VM

Tutti i repo registrati vivono dentro la stessa VM:

```text
/var/lib/pibox/
  repos/
    <repo_id>/
      worktree/
      bridge.git/
```

Non viene promesso isolamento tra repo dentro la VM.

La promessa di sicurezza è:

```text
la VM è isolata dall'host
```

non:

```text
i repo dentro la VM sono isolati tra loro
```

Se Pi rompe un repo dentro la VM, l'utente può risincronizzarlo dall'host con:

```bash
pibox sync --from-host
```

Se Pi rompe tutta la VM, l'utente può ricrearla con:

```bash
pibox vm reset
```

---

## 5. Backend VM per piattaforma

L'interfaccia verso la VM deve essere uniforme: SSH locale gestito dalla CLI.

La CLI deve nascondere il backend specifico della piattaforma.

### 5.1 macOS

Backend consigliato:

- Apple Virtualization.framework
- Linux guest LTS
- disco persistente globale
- SSH locale guest

Non richiedere Docker.

### 5.2 Linux

Backend consigliato:

- QEMU/KVM se disponibile
- fallback QEMU software se KVM non è disponibile o non è accessibile
- disco persistente globale
- SSH locale guest

Non richiedere setup manuale di Docker.

### 5.3 Windows / WSL2

Backend consigliato:

- usare WSL2 come ambiente UNIX di partenza;
- non trattare la distro WSL dell'utente come sandbox;
- creare/importare una distro/appliance dedicata se si usa WSL come backend VM-like;
- disabilitare automount e interop dove possibile.

Nota: dato che la VM è comunque considerata l'ambiente sacrificabile, il requisito primario rimane non montare filesystem host e non passare secret host dentro Pi.

---

## 6. SSH locale

La VM deve esporre SSH solo localmente.

```text
host 127.0.0.1:<porta_random> -> guest:22
```

La porta può cambiare tra run. L'utente non deve conoscerla.

La CLI è responsabile di:

- avviare la VM se spenta;
- assicurare che SSH sia raggiungibile;
- conoscere la porta locale;
- eseguire comandi nella VM via SSH;
- usare una chiave gestita dalla CLI.

### 6.1 Regole SSH

Non usare:

- private key dell'utente dentro la VM;
- ssh-agent forwarding;
- mount di `~/.ssh`;
- password login;
- root password login;
- esposizione su `0.0.0.0`.

Configurazione guest raccomandata:

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

Pi gira come root nella VM.

---

## 7. Stato su host

### 7.1 Stato globale

La CLI mantiene stato globale sotto:

```text
~/.pibox/
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

Nomi e formato specifici possono cambiare, ma devono rispettare questi concetti:

- immagine base LTS;
- disco VM persistente;
- stato runtime;
- chiavi SSH gestite dalla CLI;
- log diagnostici.

### 7.2 Stato repo-local non versionato

Ogni repo registrato deve avere metadata locali non versionati dentro `.git`.

Percorso consigliato:

```text
.git/pibox/config.json
```

Questo file non deve stare nel working tree e non deve essere committato.

Esempio:

```json
{
  "schema_version": 1,
  "repo_id": "app-frontend-a1b2c3",
  "vm_repo_path": "/var/lib/pibox/repos/app-frontend-a1b2c3",
  "worktree_path": "/var/lib/pibox/repos/app-frontend-a1b2c3/worktree",
  "bridge_path": "/var/lib/pibox/repos/app-frontend-a1b2c3/bridge.git",
  "default_branch": "main"
}
```

Motivo: quando l'utente esegue un comando da un repo host, la CLI deve sapere quale repo VM corrisponde a quel clone locale.

Non serve un file versionato tipo `pibox.yaml` nella v1.

---

## 8. Stato dentro VM

La VM contiene:

```text
/var/lib/pibox/
  repos/
    <repo_id>/
      worktree/
      bridge.git/
```

Per ogni repo:

- `worktree/` è la copia lavorabile del repo dentro la VM;
- `bridge.git/` è un bare repo Git usato come ponte verso l'host.

Dentro `worktree/`, il remote `origin` deve puntare solo al bare repo interno:

```bash
origin = /var/lib/pibox/repos/<repo_id>/bridge.git
```

Non deve puntare a:

- host filesystem;
- path montati dall'host;
- GitHub/GitLab origin dell'utente;
- SSH host;
- remote esterni.

---

## 9. Comandi CLI

## 9.1 `pibox init`

Crea o verifica la VM globale.

```bash
pibox init
```

Responsabilità:

1. rilevare OS e architettura;
2. scegliere backend VM;
3. scaricare/verificare immagine base LTS se mancante;
4. creare disco VM persistente se mancante;
5. configurare SSH guest;
6. assicurarsi che la VM possa essere avviata;
7. non toccare repo host.

Il comando deve essere idempotente.

Eseguire più volte `pibox init` deve essere sicuro.

---

## 9.2 `pibox init repo`

Registra il repo Git corrente nella VM.

```bash
cd /path/al/repo
pibox init repo
```

Precondizioni:

- deve essere eseguito dentro un repo Git host;
- se non esiste `.git`, la CLI può suggerire `git init`, ma non deve necessariamente farlo automaticamente.

Responsabilità:

1. trovare la root Git host;
2. creare un `repo_id` stabile;
3. creare metadata in `.git/pibox/config.json`;
4. avviare/verificare la VM;
5. creare dentro VM:

```text
/var/lib/pibox/repos/<repo_id>/worktree/
/var/lib/pibox/repos/<repo_id>/bridge.git/
```

6. inizializzare `bridge.git` come bare repo;
7. preparare `worktree` come repo Git;
8. configurare `origin` del worktree verso `bridge.git`.

Questo comando non deve necessariamente copiare i file del repo.

Per importare i contenuti host nella VM si usa:

```bash
pibox sync --from-host
```

---

## 9.3 `pibox sync --from-host`

Copia lo stato del repo host dentro la VM.

```bash
pibox sync --from-host
```

Semantica:

```text
host è la source of truth
VM viene sovrascritta
```

Questa operazione è distruttiva lato VM.

### 9.3.1 Cosa copia

Copia i file tracked da Git.

Non copia:

- file untracked;
- file ignorati;
- `.env`;
- `node_modules`;
- build artifacts;
- cache locali;
- secret non tracciati.

Implementazione consigliata:

```bash
git archive HEAD
```

oppure equivalente che esporti i tracked files dello stato Git corrente.

Nota: se si vuole includere modifiche tracked non committate, bisogna decidere una strategia esplicita. La v1 può richiedere working tree pulito, oppure esportare index/working tree con logica dedicata. Decisione consigliata per v1: richiedere working tree host pulito.

### 9.3.2 Warning distruttivo

Se la VM contiene modifiche non esportate, `sync --from-host` deve avvisare.

Esempio:

```text
Questo comando sovrascriverà la copia del repo dentro la VM.

Eventuali modifiche presenti in:
  /var/lib/pibox/repos/<repo_id>/worktree

andranno perse se non sono già state portate fuori.

Usa:
  pibox sync --from-host --force

per continuare.
```

`--force` esegue comunque la risincronizzazione.

---

## 9.4 `pibox run`

Lancia Pi dentro la VM sul repo corrente.

```bash
pibox run
```

Responsabilità:

1. trovare la root Git host;
2. leggere `.git/pibox/config.json`;
3. avviare la VM se spenta;
4. assicurare SSH locale;
5. eseguire Pi come root dentro:

```text
/var/lib/pibox/repos/<repo_id>/worktree
```

La VM ha internet libero.

La VM non riceve:

- `.env` host;
- API key host;
- secret host;
- ssh-agent host;
- `~/.ssh` host.

### 9.4.1 Test

I test reali si eseguono sull'host dopo `pibox pull`.

La VM può essere usata da Pi per installare tool, analizzare codice, modificare file e preparare commit, ma non deve contenere gli `.env` necessari ai test reali.

---

## 9.5 `pibox pull`

Porta le modifiche prodotte da Pi dalla VM al repo host.

```bash
pibox pull
```

Responsabilità:

1. trovare repo host corrente;
2. leggere metadata repo-local;
3. avviare/verificare la VM;
4. individuare `bridge.git` del repo corrente;
5. eseguire sull'host solo un `git pull` dal bridge Git dentro la VM.

Forma concettuale:

```bash
git pull ssh://root@127.0.0.1:<port>/var/lib/pibox/repos/<repo_id>/bridge.git <branch>
```

La CLI nasconde `<port>`, `<repo_id>` e `<branch>`.

### 9.5.1 Cosa NON deve fare

`pibox pull` non deve:

- fare `git add` nella VM;
- fare `git commit` nella VM;
- fare `git push` nella VM;
- modificare automaticamente il worktree VM;
- decidere commit message;
- creare commit al posto di Pi.

Pi deve committare e pushare da solo verso `bridge.git`.

Se Pi non ha committato o pushato nulla, `pibox pull` non porta modifiche.

### 9.5.2 Contratto per Pi

Pi deve sapere che, alla fine del lavoro, deve fare:

```bash
cd /var/lib/pibox/repos/<repo_id>/worktree
git add -A
git commit -m "<messaggio>"
git push origin <branch>
```

Dove:

```bash
origin = /var/lib/pibox/repos/<repo_id>/bridge.git
```

Branch consigliato per v1:

```text
main
```

oppure un branch fisso gestito dalla CLI, ad esempio:

```text
pi-result
```

La scelta deve essere coerente tra `run` e `pull`.

Raccomandazione v1: usare `pi-result` per evitare ambiguità con il branch host.

---

## 9.6 `pibox vm reset`

Reset completo della VM globale.

```bash
pibox vm reset
```

Questo comando è distruttivo.

Deve:

1. spegnere la VM;
2. eliminare il disco VM globale;
3. eliminare toolchain, cache, SDK, repo VM-side, bridge Git, worktree;
4. ricreare una VM pulita dall'immagine LTS corrente.

Deve mostrare un avviso esplicito.

Esempio:

```text
ATTENZIONE: questo eliminerà tutta la VM pibox.

Verranno eliminati:
- toolchain installate
- cache
- SDK
- tutti i worktree dentro la VM
- tutti i bridge.git dentro la VM
- configurazioni modificate dentro il guest

Non verranno toccati:
- repo host
- file host
- .env host
- chiavi SSH host

Per continuare:
  pibox vm reset --yes
```

`vm reset` non deve mai essere eseguito implicitamente da update o init.

---

## 9.7 `pibox image update`

Aggiorna l'immagine base LTS usata per future VM/reset.

```bash
pibox image update
```

Questo comando non modifica la VM esistente.

Serve solo a scaricare una nuova immagine base che verrà usata da:

```bash
pibox vm reset
```

o da nuove installazioni.

Regola fondamentale:

```text
aggiornare la CLI non aggiorna la VM
aggiornare l'immagine non aggiorna la VM esistente
resettare la VM è sempre esplicito
```

---

## 10. Aggiornamenti

Ci sono tre livelli distinti.

### 10.1 CLI host

Il binario `pibox`.

Può essere aggiornato tramite package manager o installer.

Aggiornare la CLI non deve cancellare né modificare la VM persistente.

### 10.2 Immagine base LTS

Template usato per creare o resettare la VM.

Si aggiorna solo esplicitamente con:

```bash
pibox image update
```

Non modifica VM già esistenti.

### 10.3 Pi dentro la VM

Pi è il payload eseguito dentro la VM.

Opzioni possibili:

- Pi è incluso nell'immagine base;
- Pi viene aggiornato dalla CLI durante `run`;
- Pi viene aggiornato con comando esplicito.

Raccomandazione v1: includere Pi nell'immagine base LTS e permettere upgrade esplicito futuro. Non introdurre update automatici nella v1.

---

## 11. Git bridge

### 11.1 Obiettivo

Git è il ponte controllato tra VM e host.

Pi non scrive mai direttamente sul repo host.

Pi scrive nel worktree VM, committa e pusha nel bare repo interno alla VM.

L'host importa con `pibox pull`, che esegue un `git pull` dal bare repo VM.

### 11.2 Flusso host -> VM

```bash
pibox sync --from-host
```

Concettualmente:

```text
host tracked files
  -> VM worktree
  -> VM bridge.git inizializzato/configurato
```

### 11.3 Flusso VM -> host

Pi dentro VM:

```bash
cd /var/lib/pibox/repos/<repo_id>/worktree
git add -A
git commit -m "..."
git push origin pi-result
```

Host:

```bash
pibox pull
```

Concettualmente:

```bash
git pull ssh://root@127.0.0.1:<port>/var/lib/pibox/repos/<repo_id>/bridge.git pi-result
```

### 11.4 Rollback

La CLI non implementa rollback.

Se il pull porta modifiche sbagliate nel repo host, l'utente usa Git normale:

```bash
git reset --hard HEAD~1
git reflog
git reset --hard <sha>
```

La CLI non deve reinventare Git.

---

## 12. Secrets

La VM non riceve secret host.

Non copiare:

- `.env`;
- `.env.*` non tracciati;
- API key da environment host;
- credenziali cloud;
- token GitHub/GitLab;
- `~/.ssh`;
- `~/.config/gh`;
- `~/.aws`;
- `~/.docker/config.json`;
- keychain host;
- ssh-agent socket.

Se in futuro servisse usare secret dentro VM, deve essere un design separato ed esplicito. Non in v1.

---

## 13. Network

La VM ha internet libero.

Motivazione:

- Pi deve poter installare toolchain;
- Pi deve poter scaricare dipendenze;
- Pi deve poter eseguire package manager;
- Pi non possiede secret host, quindi non può usare account privati dell'utente.

Limitazioni:

- internet libero non è una sandbox di rete forte;
- se Pi scarica software malevolo, il danno resta dentro la VM;
- l'host non deve essere montato o esposto.

---

## 14. File sync

### 14.1 Tracked only

`sync --from-host` importa solo file tracked da Git.

Questo evita di copiare:

- `.env`;
- build output;
- dependency folders;
- file locali;
- secret accidentali;
- cache.

### 14.2 Working tree pulito

Decisione consigliata per v1:

`sync --from-host` richiede working tree host pulito.

Se il working tree non è pulito, mostrare:

```text
Il repo host ha modifiche non committate.

Per sincronizzare nella VM, committa prima le modifiche oppure usa una futura modalità esplicita per includere working tree non committato.

Operazione annullata.
```

Motivazione: evitare ambiguità tra HEAD, index e working tree.

---

## 15. Repo ID

`repo_id` deve essere:

- stabile per quel clone locale;
- leggibile abbastanza da diagnosticare;
- unico.

Formato consigliato:

```text
<basename>-<short-random-or-hash>
```

Esempio:

```text
app-frontend-a1b2c3
```

Il mapping vive in:

```text
.git/pibox/config.json
```

Non dedurre il mapping solo dal path host, perché il repo può essere spostato.

---

## 16. Idempotenza

Tutti i comandi devono essere idempotenti dove possibile.

### `init`

Se VM esiste, verifica e non ricrea.

### `init repo`

Se repo già registrato, verifica metadata e path VM.

### `sync --from-host`

Può essere ripetuto. Sovrascrive VM secondo lo stato host.

### `run`

Può essere ripetuto. Avvia VM se serve.

### `pull`

Può essere ripetuto. Delega semantica a Git.

### `vm reset`

Non è idempotente in senso non distruttivo. Richiede `--yes`.

---

## 17. Error handling

### 17.1 VM non avviabile

Mostrare errore chiaro e suggerire:

```bash
pibox vm reset
```

solo come opzione esplicita.

### 17.2 Repo non registrato

Se l'utente esegue `run`, `pull` o `sync` in un repo non registrato:

```text
Questo repo non è registrato con pibox.

Esegui:
  pibox init repo
```

### 17.3 VM worktree mancante

Se metadata host esistono ma il worktree VM non esiste:

```text
La copia VM di questo repo non esiste o è stata eliminata.

Esegui:
  pibox sync --from-host
```

### 17.4 Bridge Git mancante

Se `bridge.git` manca:

- tentare repair se sicuro;
- altrimenti chiedere `sync --from-host`.

### 17.5 Pull senza commit Pi

Se `pibox pull` non trova modifiche/branch remoto:

```text
Nessun risultato da importare dalla VM.

Pi potrebbe non aver ancora committato/pushato nel bridge Git.
```

---

## 18. UX prevista

### Primo setup

```bash
pibox init
```

### Import progetto esistente

```bash
cd ~/code/my-app
pibox init repo
pibox sync --from-host
```

### Usare Pi

```bash
pibox run
```

### Importare modifiche da Pi

```bash
pibox pull
```

### Testare

```bash
npm test
# oppure
flutter test
# oppure qualunque comando host
```

### Se Pi rompe la copia VM

```bash
pibox sync --from-host --force
```

### Se Pi rompe tutta la VM

```bash
pibox vm reset --yes
pibox sync --from-host
```

---

## 19. Non-goals v1

Non implementare nella v1:

- broker API key;
- sync di file untracked;
- gestione `.env` dentro VM;
- test automatici nella VM con secret host;
- isolamento tra repo dentro la VM;
- `git pull pi` come comando supportato;
- snapshot VM;
- rollback CLI;
- multi-VM per repo;
- Docker come requisito obbligatorio;
- supporto Windows nativo fuori da WSL2.

---

## 20. Decisioni v1

- Nome CLI: `pibox`.
- Linguaggio implementazione: Go.
- Distro base v1: Ubuntu LTS server/cloud image headless, senza desktop.
- Disco VM v1: `qcow2` dinamico/sparse con dimensione virtuale predefinita `40G`.
- Branch bridge ufficiale: `pi-result`.
- `sync --from-host` richiede working tree host pulito.
- Pi viene installato al provisioning della VM con:
  `curl -fsSL https://pi.dev/install.sh | sh`

---

## 21. Invarianti fondamentali

Queste regole non devono essere violate:

```text
1. Una sola VM persistente per utente/macchina.

2. Pi gira come root dentro la VM.

3. La VM ha internet libero.

4. La VM non monta filesystem host.

5. La VM non riceve secret host.

6. Ogni repo registrato ha:
   /var/lib/pibox/repos/<repo_id>/worktree
   /var/lib/pibox/repos/<repo_id>/bridge.git

7. Pi pusha solo verso il bridge Git interno alla VM.

8. pibox pull esegue solo git pull dal bridge Git VM verso host.

9. pibox sync --from-host è distruttivo lato VM e copia solo tracked files.

10. pibox vm reset distrugge e ricrea tutta la VM, con avviso esplicito.

11. Rollback del repo host è responsabilità di Git, non della CLI.

12. Aggiornare la CLI non resetta né modifica implicitamente la VM.
```
