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
  Er protokolliert auf Wunsch Modell *und* Prompt jeder Invocation (`models`,
  `prompts`; Muster `modelLog`/`promptLog` — Pointer-Feld, damit `WithModel`-Clones
  denselben Log teilen). Wiederverwendbar für neue Loop-Tests: `phaseModelPRD`
  (Ein-Story-PRD) + `runPhases(t, l)` (ganzer Run inkl. Event-Drain) — nur das
  Platten-Setup gehört in einen eigenen Konstruktor.
- **Prompt-Änderungen brauchen zwei Ebenen:** den Content-Guard in
  `embed/embed_test.go` für den Wortlaut (Muster:
  `TestGetPrompt_TargetedProgressRead`, `TestGetPrompt_NoFileReadInstruction`) und
  — wenn die Aussage lautet „chief selbst tut X *nicht*" — einen Test am
  Loop-Seam, wo echte Dateien auf der Platte liegen (Muster:
  `TestBuildPrompt_DoesNotInlineProgress`). Der Wortlaut-Test allein kann nur
  „Text ist da" behaupten.
- **Bei ACs der Form „bleibt unverändert" ist der Guard das Deliverable:** rot-grün
  gibt es nicht, also Test schreiben *und* per kurzzeitiger Mutation im
  Produktionscode beweisen, dass er anschlagen würde. Ohne diese Probe ist so ein
  Test nicht von einem wertlosen zu unterscheiden.
- **Nach dem Anfügen eines Struct-Felds `gofmt -l` laufen lassen:** ein längerer
  Feldname richtet die Kommentarspalte der ganzen Struct neu aus; `go vet` merkt
  das nicht. Verfügbare Checks hier sind `gofmt -l`, `go vet ./...`,
  `go build ./...`, `go test ./...` — `golangci-lint` (via `make lint`) ist
  **nicht** installiert.
- **`ralph/PROMPT.md` ist nicht chiefs Build-Prompt.** Der ausgelieferte Prompt ist
  `embed/prompt.txt` (via `embed.GetPrompt`); `ralph/` ist die eigenständige
  Vorlage mit `progress.txt` und driftet absichtlich auseinander. Prompt-Stories
  ändern `embed/`, nicht `ralph/`.
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
- **Provider-gegatete Prompt-Blöcke** folgen alle einem Muster in `embed/embed.go`:
  Konstante `<name>Claude` + Funktion `<name>(flag bool) string`, im Prompt ein
  `{{PLATZHALTER}}` auf eigener Zeile, Block beginnt und endet mit `\n` (leer =
  genau eine Leerzeile = byte-identisch zum alten Prompt). Das Claude-Signal ist
  überall `Provider.SupportsInteractiveQuestions()`. Vorlagen: `exploreModel`,
  `prototypeBlock`, `researchDelegation`.
- **Ein neuer `GetPrompt`-Parameter muss an ZWEI Stellen verdrahtet werden:**
  `NewLoopWithEmbeddedPrompt` *und* `Manager.Start` bauen den `buildPrompt`-Closure
  je selbst. Nur eine davon zu ändern sieht in Unit-Tests grün aus und erreicht
  echte Runs nie (die laufen alle über den Manager).
- **Manager-Tests mit echtem Run** brauchen `SetBaseDir(dir)` + Git-Repo + `prd.md`
  unter `.chief/prds/<name>/` (Muster: `newResearchRepo`). Ohne baseDir läuft der
  Run im echten Projekt-Repo und ruft den Agenten gar nicht auf. Den Run über
  `SetCompletionCallback` auslaufen lassen und `m.Events()` in einer Goroutine
  drainen — `m.Stop()` mitten im Lauf triggert ein vorbestehendes Data-Race
  zwischen `Loop.Stop` und `exec.Cmd.Start`.
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

## 2026-07-27 - PERF-004
Der Build-Prompt (`embed/prompt.txt`) weist den Agenten jetzt an, `progress.md`
gezielt zu lesen: `## Codebase Patterns` oben plus die letzten ein bis zwei
Story-Einträge, ausdrücklich **nicht** die ganze Datei. Begründung steht im
Prompt selbst mit drin (die Datei wächst pro Story um einen Bericht, das Alte
ist in Patterns konsolidiert), damit der Agent die Anweisung nicht als
willkürlich verwirft. Der Schluss-Hinweis unter „Important" trägt die
Einschränkung nun ebenfalls.

