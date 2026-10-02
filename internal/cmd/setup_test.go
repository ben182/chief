package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ben182/chief/internal/config"
)

// fakeSetupDeps is a machine with gh logged in and no Hetzner token, where the
// box picker and login must not be reached unless a test says so.
func fakeSetupDeps(t *testing.T) setupDeps {
	return setupDeps{
		checkGH:     func() (bool, bool, error) { return true, true, nil },
		hasBoxToken: func() bool { return false },
		boxLogin: func() error {
			t.Error("box login must not run")
			return nil
		},
		pickBox: func(string, string) (string, string, bool, error) {
			t.Error("box picker must not run")
			return "", "", true, nil
		},
	}
}

func writeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runSetupWith(t *testing.T, dir, answers string, deps setupDeps) (*config.Config, string, error) {
	t.Helper()
	var out strings.Builder
	err := runSetup(dir, strings.NewReader(answers), &out, deps)
	cfg, loadErr := config.Load(dir)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	return cfg, out.String(), err
}

// A fresh Laravel project accepting every default gets the detected worktree
// setup, push and PR, the project's own MCP servers, and both passes on.
func TestSetupFreshProjectDefaults(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, ".env", "composer.json", "package-lock.json", ".mcp.json")

	// worktree, push, PR, MCP, skills, review, review skill, consolidate,
	// consolidate skill, box, save
	cfg, out, err := runSetupWith(t, dir, strings.Repeat("\n", 11), fakeSetupDeps(t))
	if err != nil {
		t.Fatalf("setup failed: %v\n%s", err, out)
	}

	want := `cp "$CHIEF_REPO_DIR/.env" .env && composer install --no-interaction && npm ci`
	if cfg.Worktree.Setup != want {
		t.Errorf("worktree setup = %q, want %q", cfg.Worktree.Setup, want)
	}
	if !cfg.OnComplete.Push || !cfg.OnComplete.CreatePR {
		t.Error("expected push and PR on for a fresh project")
	}
	if cfg.Agent.MCP != ".mcp.json" {
		t.Errorf("expected the project's .mcp.json, got %q", cfg.Agent.MCP)
	}
	if cfg.Agent.Skills != "" {
		t.Errorf("expected skills inherited, got %q", cfg.Agent.Skills)
	}
	if !cfg.Review.Active() || !cfg.Consolidate.Active() {
		t.Error("expected review and consolidation on")
	}
	if cfg.Box.Location != "" || cfg.Box.Type != "" {
		t.Errorf("expected no box, got %+v", cfg.Box)
	}
}

// Running it again opens on what is saved, so Enter all the way through
// changes nothing, including choices that differ from the fresh defaults.
func TestSetupRerunKeepsExistingAnswers(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, "composer.json", ".mcp.json")
	existing := config.Default()
	existing.Worktree.Setup = "make setup"
	existing.OnComplete.Push = true
	existing.Agent.MCP = "none"
	existing.Agent.Skills = "none"
	existing.Review.Enabled = config.Bool(false)
	existing.Consolidate.Skill = "/tidy"
	if err := config.Save(dir, existing); err != nil {
		t.Fatal(err)
	}

	// worktree, push, PR (off, so no gh check), MCP, skills, review (off, no
	// skill question), consolidate, consolidate skill, box, save
	cfg, out, err := runSetupWith(t, dir, strings.Repeat("\n", 10), fakeSetupDeps(t))
	if err != nil {
		t.Fatalf("setup failed: %v\n%s", err, out)
	}
	if cfg.Worktree.Setup != "make setup" {
		t.Errorf("worktree setup changed to %q", cfg.Worktree.Setup)
	}
	if !cfg.OnComplete.Push || cfg.OnComplete.CreatePR {
		t.Errorf("expected push on and PR off, got %+v", cfg.OnComplete)
	}
	if cfg.Agent.MCP != "none" || cfg.Agent.Skills != "none" {
		t.Errorf("agent changed to %+v", cfg.Agent)
	}
	if cfg.Review.Active() {
		t.Error("review was switched back on")
	}
	if cfg.Consolidate.Skill != "/tidy" {
		t.Errorf("consolidate skill changed to %q", cfg.Consolidate.Skill)
	}
}

func TestSetupAnswersAreApplied(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, "go.mod")

	answers := strings.Join([]string{
		"go mod download", // worktree
		"n",               // push (PR is not asked)
		"3",               // MCP: none
		"n",               // skills
		"y",               // review
		"/code-review",    // review skill
		"n",               // consolidate
		"n",               // box
		"y",               // save
	}, "\n") + "\n"
	cfg, out, err := runSetupWith(t, dir, answers, fakeSetupDeps(t))
	if err != nil {
		t.Fatalf("setup failed: %v\n%s", err, out)
	}
	if cfg.Worktree.Setup != "go mod download" {
		t.Errorf("worktree setup = %q", cfg.Worktree.Setup)
	}
	if cfg.OnComplete.Push || cfg.OnComplete.CreatePR {
		t.Errorf("expected push and PR off, got %+v", cfg.OnComplete)
	}
	if cfg.Agent.MCP != "none" || cfg.Agent.Skills != "none" {
		t.Errorf("agent = %+v", cfg.Agent)
	}
	if !cfg.Review.Active() || cfg.Review.Skill != "/code-review" {
		t.Errorf("review = %+v", cfg.Review)
	}
	if cfg.Consolidate.Active() {
		t.Error("expected consolidation off")
	}
}

