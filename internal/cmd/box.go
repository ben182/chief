package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ben182/chief/internal/box"
	"github.com/ben182/chief/internal/cli"
	"github.com/ben182/chief/internal/config"
)

// BoxUsage is what `chief box` prints when it is called without a command, and
// what the help refers to for the detail.
const BoxUsage = `Usage: chief box <command> [options]

Run a PRD on a throwaway cloud instance, so a run that takes hours does not take
your machine with it.

Commands:
  up <prd>      Create a box, put the project on it, and start the run
  run <prd>     The same, then follow the log until you stop watching
  logs          Follow the running box's log
  status        What the box is doing, and how long it has been billing
  ssh           A shell on the box, in the project directory
  down          Destroy the box — this is what stops the billing

Options for up/run:
  --worktree            Run in the PRD's own worktree on the box
  --verbose             Put the agent's narration in the box's log
  -n, --max-iterations  Cap the run's iterations
  --type <name>         Hetzner server type (default ` + box.DefaultType + `)
  --location <name>     Hetzner location (default ` + box.DefaultLocation + `)
  --image <name>        Hetzner image (default ` + box.DefaultImage + `)
  --file <path>         An untracked file the run needs, repeatable (default .env)
  --package <name>      An apt package to install on the box, repeatable

Options for down:
  --force               Destroy without asking about unpushed commits

Credentials are worked out rather than configured: the GitHub token comes from
'gh auth token', and the Claude token from 'claude setup-token' the first time
one is needed, saved afterwards. The Hetzner API token is the one thing to put
in place by hand — see 'chief box up' for where.`

// BoxOptions are the parsed arguments of a box command.
type BoxOptions struct {
	Command string
	PRD     string

	Worktree      bool
	Verbose       bool
	Force         bool
	MaxIterations int

	Type, Location, Image string
	Files                 []string
	Packages              []string
}

// ParseBoxArgs parses the arguments after `chief box`.
func ParseBoxArgs(args []string) (BoxOptions, error) {
	var o BoxOptions
	if len(args) == 0 {
		return o, fmt.Errorf("no command")
	}
	o.Command = args[0]

	value := func(i *int, flag string) (string, error) {
		if *i+1 >= len(args) {
			return "", fmt.Errorf("%s requires a value", flag)
		}
		*i++
		return args[*i], nil
	}

	for i := 1; i < len(args); i++ {
		arg := args[i]
		var err error
		switch {
		case arg == "--worktree":
			o.Worktree = true
		case arg == "--verbose":
			o.Verbose = true
		case arg == "--force", arg == "-f":
			o.Force = true
		case arg == "--type":
			o.Type, err = value(&i, arg)
		case arg == "--location":
			o.Location, err = value(&i, arg)
		case arg == "--image":
			o.Image, err = value(&i, arg)
		case arg == "--file":
			var v string
			if v, err = value(&i, arg); err == nil {
				o.Files = append(o.Files, v)
			}
		case arg == "--package":
			var v string
			if v, err = value(&i, arg); err == nil {
				o.Packages = append(o.Packages, v)
			}
		case arg == "-n", arg == "--max-iterations":
			var v string
			if v, err = value(&i, arg); err == nil {
				o.MaxIterations, err = parsePositive(arg, v)
			}
		case strings.HasPrefix(arg, "--"):
			// The value-carrying flags also accept --flag=value.
			name, v, found := strings.Cut(arg, "=")
			if !found {
				return o, fmt.Errorf("unknown flag: %s", arg)
			}
			switch name {
			case "--type":
				o.Type = v
			case "--location":
				o.Location = v
			case "--image":
				o.Image = v
			case "--file":
				o.Files = append(o.Files, v)
			case "--package":
				o.Packages = append(o.Packages, v)
			case "--max-iterations":
				o.MaxIterations, err = parsePositive(name, v)
			default:
				return o, fmt.Errorf("unknown flag: %s", name)
			}
		case strings.HasPrefix(arg, "-"):
			return o, fmt.Errorf("unknown flag: %s", arg)
		default:
			o.PRD = arg
		}
		if err != nil {
			return o, err
		}
	}
	return o, nil
}

func parsePositive(flag, v string) (int, error) {
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s needs a positive number, got %q", flag, v)
	}
	return n, nil
}

// RunBox executes a box command and returns the process exit code.
func RunBox(ctx context.Context, opts BoxOptions) error {
	baseDir, err := os.Getwd()
	if err != nil {
		return err
	}

	switch opts.Command {
	case "up", "run":
		return runBoxUp(ctx, baseDir, opts)
	case "logs":
		return box.Logs(ctx, baseDir, os.Stdout)
	case "status":
		return box.Status(ctx, baseDir, os.Stdout)
	case "ssh":
		return box.SSH(ctx, baseDir)
	case "down":
		return box.Down(ctx, box.DownOptions{
			BaseDir: baseDir,
			Force:   opts.Force,
			Confirm: confirm,
			Out:     os.Stderr,
		})
	default:
		return fmt.Errorf("unknown command %q\n\n%s", opts.Command, BoxUsage)
	}
}

// runBoxUp creates the box and starts the run, following the log afterwards
// when the command was `run`.
func runBoxUp(ctx context.Context, baseDir string, opts BoxOptions) error {
	prdName := opts.PRD
	if prdName == "" {
		// The branch a PRD's work happens on names it, which is the same
		// inference `chief` itself makes when started bare.
		prdName = PRDNameFromBranch("")
	}
	if prdName == "" {
		if available := cli.ListAvailablePRDs(baseDir); len(available) == 1 {
			prdName = available[0]
		}
	}
	if prdName == "" {
		return fmt.Errorf("which PRD? Name one: chief box %s <prd>", opts.Command)
	}

	cfg, err := config.Load(baseDir)
	if err != nil {
		return fmt.Errorf("failed to load .chief/config.yaml: %w", err)
	}

	// A project whose runs want a worktree says so once in its config; the flag
	// is for the run that wants one anyway.
	worktree := opts.Worktree || cfg.Worktree.Setup != ""

	up := box.UpOptions{
		PRD:           prdName,
		BaseDir:       baseDir,
		Type:          opts.Type,
		Image:         opts.Image,
		Location:      opts.Location,
		Worktree:      worktree,
		MaxIterations: opts.MaxIterations,
		Verbose:       opts.Verbose,
		ExtraFiles:    opts.Files,
		ExtraPackages: opts.Packages,
		Out:           os.Stderr,
	}

	// Everything that can be checked for free is checked first. Resolving the
	// secrets below can open a browser, and nobody should be sent through a
	// login to be told afterwards that their PRD does not exist.
	if err := box.Preflight(up); err != nil {
		return err
	}

	interactive := isTerminal(os.Stdin)
	up.Secrets, err = box.ResolveSecrets(ctx, interactive, os.Stdout, os.Stderr)
	if err != nil {
		return err
	}

	state, err := box.Up(ctx, up)
	if err != nil {
		return err
	}

	if opts.Command == "run" {
		fmt.Fprintf(os.Stderr, "\n==> Following the log. Ctrl-C stops watching; the run keeps going.\n\n")
		_ = box.Logs(ctx, baseDir, os.Stdout)
		fmt.Fprintln(os.Stderr)
		return box.Status(context.WithoutCancel(ctx), baseDir, os.Stderr)
	}
	_ = state
	return nil
}

// confirm asks a yes/no question on the terminal. With no terminal there is
// nobody to ask, and the answer is no.
func confirm(prompt string) bool {
	if !isTerminal(os.Stdin) {
		return false
	}
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// isTerminal reports whether f is a terminal, which decides whether a question
// can be asked at all.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
