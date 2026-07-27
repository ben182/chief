# Laufzeit- und Kostenanalyse: `stage-runner-header`

Analyse eines vollständigen chief-Runs vom 27.07.2026 im Projekt `agency-os`.
Anlass: der Run fühlte sich lang an (angezeigt: 2h53m, $349.20 für 7 Stories).

**Datenbasis:** `/Users/ben/Herd/agency-os/.chief/prds/stage-runner-header/claude-2026-07-27-142641.log`
(14,4 MB, 5825 stream-json-Zeilen), `progress.md` (persistierte chief-Timings),
`pmset -g log` für den Systemzustand während des Runs.

---

## 1. Kurzfassung

| Kennzahl | Wert |
|---|---|
| Wanduhr gesamt | **3h47m** (14:26:45 → 18:14:14) |
| davon macOS im Schlaf | **56 min** (Deckel zu, Batteriebetrieb) |
| tatsächliche Arbeitszeit | **2h53m** (= die von chief angezeigte Zahl) |
| Agentenaufrufe | 15 (7 Build + 7 Review + 1 Consolidate) |
| Turns (assistant messages) | 1751, also ~117 pro Agent |
| Kosten (Abo-Äquivalenzwert) | $350.71 |
| davon `cache_read` | **$268.10 (76 %)** |
| davon Output | $2.79 (0,8 %) |

Zwei Dinge dominieren: **die Reviews kosten ein Drittel des Runs**, und
**drei Viertel des Geldes fließt in das erneute Lesen von Kontext**, nicht in
erzeugten Code.

Kein API-Key im Spiel (`apiKeySource: none`) — bezahlt wird über das Abo, die
Dollarbeträge sind Äquivalenzwerte, keine Rechnung.

---

## 2. Zeitbilanz

### 2.1 Der Mac hat 56 Minuten geschlafen

`pmset -g log` für den 27.07.:

```
15:14:12  Sleep    Entering Sleep state due to 'Clamshell Sleep' … Using Batt (Charge:77%)
15:17:48  DarkWake …
15:33:19  Sleep    Entering Sleep state due to 'Maintenance Sleep' …
16:03:44  DarkWake …
16:09:40  Wake     Wake from Deep Idle … due to … lid … HID Activity
```

Deckt sich exakt mit den zwei großen Lücken im Log (19,5 min ab 15:13:39 und
36,8 min ab 15:33:15, Ortszeit). Der Run lief währenddessen nicht.

Konsequenzen, die zusammenpassen:

- **Der Watchdog hat nicht ausgelöst**, obwohl 36 Minuten kein Output kam.
  `runWatchdog` misst mit `time.Since` (monotone Uhr), und die steht unter macOS
  im Schlaf still. Aus Sicht des Watchdogs verging nur die Zeit der Wachphasen.
- **Die Story-Zeiten in der TUI stimmen** — sie messen ebenfalls monoton
  (`internal/tui/timing.go:16`) und zählen deshalb reine Arbeitszeit.
  SRH-003 steht mit 43m45s in `progress.md`, die Wanduhrspanne beträgt 97,8 min;
  die Differenz ist genau die Schlafphase.
- `loop.keepAwake` ist in `agency-os/.chief/config.yaml` **nicht gesetzt**, chief
  hat also kein `caffeinate` gestartet. Wichtig: `caffeinate` verhindert unter
  Apple Silicon den *Clamshell*-Sleep ohnehin nicht — dafür braucht es Netzteil
  plus externes Display oder einen offenen Deckel.

**Kein Rate-Limit-Problem.** Alle 150 `rate_limit_event` im Log haben
`status: allowed`, `rateLimitType: five_hour`, `overageStatus: allowed`. Die fünf
`api_retry`-Einträge sind kurze Backoffs von 0,6–2,1 s.

### 2.2 Aufteilung der 2h53m Arbeitszeit

