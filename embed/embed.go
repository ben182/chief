// Package embed provides embedded prompt templates used by Chief.
// All prompts are embedded at compile time using Go's embed directive.
package embed

import (
	_ "embed"
	"strings"
)

//go:embed prompt.txt
var promptTemplate string

//go:embed init_prompt.txt
var initPromptTemplate string

//go:embed edit_prompt.txt
var editPromptTemplate string

//go:embed followup_prompt.txt
var followupPromptTemplate string

//go:embed detect_setup_prompt.txt
var detectSetupPromptTemplate string

//go:embed summary_prompt.txt
var summaryPromptTemplate string

//go:embed review_prompt.txt
var reviewPromptTemplate string

// GetPrompt returns the agent prompt with the progress path and
// current story context substituted. The storyContext is the JSON of the
// current story to work on, inlined directly into the prompt so that the
// agent does not need to read the entire prd.md file.
func GetPrompt(progressPath, storyContext, prdName, storyID, storyTitle string) string {
	result := strings.ReplaceAll(promptTemplate, "{{PROGRESS_PATH}}", progressPath)
	result = strings.ReplaceAll(result, "{{STORY_CONTEXT}}", storyContext)
	result = strings.ReplaceAll(result, "{{PRD_NAME}}", prdName)
	result = strings.ReplaceAll(result, "{{STORY_ID}}", storyID)
	return strings.ReplaceAll(result, "{{STORY_TITLE}}", storyTitle)
}

// GetReviewPrompt returns the prompt for the separate review agent that runs
// after a story's build agent has committed. It reviews the story's changes
// with a fresh context, then fixes and re-commits anything it finds.
//
// storyContext is the current story's JSON (acceptance criteria etc.). skill is
// an optional project skill to run (Claude-only; empty to skip). instructions
// is optional free-form guidance. At least one of skill/instructions is set by
// the caller (otherwise the review is disabled and this is never called).
func GetReviewPrompt(progressPath, storyContext, storyID, storyTitle, skill, instructions string) string {
	result := strings.ReplaceAll(reviewPromptTemplate, "{{PROGRESS_PATH}}", progressPath)
	result = strings.ReplaceAll(result, "{{STORY_CONTEXT}}", storyContext)
	result = strings.ReplaceAll(result, "{{STORY_ID}}", storyID)
	result = strings.ReplaceAll(result, "{{STORY_TITLE}}", storyTitle)
	result = strings.ReplaceAll(result, "{{REVIEW_SKILL}}", reviewSkillBlock(skill))
	return strings.ReplaceAll(result, "{{REVIEW_INSTRUCTIONS}}", reviewInstructionsBlock(instructions))
}

// reviewSkillBlock renders the optional "run this skill" instruction, or an
// empty string when no skill is configured.
func reviewSkillBlock(skill string) string {
	skill = strings.TrimSpace(skill)
	if skill == "" {
		return ""
	}
	return "- Run the `" + skill + "` skill and address everything it flags.\n"
}

// reviewInstructionsBlock renders the optional free-form review guidance as its
// own paragraph, or an empty string when none is configured.
func reviewInstructionsBlock(instructions string) string {
	instructions = strings.TrimSpace(instructions)
	if instructions == "" {
		return ""
	}
	return "- Pay particular attention to the following project-specific concerns:\n\n" +
		instructions + "\n"
}

