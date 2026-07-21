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
func GetPrompt(progressPath, storyContext, storyID, storyTitle string) string {
	result := strings.ReplaceAll(promptTemplate, "{{PROGRESS_PATH}}", progressPath)
	result = strings.ReplaceAll(result, "{{STORY_CONTEXT}}", storyContext)
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

// GetInitPrompt returns the PRD generator prompt with the PRD directory, optional
// context, and provider-appropriate question format substituted.
func GetInitPrompt(prdDir, context string, nativeQuestions bool) string {
	if context == "" {
		context = "No additional context provided. Ask the user what they want to build."
	}
	result := strings.ReplaceAll(initPromptTemplate, "{{PRD_DIR}}", prdDir)
	result = strings.ReplaceAll(result, "{{CONTEXT}}", context)
	result = strings.ReplaceAll(result, "{{QUESTION_FORMAT}}", questionFormatBatch)
	return strings.ReplaceAll(result, "{{EXPLORE_MODEL}}", exploreModel(nativeQuestions))
}

// GetEditPrompt returns the PRD editor prompt with the PRD directory and
// provider-appropriate question format substituted.
func GetEditPrompt(prdDir string, nativeQuestions bool) string {
	result := strings.ReplaceAll(editPromptTemplate, "{{PRD_DIR}}", prdDir)
	result = strings.ReplaceAll(result, "{{QUESTION_FORMAT}}", questionFormatBatch)
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