// Without gh a PR cannot be opened; the setup says so and turns it off unless
// told otherwise.
func TestSetupPRWithoutGHIsTurnedOff(t *testing.T) {
	dir := t.TempDir()
	deps := fakeSetupDeps(t)
	deps.checkGH = func() (bool, bool, error) { return true, false, nil }

	// worktree, push, PR, keep anyway?, then defaults
	cfg, out, err := runSetupWith(t, dir, "\ny\ny\n"+strings.Repeat("\n", 9), deps)
	if err != nil {
		t.Fatalf("setup failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "gh auth login") {
		t.Errorf("expected the gh login hint, got:\n%s", out)
	}
	if cfg.OnComplete.CreatePR {
		t.Error("expected PR creation off without gh")
	}
	if !cfg.OnComplete.Push {
		t.Error("push must stay on")
	}
}

func TestSetupDeclinedSaveWritesNothing(t *testing.T) {
	dir := t.TempDir()
	answers := strings.Repeat("\n", 10) + "n\n"
	_, out, err := runSetupWith(t, dir, answers, fakeSetupDeps(t))
	if err != nil {
		t.Fatalf("setup failed: %v\n%s", err, out)
	}
	if config.Exists(dir) {
		t.Error("expected no config file after declining to save")
	}
}

func TestSetupInputEndingEarlyAborts(t *testing.T) {
	dir := t.TempDir()
	_, _, err := runSetupWith(t, dir, "\n\n", fakeSetupDeps(t))
	if !errors.Is(err, errSetupAborted) {
		t.Errorf("expected errSetupAborted, got %v", err)
	}
	if config.Exists(dir) {
		t.Error("expected nothing written after an aborted setup")
	}
}

// The box branch logs in when there is no token, then offers the picker once
// there is one, and keeps .env out of the file since it is the default.
func TestSetupBox(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, ".env")
	token := false
	deps := fakeSetupDeps(t)
	deps.hasBoxToken = func() bool { return token }
	deps.boxLogin = func() error { token = true; return nil }
	deps.pickBox = func(string, string) (string, string, bool, error) { return "nbg1", "cx33", false, nil }

	answers := strings.Repeat("\n", 9) + // up to and including consolidate skill
		"y\n" + // box
		"\n" + // log in now (default yes)
		"\n" + // choose location (default yes, not configured yet)
		"\n" + // files: keep .env
		"\n" // save
	cfg, out, err := runSetupWith(t, dir, answers, deps)
	if err != nil {
		t.Fatalf("setup failed: %v\n%s", err, out)
	}
	if cfg.Box.Location != "nbg1" || cfg.Box.Type != "cx33" {
		t.Errorf("box = %+v", cfg.Box)
	}
	if cfg.Box.Files != nil {
		t.Errorf("expected the default .env left implicit, got %v", cfg.Box.Files)
	}
}

func TestPrompterRepeatsInvalidAnswers(t *testing.T) {
	var out strings.Builder
	p := &prompter{in: strings.NewReader("vielleicht\nja\n9\n2\n"), out: &out}
	yes, err := p.yesNo("Go?", false)
	if err != nil || !yes {
		t.Errorf("yesNo = %v, %v", yes, err)
	}
	n, err := p.choose("Pick", []string{"a", "b"}, 0)
	if err != nil || n != 1 {
		t.Errorf("choose = %d, %v", n, err)
	}
	if !strings.Contains(out.String(), "Please answer y or n") || !strings.Contains(out.String(), "Please pick 1 to 2") {
		t.Errorf("expected both retry hints, got:\n%s", out.String())
	}
}

func TestPrompterTextClearsWithDash(t *testing.T) {
	p := &prompter{in: strings.NewReader("-\n"), out: &strings.Builder{}}
	got, err := p.text("Setup", "npm ci")
	if err != nil || got != "" {
		t.Errorf("text = %q, %v", got, err)
	}
}

func TestDetectWorktreeSetup(t *testing.T) {
	tests := []struct {
		files []string
		want  string
	}{
		{nil, ""},
		{[]string{"go.mod"}, ""},
		{[]string{"package.json", "pnpm-lock.yaml"}, "pnpm install --frozen-lockfile"},
		{[]string{"package.json"}, "npm install"},
		{[]string{"pyproject.toml", "uv.lock"}, "uv sync"},
		{[]string{".env", "composer.json"}, `cp "$CHIEF_REPO_DIR/.env" .env && composer install --no-interaction`},
	}
	for _, tt := range tests {
		dir := t.TempDir()
		writeFiles(t, dir, tt.files...)
		if got := detectWorktreeSetup(dir); got != tt.want {
			t.Errorf("%v: got %q, want %q", tt.files, got, tt.want)
		}
	}
}
