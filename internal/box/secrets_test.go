package box

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateConfig points the user config directory at a temporary one, so a test
// never reads or writes the real tokens on this machine.
func isolateConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	// Anything inherited from the surrounding shell would decide these tests
	// instead of the code under test.
	for _, key := range []string{
		"CHIEF_BOX_HETZNER_TOKEN", "HCLOUD_TOKEN",
		"CHIEF_BOX_GH_TOKEN", "GH_TOKEN", "GITHUB_TOKEN",
		"CLAUDE_CODE_OAUTH_TOKEN", "CHIEF_BOX_SSH_KEY",
	} {
		t.Setenv(key, "")
	}
	// An empty PATH means no `gh` and no `claude` to fall back on, which is what
	// makes the "nothing available" cases deterministic.
	t.Setenv("PATH", filepath.Join(home, "empty-bin"))
	return home
}

func TestTokensRoundTripThroughTheConfigDirectory(t *testing.T) {
	isolateConfig(t)

	if got := readToken("claude-token"); got != "" {
		t.Errorf("a fresh machine has a token: %q", got)
	}
	if err := writeToken("claude-token", "sk-ant-oat01-example"); err != nil {
		t.Fatalf("writeToken: %v", err)
	}
	if got := readToken("claude-token"); got != "sk-ant-oat01-example" {
		t.Errorf("readToken = %q, want what was written", got)
	}

	path, err := tokenFile("claude-token")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// A token readable by every account on the machine is a token leaked.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %o, want 600", perm)
	}
}

func TestHetznerTokenPrefersTheEnvironment(t *testing.T) {
	isolateConfig(t)
	if err := writeToken("hetzner-token", "from-file"); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CHIEF_BOX_HETZNER_TOKEN", "from-env")
	got, err := resolveHetznerToken()
	if err != nil {
		t.Fatalf("resolveHetznerToken: %v", err)
	}
	if got != "from-env" {
		t.Errorf("got %q, want the environment to win", got)
	}

	// HCLOUD_TOKEN is what someone who already uses the hcloud CLI has set.
	t.Setenv("CHIEF_BOX_HETZNER_TOKEN", "")
	t.Setenv("HCLOUD_TOKEN", "from-hcloud")
	if got, _ := resolveHetznerToken(); got != "from-hcloud" {
		t.Errorf("got %q, want HCLOUD_TOKEN to be honoured", got)
	}

	// With nothing in the environment, the saved one is used.
	t.Setenv("HCLOUD_TOKEN", "")
	if got, _ := resolveHetznerToken(); got != "from-file" {
		t.Errorf("got %q, want the saved token", got)
	}
}

func TestHetznerTokenSaysExactlyWhatToDoWhenThereIsNone(t *testing.T) {
	isolateConfig(t)
	_, err := resolveHetznerToken()
	if err == nil {
		t.Fatal("expected an error with no token anywhere")
	}
	// This is the one secret nothing can mint, so the message has to carry the
	// whole recipe rather than a name to look up.
	for _, want := range []string{"Hetzner console", "API tokens", "CHIEF_BOX_HETZNER_TOKEN"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error is missing %q:\n%s", want, err)
		}
	}
}

func TestHetznerTokenReadsTheHcloudCLIsOwnConfig(t *testing.T) {
	home := isolateConfig(t)
	// ~/.config/hcloud, which is where the CLI puts it on every Unix including
	// macOS, rather than the platform config directory.
	dir := filepath.Join(home, ".config", "hcloud")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Somebody who already set the hcloud CLI up should not have to set
	// anything up again.
	toml := `active_context = "work"

[[contexts]]
name = "personal"
token = "personal-token"

[[contexts]]
name = "work"
token = "work-token"
`
	if err := os.WriteFile(filepath.Join(dir, "cli.toml"), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := resolveHetznerToken()
	if err != nil {
		t.Fatalf("resolveHetznerToken: %v", err)
	}
	if got != "work-token" {
		t.Errorf("got %q, want the active context's token", got)
	}
}

func TestHcloudCLITokenIgnoresAFileItCannotMakeSenseOf(t *testing.T) {
	home := isolateConfig(t)
	dir := filepath.Join(home, ".config", "hcloud")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{
		"",
		"nonsense",
		`active_context = "gone"` + "\n\n[[contexts]]\nname = \"other\"\ntoken = \"t\"\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, "cli.toml"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := hcloudCLIToken(); got != "" {
			t.Errorf("hcloudCLIToken(%q) = %q, want empty", content, got)
		}
	}
}

func TestGitHubTokenPrefersTheEnvironment(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GH_TOKEN", "from-env")
	got, err := resolveGitHubToken(context.Background())
	if err != nil {
		t.Fatalf("resolveGitHubToken: %v", err)
	}
	if got != "from-env" {
		t.Errorf("got %q, want the environment to win", got)
	}
}

