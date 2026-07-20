package embed

import (
	"strings"
	"testing"
)

func TestGetPrompt(t *testing.T) {
	progressPath := "/path/to/progress.md"
	storyContext := `{"id":"US-001","title":"Test Story"}`
	prompt := GetPrompt(progressPath, storyContext, "US-001", "Test Story", "")

	// Verify all placeholders were substituted
	if strings.Contains(prompt, "{{PROGRESS_PATH}}") {
		t.Error("Expected {{PROGRESS_PATH}} to be substituted")
	}
	if strings.Contains(prompt, "{{STORY_CONTEXT}}") {
		t.Error("Expected {{STORY_CONTEXT}} to be substituted")
	}
	if strings.Contains(prompt, "{{STORY_ID}}") {
		t.Error("Expected {{STORY_ID}} to be substituted")
	}
	if strings.Contains(prompt, "{{STORY_TITLE}}") {
		t.Error("Expected {{STORY_TITLE}} to be substituted")
	}

	// Verify the commit message contains the exact story ID and title
	if !strings.Contains(prompt, "feat: US-001 - Test Story") {
		t.Error("Expected prompt to contain exact commit message 'feat: US-001 - Test Story'")
	}

	// Verify the progress path appears in the prompt
	if !strings.Contains(prompt, progressPath) {
		t.Errorf("Expected prompt to contain progress path %q", progressPath)
	}

	// Verify the story context is inlined in the prompt
	if !strings.Contains(prompt, storyContext) {
		t.Error("Expected prompt to contain inlined story context")
	}

	// Verify the prompt contains chief-done stop condition
	if !strings.Contains(prompt, "chief-done") {
		t.Error("Expected prompt to contain chief-done instruction")
	}
}

func TestGetPrompt_NoFileReadInstruction(t *testing.T) {
	prompt := GetPrompt("/path/progress.md", `{"id":"US-001"}`, "US-001", "Test Story", "")

	// The prompt should NOT instruct Claude to read the PRD file
	if strings.Contains(prompt, "Read the PRD") {
		t.Error("Expected prompt to NOT contain 'Read the PRD' file-read instruction")
	}
}

func TestPromptTemplateNotEmpty(t *testing.T) {
	if promptTemplate == "" {
		t.Error("Expected promptTemplate to be embedded and non-empty")
	}
}

func TestGetPrompt_ChiefExclusion(t *testing.T) {
	prompt := GetPrompt("/path/progress.md", `{"id":"US-001"}`, "US-001", "Test Story", "")

	// The prompt must instruct Claude to never stage or commit .chief/ files
	if !strings.Contains(prompt, ".chief/") {
		t.Error("Expected prompt to contain .chief/ exclusion instruction")
	}
	if !strings.Contains(prompt, "NEVER stage or commit") {
		t.Error("Expected prompt to explicitly say NEVER stage or commit .chief/ files")
	}
}

func TestGetPrompt_ReviewSkill(t *testing.T) {
	// With no review skill, no review step and no leftover placeholder.
	empty := GetPrompt("/p.md", `{"id":"US-001"}`, "US-001", "Test Story", "")
	if strings.Contains(empty, "{{QUALITY_REVIEW}}") {
		t.Error("Expected {{QUALITY_REVIEW}} placeholder to be substituted")
	}
	if strings.Contains(empty, "3a.") {
		t.Error("Expected no review step when reviewSkill is empty")
	}

	// With a review skill, the step is injected and references the skill.
	withSkill := GetPrompt("/p.md", `{"id":"US-001"}`, "US-001", "Test Story", "/code-quality")
	if strings.Contains(withSkill, "{{QUALITY_REVIEW}}") {
		t.Error("Expected {{QUALITY_REVIEW}} placeholder to be substituted")
	}
	if !strings.Contains(withSkill, "/code-quality") {
		t.Error("Expected review step to reference the configured skill")
	}
	if !strings.Contains(withSkill, "3a.") {
		t.Error("Expected review step to be inserted before the commit step")
	}
	// The review step must sit before the commit step.
	if strings.Index(withSkill, "3a.") > strings.Index(withSkill, "4. If checks pass, commit") {
		t.Error("Expected review step to precede the commit step")
	}
	// Whitespace-only skill is treated as disabled.
	blank := GetPrompt("/p.md", `{"id":"US-001"}`, "US-001", "Test Story", "   ")
	if strings.Contains(blank, "3a.") {
		t.Error("Expected whitespace-only reviewSkill to disable the review step")
	}
}

func TestGetInitPrompt(t *testing.T) {
	prdDir := "/path/to/.chief/prds/main"

	// Test with no context
	prompt := GetInitPrompt(prdDir, "", false)
	if !strings.Contains(prompt, "No additional context provided") {
		t.Error("Expected default context message")
	}

	// Verify PRD directory is substituted
	if !strings.Contains(prompt, prdDir) {
		t.Errorf("Expected prompt to contain PRD directory %q", prdDir)
	}
	if strings.Contains(prompt, "{{PRD_DIR}}") {
		t.Error("Expected {{PRD_DIR}} to be substituted")
	}
	if strings.Contains(prompt, "{{QUESTION_FORMAT}}") {
		t.Error("Expected {{QUESTION_FORMAT}} to be substituted")
	}

	// Test with context
	context := "Build a todo app"
	promptWithContext := GetInitPrompt(prdDir, context, false)
	if !strings.Contains(promptWithContext, context) {
		t.Error("Expected context to be substituted in prompt")
	}
}

func TestGetInitPromptQuestionFormat(t *testing.T) {
	prdDir := "/path/to/.chief/prds/main"

	native := GetInitPrompt(prdDir, "", true)
	if !strings.Contains(native, "AskUserQuestion tool") {
		t.Error("Expected native question format to reference the AskUserQuestion tool")
	}

	lettered := GetInitPrompt(prdDir, "", false)
	if strings.Contains(lettered, "AskUserQuestion tool") {
		t.Error("Expected lettered fallback to omit the native question tool")
	}
	if !strings.Contains(lettered, "lettered") && !strings.Contains(lettered, "A. ") {
		t.Error("Expected lettered fallback to present lettered options")
	}
}

func TestGetEditPrompt(t *testing.T) {
	prompt := GetEditPrompt("/test/path/prds/main", false)
	if prompt == "" {
		t.Error("Expected GetEditPrompt() to return non-empty prompt")
	}
	if !strings.Contains(prompt, "/test/path/prds/main") {
		t.Error("Expected prompt to contain the PRD directory path")
	}
	if strings.Contains(prompt, "{{QUESTION_FORMAT}}") {
		t.Error("Expected {{QUESTION_FORMAT}} to be substituted")
	}
	if !strings.Contains(GetEditPrompt("/test/path/prds/main", true), "AskUserQuestion tool") {
		t.Error("Expected native edit prompt to reference the AskUserQuestion tool")
	}
}
