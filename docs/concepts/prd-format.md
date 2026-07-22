---
description: Complete guide to Chief's PRD format. How prd.md structures user stories, status tracking, selection logic, and best practices.
---

# PRD Format

Chief uses a single `prd.md` file that serves as both human-readable context and machine-readable structured data. Chief parses structured markdown headings, status fields, and checkbox items directly from this file — no separate JSON file is needed.

::: info Multi-agent support
Chief supports multiple agent backends: **Claude Code** (default), **Codex CLI**, **OpenCode CLI**, **Cursor CLI**, and **Gemini CLI**. This page uses "the agent" to refer to whichever backend you've configured. See [Configuration](/reference/configuration) for setup details.
:::

## File Structure

Each PRD lives in its own subdirectory inside `.chief/prds/`:

```
.chief/prds/my-feature/
├── prd.md                        # Structured PRD (you write, Chief reads and updates)
├── progress.md                   # Auto-generated progress log
└── claude-2026-02-18-143012.log  # Raw agent output — one timestamped file per run
```

- **`prd.md`** — Written by you, read and updated by Chief. Contains project context and structured user stories.
- **`progress.md`** — Written by the agent. Tracks what was done, what changed, and what was learned.
- **`claude-<timestamp>.log`** (or `codex-` / `opencode-` / `cursor-` / `gemini-`) — Written by Chief. Each run writes its own timestamped log for debugging; older runs' logs are kept alongside.

## prd.md — The PRD File

The `prd.md` file has two parts: **freeform context** at the top and **structured user stories** below. The freeform section gives the agent background on your project. The structured stories use specific markdown patterns that Chief parses to drive execution.

### Freeform Context

The top of the file is your chance to give the agent context that doesn't fit into structured fields. Write whatever helps the agent understand the project — there's no required format for this section.

**What to Include:**

- **Overview** — What are you building and why?
- **Technical context** — What stack, frameworks, and patterns does the project use?
- **Design notes** — Any constraints, preferences, or conventions to follow.
- **Examples** — Reference implementations, API shapes, or UI mockups.
- **Links** — Related docs, design files, or prior art.

### Structured User Stories

Below the freeform context, define your user stories using structured markdown headings that Chief parses:

- `### US-001: Story Title` — story heading (ID + title)
- `**Status:** done|in-progress|todo|needs-review` — tracked by Chief
- `**Priority:** N` — execution order (optional; defaults to document order)
- `**Blocked by:** US-001, US-002` — story IDs that must be `done` first (optional; omit for stories with no dependencies)
- `**Description:** ...` — story description (or freeform prose after heading)
- `- [ ] criterion` / `- [x] criterion` — acceptance criteria as checkboxes

### Example prd.md

```markdown
# User Authentication System

## Overview
We're building a complete authentication system for our SaaS application.
Users need to register, log in, reset passwords, and manage sessions.

## Technical Context
- Backend: Express.js with TypeScript
- Database: PostgreSQL with Prisma ORM
- Frontend: React with Next.js
- Auth: JWT tokens stored in httpOnly cookies

## Design Notes
- Follow existing middleware patterns in `src/middleware/`
- Use Zod for input validation (already a dependency)
- All API routes should return consistent error shapes
- Tests use Vitest — run with `npm test`

## Reference
- Existing user model: `prisma/schema.prisma`
- API route pattern: `src/routes/health.ts`

## User Stories

### US-001: User Registration

**Status:** todo
**Priority:** 1
**Description:** As a new user, I want to register an account so that I can access the application.

- [ ] Registration form with email and password fields
- [ ] Email format validation
- [ ] Password minimum 8 characters
- [ ] Confirmation email sent on registration
- [ ] User redirected to login after registration

### US-002: User Login

**Status:** todo
**Priority:** 2
**Description:** As a registered user, I want to log in so that I can access my account.

- [ ] Login form with email and password fields
- [ ] Error message for invalid credentials
- [ ] JWT token issued on success
- [ ] Redirect to dashboard on success

### US-003: Password Reset

**Status:** todo
**Priority:** 3
**Description:** As a user, I want to reset my password so that I can recover my account.

- [ ] "Forgot password" link on login page
- [ ] Email with reset link sent to user
- [ ] Reset token expires after 1 hour
- [ ] New password form with confirmation field
```

This file is included in the agent's context and also parsed by Chief to track story status and selection.

::: tip
The better your `prd.md`, the better the agent's output. Spend time on the freeform context — it pays off across every story.
:::

