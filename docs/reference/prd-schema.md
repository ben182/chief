---
description: Complete prd.md format reference for Chief. Heading structure, field types, status values, and parsing behavior.
---

# PRD Format Reference

Complete format documentation for `prd.md`.

## Story Heading Format

Each user story is defined by a level-3 (or level-4) markdown heading with an ID and title:

```markdown
### ID: Title
```

The ID must match the pattern `LETTERS-NUMBERS` (one or more letters, a hyphen, then one or more digits). Headings whose ID doesn't fit this pattern are treated as regular prose, not stories.

**Examples:**
```markdown
### US-001: User Registration
### AUTH-003: Password Reset Flow
### BUG-012: Fix Login Redirect
#### US-042: Also works as a level-4 heading
```

## Story Fields

Below each story heading, Chief recognizes these bold-label fields:

| Field | Format | Required | Default | Description |
|-------|--------|----------|---------|-------------|
| Status | `**Status:** value` | No | `todo` | Current state: `done`, `in-progress`, `todo`, or `needs-review` |
| Priority | `**Priority:** N` | No | Document order | Execution order (lower = higher priority) |
| Description | `**Description:** text` | No | — | Story description (or use freeform prose) |

## Acceptance Criteria

Acceptance criteria use markdown checkboxes:

```markdown
- [ ] Criterion not yet met
- [x] Criterion completed
```

Chief reads checkbox state to track progress. The agent checks boxes as it completes each criterion.

## Status Values

| Value | Aliases (also accepted) | Meaning |
|-------|-------------------------|---------|
| `done` | `complete`, `completed`, `passed` | Story is complete — Chief skips it |
| `in-progress` | `in progress`, `started` | Agent is actively working on this story |
| `todo` | *(anything unrecognized falls back to this)* | Story is pending (also the default if Status is absent) |
| `needs-review` | `needs review`, `blocked` | Chief parked the story (e.g. after repeated failed attempts). Skipped by the loop and flagged with ⚑ in the TUI until a human resets it. |

Status matching is case-insensitive. Any value Chief doesn't recognize is treated as `todo` rather than raising an error.

## Full Example

```markdown
# User Authentication

## Overview
Complete auth system with login, registration, and password reset.

## Technical Context
- Backend: Express.js with TypeScript
- Database: PostgreSQL with Prisma ORM
- Auth: JWT tokens in httpOnly cookies

## User Stories

### US-001: User Registration

**Status:** done
**Priority:** 1
**Description:** As a new user, I want to register an account so that I can access the application.

- [x] Registration form with email and password fields
- [x] Email format validation
- [x] Password minimum 8 characters
- [x] Confirmation email sent on registration
- [x] User redirected to login after registration

### US-002: User Login

**Status:** todo
**Priority:** 2
**Description:** As a registered user, I want to log in so that I can access my account.

- [ ] Login form with email and password fields
- [ ] Error message for invalid credentials
- [ ] Remember me checkbox
- [ ] Redirect to dashboard on success
```

## Field Details

### id (from heading)

Parsed from the story heading: `### US-001: Title` → id is `US-001`.

**Format:** `LETTERS-NUMBERS` (one or more letters, a hyphen, one or more digits). Headings that don't match aren't parsed as stories.

**Example:** `US-001`, `US-042`, `AUTH-001`

### title (from heading)

Parsed from the story heading: `### US-001: User Registration` → title is `User Registration`.

**Length:** Keep under 50 characters for clean commit messages.

### description

The text after `**Description:**`, or freeform prose between the heading and the first checkbox list.

**Format:** `"As a [user], I want [feature] so that [benefit]."` recommended but not required.

### acceptanceCriteria (checkboxes)

The `- [ ]` / `- [x]` items under each story heading. The agent uses these to know when the story is complete.

**Guidelines:**
- Specific and testable
- One requirement per item
- 3-7 items per story

### priority

Lower numbers = higher priority. Chief always picks the incomplete story with the lowest priority number first. If omitted, stories are selected in document order.

**Range:** Any positive number (integers or decimals). A value that isn't a positive number is ignored, and the story falls back to document order.

### status

Tracked by Chief. Set to `in-progress` when work begins, `done` when the agent outputs `<chief-done/>` and a matching commit lands, and `needs-review` when a story is parked after repeated failed attempts.

**Values:** `done`, `in-progress`, `todo` (default if absent), `needs-review` — plus the case-insensitive aliases listed under [Status Values](#status-values)

## Parsing Behavior

Chief parses `prd.md` leniently rather than validating it against a strict schema. It reads the markdown structure and extracts whatever it recognizes; it does not reject a PRD for missing or malformed fields. Concretely:

- **Only file-read errors fail.** If `prd.md` can be read, parsing succeeds. There's no separate validation step that exits with an error for structural problems.
- **Unrecognized headings are skipped.** A heading whose ID doesn't match `LETTERS-NUMBERS` is treated as prose, not a story — so a typo silently drops the story rather than raising an error.
- **Unknown status values fall back to `todo`** instead of erroring.
- **Non-positive or non-numeric priorities are ignored**, leaving the story in document order.
- **Duplicate IDs are not detected.** Chief keeps every parsed story as-is; it does not deduplicate or warn.

Because of this, the most common "why isn't my story being picked up?" cause is a heading whose ID doesn't fit the `LETTERS-NUMBERS` pattern. See [Common Issues → Invalid PRD Format](/troubleshooting/common-issues) for how to spot it.
