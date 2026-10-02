package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ben182/chief/internal/box"
	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/tui"
)

// SetupUsage is what `chief setup --help` prints.
const SetupUsage = `Usage: chief setup

Walk through the settings a project needs once and that are easy to forget:
the worktree setup command, push and pull request at the end of a run, which
MCP servers and skills an unattended agent gets, review and consolidation, and
optionally the cloud box. Every question opens on what the project uses now (or
what chief detected), so Enter keeps it. Nothing is written until the end.

Run it again any time; the settings screen in the TUI (,) has everything else.`

// errSetupAborted is what an input that ends mid-question turns into: nothing
// has been written, and the caller should say so rather than save half answers.
var errSetupAborted = errors.New("setup aborted, nothing saved")

// setupDeps are the parts of `chief setup` that reach beyond the terminal, so a
// test can walk the questions without gh, Hetzner or a TUI.
type setupDeps struct {
	// checkGH reports whether pull requests can be opened from this machine.
	checkGH func() (installed, authenticated bool, err error)
	// hasBoxToken reports whether a Hetzner token is stored anywhere.
	hasBoxToken func() bool
	// boxLogin is `chief box token`.
	boxLogin func() error
	// pickBox shows the location and server type picker and returns the
	// choice, cancelled when nothing was picked.
	pickBox func(location, serverType string) (string, string, bool, error)
}

// RunSetup is `chief setup`: it asks, then writes .chief/config.yaml.
func RunSetup(ctx context.Context, baseDir string) error {
	deps := setupDeps{
		checkGH:     git.CheckGHCLI,
		hasBoxToken: box.HasHetznerToken,
		boxLogin:    func() error { return box.Login(ctx, os.Stdin, os.Stdout) },
		pickBox: func(location, serverType string) (string, string, bool, error) {
			fmt.Println("==> Asking Hetzner what it offers right now")
			catalog, err := box.FetchCatalog(ctx)
			if err != nil {
				return "", "", true, err
			}
			return tui.RunBoxSetup(catalog, location, serverType)
		},
	}
	return runSetup(baseDir, os.Stdin, os.Stdout, deps)
}

func runSetup(baseDir string, in io.Reader, out io.Writer, deps setupDeps) error {
	cfg, err := config.Load(baseDir)
	if err != nil {
		return fmt.Errorf("failed to load .chief/config.yaml: %w", err)
	}
	// A project with no config file has nothing to keep, so its questions open
	// on recommendations instead of on zero values nobody chose.
	fresh := !config.Exists(baseDir)

	p := &prompter{in: in, out: out}
	p.say("Setting up chief for this project. Enter keeps the value in [brackets],")
	p.say("'-' clears it. Nothing is written until the end.")

	if err := askWorktree(p, baseDir, cfg, fresh); err != nil {
		return err
	}
	if err := askOnComplete(p, cfg, fresh, deps); err != nil {
		return err
	}
	if err := askAgent(p, baseDir, cfg, fresh); err != nil {
		return err
	}
	if err := askReview(p, cfg); err != nil {
		return err
	}
	if err := askBox(p, baseDir, cfg, deps); err != nil {
		return err
	}

	p.section("Summary")
	for _, line := range setupSummary(cfg) {
		p.say("  " + line)
	}
	p.say("")
	save, err := p.yesNo("Save to .chief/config.yaml?", true)
	if err != nil {
		return err
	}
	if !save {
		p.say("Nothing saved.")
		return nil
	}
	if err := config.Save(baseDir, cfg); err != nil {
		return err
	}
	p.say("Saved to .chief/config.yaml.")
	return nil
}

// setupSummary is one line per answer, in the order they were asked.
func setupSummary(cfg *config.Config) []string {
	orNone := func(s string) string {
		if s == "" {
			return "(none)"
		}
		return s
	}
	onOff := func(on bool, skill string) string {
		if !on {
			return "off"
		}
		if skill != "" {
			return "on, " + skill
		}
		return "on"
	}
	yesNo := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}

	mcp := cfg.Agent.MCP
	if mcp == "" {
		mcp = "inherit"
	}
	skills := "loaded"
	if cfg.Agent.SkillsDisabled() {
		skills = "not loaded"
	}
	boxLine := "not set up"
	if cfg.Box.Location != "" || cfg.Box.Type != "" || len(cfg.Box.Files) > 0 {
		files := strings.Join(cfg.Box.Files, ", ")
		if files == "" {
			files = ".env"
		}
		boxLine = fmt.Sprintf("%s in %s, copies %s", orNone(cfg.Box.Type), orNone(cfg.Box.Location), files)
	}

	return []string{
		"Worktree setup  " + orNone(cfg.Worktree.Setup),
		"Push            " + yesNo(cfg.OnComplete.Push),
		"Pull request    " + yesNo(cfg.OnComplete.CreatePR),
		"MCP servers     " + mcp,
		"Skills          " + skills,
		"Review          " + onOff(cfg.Review.Active(), cfg.Review.Skill),
		"Consolidation   " + onOff(cfg.Consolidate.Active(), cfg.Consolidate.Skill),
		"Box             " + boxLine,
	}
}