## Story Selection Logic

Chief picks the next story to work on by following the **dependency frontier** — a deterministic algorithm that respects `Blocked by` edges:

```
1. If a story is **Status:** in-progress (and not parked), resume it first
2. Otherwise compute the frontier: every story that is
   not done, not needs-review, and whose Blocked by IDs are all done
3. From the frontier, pick the lowest **Priority:** (ties break by document order)
4. Fallback: if the frontier is empty but unfinished, non-parked work remains
   (a dependency cycle, or everything left is blocked by a parked story),
   pick the lowest-priority unfinished, non-parked story anyway
5. Mark the chosen story **Status:** in-progress and start the iteration
6. Nothing left → the loop ends
```

Stories parked as `needs-review` are skipped by selection just like `done` stories, so one stuck story never blocks the rest. The loop ends once no actionable stories remain (everything is either `done` or `needs-review`).

The fallback in step 4 is what makes the loop deadlock-proof: even if you write a dependency cycle, or every remaining story is blocked by a story that got parked, Chief still makes progress instead of stalling.

### How Priority and Blocking Interact

`Blocked by` decides *whether* a story is eligible; `Priority` only orders the stories that are already eligible. A story is held back until all of its blockers are `done`, no matter how low its priority number is. Priority is a number where **lower = higher priority**.

Consider three stories where US-003 depends on US-002:

| Story | Priority | Status | Blocked by | On frontier? |
|-------|----------|--------|------------|--------------|
| US-001 | 1 | `done` | — | No — already complete |
| US-002 | 2 | `todo` | — | **Yes — unblocked, lowest priority → selected** |
| US-003 | 1 | `todo` | US-002 | No — blocked until US-002 is `done` |

Even though US-003 has the lowest priority number (1), it is skipped because its blocker US-002 isn't `done` yet. Chief works US-002 first; once US-002 flips to `done`, US-003 joins the frontier and is picked on the next iteration.

::: tip
An unknown or misspelled blocker ID is treated as already satisfied (ignored), and a story can't block itself — so a typo in a `Blocked by` line can never deadlock the loop. See [`blockedBy`](/reference/prd-schema#blockedby) in the reference for the full robustness rules.
:::

### What `in-progress` Does

When Chief starts working on a story, it sets `**Status:** in-progress`. This serves as a signal that the story is being actively worked on. When the story completes:

- `**Status:**` is set to `done`
- Acceptance criteria checkboxes are checked (`- [x]`)

If Chief is interrupted mid-iteration, the status may remain `in-progress`. On the next run, Chief will pick up the same story and continue.

### Completion Signal

When the agent finishes a story, it outputs `<chief-done/>` to signal that the current story is complete. Chief then marks the story as done in `prd.md` and selects the next one. When no incomplete stories remain, the loop ends naturally.

## Annotated Example PRD

Here's a complete `prd.md` with annotations explaining each part:

```markdown
# User Authentication                    ← Project heading (shown in TUI)

## Overview
Complete auth system with login,         ← Freeform context for the agent
registration, and password reset.

## User Stories

### US-001: User Registration            ← Story ID + title (appears in commits)

**Status:** done                         ← Chief tracks this (done/in-progress/todo/needs-review)
**Priority:** 1                          ← Execution order (1 = first)
**Description:** As a new user, I want   ← Story description
to register an account so that I can
access the application.

- [x] Registration form with email       ← Acceptance criteria (checked = done)
      and password fields
- [x] Email format validation
- [x] Password minimum 8 characters
- [x] Confirmation email sent
- [x] User redirected to login

### US-002: User Login                   ← Next story

**Status:** in-progress                  ← Currently being worked on
**Priority:** 2
**Description:** As a registered user,
I want to log in so that I can access
my account.

- [x] Login form with email and         ← Some criteria already met
      password fields
- [ ] Error message for invalid          ← Still in progress
      credentials
- [ ] JWT token issued on success
- [ ] Redirect to dashboard on success

### US-003: Password Reset               ← Pending story

**Status:** todo
**Priority:** 3
**Description:** As a user, I want to
reset my password so that I can recover
my account.

- [ ] "Forgot password" link on login
- [ ] Email with reset link sent
- [ ] Reset token expires after 1 hour
- [ ] New password form with confirmation
```

## Best Practices

### Write Specific Acceptance Criteria

Each criterion should be concrete and verifiable. The agent uses these to determine what to build and when the story is done.

