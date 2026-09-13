# Laufanalyse: 12 chief-Läufe aus `agency-os` (31.08.–12.09.2026)

Nachfolger von [`2026-07-27-run-performance.md`](2026-07-27-run-performance.md).
Anlass: prüfen, welche der dort beschriebenen Hebel gewirkt haben und was den
Preis eines Laufs heute bestimmt.

**Datenbasis:** 12 stream-json-Logs aus vier PRDs (`blogpost-newsletter-native`,
`writing-rules`, `document-tables`, `db-prompts`) unter
`/Users/ben/Herd/agency-os/.chief/`, zusammen 78 MB und 73 Agentenläufe, dazu die
`chief-timing`-Zeilen aus den `progress.md` und zwei A/B-Messungen des
Startkontextes, die für diese Analyse in `agency-os` gefahren wurden.

---

## 1. Kurzfassung

| Kennzahl | Wert |
|---|---|
| Läufe / PRDs | 12 / 4 |
| Agentenaufrufe (`system/init`) | 73 |
| Turns (assistant messages) | 8 794 |
| Kontext gesamt durch die Abrechnung | **998 M Tokens** |
| Kosten (Abo-Äquivalenz) | **$1 998** |
| davon `cache_read` | $1 438 (**72 %**) |
| davon auf Sonnet (nur Konsolidierung) | $23 (1 %) |
| Ø Kontext pro Turn | **114 k** |
| Turns über 200 k Kontext | 1 013 (12 %) |
| Median je Story | $32 / 16 min |
| Teuerste Story | LFC-009: **$155** / 33 min / 416 Turns |

Die Struktur ist dieselbe wie im Juli: **drei Viertel des Geldes fließen in das
erneute Lesen von Kontext, nicht in erzeugten Code.** Was sich geändert hat: die
Reviews sind als Kostenblock verschwunden, dafür ist der Kontext pro Turn
gewachsen.

---

## 2. Was seit Juli gelandet ist

| Juli-Hebel | Status | Wirkung in den Daten |
|---|---|---|
| **H1** Modell pro Phase | ✅ umgesetzt | Konsolidierung läuft auf Sonnet: 12 Läufe, 12 Konsolidierungspässe, zusammen **$23**. Im Juli kosteten Review + Consolidate 40 % eines Laufs. Review ist in `agency-os` zusätzlich ganz aus (`review.enabled: false`). |
| **H2** Kontext-Diät für `progress.md` | ✅ als Prompt-Regel | Die Agenten lesen tatsächlich selektiv (`sed -n '1,80p'` + letzte Einträge) statt die Datei zu `cat`en. Kosten heute ~24 kB je Session statt 72 kB+. Aber: die `Codebase Patterns`-Sektion ist inzwischen selbst 60–80 Zeilen und wird pro Session 2–4× angefasst. |
| **H3** Turn-Zahl senken | ❌ offen | 70 % aller Werkzeugaufrufe sind weiterhin Recherche (siehe 3.4). |
| **H5** Systemschlaf | ✅ erledigt | Keine Schlafphasen mehr in den Logs; die größte Lücke über alle 12 Läufe ist 7 Minuten. Wanduhr und gemessene Arbeitszeit decken sich jetzt (264 min vs. 263 min im größten Lauf). |
| Watchdog | ✅ ruhig | Kein einziger Watchdog-Kill, keine geparkte Story, keine doppelt gebaute Story in 77 `chief-timing`-Einträgen. |
| Rate-Limit-Behandlung | ✅ frisch gefixt | `internal/loop/ratelimit.go` (noch uncommitted) fängt genau das Muster aus 3.5 ab. |

Die Ralph-Mechanik selbst läuft also sauber. Die verbleibenden Probleme sind alle
Kontext- und Budgetprobleme.

---

## 3. Was den Preis heute bestimmt

### 3.1 Der Kontext wächst innerhalb einer Session ungebremst

