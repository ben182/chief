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
  Soll der Test *Aufrufe zählen* (nicht nur eine Antwort liefern), reicht ein
  Funktions-Seam nicht — dann ein kleines Interface als Feld (Muster:
  `tui.sleepTracker` mit `Sample`/`SleptSince`), und am Aufrufort auf nil prüfen,
  weil ein nil-Interface beim Methodenaufruf panict.
- **Fragen parametrisieren statt Zwischenstand beim Aufrufer halten:** braucht ein
  Wert einen Bezugspunkt, gibt man ihn als Argument mit (`SleptSince(ref)`) statt
  den Aufrufer einen Baseline-Snapshot verwalten zu lassen. Das erspart einen
  Verdrahtungs-Hop in `launchLoop`/`Manager.Start` — genau die Hops, die hier
  regelmäßig vergessen werden und im `tui`-Paket kaum testbar sind, weil `Start`
  mit nil-Provider vorher abbricht.
- **Alle chief-Zeiten sind monoton** (`time.Since`), also reine Arbeitszeit:
  Systemschlaf ist darin nicht enthalten. Wer Wanduhrzeit braucht, muss den
  monotonen Anteil per `t.Round(0)` abstreifen (Muster: `awake.SleepTracker`).
- **Signaturen mit vielen positionalen Parametern:** `CompletionScreen.Configure`
  hat 10 — vor dem elften auf ein Options-Struct umstellen. Jede Erweiterung
  kostet ~21 Call-Sites in `completion_test.go`, produktiv aber nur eine
  (`auto_actions.go:showCompletionScreen`).
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


## 2026-07-27 - PERF-003: Geschlafene Zeit im Abschluss-Screen

**Implementiert**
- `awake.SleepTracker`: vergleicht Wanduhr gegen monotone Uhr, Lücken > 1 Minute
  (`awake.SleepThreshold`) gelten als Schlaf, kleinere als Jitter. Gesampelt wird
  im bestehenden Sekunden-Tick (`elapsedTickMsg`) — kein neuer Timer, keine
  Live-Warnung.
- `SleptSince(ref)` statt eines laufenden Gesamtwerts: die Antwort ist auf den
  Run-Start (`App.startTime`) verankert. Damit erbt ein zweiter Run in derselben
  Session nicht die Naps des ersten, und Schlaf *vor* dem Run — den erst das erste
  Sample nach dem Start entdeckt — wird auf den Run-Start geklippt.
- Abschluss-Screen: eine Zeile `Mac slept 56m00s during the run` direkt unter
  „Completed in …", Modal-Höhe wächst um eine Zeile. Ohne Schlaf keine Zeile.
- Nichts wird persistiert (nur Speicher, `prd.Timing` unangetastet); Story-Zeiten,
  Gesamtzeit und ETA bleiben unverändert monoton = reine Arbeitszeit.

**Dateien**
- `internal/awake/detect.go` (+ `_test.go`, neu): `SleepTracker`, `SleepThreshold`,
  `Sample`, `SleptSince`, injizierbarer `clockReader`.
- `internal/tui/sleep_detect.go` (+ `_test.go`, neu): Interface `sleepTracker`,
  `App.sleptDuringRun()`.
