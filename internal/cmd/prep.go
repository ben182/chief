package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ben182/chief/embed"
	"github.com/ben182/chief/internal/box"
	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
	"github.com/ben182/chief/internal/prd"
)

// PrepOptions contains configuration for `chief prep` and `chief box prep`.
type PrepOptions struct {
	Name     string        // PRD name (default: inferred from the branch, else "default")
	BaseDir  string        // project root (default: current directory)
	Provider loop.Provider // agent CLI the session runs on
	// Box prepares the PRD for a run on the box rather than on this machine.
	Box bool
	// Out is where chief's own lines go. Nil means stdout.
	Out io.Writer
}

// canAsk says whether there is a person at a terminal to hold a prep session
// with. A variable so a test can stand in for the terminal.
var canAsk = func() bool { return isTerminal(os.Stdin) }

// errPrepNeedsTerminal is what every non-interactive path gets instead of a
// session nobody can answer. A prep that waits for input on a box, in a script
// or under systemd is a hang, and a hang looks like work.
var errPrepNeedsTerminal = fmt.Errorf("chief prep is a conversation and needs a terminal; run it where you can answer, or pass --skip-prep")

// PrepTarget is the stamp key and the operating system of what a prep is for:
// "box" and Linux for the box, runtime.GOOS for this machine.
func PrepTarget(forBox bool) (target, goos string) {
	if forBox {
		return prd.PrepTargetBox, box.OS
	}
	return runtime.GOOS, runtime.GOOS
}

// RunPrep goes through a PRD with its author before an unattended run: blocked
// stories, open decisions, contradictions, what the target cannot run, missing
// tools. The session edits prd.md (and, for the box, .chief/config.yaml);
// afterwards chief commits and pushes whatever it changed and stamps the PRD as
// prepared for the target, which is what `chief start` and `chief box up` ask
// for.
func RunPrep(opts PrepOptions) error {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	if !canAsk() {
		return errPrepNeedsTerminal
	}

	opts.Name = resolvePRDName(opts.Name, opts.BaseDir)
	name, baseDir, prdDir, prdMdPath, err := preparePRDPaths(opts.Name, opts.BaseDir)
	if err != nil {
		return err
	}
	opts.Name, opts.BaseDir = name, baseDir
	// A local run in a worktree reads the worktree's copy; a box is sent the
	// project's own, so that is the one to prepare for it.
	if !opts.Box {
		prdDir, prdMdPath = livePRDPaths(baseDir, name, prdDir, prdMdPath)
	}
	if _, err := os.Stat(prdMdPath); err != nil {
		return fmt.Errorf("PRD not found at %s. Use 'chief new %s' to create it first", prdMdPath, name)
	}
	if opts.Provider == nil {
		return fmt.Errorf("prep command requires Provider to be set")
	}

	cfg, err := config.Load(baseDir)
	if err != nil {
		return fmt.Errorf("failed to load .chief/config.yaml: %w", err)
	}
	configPath := filepath.Join(baseDir, ".chief", "config.yaml")
	target, goos := PrepTarget(opts.Box)

	prepTarget := embed.PrepTarget{Box: opts.Box, OS: prd.OSName(goos), ConfigPath: configPath}
	if opts.Box {
		prepTarget.Environment = box.Environment(baseDir, cfg)
	} else {
		prepTarget.Environment = localEnvironment()
	}

	gitignorePath := filepath.Join(prdDir, ".gitignore")
	before := snapshot(prdMdPath, configPath, gitignorePath)
	prompt := embed.GetPrepPrompt(prdDir, prepTarget, opts.Provider.SupportsInteractiveQuestions())

	where := "this machine (" + prd.OSName(goos) + ")"
	if opts.Box {
		where = "the box"
	}
	_, _ = fmt.Fprintf(out, "Preparing PRD %s for a run on %s...\n", name, where)
	_, _ = fmt.Fprintf(out, "Launching %s to go through it with you...\n\n", opts.Provider.Name())
	if err := runInteractiveAgent(opts.Provider, baseDir, prompt); err != nil {
		return fmt.Errorf("%s session failed: %w", opts.Provider.Name(), err)
	}
	warnIfPRDUnparsable(prdMdPath)

	if err := prd.RecordPrep(prdMdPath, target, time.Now()); err != nil {
		return fmt.Errorf("failed to record the prep: %w", err)
	}
	// Before the commit, so the ignore rule that keeps the stamp local travels
	// with it.
	git.IgnorePrepStampIn(prdDir, prd.PrepStampFile)

	var changed []string
	for _, path := range []string{prdMdPath, configPath, gitignorePath} {
		if now, _ := os.ReadFile(path); !bytes.Equal(now, before[path]) {
			changed = append(changed, path)
		}
	}
	if len(changed) > 0 {
		commitAndPushPrep(out, changed, fmt.Sprintf("chore: prep %s for %s", name, targetLabel(opts.Box, goos)))
	}
	_, _ = fmt.Fprintf(out, "\nPRD %s is prepared for %s.\n", name, where)
	return nil
}