// questionFormatBatch grills the user in rounds following the batch-grill-me
// methodology: map the work as a design tree and, each round, ask the whole
// frontier of currently-answerable questions at once — every question carrying a
// recommended answer — then wait for the batch of answers before recomputing the
// frontier. It is deliberately presented in plain prose: the native
// multiple-choice picker (AskUserQuestion) is not used, so this one format
// applies to every provider.
const questionFormatBatch = `### Grill in rounds — ask the whole frontier at once

Map the work as a **design tree**: every decision branches into the decisions
that hang off it. Work the tree in **rounds**. The **frontier** is every decision
whose prerequisites are already settled — the questions you can ask *now* without
guessing at answers you haven't heard yet.

Ask the entire frontier in a single round, then **stop and wait** for the user's
answers before the next round. Do not trickle questions out one at a time, and do
not ask a question whose answer depends on another still open this round — that
one belongs to a *later* round. Each round the user's answers reshape the tree:
settled decisions push the frontier outward and unblock the questions that
depended on them. Recompute the frontier and ask the next round. Keep going until
the frontier is empty — every branch visited, nothing silently assumed.

**Present each round in plain prose — never a native multiple-choice picker or
question tool.** Number the questions so answers map back cleanly, and give every
question your recommended answer with a one-line reason, so the user can confirm
the whole batch ("all your recs") or redirect individual ones. Show the
alternatives, but make the recommendation explicit:

    Round 2 — persistence & limits

    1. State persistence — should open sessions survive a restart?
       A. Yes, persist to disk   (recommended: you already persist settings, so
          it's consistent)
       B. No, sessions are ephemeral
       C. Persist only on an explicit "save"

    2. Session cap — is there a maximum number of open sessions?
       A. No cap   (recommended: nothing in the current UI implies one)
       B. Cap at N

    (Confirm the recommendations, or redirect any of them.)

Wait for the round's answers, then compute and ask the next round. Write nothing
until the frontier is empty.`

// exploreModelClaude instructs the Claude interactive session to run codebase
// exploration on Opus via a subagent, so exploration quality does not depend on
// whichever model the user picked in the new/edit model picker (a lighter model
// like Fable is fine for the conversation, but should not drive exploration).
// It is prefixed with two newlines so it reads as its own paragraph after the
// "look it up" sentence it follows.
const exploreModelClaude = `

**Explore the codebase on Opus, always.** Do the exploration by delegating it to
a subagent with its model set to Opus — use the Explore agent if your setup has
one, otherwise a general-purpose subagent — no matter which model drives this
session. This keeps exploration high-quality even when the session runs on a
lighter, faster model. Do NOT read large parts of the repository inline on the
session model.`

// exploreModel returns the exploration-model instruction for a provider. Only
// Claude Code supports both the model picker and subagents with a per-call model
// override, so the block is injected only when native questions are available
// (the same signal used to detect Claude); other providers get an empty string.
func exploreModel(nativeQuestions bool) string {
	if nativeQuestions {
		return exploreModelClaude
	}
	return ""
}

// prototypeBlockClaude lets the interactive grill build a small, throwaway HTML
// prototype to settle a hard visual/interaction decision, delegating the build
// to an Opus subagent (the same subagent capability used for exploration). It is
// injected only for Claude; other providers (which may lack subagents) get
// nothing. The prototype lands in the PRD directory, which sits under .chief/ and
// is never committed, so it never pollutes the repo. The block leads with a
// newline so it reads as its own paragraph after the grill guidance that
// precedes it. It is shared by new/edit/followup, so it is deliberately
// section-neutral: it points the resolved decision at the stories' acceptance
// criteria and "the PRD's design section if it has one" rather than naming a
// section that only exists in the new prompt.
const prototypeBlockClaude = `
**Prototype a design question by showing it instead of describing it.** When a UI,
layout, or interaction decision would be quicker to settle by seeing it than by
talking it through, offer to build a small, self-contained
**throwaway HTML prototype**. Suggest it yourself the moment you sense it would
help — you do NOT
need a deadlock first — and briefly say what the prototype would show and which
decision it would resolve, then wait for the user to approve. Never build one
unprompted; the user nods first. Use judgment so you propose it when it genuinely
sharpens a decision rather than for every screen.

Once approved, delegate the build to a subagent with its model set to Opus (the
same way you delegate exploration), and have it write a single static ` + "`*.html`" + `
file (inline CSS/JS, no build step, no dependencies) into the PRD directory — the
same folder as ` + "`prd.md`" + `, which lives under ` + "`.chief/`" + ` and is never
committed, so the prototype never pollutes the repo. Tell the user to open it, and
use their reaction to resolve the decision.

Once the decision is made, **capture the decision itself in the PRD** — write it
into the relevant stories' acceptance criteria, and into the PRD's design/frontend
section if it has one. That is the
durable artifact, not the HTML. The prototype is then disposable: throw it
away, or keep it and reference it from the PRD if it is worth showing an
implementer later. Either way the PRD must stand on its own without it. And never
start building the actual feature — a prototype answers a question, it is not the
implementation.`

