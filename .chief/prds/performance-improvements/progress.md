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
