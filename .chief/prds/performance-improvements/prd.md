# PRD: Performance-Verbesserungen für chief-Runs

## Introduction

Ein vollständiger chief-Run (7 Stories, Projekt `agency-os`, 27.07.2026) kostete
$350 und 2h53m Arbeitszeit — plus 56 Minuten, in denen der Mac unbemerkt schlief.
Die Analyse (`analysis/2026-07-27-run-performance.md`) zeigt drei Haupttreiber:
Reviews und Konsolidierung laufen unnötig auf dem teuersten Modell (34–40 % der
Kosten), die Agenten lesen die komplette, monoton wachsende `progress.md` in
jeden Kontext (~20k Tokens pro Agent), und sie erkunden die Codebase mit
Hunderten kleinteiligen Shell-Aufrufen im Hauptkontext statt in Subagenten.
Dazu kommt: chief hat den Systemschlaf weder verhindert noch bemerkt.

Dieses PRD macht das Review-/Consolidate-Modell konfigurierbar (Default: Sonnet),
verschlankt die Prompts (progress.md-Lesen, Recherche-Delegation) und macht
Systemschlaf sichtbar: eine blockierende Warnung vor dem Run bei Batteriebetrieb
oder deaktiviertem `keepAwake`, und ein Hinweis im Abschluss-Screen, wenn der Mac
während des Runs geschlafen hat.

Wichtige Randbedingung: Clamshell-Sleep (Deckel zu ohne externes Display, auf
Batterie) ist auf Apple Silicon softwareseitig **nicht** verhinderbar — auch
nicht durch `caffeinate`. Das Ziel ist daher: verhindern wo möglich (das tut
`keepAwake` bereits), warnen wo nicht, und erkennen was trotzdem passiert.

## Goals

- Review- und Consolidate-Agenten laufen per Default auf Sonnet statt auf dem
  Build-Modell; pro Phase per config überschreibbar. Erwartete Ersparnis: die
  ~40 % Kostenanteil dieser Phasen fallen auf etwa ein Fünftel des Preises
  (~$115 pro Run vergleichbarer Größe).
