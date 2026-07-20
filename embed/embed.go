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

// GetInitPrompt returns the PRD generator prompt with the PRD directory and optional context substituted.
func GetInitPrompt(prdDir, context string) string {
	if context == "" {
		context = "No additional context provided. Ask the user what they want to build."
	}
	result := strings.ReplaceAll(initPromptTemplate, "{{PRD_DIR}}", prdDir)
	return strings.ReplaceAll(result, "{{CONTEXT}}", context)
}

// GetEditPrompt returns the PRD editor prompt with the PRD directory substituted.
func GetEditPrompt(prdDir string) string {
	return strings.ReplaceAll(editPromptTemplate, "{{PRD_DIR}}", prdDir)
}

// GetDetectSetupPrompt returns the prompt for detecting project setup commands.
func GetDetectSetupPrompt() string {
	return detectSetupPromptTemplate
}
