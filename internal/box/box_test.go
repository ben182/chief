package box

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestCloudInitIsValidCloudConfig(t *testing.T) {
	out := cloudInit(cloudInitOptions{Hostname: "chief-demo-auth"})

	// cloud-init refuses a file without this first line, and the failure is
	// silent: the instance boots, nothing is installed, and every wait after it
	// times out.
	if !strings.HasPrefix(out, "#cloud-config\n") {
		t.Fatalf("missing the #cloud-config header:\n%s", firstLines(out, 3))
	}

	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("not valid YAML: %v", err)
	}
	for _, key := range []string{"packages", "runcmd", "write_files", "users", "hostname"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("cloud-config has no %q section", key)
		}
	}
	if parsed["hostname"] != "chief-demo-auth" {
		t.Errorf("hostname = %v, want the name it was given", parsed["hostname"])
	}
}

func TestCloudInitInstallsCurrentRuntimes(t *testing.T) {
	out := cloudInit(cloudInitOptions{Hostname: "h"})
	var parsed struct {
		Packages []string `yaml:"packages"`
		Runcmd   []string `yaml:"runcmd"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	packages := strings.Join(parsed.Packages, " ")
	runcmd := strings.Join(parsed.Runcmd, "\n")

	if !strings.Contains(packages, "php"+phpSeries+"-cli") {
		t.Errorf("PHP %s is not installed:\n%s", phpSeries, packages)
	}
	if !strings.Contains(packages, "postgresql") {
		t.Error("PostgreSQL is not installed")
	}
	if !strings.Contains(runcmd, "setup_"+nodeMajor+".x") {
		t.Errorf("Node %s is not installed", nodeMajor)
	}

	// Claude Code comes from the signed apt repository. npm and the curl
	// installer both work, but unattended provisioning should not pipe a
	// downloaded script into a shell when a signed package is on offer.
	if strings.Contains(runcmd, "npm install -g @anthropic-ai/claude-code") {
		t.Error("Claude Code is still installed through npm")
	}
	if !strings.Contains(runcmd, "downloads.claude.ai/claude-code/apt") {
		t.Error("Claude Code is not installed from the apt repository")
	}
	if !strings.Contains(packages+runcmd, "claude-code") {
		t.Error("the claude-code package is never installed")
	}
	// The key is verified before it is trusted, and a mismatch has to stop the
	// provisioning rather than be a line in a log nobody reads.
	if !strings.Contains(runcmd, "31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE") {
		t.Error("the Claude Code signing key's fingerprint is never checked")
	}
}

func TestCloudInitEndsWithTheMarkerChiefWaitsFor(t *testing.T) {
	out := cloudInit(cloudInitOptions{Hostname: "h"})
	var parsed struct {
		Runcmd []string `yaml:"runcmd"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	last := parsed.Runcmd[len(parsed.Runcmd)-1]
	// Provisioning is "done" when this file appears, so it has to be the very
	// last thing: written any earlier and chief starts a run on a machine that
	// is still installing PHP.
	if !strings.Contains(last, readyMarker) {
		t.Errorf("the last runcmd is %q, want it to write %s", last, readyMarker)
	}
}

func TestCloudInitTakesExtraPackages(t *testing.T) {
	out := cloudInit(cloudInitOptions{Hostname: "h", ExtraPackages: []string{"imagemagick", " ", "ffmpeg"}})
	var parsed struct {
		Packages []string `yaml:"packages"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("extra packages broke the YAML: %v", err)
	}
	joined := strings.Join(parsed.Packages, " ")
	if !strings.Contains(joined, "imagemagick") || !strings.Contains(joined, "ffmpeg") {
		t.Errorf("extra packages missing: %s", joined)
	}
	for _, p := range parsed.Packages {
		if strings.TrimSpace(p) == "" {
			t.Error("a blank extra package became a package entry")
		}
	}
}

func TestCloudInitUnitSurvivesALongRun(t *testing.T) {
	out := cloudInit(cloudInitOptions{Hostname: "h"})
	// systemd's default start timeout would kill a five-hour run after 90
	// seconds, which is the whole point of the unit going wrong.
	if !strings.Contains(out, "TimeoutStartSec=infinity") {
		t.Error("the unit has no infinite start timeout; a long run would be killed")
	}
	// chief turns SIGTERM into a clean stop; SIGKILL would lose the ending.
	if !strings.Contains(out, "KillSignal=SIGTERM") {
		t.Error("the unit does not stop the run with SIGTERM")
	}
	if !strings.Contains(out, "--headless") {
		t.Error("the unit does not start a headless run")
	}
}

func TestHTTPSRemoteRewritesWhatTheBoxCannotAuthenticate(t *testing.T) {
	cases := map[string]string{
		// The box has no SSH key for GitHub and should not be given one, but it
		// does have a token — and a token only works over HTTPS.
		"git@github.com:ben182/chief.git":       "https://github.com/ben182/chief.git",
		"git@github.com:ben182/chief":           "https://github.com/ben182/chief.git",
		"ssh://git@github.com/ben182/chief.git": "https://github.com/ben182/chief.git",
		"https://github.com/ben182/chief.git":   "https://github.com/ben182/chief.git",
		"https://github.com/ben182/chief":       "https://github.com/ben182/chief.git",
		"git@gitlab.com:group/project.git":      "https://gitlab.com/group/project.git",
	}
	for in, want := range cases {
		if got := httpsRemote(in); got != want {
			t.Errorf("httpsRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestServerNameIsSomethingHetznerAccepts(t *testing.T) {
	cases := []string{
		"/Users/ben/Code/My_Project",
		"/tmp/a.b.c",
		"/x/ÜBER",
	}
	for _, dir := range cases {
		got := serverName(dir, "refactor_billing")
		if len(got) > 60 {
			t.Errorf("serverName(%q) is %d characters", dir, len(got))
		}
		if strings.HasPrefix(got, "-") || strings.HasSuffix(got, "-") {
			t.Errorf("serverName(%q) = %q, want no leading or trailing dash", dir, got)
		}
		for _, r := range got {
			valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
			if !valid {
				t.Errorf("serverName(%q) = %q contains %q", dir, got, r)
				break
			}
		}
	}
}

func TestServerNameSaysWhichProjectAndPRD(t *testing.T) {
	got := serverName("/Users/ben/Code/shop", "billing")
	// The name is what you see in the Hetzner console at the end of the month,
	// so it has to answer "what was this for".
	if !strings.Contains(got, "shop") || !strings.Contains(got, "billing") {
		t.Errorf("serverName = %q, want the project and the PRD in it", got)
	}
}

func TestRunFlagsPassTheRunsOptionsThrough(t *testing.T) {
	got := runFlags(UpOptions{Worktree: true, Verbose: true, MaxIterations: 12})
	for _, want := range []string{"--worktree", "--verbose", "-n 12"} {
		if !strings.Contains(got, want) {
			t.Errorf("runFlags = %q, want %q in it", got, want)
		}
	}
	if got := runFlags(UpOptions{}); got != "" {
		t.Errorf("runFlags with nothing set = %q, want empty", got)
	}
	// A zero cap means "let chief size it", not "-n 0", which the loop would
	// refuse.
	if got := runFlags(UpOptions{MaxIterations: 0}); strings.Contains(got, "-n") {
		t.Errorf("runFlags = %q, want no -n for an unset cap", got)
	}
}

func TestCloneScriptKeepsTheTokenOutOfTheCommandLine(t *testing.T) {
	script := cloneScript("https://github.com/x/y.git", "main")
	// The token reaches the box in a file; a credential helper reads it from
	// there. Interpolating it into the script would put it in the process list
	// of every process on the machine.
	if strings.Contains(script, "ghp_") || strings.Contains(script, "gho_") {
		t.Error("the script embeds a token")
	}
	if !strings.Contains(script, "credential.helper") {
		t.Error("the script sets no credential helper, so the clone cannot authenticate")
	}
	if !strings.Contains(script, ". ~/.chief-env") {
		t.Error("the script never reads the environment file holding the token")
	}
	// The identity arrives on stdin, so a name with a quote in it cannot break
	// out of the script.
	if !strings.Contains(script, "read -r NAME") {
		t.Error("the git identity is not read from stdin")
	}
}

func TestGitIdentityAlwaysProducesOne(t *testing.T) {
	// A repository with no configured user must not produce an empty identity:
	// git refuses to commit without one, and the failure would surface hours
	// later as a story that built but never landed.
	got := gitIdentity(t.TempDir())
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 2 || lines[0] == "" || lines[1] == "" {
		t.Errorf("gitIdentity = %q, want a name and an email", got)
	}
}

func TestStateRoundTripsAndStaysOutOfGit(t *testing.T) {
	dir := t.TempDir()
	if _, ok := LoadState(dir); ok {
		t.Fatal("a fresh project reports a box")
	}

	want := State{ServerID: 42, Name: "chief-demo", IP: "203.0.113.9", PRD: "auth", Created: time.Now()}
	if err := SaveState(dir, want); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	got, ok := LoadState(dir)
	if !ok {
		t.Fatal("the saved box was not found again")
	}
	if got.ServerID != want.ServerID || got.IP != want.IP || got.PRD != want.PRD {
		t.Errorf("round trip lost data: %+v", got)
	}
	if got.Age() < 0 || got.Age() > time.Minute {
		t.Errorf("Age = %s, want about zero", got.Age())
	}

	// A machine's address has no business in a commit, and not every project
	// gitignores .chief.
	ignore, err := os.ReadFile(filepath.Join(dir, ".chief", "box", ".gitignore"))
	if err != nil {
		t.Fatalf("no .gitignore beside the state: %v", err)
	}
	if !strings.Contains(string(ignore), "*") {
		t.Errorf(".gitignore does not ignore everything: %q", ignore)
	}

	info, err := os.Stat(filepath.Join(dir, ".chief", "box", "current.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("state file mode = %o, want 600", perm)
	}

	if err := ForgetState(dir); err != nil {
		t.Fatalf("ForgetState: %v", err)
	}
	if _, ok := LoadState(dir); ok {
		t.Error("the box was still found after being forgotten")
	}
	// Forgetting twice is what a failed `down` retried looks like.
	if err := ForgetState(dir); err != nil {
		t.Errorf("forgetting an absent box: %v", err)
	}
}

func TestLoadStateIgnoresRubbish(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".chief", "box"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".chief", "box", "current.json")
	for _, content := range []string{"not json", `{}`, `{"name":"x"}`} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := LoadState(dir); ok {
			t.Errorf("LoadState accepted %q", content)
		}
	}
}

func TestUpRefusesASecondBoxForTheSameProject(t *testing.T) {
	dir := t.TempDir()
	if err := SaveState(dir, State{ServerID: 1, Name: "chief-old", IP: "203.0.113.1", PRD: "auth", Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_, err := Up(context.Background(), UpOptions{PRD: "auth", BaseDir: dir})
	if err == nil {
		t.Fatal("expected a refusal — two boxes for one project is money quietly burning")
	}
	if !strings.Contains(err.Error(), "chief-old") || !strings.Contains(err.Error(), "down") {
		t.Errorf("error = %q, want it to name the box and how to get rid of it", err)
	}
}

func TestUpRefusesAPRDThatIsNotThere(t *testing.T) {
	dir := t.TempDir()
	_, err := Up(context.Background(), UpOptions{PRD: "ghost", BaseDir: dir})
	if err == nil {
		t.Fatal("expected an error for a PRD that does not exist")
	}
	if !strings.Contains(err.Error(), "chief new") {
		t.Errorf("error = %q, want it to say how to create one", err)
	}
}

func TestCommandsWithoutABoxSayHowToStartOne(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	for name, err := range map[string]error{
		"logs":   Logs(ctx, dir, os.Stderr),
		"status": Status(ctx, dir, os.Stderr),
		"ssh":    SSH(ctx, dir),
		"down":   Down(ctx, DownOptions{BaseDir: dir}),
	} {
		if err == nil {
			t.Errorf("%s: expected an error without a box", name)
			continue
		}
		if !strings.Contains(err.Error(), "chief box up") {
			t.Errorf("%s: error = %q, want it to say how to start one", name, err)
		}
	}
}

func TestProjectOriginNeedsARepoWithARemote(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := projectOrigin(dir); err == nil {
		t.Error("expected an error outside a git repository")
	}

	// A repository with no origin: the box has nothing to clone.
	for _, args := range [][]string{{"init"}, {"checkout", "-b", "main"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	_, _, err := projectOrigin(dir)
	if err == nil {
		t.Fatal("expected an error without an origin remote")
	}
	if !strings.Contains(err.Error(), "origin") {
		t.Errorf("error = %q, want it to name the missing remote", err)
	}
}

func TestShellQuoteSurvivesAwkwardNames(t *testing.T) {
	// A PRD name reaches a remote shell, so a quote in it must not end the
	// quoting and start a command.
	got := shellQuote("it's; rm -rf /")
	if strings.Contains(got, "; rm") && !strings.Contains(got, `'\''`) {
		t.Errorf("shellQuote(%q) = %q, which a shell would split", "it's; rm -rf /", got)
	}
	if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
		t.Errorf("shellQuote = %q, want it wrapped in single quotes", got)
	}
}

func TestIsChiefSourceRecognisesTheRealThing(t *testing.T) {
	dir := t.TempDir()
	if isChiefSource(dir) {
		t.Error("an empty directory passed as a chief checkout")
	}

	// A directory that merely happens to be a Go module must not be handed to
	// the compiler as though it were chief.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/other\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isChiefSource(dir) {
		t.Error("someone else's Go module passed as a chief checkout")
	}

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/ben182/chief\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isChiefSource(dir) {
		t.Error("a real chief checkout was rejected")
	}
}

func TestModuleRootHonoursAndValidatesCHIEFSRC(t *testing.T) {
	isolateConfig(t)
	ctx := context.Background()

	real := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, "go.mod"), []byte("module github.com/ben182/chief\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CHIEF_SRC", real)
	got, err := moduleRoot(ctx)
	if err != nil {
		t.Fatalf("moduleRoot: %v", err)
	}
	if got != real {
		t.Errorf("moduleRoot = %q, want %q", got, real)
	}

	// A CHIEF_SRC pointing somewhere wrong has to say so, rather than silently
	// falling through to a search and building a different checkout than the one
	// that was named.
	t.Setenv("CHIEF_SRC", t.TempDir())
	if _, err := moduleRoot(ctx); err == nil {
		t.Error("expected an error for a CHIEF_SRC that is not a chief checkout")
	}
}

func TestModuleRootRemembersWhatItFound(t *testing.T) {
	isolateConfig(t)
	ctx := context.Background()

	real := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, "go.mod"), []byte("module github.com/ben182/chief\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pretend a previous box worked this out. The search is the expensive,
	// guess-laden part; it should happen once per machine, not once per run.
	if err := writeToken("src-path", real); err != nil {
		t.Fatal(err)
	}
	got, err := moduleRoot(ctx)
	if err != nil {
		t.Fatalf("moduleRoot: %v", err)
	}
	if got != real {
		t.Errorf("moduleRoot = %q, want the remembered %q", got, real)
	}

	// A remembered path that has since been moved away must not win over a
	// fresh search.
	if err := writeToken("src-path", filepath.Join(t.TempDir(), "gone")); err != nil {
		t.Fatal(err)
	}
	if got, _ := moduleRoot(ctx); got == filepath.Join(t.TempDir(), "gone") {
		t.Error("a stale remembered path was used")
	}
}

func TestModuleRootSaysHowToFixItWhenLost(t *testing.T) {
	home := isolateConfig(t)
	// No checkout anywhere the search looks, and the working directory is not a
	// Go module — which is the normal case: `chief box` is run from the project
	// being built, not from chief.
	t.Chdir(home)

	_, err := moduleRoot(context.Background())
	if err == nil {
		t.Fatal("expected an error with no checkout to find")
	}
	if !strings.Contains(err.Error(), "CHIEF_SRC") {
		t.Errorf("error = %q, want it to name the variable that fixes this", err)
	}
}

func TestBuildForLinuxProducesALinuxBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiling chief takes a few seconds")
	}
	// The real thing: this is what gets uploaded, and a binary that will not run
	// on the box is a failure discovered after an instance has been paid for.
	path, err := buildForLinux(context.Background())
	if err != nil {
		t.Fatalf("buildForLinux: %v", err)
	}
	defer func() { _ = os.Remove(path) }()

	header := make([]byte, 20)
	f, err := os.Open(path) //nolint:gosec // a file this test just created
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Read(header); err != nil {
		t.Fatal(err)
	}
	// ELF magic, then the machine type at offset 18: 0x3e is x86-64. A macOS
	// build would be Mach-O and start differently.
	if string(header[:4]) != "\x7fELF" {
		t.Fatalf("not an ELF binary: % x", header[:4])
	}
	if header[18] != 0x3e {
		t.Errorf("machine type = %#x, want 0x3e (x86-64)", header[18])
	}
}