| Phase | Zeit | Anteil |
|---|---|---|
| 7 Build-Agenten | 94 min | 55 % |
| 7 Review-Agenten | 67 min | 39 % |
| 1 Consolidate-Pass | 9 min | 5 % |

**Die Reviews verdoppeln bei kleinen Stories fast die Laufzeit.** Bei SRH-001
dauerte der Review 14,3 min gegenüber 12,6 min Bauzeit; bei SRH-006 10,4 gegenüber
13,4 min. Nur bei den größeren Stories fällt er anteilig ab.

---

## 3. Kostenbilanz

### 3.1 Wohin das Geld geht

| Tokenart | Menge | Kosten (Opus-Preise) |
|---|---|---|
| `cache_read` | **178,73 M** | $268.10 |
| `cache_creation` | 4,20 M | $78.77 |
| `input` | 0,07 M | $1.05 |
| `output` | 0,04 M | $2.79 |

Die Summe ($350.71) deckt sich mit der TUI-Anzeige ($349.20); die kleine Differenz
sind Rundungen in der Zuordnung zu Stories.

Gegen 37 400 erzeugte Output-Tokens stehen 183 Millionen gelesene Kontext-Tokens.
Das Verhältnis ist rund **1 : 4900**.

### 3.2 Pro Phase

| Phase | Kosten | Anteil |
|---|---|---|
| Build | $207.56 | 59 % |
| Review | $120.52 | **34 %** |
| Consolidate | $22.62 | 6 % |

### 3.3 Pro Agentenaufruf

| # | Phase | Story | Start | Dauer | Turns | Kosten |
|---|---|---|---|---|---|---|
| 1 | Build | SRH-001 | 14:26:45 | 12,6 min | 149 | $28.44 |
| 2 | Review | SRH-001 | 14:39:22 | 14,3 min | 150 | $29.79 |
| 3 | Build | SRH-002 | 14:53:47 | 9,1 min | 109 | $20.06 |
| 4 | Review | SRH-002 | 15:02:56 | 5,7 min | 76 | $11.29 |
| 5 | Build | SRH-003 | 15:08:43 | 23,6 min* | 235 | $62.66 |
| 6 | Review | SRH-003 | 16:28:41 | 17,9 min | 161 | $30.65 |
| 7 | Build | SRH-004 | 16:46:37 | 17,0 min | 163 | $34.39 |
| 8 | Review | SRH-004 | 17:03:41 | 6,6 min | 72 | $11.14 |
| 9 | Build | SRH-005 | 17:10:21 | 11,8 min | 100 | $22.26 |
| 10 | Review | SRH-005 | 17:22:11 | 7,7 min | 77 | $11.27 |
| 11 | Build | SRH-006 | 17:29:57 | 13,4 min | 116 | $24.54 |
| 12 | Review | SRH-006 | 17:43:24 | 10,4 min | 108 | $18.77 |
| 13 | Build | SRH-007 | 17:53:49 | 6,9 min | 70 | $15.21 |
| 14 | Review | SRH-007 | 18:00:45 | 4,6 min | 48 | $7.61 |
| 15 | Consolidate | — | 18:05:23 | 8,8 min | 117 | $22.62 |

\* Wanduhrspanne 79,9 min abzüglich 56,3 min Systemschlaf.

Keine Retries, keine Watchdog-Kills, kein geparkter Story-Versuch — der Loop lief
sauber durch. Die Laufzeit ist also kein Fehlerbild, sondern der Normalbetrieb.

---

## 4. Warum es so teuer ist

Die Kosten eines Agentenlaufs sind näherungsweise

```
Kosten ≈ Turns × durchschnittliche Kontextgröße
```

weil jeder Turn den gesamten bisherigen Kontext erneut als `cache_read` bezahlt.
Beide Faktoren wachsen im Lauf eines Agenten mit: **der Zusammenhang ist
quadratisch in der Turn-Zahl.** Ein Agent, der eine Aufgabe in 60 statt 120 Turns
erledigt, kostet nicht die Hälfte, sondern rund ein Viertel.

