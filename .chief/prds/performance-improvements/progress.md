# Progress: performance-improvements

## Codebase Patterns
- **Config-Seam:** neue Optionen in `internal/config/config.go` als Feld am
  passenden Sub-Struct (`ReviewConfig`, `ConsolidateConfig`), Tests am Seam
  `config.Load`/`config.Save` in `t.TempDir()` (Muster: `TestReviewConfigEnabled`,
  `TestConsolidateConfigRoundTrip`). Optionale Felder brauchen
  `yaml:"...,omitempty"`, sonst schreibt `Save` sie als leere Werte in die Datei.
- **Loop-Phasen:** Build/Review/Consolidate laufen alle über
  `runIterationWithRetry(ctx, mode)` → `runIteration` mit `iterationMode`
  (`modeBuild|modeReview|modeConsolidate`). Wer etwas *pro Phase* unterscheiden
  will, hängt es an diesen Mode, nicht an ein geteiltes Flag.
- **Provider-Erweiterungen:** `loop.Provider` ist das Minimum, das alle CLIs
  können. Fähigkeiten, die nur ein Provider hat, kommen als **optionales
  Interface** dazu (z. B. `loop.ModelSwitcher`) und werden per Type-Assertion
  abgefragt — dann sind sie für die anderen Provider automatisch wirkungslos
  statt ein Fehler.
- **Loop-Tests:** `mockProvider` in `internal/loop/loop_test.go` ist der Seam für
  alles, was den Agenten-Aufruf betrifft; ein Bash-Mock-Script (`echo` von
  stream-json + `git commit`) fährt echte Runs durch `l.Run(ctx)`. Ein Run mit
  offener Story + aktivem Consolidate durchläuft in einem Rutsch alle drei Phasen.
- **Config → Loop:** die Verdrahtung passiert in `Manager.Start`
  (`internal/loop/manager.go`, ~Zeile 250). Neue Config-Optionen müssen dort
  gesetzt werden, sonst kommen sie nie am Loop an — dieser Hop ist leicht zu
  vergessen und war vorher untested.
- **Pre-Run-Checks:** jeder Run-Start läuft durch `doStartLoop`
  (`internal/tui/loop_control.go`); der eigentliche Start ist `launchLoop`. Ein
  neuer Check kommt in `doStartLoop`, und der „User hat schon geantwortet"-Pfad
  ruft `launchLoop` direkt — sonst zieht sich der eigene Dialog endlos neu auf.
- **Plattform-/Umgebungsabhängiges testbar halten:** `runtime.GOOS` & Co. nicht
  in der Entscheidungsfunktion abfragen, sondern als Argument reinreichen
  (Muster: `sleepRisks(supported, power, keepAwake)`); die echte Antwort kommt
  über einen injizierbaren Funktions-Seam als Feld am `App`/Struct (Muster:
  `App.sleepCheck`, `awake.Guard.newCmd`). Nil-Seam = fail open, dann müssen
  bestehende Tests mit handgebauten Struct-Literalen nicht angefasst werden.
- **Docs-Pflicht:** jede Config-Option gehört in `docs/reference/configuration.md`
  an *zwei* Stellen: den YAML-Beispielblock **und** die Key-Tabelle der jeweiligen
  Sektion.

---

## 2026-07-27 - PERF-001: Review und Consolidate auf eigenem Modell

**Implementiert**
- `review.model` und `consolidate.model` in `.chief/config.yaml`; ohne Angabe
  laufen Review- und Consolidate-Agent auf `sonnet` (`defaultPhaseModel` in
  `internal/loop/reviewer.go`), der Build-Agent bleibt unangetastet.
- Modell-Weitergabe pro Agenten-Invocation über das neue optionale Interface
  `loop.ModelSwitcher`: `ClaudeProvider.WithModel` gibt eine **Kopie** des
  Providers mit gesetztem `--model` zurück, der Original-Provider (und damit der
  Build-Agent) bleibt unverändert. `runIteration` wählt via
  `providerForMode(mode)`.
- Nicht-Claude-Provider (codex, opencode, cursor, gemini) implementieren
  `ModelSwitcher` nicht → die neuen Keys sind dort wirkungslos, kein Fehler.
- `Manager.Start` reicht `cfg.Review.Model` / `cfg.Consolidate.Model` durch.

