package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ben182/chief/internal/box"
	"github.com/ben182/chief/internal/cli"
	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/notify"
	"github.com/ben182/chief/internal/tui"
)

// BoxUsage is what `chief box` prints when it is called without a command, and
// what the help refers to for the detail. A var rather than a const because it
// quotes chief's own defaults, and those are numbers as well as names.
var BoxUsage = `Usage: chief box <command> [options]

Run a PRD on a throwaway cloud instance, so a run that takes hours does not take
your machine with it.

Commands:
  token         Set up the credentials a box needs (asks only for what is missing)
  config        Pick the location and the machine size, once, for this project
  up <prd>      Create a box, put the project on it, and start the run
  run <prd>     The same, then follow the log and say when the run ends
  retry         Put the project on the box that is already there, and start it
  logs          Follow the running box's log
  status        What the box is doing, and how long it has been billing
  list          Every box in your Hetzner project, and what each has cost
  ssh [cmd]     A shell on the box, or run one command there
  down          Destroy the box — this is what stops the billing

Options for up/run:
  --keep                Leave the box standing when the run ends. Without this a
                        box destroys itself once its work is on origin. From
                        ` + fmt.Sprint(box.DefaultMaxHours) + `h after boot it also stops a run that has
                        stopped committing; one that still commits goes on,
                        up to four times that
  --max-hours N         Change when that check starts
  --at HH:MM            Set the box up now, start the run at this time (next
                        time the clock reads it, in your time zone). The outside
                        limit then counts from the start. Also on retry
  --down-when-done      Destroy the box as soon as the run ends (run only)
  --worktree            Run in the PRD's own worktree on the box
  --verbose             Put the agent's narration in the box's log
  -n, --max-iterations  Cap the run's iterations
  --type <name>         Hetzner server type (default: the cheapest the location
                        sells, which is what a run that waits on an API needs)
  --location <name>     Hetzner location (default ` + box.DefaultLocation + `)
  --image <name>        Hetzner image (default ` + box.DefaultImage + `)
                        All three default to what 'chief box config' stored
  --php <series>        PHP to install, e.g. 8.3 (default: what this machine runs)
  --node <major>        Node to install, e.g. 22 (default: what this machine runs)
  --file <path>         An untracked file the run needs, repeatable (default .env)
  --package <name>      An apt package to install on the box, repeatable

What the box is built with is read from the project: PHP and its extensions from
composer.json and composer.lock, the database from .env, the JavaScript package
manager from the lockfile, browser libraries when the tests drive one. 'up'
prints what it found before creating anything.

Options for down:
  --force               Destroy without asking about unpushed commits
  --all                 Destroy every box in your Hetzner project, not just this
                        project's — this is how a forgotten box is stopped
  --name <name>         Destroy one box by name, whichever checkout created it

Credentials are worked out rather than configured: the GitHub token comes from
'gh auth token', and the Claude token from 'claude setup-token' the first time
one is needed, saved afterwards. The Hetzner API token is the one thing nothing
can mint — run 'chief box token' once to store it.`

