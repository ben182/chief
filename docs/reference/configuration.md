---
description: Chief configuration reference. Project config file, CLI flags, Settings TUI, and first-time setup flow.
---

# Configuration

Chief uses a project-level configuration file at `.chief/config.yaml` for persistent settings, plus CLI flags for per-run options.

## Config File (`.chief/config.yaml`)

Chief stores project-level settings in `.chief/config.yaml`. This file is created automatically during first-time setup or when you change settings via the Settings TUI.

### Format

```yaml
agent:
  provider: claude   # or "codex", "opencode", "cursor", or "gemini"
  cliPath: ""        # optional path to CLI binary
  model: ""          # optional model passed via --model (Claude only)
worktree:
  setup: "npm install"
onComplete:
  push: true
  createPR: true
  prBaseBranch: ""   # branch PRs merge into; empty = the branch the run's branch was cut from
  summary: true      # write & commit a timestamped summary-<date>-<time>.md when the run finishes
  notify: true       # desktop notification when the run finishes
loop:
  watchdogTimeoutSeconds: 300   # kill a silent agent after N seconds; 0 = default (5 min)
  keepAwake: true               # keep the machine awake while a loop is running (macOS)
review:
  enabled: true                 # run the review with just the built-in prompt (no extra config needed)
  model: ""                     # model the review agent runs on; empty = sonnet (Claude only)
  skill: "/code-quality"        # optional project skill the review agent runs (Claude only)
  instructions: |               # optional free-form guidance for the review agent
    Watch for N+1 queries and make sure new behavior has tests.
consolidate:
  enabled: true                 # one refactor pass over the whole run, after the last story
  model: ""                     # model the consolidation agent runs on; empty = sonnet (Claude only)
  skill: "/code-quality"        # optional project skill the consolidation agent runs (Claude only)
  instructions: |               # optional free-form guidance for the consolidation agent
    We keep all HTTP clients in internal/transport.
```

### Config Keys

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `agent.provider` | string | `"claude"` | Agent CLI to use: `claude`, `codex`, `opencode`, `cursor`, or `gemini` |
| `agent.cliPath` | string | `""` | Optional path to the agent binary (e.g. `/usr/local/bin/opencode`). If empty, Chief uses the provider name from PATH. |
| `agent.model` | string | `""` | Optional model passed to the Claude CLI via `--model`. Needed when Claude Code's `-p` mode ignores `~/.claude/settings.json` (e.g. local models via LM Studio). |
| `worktree.setup` | string | `""` | Shell command to run in new worktrees (e.g., `npm install`, `go mod download`) |
| `onComplete.push` | bool | `false` | Automatically push the branch to remote when a PRD completes. Only runs if the branch has at least one commit. |
| `onComplete.createPR` | bool | `false` | Automatically create a pull request when a PRD completes (requires `gh` CLI). Only runs after a successful push, so a run with no commits creates no PR. The PR targets the branch the run's branch was cut from, and an already-open PR for the branch is reported instead of a second one being opened — see [Pull request target](#pull-request-target). |
| `onComplete.prBaseBranch` | string | `""` | Forces the branch pull requests merge into. Empty (the default) lets Chief use the branch the run's branch was cut from. Set this only when that answer is wrong for your workflow. A branch `origin` doesn't have is ignored, leaving the choice to `gh`. |
| `onComplete.summary` | bool | `true` | When a run finishes (or hits max iterations with committed work), run the agent once more to write a human-facing, timestamped `summary-<date>-<time>.md` next to the PRD — what was built, how to test it, where the new functionality lives, and open/parked follow-ups. Each run writes its own file (sortable name), so the PRD keeps a run history. The file is committed automatically (force-added, so it lands even when `.chief/` is gitignored); with `onComplete.push` on, it rides along in the pushed branch/PR. Only runs when the branch has at least one commit. |
| `onComplete.notify` | bool | `true` | Send a desktop notification when a run finishes (macOS `osascript`, Linux `notify-send`). |
| `loop.watchdogTimeoutSeconds` | int | `0` | Seconds of agent silence (no output) before the watchdog kills the hung process. `0` uses the built-in default of 5 minutes. Raise it when the agent runs long, silent builds or test suites that would otherwise trip the watchdog. |
| `loop.keepAwake` | bool | `true` | Stop the machine from going to sleep while a loop is running. A run is a walk-away workflow — nobody touches the keyboard for an hour — so the OS would otherwise idle-sleep the machine mid-story and leave the agent frozen until you came back. macOS only (`caffeinate -i -s`, so the display still sleeps and only the machine stays up); a no-op on other platforms. Takes effect when a loop starts, so toggling it mid-run applies from the next start onwards. A closed lid still sleeps a MacBook without an external display — that's a firmware behavior no assertion can override, so a run started on battery is warned about before it begins (see [The Machine Fell Asleep Mid-Run](/troubleshooting/common-issues#the-machine-fell-asleep-mid-run)); setting this to `false` raises the same warning. |
| `review.enabled` | bool | unset | The hard switch, and it always wins. `true` runs the review with just the built-in prompt (the two-axis Spec/Standards review and code-smell baseline) — no skill or instructions needed. `false` keeps the review off even when `review.skill` or `review.instructions` are set, so you can park a skill in the config without it running. Left out entirely, a skill or instructions enable the review by themselves. |
| `review.model` | string | `""` (= `sonnet`) | Model the **review agent** runs on, e.g. `haiku`, `sonnet`, `opus`. Left empty, the review runs on **Sonnet** rather than on the build agent's model: reviewing one story's diff is a large share of a run's cost and doesn't need the build model. Claude-specific (passed via `--model`); other providers ignore it. `agent.model` is untouched either way — if your build agent runs on a model of its own (e.g. a local model via LM Studio), set this to the same value so the review reaches it too. |
| `review.skill` | string | `""` | Name of a project skill (e.g. `/code-quality`) the **separate review agent** runs as part of its review. Claude-specific; other providers ignore it. Optional — setting it also enables the review unless `review.enabled: false` says otherwise. |
| `review.instructions` | string | `""` | Free-form guidance for the review agent (e.g. "watch for N+1 queries and missing tests"). Works with any provider. Optional — setting it also enables the review unless `review.enabled: false` says otherwise. |