// targetLabel names the target in a commit subject: "the box", "macOS".
func targetLabel(forBox bool, goos string) string {
	if forBox {
		return "the box"
	}
	return prd.OSName(goos)
}

// localEnvironment describes this machine for a local prep. What is installed
// is left to the session to look up — it can, and a list made here would be
// either incomplete or enormous.
func localEnvironment() string {
	return fmt.Sprintf("The run happens on this machine: %s (%s/%s), started with `chief start` in this\n"+
		"checkout. A story's `**Braucht:**` line is compared with %s here. The person may\n"+
		"walk away once it runs: a system dialog, a password prompt, or a locked password\n"+
		"manager in the middle of the run stops it just as it would on a server.",
		prd.OSName(runtime.GOOS), runtime.GOOS, runtime.GOARCH, prd.OSName(runtime.GOOS))
}

// snapshot reads the files a prep may change, so what it changed can be told
// afterwards. A file that does not exist yet is recorded as empty.
func snapshot(paths ...string) map[string][]byte {
	out := make(map[string][]byte, len(paths))
	for _, p := range paths {
		data, _ := os.ReadFile(p)
		out[p] = data
	}
	return out
}

// commitAndPushPrep commits what a prep session changed and pushes it, so the
// branch a box clones says what the prep settled. Files are committed in the
// checkout they belong to — a worktree's PRD in the worktree — and files the
// project keeps out of git stay out. Every failure is reported and none is
// fatal: the prep itself is done, and a box gets its PRD copied either way.
func commitAndPushPrep(out io.Writer, paths []string, message string) {
	byRoot := map[string][]string{}
	var roots []string
	for _, p := range paths {
		root, err := git.RepoRoot(filepath.Dir(p))
		if err != nil {
			continue
		}
		if _, seen := byRoot[root]; !seen {
			roots = append(roots, root)
		}
		byRoot[root] = append(byRoot[root], p)
	}
	for _, root := range roots {
		files := git.CommittablePaths(root, byRoot[root]...)
		if len(files) == 0 {
			continue
		}
		if err := git.CommitPaths(root, message, files...); err != nil {
			_, _ = fmt.Fprintf(out, "Warning: could not commit the prep's changes: %v\n", err)
			continue
		}
		_, _ = fmt.Fprintf(out, "Committed: %s\n", message)
		if !git.HasOrigin(root) {
			continue
		}
		branch, err := git.GetCurrentBranch(root)
		if err != nil || branch == "HEAD" {
			continue
		}
		if err := git.PushBranch(root, branch); err != nil {
			_, _ = fmt.Fprintf(out, "Warning: could not push %s: %v\n", branch, err)
			continue
		}
		_, _ = fmt.Fprintf(out, "Pushed %s\n", branch)
	}
}