// BoxOptions are the parsed arguments of a box command.
type BoxOptions struct {
	Command string
	// Command2 is the command `chief box ssh` runs on the box, empty for a shell.
	Command2 string
	PRD      string

	Worktree      bool
	Verbose       bool
	Force         bool
	All           bool
	Name          string
	DownWhenDone  bool
	Keep          bool
	MaxHours      int
	MaxIterations int
	// At is when the run should start, already resolved to the next time the
	// clock reads what was typed. Zero starts it once the box is ready.
	At time.Time

	Type, Location, Image string
	PHP, Node             string
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

	// `ssh` takes the rest of the line verbatim: it is a command for the box, and
	// parsing it here would swallow its flags as chief's own.
	if o.Command == "ssh" {
		o.Command2 = strings.Join(args[1:], " ")
		return o, nil
	}

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
		case arg == "--down-when-done":
			o.DownWhenDone = true
		case arg == "--keep":
			o.Keep = true
		case arg == "--max-hours":
			var v string
			if v, err = value(&i, arg); err == nil {
				o.MaxHours, err = parsePositive(arg, v)
			}
		case arg == "--at":
			var v string
			if v, err = value(&i, arg); err == nil {
				o.At, err = box.NextAt(time.Now(), v)
			}
		case arg == "--force", arg == "-f":
			o.Force = true
		case arg == "--all":
			o.All = true
		case arg == "--name":
			o.Name, err = value(&i, arg)
		case arg == "--type":
			o.Type, err = value(&i, arg)
		case arg == "--location":
			o.Location, err = value(&i, arg)
		case arg == "--image":
			o.Image, err = value(&i, arg)
		case arg == "--php":
			o.PHP, err = value(&i, arg)
		case arg == "--node":
			o.Node, err = value(&i, arg)
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
			case "--name":
				o.Name = v
			case "--type":
				o.Type = v
			case "--location":
				o.Location = v
			case "--image":
				o.Image = v
			case "--php":
				o.PHP = v
			case "--node":
				o.Node = v
			case "--file":
				o.Files = append(o.Files, v)
			case "--package":
				o.Packages = append(o.Packages, v)
			case "--max-iterations":
				o.MaxIterations, err = parsePositive(name, v)
			case "--max-hours":
				o.MaxHours, err = parsePositive(name, v)
			case "--at":
				o.At, err = box.NextAt(time.Now(), v)
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
	case "token", "login":
		return box.Login(ctx, os.Stdin, os.Stderr)
	case "config":
		return runBoxConfig(ctx, baseDir)
	case "up", "run":
		return runBoxUp(ctx, baseDir, opts)
	case "retry":
		return runBoxRetry(ctx, baseDir, opts)
	case "logs":
		return box.Logs(ctx, baseDir, os.Stdout)
	case "status":
		return box.Status(ctx, baseDir, os.Stdout)
	case "list", "ls":
		return box.List(ctx, baseDir, os.Stdout)
	case "ssh":
		return box.SSH(ctx, baseDir, opts.Command2, os.Stdout)
	case "down":
		return box.Down(ctx, box.DownOptions{
			BaseDir: baseDir,
			Force:   opts.Force,
			All:     opts.All,
			Name:    opts.Name,
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

	// A project that has never been asked where its boxes should run gets asked
	// now, if there is somebody to ask. The alternative is a machine created in
	// a country nobody chose, holding the project's .env — a default worth
	// interrupting for exactly once.
	//
	// Only at a terminal, and only when no flag already answers it: `box up` is
	// run from scripts and background shells, where a prompt is a hang.
	if isTerminal(os.Stdin) && cfg.Box.Location == "" && cfg.Box.Type == "" &&
		opts.Location == "" && opts.Type == "" {
		if err := runBoxConfig(ctx, baseDir); err != nil {
			// Not fatal. The catalog needs a Hetzner token, and if there is none
			// the next step says so with the command that fixes it.
			fmt.Fprintf(os.Stderr, "==> Skipping the setup screen (%v)\n", err)
		} else if reloaded, err := config.Load(baseDir); err == nil {
			cfg = reloaded
		}
	}

	up := boxUpOptions(baseDir, prdName, cfg, opts)

	// Everything that can be checked for free is checked first. Resolving the
	// secrets below can open a browser, and nobody should be sent through a
	// login to be told afterwards that their PRD does not exist.
	if err := box.Preflight(ctx, up); err != nil {
		return err
	}

	// Never interactive, even at a terminal. Creating a box is something scripts
	// and background shells do, and a browser login started there waits forever
	// on a prompt nobody will answer — a hang, which is worse than a failure
	// because it looks like work. The login lives in 'chief box token' instead,
	// which is done once, deliberately, by a person.
	up.Secrets, err = box.ResolveSecrets(ctx, false, os.Stdout, os.Stderr)
	if err != nil {
		return fmt.Errorf("%w\n  Run 'chief box token' once to set this up", err)
	}

	state, err := box.Up(ctx, up)
	if err != nil {
		return err
	}

	if opts.Command == "run" {
		return followRun(ctx, baseDir, opts)
	}
	_ = state
	return nil
}

// boxUpOptions assembles what the box is created and built with: a flag beats
// the project's config, the config beats chief's default, and what neither
// answers is read out of the project itself.
//
// The profile it printed is printed rather than merely computed, because a
// wrong guess about which PHP or which database a project wants is cheapest to
// catch while it is still a line on a screen instead of a provisioned machine.
func boxUpOptions(baseDir, prdName string, cfg *config.Config, opts BoxOptions) box.UpOptions {
	// A project whose runs want a worktree says so once in its config; the flag
	// is for the run that wants one anyway.
	worktree := opts.Worktree || cfg.Worktree.Setup != ""

	profile := box.Discover(baseDir, box.DiscoverOptions{
		PHP:  firstNonEmpty(opts.PHP, cfg.Box.PHP),
		Node: firstNonEmpty(opts.Node, cfg.Box.Node),
	})
	fmt.Fprintln(os.Stderr, "==> Read from the project")
	for _, line := range profile.Summary() {
		fmt.Fprintf(os.Stderr, "    %s\n", line)
	}

	return box.UpOptions{
		PRD:           prdName,
		BaseDir:       baseDir,
		Type:          firstNonEmpty(opts.Type, cfg.Box.Type),
		Image:         firstNonEmpty(opts.Image, cfg.Box.Image),
		Location:      firstNonEmpty(opts.Location, cfg.Box.Location),
		Worktree:      worktree,
		Keep:          opts.Keep || cfg.Box.Keep,
		MaxHours:      firstPositive(opts.MaxHours, cfg.Box.MaxHours),
		StartAt:       opts.At,
		MaxIterations: opts.MaxIterations,
		Verbose:       opts.Verbose,
		ExtraFiles:    firstNonEmptyList(opts.Files, cfg.Box.Files),
		Profile:       profile,
		ExtraPackages: append(append([]string{}, cfg.Box.Packages...), opts.Packages...),
		Out:           os.Stderr,
	}
}

// runBoxRetry puts the project on the box this checkout already has and starts
// the run there.
//
// The PRD comes from the box's own record rather than from the command line:
// the machine was created for one run, its systemd unit is named after it, and
// retrying with a different PRD would be a different box.
func runBoxRetry(ctx context.Context, baseDir string, opts BoxOptions) error {
	cfg, err := config.Load(baseDir)
	if err != nil {
		return fmt.Errorf("failed to load .chief/config.yaml: %w", err)
	}

	state, ok := box.LoadState(baseDir)
	if !ok {
		return fmt.Errorf("no box for this project to retry on — start one with 'chief box up <prd>'")
	}

	up := boxUpOptions(baseDir, state.PRD, cfg, opts)
	up.Secrets, err = box.ResolveSecrets(ctx, false, os.Stdout, os.Stderr)
	if err != nil {
		return fmt.Errorf("%w\n  Run 'chief box token' once to set this up", err)
	}

	_, err = box.Retry(ctx, up)
	return err
}

// followRun watches a run that was just started, and destroys the box
// afterwards when that was asked for.
//
// Both forms wait for the run rather than for the reader. Following the log
// until the reader stops looking was the older behaviour of the plain `run`,
// and it has the flaw that a finished run looks exactly like a quiet one:
// journalctl keeps following a unit that has ended, so the screen simply stops
// moving. Waiting for the end means there is a moment to report — and something
// to report it with.
func followRun(ctx context.Context, baseDir string, opts BoxOptions) error {
	if opts.DownWhenDone {
		fmt.Fprintf(os.Stderr, "\n==> Following the log. The box is destroyed when the run ends.\n")
		fmt.Fprintf(os.Stderr, "    Ctrl-C stops watching — and then the box stays up, billing.\n\n")
	} else {
		fmt.Fprintf(os.Stderr, "\n==> Following the log until the run ends. Ctrl-C stops watching;\n")
		fmt.Fprintf(os.Stderr, "    the run keeps going, and so does the billing.\n\n")
	}

	outcome, err := box.Watch(ctx, baseDir, os.Stdout)
	if err != nil {
		// Interrupted rather than finished. The box is still there on purpose:
		// nobody asked for a machine to be destroyed because a terminal was
		// closed, and the run on it is still going. Ctrl-C is how somebody says
		// "I have seen enough", which is not a failure to report as one.
		fmt.Fprintf(os.Stderr, "\n==> Stopped watching. The box is still up — 'chief box down' ends it.\n")
		return nil //nolint:nilerr // an interrupted watch is a choice, not an error
	}

	notifyRunEnded(baseDir, outcome)

	if !opts.DownWhenDone {
		fmt.Fprintln(os.Stderr)
		return box.Status(context.WithoutCancel(ctx), baseDir, os.Stderr)
	}

	fmt.Fprintln(os.Stderr)
	// Not forced: a run can end having committed work that was never pushed, and
	// that work exists nowhere else. Down asks about it, and the answer at a
	// terminal is a person's.
	return box.Down(context.WithoutCancel(ctx), box.DownOptions{
		BaseDir: baseDir,
		Confirm: confirm,
		Out:     os.Stderr,
	})
}

// notifyRunEnded pings this machine when a box run it was watching has ended.
//
// The notification chief already sends fires on the machine the run happens on
// — which, for a box, is the box: a server with no display, where notify-send
// reaches nobody. This is the same ping, sent from the side that has a screen.
// It only works while this command is still watching, which is the point: a
// laptop that is awake and watching is exactly the case a banner is for, and a
// laptop that is asleep is the case the pull request in the morning is for.
//
// Governed by onComplete.notify, the same switch as every other run's, because
// somebody who turned notifications off meant all of them.
func notifyRunEnded(baseDir string, outcome box.Outcome) {
	cfg, err := config.Load(baseDir)
	if err != nil || !cfg.OnComplete.Notify {
		return
	}
	name := ""
	if s, ok := box.LoadState(baseDir); ok {
		name = s.PRD
	}
	title := "chief box"
	if name != "" {
		title += ": " + name
	}
	notify.Send(title, outcome.Describe())
}

// runBoxConfig asks where this project's boxes should run and on what, and
// writes the answer into .chief/config.yaml.
//
// The list comes from the API rather than from a table in this repository. A
// hard-coded one is wrong the week Hetzner retires a generation or opens a
// location, and the failure it causes — "unsupported location" on a machine
// somebody has been creating for months — reads like a bug in chief.
func runBoxConfig(ctx context.Context, baseDir string) error {
	cfg, err := config.Load(baseDir)
	if err != nil {
		return fmt.Errorf("failed to load .chief/config.yaml: %w", err)
	}

	fmt.Fprintln(os.Stderr, "==> Asking Hetzner what it offers right now")
	catalog, err := box.FetchCatalog(ctx)
	if err != nil {
		return err
	}

	location, serverType, cancelled, err := tui.RunBoxSetup(catalog, cfg.Box.Location, cfg.Box.Type)
	if err != nil {
		return err
	}
	if cancelled {
		return fmt.Errorf("nothing chosen")
	}

	cfg.Box.Location, cfg.Box.Type = location, serverType
	if err := config.Save(baseDir, cfg); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "==> %s in %s, saved to .chief/config.yaml\n", serverType, location)
	return nil
}

// firstPositive is firstNonEmpty for the settings that are counts.
func firstPositive(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

// firstNonEmpty returns the first value that was actually set, which is how a
// flag beats the project's config and the config beats chief's default.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// firstNonEmptyList is firstNonEmpty for the settings that are lists. They
// replace rather than merge: a run that names the files it needs means those
// files, not those plus whatever the config remembers.
func firstNonEmptyList(values ...[]string) []string {
	for _, v := range values {
		if len(v) > 0 {
			return v
		}
	}
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
