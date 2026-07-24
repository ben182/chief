package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ben182/chief/internal/agent"
	"github.com/ben182/chief/internal/cli"
	"github.com/ben182/chief/internal/cmd"
	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/loop"
	"github.com/ben182/chief/internal/prd"
	"github.com/ben182/chief/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

// Version is set at build time via ldflags
var Version = "dev"

func main() {
	// Handle subcommands first
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "new":
			runNew()
			return
		case "edit":
			runEdit()
			return
		case "followup":
			runFollowup()
			return
		case "status":
			runStatus()
			return
		case "list":
			runList()
			return
		case "help", "--help", "-h":
			printHelp()
			return
		case "--version", "-v":
			fmt.Printf("chief version %s\n", Version)
			return
		case "wiggum":
			printWiggum()
			return
		case "start":
			// chief start [name] [flags]: launch the TUI and begin the loop
			// automatically. Drop "start" from args so the normal TUI parser
			// handles the remaining name/flags.
			os.Args = append(os.Args[:1], os.Args[2:]...)
			opts := parseTUIOptions()
			if opts == nil {
				return
			}
			opts.AutoStart = true
			runTUIWithOptions(opts)
			return
		}
	}

	// Parse flags for TUI mode
	opts := parseTUIOptions()
	if opts == nil {
		// Already handled (--help or --version)
		return
	}

	// Run the TUI
	runTUIWithOptions(opts)
}

// parseTUIOptions parses os.Args for TUI mode. It turns cli.ParseArgs' errors
// and the --help/--version flags into program behavior (print + exit/stop),
// returning nil when the program should stop without running the TUI.
func parseTUIOptions() *cli.Options {
	opts, err := cli.ParseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintf(os.Stderr, "Run 'chief --help' for usage.\n")
		os.Exit(1)
	}
	if opts.ShowHelp {
		printHelp()
		return nil
	}
	if opts.ShowVersion {
		fmt.Printf("chief version %s\n", Version)
		return nil
	}
	return opts
}

// fatal prints "Error: <err>" to stderr and exits with status 1. It bundles the
// fmt.Fprintf(os.Stderr, ...) + os.Exit(1) idiom used throughout the CLI.
func fatal(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

// fatalf is fatal with a formatted message (no wrapped error value).
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}

func runNew() {
	opts := cmd.NewOptions{}

	// Parse arguments: chief new [name] [context...] [--agent X] [--agent-path X]
	flagAgent, flagPath, flagModel, positional, err := cli.AgentFlags(os.Args, 2)
	if err != nil {
		fatal(err)
	}
	// Filter out remaining flags, keep only positional args
	var args []string
	for _, a := range positional {
		if !strings.HasPrefix(a, "-") {
			args = append(args, a)
		}
	}
	if len(args) > 0 {
		opts.Name = args[0]
	}
	if len(args) > 1 {
		opts.Context = strings.Join(args[1:], " ")
	}

	opts.Provider = resolveProvider(flagAgent, flagPath, flagModel)
	if !selectModelForProvider(opts.Provider, "Create PRD", flagModel) {
		return // user cancelled the model select
	}
	if err := cmd.RunNew(opts); err != nil {
		fatal(err)
	}
}

func runEdit() {
	opts := cmd.EditOptions{}

	// Parse arguments: chief edit [name] [--agent X] [--agent-path X]
	flagAgent, flagPath, flagModel, remaining, err := cli.AgentFlags(os.Args, 2)
	if err != nil {
		fatal(err)
	}
	for _, arg := range remaining {
		if opts.Name == "" && !strings.HasPrefix(arg, "-") {
			opts.Name = arg
		}
	}

	opts.Provider = resolveProvider(flagAgent, flagPath, flagModel)
	if !selectModelForProvider(opts.Provider, "Edit PRD", flagModel) {
		return // user cancelled the model select
	}
	if err := cmd.RunEdit(opts); err != nil {
		fatal(err)
	}
}

