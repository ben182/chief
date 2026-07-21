# Changelog

All notable changes to Chief are documented in this file.

## [Unreleased]

### Features
- **Per-story cost and token usage** — the story details panel now shows what each story cost and the tokens it burned (`Cost: ~$X · N out · N in · N cache`), live while a story is in progress and final once it's done. This required a new data source: the estimate previously relied on Claude's `result` event, which never arrives because the loop kills the process on `<chief-done/>`, so cost and token totals were almost always blank. Usage is now summed from every assistant message (input/output/cache-creation/cache-read) and cost is derived from a per-model price table (`internal/loop/pricing.go`, Opus/Sonnet/Haiku Anthropic list prices; unknown models show tokens without a cost). The running total in the header and the per-story cost column on the completion screen are now populated too. Note the cost is an estimate — cache-read tokens dominate on long Opus runs with a large reused context
- **Larger PRD names in the TUI** — PRD names are now given more room before they get truncated: the tab bar shows up to 16 characters (9 in the compact bar for narrow terminals, up from 8/5), and the PRD list (`l`) widens the name column from 12 to 20 characters. The picker modal grew from 60 to 72 columns so the wider names still leave room for progress and branch info
- **Archive & restore PRDs** — old PRDs no longer have to clutter the tab bar. In the PRD list (`l`), press `a` to archive the selected PRD: its directory moves from `.chief/prds/<name>/` to `.chief/archive/<name>/`, so it drops out of the tab bar but stays intact. Archived PRDs appear in an "── Archived ──" section at the bottom of the list, where `u` restores the selected one back into `.chief/prds/`. Archiving is blocked for a running PRD (stop it first) and, when the archived PRD is the one currently in view, the dashboard switches to the next remaining PRD. Fully reversible — nothing is deleted
- **Batch grilling in rounds** — the `chief new`/`chief edit` interview no longer trickles out one question at a time through a native picker. It now follows a *batch-grill* method: the agent maps the open decisions as a tree and, each round, asks the whole *frontier* of currently-answerable questions at once — numbered, each with a recommended answer — in plain prose, then waits for your batch of answers before recomputing the frontier for the next round. Faster to get through, and questions that depend on unanswered ones are held for a later round instead of being guessed at. The `AskUserQuestion` picker is no longer used
- **`chief new` creates the PRD branch up front** — when run inside a git repo, `chief new` now creates (or checks out) the `chief/<name>` branch immediately, the same branch the loop uses when the PRD is later run, so the PRD and its implementation stay off your default branch. A git failure only warns and lets PRD authoring continue
- **`chief new`/`chief edit` skip permission prompts** — the interactive Claude session now launches with `--dangerously-skip-permissions` (matching the autonomous loop), so the PRD interview reads the repo and writes `prd.md` without a permission prompt on every tool call (Claude only)
- **Codebase exploration always runs on Opus** — during `chief new`/`chief edit`, the agent now delegates repository exploration to an Opus subagent regardless of which model you picked in the model picker. This means a lighter session model (e.g. Fable) can drive the grilling/conversation to save cost without degrading how well Chief reads your codebase (Claude only)
- **`chief new` asks what you want to build first** — the PRD-creation prompt now runs an explicit "Phase 0" that asks, in plain prose, what you want to build and why *before* exploring the codebase or grilling. Previously the agent inferred the feature from the PRD name alone and dove straight into codebase analysis and multiple-choice questions. Codebase exploration and grilling now happen only after the goal is clear (and the open question is skipped when `context` is passed on the command line)
- **Model picker for PRD create/edit** — `chief new` and `chief edit` now open an interactive Claude model select before launching the session (Default, Opus, Sonnet, Haiku, Fable, or a custom model ID). The choice is passed to the Claude CLI via `--model`, which the interactive session previously ignored entirely. Shown only for the Claude provider and skipped when a model is pinned via `--model`
- **Light-terminal support & plain-text mode** — the TUI palette is now adaptive: lipgloss detects the terminal background and swaps the pale dark-mode greens/yellows for darker, higher-contrast tones on light terminals (no config needed). Setting `NO_COLOR` strips all styling to plain text, and `CHIEF_ASCII=1` replaces the emoji/Unicode icons (status markers, log tool cards, `🎉`/`💡`/`📋`/`🔄`) with ASCII fallbacks for terminals, multiplexers, or piped logs that render them poorly
- **Settings shortcut is discoverable** — `,` (open Settings) now appears in the dashboard/log footer and the `?` help overlay, instead of being hidden
- **Run summary on completion** — when a run finishes (or hits max iterations with committed work), Chief runs the agent once more to write a timestamped `SUMMARY-<date>-<time>.md` next to the PRD: what was built, **how to test it**, where the new functionality lives, and any open/parked follow-ups. Each run writes its own file (sortable name), so a PRD keeps a history of its runs. The file is committed automatically (force-added so it lands even when `.chief/` is gitignored) and, when auto-push is on, rides along in the pushed branch/PR. On by default; toggle via settings (`,`) or `onComplete.summary: false` in `.chief/config.yaml`
- **Default PRD renamed `main` → `default`** — the unnamed PRD now lives at `.chief/prds/default/` to avoid confusion with git branch names ([#9](https://github.com/MiniCodeMonkey/chief/issues/9)). Existing `.chief/prds/main/` setups still load: bare `chief` falls back to `main` when no `default` exists. For `chief status`/`edit` on an old setup, pass the name explicitly (`chief status main`)
- **Desktop notification on completion** — when a run finishes, Chief pings the desktop (macOS `osascript`, Linux `notify-send`) so you don't have to babysit long loops. On by default; toggle via settings (`,`) or `onComplete.notify: false` in `.chief/config.yaml`
- **Per-story cost** — the dashboard shows a running USD total and the completion screen breaks cost down per story (Claude only; other providers don't report cost). Parsed from the agent's `result` event
- **Configurable watchdog timeout** — `loop.watchdogTimeoutSeconds` in `.chief/config.yaml` overrides the 5-minute default, so agents running long, silent builds or test suites no longer get killed as hung (`0` keeps the default)
- **Per-project code-quality review** — `review.skill` in `.chief/config.yaml` (e.g. `/code-quality`) makes the agent run a project-specific skill at the end of each iteration to review its changes against your standards, fix anything flagged, and only then commit. Because a story counts as done only once a matching commit lands, a review that blocks the commit automatically causes a retry. Empty by default (no review step)

### Bug Fixes
- **ETA now appears reliably, including for background PRDs** — the completion estimate is driven by observed per-story durations, but those were tracked only for the PRD currently on screen and were wiped on every tab switch. When juggling several PRDs (or letting one run in the background while viewing another), the timing data was almost always empty, so no ETA ever showed. Timings are now kept per PRD, recorded for every running PRD regardless of which is on screen, and no longer cleared when switching tabs. The threshold to show an ETA also dropped from 3 completed stories to 2 (only the first, unrepresentative exploration story is skipped), so small PRDs get an estimate too
- **Progress markdown is readable on light terminals** — inline code in the progress panel (`` `Foo` ``, file paths, method names) used glamour's default bright red on a grey block, rendered with a fixed dark theme regardless of the terminal background. On light terminals this showed up as an unreadable dark-red box. The progress renderer now matches the terminal background (light/dark) and styles inline code as a calm cyan accent with no background box
- The agent and its whole subprocess tree are now killed together (the agent runs in its own process group). Previously only the direct child was killed, orphaning the tool and MCP subprocesses it spawned, which piled up across iterations
- A warning is surfaced when the working directory is not a git repo: `<chief-done/>` can't be commit-verified and work isn't persisted between fresh-context iterations
- Stories are only marked `done` when a matching commit actually landed. If the agent emits `<chief-done/>` without committing (forgot, a hook rejected it, or it crashed), the story is treated as a failed attempt instead of being falsely completed, so uncommitted work is no longer silently lost
- `prd.md` is now written atomically (temp file + rename), so a crash mid-write can never truncate the source of truth. The file watcher survives atomic replacement (also fixes spurious "removed" events from editors that save atomically)
- Auto-push and auto-PR on completion only run when the branch has at least one commit, so a run with no committed work no longer creates an empty branch or pull request

## [0.7.0] - 2026-03-08

### Features
- **Pluggable agent backend** — in addition to Claude Code, Chief now supports OpenCode and Codex as agent CLIs thanks to @Simon-BEE and @tpaulshippy
- PRD robustness: handle large PRDs exceeding token limits, prevent merge/edit from truncating JSON, stable task IDs across PRD regeneration, watchdog for hung agents with no output, fix partially created PRD directories causing UI bugs, fix `.chief/` files committed despite gitignore
- Scrollable TUI task list with proper label wrapping

### Refactoring
- Deduplicate flag parsing and convert helpers with improved test coverage
- Inline story ID and title into commit message template

## [0.6.1] - 2026-02-24

### Bug Fixes
- Agent prompt now uses explicit `{{PROGRESS_PATH}}` template variable so progress.md is written next to prd.json instead of the working directory

## [0.6.0] - 2026-02-21

### Features
- Continuous, responsive confetti animation on the completion screen
- Quit confirmation modal when Ralph loop is running
- Live progress from progress.md in dashboard details panel
- Per-story timing and total duration on the completion screen

### Bug Fixes
- Match story commits by ID + title to prevent false positives
- Show uncommitted WIP changes when story has no commit yet
- Load claude.md on each iteration instead of only at startup
- Update elapsed time display every second while running
- Dynamically recalculate max iterations when switching PRDs

## [0.5.4] - 2026-02-20

### Bug Fixes
- TUI now manages story in-progress state directly on `EventStoryStarted`, fixing a race where the status was never shown
- TUI auto-selects the active story so its details are visible immediately
- Clear in-progress flags on completion, error, or max iterations
- Prevent non-JSON output from PRD conversion by disabling tools

## [0.5.3] - 2026-02-20

### Performance
- Cache pre-rendered log lines to eliminate per-frame TUI rebuilds

### Documentation
- Update documentation for v0.3.1–v0.5.2 release changes

## [0.5.2] - 2026-02-20

### Bug Fixes
- Log raw output when PRD JSON conversion fails, making it easier to diagnose parsing errors

## [0.5.1] - 2026-02-19

### Features
- Diff view now shows the commit for the selected user story instead of the entire branch diff
- Add `PgUp`/`PgDn` key bindings for page scrolling in log and diff views
- Diff header shows which story's commit is being viewed

### Bug Fixes
- Fix stale `GetConvertPrompt` test after inline content refactor
- Diff view now uses the correct worktree directory for PRDs with worktrees

## [0.5.0] - 2026-02-19

### Features
- Add version check and self-update command (`chief update`)
- Add diff view for viewing task changes
- Add `e` keybinding to edit current PRD directly
- Add live progress display during PRD-to-JSON conversion
- Add first-time setup post-completion config (auto-push, create PR)
- Add git worktree support for isolated PRD branches
- Add config system for per-project settings
- Improve PRD conversion UX with styled progress panel

### Bug Fixes
- Fix Rosetta 2 deadlock on Apple Silicon caused by oto/v2 audio library (#13)
- Fix missing `--verbose` flag for stream-json output

### Breaking Changes
- Remove `--no-sound` flag (sound feature removed entirely)

### Performance
- Inline prompt for PRD conversion instead of agentic tool use

## [0.4.0] - 2026-02-06

### Features
- Add `l` keybinding to open PRD picker in selection mode

### Bug Fixes
- Prevent Claude from implementing PRD after creation
- Let Claude write prd.json directly with better error handling

## [0.3.1] - 2026-02-04

### Bug Fixes
- Fix TUI becoming unresponsive after ralph loop completes

## [0.3.0] - 2026-01-31

### Features
- Add syntax highlighting for code snippets in log view
- Add editable branch name in branch warning dialog
- Add first-time setup flow with gitignore prompt

### Bug Fixes
- Launch Claude from project root for full codebase context

## [0.2.0] - 2026-01-29

### Features
- Add max iterations control with `+`/`-` keys
- Enhanced log viewer with tool call icons and full-width results
- Add branch protection warning when starting on main/master
- Add crash recovery with automatic retry

### Bug Fixes
- Remove duplicate "Converting prd.md to prd.json..." message

## [0.1.0] - 2026-01-28

Initial release.

### Features
- Core agent loop with Claude Code integration
- TUI dashboard with Bubble Tea
- PRD file watching and auto-conversion
- Parallel PRD execution
- Log viewer with tool cards
- PRD picker with tab bar
- Help overlay
- Narrow terminal support
- CLI commands: `chief new`, `chief edit`, `chief status`, `chief list`
- Homebrew formula and install script

[0.7.0]: https://github.com/MiniCodeMonkey/chief/compare/v0.6.1...v0.7.0
[0.6.1]: https://github.com/MiniCodeMonkey/chief/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/MiniCodeMonkey/chief/compare/v0.5.4...v0.6.0
[0.5.4]: https://github.com/MiniCodeMonkey/chief/compare/v0.5.3...v0.5.4
[0.5.3]: https://github.com/MiniCodeMonkey/chief/compare/v0.5.2...v0.5.3
[0.5.2]: https://github.com/MiniCodeMonkey/chief/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/MiniCodeMonkey/chief/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/MiniCodeMonkey/chief/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/MiniCodeMonkey/chief/compare/v0.3.1...v0.4.0
[0.3.1]: https://github.com/MiniCodeMonkey/chief/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/MiniCodeMonkey/chief/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/MiniCodeMonkey/chief/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/MiniCodeMonkey/chief/releases/tag/v0.1.0