Unverändert geblieben (ist jeweils per Test festgenagelt): die
Append-Anweisung („never replace, always append"), die Patterns-Pflege
(`## Codebase Patterns` section at the TOP), und dass chief selbst nichts aus
`progress.md` in den Prompt inlined — `promptBuilderForPRD` übergibt weiter nur
`prd.ProgressPath(...)`. Kein Produktionscode angefasst außer dem Prompt-Text.

**Dateien**
- `embed/prompt.txt`: Schritt 1 umformuliert, „Important"-Bullet ergänzt.
- `embed/embed_test.go`: `TestGetPrompt_TargetedProgressRead` (Content-Guard:
  gezielt-lesen + Verbot + Append/Patterns überleben + Bedingtheit).
- `internal/loop/progress_prompt_test.go` (neu): `promptLog`,
  `newProgressPromptLoop`, `TestBuildPrompt_DoesNotInlineProgress`,
  `TestBuildPrompt_NoProgressFileYet`.
- `internal/loop/loop_test.go`: Feld `prompts *promptLog` am `mockProvider`,
  `LoopCommand` zeichnet den Prompt auf.
- `docs/concepts/ralph-loop.md`: „What the agent receives" nachgezogen.

**Learnings for future iterations**
- **Der `mockProvider` kann jetzt auch Prompts beobachten, nicht nur Modelle.**
  `promptLog` ist nach dem Muster von `modelLog` gebaut (Mutex + `record`/`all`,
  als Pointer-Feld, damit `WithModel`-Clones denselben Log teilen). Wer künftig
  prüfen will, *was* eine Phase dem Agenten übergibt, braucht keinen neuen Seam:
  `&mockProvider{cliPath: ..., prompts: &promptLog{}}` und `got[0]` ist der
  Build-Prompt, `got[1]` der Review-, `got[2]` der Consolidate-Prompt.
- **Prompt-Änderungen brauchen zwei Testebenen.** Der Content-Guard in
  `embed/` prüft den Wortlaut (billig, aber tautologiegefährdet — er kann nur
  „Text ist da" sagen). Die Verhaltensaussage „chief inlined nichts" gehört an
  den Loop-Seam, weil nur dort eine echte `progress.md` auf der Platte liegt.
  Der Loop-Test hat sich per Mutation als scharf erwiesen (Inlining in
  `promptBuilderForPRD` eingebaut ⇒ beide Marker-Checks schlagen an).
- **Grüne Tests für „bleibt wie heute" sind hier der Punkt, nicht ein Versäumnis.**
  Bei einer Story, deren halbe AC-Liste „X bleibt unverändert" lautet, ist
  test-first nicht rot-grün, sondern „Guard schreiben, per Mutation beweisen,
  dass er greifen würde". Ohne die Mutationsprobe wäre der Test wertlos gewesen.
- **`phaseModelPRD` (aus `phase_model_test.go`) ist die wiederverwendbare
  Ein-Story-PRD für Loop-Tests**, und `runPhases(t, l)` fährt einen ganzen Run
  inkl. Event-Drain. Neue Loop-Tests sollten beide nutzen statt eigene
  Harness-Kopien zu bauen; nur das Setup (was vorher auf der Platte liegt)
  gehört in den eigenen Konstruktor.
- **`gofmt` nach dem Anfügen eines Struct-Felds mitlaufen lassen:** ein längerer
  Feldname (`prompts *promptLog`) richtet die Kommentarspalte der *ganzen*
  Struct neu aus. `go vet` merkt das nicht, `gofmt -l` schon.
- **`golangci-lint` ist in dieser Umgebung nicht installiert** (`make lint`
  bricht mit „No such file or directory" ab). Verfügbare Checks sind
  `gofmt -l`, `go vet ./...`, `go build ./...` und `go test ./...` — die laufen
  alle grün durch (~2 min für die volle Suite).
- **Berührt: `ralph/PROMPT.md` bewusst nicht.** Das ist die eigenständige
  Ralph-Loop-Vorlage mit `progress.txt`, nicht der von `embed.GetPrompt`
  ausgelieferte Build-Prompt. Wer dort etwas ändert, ändert nicht chiefs
  Verhalten — die beiden driften absichtlich auseinander.
---
<!-- chief-timing story="PERF-004" duration_ms=365140 cost=9.280738 in=160 out=1720 cache_create=152499 cache_read=4193321 -->

## 2026-07-27 - PERF-005: Recherche-Delegation im Build-Prompt

**Implementiert**
- Neuer Block `## Codebase Research` im Build-Prompt (`{{RESEARCH}}` in
  `embed/prompt.txt`, Inhalt als `researchDelegationClaude` in `embed/embed.go`):
  breite Codebase-Recherche an einen Subagenten (Explore-Agent, sonst
  general-purpose) delegieren statt mit vielen Shell-Aufrufen im Hauptkontext;
  Dateien mit dem Read-Tool lesen statt `cat`/`sed`/`head`/`tail`. Die Begründung
  („jeder Turn schickt die ganze Konversation erneut") steht im Block, damit der
  Agent die Regel nicht als willkürlich verwirft — dasselbe Vorgehen wie PERF-004.
- Gating exakt wie beim bestehenden Explore-Block der interaktiven Prompts:
  `researchDelegation(subagents bool)` neben `exploreModel`/`prototypeBlock`,
  gespeist aus `Provider.SupportsInteractiveQuestions()`. Nicht-Claude-Provider
  bekommen einen unveränderten Build-Prompt (leerer String, identisches
  Whitespace-Layout — verifiziert).
- `GetPrompt` hat einen sechsten Parameter `subagents bool`;
  `promptBuilderForPRD(prdPath, subagents)` reicht ihn durch. Verdrahtet an
  *beiden* Stellen: `NewLoopWithEmbeddedPrompt` und `Manager.Start`.
- Review- und Consolidate-Prompt unangetastet.

**Dateien**
- `embed/embed.go`: `researchDelegationClaude`, `researchDelegation`,
  `GetPrompt`-Signatur.
- `embed/prompt.txt`: `{{RESEARCH}}` zwischen Task-Liste und `## Testing`.
- `embed/embed_test.go`: `TestGetPrompt_ResearchDelegation`,
  `TestReviewAndConsolidatePrompts_NoResearchBlock`.
- `internal/loop/loop.go`, `internal/loop/manager.go`: Verdrahtung.
- `internal/loop/research_prompt_test.go` (neu): `newResearchRepo`,
  `newResearchPromptLoop`, drei Tests inkl. Manager-Hop.
- `internal/loop/loop_test.go`: Feld `native` am `mockProvider`.
- `docs/concepts/ralph-loop.md`: „Build Prompt" nachgezogen.

**Learnings for future iterations**
- **`mockProvider` kann jetzt auch „ist Claude" simulieren:** Feld `native bool`,
  `SupportsInteractiveQuestions()` gibt es zurück. Default `false` = Verhalten
  wie bisher, also mussten bestehende Literale nicht angefasst werden. Damit sind
  provider-gegatete Features am Loop-Seam testbar, ohne einen Wrapper-Typ zu
  bauen.
- **`Manager.Stop()` mitten im Lauf triggert ein vorbestehendes Data-Race**
  (`Loop.Stop` liest `l.cmd`, während `runIteration` es via `exec.Cmd.Start`
  schreibt) — reproduzierbar mit `-race`. Manager-Tests, die einen echten Run
  fahren, sollten ihn deshalb über `SetCompletionCallback` + Channel auslaufen
  lassen statt zu stoppen, und `m.Events()` in einer Goroutine drainen (Puffer
  100, sonst kann der Run stallen). Das Race ist Produktionscode, nicht Testcode
  — falls es je jemanden beißt, liegt es in `internal/loop/loop.go:816`.
- **Manager-Tests brauchen `SetBaseDir` + echtes Git-Repo + `prd.md` unter
  `.chief/prds/<name>/`.** Mit der JSON-PRD aus `createTestPRDWithName` und ohne
  baseDir läuft der Run im echten Projekt-Repo und invoziert den Agenten gar nicht
  — der Test wartet dann ins Leere. `newResearchRepo(t)` in
  `research_prompt_test.go` liefert das komplette Setup (Repo, PRD, Mock-Agent)
  und ist für weitere Manager-/Loop-Tests wiederverwendbar.
- **`buildPrompt()` im Test ein zweites Mal aufzurufen taugt nicht**, wenn der
  Loop schon läuft: die Story ist dann durch und man bekommt „all stories are
  complete". Beobachten statt nachfragen — `promptLog` zeigt, was der Agent
  wirklich bekommen hat.
- **Go-String-Konkatenation für Backticks bricht das Zeilenlayout des Prompts.**
  `` ` + "`cat`" + ` `` mitten in einem Absatz erzeugt beim Rendern eine
  überlange Zeile, weil der Quelltext-Umbruch woanders sitzt als der gerenderte.
  Den Umbruch so legen, dass die konkatenierte Stelle am Zeilenanfang steht, und
  das Ergebnis einmal wirklich ausgeben (Wegwerf-Test mit `fmt.Println(GetPrompt(...))`).
- **Prompt-Layout für den Nicht-Claude-Fall verifizieren, nicht annehmen:** ein
  Platzhalter auf eigener Zeile plus ein Block, der mit `\n` beginnt und endet,
  ergibt leer genau eine Leerzeile — also byte-identisch zum alten Prompt. Mit
  falscher Newline-Bilanz hätte man dort eine zusätzliche Leerzeile.
- **Vier Mutationsproben, alle gefangen:** (1) Block nie injizieren → embed-Test +
  beide Loop-Tests; (2) Gating entfernt (`if true`) → die „non-Claude"-Zweige;
  (3) Manager-Hop auf `false` verdrahtet → nur `TestManagerGatesResearchBlock…`
  (der Hop ist also echt abgedeckt, nicht bloß mitgetestet); (4) Block in
  `review_prompt.txt`/`consolidate_prompt.txt` geleakt → AC4-Guards.
- **`GetPrompt` hat jetzt 6 Parameter.** Für den siebten gilt sinngemäß, was für
  `CompletionScreen.Configure` notiert ist: vorher auf ein Options-Struct
  umstellen. Call-Sites sind hier aber billig (eine produktiv, ~8 im Test).
---
<!-- chief-timing story="PERF-005" duration_ms=645361 cost=23.444932 in=254 out=2384 cache_create=299503 cache_read=11764427 -->