- Build-Agenten lesen von `progress.md` nur noch die relevanten Teile
  („Codebase Patterns" + die letzten Einträge) statt der ganzen Datei.
- Build-Agenten (Claude) delegieren Codebase-Recherche an Subagenten und nutzen
  das Read-Tool statt `cat`/`sed`/`grep`-Ketten im Hauptkontext.
- Vor einem Run auf Batterie oder mit deaktiviertem `keepAwake` erscheint eine
  blockierende Warnung, dass der Mac einschlafen kann.
- Nach einem Run, während dessen der Mac geschlafen hat, zeigt der
  Abschluss-Screen die geschlafene Zeit an.

## User Stories

### PERF-001: Review und Consolidate auf eigenem Modell
**Status:** done
**Priority:** 1
**Description:** Als Ben möchte ich, dass Review- und Consolidate-Agenten per
Default auf Sonnet laufen (überschreibbar per config), damit die 40 % der
Run-Kosten, die diese Phasen ausmachen, auf ein Fünftel schrumpfen, ohne die
Build-Qualität zu berühren.

**Acceptance Criteria:**
- [x] `.chief/config.yaml` akzeptiert `review.model` und `consolidate.model`
- [x] Ohne Angabe laufen Review- und Consolidate-Agenten auf Sonnet: die
      `system/init`-Zeile im Agenten-Log dieser Phasen weist ein Sonnet-Modell
      aus, während der Build unverändert läuft
- [x] Ein explizit gesetzter Wert (z. B. `review.model: haiku`) wird für die
      jeweilige Phase verwendet und erscheint entsprechend im Agenten-Log
- [x] Das Build-Verhalten ändert sich nicht: leeres `agent.model` reicht
      weiterhin kein `--model` an Claude Code durch
- [x] Bei Nicht-Claude-Providern (codex, opencode, cursor, gemini) sind die
      neuen Optionen wirkungslos und erzeugen keinen Fehler
- [x] `config.Save`/`config.Load` erhalten gesetzte Werte unverändert
      (Roundtrip); ungesetzte Werte erzeugen keine Einträge in der
      gespeicherten Datei
- [x] `docs/reference/configuration.md` dokumentiert beide Optionen inkl.
      Default

### PERF-002: Warnung vor dem Run, wenn der Mac einschlafen kann
**Status:** done
**Priority:** 2
**Description:** Als Ben möchte ich vor dem Start eines Runs gewarnt werden,
wenn mein Mac auf Batterie läuft oder `keepAwake` deaktiviert ist, damit ich
nicht erst nach Stunden merke, dass der Run 56 Minuten lang geschlafen hat.

**Acceptance Criteria:**
- [x] Läuft der Mac beim Run-Start auf Batterie, erscheint vor dem Start ein
      blockierender Dialog im Stil der bestehenden Branch-Warnung
      (`BranchWarning`-Muster) mit den Optionen „Trotzdem starten" und
      „Abbrechen"
- [x] Der Dialogtext bei Batteriebetrieb erklärt, dass der Mac bei geschlossenem
      Deckel trotz `keepAwake` einschläft (Clamshell-Sleep ist auf Apple Silicon
      nicht verhinderbar) und nennt Abhilfe: Netzteil anschließen oder Deckel
      offen lassen
- [x] Ist `loop.keepAwake` explizit auf `false` gesetzt, erscheint derselbe
      Dialog mit angepasstem Text (Schlafschutz ist ausgeschaltet)
- [x] Treffen beide Bedingungen zu, erscheint **ein** Dialog, der beide Gründe
      nennt
- [x] „Abbrechen" startet den Run nicht und kehrt zur vorherigen Ansicht zurück;
      „Trotzdem starten" startet ihn normal
- [x] Am Netzteil mit aktivem `keepAwake` erscheint kein Dialog; der Run startet
      wie bisher ohne zusätzlichen Schritt
- [x] Lässt sich der Batteriestatus nicht ermitteln, erscheint kein Dialog
      (fail open)
- [x] Auf Nicht-macOS-Systemen erscheint der Dialog nie
- [x] Der Dialog erscheint bei jedem Run-Start erneut (keine
      „Nicht mehr anzeigen"-Persistenz)
- [x] Der Dialog ist per Tastatur bedienbar (Auswahl wechseln + bestätigen),
      wie die Branch-Warnung

### PERF-003: Geschlafene Zeit im Abschluss-Screen
**Status:** done
**Priority:** 3
**Description:** Als Ben möchte ich nach einem Run sehen, ob und wie lange der
Mac währenddessen geschlafen hat, damit ich verstehe, warum die Wanduhrzeit von
der angezeigten Arbeitszeit abweicht.

**Acceptance Criteria:**
- [x] chief erkennt Systemschlaf während eines Runs durch periodischen Vergleich
      von Wanduhr und monotoner Uhr; eine Lücke von mehr als 1 Minute zählt als
      Schlaf
- [x] Mehrere Schlafphasen innerhalb eines Runs werden aufsummiert
- [x] Hat der Mac während des Runs geschlafen, zeigt der bestehende
      Abschluss-Screen (Completion-Modal) eine zusätzliche Zeile im Stil
      „Mac schlief 56m während des Runs" unterhalb der „Completed in …"-Zeile;
      die Modal-Höhe wächst entsprechend mit
- [x] Ohne erkannten Schlaf erscheint keine solche Zeile und der
      Abschluss-Screen sieht aus wie heute
- [x] Während des Runs erscheint keine Live-Warnung; Dashboard und Log bleiben
      unverändert
- [x] Angezeigte Story-Zeiten, Gesamtzeit und ETA bleiben reine Arbeitszeit
      (monotone Messung wie heute); die geschlafene Zeit wird separat
      ausgewiesen, nicht eingerechnet
- [x] Die geschlafene Zeit wird nicht persistiert; nach einem chief-Neustart
      beginnt die Zählung bei null

### PERF-004: Build-Agent liest progress.md gezielt statt komplett
**Status:** done
**Priority:** 4
**Description:** Als Ben möchte ich, dass der Build-Agent aus `progress.md` nur
die „Codebase Patterns"-Sektion und die letzten Einträge liest, damit nicht
~20k Tokens Alt-Berichte durch jeden der ~117 Turns jedes Agenten geschleppt
werden.

**Acceptance Criteria:**
- [x] Der Build-Prompt weist den Agenten an, aus `progress.md` gezielt die
      „Codebase Patterns"-Sektion und die letzten ein bis zwei Story-Einträge
      zu lesen — und ausdrücklich **nicht** die gesamte Datei
- [x] Die Anweisung, den eigenen Bericht an `progress.md` **anzuhängen** (nie zu
      ersetzen), bleibt unverändert bestehen
- [x] Die Anweisung, die „Codebase Patterns"-Sektion zu pflegen, bleibt
      unverändert bestehen
- [x] chief selbst parst und inlined weiterhin nichts aus `progress.md` in den
      Prompt; es ändert sich nur der Prompt-Text
- [x] Der Fall „`progress.md` existiert noch nicht" verhält sich wie heute
      (Anweisung gilt nur, wenn die Datei existiert)

### PERF-005: Recherche-Delegation im Build-Prompt
**Priority:** 5
**Description:** Als Ben möchte ich, dass Build-Agenten (Claude) ihre
Codebase-Recherche an Subagenten delegieren und das Read-Tool statt
`cat`/`sed`/`grep`-Ketten nutzen, damit die ~350 reinen Erkundungs-Turns nicht
jedes Mal den kompletten Kontext durch die Abrechnung ziehen — der quadratische
Hebel.

**Acceptance Criteria:**
- [ ] Der Build-Prompt enthält für den Claude-Provider einen Block, der
      anweist: breite Codebase-Recherche an einen Explore-/Subagenten
      delegieren statt sie mit vielen einzelnen Shell-Aufrufen im Hauptkontext
      zu erledigen
- [ ] Derselbe Block weist an, zum Lesen von Dateien das Read-Tool statt
      `cat`/`sed`/`head`/`tail` zu verwenden
- [ ] Der Block wird nur für den Claude-Provider injiziert; bei allen anderen
      Providern enthält der Build-Prompt ihn nicht (gleiche Gating-Mechanik wie
      der bestehende Explore-Block der interaktiven PRD-Prompts)
- [ ] Review- und Consolidate-Prompts bleiben unverändert (der Review-Prompt
      enthält bereits die Anweisung, nur den Diff zu betrachten)

## Functional Requirements

- FR-1: Die Konfiguration muss `review.model` und `consolidate.model` als
  optionale String-Felder akzeptieren.
- FR-2: Ist `review.model` bzw. `consolidate.model` nicht gesetzt und der
  Provider Claude, muss chief die jeweilige Phase mit einem Sonnet-Modell
  starten; ein gesetzter Wert muss unverändert als Modell der Phase verwendet
  werden.
- FR-3: Die Modellauflösung der Build-Phase darf sich nicht ändern
  (Flag > `CHIEF_MODEL` > `agent.model`; leer ⇒ kein `--model`).
- FR-4: Vor dem Start eines Runs muss chief auf macOS prüfen, ob der Rechner
  auf Batterie läuft und ob `loop.keepAwake` deaktiviert ist; trifft mindestens
  eines zu, muss ein blockierender Bestätigungsdialog erscheinen, bevor der Run
  startet.
- FR-5: Der Dialog muss die zutreffenden Gründe benennen und die Wahl zwischen
  Starten und Abbrechen lassen; Abbrechen darf keinen Run starten.
- FR-6: Schlägt die Batteriestatus-Ermittlung fehl oder läuft chief nicht auf
  macOS, darf kein Dialog erscheinen (fail open).
- FR-7: Während eines Runs muss chief periodisch Wanduhr- und monotone Zeit
  vergleichen und Lücken über 1 Minute als Schlafzeit aufsummieren.
- FR-8: Der Abschluss-Screen muss die aufsummierte Schlafzeit anzeigen, wenn
  sie größer als null ist, und andernfalls unverändert bleiben.
- FR-9: Story-Zeiten, Gesamtdauer und ETA müssen weiterhin ausschließlich
  monoton gemessene Arbeitszeit anzeigen.
- FR-10: Der Build-Prompt muss anweisen, aus `progress.md` nur die
  „Codebase Patterns"-Sektion und die letzten Einträge zu lesen; die
  Append- und Patterns-Pflege-Anweisungen müssen erhalten bleiben.
- FR-11: Der Build-Prompt muss — nur für den Claude-Provider — anweisen,
  Codebase-Recherche an Subagenten zu delegieren und das Read-Tool statt
  Shell-Lesekommandos zu verwenden.

## Non-Goals

- **Kein Verhindern von Clamshell-Sleep** — auf Apple Silicon softwareseitig
  unmöglich; `keepAwake`/`caffeinate` bleiben unverändert wie sie sind.
- **Keine Änderung am Build-Modell-Default** — leeres `agent.model` reicht
  weiterhin kein `--model` durch (Claude Code wählt selbst).
- **Kein chief-seitiges Parsen/Inlinen/Rotieren von `progress.md`** — nur die
  Leseanweisung im Prompt ändert sich; Format, Wachstum und Committen der Datei
  bleiben unberührt.
- **Keine Kopplung des Review-Umfangs an die Story-Größe** (H4 der Analyse) —
  bewusst gestrichen: schlechtes Risiko/Nutzen-Verhältnis, und mit
  Sonnet-Reviews schrumpft der Kostenanteil ohnehin.
- **Keine Live-Warnung bei erkanntem Schlaf** während des Runs — nur der
  Abschluss-Screen.
- **Keine Persistenz der Schlafzeit** über chief-Neustarts hinweg.
- **Keine Änderung am Watchdog** — er misst monoton und feuert deshalb im
  Systemschlaf nicht fälschlich; das ist gewollt.
- **Keine Änderungen an Review-/Consolidate-Prompts** (außer der
  Modell-Weitergabe).

## Design & Frontend

Zwei TUI-Berührungspunkte, beide auf bestehenden Komponenten:

**1. Schlaf-Warn-Dialog (vor Run-Start):**
- Erscheint als blockierendes Modal vor dem Run-Start, im selben Stil und mit
  derselben Interaktion wie die bestehende Branch-Warnung (`BranchWarning`):
  Titel, erklärender Text, zwei Optionen, Tastaturauswahl (hoch/runter +
  Enter/Esc).
- Drei Textvarianten: (a) Batteriebetrieb — „Dein Mac läuft auf Batterie. Bei
  geschlossenem Deckel schläft er trotz keepAwake ein; der Run pausiert dann
  unbemerkt. Netzteil anschließen oder Deckel offen lassen." (b) `keepAwake`
  deaktiviert — Hinweis, dass der Schlafschutz per config aus ist. (c) beides —
  ein Dialog, beide Gründe aufgezählt.
- Optionen: „Trotzdem starten" / „Abbrechen" (Abbrechen als sichere
  Default-Auswahl).
- Zustände: Dialog sichtbar (blockiert alles darunter) oder nicht vorhanden.
  Kein Lade-/Fehlerzustand — schlägt die Batterieabfrage fehl, gibt es schlicht
  keinen Dialog.

**2. Abschluss-Screen (Completion-Modal):**
- Bestehendes Modal; neu ist genau eine Zeile unterhalb von
  „Completed in … • $…": „Mac schlief 56m während des Runs" (Dauer im
  bestehenden `formatDuration`-Format).
- Die Zeile erscheint nur bei erkannter Schlafzeit > 0; die berechnete
  Modal-Höhe berücksichtigt die Zusatzzeile.
- Keine weiteren Layout-Änderungen; Story-Balkendiagramm und Auto-Action-Zeilen
  bleiben unverändert.

## Technical Considerations

- Die Modell-Weitergabe an Review/Consolidate braucht einen per-Phase-Hook um
  die Agenten-Invocation; heute teilen sich alle drei Phasen denselben
  Provider. `SetModel` existiert am Claude-Provider bereits.
- Der Sonnet-Default gilt nur für den Claude-Provider — die anderen Provider
  nehmen konstruktionsbedingt kein Modell entgegen.
- Batteriestatus auf macOS ist per Systemabfrage ermittelbar (z. B. `pmset -g
  batt`); die Abfrage muss best-effort sein und darf einen Run nie blockieren
  oder verzögern, wenn sie fehlschlägt.
- Die Sleep-Erkennung (Wanduhr vs. monotone Uhr) ist plattformneutral und darf
  auch auf Nicht-macOS mitlaufen; nur Dialog und `caffeinate` sind
  macOS-spezifisch.
- Prompt-Änderungen (PERF-004, PERF-005) wirken auf laufende Projekte sofort;
  ihre Kostenwirkung ist erst über mehrere Runs beurteilbar.

## Testing Decisions

- **Config (PERF-001):** am bestehenden Seam `config.Load`/`config.Save` —
  YAML-String in `t.TempDir()` schreiben, laden, Felder prüfen; Roundtrip- und
  Default-Tests nach dem Muster von `TestReviewConfigEnabled` in
  `internal/config/config_test.go`. Kein neuer Seam.
- **Modell-Weitergabe (PERF-001):** über den bestehenden `mockProvider` in
  `internal/loop/loop_test.go` — beobachten, mit welchem Modell die Review-
  bzw. Consolidate-Iteration den Provider aufruft; nicht in die Loop-Interna
  greifen.
- **Prompts (PERF-004, PERF-005):** über die bestehenden Content-Guard-Tests in
  `embed/embed_test.go` (Muster: `TestGetPrompt_NoFileReadInstruction`,
  `TestGetInitPromptExploreModel`) — prüfen, dass der Build-Prompt die neuen
  Anweisungen enthält bzw. für Nicht-Claude-Provider nicht enthält.
- **Sleep-Erkennung (PERF-003):** als pure Funktion testen — Zeitstempel-Paare
  (Wanduhr/monoton) rein, aufsummierte Schlafdauer raus; Erwartungswerte sind
  Literale aus der Spezifikation (Schwelle 1 Minute), nicht nachgerechnet.
  Das ist der einzige neue Seam.
- **Warn-Dialog (PERF-002):** Entscheidungslogik (Batterie? keepAwake aus? ⇒
  welche Textvariante, Dialog ja/nein) als pure Funktion testen; die
  Batterieabfrage selbst wird wie in `internal/awake/awake_test.go` über einen
  injizierbaren Command-Seam ersetzt, nicht real ausgeführt.

## Success Metrics

- Bei einem Run vergleichbarer Größe (7 Stories) sinken die Kosten der Review-
  und Consolidate-Phasen von ~$143 auf grob ein Fünftel (~$30) — ablesbar an
  den `chief-timing`-Kommentaren bzw. einer Log-Auswertung wie in
  `analysis/2026-07-27-run-performance.md`.
- Ein Run auf Batterie startet nie mehr ohne expliziten Klick auf „Trotzdem
  starten".
- Nach einem Run mit Systemschlaf ist die geschlafene Zeit im Abschluss-Screen
  ablesbar, statt nur als unerklärliche Wanduhr-Differenz zu existieren.
- Über die nächsten Runs beobachtbar (weich): weniger `grep`/`cat`/`sed`-Turns
  im Agenten-Log und weniger Kontext-Tokens pro Agent als im Referenz-Run
  (dort: 220 `grep`, 60 `cat`, 70 `sed`, Ø 105k Kontext).

## Open Questions

Keine — alle entscheidbaren Fragen wurden in Phase 1 geklärt (Schlaf-Paket als
Warnung+Erkennung statt unmöglichem Verhindern; Sonnet-Default für
Review/Consolidate; Build-Modell unangetastet; progress.md-Diät nur per
Prompt-Anweisung; H4 gestrichen).