- `internal/tui/completion.go` (+ `_test.go`): `slept` an `Configure` (Parameter
  #8) + Render + `calculateModalHeight`.
- `internal/tui/app.go`: Feld `sleepTracker`, `awake.NewSleepTracker()` in
  `NewAppWithOptions`, Sampling im `elapsedTickMsg`-Handler.
- `internal/tui/auto_actions.go`: `a.sleptDuringRun()` an `Configure`.
- `docs/troubleshooting/common-issues.md`.

**Learnings for future iterations**
- **Monotone Uhr in Go stoppt bei Systemschlaf (macOS), Wanduhr nicht.** Das ist
  der ganze Trick: `time.Now()` trägt beide Werte, `t.Round(0)` streift den
  monotonen Anteil ab, also ist `now.Round(0).Sub(prev.Round(0))` die Wanduhr-
  Differenz und `now.Sub(prev)` die monotone. Die Differenz der Differenzen ist
  der Schlaf. Nebeneffekt: alle bestehenden chief-Zeiten (`time.Since`) waren
  schon immer reine Arbeitszeit — dafür war nichts zu tun.
- **Ein Zeitstempel-Anker schlägt einen Baseline-Hop.** Erster Entwurf war
  „Tracker liefert Gesamtsumme + `App` merkt sich den Wert bei Run-Start in einer
  Map". Das hätte einen neuen Verdrahtungs-Hop in `launchLoop` gebraucht, der
  hinter `manager.Start` liegt und im `tui`-Paket praktisch untestbar ist (nil
  Provider ⇒ `Start` bricht vorher ab). `SleptSince(ref)` verlagert die Logik in
  das Paket, das sie testen kann, und kommt ohne neuen State in `App` aus.
- **Zwei-Uhren-Logik braucht einen Clock-Seam, nicht `runtime.GOOS`.** Wie bei
  PERF-002: `newSleepTracker(clockReader)` als unexportierter Konstruktor, echte
  Uhren nur in `NewSleepTracker()`. Der `fakeClock` im Test hat zwei Methoden,
  `run(d)` (beide Uhren) und `suspend(d)` (nur Wanduhr) — damit liest sich jedes
  Szenario wie das, was es modelliert.
- **`App`-Felder, die Tests injizieren, als Interface statt konkretem Typ**, wenn
  der Test *Aufrufe zählen* will (`Sample()`): ein Funktions-Seam reicht nur für
  reine Abfragen. Achtung: nil-Interface ⇒ `a.sleepTracker.Sample()` panict, also
  entweder am Aufrufort prüfen (so gemacht) oder nil-Receiver-Methoden am
  konkreten Typ (`SleepTracker` hat beides).
- **Sampling-Intervall = Auflösungsgrenze.** Ein Test, der exakt „20m" von einem
  Schlaf erwartet, der eine Sample-Grenze überschreitet, ist zu präzise: die
  Arbeitssekunde nach dem Aufwachen ist nicht vom Schlaf zu trennen. Statt
  Toleranzen einzubauen habe ich den Test auf das *erreichbare* Szenario
  umgeschrieben (Schlaf im Idle, dann Run-Start) und dort die harte Schranke
  „höchstens ein Sample-Intervall darf durchsickern" behauptet — der Test wurde
  dadurch schärfer, nicht weicher.
- **`CompletionScreen.Configure` hat jetzt 10 positionale Parameter.** Wer den
  elften braucht, sollte vorher auf ein Options-Struct umstellen; das Ändern der
  Signatur kostet ~21 Call-Sites in `completion_test.go` (perl-Ersetzung auf
  `..., 0, nil, 0)` ging problemlos). Alle Aufrufe kommen aus genau einer Stelle
  in `auto_actions.go:showCompletionScreen`.
- **Bekannte Grenze:** `elapsedTickMsg` feuert nur weiter, solange die *betrachtete*
  PRD `StateRunning` ist. Läuft nur eine Hintergrund-PRD, wird nicht gesampelt.
  Dieselbe Einschränkung hat die angezeigte Gesamtzeit (`App.startTime`) schon
  vorher; wer echtes Multi-PRD-Timing will, braucht einen globalen Herzschlag.
- **`gofmt -l internal/tui/app.go` meldet die Datei weiterhin** — vorbestehende
  Ausrichtung im `App`-Struct-Kopf, verifiziert per `git stash` gegen HEAD. Nicht
  „nebenbei" mitformatieren, das bläht jeden Story-Diff auf.
- **Mutationsprobe zahlt sich aus:** fünf Mutationen (`sleptDuringRun` → 0,
  `Sample()` entfernt, Threshold → 0, Ref-Clipping entfernt, `sleepLine` → 0)
  wurden von je genau einem Test gefangen.
---
<!-- chief-timing story="PERF-003" duration_ms=810560 cost=21.883248 in=228 out=2062 cache_create=308034 cache_read=10633027 -->
