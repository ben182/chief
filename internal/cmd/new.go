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

// resolveBaseDir returns base when non-empty, otherwise the current working
// directory. It is the shared default for the CLI commands' BaseDir option.
func resolveBaseDir(base string) (string, error) {
	if base != "" {
		return base, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	return cwd, nil
}

// preparePRDPaths applies the default PRD name, resolves the base directory, and
// validates the name, returning the resolved name/base and the PRD directory and
// prd.md path. It is the shared front matter of RunNew and RunEdit.
func preparePRDPaths(name, baseDir string) (resolvedName, resolvedBase, prdDir, prdMdPath string, err error) {
	if name == "" {
		name = "default"
	}
	baseDir, err = resolveBaseDir(baseDir)
	if err != nil {
		return "", "", "", "", err
	}
	// Validate name (alphanumeric, -, _)
	if !isValidPRDName(name) {
		return "", "", "", "", fmt.Errorf("invalid PRD name %q: must contain only letters, numbers, hyphens, and underscores", name)
	}
	prdDir = prd.PRDDir(baseDir, name)
	return name, baseDir, prdDir, filepath.Join(prdDir, "prd.md"), nil
}

// resolvePRDName applies branch-based PRD inference: when name is empty and the
// current git branch follows chief's chief/<name> convention with a matching
// PRD, it returns that name (and notes it on stdout); otherwise it returns name
// unchanged. Shared by RunEdit and RunFollowup so running either from a PRD's own
// branch targets that PRD instead of silently falling back to "default".
func resolvePRDName(name, baseDir string) string {
	if name != "" {
		return name
	}
	base, err := resolveBaseDir(baseDir)
	if err != nil {
		return name
	}
	inferred := PRDNameFromBranch(base)
	if inferred == "" {
		return name
	}
	fmt.Printf("Using PRD %q inferred from current branch chief/%s\n", inferred, inferred)
	return inferred
}

// warnIfPRDUnparsable re-parses prd.md after an interactive edit/followup session
// and warns if it no longer parses. Best-effort validation only — the session has
// already written the file, so a parse failure warns rather than fails.
func warnIfPRDUnparsable(prdMdPath string) {
	if _, err := prd.ParseMarkdownPRD(prdMdPath); err != nil {
		fmt.Printf("Warning: prd.md could not be parsed: %v\n", err)
	}
}

// RunNew creates a new PRD by launching an interactive agent session.
func RunNew(opts NewOptions) error {
	name, baseDir, prdDir, prdMdPath, err := preparePRDPaths(opts.Name, opts.BaseDir)
	if err != nil {
		return err
	}
	opts.Name, opts.BaseDir = name, baseDir

	// Create directory structure: .chief/prds/<name>/
	if err := os.MkdirAll(prdDir, 0755); err != nil {
		return fmt.Errorf("failed to create PRD directory: %w", err)
	}

	// Check if prd.md already exists
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
		// Clean up empty directory to prevent broken picker entries. Best-effort:
		// a leftover directory is cosmetic and the user is already being told to
		// re-run.
		_ = os.Remove(prdDir)
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

	// Scaffold a follow-up inbox so there's a ready place to collect
	// post-implementation follow-ups for `chief followup`. Best-effort — a
	// failure here should never fail PRD creation.
	if err := scaffoldFollowupInbox(prdDir); err != nil {
		fmt.Printf("Warning: could not create todos.md: %v\n", err)
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
