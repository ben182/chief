package cmd

import (
	"fmt"
	"os"

	"github.com/ben182/chief/embed"
	"github.com/ben182/chief/internal/loop"
)

// EditOptions contains configuration for the edit command.
type EditOptions struct {
	Name     string        // PRD name (default: "default")
	BaseDir  string        // Base directory for .chief/prds/ (default: current directory)
	Provider loop.Provider // Agent CLI provider (Claude or Codex)
}

// RunEdit edits an existing PRD by launching an interactive Claude session.
func RunEdit(opts EditOptions) error {
	// When no PRD name is given explicitly, infer it from the current git branch
	// (chief/<name>) so running `chief edit` from within a PRD's own branch edits
	// that PRD instead of silently falling back to "default".
	opts.Name = resolvePRDName(opts.Name, opts.BaseDir)

	name, baseDir, prdDir, prdMdPath, err := preparePRDPaths(opts.Name, opts.BaseDir)
	if err != nil {
		return err
	}
	opts.Name, opts.BaseDir = name, baseDir

	// Check if prd.md exists
	if _, err := os.Stat(prdMdPath); os.IsNotExist(err) {
		return fmt.Errorf("PRD not found at %s. Use 'chief new %s' to create it first", prdMdPath, opts.Name)
	}

	if opts.Provider == nil {
		return fmt.Errorf("edit command requires Provider to be set")
	}

	// Get the edit prompt with the PRD directory path
	prompt := embed.GetEditPrompt(prdDir, opts.Provider.SupportsInteractiveQuestions())

	// Launch interactive agent session
	fmt.Printf("Editing PRD at %s...\n", prdDir)
	fmt.Printf("Launching %s to help you edit your PRD...\n", opts.Provider.Name())
	fmt.Println()

	if err := runInteractiveAgent(opts.Provider, opts.BaseDir, prompt); err != nil {
		return fmt.Errorf("%s session failed: %w", opts.Provider.Name(), err)
	}

	fmt.Println("\nPRD editing complete!")

	// Validate the edited prd.md can be parsed
	warnIfPRDUnparsable(prdMdPath)

	fmt.Printf("\nYour PRD is updated! Run 'chief' or 'chief %s' to continue working on it.\n", opts.Name)
	return nil
}
