# PRD: Chief TUI - Autonomous Agent Loop for PRDs

## Introduction

Chief is a TUI application that orchestrates Claude Code to work through Product Requirements Documents autonomously. It wraps an agent loop with monitoring, controls, and a delightful developer experience. Users write PRDs describing features, and Chief invokes Claude repeatedly until all user stories are complete.

*Named after Chief Wiggum, Ralph Wiggum's dad from The Simpsons. Inspired by [snarktank/ralph](https://github.com/snarktank/ralph).*

The core value proposition: transform a PRD into working code with minimal human intervention, while providing visibility and control over the process.

## Goals

- **Delightful DX**: Make monitoring and controlling the agent loop a pleasure with a modern, keyboard-driven TUI
- **Easy Distribution**: Single binary with no dependencies, cross-platform (macOS, Linux, Windows)
- **Simple Core**: The agent loop should be ~80 lines, easy to understand and debug
- **Self-Contained**: Embed the agent prompt, PRD skills, and completion sound in the binary
- **Autonomous Execution**: Claude works through stories one at a time, updating state as it goes
- **Recovery**: Handle interruptions gracefully; resume where left off

## User Stories

### Phase 1: Core

---

### US-001: Go Project Setup with Bubble Tea
**Description:** As a developer, I need a properly structured Go project with Bubble Tea so that I have a foundation for building the TUI.

**Acceptance Criteria:**
- [ ] Initialize Go module at `github.com/minicodemonkey/chief`
- [ ] Add Bubble Tea, Lip Gloss, and Bubbles dependencies
- [ ] Create directory structure: `cmd/chief/`, `internal/loop/`, `internal/prd/`, `internal/tui/`, `internal/notify/`, `embed/`
- [ ] Create minimal `main.go` that launches a "Hello World" Bubble Tea app
- [ ] `go build ./cmd/chief` produces a working binary
- [ ] Typecheck passes

---

### US-002: PRD Types and Loader
**Description:** As a developer, I need to load and parse PRD JSON files so that the loop knows what stories to work on.

**Acceptance Criteria:**
- [ ] Create `internal/prd/types.go` with PRD and UserStory structs matching the schema
- [ ] Create `internal/prd/loader.go` with `LoadPRD(path string) (*PRD, error)`
- [ ] Add `PRD.AllComplete() bool` method that returns true when all stories have `passes: true`
- [ ] Add `PRD.NextStory() *UserStory` that returns: first `inProgress: true` story, or lowest priority `passes: false` story
- [ ] Add `PRD.SavePRD(path string) error` for updating the JSON file
- [ ] Unit tests for all methods including edge cases (empty PRD, all complete, interrupted story)
- [ ] Typecheck passes

---

### US-003: Stream-JSON Parser
**Description:** As a developer, I need to parse Claude's stream-json output so that I can extract events for the TUI.

**Acceptance Criteria:**
- [ ] Create `internal/loop/parser.go` with `EventType` constants: `IterationStart`, `AssistantText`, `ToolStart`, `ToolResult`, `StoryStarted`, `StoryCompleted`, `Complete`, `MaxIterationsReached`, `Error`
- [ ] Create `Event` struct with fields: `Type`, `Iteration`, `Text`, `Tool`, `ToolInput`, `StoryID`, `Err`
- [ ] Create `parseLine(line string) *Event` function that parses stream-json format
- [ ] Detect `<chief-complete/>` in assistant text and return `Complete` event
- [ ] Extract tool name and input from `tool_use` content blocks
- [ ] Unit tests with example stream-json lines from the spec
- [ ] Typecheck passes

---

### US-004: Core Agent Loop
**Description:** As a developer, I need the core loop that invokes Claude repeatedly until all stories are complete.

