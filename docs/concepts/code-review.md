---
description: How Chief's separate review agent works — a second, independent agent that adversarially reviews and fixes each story's committed changes with a fresh context.
---

# The Review Agent

By default, Chief marks a story done as soon as the build agent commits its
work. With a review configured, Chief inserts a second opinion first: after the
build agent commits, a **separate agent with a fresh context** reviews the
committed changes, fixes anything it finds, and only then is the story signed
off.

::: info Multi-agent support
This page uses "the agent" to refer to whichever backend you've configured
(Claude Code, Codex, OpenCode, Cursor, or Gemini). The review agent uses the
same backend as the build agent. See [Configuration](/reference/configuration).
:::

## Why a separate agent

Chief has always been able to run a review — but historically it did so by
injecting a line into the build agent's *own* prompt: "before you commit, run
the `/code-quality` skill, fix anything it flags, then commit." That is
**self-review**: the same agent, in the same session, checking its own work.

Self-review has structural blind spots:

- **The blind spot stays blind.** An agent that misread a requirement reviews it
  with the same misunderstanding. It just convinced itself the code is correct —
  it's the worst-placed reviewer to disprove that.
- **Context bias.** It sees its own chain of reasoning and rationalizes it rather
  than taking it apart.
- **It's cooperative, not adversarial.** The goal is "get the commit through,"
  not "find what's wrong with this."

The review agent is structurally different. It runs as a **fresh process with a
fresh context** — it never sees the build agent's reasoning, only the diff and
the story's acceptance criteria, like a colleague reviewing a pull request
rather than the author re-reading their own patch. Its instructions are
adversarial by design: *find what's wrong.*

::: tip Analogy
`review` used to be a linter the author ran on themselves. The review agent is a
code review from a teammate who doesn't already have your approach in their head.
:::

## Where it runs in the loop

The review slots in between the build agent's commit and the story being marked
done:

```
        ┌──────────────┐
        │ Build agent  │  implements the story, runs checks, commits,
        │              │  emits <chief-done/>
        └──────┬───────┘
               │
               ▼
        ┌──────────────┐   no matching commit? → treated as a failed attempt
        │ Commit check │──────────────────────────────────────────────┐
        └──────┬───────┘                                               │
               │ commit found                                          │
               ▼                                                       │
        ┌──────────────┐   review configured?                          │
        │ Review agent │   • fresh context, sees only the diff         │
        │ (separate    │   • reviews adversarially                     │
        │  process)    │   • fixes issues, amends the story's commit   │
        └──────┬───────┘   • emits <chief-done/>                       │
               │                                                       │
               ▼                                                       ▼
        ┌──────────────┐                                     ┌──────────────┐
        │ Story → done │                                     │ Retry / park │
        └──────────────┘                                     └──────────────┘
```

Because the review agent fixes problems itself and folds them into the story's
existing commit (`git commit --amend --no-edit`), the "one commit per story"
history stays intact.

## Configuration

Enable the review by setting either or both fields under `review` in
`.chief/config.yaml`:

```yaml
review:
  skill: "/code-quality"        # optional project skill (Claude only)
  instructions: |               # optional free-form guidance (any provider)
    Watch for N+1 queries and make sure new behavior has tests.
    Flag any public function added without a doc comment.
```

- **`review.skill`** — a project skill the review agent runs as part of its
  review (e.g. `/code-quality`). This is Claude-specific; other providers ignore
  it.
- **`review.instructions`** — free-form text steering what the reviewer should
  pay attention to. Works with any provider.

Setting **either one enables the review**. Leaving both empty (the default)
disables it, and Chief marks stories done straight after the build commit as
before.

## How the review prompt is assembled

The prompt the review agent receives is built in two layers: a fixed template
plus values substituted at runtime.

### Layer 1 — the fixed template

The template (`embed/review_prompt.txt`, compiled into the binary) defines the
reviewer's *stance* and never changes. Its sections:

| Section | What it does |
|---------|--------------|
| **Role** | "You are a critical, independent code reviewer. You did NOT write that code and have no stake in defending it." — the adversarial core. |
| **What to review** | Inspect the diff (`git show HEAD`), read surrounding code for context, but do **not** re-read the whole repo or redo the story. |
| **How to review** | A checklist: partially-met acceptance criteria, missed edge cases, missing/weak tests, deviations from existing patterns, stray `.chief/` files. |
| **Fixing** | Fix issues yourself, run the project's checks, then `git commit --amend --no-edit` (staging only changed files). If the work is already correct, change nothing. |
| **Progress note** | Append a short review note to `progress.md`. |
| **Stop condition** | Emit `<chief-done/>` once the review is complete and fixes are committed. |

### Layer 2 — the substituted values

`embed.GetReviewPrompt(...)` fills the template's placeholders per story:

| Placeholder | Source | Contents |
|-------------|--------|----------|
| `{{STORY_CONTEXT}}` | the PRD | the story as JSON — title, description, acceptance criteria; what the reviewer checks against |
| `{{PROGRESS_PATH}}` | the PRD dir | path to `progress.md` for the review note |
| `{{STORY_ID}}` | loop state | e.g. `US-001` |
| `{{REVIEW_SKILL}}` | `review.skill` | rendered as `- Run the \`/code-quality\` skill and address everything it flags.` |
| `{{REVIEW_INSTRUCTIONS}}` | `review.instructions` | your free-form text, rendered as its own "pay particular attention to…" paragraph |

The two configurable blocks are **conditional**: when a field is empty (or
whitespace-only), its block resolves to an empty string and disappears
entirely — no dangling bullet, no empty header. So if you set only
`instructions`, the reviewer gets the fixed template plus exactly your text at
the "concerns" spot, with no skill line at all.

Your free-form text lands **verbatim as its own paragraph** inside the "how to
review" section — it *augments* the standard checklist, it doesn't replace it.

## What you see in the log

When a review is configured, the story markers make the extra phase explicit:

```
────────────────────────────────
  ✓ Build done — review pending      (yellow — the build agent finished, but the story isn't signed off yet)
────────────────────────────────

  🔍 Reviewing changes

  ✓ Review complete                  (green — now the story is done)
```

Without a review configured, you get the familiar single green `✓ Story done`.

## Behavior notes

- **Best-effort.** The review is a quality gate, not a blocker. If the review
  agent crashes or fails to finish cleanly, Chief logs it and still marks the
  story done — a flaky reviewer never stalls the loop or loses the build agent's
  committed work.
- **Same backend and model.** The review agent runs on the same provider (and
  model) as the build agent. There is currently no separate `review.model`
  override.
- **Runs on every story.** The review runs once per completed story, so it adds
  an agent invocation (and cost) per story. Factor that in on large PRDs.

## See also

- [Configuration → review](/reference/configuration) — the config keys.
- [How Chief Works](/concepts/how-it-works) — the overall loop.
- [The .chief Directory](/concepts/chief-directory) — where `progress.md` and
  commits live.