**Dateien**
- `internal/config/config.go` (+ `_test.go`): `Model` an `ReviewConfig` /
  `ConsolidateConfig`, `omitempty`.
- `internal/loop/provider.go`: `ModelSwitcher`.
- `internal/loop/reviewer.go` / `consolidator.go`: `model` + `effectiveModel()`,
  `defaultPhaseModel = "sonnet"`.
- `internal/loop/loop.go`: `SetReviewModel`, `SetConsolidateModel`,
  `providerForMode`, `modelForMode`; `runIteration` nutzt den Phasen-Provider.
- `internal/loop/manager.go`: Verdrahtung Config → Loop.
- `internal/agent/claude.go` (+ `_test.go`): `WithModel`.
- `internal/loop/loop_test.go`: `mockProvider` kann Modelle klonen + mitschreiben
  (`modelLog`).
- `internal/loop/phase_model_test.go` (neu), `internal/loop/manager_test.go`.
- `docs/reference/configuration.md`.

**Learnings for future iterations**
- `Save` schreibt jedes Feld ohne `omitempty` mit — `agent.model` steht deshalb
  auch als `model: ""` in der Datei. Ein Test, der prüft „diese Option steht
  *nicht* drin", darf nicht per `strings.Contains(data, "model:")` arbeiten,
  sondern muss das YAML in eine `map[string]map[string]any` laden und die
  konkrete Sektion prüfen.
- `Manager.GetInstance` liefert einen `snapshot()` **ohne** das `Loop`-Feld. Wer
  im Test an den echten Loop will, nimmt `m.lookup(name)` und liest
  `instance.Loop` unter `instance.mu` (race-detector-sauber).
- Der Consolidate-Pass läuft nur, wenn `git.CommitLogForStories` im Fenster
  `startRef..HEAD` Commits findet, die zu `feat: <prdName>/<ID> - <Title>`
  passen. Für einen End-to-End-Test heißt das: PRD unter
  `.chief/prds/<name>/prd.md` ablegen (daraus kommt der PRD-Name) und den
  Mock-Agenten genau diese Commit-Message schreiben lassen — sonst wird die
  Phase still übersprungen und der Test prüft nichts.
- Der Sonnet-Default wird bewusst im Loop (`effectiveModel()`) aufgelöst, nicht
  in `config.Default()`: so bleibt die gespeicherte Config leer und behauptet
  keine Modellwahl, die der User nie getroffen hat.
- Kollateral-Hinweis für PERF-004/005: `mockProvider` implementiert jetzt
  `WithModel`; ein Test, der einen Provider *ohne* Modell-Support braucht, kann
  `fixedModelProvider` aus `phase_model_test.go` benutzen (delegiert an den Mock,
  lässt `ModelSwitcher` aber weg).
- `gofmt -l .` meldet in diesem Repo vorbestehend acht Dateien in
  `internal/tui/` — nicht erschrecken, das sind keine eigenen Änderungen.

---
<!-- chief-timing story="PERF-001" duration_ms=649937 cost=21.614842 in=242 out=2259 cache_create=302361 cache_read=10515012 -->

## 2026-07-27 - PERF-002: Warnung vor dem Run, wenn der Mac einschlafen kann

