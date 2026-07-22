---
description: Complete CLI reference for Chief. All commands, flags, keyboard shortcuts, exit codes, and environment variables.
---

# CLI Reference

Chief provides a minimal but powerful CLI. All commands operate on the current working directory's `.chief/` folder.

## Usage

```
chief [command] [flags]
```

**Available Commands:**

| Command | Description |
|---------|-------------|
| *(default)* | Run the Ralph Loop on the active PRD |
| `start` | Launch the TUI and start the Ralph Loop immediately |
| `new` | Create a new PRD in the current project |
| `edit` | Open the PRD for editing |
| `status` | Show current PRD progress |
| `list` | List all PRDs in the project |

## Commands

### chief (default)

Launch the TUI dashboard for the active PRD. This opens Chief in **Ready** state—press `s` to start the Ralph Loop, which then reads your PRD, selects stories, and invokes the agent iteratively.

```bash
chief [name]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `name` | PRD name to run (optional, auto-detects if omitted) |

**Flags:**

| Flag | Description | Default |
|------|-------------|---------|
| `--max-iterations <n>`, `-n` | Maximum loop iterations | Dynamic |
| `--no-retry` | Disable auto-retry on agent crashes | `false` |
| `--verbose` | Show raw agent output in log | `false` |

**Examples:**

```bash
# Run with auto-detected PRD
chief

# Run a specific PRD by name
chief auth-system

# Increase iteration limit for large PRDs
chief --max-iterations 200

# Combine flags
chief auth-system -n 50 --verbose
```

::: info Dynamic iteration limit
This is a global runaway backstop only. Chief primarily limits work **per story** — a story that fails 5 times is parked as `needs-review` and the loop continues with others. When `--max-iterations` is not specified, the global limit is calculated dynamically from the remaining stories and their per-story attempt budget, so it rarely fires first. You can adjust it at runtime with `+`/`-` in the TUI.
:::

::: tip
If your project has only one PRD, Chief auto-detects it. Pass a name when you have multiple PRDs.
:::

---

### chief start

Same as the default command, but starts the Ralph Loop immediately instead of opening in **Ready** state. Skips the manual `s` keypress—useful for scripts, CI, or when you just want Chief to get going.

```bash
chief start [name]
```

Accepts the same arguments and flags as the default command (`name`, `--max-iterations`, `--no-retry`, `--verbose`, `--agent`, `--model`, ...).

**Examples:**

```bash
# Start the auto-detected PRD immediately
chief start

# Start a specific PRD by name
chief start auth-system

# Start with a custom iteration limit
chief start auth-system -n 50
```

::: info Branch safety
If you're on a protected branch (e.g. `main`) or another PRD is already running in the same directory, Chief still shows the branch/worktree confirmation before starting.
:::

---

### chief new

Create a new PRD in the current project. This command launches the agent CLI with a preloaded prompt to help you define your project requirements interactively.

```bash
chief new [name] [context]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `name` | PRD name (optional, defaults to `default`). Must contain only letters, numbers, hyphens, and underscores. |
| `context` | Additional context to pass to the agent (optional). Included in the PRD creation prompt. |

**How it works:**

