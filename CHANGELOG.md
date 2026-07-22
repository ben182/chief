# Changelog

All notable changes to Chief are documented in this file.

## [Unreleased]

### Features
- **Story dependency edges + frontier-based selection** — stories can now declare cross-story ordering with an optional `**Blocked by:** US-001, US-002` line: a comma-separated list of story IDs that must be `done` before the story may start. Selection is no longer a flat priority sort. Chief still resumes any in-progress, non-parked story first, but otherwise it picks the lowest-`Priority` story on the *frontier* — the stories that are not done, not needs-review, and whose every blocker is already `done` — with ties broken by document order. This lets `Priority` do just one job (ordering among work that's actually unblocked) while real "this must come before that" constraints live in `Blocked by`. Parsing is deliberately forgiving so an authoring mistake can never deadlock the loop: an unknown/typo blocker ID is treated as satisfied, a self-reference is ignored, and duplicates are harmless. If nothing on the frontier is eligible but unfinished non-parked work still remains (a dependency cycle, or everything left is blocked by a parked `needs-review` story), Chief falls back to the lowest-priority unfinished non-parked story so the loop always makes progress. Fully backward compatible — a PRD with no `Blocked by` lines behaves exactly as it did before
- **Test-driven development at seams** — `chief new` now bakes testing strategy into the PRD and the loop instead of leaving it to chance. When a feature has testable logic, the generated PRD grows a **"Testing Decisions"** section that records the *seams* to test at (a seam is the public boundary where behavior can be observed without reaching inside — the guidance prefers existing seams, the highest seam, and the fewest, ideally one), what makes a good test here (verifying observable behavior through the public interface rather than implementation details), and prior art to mirror. During grilling the agent sketches the seams and confirms them with you; how the existing tests look is a fact it looks up, not a question it asks. The build agent then works **test-first in vertical slices** — one failing test, minimal code to pass it, repeat — testing at the named seam and steering clear of two anti-patterns: *implementation-coupled* tests (asserting private state, mocking internal collaborators, or querying a DB instead of the interface, so they break on a refactor even when behavior is unchanged) and *tautological* tests (the expected value is recomputed the same way the code does it, so it can never disagree — expected values must come from an independent source such as a known-good literal or the spec). The review agent now flags both anti-patterns too and checks that new behavior is covered at the right seam
- **Throwaway HTML prototypes during PRD grilling (Claude only)** — when a UI, layout, or interaction decision would be faster to settle by *seeing* it than describing it, the `chief new` agent now proactively proposes building a small, self-contained throwaway HTML prototype. It doesn't wait for a deadlock — it offers one whenever it would sharpen a decision, says what the prototype would show and which decision it resolves, and waits for your go-ahead (it never builds one unprompted). On approval it delegates the build to an **Opus subagent** (the same Opus-subagent pattern already used for codebase exploration), producing a single static `*.html` file with inline CSS/JS — no build step, no dependencies — written into the PRD directory under `.chief/`, which is never committed, so it can't pollute the repo. You open it, and your reaction resolves the decision; the decision itself is then captured in the PRD (Design & Frontend section plus the relevant acceptance criteria), which is the durable artifact. The HTML is disposable — throw it away or keep and reference it from the PRD — and never becomes the actual implementation. Claude only, since it relies on subagents
- **Separate review agent (replaces the inline review step)** — `review` no longer injects a "run this skill before you commit" line into the build agent's own prompt. Instead, once a story's build agent has committed, Chief spawns a **separate agent with a fresh context** that reviews the committed changes adversarially — it never sees the build agent's reasoning, so it acts like a colleague reviewing a PR rather than the author re-checking their own work. It fixes anything it finds and amends the story's commit (one commit per story stays intact). The config grew a free-form field so you can steer the reviewer beyond a skill: `review.skill` (optional project skill, Claude-only) and `review.instructions` (optional free-form guidance like "watch for N+1 queries and missing tests", works with any provider). Setting either enables the review. The review phase shows up in the log: when a review is configured the build agent's story marker reads `✓ Build done — review pending` in yellow (instead of the final green `✓ Story done`), then `🔍 Reviewing changes`, and finally a green `✓ Review complete` once the reviewer signs off — so it's clear the build agent's `<chief-done/>` isn't the last word. Best-effort: a review crash is logged but never un-completes the story
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
- **Run summary on completion** — when a run finishes (or hits max iterations with committed work), Chief runs the agent once more to write a timestamped `summary-<date>-<time>.md` next to the PRD. The summary is **product-level** — what was built and what changed from the user's point of view, and **how to try it** (where to click, what looks different) — not a technical, file-by-file inventory. It is scoped to **exactly the commits this run made for the PRD's own user stories** (matched by their `feat: <ID> - <title>` subjects), so unrelated work sitting on the same branch — including same-numbered stories from other PRDs — never leaks in. Each run writes its own file (sortable, lowercase name), so a PRD keeps a history of its runs. The file is committed automatically (force-added so it lands even when `.chief/` is gitignored) and, when auto-push is on, rides along in the pushed branch/PR. On by default; toggle via settings (`,`) or `onComplete.summary: false` in `.chief/config.yaml`
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
- **Parallel-PRD deadlock closed** — `Manager.Start` took the per-instance lock and then reached for the manager-wide lock, the reverse of the order `GetAllInstances`/`GetRunningPRDs` use. With several PRDs running while the dashboard polled their state, that AB-BA ordering could deadlock under load. Start now snapshots the manager fields it needs under the manager lock first, then takes the instance lock, so the two are always acquired in the same order
- **Run logs no longer interleave mid-line** — the agent's stdout and stderr are logged from separate goroutines that wrote to the log file without synchronization, so lines could splice into one another and corrupt the stream-json log. Writes are now serialized behind a dedicated mutex
- **Failed `prd.md` status writes stop the run instead of looping forever** — marking a story `done` or parking it `needs-review` is the source of truth for what's left to do, but a failed write was silently swallowed, so the loop would pick the same story again on the next iteration and never make progress (burning iterations). The failure is now logged, surfaced as an error event, and stops the run so the cause (e.g. a read-only filesystem) can be fixed

### Performance
- **Tab bar no longer rescans every PRD on each streaming chunk** — `tabBar.Refresh()` re-reads and re-parses every `prd.md` from disk, and it was called on every loop event. Streaming events (assistant text, tool calls, token usage) fire many times per second, so each chunk triggered a full directory scan of `.chief/prds/`. Refresh now runs only on events that change a tab's displayed state (iteration start, story done/needs-review, complete, error, max-iterations)
- **Lock-free liveness tracking in the loop's hot path** — the agent-output reader took the loop's mutex on *every* line just to stamp `lastOutputTime`, contending with the watchdog and every getter on high-volume JSON streams. It is now an `atomic.Int64` (Unix nanos), so the per-line path and the watchdog read/write it without locking
- **Regexes compiled once instead of per call** — the PRD-name validator (ran on every keystroke in the name field) and the story-status heading matcher (ran on every status write) compiled their regex on each invocation. Both now use package-level regexes
- **Static styles hoisted out of per-frame render paths** — ~200 `lipgloss.NewStyle()` calls sat inside `View()`/`Render()` methods that run every frame (the completion screen re-styles every 50 ms while the confetti animates). The static ones moved to package-level vars in `styles.go`, and glamour markdown rendering in the details panel is now memoized per `(markdown, width)` instead of re-rendered each frame
- **`progress.md` parsed in a single scan** — `ParseProgress` and `ParseTimings` each opened and scanned the same file with the same regexes; callers needing both read the file from disk twice. A new `ParseProgressFile` does one scan and the two functions are thin wrappers

### Refactoring
- **CLI parsing extracted into `internal/cli`** — `cmd/chief/main.go` (667 lines) mixed subcommand dispatch, flag parsing, PRD-path resolution, provider setup and ~70 lines of ASCII art in one untested file. Argument parsing (`ParseArgs`, `AgentFlags`, `PRDPathFromArg`) and PRD lookup (`FindAvailablePRD`/`ListAvailablePRDs`) now live in a unit-tested `internal/cli` package that returns errors instead of calling `os.Exit`; help text and the ASCII art moved to their own file. `main.go` drops to 373 lines and is a thin orchestrator
- **Central PRD path helpers** — the `.chief/prds/<name>/prd.md` convention was hardcoded in ~15 places. New `prd.PRDDir`/`prd.PRDPath` helpers (alongside the existing `PrdsDir`) centralize it so a layout change is a one-line edit
- **git command helpers** — the `internal/git` package repeated the same `exec.Command("git", …)` / `cmd.Dir` / output-and-error dance ~34 times. Three helpers (`runGit`, `runGitRaw`, `runGitChecked`) fold it together and centralize error handling, trimming `git.go` from 303 to 253 lines with no behavior change
- Added test coverage for the cost/pricing table (`internal/loop/pricing.go`) and `git.IsChiefIgnored`, plus full coverage of the new `internal/cli` package
- **`internal/tui/app.go` split from 2808 to 1428 lines** — the file was a single God-object acting as model, controller and renderer for every screen. Its distinct concerns moved to their own files in the same package (no API change): `messages.go`, `loop_control.go`, `auto_actions.go`, `timing.go`, `ticks.go`, `picker_actions.go`, plus shared UI helpers in `modal.go`, `format.go`, `ansi.go` and `scrollable.go`
- **Modal and header rendering deduplicated** — `centerModal` was copy-pasted 8 times across the screens; it is now a single helper in `modal.go`, joined by a `modalBoxStyle`/`dividerLine` pair and a `headerBar` helper that collapse ~10 hand-built modal boxes and the 6 near-identical header renderers. A new `Scrollable` interface unifies the log and diff viewers, removing the paired `if ViewLog … else if ViewDiff` branches from the update switch
- **`Loop.Run` and the parsers decomposed** — the 170-line `Run` shed its story-completion/commit-gate logic into `finalizeStory` and its log setup into `openRunLog` (`finalize.go`); the process-exit classification became a pure, unit-tested `classifyExit`. The five agent parsers shared their `<chief-done/>` detection and line-decoding boilerplate into `parse_common.go`, the review concern moved into a `reviewer` type (`reviewer.go`) threaded via an explicit `iterationMode` instead of a shared flag, and the manager grew `lookup`/`snapshot` helpers that fold 7+ repeated locking blocks
- **`runTUIWithOptions` untangled and de-recursed** — the CLI entry point mixed provider resolution, PRD discovery, first-time setup, migration, error rendering and post-exit relaunch, using recursion to restart the TUI. It is now split into `resolvePRDPath`/`runFirstTimeSetup`/`maybeMigrate`/`handlePostExit` with the restart driven by a `for` loop, removing the unbounded recursion
- **Shared helpers across the small packages** — generic NDJSON parsing (`internal/agent/ndjson.go`) replaces three copied `CleanOutput` loops; a generic `fileWatcher[T]` (`internal/prd/filewatcher.go`) backs both watchers; `git` errors now wrap the failing command with `%w`, `GetDiff`/`GetDiffStats` share a `diffRange` helper, and `firstNonEmpty`, `resolveBaseDir`, `prd.ChiefDir` and `(*PRD).CompletedCount` remove scattered duplication

### Dependencies
- Security-relevant bumps: `golang.org/x/net` 0.33→0.57 and `golang.org/x/crypto` 0.31→0.54, plus `chroma` 2.27, `fsnotify` 1.10.1 and `glamour` 1.0.0 (API- and render-compatible with 0.10.0 — no source changes). The release workflow's Go version moves 1.23→1.25 to match the `go` directive raised by the updated dependency graph

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