**Implementiert**
- Neuer blockierender Dialog **„Mac May Fall Asleep Mid-Run"** vor jedem
  Run-Start (`ViewSleepWarning`, Stil/Interaktion der `BranchWarning`:
  ↑/↓ bzw. k/j, Enter, Esc; „Cancel" ist die vorgewählte sichere Option).
- Gründe (beide zusammen in *einem* Dialog): Batteriebetrieb (Clamshell-Sleep
  trotz `keepAwake`, Abhilfe Netzteil/Deckel offen) und `loop.keepAwake: false`.
- Batteriestatus über `pmset -g batt` im `awake`-Paket; fail open: kein
  ermittelbarer Status ⇒ kein Dialog, ebenso auf Nicht-macOS
  (`awake.Supported()` = `inhibitCmd() != nil`).
- `doStartLoop` ist jetzt „Pre-Run-Checks", der eigentliche Start heißt
  `launchLoop`. „Trotzdem starten" ruft `launchLoop` direkt — über
  `doStartLoop` würde der Dialog sich selbst neu aufziehen.
- Keine Persistenz: der Dialog erscheint bei jedem Start neu.

**Dateien**
- `internal/awake/power.go` (+ `_test.go`, neu): `PowerSource`,
  `CurrentPowerSource`, `Supported`, `parsePowerSource`.
- `internal/tui/sleep_warning.go` (+ `_test.go`, neu): pure `sleepRisks(...)`,
  `SleepWarning`-Komponente, `sleepWarningPreflight`, Key-Handler.
- `internal/tui/loop_control.go`: `doStartLoop` → Checks + `launchLoop`.
- `internal/tui/app.go`: Felder `sleepWarning`/`sleepCheck`, View-Dispatch,
  Key-Dispatch. `internal/tui/messages.go`: `ViewSleepWarning`.
- `docs/reference/configuration.md`, `docs/troubleshooting/common-issues.md`.

**Learnings for future iterations**
- **Start-Pfad:** *jeder* Run-Start läuft durch `doStartLoop`
  (`internal/tui/loop_control.go`) — Dashboard, Picker, `chief start`
  (autoStart), Branch-Dialog, Worktree-Spinner und der Branch-Sync-Resume.
  Wer einen Pre-Run-Check braucht, hängt ihn dort ein und lässt den
  „schon beantwortet"-Pfad an `launchLoop` gehen, sonst fragt der Dialog
  endlos nach.
- **Plattformabhängige Logik testbar halten:** nicht `runtime.GOOS` in der
  Entscheidungsfunktion abfragen, sondern als Argument reinreichen
  (`sleepRisks(supported, power, keepAwake)`) und die echte Antwort über einen
  injizierbaren Seam am `App` (`sleepCheck`) liefern. Sonst hängen die Tests
  am Betriebssystem des Entwicklers.
- **Nil-Seam = fail open:** `sleepCheck == nil` (handgebaute `App`-Literale in
  Tests, wie sie `branch_sync_test.go` benutzt) verhält sich wie „keine
  Warnung" — dadurch mussten bestehende TUI-Tests nicht angefasst werden.
- **`App`-Literale in Tests:** `launchLoop` schreibt in `a.storyTimings` &
  Co. (nil-Map-Panic), erreicht diese Zeilen aber nur, wenn `manager.Start`
  erfolgreich war. Ein `loop.NewManager(10, nil)` (Provider nil) bricht dort
  mit Fehler ab — praktisch, um „wurde der Start überhaupt versucht?" zu
  beobachten: `manager.GetInstance(prd) != nil` (Register läuft vor Start).
- **`previousViewMode` ist kein Stack:** das Help-Overlay (`?`) überschreibt es
  auch, während ein Modal offen ist. Beim Zurückkehren also prüfen, dass man
  nicht in das eben geschlossene Modal zurückspringt
  (`viewBehindSleepWarning`).
- **Settings-Hotkey ist `,`, nicht `s`** — Dialogtexte, die auf die Settings
  verweisen, sollten keinen falschen Key nennen.
- **Mutationsprobe statt Vertrauen:** einmal `sleepWarningPreflight` auf
  „immer false" verbiegen und prüfen, welche Tests rot werden. Zwei Tests
  waren dadurch trivial grün (der Run startet in der Mutation ja sowieso) und
  brauchten eine zusätzliche Vorbedingung.
- **Sprache:** die TUI ist durchgehend englisch (alle anderen Dialoge, Optionen,
  Footer). Die im PRD zitierten deutschen Dialogtexte sind daher inhaltlich,
  nicht wörtlich umgesetzt — deutscher Text in einer englischen TUI würde wie
  ein Bug aussehen.
- **Randfall AC7 vs. AC3:** „Batteriestatus unermittelbar ⇒ kein Dialog" gilt
  für den *Batteriegrund*. Ein explizit auf `false` gesetztes `keepAwake`
  warnt weiterhin, auch wenn `pmset` nichts liefert — das ist keine Vermutung,
  sondern eine Einstellung, die der User selbst getroffen hat.
- Für PERF-003: die Schlaf-Erkennung ist plattformneutral und gehört *nicht*
  hinter `awake.Supported()` — nur Dialog und `caffeinate` sind macOS-Sachen.

---
<!-- chief-timing story="PERF-002" duration_ms=729368 cost=22.239082 in=32347 out=1441 cache_create=293015 cache_read=10767847 -->