Verteilung über alle 8 794 Turns:

| Kontext pro Anfrage | Turns |
|---|---|
| < 50 k | 1 202 |
| 50–100 k | 3 207 |
| 100–150 k | 2 366 |
| 150–200 k | 1 001 |
| 200–250 k | 701 |
| 250–300 k | 189 |
| > 300 k | 123 |

Ø 114 k, Maximum **394 k**. Das Modell ist in allen Build-Sessions
`claude-opus-5[1m]` — bei 1 M Fenster greift die 200-k-Grenze nicht, an der Claude
Code sonst zusammenfasst. Der Kontext wächst bis zum Ende der Story weiter und
wird in jedem folgenden Turn voll als `cache_read` bezahlt.

Was ein Deckel brächte (obere Schranke, ohne die Kosten des Compact-Passes
gegenzurechnen):

| Deckel | eingesparter Kontext | Ersparnis über diese 12 Läufe |
|---|---|---|
| 200 k | 49 M Tokens | **$73** (4 %) |
| 150 k | 119 M Tokens | **$179** (9 %) |
| 100 k | 276 M Tokens | $414 (21 %) |

Die Wirkung ist real größer als die Tabelle sagt, weil der Zusammenhang
quadratisch ist: ein Compact senkt nicht nur den einen Turn, sondern alle
folgenden.

### 3.2 43 k Startkontext, davon 13 k Fremdkörper

Gemessen am 13.09. in `/Users/ben/Herd/agency-os`, jeweils eine Ein-Turn-Session
auf Sonnet:

| Start | Tools | Skills | Kontext Turn 1 |
|---|---|---|---|
| wie chief heute startet | 233 | 59 | **43 k** |
| `--strict-mcp-config` | 18 | 59 | 37 k |
| `--strict-mcp-config --disable-slash-commands` | 17 | 0 | **30 k** |

In jeder Build-Session hängen ~25 MCP-Server mit ~170 Tools (Gmail, Notion,
Todoist, Riverside, Breakcold, Apify, vidIQ, SE Ranking, Clay …) und ein Katalog
aus 59 Skills und 98 Slash-Commands — in einem Laravel-Repo, das davon nichts
braucht. Diese 13 k stehen in **jedem** Turn erneut in der Abrechnung:

```
13 k × 8 794 Turns = 114 M Tokens ≈ $171  (8,6 % der Laufkosten)
```

Der gesamte Startkontext (43 k, gemessen als Kontext des ersten Turns jeder
Session) macht über alle Läufe **$428 = 21 %** aus. Er ist nicht ganz vermeidbar
— Systemprompt, eingebaute Tools, der chief-Prompt und die Story gehören dazu —
aber ein knappes Drittel davon ist Konfigurationsrauschen der
Entwicklermaschine, das in einen autonomen Lauf gar nicht gehört.

Was von den 255 angebotenen MCP-Tools über **alle 20 Läufe seit August**
tatsächlich aufgerufen wurde:

| Tool | Aufrufe | Quelle |
|---|---|---|
| `laravel-boost__record-rule` | 93 | `.mcp.json` im Repo |
| `claude_ai_Notion__notion-fetch` / `notion-search` | 6 | Account-Konnektor |
| `laravel-boost__database-schema` / `database-query` | je 1 | `.mcp.json` im Repo |

**Fünf Tools von 255.** Nie aufgerufen: Gmail (29 Tools), Todoist (47), ploi (60),
Google Calendar (9), Quiz (13), Agency OS + Staging (30), herd (6), nightwatch (6),
context7 (2). Ebenso bei den Skills: aufgerufen wurden nur `code-review` (13×, das
ist chiefs eigener Review-/Konsolidierungspass) und `pest-plugin-agent` (3×).

