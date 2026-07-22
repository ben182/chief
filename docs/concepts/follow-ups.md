---
description: How Chief closes the loop on post-implementation follow-ups — collect fixes and polish in a todos.md inbox, then turn them into user stories with `chief followup`.
---

# Follow-ups

A PRD is rarely truly finished the moment its last story goes green. You launch
the feature, click through it by hand, and inevitably find a handful of small
things: a card that should be hidden in one state, a missing download button, a
badge that counts wrong. These aren't new features — they're the **polish and
fixes you only see once the thing exists**.

Chief gives this its own lightweight loop: collect those items in a **follow-up
inbox** while you review, then run [`chief followup`](/reference/cli#chief-followup)
to turn them into proper user stories that the [Ralph loop](/concepts/ralph-loop)
works through like any other.

::: info Multi-agent support
This page uses "the agent" to refer to whichever backend you've configured
(Claude Code, Codex, OpenCode, Cursor, or Gemini). See
[Configuration](/reference/configuration).
:::

## The problem it solves

Without a dedicated flow, post-implementation follow-ups tend to end up in one
of two bad places:

- **A scratch file Chief never reads.** A hand-written `todos.md` full of
  checkboxes is invisible to Chief — it only parses the structured `### US-XXX`
  stories in `prd.md`. The list just sits there as dead text; you'd have to
  hand-translate every item into a story to make Chief act on it.
- **Reopened "done" stories.** Editing a story that already shipped, flipping it
  back to `todo`, and re-running breaks the one-commit-per-story history Chief
  works hard to keep clean. Now one commit no longer maps to one story.

`chief followup` avoids both. Follow-ups become **new** stories with fresh IDs,
so completed work is never disturbed, and the raw inbox is converted for you
rather than transcribed by hand.

## The flow

```
   implement PRD              review by hand            chief followup            resume loop
        │                          │                         │                        │
        ▼                          ▼                         ▼                        ▼
 ┌──────────────┐          ┌──────────────┐         ┌──────────────┐         ┌──────────────┐
 │ all stories  │          │ jot fixes &  │         │ each open    │         │ loop picks up│
 │ done          │  ───▶   │ polish into  │  ───▶   │ - [ ] item → │  ───▶   │ new todo     │
 │              │          │ todos.md      │         │ new US-XXX   │         │ stories only │
 └──────────────┘          └──────────────┘         └──────────────┘         └──────────────┘
                                                            │
                                            flips consumed items to - [x]
                                            → re-running never duplicates
```

### 1. Collect in the inbox

Each PRD directory can hold a **follow-up inbox** — a flat markdown checklist.
`chief new` scaffolds an empty one (`todos.md`, comment-only) next to `prd.md`
when it creates a PRD, so the place to dump items exists from day one. You can
also create it yourself at any time; `followups.md` and `follow-ups.md` are
accepted as alternative names.

While reviewing the finished feature, write one item per line:

```markdown
- [ ] Media card should be hidden when no media is attached
- [ ] Add a download button to the media view
- [ ] "In use" badge counts the same post twice
- [ ] Show media revisions in later stages, like text revisions
```

Keep them short — one thought per line. You don't need to phrase them as stories
or add acceptance criteria; that's the ingest step's job.

### 2. Ingest with `chief followup`

Run the command against the PRD whose inbox you filled:

```bash
chief followup                 # auto-detected PRD
chief followup linkedin-media  # a specific PRD
```

Chief launches the agent, which reads the inbox **and** the existing `prd.md`,
then for every **open** (`- [ ]`) item appends a proper story to `prd.md`:

```markdown
### US-013: Hide the media card when no media is attached
**Status:** todo
**Priority:** 13
**Blocked by:** US-004
**Description:** As a team member, I want the media card hidden while a post has
no media so that the Production view isn't cluttered with an empty slot.

**Acceptance Criteria:**
- [ ] The media card is not rendered when the post's media slot is empty
- [ ] The card reappears as soon as media is attached
```

The stories it writes meet the **same quality bar as `chief new`**: each is a
vertical slice, acceptance criteria describe observable outcomes (not code
steps), and generic quality gates ("tests pass") are left out because they're
enforced on every commit anyway.

### 3. Resume the loop

The new stories are plain `todo` stories, so a normal `chief` run picks them up
next. Because [story selection](/concepts/prd-format#story-selection-logic)
skips everything already `done`, the loop works through **only the follow-ups** —
one commit each, review agent included if you have one configured.

## How items become stories

A few rules keep the conversion predictable:

| Rule | Behavior |
|------|----------|
| **Sequential IDs** | Follow-ups get the next free `US-XXX` — no separate range. If the PRD ends at US-012, the first follow-up is US-013. |
| **Always `todo`** | New stories never start as `done`. |
| **`Blocked by` = lineage** | When a follow-up refines an already-`done` story, its `**Blocked by:**` records that story's ID. A done blocker never holds the story back — it's purely a note of where the follow-up came from. Items with no clear origin omit the line. |
| **No reopening** | Existing `done` stories are never edited or flipped back. The follow-up is always a new story. |
| **Idempotent** | After writing, each consumed item is flipped to `- [x]` with its new ID appended (`- [x] Add a download button → US-014`). Re-running skips checked items, so you never get duplicates. |

The `Blocked by` lineage is why follow-ups don't get their own ID range: a done
blocker is a no-op for selection (its dependency is already satisfied), so the
edge documents the connection without changing what the loop does.

## Grilling: only when it's ambiguous

`chief followup` uses the same batch-grill machinery as `chief new` and
`chief edit`, but leans on the **escape hatch**: most follow-ups are small and
clear ("add a download button", "fix the badge count"), so they're converted
directly without questions. The agent only grills you about items whose intended
behavior is genuinely unclear from the item text plus what it can see in the
repo — and before writing anything, it lists the stories it intends to create
and waits for your confirmation.

This is deliberate. Interrogating you over every one-line fix would defeat the
purpose; the grill is reserved for the items that actually need a decision.

## What it never does

- **It never writes implementation code.** `chief followup` is a PRD editor — it
  converts inbox items into stories and updates the inbox, nothing else. The
  actual work happens later, in the loop.
- **It never deletes the inbox.** Consumed items are checked off, not removed, so
  `todos.md` doubles as a record of which follow-up became which story.
- **It never touches finished work.** Done stories, functional requirements, and
  freeform context in `prd.md` are preserved; the command only appends.

## See also

- [`chief followup`](/reference/cli#chief-followup) — command reference
- [The `.chief` Directory](/concepts/chief-directory#todos-md-optional) — where the inbox lives
- [PRD Format](/concepts/prd-format) — the story format follow-ups are written in
- [How Chief Works](/concepts/how-it-works) — the loop that runs the new stories