func runFollowup() {
	opts := cmd.FollowupOptions{}

	// Parse arguments: chief followup [name] [--agent X] [--agent-path X]
	flagAgent, flagPath, flagModel, remaining, err := cli.AgentFlags(os.Args, 2)
	if err != nil {
		fatal(err)
	}
	for _, arg := range remaining {
		if opts.Name == "" && !strings.HasPrefix(arg, "-") {
			opts.Name = arg
		}
	}

	opts.Provider = resolveProvider(flagAgent, flagPath, flagModel)
	if !selectModelForProvider(opts.Provider, "Ingest follow-ups", flagModel) {
		return // user cancelled the model select
	}
	if err := cmd.RunFollowup(opts); err != nil {
		fatal(err)
	}
}

func runStatus() {
	opts := cmd.StatusOptions{}

	// Parse arguments: chief status [name]
	if len(os.Args) > 2 && !strings.HasPrefix(os.Args[2], "-") {
		opts.Name = os.Args[2]
	}

	if err := cmd.RunStatus(opts); err != nil {
		fatal(err)
	}
}

func runList() {
	opts := cmd.ListOptions{}

	if err := cmd.RunList(opts); err != nil {
		fatal(err)
	}
}

// resolveProvider loads config and resolves the agent provider, exiting on error.
func resolveProvider(flagAgent, flagPath, flagModel string) loop.Provider {
	cwd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	cfg, err := config.Load(cwd)
	if err != nil {
		fatalf("failed to load .chief/config.yaml: %v", err)
	}
	provider, err := agent.Resolve(flagAgent, flagPath, cfg, flagModel)
	if err != nil {
		fatal(err)
	}
	if err := agent.CheckInstalled(provider); err != nil {
		fatal(err)
	}
	return provider
}

// selectModelForProvider shows an interactive Claude model picker before an
// interactive PRD flow and applies the choice to the provider. It only runs for
// the Claude provider and is skipped when the user already pinned a model via
// the --model flag. Returns false if the user cancelled the picker (the caller
// should abort), true otherwise.
func selectModelForProvider(provider loop.Provider, title, flagModel string) bool {
	claude, ok := provider.(*agent.ClaudeProvider)
	if !ok || flagModel != "" {
		return true
	}
	model, cancelled, err := tui.RunModelSelect(title, claude.Model())
	if err != nil {
		fatal(err)
	}
	if cancelled {
		return false
	}
	claude.SetModel(model)
	return true
}

// runTUIWithOptions resolves the PRD, launches the TUI, and restarts it when the
// user asked to create/edit a PRD on exit. The restart is an explicit loop
// rather than a recursive self-call, so a session that repeatedly re-inits/edits
// can no longer grow the stack without bound.
func runTUIWithOptions(opts *cli.Options) {
	for {
		provider := resolveProvider(opts.Agent, opts.AgentPath, opts.Model)

		prdPath, ok := resolvePRDPath(opts, provider)
		if !ok {
			return // first-time setup was cancelled
		}

		maybeMigrate(filepath.Dir(prdPath))

		app, err := tui.NewAppWithOptions(prdPath, opts.MaxIterations, provider)
		if err != nil {
			reportAppInitError(prdPath, err)
		}

		// Apply per-run flags.
		if opts.Verbose {
			app.SetVerbose(true)
		}
		if opts.NoRetry {
			app.DisableRetry()
		}
		if opts.AutoStart {
			app.SetAutoStart(true)
		}

		p := tea.NewProgram(app, tea.WithAltScreen())
		model, err := p.Run()
		if err != nil {
			fmt.Printf("Error running program: %v\n", err)
			os.Exit(1)
		}

		// A post-exit init/edit sets opts.PRDPath and loops back to relaunch the
		// TUI on the (new) PRD; anything else ends the session.
		if !handlePostExit(model, opts, provider) {
			return
		}
	}
}