1. When the agent is Claude, Chief first shows a model picker (see below) — pick which Claude model drives the session
2. If you're in a git repo, Chief creates (or checks out) a `chief/<name>` branch up front — the same branch the loop uses when the PRD is later run — so the PRD and its implementation land off your default branch. If the branch can't be created, Chief warns and continues so PRD authoring is never blocked
3. Chief launches the agent CLI with a specialized PRD-creation prompt. For Claude the session runs with `--dangerously-skip-permissions`, so it can read the repo and write `prd.md` without a permission prompt on every step
4. The agent first asks, in plain prose, **what you want to build and why** — before touching your codebase (it does not guess the feature from the PRD name). If you passed `context`, it plays that back to confirm instead of asking
5. Only once the goal is clear does it explore the repository, then grill you in **rounds**: it maps the decisions as a tree and asks the whole *frontier* of currently-answerable questions at once (each numbered, each with a recommended answer), waits for your batch of answers, then recomputes the frontier for the next round — no native multiple-choice picker, just plain prose you can confirm or redirect. Codebase exploration always runs on **Opus** (via a subagent), independent of the model you picked for the session — so a lighter session model (e.g. Fable) speeds up the conversation without degrading how well the repo is understood
6. After you confirm the shared understanding, the agent writes `prd.md` in one pass
7. When done, type `/exit` to leave the agent session
8. Chief validates the `prd.md` can be parsed
9. Chief scaffolds an empty `todos.md` [follow-up inbox](/concepts/chief-directory#todos-md-optional) next to `prd.md`, ready for the fixes and polish items you collect after the feature is built (fed to [`chief followup`](#chief-followup)). An existing inbox is never overwritten

**Model picker (Claude only):**

Before the session starts, Chief opens an interactive picker so you can choose the model for this PRD session:

- **Default** — no `--model` is passed; the Claude CLI uses its own configured model
- **Opus**, **Sonnet**, **Haiku**, **Fable** — passed to the CLI as `--model <alias>`
- **Custom…** — type any model ID (e.g. `claude-opus-4-8`)

The chosen model is passed to the Claude CLI via `--model`. The picker is skipped when you pin a model explicitly with the `--model` flag, and it does not appear for non-Claude agents. Pressing `Esc` cancels without creating the PRD.

The chosen model drives the **conversation** (grilling and PRD writing). Codebase exploration is always delegated to an **Opus** subagent regardless of this choice, so picking a lighter model like Fable trims cost on the back-and-forth without weakening how well Chief reads your repository.

**What it creates:**

```
.chief/
└── prds/
    └── <name>/
        └── prd.md       # Structured PRD (written with the agent)
```

**Examples:**

```bash
# Create a new PRD (defaults to name "default")
chief new

# Create a named PRD
chief new auth-system

# Create a PRD with additional context
chief new auth-system "We use Express.js with JWT tokens"

# The agent opens - describe what you want to build
# Type /exit when done - Chief generates the PRD files
```

::: info
Run `chief new` from the root of your project. Chief creates the `.chief/` directory if it doesn't exist.
:::

---

### chief edit

Open an existing PRD for editing via the agent CLI.

```bash
chief edit
```

Launches the agent with your PRD loaded, allowing you to refine requirements, add stories, or update `prd.md` conversationally. When you `/exit`, Chief validates the updated `prd.md` can be parsed.

Like `chief new`, this shows the [Claude model picker](#chief-new) before the session starts (Claude only; skipped when `--model` is set).

**Arguments:**

| Argument | Description |
|----------|-------------|
| `name` | PRD name to edit (optional, auto-detects if omitted) |

**Examples:**

```bash
# Edit the auto-detected PRD
chief edit

# Edit a specific PRD
chief edit auth-system
```

---

### chief followup

Convert a PRD's **follow-up inbox** into new user stories.

```bash
chief followup
```

After a PRD is implemented, you often find small fixes and polish items while
reviewing the finished work by hand. Drop them as a flat markdown checklist into
a `todos.md` (or `followups.md`) file inside the PRD's directory:

```markdown
- [ ] Media card should be hidden when no media is attached
- [ ] Add a download button to the media view
- [ ] "In use" badge counts the same post twice
```

`chief followup` launches the agent, which reads that inbox plus the existing
`prd.md`, and turns each **open** (`- [ ]`) item into a proper `### US-XXX` story
appended to `prd.md`:

- New stories get the **next available sequential ID** (no separate range) and
  start as `**Status:** todo`, so the normal loop picks them up next — completed
  stories are skipped by [story selection](/concepts/prd-format#story-selection-logic).
- When a follow-up refines an already-`done` story, its `**Blocked by:**` records
  that story's ID as a lineage note (a done blocker never holds the story back).
- Existing `done` stories are never reopened, keeping the one-commit-per-story
  history intact.
- Converted items are flipped to `- [x]` in the inbox with their new story ID, so
  re-running is idempotent.

Like `chief new`/`chief edit`, this shows the [Claude model picker](#chief-new)
before the session starts (Claude only; skipped when `--model` is set). It only
grills you about items whose intended behavior is genuinely ambiguous — clear
items are converted directly.

**Arguments:**

| Argument | Description |
|----------|-------------|
| `name` | PRD name whose inbox to ingest (optional, auto-detects if omitted) |

**Examples:**

```bash
# Ingest the auto-detected PRD's todos.md
chief followup

# Ingest a specific PRD's inbox
chief followup auth-system
```

---

### chief status

Show progress for the current PRD. Displays a summary of story completion at a glance.

```bash
chief status
```

**Output includes:**

- The project name (from the PRD's top-level heading)
- An `X/Y stories complete` count
- A list of the incomplete stories, with `(in progress)` next to the one currently being worked on (or `All stories complete!` when none remain)

**Examples:**

```bash
# Check progress on the auto-detected PRD
chief status

# Example output:
#   My Auth Project
#   5/8 stories complete
#
#   Incomplete stories:
#     US-006: Password Reset Flow (in progress)
#     US-007: Account Deletion
#     US-008: Two-Factor Auth
```

---

### chief list

List all PRDs in the current project.

```bash
chief list
```

Scans `.chief/prds/` and shows each PRD with its completion status.

**Examples:**

```bash
# List all PRDs
chief list

# Example output (format: <name>: <title> (<completed>/<total>, <pct>%)):
#   auth-system: My Auth Project (5/8, 62%)
#   landing-page: Marketing Site (12/12, 100%)
#   api-v2: Public API (0/6, 0%)
```

---

## Keyboard Shortcuts (TUI)

When Chief is running, the TUI provides real-time feedback and interactive controls:

### Loop Control

| Key | Action |
|-----|--------|
| `s` | **Start** the loop (when Ready, Paused, Stopped, or Error) |
| `p` | **Pause** the loop (finishes current iteration gracefully) |
| `x` | **Stop** the loop immediately (kills agent process) |

### View Switching

| Key | Action |
|-----|--------|
| `t` | **Toggle** between Dashboard and Log views |
| `d` | **Toggle** Diff view (shows the selected story's commit diff) |

### PRD Management

| Key | Action |
|-----|--------|
| `n` | Open **PRD picker** in create mode (switch PRDs or create new) |
| `l` | Open **PRD picker** in selection mode (switch between existing PRDs) |
| `1-9` | **Quick switch** to PRD tabs 1-9 |
| `e` | **Edit** current PRD (from any main view) |
| `m` | **Merge** completed PRD's branch into main (in picker or completion screen) |
| `c` | **Clean** worktree and optionally delete branch (in picker or completion screen) |

### Settings

| Key | Action |
|-----|--------|
| `,` | Open **Settings** overlay (from any view) |

### Navigation

| Key | Action |
|-----|--------|
| `j` / `↓` | Move down (stories in Dashboard, scroll in Log/Diff) |
| `k` / `↑` | Move up (stories in Dashboard, scroll in Log/Diff) |
| `Ctrl+D` / `PgDn` | Page down (Log/Diff view) |
| `Ctrl+U` / `PgUp` | Page up (Log/Diff view) |
| `g` | Jump to top (Log/Diff view) |
| `G` | Jump to bottom (Log/Diff view) |
| `+` / `=` | Increase max iterations by 5 |
| `-` / `_` | Decrease max iterations by 5 |

### General

| Key | Action |
|-----|--------|
| `?` | Show **help** overlay (context-aware) |
| `Esc` | Close modals/overlays |
| `q` | **Quit** (gracefully stops all loops) |
| `Ctrl+C` | Force quit |

::: tip
The TUI has three views: **Dashboard** showing stories and progress, **Log** streaming the agent's output in real time, and **Diff** showing the commit diff for the selected story. Press `t` to toggle Dashboard/Log, or `d` to open the Diff view.
:::

## Environment Variables

| Variable | Effect |
|----------|--------|
| `CHIEF_AGENT` | Agent CLI to use: `claude`, `codex`, `opencode`, `cursor`, or `gemini`. Overridden by `--agent`. |
| `CHIEF_AGENT_PATH` | Custom path to the agent CLI binary. Overridden by `--agent-path`. |
| `CHIEF_MODEL` | Model passed to the Claude CLI via `--model`. Overridden by `--model`. |
| `NO_COLOR` | Any non-empty value strips all colors/styling from the TUI ([no-color.org](https://no-color.org)). |
| `CHIEF_ASCII` | Truthy value (`1`/`true`/`yes`/`on`) replaces emoji/Unicode icons with ASCII fallbacks, for terminals that render them poorly. |

Agent resolution order: `--agent` / `--agent-path` / `--model` flags → `CHIEF_AGENT` / `CHIEF_AGENT_PATH` / `CHIEF_MODEL` → `.chief/config.yaml` → default `claude`. See [Configuration → Appearance](/reference/configuration#appearance) for the TUI appearance variables.

## Exit Codes

Chief uses standard exit codes:

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Error |
