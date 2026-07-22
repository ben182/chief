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

func runNew() {
	opts := cmd.NewOptions{}

	// Parse arguments: chief new [name] [context...] [--agent X] [--agent-path X]
	flagAgent, flagPath, flagModel, positional, err := cli.AgentFlags(os.Args, 2)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
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
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runEdit() {
	opts := cmd.EditOptions{}

	// Parse arguments: chief edit [name] [--agent X] [--agent-path X]
	flagAgent, flagPath, flagModel, remaining, err := cli.AgentFlags(os.Args, 2)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
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
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runStatus() {
	opts := cmd.StatusOptions{}

	// Parse arguments: chief status [name]
	if len(os.Args) > 2 && !strings.HasPrefix(os.Args[2], "-") {
		opts.Name = os.Args[2]
	}

	if err := cmd.RunStatus(opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runList() {
	opts := cmd.ListOptions{}

	if err := cmd.RunList(opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// resolveProvider loads config and resolves the agent provider, exiting on error.
func resolveProvider(flagAgent, flagPath, flagModel string) loop.Provider {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	cfg, err := config.Load(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load .chief/config.yaml: %v\n", err)
		os.Exit(1)
	}
	provider, err := agent.Resolve(flagAgent, flagPath, cfg, flagModel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := agent.CheckInstalled(provider); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
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
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if cancelled {
		return false
	}
	claude.SetModel(model)
	return true
}

func runTUIWithOptions(opts *cli.Options) {
	provider := resolveProvider(opts.Agent, opts.AgentPath, opts.Model)

	prdPath := opts.PRDPath

	// If no PRD specified, try to find one
	if prdPath == "" {
		// Try "default" first (falls back to "main" for older setups)
		defaultPath := prd.PRDPath("", "default")
		mainPath := prd.PRDPath("", "main")
		if _, err := os.Stat(defaultPath); err == nil {
			prdPath = defaultPath
		} else if _, err := os.Stat(mainPath); err == nil {
			prdPath = mainPath
		} else {
			// Look for any available PRD
			prdPath = cli.FindAvailablePRD("")
		}

		// If still no PRD found, run first-time setup
		if prdPath == "" {
			cwd, _ := os.Getwd()
			showGitignore := git.IsGitRepo(cwd) && !git.IsChiefIgnored(cwd)

			// Run the first-time setup TUI
			result, err := tui.RunFirstTimeSetup(cwd, showGitignore)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			if result.Cancelled {
				return
			}

			// Save config from setup
			cfg := config.Default()
			cfg.OnComplete.Push = result.PushOnComplete
			cfg.OnComplete.CreatePR = result.CreatePROnComplete
			if err := config.Save(cwd, cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to save config: %v\n", err)
			}

			// Create the PRD
			newOpts := cmd.NewOptions{
				Name:     result.PRDName,
				Provider: provider,
			}
			if err := cmd.RunNew(newOpts); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			// Restart TUI with the new PRD
			opts.PRDPath = prd.PRDPath("", result.PRDName)
			runTUIWithOptions(opts)
			return
		}
	}

	prdDir := filepath.Dir(prdPath)

	// Auto-migrate: if prd.json exists alongside prd.md, migrate status
	jsonPath := filepath.Join(prdDir, "prd.json")
	if _, err := os.Stat(jsonPath); err == nil {
		fmt.Println("Migrating status from prd.json to prd.md...")
		if err := prd.MigrateFromJSON(prdDir); err != nil {
			fmt.Printf("Warning: migration failed: %v\n", err)
		} else {
			fmt.Println("Migration complete (prd.json renamed to prd.json.bak).")
		}
	}

	app, err := tui.NewAppWithOptions(prdPath, opts.MaxIterations, provider)
	if err != nil {
		// Check if this is a missing PRD file error
		if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file") {
			fmt.Printf("PRD not found: %s\n", prdPath)
			fmt.Println()
			// Show available PRDs if any exist
			available := cli.ListAvailablePRDs("")
			if len(available) > 0 {
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

	// Set verbose mode if requested
	if opts.Verbose {
		app.SetVerbose(true)
	}

	// Disable retry if requested
	if opts.NoRetry {
		app.DisableRetry()
	}

	// Auto-start the loop if requested (chief start)
	if opts.AutoStart {
		app.SetAutoStart(true)
	}

	p := tea.NewProgram(app, tea.WithAltScreen())
	model, err := p.Run()
	if err != nil {
		fmt.Printf("Error running program: %v\n", err)
		os.Exit(1)
	}

	// Check for post-exit actions
	if finalApp, ok := model.(tui.App); ok {
		switch finalApp.PostExitAction {
		case tui.PostExitInit:
			// Run new command then restart TUI
			newOpts := cmd.NewOptions{
				Name:     finalApp.PostExitPRD,
				Provider: provider,
			}
			if err := cmd.RunNew(newOpts); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			// Restart TUI with the new PRD
			opts.PRDPath = prd.PRDPath("", finalApp.PostExitPRD)
			runTUIWithOptions(opts)

		case tui.PostExitEdit:
			// Run edit command then restart TUI
			editOpts := cmd.EditOptions{
				Name:     finalApp.PostExitPRD,
				Provider: provider,
			}
			if err := cmd.RunEdit(editOpts); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			// Restart TUI with the edited PRD
			opts.PRDPath = prd.PRDPath("", finalApp.PostExitPRD)
			runTUIWithOptions(opts)
		}
	}
}