**Korrektur zu einer früheren Fassung dieser Analyse:** `agency-os` hat sehr wohl
eine `CLAUDE.md` — 15,6 kB, seit dem 07.01.2026 im Repo. Ich hatte sie in der
`init`-Zeile des Logs nicht gefunden, aber dort steht nur `memory_paths`, und
darin taucht `CLAUDE.md` gar nicht auf. Sie ist Teil des Startkontextes, rund 4 k
davon.

Was sie ist, bleibt trotzdem relevant: die generierten Laravel-Boost-Richtlinien.
Sie enthält **keine einzige** Erwähnung von `Domains/` — der Architektur, in der
jede Story dieses Projekts gebaut wird. Das repo-spezifische Strukturwissen steht
ausschließlich in der `Codebase Patterns`-Sektion der jeweiligen `progress.md`,
und die ist PRD-lokal: jedes neue PRD fängt damit wieder bei null an.

Und sie schreibt vor, die Projekt-Skills zu benutzen — „You MUST activate the
relevant skill", `pest-plugin-agent` vor jeder Verifikation,
`testing-best-practices` vor Tests. Das entscheidet über V2b (siehe dort).

### 3.3 Einzelne Stories laufen aus dem Ruder, nichts bremst sie

Kosten je Story: Median $32, Ø $38, Maximum $155.

| Story | Dauer | Turns | Ø Kontext | Kosten |
|---|---|---|---|---|
| LFC-009 (Inline-Kommentare) | 33 min | 416 | 197 k | **$155** |
| PRMT-017 | 25 min | — | — | $97 |
| LFC-001 | 19 min | — | — | $78 |
| PRMT-001 | 40 min | — | — | $78 |

LFC-009 allein kostet so viel wie fünf mittlere Stories, die fünf teuersten
zusammen 17 % der Summe. chief misst die Kosten pro Story live (`chief-timing`),
aber es gibt keine Grenze: weder eine Turn- noch eine Budgetobergrenze je
Iteration. Eine Story, die sich verrennt, läuft, bis sie fertig ist oder der
Watchdog nach 5 Minuten Stille zuschlägt — und Stille ist genau das, was eine
sich verrennende Story nicht produziert.

### 3.4 Die Agenten lesen über die Shell, und sie lesen viel

Werkzeugnutzung über vier große Läufe (blogpost 11.09., writing-rules 07.09.,
document-tables 07.09., db-prompts 03.09.):

| Art | Aufrufe | Ø Ergebnis |
|---|---|---|
| `grep` (Suche) | 854 | 1,7 kB |
| `sed -n` (Datei lesen) | 669 | 4,6 kB |
| `cat` (Datei lesen) | 498 | 7,0 kB |
| `Read` | 221 | 6,3 kB |
| `Write` | 90 | — |
| Heredoc → Datei | 97 | — |
| `Edit` | 40 | — |
| `sed -i` | **10** | — |
| Datei-Edit per `python3` | **8** | — |

**Die Shell wird zum Lesen benutzt, nicht zum Editieren.** Die 223 `sed`-Aufrufe
aus einer früheren Fassung dieser Analyse waren fast alle `sed -n '80,160p'` —
Leseoperationen. Echte Shell-Edits an bestehenden Dateien gibt es 18. Die
Gegenprobe über git stimmt: der blogpost-Lauf hat **116 Dateien neu angelegt und
38 geändert** — zu den 40 `Edit` + 18 Shell-Edits passt das genau. Hier ist also
kein Problem.

Das Problem ist die Lesemenge. Über alle 74 Sessions:

| | |
|---|---|
| Lesevorgänge auf konkrete Dateien | **3 478** (47 pro Session) |
| davon Fortsetzungs-Reads derselben Datei (Stückeln) | 524 = 15 % |
| davon Dateien, die in derselben Session schon gelesen waren | 1 210 = 35 % |