Gemessene Kontextverteilung über alle 1751 Turns:

| Kontext pro Anfrage | Anfragen |
|---|---|
| < 50k | 132 |
| 50k–100k | 723 |
| 100k–150k | 701 |
| 150k–200k | 136 |
| > 200k | 59 |

Größter Einzelkontext: **234k Tokens**. Durchschnitt: 105k.

### 4.1 Das Modell ist `claude-opus-5[1m]` — unbeabsichtigt

`agent.model` ist in `agency-os/.chief/config.yaml` nicht gesetzt. Der
`ClaudeProvider` hängt dann kein `--model` an (`internal/agent/claude.go:57`),
und Claude Code nimmt seinen eigenen Default. Der ist hier das Opus-Modell mit
1M-Kontextfenster — nachweisbar in allen 15 `system/init`-Zeilen des Logs.

Zwei Folgen:

1. **Alles läuft auf Opus**, auch die sieben Reviews und die Konsolidierung, die
   zusammen 40 % der Kosten ausmachen.
2. **Kein Auto-Compact.** Bei 1M Fenster gibt es die 200k-Grenze nicht, an der
   Claude Code den Kontext sonst zusammenfasst. Der Kontext wächst ungebremst
   weiter und wird in jedem folgenden Turn voll bezahlt. 837 der 1751 Turns
   liefen mit über 100k Kontext.

chief kennt heute nur ein einziges Modell für alle Phasen (`AgentConfig.Model`,
`internal/config/config.go`). Ein Review auf Sonnet ist nicht konfigurierbar.

### 4.2 `progress.md` ist auf 72 KB gewachsen

Der Build-Prompt (`embed/prompt.txt`, Schritt 1) weist jeden Agenten an, die
Datei zu lesen; chief übergibt nur den Pfad, nicht den Inhalt
(`embed.GetPrompt`). Jeder der 15 Agenten liest damit rund 20k Tokens ein und
trägt sie durch alle folgenden ~117 Turns mit. Über den ganzen Run sind das grob
**35 M Tokens**, allein für eine Datei, von der pro Story die
„Codebase Patterns"-Sektion und die letzten ein bis zwei Einträge relevant sind.

Die Datei wächst monoton: jede Story hängt einen Implementierungsbericht plus
einen Review-Bericht an. Der nächste Run auf demselben PRD startet also teurer
als dieser.

### 4.3 Sehr viele kleinteilige Shell-Aufrufe

774 Bash-Aufrufe im Run, aufgeschlüsselt nach erstem Kommando:

| Kommando | Aufrufe |
|---|---|
| `grep` | 220 |
| `php` (Tests/Artisan) | 193 |
| `git` | 76 |
| `sed` | 70 |
| `ls` | 65 |
| `cat` | 60 |
| `find` | 21 |
| `vendor/bin/pint` | 14 |

Dazu 182 `Read` (Ø 12,8 KB Ergebnis), 189 `Edit`, 27 `Write`.

Auffällig ist die Recherche: 220 `grep` plus 60 `cat` plus 70 `sed` sind über 350
Turns, die fast ausschließlich Codebase-Erkundung sind — und jeder davon zieht den
kompletten Kontext erneut durch die Abrechnung. Genau diese Arbeit wäre in
Subagenten mit eigenem Kontext billiger; der Explore-Agent taucht im Log kein
einziges Mal auf.

Die Testläufe sind dagegen bereits gut organisiert: 121 der Bash-Aufrufe liefen
als Background-Tasks (`system/task_started`, `task_type: local_bash`), oft
gebündelt (`for f in … php artisan test`) und mit gekappter Ausgabe (`| tail -5`).
Die Summe aller Tool-Ergebnisse beträgt nur 3,7 MB (~0,9 M Tokens) — die
Werkzeugausgaben sind **nicht** das Problem.

---

## 5. Hebel

