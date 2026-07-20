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

// GetPrompt returns the agent prompt with the progress path and
// current story context substituted. The storyContext is the JSON of the
// current story to work on, inlined directly into the prompt so that the
// agent does not need to read the entire prd.md file.
//
// reviewSkill, when non-empty, is the name of a project-specific skill (e.g.
// "/code-quality") that the agent must run to review its changes before
// committing. When empty, no review step is injected and the prompt is
// identical to the pre-review behavior.
func GetPrompt(progressPath, storyContext, storyID, storyTitle, reviewSkill string) string {
	result := strings.ReplaceAll(promptTemplate, "{{PROGRESS_PATH}}", progressPath)
	result = strings.ReplaceAll(result, "{{STORY_CONTEXT}}", storyContext)
	result = strings.ReplaceAll(result, "{{STORY_ID}}", storyID)
	result = strings.ReplaceAll(result, "{{STORY_TITLE}}", storyTitle)
	return strings.ReplaceAll(result, "{{QUALITY_REVIEW}}", reviewInstruction(reviewSkill))
}

// reviewInstruction builds the code-quality review step inserted before the
// commit step. It returns an empty string when no review skill is configured,
// so the numbered steps stay contiguous.
func reviewInstruction(reviewSkill string) string {
	reviewSkill = strings.TrimSpace(reviewSkill)
	if reviewSkill == "" {
		return ""
	}
	return "3a. Before committing, run the `" + reviewSkill + "` skill to review ALL changes " +
		"you made for this story against the project's code-quality standards. Fix anything it " +
		"flags, then re-run it. Only proceed to commit once the review passes; if it cannot be " +
		"made to pass, do NOT commit and do NOT output <chief-done/>.\n"
}

// questionFormatNative instructs the agent to ask each grilling question through
// a native multiple-choice UI (Claude Code's question tool).
const questionFormatNative = `### Every question carries your recommended answer

Ask each question with the AskUserQuestion tool. It renders a native picker the
user navigates with the arrow keys — far clearer than reading lettered options
and typing "1A". Rules:

- One question per tool call. Never batch several questions into one call.
- Offer only the 2–4 genuine choices.
- Put your recommended choice FIRST and append " (Recommended)" to its label; put
  the one-line reason in that option's description.
- The tool always adds an "Other" free-text escape, so the user can redirect even
  when none of your options fit.

After each answer, ask the next question. Write nothing until every open decision
is resolved.`

// questionFormatLettered is the fallback for providers without a native question
// UI: one question per message, presented as indented lettered options.
const questionFormatLettered = `### Every question carries your recommended answer

For each question, give your recommended answer and a one-line reason, so the user
can simply confirm ("yes" / "go with your rec") or redirect. Show the
alternatives, but make the recommendation explicit. Present it like this:

    Question 3 — State persistence

    Should open sessions survive a restart?
      A. Yes, persist to localStorage   (recommended: you already do this in the
         settings panel, so it's consistent)
      B. No, sessions are ephemeral
      C. Persist only on explicit "save"

    (Confirm A, or pick another.)

Ask only one such block per message. Wait for the reply. Then ask the next.`

// questionFormat picks the grilling question format for a provider based on
// whether its interactive session can render native multiple-choice questions.
func questionFormat(nativeQuestions bool) string {
	if nativeQuestions {
		return questionFormatNative
	}
	return questionFormatLettered
}

// GetInitPrompt returns the PRD generator prompt with the PRD directory, optional
// context, and provider-appropriate question format substituted.
func GetInitPrompt(prdDir, context string, nativeQuestions bool) string {
	if context == "" {
		context = "No additional context provided. Ask the user what they want to build."
	}
	result := strings.ReplaceAll(initPromptTemplate, "{{PRD_DIR}}", prdDir)
	result = strings.ReplaceAll(result, "{{CONTEXT}}", context)
	return strings.ReplaceAll(result, "{{QUESTION_FORMAT}}", questionFormat(nativeQuestions))
}

// GetEditPrompt returns the PRD editor prompt with the PRD directory and
// provider-appropriate question format substituted.
func GetEditPrompt(prdDir string, nativeQuestions bool) string {
	result := strings.ReplaceAll(editPromptTemplate, "{{PRD_DIR}}", prdDir)
	return strings.ReplaceAll(result, "{{QUESTION_FORMAT}}", questionFormat(nativeQuestions))
}

// GetDetectSetupPrompt returns the prompt for detecting project setup commands.
func GetDetectSetupPrompt() string {
	return detectSetupPromptTemplate
}
