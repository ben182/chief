// Package cmd provides CLI command implementations for Chief.
// This includes new, edit, status, and list commands that can be
// run from the command line without launching the full TUI.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ben182/chief/embed"
	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
	"github.com/ben182/chief/internal/prd"
)

// NewOptions contains configuration for the new command.
type NewOptions struct {
	Name     string        // PRD name (default: "default")
	Context  string        // Optional context to pass to the agent
	BaseDir  string        // Base directory for .chief/prds/ (default: current directory)
	Provider loop.Provider // Agent CLI provider (Claude or Codex)
}

// RunNew creates a new PRD by launching an interactive agent session.
func RunNew(opts NewOptions) error {
	// Set defaults
	if opts.Name == "" {
		opts.Name = "default"
	}
	if opts.BaseDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		opts.BaseDir = cwd
	}

	// Validate name (alphanumeric, -, _)
	if !isValidPRDName(opts.Name) {
		return fmt.Errorf("invalid PRD name %q: must contain only letters, numbers, hyphens, and underscores", opts.Name)
	}

	// Create directory structure: .chief/prds/<name>/
	prdDir := filepath.Join(opts.BaseDir, ".chief", "prds", opts.Name)
	if err := os.MkdirAll(prdDir, 0755); err != nil {
		return fmt.Errorf("failed to create PRD directory: %w", err)
	}

	// Check if prd.md already exists
	prdMdPath := filepath.Join(prdDir, "prd.md")
	if _, err := os.Stat(prdMdPath); err == nil {
		return fmt.Errorf("PRD already exists at %s. Use 'chief edit %s' to modify it", prdMdPath, opts.Name)
	}

	if opts.Provider == nil {
		return fmt.Errorf("new command requires Provider to be set")
	}

	// Give the PRD its own branch (chief/<name>) right away, matching how the
	// loop branches when a PRD is started. This keeps the PRD and its later
	// implementation off the default branch. It's a convenience, not a hard
	// requirement, so a git failure only warns and lets PRD authoring continue.
	if git.IsGitRepo(opts.BaseDir) {
		expectedBranch := fmt.Sprintf("chief/%s", opts.Name)
		if current, err := git.GetCurrentBranch(opts.BaseDir); err == nil && current != expectedBranch {
			if err := git.CreateBranch(opts.BaseDir, expectedBranch); err != nil {
				fmt.Printf("Warning: could not create branch %s: %v\n", expectedBranch, err)
			} else {
				fmt.Printf("Created branch %s\n", expectedBranch)
			}
		}
	}

	// Get the init prompt with the PRD directory path
	prompt := embed.GetInitPrompt(prdDir, opts.Context, opts.Provider.SupportsInteractiveQuestions())

	// Launch interactive agent session
	fmt.Printf("Creating PRD in %s...\n", prdDir)
	fmt.Printf("Launching %s to help you create your PRD...\n", opts.Provider.Name())
	fmt.Println()

	if err := runInteractiveAgent(opts.Provider, opts.BaseDir, prompt); err != nil {
		return fmt.Errorf("%s session failed: %w", opts.Provider.Name(), err)
	}

	// Check if prd.md was created
	if _, err := os.Stat(prdMdPath); os.IsNotExist(err) {
		// Clean up empty directory to prevent broken picker entries
		os.Remove(prdDir)
		fmt.Println("\nNo prd.md was created. Run 'chief new' again to try again.")
		return nil
	}

	// Validate the created prd.md can be parsed
	if _, err := prd.ParseMarkdownPRD(prdMdPath); err != nil {
		fmt.Printf("\nWarning: prd.md was created but could not be parsed: %v\n", err)
		fmt.Println("You may need to edit it to match the expected format.")
	} else {
		fmt.Println("\nPRD created successfully!")
	}

	fmt.Printf("\nYour PRD is ready! Run 'chief' or 'chief %s' to start working on it.\n", opts.Name)
	return nil
}

// runInteractiveAgent launches an interactive agent session in the specified directory.
func runInteractiveAgent(provider loop.Provider, workDir, prompt string) error {
	if provider == nil {
		return fmt.Errorf("interactive agent requires Provider to be set")
	}
	cmd := provider.InteractiveCommand(workDir, prompt)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// isValidPRDName checks if the name contains only valid characters.
func isValidPRDName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