Ein `sed -n '1,80p'` liefert im Schnitt 4,6 kB, ein `Read` 6,3 kB — die Shell
liest in kleineren Häppchen, und jedes Häppchen ist ein eigener Turn, der den
gesamten bisherigen Kontext erneut bezahlt. Allein die 524 Fortsetzungs-Reads
kosten grob **$90** über diese Läufe. Meistgestückelt: `progress.md` (74 Mal) und
große Testdateien.

Dazu kommt: Recherche läuft im teuren Hauptkontext. Subagenten, die dafür da sind
und ihren eigenen Kontext haben, wurden im größten Lauf **15 Mal in 17 Sessions**
benutzt — nicht einmal pro Story.

### 3.4.1 Woher das kommt: die CLI hat im August ihr Verhalten gedreht

Dieselben Zahlen über die Zeit, gleiches Repo, gleicher chief-Prompt:

| Lauf | CLI | `Read` | `Edit` | `cat` | `sed` | Heredocs |
|---|---|---|---|---|---|---|
| 10.08. linkedin-post-auto-scheduling | 2.1.226 | 163 | 115 | 74 | 36 | 14 |
| 11.08. better-fake-wizard | 2.1.227 | 141 | **192** | 141 | 75 | 22 |
| 24.08. generic-documents | 2.1.241 | 6 | **8** | 38 | 18 | 19 |
| 26.08. db-prompts | 2.1.246 | 80 | 24 | 570 | 435 | 419 |
| 07.09. writing-rules | 2.1.263 | 43 | 5 | 263 | 170 | 135 |
| 11.09. blogpost | 2.1.268 | 107 | 26 | 424 | 279 | 264 |

Zwischen 2.1.227 und 2.1.241 kippt das Verhalten. Der Grund steht im Systemprompt
von Claude Code selbst: im Bypass-Permissions-Modus — den chief mit
`--dangerously-skip-permissions` immer aktiviert — weist die CLI den Agenten an,
Arbeit durch die Shell zu erledigen (`cat`, `head`, `sed -n`, `grep`, Heredocs)
statt über `Read`/`Edit`/`Write`. Für einen interaktiven Lauf spart das
Berechtigungsdialoge. In einem chief-Lauf gibt es keine Dialoge zu sparen — der
Modus ist ja gerade deshalb an —, die Nebenwirkung bleibt aber: mehr Turns,
kleinere Häppchen.

Das ist kein chief-Fehler, aber chief ist die Stelle, an der man gegensteuern
kann.

### 3.5 Das Rate-Limit war die Folge, nicht die Ursache

Der Lauf vom 11.09. verbrauchte in **4h13m das komplette 5-Stunden-Fenster** und
lief um 18:26 in `status: rejected`. Danach:

```
18:26  Session 14   13 Turns   → "You've hit your session limit · resets 8pm"
18:27  Session 15    1 Turn    → dieselbe Meldung
18:27  Session 16    1 Turn    → dieselbe Meldung
18:27  Session 17    1 Turn    → dieselbe Meldung
```

Die CLI beendet sich dabei mit `result: success` — der Lauf sieht für chief aus
wie eine Iteration, die einfach kein `<chief-done/>` geschrieben hat, also wurde
dreimal in 20 Sekunden nachgesetzt. Der Rest des PRDs (LFC-014 + Konsolidierung,
14 Minuten Arbeit) wurde erst am nächsten Vormittag gefahren: **rund 16 Stunden
Wanduhr für 14 Minuten Arbeit.**

Der frische Fix in `internal/loop/ratelimit.go` fängt das ab. Was er nicht ändert:
dass ein Lauf dieser Größe **zwangsläufig** ins Fenster läuft. Bei $700
Äquivalenzwert in vier Stunden ist das Limit kein Zwischenfall, sondern das
erwartbare Ende. Alles unter 3.1–3.3 zahlt deshalb doppelt: auf den Preis und auf
die Reichweite eines Fensters.

---

## 4. Hebel

Nicht umgesetzt, nur bewertet. Reihenfolge nach Wirkung pro Aufwand.

