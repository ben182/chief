# Changelog

All notable changes to Chief are documented in this file.

## [Unreleased]

### Features
- **Run summary on completion** — when a run finishes (or hits max iterations with committed work), Chief runs the agent once more to write a `SUMMARY.md` next to the PRD: what was built, **how to test it**, where the new functionality lives, and any open/parked follow-ups. The file is committed automatically (force-added so it lands even when `.chief/` is gitignored) and, when auto-push is on, rides along in the pushed branch/PR. On by default; toggle via settings (`,`) or `onComplete.summary: false` in `.chief/config.yaml`
- **Default PRD renamed `main` → `default`** — the unnamed PRD now lives at `.chief/prds/default/` to avoid confusion with git branch names ([#9](https://github.com/MiniCodeMonkey/chief/issues/9)). Existing `.chief/prds/main/` setups still load: bare `chief` falls back to `main` when no `default` exists. For `chief status`/`edit` on an old setup, pass the name explicitly (`chief status main`)
- **Desktop notification on completion** — when a run finishes, Chief pings the desktop (macOS `osascript`, Linux `notify-send`) so you don't have to babysit long loops. On by default; toggle via settings (`,`) or `onComplete.notify: false` in `.chief/config.yaml`
- **Per-story cost** — the dashboard shows a running USD total and the completion screen breaks cost down per story (Claude only; other providers don't report cost). Parsed from the agent's `result` event
- **Configurable watchdog timeout** — `loop.watchdogTimeoutSeconds` in `.chief/config.yaml` overrides the 5-minute default, so agents running long, silent builds or test suites no longer get killed as hung (`0` keeps the default)
- **Per-project code-quality review** — `review.skill` in `.chief/config.yaml` (e.g. `/code-quality`) makes the agent run a project-specific skill at the end of each iteration to review its changes against your standards, fix anything flagged, and only then commit. Because a story counts as done only once a matching commit lands, a review that blocks the commit automatically causes a retry. Empty by default (no review step)

### Bug Fixes
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