When `review.enabled` is true, or it is absent and either `review.skill` or `review.instructions` is set, Chief spawns a **separate agent with a fresh context** after each story's build agent has committed. It never sees the build agent's reasoning, so it reviews the committed changes adversarially — like a colleague reviewing a PR rather than the author re-checking their own work. It fixes anything it finds and amends the story's commit (keeping one commit per story). The review is best-effort: a review crash is logged but does not un-complete the story.

See [The Review Agent](/concepts/code-review) for the full picture — why it's a separate agent, how it slots into the loop, and how the review prompt is assembled from your `skill`/`instructions`.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `consolidate.enabled` | bool | unset | The hard switch, and it always wins. `true` runs one **consolidation pass** over the whole run after the last story finishes: a single agent looks across every commit the run landed and refactors away the seams that no per-story review can see. `false` keeps the pass off even when `consolidate.skill` or `consolidate.instructions` are set. Left out entirely, a skill or instructions enable the pass by themselves. |
| `consolidate.model` | string | `""` (= `sonnet`) | Model the **consolidation agent** runs on, e.g. `haiku`, `sonnet`, `opus`. Left empty, the pass runs on **Sonnet** rather than on the build agent's model, for the same reason the review does. Claude-specific; other providers ignore it. Set it to the same value as `agent.model` when your build agent runs on a model of its own. |
| `consolidate.skill` | string | `""` | Name of a project skill (e.g. `/code-quality`) the consolidation agent runs as part of its pass. Claude-specific; other providers ignore it. Optional — setting it also enables the pass unless `consolidate.enabled: false` says otherwise. |
| `consolidate.instructions` | string | `""` | Free-form guidance for the consolidation agent (e.g. "we keep all HTTP clients in `internal/transport`"). Works with any provider. Optional — setting it also enables the pass unless `consolidate.enabled: false` says otherwise. |

The review agent judges **one story at a time**, which leaves a blind spot no per-story check can cover. Because every story is built by a separate agent with a fresh context, two stories can each grow their own helper for the same job, or introduce competing patterns for one concern — and both commits still look correct in isolation. The damage only becomes visible when someone finally looks at the whole run.

The consolidation agent is that someone. It runs **once**, after the last story, and is the only agent that ever sees the entire run. What it looks for:

- parallel implementations of the same thing in different stories,
- competing patterns for one concern (two ways to handle errors, name things, access data),
- near-duplicate code that wants one abstraction,
- leftovers from abandoned approaches (dead code, unused helpers, debug output),
- drift from the codebase's own conventions.

Three properties keep it safe:

- **Scoped to this run.** The pass only ever sees `StartRef..HEAD` — the commits *this* run landed. A followup run never reopens work an earlier run already reviewed, summarized or pushed. When the run landed no commits, or the window can't be determined, the pass is skipped rather than let loose on the whole branch.
- **Pure refactor, own commit.** Behavior must not change, tests may not be bent to pass, and the work lands as one separate `refactor: consolidate <name> run` commit — never amended into a story commit — so it can be reviewed and reverted on its own. The prompt instructs the agent to run your project's checks and to **revert rather than commit** if it can't get them green.
- **Never blocks the run.** Like the review, it's best-effort: a crash or an unfinished pass is surfaced as an event, but the stories stay done. It runs *before* the run summary and push, so the summary describes the consolidated result and the PR carries it.

It's off by default, deliberately: it edits code that already worked and was already signed off.

### Pull request target

A pull request has to merge back into the branch the work came from. In a repo where feature branches come off `develop`, a PR opened against `main` is the wrong merge — it drags every commit `develop` is ahead by into the diff, and merging it puts unreviewed work on the release branch.

So Chief doesn't assume the default branch. It decides the base in this order:

1. **`onComplete.prBaseBranch`**, when you set it. An explicit answer always wins.
2. **The branch Chief cut the run's branch from.** Chief records this in the repo's git config (`branch.<name>.chiefbase`) at the moment it creates the branch, when the answer is still known for certain. Branches created from your current checkout record that branch; worktree branches are always cut from the default branch and record that.
3. **The closest ancestor in history**, for a branch Chief didn't create (or one created before this behavior existed). Chief asks every other branch how many commits the run's branch has added since they last shared a commit, and takes the branch that answers with the fewest — a branch cut from `develop` is a handful of commits past `develop`, but those commits *plus* everything `develop` gained since it left `main` past `main`. Ties go to the default branch, so two feature branches cut from the same point don't nominate each other.
4. **The default branch**, when there's nothing to go on.

A base that `origin` doesn't have is dropped rather than passed to `gh`, which would fail outright; `gh` then applies the repository default.

**Existing pull requests.** Before opening one, Chief checks whether the branch already has an open PR — the normal case for a followup run on the same branch. If it does, the push has already updated it, so Chief reports that PR (with its URL and target branch) instead of failing at `gh pr create`.

### Example Configurations

**Minimal (defaults):**

```yaml
worktree:
  setup: ""
onComplete:
  push: false
  createPR: false
```

**Full automation:**

```yaml
worktree:
  setup: "npm install && npm run build"
onComplete:
  push: true
  createPR: true
```

## Settings TUI

Press `,` from any view in the TUI to open the Settings overlay. This provides an interactive way to view and edit all config values.

Every key documented above has a row here — nothing is reachable only by hand-editing the YAML. Settings are organized by section:

- **Worktree** — Setup command (text)
- **On Complete** — Push to remote (toggle), Create pull request (toggle), PR base branch (text; empty = the branch the run's branch was cut from), Write run summary (toggle), Desktop notification (toggle)
- **Loop** — Keep machine awake (toggle), Watchdog timeout in seconds (number; empty = the built-in default)
- **Agent** — Provider (cycles through the supported CLIs; empty = `claude`), CLI path (text), Model (text)
- **Review** — Enabled (three-way), Model (text; empty = `sonnet`), Skill (text), Instructions (text)
- **Consolidate** — Enabled (three-way), Model (text; empty = `sonnet`), Skill (text), Instructions (text)

The two **Enabled** switches are three-way rather than on/off, matching the config: `Enter` cycles them **Default → Yes → No → Default**. On *Default* the value column shows what the setting currently resolves to — `Default (on)` once a skill or instructions are set, `Default (off)` otherwise — and no `enabled:` key is written to the file. Leaving a switch on *Default* is not the same as setting it to No, so editing an unrelated setting never freezes a derived pass at whatever it happened to resolve to at the time.

Changes are saved immediately to `.chief/config.yaml` on every edit.

When toggling "Create pull request" to Yes, Chief validates that the `gh` CLI is installed and authenticated. If validation fails, the toggle reverts and an error message is shown with installation instructions.

Navigate with `j`/`k` or arrow keys. The list scrolls when the terminal is too short for it; a `⋯` marks that it continues above or below. Press `Enter` to toggle booleans, cycle three-way switches and the provider, or edit text and number fields. Press `Esc` to close.

## First-Time Setup

When you launch Chief for the first time in a project, you'll be prompted to configure:

1. **Post-completion settings** — Whether to automatically push branches and create PRs when a PRD completes
2. **Worktree setup command** — A shell command to run in new worktrees (e.g., installing dependencies)

For the setup command, you can:
- **Auto-detect** (Recommended) — The agent analyzes your project and suggests appropriate setup commands
- **Enter manually** — Type a custom command
- **Skip** — Leave it empty

These settings are saved to `.chief/config.yaml` and can be changed at any time via the Settings TUI (`,`).

## CLI Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--agent <provider>` | Agent CLI to use: `claude`, `codex`, `opencode`, `cursor`, or `gemini` | From config / env / `claude` |
| `--agent-path <path>` | Custom path to the agent CLI binary | From config / env |
| `--model <model>` | Model passed to the Claude CLI via `--model` | From config / env |
| `--max-iterations <n>`, `-n` | Loop iteration limit | Dynamic |
| `--no-retry` | Disable auto-retry on agent crashes | `false` |
| `--verbose` | Show raw agent output in log | `false` |

Agent resolution order: `--agent` / `--agent-path` / `--model` → `CHIEF_AGENT` / `CHIEF_AGENT_PATH` / `CHIEF_MODEL` env vars → `agent.provider` / `agent.cliPath` / `agent.model` in `.chief/config.yaml` → default `claude`.

When `--max-iterations` is not specified, Chief calculates a dynamic limit based on the number of remaining stories plus a buffer. You can also adjust the limit at runtime with `+`/`-` in the TUI.

## Agent

Chief can use **Claude Code** (default), **Codex CLI**, **OpenCode CLI**, **Cursor CLI**, or **Gemini CLI** as the agent. Choose via:

- **Config:** `agent.provider: opencode` and optionally `agent.cliPath: /path/to/opencode` in `.chief/config.yaml`
- **Environment:** `CHIEF_AGENT=opencode`, `CHIEF_AGENT_PATH=/path/to/opencode`
- **CLI:** `chief --agent opencode --agent-path /path/to/opencode`

## Agent-Specific Configuration

Each agent has its own configuration. For example, when using Claude Code:

```bash
# Authenticate: run claude and follow the browser prompts
claude

# Pick a model interactively with /model inside the session,
# or let Chief pass one through with --model / agent.model
```

See the [Claude Code documentation](https://code.claude.com/docs/en/setup) for details.

When using Cursor CLI:

```bash
# Authentication (or set CURSOR_API_KEY for headless)
agent login
```

Chief runs Cursor in headless mode with `--trust` and `--force` so it can modify files without prompts. See [Cursor CLI documentation](https://cursor.com/docs/cli/overview) for details.

## Appearance

The TUI adapts to your terminal automatically and can be forced into plainer modes via environment variables.

- **Adaptive colors** — the color palette detects your terminal's background and picks a matching variant: the original bright theme on dark terminals, and darker, higher-contrast tones on light terminals (the dark-mode pale greens/yellows are nearly invisible on white). No configuration required.
- **`NO_COLOR`** — set to any non-empty value (per [no-color.org](https://no-color.org)) to strip all colors and styling. The TUI degrades to plain text.
- **`CHIEF_ASCII`** — set to a truthy value (`1`, `true`, `yes`, `on`) to replace Unicode/emoji icons with ASCII fallbacks. Useful for terminals, multiplexers, or piped logs that render emoji poorly (mis-measured widths break the layout).

| Variable | Effect |
|----------|--------|
| `NO_COLOR` | Any non-empty value disables all colors/styling (plain text). |
| `CHIEF_ASCII` | Truthy value (`1`/`true`/`yes`/`on`) switches status icons, log tool cards, and decorative glyphs to ASCII. |

Icon fallbacks in ASCII mode:

| Unicode | ASCII | Meaning |
|---------|-------|---------|
| `✓` | `v` | Passed |
| `●` | `*` | In progress |
| `○` | `.` | Pending |
| `✗` | `x` | Failed |
| `⚑` | `!` | Needs review |
| `📖 ✏️ 📝 🔨 🔍 🔎 🤖 🧐 🌐 ⚙️` | `[R] [E] [W] [$] [G] [/] [T] [S] [@] [*]` | Log tool cards (Read/Edit/Write/Bash/Glob/Grep/Task/Skill/Web/other) |

## Permission Handling

Some agents (like Claude Code) ask for permission before executing bash commands, writing files, and making network requests. Chief automatically configures the agent for autonomous operation by disabling these prompts.

::: warning
Chief runs the agent with full permissions to modify your codebase. Only run Chief on PRDs you trust.

For additional isolation, consider using [Claude Code's sandbox mode](https://code.claude.com/docs/en/sandboxing) or running Chief in a Docker container.
:::