### V1 — Kontextdeckel je Iteration (`--autocompact`)

Die Claude-CLI nimmt `--autocompact <auto|tokens>`. chief gibt das Flag heute nicht
weiter (`internal/agent/claude.go:61`, `LoopCommand`). Neuer Schlüssel
`agent.autocompact` in `AgentConfig` (`internal/config/config.go:129`), Default
z. B. `200000`, `auto` und `off` als Sonderwerte.

*Wirkung:* $73–179 über diese 12 Läufe, wachsend mit der Storygröße; nebenbei
deutlich mehr Reichweite pro Rate-Limit-Fenster.
*Risiko:* mittel — ein Compact mitten in einer Story kann Details verlieren. Bei
200 k liegt der Schnitt oberhalb von 88 % aller heutigen Turns, trifft also nur
die Ausreißer.

### V2 — MCP-Umfang je Projekt festlegen

Gemessen am 13.09. in `agency-os`, je eine Ein-Turn-Session auf Sonnet:

| Start | Tools | MCP-Tools | Startkontext |
|---|---|---|---|
| wie chief heute startet | 196–233 | 169 | 43–45 k |
| `--allowed-tools "mcp__laravel-boost …"` | 196 | 169 | 45 k |
| `--disallowed-tools "mcp__claude_ai_Gmail mcp__ploi …"` | 33 | 12 | 40 k |
| **`--strict-mcp-config --mcp-config <eigene Liste>`** | **74** | **53** | **39 k** |
| `--strict-mcp-config` (gar keine) | 18 | 0 | 37 k |
| `--strict-mcp-config --disable-slash-commands` | 17 | 0 | 30 k |
| **eigene Liste (boost + Notion) + `--disable-slash-commands`** | **73** | **53** | **21 k** (2× reproduziert) |

Zwei Ergebnisse, die die Umsetzung bestimmen:

1. **`--allowed-tools` filtert nicht.** Es ist eine Berechtigungsliste, keine
   Bestückungsliste — die 169 Tools bleiben im Kontext stehen. Für eine
   Positivliste taugt nur `--mcp-config` + `--strict-mcp-config`.
2. **Account-Konnektoren lassen sich in einer eigenen Liste nachbauen.** Eine
   Datei mit `laravel-boost` (stdio, aus der `.mcp.json` des Repos) und Notion
   (`{"type":"http","url":"https://mcp.notion.com/mcp"}`) meldet beide als
   `connected` — die vorhandene OAuth-Anmeldung trägt. Die Sorge, dass eine
   Positivliste den Notion-Zugang kostet, hat sich nicht bestätigt.

Vorschlag:

```yaml
agent:
  mcp: inherit            # Default, heutiges Verhalten: alles von der Maschine
  # mcp: none             # --strict-mcp-config
  # mcp: .chief/mcp.json  # --strict-mcp-config --mcp-config <pfad>
```

*Wirkung:* 43 k → 39 k Startkontext allein durch die MCP-Liste, rund **$50** über
diese 12 Läufe. Zusammen mit V2b (eigene Liste *und* kein Skill-Katalog) sind es
**43–45 k → 21 k**, zweimal reproduziert — über 8 794 Turns grob **$250**. Der
zweite Gewinn ist die Kontrolle: ein Lauf mit `--dangerously-skip-permissions`
hat heute 29 Gmail-, 47 Todoist- und 60 ploi-Tools in Reichweite, die er nie
gebraucht hat.
*Risiko:* gering. Der Default ändert nichts; Fehler beim Pfad müssen beim
Laufstart auffallen, nicht erst in der Iteration (`mcp_servers`-Status aus der
`init`-Zeile prüfen).

### V2b — Skill-Katalog für Build-Iterationen abschalten

`--disable-slash-commands` bringt weitere 37 k → 30 k, also mehr als die
MCP-Liste. Aufgerufen wurden in 20 Läufen nur `code-review` (chiefs eigener
Pass) und `pest-plugin-agent` (3×).