func askWorktree(p *prompter, baseDir string, cfg *config.Config, fresh bool) error {
	p.section("Worktrees")
	p.say("A command that runs in every new worktree before the agent starts,")
	p.say("typically installing dependencies.")
	def := cfg.Worktree.Setup
	if def == "" && fresh {
		def = detectWorktreeSetup(baseDir)
	}
	setup, err := p.text("Setup command", def)
	if err != nil {
		return err
	}
	cfg.Worktree.Setup = setup
	return nil
}

func askOnComplete(p *prompter, cfg *config.Config, fresh bool, deps setupDeps) error {
	p.section("When a run finishes")
	push, err := p.yesNo("Push the branch?", cfg.OnComplete.Push || fresh)
	if err != nil {
		return err
	}
	cfg.OnComplete.Push = push
	if !push {
		// A pull request needs the branch on the remote.
		cfg.OnComplete.CreatePR = false
		return nil
	}

	pr, err := p.yesNo("Open a pull request?", cfg.OnComplete.CreatePR || fresh)
	if err != nil {
		return err
	}
	if pr {
		if problem := ghProblem(deps.checkGH); problem != "" {
			p.say("  ! " + problem)
			pr, err = p.yesNo("Keep pull requests on anyway?", false)
			if err != nil {
				return err
			}
		}
	}
	cfg.OnComplete.CreatePR = pr
	return nil
}

// ghProblem says why pull requests cannot be opened from here, or "" when they
// can.
func ghProblem(check func() (bool, bool, error)) string {
	installed, authenticated, err := check()
	switch {
	case err != nil:
		return fmt.Sprintf("could not check the GitHub CLI: %v", err)
	case !installed:
		return "the GitHub CLI (gh) is not installed: https://cli.github.com"
	case !authenticated:
		return "the GitHub CLI (gh) is not logged in: run 'gh auth login'"
	}
	return ""
}

// MCP choices, in the order they are offered.
const (
	mcpInherit = iota
	mcpProject
	mcpNone
	mcpFile
)

// projectMCPFile is the repository's own MCP config, the one most projects
// that need a server for their runs already have.
const projectMCPFile = ".mcp.json"

func askAgent(p *prompter, baseDir string, cfg *config.Config, fresh bool) error {
	p.section("Agent")
	p.say("Which MCP servers an unattended iteration starts with. Every server it")
	p.say("has is reachable without asking, since runs skip permission prompts.")

	hasProjectMCP := fileExistsIn(baseDir, projectMCPFile)
	options := []string{
		"inherit: everything this machine has (.mcp.json, your own, account connectors)",
		"project: only the servers in " + projectMCPFile,
		"none: no MCP servers at all",
		"file: a JSON file in the shape of .mcp.json",
	}
	if !hasProjectMCP {
		options[mcpProject] += " (there is none yet)"
	}

	strict, path := cfg.Agent.MCPSetting()
	def := mcpInherit
	switch {
	case strict && path == "":
		def = mcpNone
	case strict && filepath.Clean(path) == projectMCPFile:
		def = mcpProject
	case strict:
		def = mcpFile
	case fresh && hasProjectMCP:
		def = mcpProject
	}

	choice, err := p.choose("MCP servers", options, def)
	if err != nil {
		return err
	}
	switch choice {
	case mcpInherit:
		cfg.Agent.MCP = ""
	case mcpProject:
		cfg.Agent.MCP = projectMCPFile
		if !hasProjectMCP {
			p.say("  ! " + projectMCPFile + " does not exist yet; a run will refuse to start until it does")
		}
	case mcpNone:
		cfg.Agent.MCP = "none"
	case mcpFile:
		current := ""
		if def == mcpFile {
			current = path
		}
		file, err := p.text("Path, relative to the project", current)
		if err != nil {
			return err
		}
		if file == "" {
			cfg.Agent.MCP = ""
		} else {
			cfg.Agent.MCP = file
			if !fileExistsIn(baseDir, file) {
				p.say("  ! " + file + " does not exist yet; a run will refuse to start until it does")
			}
		}
	}

	skills, err := p.yesNo("Load the skill catalogue into build iterations?", !cfg.Agent.SkillsDisabled())
	if err != nil {
		return err
	}
	if skills {
		cfg.Agent.Skills = ""
	} else {
		cfg.Agent.Skills = "none"
	}
	return nil
}

