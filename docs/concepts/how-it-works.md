---
description: Learn how Chief works as an autonomous coding agent, transforming your requirements into working code through an automated execution loop.
---

# How Chief Works

Chief is an autonomous coding agent that transforms your requirements into working code, without constant back-and-forth prompting.

::: tip Background
For the motivation behind Chief and a deeper exploration of autonomous coding agents, read the blog post: [Introducing Chief: Autonomous PRD Agent](https://www.geocod.io/code-and-coordinates/2026-02-18-introducing-chief/)
:::

::: info Multi-agent support
Chief supports multiple agent backends: **Claude Code** (default), **Codex CLI**, **OpenCode CLI**, **Cursor CLI**, and **Gemini CLI**. This page uses "the agent" to refer to whichever backend you've configured. See [Configuration](/reference/configuration) for setup details.
:::

## The Core Concept

Traditional AI coding assistants hit a wall: the context window. As your conversation grows, the AI loses track of earlier details, makes contradictory decisions, or simply runs out of space. Long coding sessions become unwieldy.

Chief takes a different approach using a [Ralph Wiggum loop](https://ghuntley.com/ralph/): **each iteration starts fresh, but nothing is forgotten.**

You describe what you want to build as a series of user stories. Chief works through them one at a time, spawning a fresh agent session for each. Between iterations, Chief persists state to a `progress.md` file: what was built, which files changed, patterns discovered, and context for future work. The next iteration loads this history, giving the agent everything it needs without the baggage of a bloated conversation.

Running `chief` opens a TUI dashboard where you can review your project, then press `s` to start the loop.

## The Execution Loop

Chief works through your stories methodically. Each iteration focuses on a single story:

```
                ┌───────────────────────────────────────┐
                │                                       │
                ▼                                       │
        ┌──────────────┐                                │
        │  Pick Story  │                                │
        │  (next todo) │                                │
        └──────┬───────┘                                │
               │                                        │
               ▼                                        │
        ┌──────────────┐                                │
        │ Invoke Agent │                                │
        │  with prompt │                                │
        └──────┬───────┘                                │
               │                                        │
               ▼                                        │
        ┌──────────────┐                                │
        │    Agent     │                                │
        │ codes & tests│                                │
        └──────┬───────┘                                │
               │                                        │
               ▼                                        │
        ┌──────────────┐                                │
        │    Commit    │                                │
        │   changes    │                                │
        └──────┬───────┘                                │
               │                                        │
               ▼                                        │
        ┌──────────────┐           more stories         │
        │ Mark Complete├────────────────────────────────┘
        └──────┬───────┘
               │ all done
               ▼
           ✓ Finished
```

Here's what happens in each step:

1. **Pick Story**: Chief selects the next story along the dependency frontier — the highest-priority incomplete story whose `Blocked by` dependencies are all done. See [PRD Format](/concepts/prd-format#story-selection-logic) for the full selection algorithm
2. **Invoke Agent**: Constructs a prompt with the story details and project context, then spawns the agent
3. **Agent Codes**: The agent reads files, writes code, runs tests, and fixes issues until the story is complete. It works test-first in small vertical slices, testing observable behavior at the seams agreed during PRD authoring (see [PRD Format](/concepts/prd-format#what-chief-new-grilling-adds))
4. **Commit**: The agent commits the changes with a message like `feat: US-001 - Feature Title`
5. **Review** (optional): If a review is configured, a separate agent with a fresh context reviews the committed changes, fixes anything it finds, and amends the commit — see [The Review Agent](/concepts/code-review)
6. **Mark Complete**: Chief updates the story status in `prd.md` and records progress
7. **Repeat**: If more stories remain, the loop continues

This isolation is intentional. If something breaks, you know exactly which story caused it. Each commit represents one complete feature.

## Commit Messages & Story IDs

Every completed story results in a well-formed commit:

```
feat: auth/US-003 - Add user authentication

- Implemented login/logout endpoints
- Added JWT token validation
- Created auth middleware
```

Your git history becomes a timeline of features, matching 1:1 with your stories.

### The commit subject is PRD-namespaced

The subject follows a fixed shape that Chief both instructs the agent to write and later parses back:

```
feat: <prd-name>/<story-id> - <story-title>
```

- **`<prd-name>`** is the PRD's directory name under `.chief/prds/` — the same `<name>` as its `chief/<name>` branch.
- **`<story-id>`** is the story's ID from `prd.md` (e.g. `US-003`, `AUTH-003`).
- **`<story-title>`** is the story's title.

### Why the `<prd-name>/` prefix exists

Story IDs are only unique **within** a single PRD, and their numbering restarts per PRD — so two PRDs that both use the generic `US-` prefix each own a `US-001`. Since several PRDs can land commits reachable from the same history, the story ID alone is not a reliable key for "which commit implemented this story".

The `<prd-name>/` prefix fixes that: a PRD's directory name is unique, so **`<prd-name>/<story-id>` is a genuinely unique key** for a story's commit, even across PRDs that reuse the same numbers.

### How Chief uses it

Three places look a story's commit back up by this subject:

- **Completion check** — before trusting a `<chief-done/>` signal, Chief confirms a matching commit actually landed.
- **Per-story diff** — the TUI shows the diff for the selected story's commit.
- **Run summary** — the end-of-run summary is scoped to exactly the commits this run authored for the PRD's stories.

All three match on the `feat: <prd-name>/<story-id> - ` **prefix**, independent of the title. That means **editing a story's title in `prd.md` after it was committed no longer loses the commit**, and two PRDs that happen to share both an ID *and* a title are still told apart. Commits authored before this scheme existed (`feat: <story-id> - <title>`, with no namespace) are still found through a legacy fallback that matches the old ID-plus-title subject.

### Choosing story IDs

Because the namespace already guarantees correctness, the ID prefix is purely a **readability** choice. Prefer a short, feature-scoped prefix (`AUTH-`, `BILL-`) over the generic `US-` so IDs read unambiguously across PRDs — see [PRD Format → Use Consistent ID Patterns](/concepts/prd-format#use-consistent-id-patterns). Nothing breaks if two PRDs reuse the same prefix; the commit namespace keeps them distinct regardless.

## Progress Tracking

The `progress.md` file is what makes fresh context windows possible. After every iteration, the agent appends:

- What was implemented
- Which files changed
- Learnings for future iterations (patterns discovered, gotchas, context)

When the next iteration starts, the agent reads this file and immediately understands the project's history, without needing thousands of tokens of prior conversation. This gives you the benefits of long-running context (consistency, institutional memory) without the downsides (context overflow, degraded performance).

## Worktree Isolation for Parallel PRDs

When running multiple PRDs simultaneously, each PRD can work in its own isolated git worktree. This prevents parallel agent instances from conflicting over files, producing interleaved commits, or stepping on each other's branches.

When you start a PRD, Chief offers to create a worktree:
- A new branch is created (e.g., `chief/auth-system`) from your default branch
- A worktree is set up at `.chief/worktrees/<prd-name>/`
- Any configured setup command runs automatically (e.g., `npm install`)

Each worktree is a full checkout of your project, so the agent can read, write, and run tests independently. When the PRD completes, you can merge the branch back, push it to a remote, or have Chief automatically create a pull request.

The TUI shows branch and directory information throughout:
- **Tab bar**: Branch name next to each PRD tab
- **Dashboard header**: Current branch and working directory
- **PRD picker**: Branch and worktree path for each PRD

## Staying in Control

Autonomous doesn't mean unattended. The TUI lets you:

- **Start / Pause / Stop**: Press `s` to start, `p` to pause after the current story, `x` to stop immediately
- **Review diffs**: Press `d` to see the commit diff for the selected story
- **Edit the PRD**: Press `e` to open the current PRD in the agent for refinement
- **Switch projects**: Press `l` to list PRDs, `n` to create a new one, or `1-9` to jump directly
- **Resume anytime**: Walk away, come back, press `s`. Chief picks up where you left off
- **Merge branches**: Press `m` in the picker to merge a completed branch
- **Clean worktrees**: Press `c` in the picker to remove a worktree and optionally delete the branch
- **Configure settings**: Press `,` to open the Settings overlay

## After the Loop: Follow-ups

The loop ends when every story is `done` — but reviewing the finished feature by
hand usually turns up small fixes and polish. Rather than reopening completed
stories (which breaks the one-commit-per-story history), Chief lets you collect
those in a **follow-up inbox** and convert them into fresh stories with
[`chief followup`](/reference/cli#chief-followup). The loop then works through
only the new stories. See [Follow-ups](/concepts/follow-ups) for the workflow.

## Further Reading

- [The Ralph Loop](/concepts/ralph-loop): Deep dive into the execution loop mechanics
- [PRD Format](/concepts/prd-format): How to structure your project with effective user stories
- [Follow-ups](/concepts/follow-ups): Turn post-launch fixes and polish into new stories
- [The .chief Directory](/concepts/chief-directory): Understanding where state is stored