*Wirkung:* rund **$90** über diese 12 Läufe.
*Risiko:* zwei, und das zweite schließt es für `agency-os` aus:

- Das Flag darf nur für Build-Iterationen gelten, nie für Review/Konsolidierung,
  deren `skill` `/code-review` ist. Das ist eine Frage der Umsetzung.
- **Die `CLAUDE.md` dieses Repos verlangt die Skills.** Sie nennt
  `pest-plugin-agent` als Pflicht vor jeder Verifikation und
  `testing-best-practices` vor jedem Test, und unter `.claude/skills/` liegen 23
  Projekt-Skills. `--disable-slash-commands` nimmt die mit weg, nicht nur die 59
  globalen. Ein Agent, dem seine eigene Anweisung etwas vorschreibt, das er nicht
  mehr hat, ist schlechter dran als einer, der 7 k Katalog mitschleppt.

Für `agency-os` also **nein**, bewusst und dokumentiert (`skills: inherit` steht
mit dieser Begründung in der `config.yaml`). Für ein Repo ohne eigene Skills und
ohne solche Anweisung bleibt es ein sauberer Hebel.

**Umgesetzt am 13.09.** als `agent.mcp` (`inherit` | `none` | Pfad) und
`agent.skills` (`inherit` | `none`) in `.chief/config.yaml`; Details in
`docs/reference/configuration.md`.

In `agency-os` gesetzt: `mcp: .chief/mcp.json` mit `laravel-boost` und Notion,
`skills: inherit` aus dem oben genannten Grund. Gemessen im echten Projekt mit
genau dieser Konfiguration: **43–45 k → 27 k** Startkontext, beide Server
`connected`, Skill-Katalog unangetastet. Über die 8 794 Turns dieser Analyse wären
das grob **$200**.

Offen geblieben: eine Warnung, wenn ein in der
Liste genannter Server sich nicht verbindet. Geprüft wird heute die Datei (Pfad,
JSON, nicht-leere Serverliste) beim Start, nicht der Verbindungsstatus aus der
`init`-Zeile — dafür bräuchte es ein eigenes Ereignis und eine Darstellung in der
TUI, die es für Warnungen noch nicht gibt.

### V3 — Budgetbremse je Story (`--max-budget-usd`)

Die CLI kann sich selbst deckeln (`--max-budget-usd`, nur mit `-p`, was chief
ohnehin nutzt). Dazu ein chief-seitiges Ereignis, wenn eine Story den Deckel
reißt, und die bestehende Park-Mechanik (`DefaultMaxAttemptsPerStory`,
`internal/loop/loop.go:39`) darauf anwenden.

*Wirkung:* kein direkter Spareffekt — eine gedeckelte Story ist nicht fertig,
sondern abgebrochen. Der Nutzen ist die Vorhersagbarkeit: über die 77 erfassten
Stories lagen 9 über $60 und haben zusammen $230 über dieser Marke verbraucht,
ohne dass jemand davon erfahren hätte, bevor die Rechnung da war.
*Risiko:* mittel — eine abgebrochene Story muss sauber geparkt und sichtbar
gemacht werden, sonst verschwindet Arbeit. Deshalb erst nach V1/V2.

### V4 — Gegen den Shell-Reflex steuern (`--append-system-prompt`)

Die CLI-Anweisung aus 3.4.1 steht im Systemprompt; dagegen hilft am ehesten eine
Anweisung auf derselben Ebene. `--append-system-prompt` funktioniert und kommt
beim Agenten an (verifiziert: eine angehängte Formvorgabe wurde befolgt). chief
gibt das Flag heute nicht weiter — es käme neben `--model` in `LoopCommand`
(`internal/agent/claude.go:61`), gespeist aus einem Konfigurationsschlüssel.

