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
  summary: true      # write & commit a timestamped summary-<date>-<time>.md when the run finishes
  notify: true       # desktop notification when the run finishes
loop:
  watchdogTimeoutSeconds: 300   # kill a silent agent after N seconds; 0 = default (5 min)
review:
  enabled: true                 # run the review with just the built-in prompt (no extra config needed)
  skill: "/code-quality"        # optional project skill the review agent runs (Claude only)
  instructions: |               # optional free-form guidance for the review agent
    Watch for N+1 queries and make sure new behavior has tests.
```

### Config Keys

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `agent.provider` | string | `"claude"` | Agent CLI to use: `claude`, `codex`, `opencode`, `cursor`, or `gemini` |
| `agent.cliPath` | string | `""` | Optional path to the agent binary (e.g. `/usr/local/bin/opencode`). If empty, Chief uses the provider name from PATH. |
| `agent.model` | string | `""` | Optional model passed to the Claude CLI via `--model`. Needed when Claude Code's `-p` mode ignores `~/.claude/settings.json` (e.g. local models via LM Studio). |
| `worktree.setup` | string | `""` | Shell command to run in new worktrees (e.g., `npm install`, `go mod download`) |
| `onComplete.push` | bool | `false` | Automatically push the branch to remote when a PRD completes. Only runs if the branch has at least one commit. |
| `onComplete.createPR` | bool | `false` | Automatically create a pull request when a PRD completes (requires `gh` CLI). Only runs after a successful push, so a run with no commits creates no PR. |
| `onComplete.summary` | bool | `true` | When a run finishes (or hits max iterations with committed work), run the agent once more to write a human-facing, timestamped `summary-<date>-<time>.md` next to the PRD — what was built, how to test it, where the new functionality lives, and open/parked follow-ups. Each run writes its own file (sortable name), so the PRD keeps a run history. The file is committed automatically (force-added, so it lands even when `.chief/` is gitignored); with `onComplete.push` on, it rides along in the pushed branch/PR. Only runs when the branch has at least one commit. |
| `onComplete.notify` | bool | `true` | Send a desktop notification when a run finishes (macOS `osascript`, Linux `notify-send`). |
| `loop.watchdogTimeoutSeconds` | int | `0` | Seconds of agent silence (no output) before the watchdog kills the hung process. `0` uses the built-in default of 5 minutes. Raise it when the agent runs long, silent builds or test suites that would otherwise trip the watchdog. |
| `review.enabled` | bool | `false` | Turn the review agent on with just the built-in review prompt (the two-axis Spec/Standards review and code-smell baseline) — no skill or instructions needed. Setting `review.skill` or `review.instructions` also enables the review on its own, so this flag is only needed to run the bare baseline. |
| `review.skill` | string | `""` | Name of a project skill (e.g. `/code-quality`) the **separate review agent** runs as part of its review. Claude-specific; other providers ignore it. Optional — setting it also enables the review. |
| `review.instructions` | string | `""` | Free-form guidance for the review agent (e.g. "watch for N+1 queries and missing tests"). Works with any provider. Optional — setting it also enables the review. |

When `review.enabled` is true, or either `review.skill` or `review.instructions` is set, Chief spawns a **separate agent with a fresh context** after each story's build agent has committed. It never sees the build agent's reasoning, so it reviews the committed changes adversarially — like a colleague reviewing a PR rather than the author re-checking their own work. It fixes anything it finds and amends the story's commit (keeping one commit per story). The review is best-effort: a review crash is logged but does not un-complete the story.

See [The Review Agent](/concepts/code-review) for the full picture — why it's a separate agent, how it slots into the loop, and how the review prompt is assembled from your `skill`/`instructions`.

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

Settings are organized by section:

- **Worktree** — Setup command (string, editable inline)
- **On Complete** — Push to remote (toggle), Create pull request (toggle), Write run summary (toggle), Desktop notification (toggle)

Changes are saved immediately to `.chief/config.yaml` on every edit.

When toggling "Create pull request" to Yes, Chief validates that the `gh` CLI is installed and authenticated. If validation fails, the toggle reverts and an error message is shown with installation instructions.

Navigate with `j`/`k` or arrow keys. Press `Enter` to toggle booleans or edit strings. Press `Esc` to close.

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
