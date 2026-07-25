---
description: Understand the .chief directory structure where Chief stores all state. Self-contained, portable, and git-friendly.
---

# The .chief Directory

Chief stores all of its state in a single `.chief/` directory at the root of your project. This is a deliberate design choice — there are no global config files, no hidden state in your home directory, no external databases. Everything Chief needs lives right alongside your code.

## Directory Structure

A typical `.chief/` directory looks like this:

```
your-project/
├── src/
├── package.json
└── .chief/
    ├── config.yaml             # Project settings (agent, worktree, onComplete, loop, review)
    ├── prds/
    │   └── my-feature/
    │       ├── prd.md                        # Structured PRD (you write, Chief reads/updates)
    │       ├── progress.md                   # Progress log (Chief appends after each story)
    │       ├── todos.md                       # Optional follow-up inbox (fed to `chief followup`)
    │       └── claude-<timestamp>.log        # Raw agent output — one file per run (for debugging)
    ├── archive/                # Archived PRDs (hidden from the tab bar)
    │   └── old-feature/        # Same layout as a prds/ entry, restorable
    └── worktrees/              # Isolated checkouts for parallel PRDs
        └── my-feature/         # Git worktree (full project checkout)
```

The root `.chief/` directory contains:
- `config.yaml` — Project-level settings (see [Configuration](/reference/configuration))
- `prds/` — One subdirectory per PRD with requirements, state, and logs
- `archive/` — Archived PRDs, moved out of `prds/` so they no longer clutter the tab bar (created on first archive)
- `worktrees/` — Git worktrees for parallel PRD isolation (created on demand)

## The `prds/` Subdirectory

Every PRD lives in its own named folder under `.chief/prds/`. The folder name is what you pass to Chief when running a specific PRD:

```bash
chief my-feature
```

Chief uses this folder as the working context for the entire run. All reads and writes happen within this folder — the PRD state, progress log, and agent output are all scoped to the specific PRD being executed.

## File Explanations

### `prd.md`

The structured product requirements document. You write this file (or generate it with `chief new`). It contains freeform context at the top (background, technical notes, design guidance) and structured user stories that Chief parses and updates.

Chief reads this file at the start of each iteration to determine which story to work on, and updates status fields after completing a story. The agent also reads the freeform context to understand what you're building and how.

Key story fields (parsed from markdown):

| Field | Format | Description |
|-------|--------|-------------|
| ID + Title | `### US-001: Story Title` | Story heading parsed by Chief |
| Status | `**Status:** done\|in-progress\|todo` | Completion state, updated by Chief |
| Priority | `**Priority:** N` | Execution order (lower = higher priority) |
| Description | `**Description:** ...` | Story description |
| Acceptance Criteria | `- [ ]` / `- [x]` | Checkbox items tracked by Chief |

Chief selects the next story by finding the highest-priority story (lowest `**Priority:**` number) without `**Status:** done`. See the [PRD Format](/concepts/prd-format) reference for full details.

### `progress.md`

An append-only log of completed work. After each story, Chief adds an entry documenting what was implemented, which files changed, and lessons learned. This file serves two purposes:

1. **Context for future iterations** — Chief reads this at the start of each run to understand what has already been built and avoid repeating mistakes
2. **Audit trail** — You can review exactly what happened during each iteration

A typical entry looks like:

```markdown
## 2024-01-15 - US-003
- What was implemented: User authentication middleware
- Files changed:
  - src/middleware/auth.ts - new JWT verification middleware
  - src/routes/login.ts - login endpoint
  - tests/auth.test.ts - authentication tests
- **Learnings for future iterations:**
  - Middleware pattern uses `req.user` for authenticated user data
  - JWT secret is in environment variable `JWT_SECRET`
---
```

The `Codebase Patterns` section at the top of this file consolidates reusable patterns discovered across iterations — things like naming conventions, file locations, and architectural decisions that future iterations should follow.

