package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ben182/chief/embed"
	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
	"github.com/ben182/chief/internal/prd"
)

// followupInboxNames lists the accepted follow-up inbox filenames, in preference
// order. A follow-up inbox is a flat markdown checklist the user fills in by hand
// while reviewing a finished PRD; `chief followup` converts its open items into
// structured user stories.
var followupInboxNames = []string{"todos.md", "followups.md", "follow-ups.md"}

// FollowupOptions contains configuration for the followup command.
type FollowupOptions struct {
	Name     string        // PRD name (default: "default")
	BaseDir  string        // Base directory for .chief/prds/ (default: current directory)
	Provider loop.Provider // Agent CLI provider (Claude or Codex)
}

// followupInboxScaffold is the starter content written into a new PRD's
// todos.md. It is comment-only (no checkbox items), so `chief followup` reports
// "nothing to ingest" until the user actually adds items — an empty open item
// would otherwise become a meaningless story.
const followupInboxScaffold = `<!--
Follow-up inbox for this PRD.

While reviewing the finished feature by hand, jot down the fixes and polish
items you find here as a flat markdown checklist, one per line:

    - [ ] Media card should be hidden when no media is attached
    - [ ] Add a download button to the media view

Then run:

    chief followup

Each open "- [ ]" item is converted into a new user story appended to prd.md
(Status: todo), which the loop picks up on the next run. Converted items are
flipped to "- [x]" with their new story ID, so re-running never duplicates them.
-->
`

// scaffoldFollowupInbox writes a starter todos.md into prdDir so a new PRD has a
// ready place to collect post-implementation follow-ups. It never overwrites an
// existing inbox file (any of followupInboxNames) — it is a convenience, so a
// write failure is returned for the caller to treat as non-fatal.
func scaffoldFollowupInbox(prdDir string) error {
	if findFollowupInbox(prdDir) != "" {
		return nil // an inbox already exists — leave it untouched
	}
	return os.WriteFile(filepath.Join(prdDir, followupInboxNames[0]), []byte(followupInboxScaffold), 0644)
}

// findFollowupInbox returns the path to the first existing follow-up inbox file
// in prdDir (see followupInboxNames), or "" when none exists.
func findFollowupInbox(prdDir string) string {
	for _, name := range followupInboxNames {
		path := filepath.Join(prdDir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// prdNameFromBranch infers a PRD name from the current git branch when it
// follows chief's chief/<name> convention (e.g. branch chief/linkedin-post-media
// -> "linkedin-post-media"). It only returns a name whose prd.md actually exists
// under baseDir, so a stray chief/ branch without a matching PRD leaves the
// normal "default" resolution untouched. Returns "" when nothing applies.
func prdNameFromBranch(baseDir string) string {
	if !git.IsGitRepo(baseDir) {
		return ""
	}
	branch, err := git.GetCurrentBranch(baseDir)
	if err != nil {
		return ""
	}
	name, ok := strings.CutPrefix(branch, "chief/")
	if !ok || !isValidPRDName(name) {
		return ""
	}
	if _, err := os.Stat(filepath.Join(prd.PRDDir(baseDir, name), "prd.md")); err != nil {
		return ""
	}
	return name
}

// RunFollowup converts a PRD's follow-up inbox (e.g. todos.md) into structured
// user stories appended to prd.md, by launching an interactive agent session.
// The PRD must already exist and carry an inbox file; new stories are appended
// as `todo` so the normal loop picks them up next.
func RunFollowup(opts FollowupOptions) error {
	// When no PRD name is given explicitly, try to infer it from the current git
	// branch. Chief branches follow the chief/<name> convention (see RunNew), so
	// running `chief followup` from within a PRD's own branch picks up that PRD
	// instead of silently falling back to "default".
	if opts.Name == "" {
		if base, err := resolveBaseDir(opts.BaseDir); err == nil {
			if inferred := prdNameFromBranch(base); inferred != "" {
				fmt.Printf("Using PRD %q inferred from current branch chief/%s\n", inferred, inferred)
				opts.Name = inferred
			}
		}
	}

	name, baseDir, prdDir, prdMdPath, err := preparePRDPaths(opts.Name, opts.BaseDir)
	if err != nil {
		return err
	}
	opts.Name, opts.BaseDir = name, baseDir

	// The PRD must exist — follow-ups extend an already-authored PRD.
	if _, err := os.Stat(prdMdPath); os.IsNotExist(err) {
		return fmt.Errorf("PRD not found at %s. Use 'chief new %s' to create it first", prdMdPath, opts.Name)
	}

	inboxPath := findFollowupInbox(prdDir)
	if inboxPath == "" {
		return fmt.Errorf("no follow-up inbox found in %s. Create a %s with your follow-up items as a markdown checklist (- [ ] ...), then run 'chief followup %s' again", prdDir, followupInboxNames[0], opts.Name)
	}

	if opts.Provider == nil {
		return fmt.Errorf("followup command requires Provider to be set")
	}

	prompt := embed.GetFollowupPrompt(prdDir, inboxPath, opts.Provider.SupportsInteractiveQuestions())

	// Launch interactive agent session
	fmt.Printf("Ingesting follow-ups from %s...\n", inboxPath)
	fmt.Printf("Launching %s to convert them into user stories...\n", opts.Provider.Name())
	fmt.Println()

	if err := runInteractiveAgent(opts.Provider, opts.BaseDir, prompt); err != nil {
		return fmt.Errorf("%s session failed: %w", opts.Provider.Name(), err)
	}

	fmt.Println("\nFollow-up ingest complete!")

	// Validate the edited prd.md can still be parsed
	if _, err := prd.ParseMarkdownPRD(prdMdPath); err != nil {
		fmt.Printf("Warning: prd.md could not be parsed: %v\n", err)
	}

	fmt.Printf("\nFollow-ups added! Run 'chief' or 'chief %s' to work through them.\n", opts.Name)
	return nil
}