```markdown
<!-- ✓ Good — specific and testable -->
- [ ] Login form with email and password fields
- [ ] Error message shown for invalid credentials
- [ ] JWT token stored in httpOnly cookie on success
- [ ] Redirect to /dashboard after login

<!-- ✗ Bad — vague and subjective -->
- [ ] Nice login page
- [ ] Good error handling
- [ ] Secure authentication
```

### Keep Stories Small

A story should represent one logical piece of work. If a story has more than 5–7 acceptance criteria, consider splitting it into multiple stories.

**Too large:**
```markdown
### US-001: Complete Authentication System

- [ ] Registration form
- [ ] Login form
- [ ] Password reset
- [ ] Email verification
- [ ] OAuth integration
- [ ] Session management
- [ ] Rate limiting
- [ ] Account lockout
- [ ] Audit logging
```

**Better — split into focused stories:**
```markdown
### US-001: User Registration
### US-002: User Login
### US-003: Password Reset
### US-004: OAuth Integration
```

### Order Stories by Dependency

Express real cross-story ordering with `**Blocked by:**`, not priority. A blocker keeps a story off the frontier until every ID it lists is `done`, so it's the right tool when one story genuinely needs another finished first. Reserve `Priority` for tie-breaking among stories that are all unblocked — deciding what to do first when nothing forces the order. Keep the dependency graph acyclic (no story transitively blocked by itself).

```markdown
### US-001: Database Schema
**Priority:** 1

### US-002: API Endpoints
**Priority:** 2
**Blocked by:** US-001

### US-003: Frontend Forms
**Priority:** 3
**Blocked by:** US-002

### US-004: Integration Tests
**Priority:** 4
**Blocked by:** US-002, US-003
```

Frame each story as a **vertical slice** (a "tracer bullet"): a narrow but *complete* path through the stack that is demoable or verifiable on its own, rather than a horizontal slice of a single layer. "Wire the login form end-to-end for one provider" is a vertical slice; "build all the database tables" is a horizontal one. Vertical slices keep the `Blocked by` graph shallow and give each iteration something you can actually try.

### Use Consistent ID Patterns

Story IDs appear in commit messages (`feat: US-001 - User Registration`) and must match the `LETTERS-NUMBERS` pattern. Pick a prefix and stick with it:

- `US-001`, `US-002` — generic user stories
- `AUTH-001`, `AUTH-002` — feature-scoped prefixes
- `BUG-001`, `FIX-001` — for bug fix PRDs

### Give the Agent Context

The freeform context section at the top of `prd.md` is where you set the agent up for success. Since the context and structured stories live in the same file, the agent sees everything in one place. Include:

- What frameworks and tools the project uses
- Where to find existing patterns to follow
- Any constraints or conventions
- What "done" looks like beyond acceptance criteria

### Use `chief new` to Get Started

Running `chief new` scaffolds a `prd.md` with a template. You can also run `chief edit` to open an existing PRD for editing. This is the easiest way to create a well-structured PRD.

### What `chief new` Grilling Adds

The interactive `chief new` interview does two extra things worth knowing about.

**Testing Decisions.** When the feature has testable logic, the generated PRD includes a **"Testing Decisions"** section that records the *seams* to test at — the public boundaries where behavior can be observed without reaching inside — along with what makes a good test here (observable behavior through the public interface, not implementation details) and prior art to mirror. During grilling the agent sketches the seams and confirms them with you; which test framework you use and how existing tests look are facts it looks up, not questions it asks. Trivial, no-logic features skip the section entirely.

**Throwaway HTML prototypes (Claude only).** When a UI, layout, or interaction decision would be quicker to settle by *seeing* it than describing it, the agent proactively offers to build a small, self-contained throwaway HTML prototype — it says what the prototype would show and which decision it resolves, then waits for your go-ahead (it never builds one unprompted). On approval it produces a single static `*.html` file, inline CSS/JS with no build step, in the PRD directory under `.chief/` (never committed). You open it, your reaction settles the decision, and the decision itself is captured back in the PRD — that's the durable artifact. The HTML is disposable. This is Claude-only, since it relies on subagents.

## What's Next

- [PRD Format Reference](/reference/prd-schema) — Complete field documentation and parsing behavior
- [The .chief Directory](/concepts/chief-directory) — Understanding the full directory structure
- [How Chief Works](/concepts/how-it-works) — How Chief uses these files during execution