**Acceptance Criteria:**
- [ ] Create `internal/loop/loop.go` with `Loop` struct containing: `prdPath`, `prompt`, `maxIter`, `iteration`, `events chan Event`, `claudeCmd`
- [ ] Implement `Run(ctx context.Context) error` that loops until complete or max iterations
- [ ] Implement `runIteration(ctx context.Context) error` that spawns Claude with correct flags: `--dangerously-skip-permissions`, `-p`, `--output-format stream-json`, `--verbose`
- [ ] Stream stdout line by line, parse each line, send events to channel
- [ ] Log raw output to `claude.log` file in PRD directory
- [ ] Implement `Stop()` method that kills the Claude process
- [ ] Check `prd.json` after each iteration to detect completion
- [ ] Integration test with mock Claude script that outputs predefined stream-json
- [ ] Typecheck passes

---

### US-005: Embed Agent Prompt
**Description:** As a developer, I need the agent prompt embedded in the binary so that Claude knows how to work through stories.

**Acceptance Criteria:**
- [ ] Create `embed/prompt.txt` with the agent prompt from the spec
- [ ] Use Go's `//go:embed` directive to embed the prompt
- [ ] Loop uses embedded prompt, substituting PRD path placeholder
- [ ] Prompt instructs Claude to: read prd.json, select next story, set inProgress, implement, mark passes, append to progress.md, output `<chief-complete/>` when done
- [ ] Typecheck passes

---

### US-006: Basic Dashboard View
**Description:** As a user, I want to see a dashboard showing story status and details so that I can monitor progress.

**Acceptance Criteria:**
- [ ] Create `internal/tui/app.go` with main Bubble Tea model
- [ ] Create `internal/tui/dashboard.go` with two-panel layout: Stories list (left) and Details (right)
- [ ] Stories panel shows: status icon (○/▶/✓/✗/⏸), story ID, truncated title
- [ ] Details panel shows: full title, description, acceptance criteria list, priority
- [ ] Header shows: "chief" branding, state indicator (READY/RUNNING/PAUSED/COMPLETE/ERROR), iteration count, elapsed time
- [ ] Footer shows: available keyboard shortcuts, current PRD name
- [ ] Progress bar at bottom of stories panel showing completion percentage
- [ ] Keyboard navigation: ↑/k and ↓/j to move selection
- [ ] Typecheck passes

---

### US-007: Start/Pause/Stop Controls
**Description:** As a user, I want to control the agent loop with keyboard shortcuts so that I can start, pause, or stop execution.

**Acceptance Criteria:**
- [ ] `s` key starts the loop when in Ready/Paused state
- [ ] `p` key sets pause flag (loop stops after current iteration completes)
- [ ] `x` key stops immediately (kills Claude process)
- [ ] State transitions: Ready → Running, Running → Paused (after iteration), Running → Stopped
- [ ] TUI updates header state indicator in real-time
- [ ] Activity line at bottom shows current Claude activity (last assistant text)
- [ ] Unit tests for state machine transitions
- [ ] Typecheck passes

---

### US-008: PRD File Watching
**Description:** As a user, I want the TUI to update when prd.json changes so that I see story status changes in real-time.