// resolvePRDPath determines which prd.md to open. It honours an explicit
// --prd path, then a PRD inferred from the current chief/<name> branch, otherwise
// prefers "default" (falling back to "main" for older setups) or any other
// existing PRD, and finally runs first-time setup when no PRD exists. The bool is
// false only when the user cancelled first-time setup, in which case the caller
// should stop.
func resolvePRDPath(opts *cli.Options, provider loop.Provider) (string, bool) {
	if opts.PRDPath != "" {
		return opts.PRDPath, true
	}
	// Running bare `chief` from a PRD's own chief/<name> branch opens that PRD,
	// so you land on the story you're working on instead of "default".
	if name := cmd.PRDNameFromBranch(""); name != "" {
		fmt.Printf("Using PRD %q inferred from current branch chief/%s\n", name, name)
		return prd.PRDPath("", name), true
	}
	if defaultPath := prd.PRDPath("", "default"); fileExists(defaultPath) {
		return defaultPath, true
	}
	if mainPath := prd.PRDPath("", "main"); fileExists(mainPath) {
		return mainPath, true
	}
	if available := cli.FindAvailablePRD(""); available != "" {
		return available, true
	}
	return runFirstTimeSetup(provider)
}

// fileExists reports whether path exists (as any kind of file).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// runFirstTimeSetup drives the first-run TUI (config choices + PRD name),
// persists the chosen config, and creates the initial PRD. It returns the new
// PRD's path, or ok=false when the user cancelled setup.
func runFirstTimeSetup(provider loop.Provider) (string, bool) {
	cwd, _ := os.Getwd()
	showGitignore := git.IsGitRepo(cwd) && !git.IsChiefIgnored(cwd)

	result, err := tui.RunFirstTimeSetup(cwd, showGitignore)
	if err != nil {
		fatal(err)
	}
	if result.Cancelled {
		return "", false
	}

	// Save config from setup.
	cfg := config.Default()
	cfg.OnComplete.Push = result.PushOnComplete
	cfg.OnComplete.CreatePR = result.CreatePROnComplete
	if err := config.Save(cwd, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save config: %v\n", err)
	}

	if err := cmd.RunNew(cmd.NewOptions{Name: result.PRDName, Provider: provider}); err != nil {
		fatal(err)
	}
	return prd.PRDPath("", result.PRDName), true
}

// maybeMigrate migrates a legacy prd.json into prd.md when one sits alongside the
// PRD, printing progress. It is a no-op when there is nothing to migrate.
func maybeMigrate(prdDir string) {
	jsonPath := filepath.Join(prdDir, "prd.json")
	if !fileExists(jsonPath) {
		return
	}
	fmt.Println("Migrating status from prd.json to prd.md...")
	if err := prd.MigrateFromJSON(prdDir); err != nil {
		fmt.Printf("Warning: migration failed: %v\n", err)
	} else {
		fmt.Println("Migration complete (prd.json renamed to prd.json.bak).")
	}
}

// reportAppInitError prints a helpful message for a failed TUI init and exits.
// A missing PRD file gets a "not found" hint listing available PRDs; any other
// error is printed verbatim.
func reportAppInitError(prdPath string, err error) {
	if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file") {
		fmt.Printf("PRD not found: %s\n", prdPath)
		fmt.Println()
		if available := cli.ListAvailablePRDs(""); len(available) > 0 {
			fmt.Println("Available PRDs:")
			for _, name := range available {
				fmt.Printf("  chief %s\n", name)
			}
			fmt.Println()
		}
		fmt.Println("Or create a new one:")
		fmt.Println("  chief new               # Create default PRD")
		fmt.Println("  chief new <name>        # Create named PRD")
	} else {
		fmt.Printf("Error: %v\n", err)
	}
	os.Exit(1)
}

// handlePostExit runs a PRD init/edit requested from within the TUI and points
// opts at the resulting PRD so the caller can relaunch on it. It returns true
// when such a restart is pending, false when the session should end.
func handlePostExit(model tea.Model, opts *cli.Options, provider loop.Provider) bool {
	finalApp, ok := model.(tui.App)
	if !ok {
		return false
	}
	switch finalApp.PostExitAction {
	case tui.PostExitInit:
		if err := cmd.RunNew(cmd.NewOptions{Name: finalApp.PostExitPRD, Provider: provider}); err != nil {
			fatal(err)
		}
	case tui.PostExitEdit:
		if err := cmd.RunEdit(cmd.EditOptions{Name: finalApp.PostExitPRD, Provider: provider}); err != nil {
			fatal(err)
		}
	default:
		return false
	}
	opts.PRDPath = prd.PRDPath("", finalApp.PostExitPRD)
	return true
}