func TestGitHubTokenPointsAtGhAuthLogin(t *testing.T) {
	isolateConfig(t)
	_, err := resolveGitHubToken(context.Background())
	if err == nil {
		t.Fatal("expected an error with no token and no gh")
	}
	// `gh auth login` is the fix for almost everyone who hits this.
	if !strings.Contains(err.Error(), "gh auth login") {
		t.Errorf("error = %q, want it to name the command that fixes this", err)
	}
}

func TestClaudeTokenPrefersTheEnvironmentThenTheSavedOne(t *testing.T) {
	isolateConfig(t)
	ctx := context.Background()

	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "sk-ant-from-env")
	got, err := resolveClaudeToken(ctx, false, os.Stdout, os.Stderr)
	if err != nil {
		t.Fatalf("resolveClaudeToken: %v", err)
	}
	if got != "sk-ant-from-env" {
		t.Errorf("got %q, want the environment to win", got)
	}

	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	if err := writeToken("claude-token", "sk-ant-saved"); err != nil {
		t.Fatal(err)
	}
	// The saved token is the normal case after the first run: no browser, no
	// prompt, nothing to type.
	got, err = resolveClaudeToken(ctx, false, os.Stdout, os.Stderr)
	if err != nil {
		t.Fatalf("resolveClaudeToken: %v", err)
	}
	if got != "sk-ant-saved" {
		t.Errorf("got %q, want the saved token", got)
	}
}

func TestClaudeTokenDoesNotWaitForSomebodyWhoIsNotThere(t *testing.T) {
	isolateConfig(t)
	// Without a terminal, starting a browser login would hang forever on a
	// prompt nobody will answer — in a systemd unit or a CI job, that is a job
	// that never ends rather than one that fails.
	_, err := resolveClaudeToken(context.Background(), false, os.Stdout, os.Stderr)
	if err == nil {
		t.Fatal("expected an error rather than a wait")
	}
	if !strings.Contains(err.Error(), "CLAUDE_CODE_OAUTH_TOKEN") {
		t.Errorf("error = %q, want it to name the variable to set", err)
	}
}

func TestClaudeTokenPatternFindsTheTokenAnywhereInTheOutput(t *testing.T) {
	// The command prints prose around the token, and which line it lands on is
	// presentation this should not be brittle about.
	output := "Success! Your token is:\n\nsk-ant-oat01-AbCdEf0123456789_xyz-ABCDEFG\n\nStore it somewhere safe.\n"
	got := claudeTokenPattern.FindString(output)
	if got != "sk-ant-oat01-AbCdEf0123456789_xyz-ABCDEFG" {
		t.Errorf("found %q", got)
	}
	if claudeTokenPattern.FindString("no token here at all") != "" {
		t.Error("matched something that is not a token")
	}
	// Too short to be a token: matching it would save rubbish and fail on the box.
	if claudeTokenPattern.FindString("sk-ant-short") != "" {
		t.Error("matched a string too short to be a token")
	}
}

func TestLocalPublicKeyPrefersTheModernAlgorithm(t *testing.T) {
	home := isolateConfig(t)
	ssh := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ssh, "id_rsa.pub"), []byte("ssh-rsa AAAA rsa\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ssh, "id_ed25519.pub"), []byte("ssh-ed25519 AAAA ed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	key, path, err := localPublicKey()
	if err != nil {
		t.Fatalf("localPublicKey: %v", err)
	}
	if !strings.Contains(key, "ed25519") {
		t.Errorf("picked %q from %s, want the ed25519 key", key, path)
	}
}

func TestLocalPublicKeySaysHowToMakeOne(t *testing.T) {
	isolateConfig(t)
	_, _, err := localPublicKey()
	if err == nil {
		t.Fatal("expected an error with no key")
	}
	if !strings.Contains(err.Error(), "ssh-keygen") {
		t.Errorf("error = %q, want it to name the command that fixes this", err)
	}
}

func TestLocalPublicKeyHonoursAnExplicitPath(t *testing.T) {
	home := isolateConfig(t)
	explicit := filepath.Join(home, "my-key.pub")
	if err := os.WriteFile(explicit, []byte("ssh-ed25519 AAAA chosen\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHIEF_BOX_SSH_KEY", explicit)

	key, path, err := localPublicKey()
	if err != nil {
		t.Fatalf("localPublicKey: %v", err)
	}
	if path != explicit || !strings.Contains(key, "chosen") {
		t.Errorf("got %q from %q, want the named key", key, path)
	}

	t.Setenv("CHIEF_BOX_SSH_KEY", filepath.Join(home, "missing.pub"))
	if _, _, err := localPublicKey(); err == nil {
		t.Error("expected an error when the named key is not there")
	}
}