Was nicht verifiziert ist: ob der *Inhalt* wirkt. Eine Mikro-Probe (Sonnet, zwei
Dateien, eine Änderung) reproduziert das Muster gar nicht erst — dort greift der
Agent von sich aus zu `Read` und `Edit`. Der Shell-Reflex zeigt sich in den echten
Läufen: Opus, großer Kontext, viele Dateien. Ein sauberes A/B braucht deshalb zwei
vergleichbare PRDs.

*Wirkung:* messbar sind $90 Stückelung plus ein Teil der 1 210 Wiederholungs-Reads;
der größere, nicht versprechbare Teil liegt in der Turn-Zahl, die quadratisch
durchschlägt.
*Risiko:* gering im Mechanismus, unklar in der Wirkung. Als Experiment fahren und
Turns/Story sowie $/Story aus den `chief-timing`-Zeilen vergleichen.

### V5 — Fenster-Vorschau vor dem Start

Ergänzung zum frischen Rate-Limit-Fix: Beim Start ist die letzte
`rate_limit_event`-Meldung bekannt (Auslastung + `resetsAt`). Wer um 16 Uhr einen
14-Story-Lauf startet, während das Fenster zu 70 % voll ist, sollte das vorher
sehen — inklusive der Schätzung aus `progress.md`, was ein Lauf dieser Größe
historisch verbraucht hat.

*Wirkung:* keine Kostenersparnis, aber genau die 16 verlorenen Stunden vom 11.09.
*Risiko:* keins.

### V6 — Strukturwissen aus `progress.md` in die `CLAUDE.md` heben (projektseitig)

Die `CLAUDE.md` von `agency-os` sind die generierten Laravel-Boost-Richtlinien:
PHP-, Pest- und Livewire-Konventionen, kein Wort über `Domains/`. Das
Strukturwissen, das ein Agent hier wirklich braucht — wo eine Domain registriert
wird, wo Factories liegen, wie `stage_data` funktioniert — steht in der
`Codebase Patterns`-Sektion der `progress.md` und ist damit an ein PRD gebunden.
Jedes neue PRD fängt wieder bei null an und erarbeitet es sich per `grep`.

Der allgemeine Teil davon gehört ins Repo, unterhalb der generierten Boost-Blöcke.

*Wirkung:* weniger Erkundung pro Session, quer über alle künftigen PRDs.
*Risiko:* keins — außer dass die Sektion gepflegt werden will.

---

## 5. Reproduktion

Die Auswertung liest ausschließlich die Logs; die Skripte liegen unter
`/tmp/chiefan/` (`an2.py` Segmentbilanz, `agg.py` Kostenaggregation, `ctx.py`
Kontextverteilung, `tools.py` Werkzeugklassifikation, `prog.py`
`progress.md`-Zugriffe).

- **Segmentierung:** jede `system/init`-Zeile beginnt einen Agentenaufruf.
  `timestamp` steht nur auf `assistant`- und `user`-Zeilen.
- **Kosten:** `message.usage` je `assistant`-Zeile, bewertet mit $15 / $75 /
  $18.75 / $1.50 je Mio. (in / out / cache write / cache read), Sonnet mit
  $3 / $15 / $3.75 / $0.30. Ein `result`-Event gibt es meist nicht, weil chief den
  Prozess nach `<chief-done/>` killt — außer bei Rate-Limit-Abbrüchen, die
  ironischerweise `subtype: success` melden.
- **Startkontext:** Kontext des ersten `assistant`-Turns einer Session.
  Die A/B-Messung in 3.2 lief als `claude -p "Antworte nur mit OK" --model sonnet`
  mit und ohne `--strict-mcp-config` / `--disable-slash-commands`.
- **Gegenprobe:** die `<!-- chief-timing … -->`-Zeilen in `progress.md` (im
  Worktree, nicht im Hauptverzeichnis) enthalten chiefs eigene Messung je Story.