// prototypeBlock returns the throwaway-prototype instruction for a provider,
// gated on Claude (native questions) exactly like exploreModel, since it relies
// on the Opus subagent capability.
func prototypeBlock(nativeQuestions bool) string {
	if nativeQuestions {
		return prototypeBlockClaude
	}
	return ""
}

// GetInitPrompt returns the PRD generator prompt with the PRD directory, optional
// context, and provider-appropriate question format substituted.
func GetInitPrompt(prdDir, context string, nativeQuestions bool) string {
	if context == "" {
		context = "No additional context provided. Ask the user what they want to build."
	}
	result := strings.ReplaceAll(initPromptTemplate, "{{PRD_DIR}}", prdDir)
	result = strings.ReplaceAll(result, "{{CONTEXT}}", context)
	result = strings.ReplaceAll(result, "{{QUESTION_FORMAT}}", questionFormatBatch)
	result = strings.ReplaceAll(result, "{{PROTOTYPE}}", prototypeBlock(nativeQuestions))
	return strings.ReplaceAll(result, "{{EXPLORE_MODEL}}", exploreModel(nativeQuestions))
}

// GetEditPrompt returns the PRD editor prompt with the PRD directory and
// provider-appropriate question format substituted.
func GetEditPrompt(prdDir string, nativeQuestions bool) string {
	result := strings.ReplaceAll(editPromptTemplate, "{{PRD_DIR}}", prdDir)
	result = strings.ReplaceAll(result, "{{QUESTION_FORMAT}}", questionFormatBatch)
	result = strings.ReplaceAll(result, "{{PROTOTYPE}}", prototypeBlock(nativeQuestions))
	return strings.ReplaceAll(result, "{{EXPLORE_MODEL}}", exploreModel(nativeQuestions))
}

// GetFollowupPrompt returns the follow-up ingest prompt with the PRD directory,
// the inbox file path, and the provider-appropriate question/explore blocks
// substituted. It drives an interactive session that converts a raw follow-up
// inbox (e.g. todos.md) into structured user stories appended to prd.md.
func GetFollowupPrompt(prdDir, inboxPath string, nativeQuestions bool) string {
	result := strings.ReplaceAll(followupPromptTemplate, "{{PRD_DIR}}", prdDir)
	result = strings.ReplaceAll(result, "{{INBOX_PATH}}", inboxPath)
	result = strings.ReplaceAll(result, "{{QUESTION_FORMAT}}", questionFormatBatch)
	result = strings.ReplaceAll(result, "{{PROTOTYPE}}", prototypeBlock(nativeQuestions))
	return strings.ReplaceAll(result, "{{EXPLORE_MODEL}}", exploreModel(nativeQuestions))
}

// GetDetectSetupPrompt returns the prompt for detecting project setup commands.
func GetDetectSetupPrompt() string {
	return detectSetupPromptTemplate
}

// GetSummaryPrompt returns the run-summary prompt with the target file path, the
// commit list, and the optional parked-stories note substituted. commits is a
// one-line-per-commit log; parked lists stories that were left for human review
// (empty when none), which is rendered as an extra context block.
func GetSummaryPrompt(summaryPath, commits string, parked []string) string {
	parkedBlock := ""
	if len(parked) > 0 {
		parkedBlock = "\nThe following stories were parked for human review (they could not be" +
			" completed automatically) — call these out under \"Offene Punkte\":\n"
		for _, s := range parked {
			parkedBlock += "- " + s + "\n"
		}
	}
	result := strings.ReplaceAll(summaryPromptTemplate, "{{SUMMARY_PATH}}", summaryPath)
	result = strings.ReplaceAll(result, "{{COMMITS}}", commits)
	return strings.ReplaceAll(result, "{{PARKED}}", parkedBlock)
}