Two optional agents append their own entry types here. A configured review agent adds a `## <date> - US-003 (review)` note recording what it checked and changed. The [consolidation pass](/concepts/code-review#the-consolidation-pass), if enabled, appends one `## <date> - consolidation` entry at the end of the run — what it consolidated, out-of-scope findings it deliberately left for a human, and the pattern the run *should* have followed from the start. That last part matters more than it looks: it's written by the only agent that ever saw the whole run, and it's read by the next run's fresh-context agents before they start diverging again.

### `todos.md` (optional)

A **follow-up inbox** you fill in by hand. `chief new` scaffolds an empty one
(comment-only) next to `prd.md`, so the inbox is there from the start; an
existing inbox is never overwritten. Once a PRD is implemented, you often spot
small fixes and polish items while reviewing the finished work. Jot them down
here as a flat markdown checklist:

```markdown
- [ ] Media card should be hidden when no media is attached
- [ ] Add a download button to the media view
```

Running [`chief followup`](/reference/cli#chief-followup) reads this file and the
existing `prd.md`, converts each open (`- [ ]`) item into a new `todo` user story
with the next available ID (and a `Blocked by` lineage note when it refines a
`done` story), then flips the consumed items to `- [x]`. The loop picks up the
new stories on the next run. `followups.md` and `follow-ups.md` work as
alternative names.

### `claude-<timestamp>.log`

Raw output from the agent during execution. This file captures everything the agent outputs, including tool calls, reasoning, and results. It's primarily useful for debugging when something goes wrong.

Each run writes its own timestamped file — `claude-2026-02-18-143012.log` (or `codex-`, `opencode-`, `cursor-`, `gemini-` depending on your agent), so previous runs' logs stay around rather than being overwritten. These files can get large (multiple megabytes per run). You typically don't need to read them unless you're investigating an issue, and you can safely delete old ones.

## The `worktrees/` Subdirectory

When you run multiple PRDs in parallel, each PRD can get its own isolated git worktree under `.chief/worktrees/`. A worktree is a full checkout of your project on a separate branch, so parallel agent instances never conflict over files or git state.

```
.chief/worktrees/
├── auth-system/         # Full checkout on branch chief/auth-system
└── payment-integration/ # Full checkout on branch chief/payment-integration
```

Worktrees are created when you choose "Create worktree + branch" from the start dialog. Each worktree:
- Has its own branch (named `chief/<prd-name>`)
- Is a complete copy of your project
- Runs the configured setup command (e.g., `npm install`) automatically

You can merge completed branches via `m` in the picker, and clean up worktrees via `c`.

## Archiving PRDs

Every folder under `.chief/prds/` shows up as a tab across the top of the TUI. Once a feature ships, its PRD keeps taking up a tab even though you're done with it. Archiving moves a PRD out of the way without deleting anything.

Open the PRD list with `l`, select a PRD, and press:

- `a` — **archive** the selected PRD. Its folder moves from `.chief/prds/<name>/` to `.chief/archive/<name>/`, so it disappears from the tab bar. If you archive the PRD you're currently viewing, the dashboard switches to the next remaining PRD.
- `u` — **restore** (unarchive) the selected PRD. Its folder moves back into `.chief/prds/<name>/` and the tab reappears.

Archived PRDs are listed in a separate **Archived** section at the bottom of the picker, so they stay discoverable and restorable.

```
.chief/
├── prds/
│   └── auth-system/       # Active — shown as a tab
└── archive/
    └── old-experiment/    # Archived — hidden from tabs, restorable with `u`
```

A few things to know:

- **Nothing is deleted.** Archiving and restoring are just directory moves — the `prd.md`, `progress.md`, and logs travel with the folder. To delete a PRD for good, remove its folder from `.chief/archive/` (or `.chief/prds/`) yourself.
- **Stop the loop first.** A running PRD can't be archived; stop it with `x` before archiving.
- **Worktrees are left alone.** Archiving only moves the PRD folder. If the PRD has a git worktree, clean it up first with `c` if you no longer need it.

## The `config.yaml` File

Project-level settings are stored in `.chief/config.yaml`. This file is created during first-time setup or when you change settings via the Settings TUI (`,`).

```yaml
agent:
  provider: claude   # claude (default) | codex | opencode | cursor | gemini
worktree:
  setup: "npm install"
onComplete:
  push: true
  createPR: true
  summary: true      # write & commit a timestamped summary file when a run finishes
  notify: true       # desktop notification when a run finishes
loop:
  watchdogTimeoutSeconds: 0   # 0 = built-in default (5 min)
review:
  instructions: ""   # optional guidance to enable the separate review agent
```

Every key is optional — Chief fills in defaults for anything you omit. See [Configuration](/reference/configuration) for the full list of settings and their defaults.

## Self-Contained by Design

Chief has no global configuration. There is no `~/.chiefrc`, no `~/.config/chief/`, no environment variables required. Every piece of state Chief needs is inside `.chief/`.

This means:

- **No setup beyond installation** — Install the binary, run `chief new`, and you're ready
- **No conflicts between projects** — Each project has its own isolated state
- **No "works on my machine" issues** — The state is the same for everyone who clones the repo
- **No cleanup needed** — Delete `.chief/` and it's as if Chief was never there

## Portability

Because everything is self-contained, your project is fully portable:

```bash
# Move your project anywhere — Chief picks up right where it left off
mv my-project /new/location/
cd /new/location/my-project
chief  # Continues from the last completed story
```

```bash
# Clone on a different machine — same state, same progress
git clone git@github.com:you/my-project.git
cd my-project
chief  # Sees the same PRD state as the original machine
```

This also works for remote servers. SSH into a machine, clone your repo, and run Chief — no additional setup required.

## Multiple PRDs in One Project

A single project can have multiple PRDs, each tracking a separate feature or initiative:

```
.chief/
├── config.yaml
├── prds/
│   ├── auth-system/
│   │   ├── prd.md
│   │   └── progress.md
│   ├── payment-integration/
│   │   ├── prd.md
│   │   └── progress.md
│   └── admin-dashboard/
│       ├── prd.md
│       └── progress.md
└── worktrees/
    ├── auth-system/
    └── payment-integration/
```

Run a specific PRD by name:

```bash
chief auth-system
chief payment-integration
```

Each PRD tracks its own stories, progress, and logs independently. When running multiple PRDs in parallel, each gets its own git worktree and branch for full isolation. You can run them simultaneously without worrying about file conflicts or interleaved commits.

## Git Considerations

You have two options depending on whether you want to share Chief state with your team.

### Option 1: Keep It Private

If Chief is just for your personal workflow, ignore the entire directory:

```gitignore
# In your repo's .gitignore
.chief/
```

Or add it to your global gitignore to keep it private across all projects without modifying each repo:

```bash
# Check if you have a global gitignore configured
git config --global core.excludesFile

# If not set, create one
git config --global core.excludesFile ~/.gitignore

# Then add .chief/ to that file
echo ".chief/" >> "$(git config --global core.excludesFile)"
```

### Option 2: Share With Your Team

If you want collaborators to see progress and continue where you left off, commit everything except the log files. You don't have to configure this yourself: Chief automatically drops a scoped `.gitignore` containing `*.log` into each PRD directory, so the per-run log files stay out of version control while `prd.md` and `progress.md` remain committable.

If you'd rather manage it at the repo root, use a `*.log` glob (the older `.chief/prds/*/claude.log` pattern no longer matches, since logs are now named `claude-<timestamp>.log`):

```gitignore
# In your repo's .gitignore
.chief/prds/*/*.log
```

This shares:
- `prd.md`: Your requirements and story state — the source of truth for what to build and what's done
- `progress.md`: Implementation history and learnings, valuable project context

The agent log files are large, written fresh each run, and only useful for debugging.

## What's Next

- [PRD Format](/concepts/prd-format) — Learn how to write effective PRDs
- [The Ralph Loop](/concepts/ralph-loop) — Understand what happens during execution
- [CLI Reference](/reference/cli) — See all available commands