Nicht umgesetzt, nur bewertet. Reihenfolge nach Wirkung pro Aufwand.

### H1 — Modell pro Phase konfigurierbar machen

`review.model` und `consolidate.model` in der `config.yaml`, Default Sonnet;
Opus bleibt beim Bauen. Betrifft `internal/config/config.go`, den Provider
(`SetModel` existiert bereits) und die Übergabe in `runReview` /
`runConsolidation`.

*Wirkung:* 40 % der Token-Kosten auf etwa ein Fünftel des Preises → rund
**$115 pro Run**, plus etwas Zeit, weil Sonnet schneller generiert.
*Risiko:* gering; der Reviewer prüft fertigen Code gegen ein Story-Kriterium.

### H2 — Kontext-Diät für `progress.md`

Statt „lies die Datei" nur die Patterns-Sektion und die letzten N Einträge in den
Prompt inlinen; optional die Datei ab einer Größe rotieren.

*Wirkung:* ~20k Tokens weniger in jedem Turn jedes Agenten, grob **$40–50 pro
Run**, und der Effekt wächst mit jedem weiteren Run auf demselben PRD.
*Risiko:* mittel — die Auswahl entscheidet, was der Agent über frühere Stories
weiß.

### H3 — Turn-Zahl senken (Prompt-Tuning)

Recherche in Subagenten verlagern (ein Explore-Subagent statt 40 `grep`-Turns im
Hauptkontext), `Read` statt `cat`/`sed`, Regressionsläufe weiter bündeln.

*Wirkung:* der stärkste Hebel, weil quadratisch. Eine Halbierung der
Recherche-Turns liegt in der Größenordnung **$80–120 pro Run** plus spürbar Zeit.
*Risiko:* höher — Eingriff in das Verhalten der Build-Agenten, Wirkung erst über
mehrere Runs beurteilbar.

### H4 — Review-Umfang an die Story koppeln

Der Review von SRH-001 kostete mit $29.79 mehr als der Build von SRH-002 ($20.06).
Denkbar: Review überspringen oder kürzen, wenn die Story wenige Dateien berührt.

*Wirkung:* punktuell, bis 15 %.
*Risiko:* Qualitätsverlust genau dort, wo er nicht auffällt.

### H5 — Schlaf während eines Runs verhindern bzw. sichtbar machen

`loop.keepAwake` ist verfügbar, aber in `agency-os` nicht gesetzt — und gegen
Clamshell-Sleep hilft `caffeinate` unter Apple Silicon ohnehin nicht. Realistisch:
Warnung beim Start, wenn `keepAwake` aus ist oder der Rechner auf Batterie läuft,
plus ein Hinweis nach dem Run, wenn Wanduhr und gemessene Zeit auseinanderlaufen.

*Wirkung:* keine Beschleunigung, aber der Run wäre an diesem Tag 56 Minuten früher
fertig gewesen.
*Risiko:* keins.

---

## 6. Reproduktion

Die Auswertung liest ausschließlich das Log:

- **Segmentierung:** jede `system/init`-Zeile beginnt einen neuen Agentenaufruf.
  Die Reihenfolge ist Build/Review paarweise pro Story, Consolidate zuletzt.
- **Dauer:** Differenz erster/letzter `timestamp` innerhalb eines Segments.
  Achtung: `system`-Zeilen (`thinking_tokens`, `rate_limit_event`) tragen keinen
  Timestamp, halten aber den Watchdog wach.
- **Kosten:** `message.usage` je `assistant`-Zeile, bewertet mit
  $15 / $75 / $18.75 / $1.50 pro Mio. (in / out / cache write / cache read).
  Ein `result`-Event gibt es nicht, weil chief den Prozess nach `<chief-done/>`
  killt (siehe Kommentar in `internal/loop/parser.go`).
- **Gegenprobe:** die `<!-- chief-timing … -->`-Zeilen in `progress.md` enthalten
  chiefs eigene Messung pro Story und decken sich mit der Segmentzuordnung.