func askReview(p *prompter, cfg *config.Config) error {
	p.section("Review")
	p.say("A separate agent reviews each story after it is built and fixes what it finds.")
	on, err := p.yesNo("Review every story?", cfg.Review.Active())
	if err != nil {
		return err
	}
	cfg.Review.Enabled = enabledSwitch(on)
	if on {
		skill, err := p.text("Skill the reviewer runs (e.g. /code-review, empty for none)", cfg.Review.Skill)
		if err != nil {
			return err
		}
		cfg.Review.Skill = skill
	}

	p.section("Consolidation")
	p.say("One agent at the end of a run sees all of its stories and removes duplicate")
	p.say("helpers and competing patterns, in a separate refactor commit.")
	on, err = p.yesNo("Consolidate at the end of a run?", cfg.Consolidate.Active())
	if err != nil {
		return err
	}
	cfg.Consolidate.Enabled = enabledSwitch(on)
	if on {
		skill, err := p.text("Skill it runs (empty for none)", cfg.Consolidate.Skill)
		if err != nil {
			return err
		}
		cfg.Consolidate.Skill = skill
	}
	return nil
}

// enabledSwitch turns a yes into "no key" and a no into an explicit off. Both
// passes are on by default, so the file only needs to say so when they are not.
func enabledSwitch(on bool) *bool {
	if on {
		return nil
	}
	return config.Bool(false)
}

func askBox(p *prompter, baseDir string, cfg *config.Config, deps setupDeps) error {
	p.section("Cloud box")
	configured := cfg.Box.Location != "" || cfg.Box.Type != ""
	use, err := p.yesNo("Set up a cloud box for unattended runs (chief box)?", configured)
	if err != nil || !use {
		return err
	}

	if deps.hasBoxToken() {
		check, err := p.yesNo("Check the box credentials (Hetzner, GitHub, Claude)?", false)
		if err != nil {
			return err
		}
		if check {
			loginOrWarn(p, deps)
		}
	} else {
		p.say("No Hetzner token stored yet.")
		login, err := p.yesNo("Set up the box credentials now (chief box token)?", true)
		if err != nil {
			return err
		}
		if login {
			loginOrWarn(p, deps)
		}
	}

	if deps.hasBoxToken() {
		pick, err := p.yesNo("Choose where the box runs and on what?", !configured)
		if err != nil {
			return err
		}
		if pick {
			location, serverType, cancelled, err := deps.pickBox(cfg.Box.Location, cfg.Box.Type)
			switch {
			case err != nil:
				p.say(fmt.Sprintf("  ! %v; 'chief box config' asks again", err))
			case cancelled:
				p.say("  Nothing chosen, keeping what was there.")
			default:
				cfg.Box.Location, cfg.Box.Type = location, serverType
				p.say(fmt.Sprintf("  ✓ %s in %s", serverType, location))
			}
		}
	} else {
		p.say("Skipping location and server type: they need a Hetzner token. 'chief box config' asks later.")
	}

	def := strings.Join(cfg.Box.Files, ", ")
	if def == "" && fileExistsIn(baseDir, ".env") {
		def = ".env"
	}
	files, err := p.text("Untracked files to copy to the box (comma separated)", def)
	if err != nil {
		return err
	}
	cfg.Box.Files = boxFiles(files)
	return nil
}

// boxFiles parses the comma separated list, leaving the key out when the
// answer is the default a box uses anyway.
func boxFiles(answer string) []string {
	var files []string
	for _, f := range strings.Split(answer, ",") {
		if f = strings.TrimSpace(f); f != "" {
			files = append(files, f)
		}
	}
	if len(files) == 1 && files[0] == ".env" {
		return nil
	}
	return files
}

