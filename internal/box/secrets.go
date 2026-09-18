package box

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Secrets are what a box needs to be useful: an agent that is logged in, and a
// git remote it can clone from and push to.
type Secrets struct {
	// ClaudeToken authenticates the agent. It is a long-lived token from
	// `claude setup-token`, which exists for exactly this — a machine with no
	// browser and nobody at it.
	ClaudeToken string
	// GitHubToken lets the box clone the project and, when the run finishes,
	// push the branch and open the pull request.
	GitHubToken string
	// HetznerToken creates and destroys the box itself. It never leaves this
	// machine.
	HetznerToken string
}

// tokenFile is where a token chief had to obtain interactively is kept, so the
// question is asked once rather than before every run.
func tokenFile(name string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "chief", name), nil
}

// readToken returns a token saved by a previous run, or "" when there is none.
func readToken(name string) string {
	path, err := tokenFile(name)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path) //nolint:gosec // the path is chief's own config directory
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// writeToken saves a token for next time, readable only by its owner. A failure
// to save is not fatal: the token in hand still works for this run, and being
// asked again next time is a smaller problem than refusing to start.
func writeToken(name, token string) error {
	path, err := tokenFile(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token+"\n"), 0o600)
}

// claudeTokenPattern matches the long-lived OAuth token `claude setup-token`
// prints. It is used to pick the token out of the command's output rather than
// assuming which line it lands on, because that is presentation and this is not
// the place to be brittle about it.
var claudeTokenPattern = regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{20,}`)

// ResolveSecrets collects everything a box needs, asking only for what it
// cannot work out.
//
// The order is the same for each: an environment variable wins, then what a
// previous run saved, then the tool that can produce one. That last step is the
// point — `gh auth token` already knows the GitHub token, and `claude
// setup-token` can mint the Claude one, so the normal case involves reading no
// documentation and copying nothing into a file.
//
// interactive says whether there is a person at the terminal to complete a
// browser login. With no one there, a missing token is an error naming the
// command that would fix it rather than a process quietly waiting forever on a
// prompt nobody will answer.
func ResolveSecrets(ctx context.Context, interactive bool, out *os.File, errOut io.Writer) (Secrets, error) {
	var s Secrets
	var err error

	if s.HetznerToken, err = resolveHetznerToken(); err != nil {
		return s, err
	}
	if s.GitHubToken, err = resolveGitHubToken(ctx); err != nil {
		return s, err
	}
	if s.ClaudeToken, err = resolveClaudeToken(ctx, interactive, out, errOut); err != nil {
		return s, err
	}
	return s, nil
}

// resolveHetznerToken finds the Hetzner API token. There is no command that can
// mint one — it comes from the Hetzner console — so this is the one secret that
// has to be put somewhere by hand, once.
func resolveHetznerToken() (string, error) {
	if t := strings.TrimSpace(os.Getenv("CHIEF_BOX_HETZNER_TOKEN")); t != "" {
		return t, nil
	}
	if t := strings.TrimSpace(os.Getenv("HCLOUD_TOKEN")); t != "" {
		return t, nil
	}
	if t := readToken("hetzner-token"); t != "" {
		return t, nil
	}
	// The hcloud CLI keeps its contexts here. Reading it means someone who
	// already set that up does not have to set anything up again.
	if t := hcloudCLIToken(); t != "" {
		return t, nil
	}
	// The instruction is a command rather than a shell one-liner writing to a
	// path: the path contains a space on macOS, and a `read` in a pasted block
	// swallows the next line of the paste as its input.
	return "", fmt.Errorf(
		"no Hetzner API token.\n" +
			"  Create one in the Hetzner console (Security → API tokens, Read & Write),\n" +
			"  then run:  chief box token\n" +
			"  or set CHIEF_BOX_HETZNER_TOKEN")
}

// hcloudCLIToken reads the active context's token out of the hcloud CLI's own
// configuration, for the user who already has that set up.
//
// The path is ~/.config/hcloud/cli.toml on every Unix including macOS — the CLI
// hardcodes it rather than using the platform's config directory, so looking in
// ~/Library/Application Support would find nothing on the machine most likely to
// have it.
func hcloudCLIToken() string {
	path := strings.TrimSpace(os.Getenv("HCLOUD_CONFIG"))
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		path = filepath.Join(home, ".config", "hcloud", "cli.toml")
	}
	data, err := os.ReadFile(path) //nolint:gosec // the hcloud CLI's own config path
	if err != nil {
		return ""
	}
	// A hand-rolled read of the two fields that matter, rather than a TOML
	// dependency for a file chief only ever reads opportunistically.
	var active string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "active_context"); ok {
			active = strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), "=")), `"`)
			break
		}
	}
	if active == "" {
		return ""
	}
	var inContext bool
	var name, token string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "[[contexts]]":
			if inContext && name == active && token != "" {
				return token
			}
			inContext, name, token = true, "", ""
		case inContext && strings.HasPrefix(line, "name"):
			name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "name")), "=")), `"`)
		case inContext && strings.HasPrefix(line, "token"):
			token = strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "token")), "=")), `"`)
		}
	}
	if name == active {
		return token
	}
	return ""
}