**Acceptance Criteria:**
- [ ] Watch prd.json for filesystem changes using `fsnotify`
- [ ] Reload PRD and update TUI when file changes
- [ ] Detect when a story's `inProgress` or `passes` field changes
- [ ] Update story icons in real-time as Claude marks progress
- [ ] Handle file not found gracefully (show error, don't crash)
- [ ] Integration test verifying TUI updates when file is modified externally
- [ ] Typecheck passes

---

### Phase 2: Full TUI

---

### US-009: Lip Gloss Styling System
**Description:** As a developer, I need a consistent styling system so that the TUI looks polished and cohesive.

**Acceptance Criteria:**
- [ ] Create `internal/tui/styles.go` with Lip Gloss style definitions
- [ ] Define color palette: Primary cyan (#00D7FF), Success green (#5AF78E), Warning yellow (#F3F99D), Error red (#FF5C57), Muted gray (#6C7086), Border gray (#45475A)
- [ ] Create styles for: headers, borders, selected items, status badges, progress bars
- [ ] Status indicators use correct colors: ▶ cyan, ✓ green, ○ muted, ✗ red, ⏸ yellow
- [ ] All existing views use the new styling system
- [ ] Typecheck passes

---

### US-010: Log Viewer with Tool Cards
**Description:** As a user, I want a full-screen log view showing Claude's streaming output so that I can see what Claude is doing in detail.

**Acceptance Criteria:**
- [ ] Create `internal/tui/log.go` with scrollable log viewport
- [ ] Toggle with `t` key from dashboard
- [ ] Display assistant text as regular paragraphs
- [ ] Display tool calls as styled cards with icon and tool name (📖 Read, ✏️ Edit, 📝 Write, 🔨 Bash, 🔍 Glob, 🔎 Grep, 🤖 Task, 🌐 WebFetch)
- [ ] Vim-style scrolling: j/↓ down, k/↑ up, Ctrl+D page down, Ctrl+U page up, G bottom, g top
- [ ] Auto-scroll to bottom when new content arrives (unless user has scrolled up)
- [ ] Show cursor indicator (▌) at bottom when streaming
- [ ] Footer shows log-specific shortcuts
- [ ] Typecheck passes

---

### US-011: PRD Picker Modal
**Description:** As a user, I want to switch between multiple PRDs so that I can manage different features.

**Acceptance Criteria:**
- [ ] Create `internal/tui/picker.go` with modal overlay
- [ ] Toggle with `l` key
- [ ] List all PRDs in `.chief/prds/` directory
- [ ] Each entry shows: name, project title, progress bar, status (Ready/Running/Paused/Complete)
- [ ] Keyboard navigation: ↑/↓ to select, Enter to switch, Esc to cancel
- [ ] `n` key in picker opens prompt for new PRD name (launches `chief new <name>`)
- [ ] Current PRD highlighted with indicator
- [ ] Typecheck passes

---

### US-012: Parallel PRD Execution
**Description:** As a user, I want to run multiple PRDs simultaneously so that I can work on different features in parallel.

**Acceptance Criteria:**
- [ ] Each PRD has its own independent loop instance (goroutine with its own Claude process)
- [ ] PRD picker shows real-time status of all PRDs: Running (with iteration count), Paused, Ready, Complete, Error
- [ ] Can start/pause/stop any PRD from the picker without affecting others
- [ ] Switching PRDs in the dashboard shows that PRD's details and log
- [ ] Activity indicator in header shows which PRDs are currently running
- [ ] Completion sound plays when ANY PRD completes (not just the currently viewed one)
- [ ] `x` (stop) in picker stops the selected PRD, not the currently viewed one
- [ ] Resource cleanup: when quitting Chief, gracefully stop all running Claude processes
- [ ] Unit tests for multi-loop state management
- [ ] Typecheck passes

---

### US-013: Help Overlay
**Description:** As a user, I want to see all keyboard shortcuts in a help overlay so that I can learn the interface quickly.

**Acceptance Criteria:**
- [ ] Toggle with `?` key from any view
- [ ] Display all shortcuts organized by category: Loop Control, Views, Navigation, Scrolling, General
- [ ] Shortcuts display matches current view context
- [ ] Esc or `?` closes overlay
- [ ] Centered modal with border
- [ ] Typecheck passes

---

### US-014: Narrow Terminal Support
**Description:** As a user, I want the TUI to work on narrow terminals so that I can use it in split panes.

**Acceptance Criteria:**
- [ ] Detect terminal width on startup and resize
- [ ] Below 100 columns: switch to single-column stacked layout
- [ ] Stories panel on top, details panel below
- [ ] Minimum supported width: 80 columns
- [ ] Gracefully truncate long text with ellipsis
- [ ] Keyboard shortcuts condensed in narrow mode
- [ ] Unit tests for layout calculations at various widths
- [ ] Typecheck passes

---

### US-015: Error and Warning States
**Description:** As a user, I want clear error and warning displays so that I know when something goes wrong and how to fix it.

**Acceptance Criteria:**
- [ ] Error state: red ERROR indicator in header, error details in right panel
- [ ] Display exit code and error message from Claude
- [ ] "Check claude.log for full error details" hint
- [ ] `s` key shows "Retry" in error state
- [ ] Interrupted story warning: when starting with `inProgress: true` story, show notice in details panel
- [ ] Empty state: when no PRDs exist, show centered message with `chief new` instructions
- [ ] Typecheck passes

---

### Phase 3: PRD Generation

---

### US-016: Chief Init Command
**Description:** As a user, I want to create new PRDs interactively so that I can describe features and have Claude help structure them.

**Acceptance Criteria:**
- [ ] `chief new` creates PRD in `.chief/prds/main/`
- [ ] `chief new <name>` creates PRD in `.chief/prds/<name>/`
- [ ] `chief new <name> "<context>"` passes context to Claude
- [ ] Command launches interactive Claude Code session with embedded PRD generator prompt
- [ ] Creates directory structure if it doesn't exist
- [ ] After Claude session ends, automatically run conversion (see US-018)
- [ ] Exit cleanly after conversion completes
- [ ] Integration test verifying directory creation and Claude invocation
- [ ] Typecheck passes

---

### US-017: Chief Edit Command
**Description:** As a user, I want to edit existing PRDs interactively so that I can refine requirements with Claude's help.

**Acceptance Criteria:**
- [ ] `chief edit` edits `.chief/prds/main/prd.md`
- [ ] `chief edit <name>` edits `.chief/prds/<name>/prd.md`
- [ ] Error if PRD doesn't exist (suggest `chief new` instead)
- [ ] Launch interactive Claude session with existing prd.md as context
- [ ] After session ends, run conversion with progress protection (see US-019)
- [ ] Typecheck passes

---

### US-018: Auto-Conversion (prd.md → prd.json)
**Description:** As a user, I want prd.md automatically converted to prd.json so that I only edit the human-readable format.

**Acceptance Criteria:**
- [ ] Create `internal/prd/generator.go` with conversion logic
- [ ] Conversion runs: after `chief new`, after `chief edit`, before `chief run` if prd.md is newer
- [ ] Invoke Claude one-shot (non-interactive) with embedded converter prompt
- [ ] Claude reads prd.md and writes prd.json
- [ ] Verify prd.json is valid JSON after conversion
- [ ] Integration test with sample prd.md files
- [ ] Typecheck passes

---

### US-019: Progress Protection on Conversion
**Description:** As a user, I want my progress preserved when I edit the PRD so that I don't lose completed work.

**Acceptance Criteria:**
- [ ] Before conversion, check if prd.json has progress (any `passes: true` or `inProgress: true`)
- [ ] If progress exists and prd.md changed, show warning with options: [M]erge, [O]verwrite, [C]ancel
- [ ] Merge behavior: keep status for matching story IDs, add new stories, drop removed stories
- [ ] `--merge` flag auto-merges without prompting
- [ ] `--force` flag auto-overwrites without prompting
- [ ] Unit tests for merge logic with various scenarios
- [ ] Typecheck passes

---

### Phase 4: Polish

---

### US-020: Completion Sound
**Description:** As a user, I want an audio notification when the loop completes so that I can work on other things and be alerted.

**Acceptance Criteria:**
- [ ] Embed ~30KB WAV file in binary using `//go:embed`
- [ ] Play sound when: all stories complete (`<chief-complete/>`), max iterations reached
- [ ] Use `github.com/hajimehoshi/oto/v2` for cross-platform audio playback
- [ ] `--no-sound` flag disables audio
- [ ] Handle audio device errors gracefully (log warning, don't crash)
- [ ] Typecheck passes

---

### US-021: Quick Commands (status, list)
**Description:** As a user, I want quick CLI commands to check progress without launching the TUI.

**Acceptance Criteria:**
- [ ] `chief status` prints progress for default PRD: project name, X/Y stories complete, list of incomplete stories
- [ ] `chief status <name>` prints progress for specific PRD
- [ ] `chief list` prints all PRDs in `.chief/prds/` with name, title, and progress percentage
- [ ] Output is plain text, suitable for scripting
- [ ] Exit code 0 for success, non-zero for errors
- [ ] Typecheck passes

---

### US-022: CLI Flag Parsing
**Description:** As a user, I want consistent CLI flags so that I can configure Chief's behavior.

**Acceptance Criteria:**
- [ ] `--max-iterations N` sets iteration limit (default: 10)
- [ ] `--no-sound` disables completion sound
- [ ] `--verbose` shows raw Claude output in log
- [ ] `--merge` auto-merges on conversion conflicts
- [ ] `--force` auto-overwrites on conversion conflicts
- [ ] Positional argument: PRD name or path to prd.json
- [ ] `--help` shows usage with all flags documented
- [ ] `--version` shows version number
- [ ] Typecheck passes

---

### Phase 5: Distribution

---

### US-023: Goreleaser Configuration
**Description:** As a maintainer, I need goreleaser configured so that I can build release binaries for all platforms.

**Acceptance Criteria:**
- [ ] Create `.goreleaser.yaml` with build configuration
- [ ] Build targets: darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/amd64
- [ ] Binary name: `chief`
- [ ] Include version info via ldflags
- [ ] `goreleaser release --snapshot --clean` produces all binaries
- [ ] Archives use `.tar.gz` for Unix, `.zip` for Windows
- [ ] Typecheck passes

---

### US-024: Homebrew Formula
**Description:** As a macOS/Linux user, I want to install Chief via Homebrew so that I get easy updates.

**Acceptance Criteria:**
- [ ] Create Homebrew formula in separate tap repository or inline
- [ ] Formula downloads correct binary for platform
- [ ] `brew install chief` works on macOS (Intel and Apple Silicon)
- [ ] `brew install chief` works on Linux
- [ ] Formula includes description and homepage
- [ ] Typecheck passes (for any Go code in the formula repo)

---

### US-025: Install Script
**Description:** As a user without Homebrew, I want a simple install script so that I can install Chief easily.

**Acceptance Criteria:**
- [ ] Create `install.sh` script hosted at `raw.githubusercontent.com/ben182/chief/refs/heads/main/install.sh`
- [ ] Script detects OS (darwin/linux) and architecture (amd64/arm64)
- [ ] Downloads correct binary from GitHub releases
- [ ] Installs to `/usr/local/bin` (or `~/.local/bin` if no sudo)
- [ ] Verifies checksum before installing
- [ ] `curl -fsSL https://raw.githubusercontent.com/ben182/chief/refs/heads/main/install.sh | sh` works
- [ ] Script is idempotent (can run multiple times safely)

---

### US-026: README and Documentation
**Description:** As a new user, I want clear documentation so that I can understand and use Chief quickly.

**Acceptance Criteria:**
- [ ] Create comprehensive README.md with: Quick Start, How It Works, Installation (brew, go install, curl), Usage examples, Keyboard shortcuts table, PRD format reference, Troubleshooting
- [ ] Include ASCII diagram showing data flow
- [ ] Create `docs/adr/` with Architecture Decision Records for key decisions
- [ ] Add inline code documentation for all public functions
- [ ] All code examples in docs are tested/verified
- [ ] Typecheck passes

---

## Functional Requirements

- **FR-01**: Chief must invoke Claude Code with `--dangerously-skip-permissions -p <prompt> --output-format stream-json --verbose` flags
- **FR-02**: Chief must parse stream-json output line by line and extract: assistant text, tool calls, tool results
- **FR-03**: Chief must detect `<chief-complete/>` in Claude's output to know when all stories are done
- **FR-04**: Chief must re-read prd.json after each iteration to check for completion
- **FR-05**: Chief must log all raw Claude output to `.chief/prds/<name>/claude.log`
- **FR-06**: Chief must watch prd.json for external changes and update the TUI in real-time
- **FR-07**: Chief must support pausing (after current iteration) and stopping (immediately) the loop
- **FR-08**: Chief must handle interrupted sessions by detecting `inProgress: true` stories on startup
- **FR-09**: Chief must convert prd.md to prd.json automatically, preserving progress when possible
- **FR-10**: Chief must play a completion sound when all stories pass or max iterations reached
- **FR-11**: Chief must support multiple PRDs in `.chief/prds/` directory with switching between them
- **FR-15**: Chief must support running multiple PRDs in parallel, each with its own Claude process
- **FR-12**: Chief must work on terminals as narrow as 80 columns with graceful layout adaptation
- **FR-13**: Chief must build as a single static binary with no runtime dependencies
- **FR-14**: Chief must support darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/amd64

## Non-Goals

- **Branch management**: Users handle git themselves; Chief does not create or switch branches
- **Headless/CI mode**: Not needed for v1; TUI is the primary interface
- **Settings persistence**: CLI flags are sufficient; no config file
- **Cost tracking**: Not tracking API usage or costs in v1
- **Web UI**: Terminal-only for v1
- **Team/multiplayer mode**: Single user only
- **Subagent monitoring**: Not tracking Task tool spawns in v1
- **Automatic retry on error**: User decides whether to retry; loop stops on error

## Design Considerations

### TUI Layout
- Two-panel dashboard: Stories list (left, ~40%), Details (right, ~60%)
- Full-screen log viewer as alternate view
- Modal overlays for: PRD picker, help
- Header always visible with state and timing
- Footer always visible with context-sensitive shortcuts

### Color Palette
| Element | Color | Hex |
|---------|-------|-----|
| Primary accent | Cyan | `#00D7FF` |
| Success | Green | `#5AF78E` |
| Warning | Yellow | `#F3F99D` |
| Error | Red | `#FF5C57` |
| Muted text | Gray | `#6C7086` |
| Border | Dim gray | `#45475A` |

### Status Indicators
| Symbol | State | Color |
|--------|-------|-------|
| `▶` | In progress | Cyan |
| `✓` | Completed | Green |
| `○` | Pending | Muted gray |
| `✗` | Failed | Red |
| `⏸` | Paused | Yellow |

## Technical Considerations

### Dependencies
- **Bubble Tea**: TUI framework
- **Lip Gloss**: Styling
- **Bubbles**: Common TUI components (list, viewport, etc.)
- **fsnotify**: File watching
- **oto**: Cross-platform audio

### File Structure
```
.chief/
└── prds/
    ├── main/
    │   ├── prd.md          # Human-readable (source of truth)
    │   ├── prd.json        # Machine-readable (auto-generated)
    │   ├── progress.md     # Implementation notes
    │   └── claude.log      # Raw Claude output
    └── <other-prds>/
```

### State Management
- All persistent state lives in prd.json
- TUI model is ephemeral; reconstructs from prd.json on startup
- Loop state: Ready → Running → Paused/Complete/Error

### Error Recovery
- Non-zero Claude exit: show error, let user retry
- `inProgress: true` on startup: show warning, auto-resume on start
- File system errors: show error, don't crash

## Success Metrics

- User can go from `chief new` to running loop in under 2 minutes
- Loop correctly completes all stories in a test PRD without human intervention
- TUI remains responsive during Claude execution (no blocking)
- Binary size under 15MB
- Works on all target platforms without modification

## Design Decisions

1. **Multiple PRDs in parallel**: Yes — users can run multiple PRDs simultaneously, managed through the TUI. Each PRD has its own loop instance. The PRD picker shows real-time status of all PRDs.
2. **Dry run mode**: No — not needed; users can inspect the PRD to see what's next.
3. **progress.md format**: Freeform markdown — human-friendly, Claude writes it naturally.
4. **Schema validation**: No — trust the conversion process; if prd.json is invalid, Claude will fail with a clear error.