func loginOrWarn(p *prompter, deps setupDeps) {
	if err := deps.boxLogin(); err != nil {
		p.say(fmt.Sprintf("  ! %v; 'chief box token' tries again", err))
	}
}

// detectWorktreeSetup proposes a worktree setup command from what sits in the
// project: copy the .env a fresh checkout lacks, then install dependencies
// with the package manager the lockfile belongs to.
func detectWorktreeSetup(dir string) string {
	var steps []string
	// The .env comes first: a Laravel install runs artisan in its post-install
	// scripts, and artisan without an .env fails.
	if fileExistsIn(dir, ".env") {
		steps = append(steps, `cp "$CHIEF_REPO_DIR/.env" .env`)
	}
	if fileExistsIn(dir, "composer.json") {
		steps = append(steps, "composer install --no-interaction")
	}
	switch {
	case fileExistsIn(dir, "pnpm-lock.yaml"):
		steps = append(steps, "pnpm install --frozen-lockfile")
	case fileExistsIn(dir, "yarn.lock"):
		steps = append(steps, "yarn install --frozen-lockfile")
	case fileExistsIn(dir, "bun.lock"), fileExistsIn(dir, "bun.lockb"):
		steps = append(steps, "bun install --frozen-lockfile")
	case fileExistsIn(dir, "package-lock.json"):
		steps = append(steps, "npm ci")
	case fileExistsIn(dir, "package.json"):
		steps = append(steps, "npm install")
	}
	switch {
	case fileExistsIn(dir, "uv.lock"):
		steps = append(steps, "uv sync")
	case fileExistsIn(dir, "poetry.lock"):
		steps = append(steps, "poetry install")
	}
	if fileExistsIn(dir, "Gemfile.lock") {
		steps = append(steps, "bundle install")
	}
	return strings.Join(steps, " && ")
}

func fileExistsIn(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// prompter asks questions a line at a time. It reads a byte at a time rather
// than through a buffer, so nothing typed ahead is taken from the TUI pickers
// and logins that share the terminal with it.
type prompter struct {
	in  io.Reader
	out io.Writer
}

func (p *prompter) say(line string) {
	_, _ = fmt.Fprintln(p.out, line)
}

func (p *prompter) section(title string) {
	p.say("")
	p.say("── " + title)
}

// line reads one answer, without its newline. An input that ends before an
// answer aborts the setup.
func (p *prompter) line() (string, error) {
	var line []byte
	buf := make([]byte, 1)
	for {
		n, err := p.in.Read(buf)
		if n == 1 {
			if buf[0] == '\n' {
				return strings.TrimSpace(string(line)), nil
			}
			line = append(line, buf[0])
		}
		if err != nil {
			if len(line) > 0 {
				return strings.TrimSpace(string(line)), nil
			}
			_, _ = fmt.Fprintln(p.out)
			return "", errSetupAborted
		}
	}
}

func (p *prompter) yesNo(question string, def bool) (bool, error) {
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	for {
		_, _ = fmt.Fprintf(p.out, "%s %s ", question, hint)
		answer, err := p.line()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "":
			return def, nil
		case "y", "yes", "j", "ja":
			return true, nil
		case "n", "no", "nein":
			return false, nil
		}
		p.say("  Please answer y or n.")
	}
}

// text asks for a free-form answer. Enter keeps def, "-" clears it.
func (p *prompter) text(question, def string) (string, error) {
	if def != "" {
		_, _ = fmt.Fprintf(p.out, "%s [%s]: ", question, def)
	} else {
		_, _ = fmt.Fprintf(p.out, "%s: ", question)
	}
	answer, err := p.line()
	switch {
	case err != nil:
		return "", err
	case answer == "":
		return def, nil
	case answer == "-":
		return "", nil
	}
	return answer, nil
}

// choose offers numbered options and returns the index picked; Enter takes def.
func (p *prompter) choose(question string, options []string, def int) (int, error) {
	p.say(question + ":")
	for i, o := range options {
		marker := " "
		if i == def {
			marker = "›"
		}
		p.say(fmt.Sprintf("  %s %d) %s", marker, i+1, o))
	}
	for {
		_, _ = fmt.Fprintf(p.out, "Choice [%d]: ", def+1)
		answer, err := p.line()
		if err != nil {
			return 0, err
		}
		if answer == "" {
			return def, nil
		}
		var n int
		if _, err := fmt.Sscanf(answer, "%d", &n); err == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		p.say(fmt.Sprintf("  Please pick 1 to %d.", len(options)))
	}
}