// resolveGitHubToken finds the GitHub token, preferring the one `gh` is already
// holding. Somebody who can run `gh pr create` on this machine has, by
// definition, a token that works for the repository they are about to build.
func resolveGitHubToken(ctx context.Context) (string, error) {
	for _, key := range []string{"CHIEF_BOX_GH_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if t := strings.TrimSpace(os.Getenv(key)); t != "" {
			return t, nil
		}
	}
	if path, err := exec.LookPath("gh"); err == nil {
		cmd := exec.CommandContext(ctx, path, "auth", "token")
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		if cmd.Run() == nil {
			if t := strings.TrimSpace(stdout.String()); t != "" {
				return t, nil
			}
		}
	}
	return "", fmt.Errorf(
		"no GitHub token — the box needs one to clone the project and to push what it builds.\n" +
			"  Run 'gh auth login', or set GH_TOKEN to a fine-grained token with access to this repository")
}

// resolveClaudeToken finds the long-lived agent token, minting one if it has to.
//
// `claude setup-token` runs a browser login and prints the token, and this hands
// it the terminal to do that in. It is the difference between a setup step
// somebody has to read about and one that happens the first time they ask for a
// box — and it happens once, because the result is saved.
func resolveClaudeToken(ctx context.Context, interactive bool, out *os.File, errOut io.Writer) (string, error) {
	if t := strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")); t != "" {
		return t, nil
	}
	if t := readToken("claude-token"); t != "" {
		return t, nil
	}

	claude, err := exec.LookPath("claude")
	if err != nil {
		return "", fmt.Errorf(
			"no Claude token, and no claude CLI to create one with.\n" +
				"  Install Claude Code, then run 'claude setup-token', " +
				"or set CLAUDE_CODE_OAUTH_TOKEN")
	}
	if !interactive {
		return "", fmt.Errorf("no Claude token — the agent on the box would have nothing to log in with")
	}

	_, _ = fmt.Fprintln(errOut, "No Claude token yet. Running 'claude setup-token' — "+
		"it opens a browser once, and the result is saved for every run after this.")

	// The token is on stdout, captured to read it. The login flow's own prompts
	// and its browser URL go to stderr and must reach the real terminal, not a
	// buffer — so os.Stderr, rather than wherever this function reports to.
	cmd := exec.CommandContext(ctx, claude, "setup-token")
	var stdout bytes.Buffer
	cmd.Stdin = os.Stdin
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude setup-token: %w", err)
	}

	token := claudeTokenPattern.FindString(stdout.String())
	if token == "" {
		return "", fmt.Errorf(
			"claude setup-token did not print a token.\n" +
				"  Run it by hand and set CLAUDE_CODE_OAUTH_TOKEN to the result")
	}
	if err := writeToken("claude-token", token); err != nil {
		_, _ = fmt.Fprintf(errOut, "  (could not save the token for next time: %v)\n", err)
	} else {
		path, _ := tokenFile("claude-token")
		_, _ = fmt.Fprintf(errOut, "  Saved to %s\n", path)
	}
	_ = out
	return token, nil
}

// gitHubTokenNote warns when the token going onto the box reaches further than
// the project the box was created for, and returns "" when it does not.
//
// This is the widest thing a box holds, and the easiest to overlook. `gh auth
// token` hands back the token behind `gh auth login`, which is an account-wide
// credential: every repository the person can read, and every one they can push
// to. It then sits in a file on a machine where an agent runs for hours with
// permissions skipped, working on code and dependencies nobody read first.
//
// Nothing here refuses to proceed. The token is what makes the box work at all,
// most projects are private repositories belonging to the person running this,
// and a warning that blocks is a warning people route around. It names the
// exposure once, and names the narrower thing.
func gitHubTokenNote(token string) string {
	token = strings.TrimSpace(token)
	switch {
	case strings.HasPrefix(token, "github_pat_"):
		// Fine-grained: already scoped to chosen repositories.
		return ""
	case strings.HasPrefix(token, "ghs_"):
		// A GitHub App installation token: scoped, and it expires within the hour.
		return ""
	case strings.HasPrefix(token, "gho_"), strings.HasPrefix(token, "ghp_"), strings.HasPrefix(token, "ghu_"):
		return "the GitHub token reaches every repository your account does.\n" +
			"      To narrow it to this one, put a fine-grained token in CHIEF_BOX_GH_TOKEN"
	default:
		// An unrecognised prefix. Saying nothing beats guessing wrong about
		// somebody's enterprise setup.
		return ""
	}
}
