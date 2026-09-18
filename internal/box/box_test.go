package box

import (
	"context"
	"fmt"
	"io"
	"net/http"
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

// laravelProfile is what discovery reads out of a Laravel app like the one the
// first real box was built for: pinned PHP, an extension no base list carries,
// Postgres, Redis, Bun, and a browser-driven test suite.
func laravelProfile() Profile {
	return Profile{
		Stack: StackLaravel, Framework: "Laravel 13",
		PHP: "8.3", PHPFrom: "Herd, isolated for demo.test",
		Extensions: []string{"curl", "imagick", "pgsql", "redis"},
		Database:   "pgsql", DatabaseName: "agency_os", Redis: true,
		Node: "24", NodeFrom: "the node on this machine", PackageManager: "bun",
		Browser: true,
	}
}

// parseCloudInit renders a cloud-config and returns its packages and runcmd
// joined into searchable strings, failing the test if it is not valid YAML.
func parseCloudInit(t *testing.T, opts cloudInitOptions) (packages, runcmd, files string) {
	t.Helper()
	out := cloudInit(opts)
	var parsed struct {
		Packages   []string `yaml:"packages"`
		Runcmd     []string `yaml:"runcmd"`
		WriteFiles []struct {
			Path    string `yaml:"path"`
			Content string `yaml:"content"`
		} `yaml:"write_files"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("not valid YAML: %v\n%s", err, out)
	}
	var f strings.Builder
	for _, w := range parsed.WriteFiles {
		f.WriteString(w.Path + "\n" + w.Content + "\n")
	}
	return strings.Join(parsed.Packages, " "), strings.Join(parsed.Runcmd, "\n"), f.String()
}

func TestCloudInitInstallsWhatTheProfileAsksFor(t *testing.T) {
	packages, runcmd, _ := parseCloudInit(t, cloudInitOptions{Hostname: "h", Profile: laravelProfile()})

	// PHP is the project's series, from the PPA that has every series, and the
	// extensions are the ones discovery found rather than a fixed list.
	if !strings.Contains(runcmd, "ppa:ondrej/php") {
		t.Error("the PHP PPA is never added")
	}
	for _, want := range []string{"php8.3-cli", "php8.3-imagick", "php8.3-pgsql", "php8.3-redis"} {
		if !strings.Contains(runcmd, want) {
			t.Errorf("%s is not installed:\n%s", want, runcmd)
		}
	}
	if strings.Contains(runcmd, "php"+phpSeries+"-") && phpSeries != "8.3" {
		t.Errorf("the default PHP series is installed next to the project's:\n%s", runcmd)
	}
	if !strings.Contains(packages, "postgresql") {
		t.Error("PostgreSQL is not installed")
	}
	if !strings.Contains(runcmd, "createdb -O chief agency_os") {
		t.Error("the project's database is not created")
	}
	if !strings.Contains(packages, "redis-server") {
		t.Error("Redis is not installed")
	}
	if !strings.Contains(runcmd, "setup_24.x") {
		t.Error("Node 24 is not installed")
	}
	if !strings.Contains(runcmd, "/usr/local/bin/bun") || !strings.Contains(runcmd, "test -x /usr/local/bin/bun") {
		t.Error("Bun is not installed, or not checked")
	}
	if !strings.Contains(runcmd, "playwright install-deps") {
		t.Error("the browser's system libraries are not installed")
	}
	// Not asked for, not installed.
	for _, unwanted := range []string{"mariadb", "google-chrome", "go.dev/dl", "meilisearch"} {
		if strings.Contains(packages+runcmd, unwanted) {
			t.Errorf("%s is installed though the profile never asked for it", unwanted)
		}
	}
}

func TestCloudInitBuildsAMySQLBox(t *testing.T) {
	p := Profile{Stack: StackLaravel, PHP: "8.2", Extensions: []string{"mysql"}, Database: "mysql", DatabaseName: "shop"}
	packages, runcmd, files := parseCloudInit(t, cloudInitOptions{Hostname: "h", Profile: p})

	if !strings.Contains(packages, "mariadb-server") {
		t.Error("MariaDB is not installed")
	}
	if strings.Contains(packages, "postgresql") {
		t.Error("PostgreSQL is installed for a MySQL project")
	}
	if !strings.Contains(runcmd, "php8.2-mysql") {
		t.Error("the MySQL driver is not installed")
	}
	// The account and the database come from a file, because a line of SQL
	// with quotes and backticks in it is not a thing to hand to a YAML list.
	if !strings.Contains(runcmd, "mariadb < /etc/chief/mysql-setup.sql") {
		t.Error("the MySQL setup is never run")
	}
	if !strings.Contains(files, "CREATE DATABASE IF NOT EXISTS `shop`") || !strings.Contains(files, "'chief'@'127.0.0.1'") {
		t.Errorf("the SQL does not create the database and the account:\n%s", files)
	}
}

func TestCloudInitLeavesOutWhatTheProjectDoesNotUse(t *testing.T) {
	// A Go module gets its toolchain and nothing PHP.
	p := Profile{Stack: StackGo, Go: "1.27.1"}
	packages, runcmd, _ := parseCloudInit(t, cloudInitOptions{Hostname: "h", Profile: p})
	if !strings.Contains(runcmd, "go.dev/dl/go1.27.1.linux-amd64.tar.gz") {
		t.Error("Go is not installed")
	}
	for _, unwanted := range []string{"php", "composer", "postgresql", "redis", "nodesource"} {
		if strings.Contains(packages+runcmd, unwanted) {
			t.Errorf("%s is installed for a Go project", unwanted)
		}
	}
	// Even with nothing recognised, the agent and the tooling around it arrive.
	_, runcmd, _ = parseCloudInit(t, cloudInitOptions{Hostname: "h"})
	if !strings.Contains(runcmd, "apt-get install -y claude-code gh") {
		t.Error("the base is not installed on a project chief does not recognise")
	}
}

func TestCloudInitInstallsBrowsersAndSearch(t *testing.T) {
	p := Profile{Stack: StackLaravel, PHP: "8.4", Node: "22", PackageManager: "pnpm", PackageManagerVersion: "9.1.0",
		Browser: true, Chrome: true, Meilisearch: true}
	_, runcmd, files := parseCloudInit(t, cloudInitOptions{Hostname: "h", Profile: p})

	if !strings.Contains(runcmd, "google-chrome-stable_current_amd64.deb") {
		t.Error("Dusk's Chrome is not installed")
	}
	if !strings.Contains(runcmd, "corepack prepare pnpm@9.1.0 --activate") {
		t.Error("the pinned pnpm is not installed")
	}
	if !strings.Contains(runcmd, "install.meilisearch.com") || !strings.Contains(runcmd, "systemctl enable --now meilisearch") {
		t.Error("Meilisearch is not installed and started")
	}
	if !strings.Contains(files, "/etc/systemd/system/meilisearch.service") || !strings.Contains(files, "--http-addr 127.0.0.1:7700") {
		t.Error("Meilisearch has no unit bound to the box itself")
	}
}

func TestCloudInitInstallsClaudeCodeFromItsRepository(t *testing.T) {
	_, runcmd, _ := parseCloudInit(t, cloudInitOptions{Hostname: "h", Profile: laravelProfile()})

	// Claude Code comes from the signed apt repository. npm and the curl
	// installer both work, but unattended provisioning should not pipe a
	// downloaded script into a shell when a signed package is on offer.
	if strings.Contains(runcmd, "npm install -g @anthropic-ai/claude-code") {
		t.Error("Claude Code is still installed through npm")
	}
	if !strings.Contains(runcmd, "downloads.claude.ai/claude-code/apt") {
		t.Error("Claude Code is not installed from the apt repository")
	}
	if !strings.Contains(runcmd, "claude-code") {
		t.Error("the claude-code package is never installed")
	}
	// The key is verified before it is trusted, and a mismatch has to stop the
	// provisioning rather than be a line in a log nobody reads.
	if !strings.Contains(runcmd, "31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE") {
		t.Error("the Claude Code signing key's fingerprint is never checked")
	}
}

func TestCloudInitStopsAtTheFirstFailure(t *testing.T) {
	_, runcmd, _ := parseCloudInit(t, cloudInitOptions{Hostname: "h", Profile: laravelProfile()})
	lines := strings.Split(runcmd, "\n")

	// cloud-init runs runcmd as one shell script and carries on past a failing
	// line. The first real box proved it: Bun's download got a 504, the check
	// after it failed too, and the ready marker was written regardless.
	setE := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "set -e" {
			setE = i
			break
		}
	}
	if setE < 0 {
		t.Fatal("runcmd never turns on set -e; a failed step would not stop provisioning")
	}
	if setE > 1 {
		t.Errorf("set -e is line %d of runcmd; everything before it can fail unnoticed", setE)
	}
	// And a failure has to be visible to chief, which is waiting on the other
	// side of the network for a file: a second marker, written on the way out
	// when the first was never reached.
	if !strings.Contains(runcmd, "trap 'test -f "+readyMarker+" || touch "+failedMarker+"' EXIT") {
		t.Error("no trap leaves the failed marker; chief would wait for the timeout")
	}
	// Downloads of binaries retry, because a CDN's 504 is not a final answer.
	if !strings.Contains(runcmd, "bun-linux-x64.zip") || !strings.Contains(runcmd, "--retry-all-errors") {
		t.Error("Bun is not fetched from its release zip with retries")
	}
	if strings.Contains(runcmd, "bun.sh/install") {
		t.Error("Bun is still installed through the install script, which does not retry")
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
	// Every box run keeps its log in the branch and pushes what it built,
	// whatever else was asked for: the journal dies with the machine, and so
	// does a commit that was never pushed.
	if got := runFlags(UpOptions{}); got != "--log-to-branch --push" {
		t.Errorf("runFlags with nothing set = %q, want the log kept and the branch pushed", got)
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
	stillThere(t)
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

// stillThere makes the recorded box exist for the duration of a test, so a
// check that would otherwise ask Hetzner about it does not.
func stillThere(t *testing.T) {
	t.Helper()
	previous := vanishedCheck
	vanishedCheck = func(context.Context, State) bool { return false }
	t.Cleanup(func() { vanishedCheck = previous })
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
		"ssh":    SSH(ctx, dir, "", io.Discard),
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

// pipeWith returns a readable file carrying input, standing in for a terminal
// that is not there.
func pipeWith(t *testing.T, input string) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = w.WriteString(input)
		_ = w.Close()
	}()
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// fakeDial builds the injected client factory pointing at a fake API.
func fakeDial(f *fakeHetzner) func(string) *hetzner {
	return func(token string) *hetzner {
		h := newHetzner(token)
		h.base = f.URL
		return h
	}
}

func TestSetUpHetznerRejectsAnEmptyToken(t *testing.T) {
	isolateConfig(t)
	f := newFakeHetzner(t, map[string]any{"GET /ssh_keys": `{"ssh_keys":[]}`})

	err := setUpHetzner(context.Background(), pipeWith(t, "\n"), io.Discard, fakeDial(f))
	if err == nil {
		t.Fatal("expected an error for an empty token")
	}
	if got := readToken("hetzner-token"); got != "" {
		t.Errorf("an empty token was saved as %q", got)
	}
}

func TestSetUpHetznerDoesNotSaveATokenTheAPIRejects(t *testing.T) {
	isolateConfig(t)
	f := newFakeHetzner(t, map[string]any{"GET /ssh_keys": http.StatusUnauthorized})

	// The check has to happen before the save, or a typo is discovered later as
	// a confusing failure in the middle of creating a box.
	err := setUpHetzner(context.Background(), pipeWith(t, "wrong-token\n"), io.Discard, fakeDial(f))
	if err == nil {
		t.Fatal("expected an error for a rejected token")
	}
	if got := readToken("hetzner-token"); got != "" {
		t.Errorf("a rejected token was saved as %q", got)
	}
}

func TestSetUpHetznerSavesAGoodTokenPrivately(t *testing.T) {
	isolateConfig(t)
	f := newFakeHetzner(t, map[string]any{"GET /ssh_keys": `{"ssh_keys":[]}`})

	// Trailing whitespace is what a paste brings along; it must not become part
	// of the token.
	if err := setUpHetzner(context.Background(), pipeWith(t, "  good-token  \n"), io.Discard, fakeDial(f)); err != nil {
		t.Fatalf("setUpHetzner: %v", err)
	}
	if got := readToken("hetzner-token"); got != "good-token" {
		t.Errorf("saved %q, want the trimmed token", got)
	}

	path, _ := tokenFile("hetzner-token")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %o, want 600", perm)
	}
}

func TestSetUpHetznerDoesNotAskAgainForAWorkingToken(t *testing.T) {
	isolateConfig(t)
	f := newFakeHetzner(t, map[string]any{"GET /ssh_keys": `{"ssh_keys":[]}`})
	if err := writeToken("hetzner-token", "already-good"); err != nil {
		t.Fatal(err)
	}

	// Nothing on the input: if this reaches the prompt at all, the read returns
	// empty and the call fails — which is the assertion.
	var out strings.Builder
	if err := setUpHetzner(context.Background(), pipeWith(t, ""), &out, fakeDial(f)); err != nil {
		t.Fatalf("a stored, working token was not accepted: %v", err)
	}
	if !strings.Contains(out.String(), "already stored") {
		t.Errorf("output does not say the token was already there:\n%s", out.String())
	}
}

func TestSetUpHetznerReplacesATokenThatStoppedWorking(t *testing.T) {
	isolateConfig(t)
	// A token revoked in the console looks identical to a good one until a box
	// fails to appear, so the stored one is checked rather than trusted.
	calls := 0
	f := newFakeHetzner(t, map[string]any{})
	dial := func(token string) *hetzner {
		calls++
		h := newHetzner(token)
		if token == "revoked" {
			h.base = f.URL // the fake answers 404 for unknown routes
			return h
		}
		good := newFakeHetzner(t, map[string]any{"GET /ssh_keys": `{"ssh_keys":[]}`})
		h.base = good.URL
		return h
	}
	if err := writeToken("hetzner-token", "revoked"); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := setUpHetzner(context.Background(), pipeWith(t, "fresh-token\n"), &out, dial); err != nil {
		t.Fatalf("setUpHetzner: %v", err)
	}
	if got := readToken("hetzner-token"); got != "fresh-token" {
		t.Errorf("stored %q, want the replacement", got)
	}
	if !strings.Contains(out.String(), "rejected") {
		t.Errorf("output does not explain why it asked again:\n%s", out.String())
	}
}

func TestLoginReportsEveryMissingCredential(t *testing.T) {
	isolateConfig(t)
	f := newFakeHetzner(t, map[string]any{"GET /ssh_keys": `{"ssh_keys":[]}`})

	var out strings.Builder
	err := loginWith(context.Background(), pipeWith(t, "good-token\n"), &out, fakeDial(f))
	// Hetzner succeeds; GitHub and Claude cannot, because the isolated PATH has
	// neither gh nor claude on it.
	if err == nil {
		t.Fatal("expected an error while credentials are still missing")
	}
	text := out.String()
	// All three are named, so somebody reading this knows what is left.
	for _, want := range []string{"Hetzner", "GitHub", "Claude"} {
		if !strings.Contains(text, want) {
			t.Errorf("output never mentions %s:\n%s", want, text)
		}
	}
	// The one that worked is not reported as a problem.
	if !strings.Contains(text, "saved to") {
		t.Errorf("the Hetzner token was not reported as saved:\n%s", text)
	}
}

func TestCloudInitGivesTheUserTheirOwnHome(t *testing.T) {
	out := cloudInit(cloudInitOptions{Hostname: "h"})

	// write_files runs before users exist. Without `defer`, cloud-init creates
	// /home/chief as root to hold the env file, useradd then leaves the existing
	// directory alone, and the user is locked out of their own home — a box that
	// provisions perfectly and then fails every write with "Permission denied".
	var parsed struct {
		WriteFiles []struct {
			Path  string `yaml:"path"`
			Owner string `yaml:"owner"`
			Defer bool   `yaml:"defer"`
		} `yaml:"write_files"`
		Runcmd []string `yaml:"runcmd"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}

	for _, f := range parsed.WriteFiles {
		if f.Owner != "" && !strings.HasPrefix(f.Owner, "root") && !f.Defer {
			t.Errorf("%s is owned by %q but not deferred; it will be written before that user exists", f.Path, f.Owner)
		}
	}

	// And the belt to that pair of braces: whatever else happened, the home
	// belongs to its user before anything is written into it.
	var chownIndex = -1
	for i, cmd := range parsed.Runcmd {
		if strings.Contains(cmd, "chown chief:chief /home/chief") && !strings.Contains(cmd, ".ssh") {
			chownIndex = i
			break
		}
	}
	if chownIndex != 0 {
		t.Errorf("the home is chowned at runcmd step %d, want it first", chownIndex)
	}
}

func TestCloudInitInstallsComposerWithAHome(t *testing.T) {
	out := cloudInit(cloudInitOptions{Hostname: "h", Profile: laravelProfile()})
	var parsed struct {
		Runcmd []string `yaml:"runcmd"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatal(err)
	}
	runcmd := strings.Join(parsed.Runcmd, "\n")

	// The installer refuses to do anything without HOME ("The HOME or
	// COMPOSER_HOME environment variable must be set") — and reports that on
	// stdout while still exiting 0, so the failure is silent and composer is
	// simply absent hours later when a project's setup needs it.
	if !strings.Contains(runcmd, "HOME=/root php /tmp/composer-setup.php") {
		t.Error("the Composer installer runs without HOME; it will silently do nothing")
	}
	// Which is why its result is checked rather than assumed.
	if !strings.Contains(runcmd, "test -x /usr/local/bin/composer") {
		t.Error("nothing verifies that composer actually landed")
	}
}

func TestCloneScriptExportsTheCredentialsItSources(t *testing.T) {
	script := cloneScript("https://github.com/x/y.git", "main")

	// Sourcing alone makes these shell variables, not environment ones. The
	// credential helper runs as a child of git and would see no password at all,
	// which GitHub reports as "Invalid username or token" — an error that points
	// at the token rather than at the shell, and sends you looking in the wrong
	// place entirely.
	if !strings.Contains(script, "set -a") {
		t.Error("the script sources the env file without exporting it; git's credential helper will see nothing")
	}
	// And the file itself cannot carry `export`: systemd reads it as an
	// EnvironmentFile, where every line has to be a bare KEY=VALUE.
	if strings.Contains(script, "export GH_TOKEN") {
		t.Error("the script expects an exported token in the file, which systemd's EnvironmentFile cannot carry")
	}
	// set -a stays on only as long as it is needed.
	if !strings.Contains(script, "set +a") {
		t.Error("automatic export is never turned off again")
	}
	if strings.Index(script, "set -a") > strings.Index(script, ". ~/.chief-env") {
		t.Error("export is switched on after the file is sourced, which is too late")
	}
}

// git runs a git command in dir and fails the test when it does not work. The
// tests below build real repositories, because what is being checked is what
// git actually answers rather than what chief believes it answers.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// clonedProject builds a bare "origin" with one commit on main, and a clone of
// it, which is the shape every box works in.
func clonedProject(t *testing.T) (origin, work string) {
	t.Helper()
	root := t.TempDir()
	origin = filepath.Join(root, "origin.git")
	work = filepath.Join(root, "work")
	gitIn(t, root, "init", "--quiet", "--bare", "--initial-branch=main", origin)
	gitIn(t, root, "clone", "--quiet", origin, work)
	if err := os.WriteFile(filepath.Join(work, "README"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work, "add", "README")
	gitIn(t, work, "commit", "--quiet", "-m", "first")
	gitIn(t, work, "push", "--quiet", "-u", "origin", "main")
	return origin, work
}

// TestUnpushedProbeSeesCommitsThatExistNowhereElse is the test the old probe
// would have failed. It ran `git log @{upstream}..HEAD`, which on a branch with
// no upstream — every branch a box creates — fails, prints nothing, and is
// counted as zero: the question "is there work here that only exists on this
// machine" answered "no" precisely when the answer was "yes".
func TestUnpushedProbeSeesCommitsThatExistNowhereElse(t *testing.T) {
	_, work := clonedProject(t)

	// The probe runs on the box, where the project sits at a fixed path. Here it
	// runs against a real repository in the same shape.
	probe := strings.Replace(unpushedProbe, "cd "+remoteProject, "cd "+work, 1)
	count := func() string {
		cmd := exec.Command("sh", "-c", probe)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		return strings.TrimSpace(string(out))
	}

	if got := count(); got != "0" {
		t.Errorf("a fresh clone reports %s unpushed commits, want 0", got)
	}

	// What a run does: a branch of its own, never pushed.
	gitIn(t, work, "checkout", "--quiet", "-b", "chief/feature")
	if err := os.WriteFile(filepath.Join(work, "story"), []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work, "add", "story")
	gitIn(t, work, "commit", "--quiet", "-m", "story one")
	if got := count(); got != "1" {
		t.Errorf("a branch with no upstream reports %s unpushed commits, want 1", got)
	}

	// And what a --worktree run does: the commits are not on the checkout's own
	// HEAD at all, which the old probe could not have seen either.
	wt := filepath.Join(t.TempDir(), "wt")
	gitIn(t, work, "worktree", "add", "--quiet", "-b", "chief/second", wt)
	if err := os.WriteFile(filepath.Join(wt, "more"), []byte("more\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wt, "add", "more")
	gitIn(t, wt, "commit", "--quiet", "-m", "story two")
	if got := count(); got != "2" {
		t.Errorf("with a worktree commit the probe reports %s, want 2", got)
	}

	// Pushed work exists elsewhere and must stop counting, or the question is
	// asked on every destroy and stops being read.
	gitIn(t, work, "push", "--quiet", "origin", "chief/feature")
	gitIn(t, work, "push", "--quiet", "origin", "chief/second")
	if got := count(); got != "0" {
		t.Errorf("after pushing, the probe still reports %s unpushed commits", got)
	}
}

func TestBranchIsOnOriginRefusesABranchTheBoxCouldNotFind(t *testing.T) {
	_, work := clonedProject(t)

	if err := branchIsOnOrigin(work, "main"); err != nil {
		t.Errorf("a pushed branch was refused: %v", err)
	}

	gitIn(t, work, "checkout", "--quiet", "-b", "local-only")
	err := branchIsOnOrigin(work, "local-only")
	if err == nil {
		t.Fatal("a branch that was never pushed was accepted — the box would have run on main")
	}
	for _, want := range []string{"local-only", "git push"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

func TestBranchIsOnOriginRefusesCommitsOriginHasNotSeen(t *testing.T) {
	_, work := clonedProject(t)

	if err := os.WriteFile(filepath.Join(work, "later"), []byte("later\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work, "add", "later")
	gitIn(t, work, "commit", "--quiet", "-m", "not pushed yet")

	err := branchIsOnOrigin(work, "main")
	if err == nil {
		t.Fatal("a branch ahead of origin was accepted — the run would have started without that commit")
	}
	if !strings.Contains(err.Error(), "1 commit") {
		t.Errorf("error = %q, want it to count what origin is missing", err)
	}

	gitIn(t, work, "push", "--quiet", "origin", "main")
	if err := branchIsOnOrigin(work, "main"); err != nil {
		t.Errorf("after pushing, the check still refuses: %v", err)
	}
}

func TestBranchIsOnOriginStaysOutOfTheWayWhenItCannotAsk(t *testing.T) {
	// A detached HEAD has no branch to look for, and a repository with no origin
	// at all is a case projectOrigin already refuses with a better message. In
	// neither does this check get to be the one that blocks a run.
	_, work := clonedProject(t)
	if err := branchIsOnOrigin(work, "HEAD"); err != nil {
		t.Errorf("a detached HEAD was refused: %v", err)
	}
	if err := branchIsOnOrigin(t.TempDir(), "main"); err != nil {
		t.Errorf("an unreachable origin blocked the run: %v", err)
	}
}

func TestProvisionTimeoutGrowsWithWhatIsInstalled(t *testing.T) {
	bare := provisionTimeout(Profile{})
	laravel := provisionTimeout(Profile{
		PHP: "8.4", Node: "24", PackageManager: "bun",
		Database: "pgsql", Redis: true, Browser: true, Chrome: true,
	})
	if laravel <= bare {
		t.Errorf("a Laravel box gets %s and a bare one %s — the wait does not follow the work", laravel, bare)
	}
	if bare < 5*time.Minute {
		t.Errorf("even a bare box needs apt: %s is not a budget", bare)
	}
	if laravel > maxProvision {
		t.Errorf("the budget ran past its ceiling: %s > %s", laravel, maxProvision)
	}
	// Everything at once must still be capped, or a profile that grows a few
	// more entries silently becomes an hour of waiting for a stuck box.
	everything := provisionTimeout(Profile{
		PHP: "8.4", Node: "24", PackageManager: "pnpm", Go: "1.27.0",
		Database: "mysql", Redis: true, Meilisearch: true, Browser: true, Chrome: true,
	})
	if everything != maxProvision {
		t.Errorf("the fullest profile gets %s, want it capped at %s", everything, maxProvision)
	}
}

func TestOutcomeSaysHowTheRunEnded(t *testing.T) {
	cases := []struct {
		name    string
		outcome Outcome
		done    bool
		says    string
	}{
		{"finished with everything resolved", Outcome{Result: "success"}, true, "done"},
		// chief's headless mode exits 1 when it ends with stories unresolved, and
		// systemd reports that as exit-code. Calling it "done" would be a lie told
		// to somebody who is about to stop looking.
		{"stories left over", Outcome{Result: "exit-code", Status: 1}, false, "work left"},
		{"killed", Outcome{Result: "signal"}, false, "killed"},
		{"timed out", Outcome{Result: "timeout"}, false, "timed out"},
		{"something else", Outcome{Result: "start-limit-hit"}, false, "start-limit-hit"},
		// A box that stopped answering has not told us anything, and the one thing
		// the notification must never do is claim success on its behalf.
		{"the box did not answer", Outcome{}, false, "did not say"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.outcome.Completed(); got != c.done {
				t.Errorf("Completed() = %v, want %v", got, c.done)
			}
			if got := c.outcome.Describe(); !strings.Contains(got, c.says) {
				t.Errorf("Describe() = %q, want it to say %q", got, c.says)
			}
		})
	}

	// A success that somehow carries a non-zero status is not a success.
	odd := Outcome{Result: "success", Status: 2}
	if odd.Completed() {
		t.Error("an exit status of 2 was reported as a completed run")
	}
}

func TestASelfDestructingBoxIsBuiltWithItsOwnShutoff(t *testing.T) {
	cfg := cloudInit(cloudInitOptions{Hostname: "chief-shop-auth", SelfDestruct: true, MaxHours: 6})
	for _, want := range []string{
		"/usr/local/bin/chief-reap",
		"chief-reap.timer",
		"OnSuccess=chief-reap.timer",
		"chief-deadline.timer",
		"OnBootSec=6h",
		"systemctl enable --now chief-deadline.timer",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("cloud-config is missing %q", want)
		}
	}
	// The token is not in here, and must never be: a cloud-config is readable
	// from the instance's own metadata service by anything that can make an
	// HTTP request, no password required.
	if strings.Contains(cfg, "CHIEF_HETZNER_TOKEN=") {
		t.Error("the cloud-config carries the Hetzner token — it has to arrive over SSH")
	}
}

func TestAKeptBoxHasNoShutoffAtAll(t *testing.T) {
	cfg := cloudInit(cloudInitOptions{Hostname: "chief-shop-auth"})
	for _, unwanted := range []string{"chief-reap", "chief-deadline", "OnSuccess="} {
		if strings.Contains(cfg, unwanted) {
			t.Errorf("a box that was asked to stay still renders %q", unwanted)
		}
	}
}

func TestTheDefaultOutsideLimitIsRenderedWhenNoneWasAsked(t *testing.T) {
	cfg := cloudInit(cloudInitOptions{Hostname: "chief-shop-auth", SelfDestruct: true})
	if !strings.Contains(cfg, fmt.Sprintf("OnBootSec=%dh", DefaultMaxHours)) {
		t.Errorf("cloud-config does not carry the default limit of %dh", DefaultMaxHours)
	}
}

func TestTheReaperRefusesToDestroyWorkThatIsOnlyOnTheBox(t *testing.T) {
	cfg := cloudInit(cloudInitOptions{Hostname: "chief-shop-auth", SelfDestruct: true})
	script, _, found := strings.Cut(cfg, "chief-reap.service")
	if !found {
		t.Fatal("no reaper in the cloud-config")
	}
	// The order is the whole safety property: push what is here, count what is
	// still only here, and only then call the API.
	push := strings.Index(script, "git push")
	count := strings.Index(script, "rev-list --count --branches --not --remotes")
	del := strings.Index(script, "-X DELETE")
	if push < 0 || count < 0 || del < 0 || !(push < count && count < del) {
		t.Errorf("the reaper does not push, then count, then destroy (%d, %d, %d)", push, count, del)
	}
}

func TestReapEnvIsOneKeyPerLine(t *testing.T) {
	got := reapEnv("abc123", 42)
	if got != "CHIEF_HETZNER_TOKEN=abc123\nCHIEF_SERVER_ID=42\n" {
		t.Errorf("reapEnv = %q", got)
	}
}

func TestPreflightForgetsABoxThatDestroyedItself(t *testing.T) {
	dir := t.TempDir()
	if err := SaveState(dir, State{ServerID: 7, Name: "chief-gone", IP: "203.0.113.9", PRD: "auth", Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	previous := vanishedCheck
	vanishedCheck = func(context.Context, State) bool { return true }
	t.Cleanup(func() { vanishedCheck = previous })

	// It gets past the "you already have a box" check and fails on the project
	// instead, which is the next thing wrong with this temp directory.
	err := Preflight(context.Background(), UpOptions{PRD: "auth", BaseDir: dir})
	if err == nil || strings.Contains(err.Error(), "chief-gone") {
		t.Errorf("error = %v, want the record of the vanished box to be gone", err)
	}
	if _, ok := LoadState(dir); ok {
		t.Error("the record of a box that no longer exists was kept")
	}
}

func TestFollowResumesWhereItLeftOff(t *testing.T) {
	got := followScript("chief-run@'auth'", "/tmp/chief-follow-1-2.cursor")
	// Without the cursor file a reconnect either repeats what was already read
	// or skips what arrived while the connection was down.
	if !strings.Contains(got, "--cursor-file=/tmp/chief-follow-1-2.cursor") {
		t.Errorf("followScript = %q, want it to carry a cursor file", got)
	}
	if !strings.Contains(got, "-f") || !strings.Contains(got, "chief-run@'auth'") {
		t.Errorf("followScript = %q, want it to follow the run's unit", got)
	}
}

func TestASelfDestructingCloudConfigIsStillValidYAML(t *testing.T) {
	out := cloudInit(cloudInitOptions{Hostname: "chief-demo-auth", SelfDestruct: true, Profile: Profile{PHP: "8.3", Database: "mysql"}})
	var parsed struct {
		WriteFiles []struct {
			Path        string `yaml:"path"`
			Content     string `yaml:"content"`
			Permissions string `yaml:"permissions"`
		} `yaml:"write_files"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("not valid YAML: %v", err)
	}
	// A block scalar that loses its indentation does not fail loudly — it
	// produces a script that is silently half a script.
	var script string
	for _, f := range parsed.WriteFiles {
		if f.Path == "/usr/local/bin/chief-reap" {
			script = f.Content
			if f.Permissions != "0755" {
				t.Errorf("the reaper is written %s, and systemd has to be able to run it", f.Permissions)
			}
		}
	}
	if script == "" {
		t.Fatal("no reaper script in the cloud-config")
	}
	if !strings.HasPrefix(script, "#!/bin/sh\n") {
		t.Errorf("the reaper does not start with a shebang:\n%s", firstLines(script, 3))
	}
	if !strings.Contains(script, "api.hetzner.cloud/v1/servers/$CHIEF_SERVER_ID") {
		t.Error("the reaper does not name the server it is meant to destroy")
	}
}

func TestTheReaperScriptParsesAsAShellScript(t *testing.T) {
	// The script runs at three in the morning on a machine nobody is watching,
	// and a syntax error there looks exactly like a box that decided to keep
	// billing. `sh -n` is the cheapest way to find out here instead.
	out := cloudInit(cloudInitOptions{Hostname: "chief-demo-auth", SelfDestruct: true})
	var parsed struct {
		WriteFiles []struct {
			Path    string `yaml:"path"`
			Content string `yaml:"content"`
		} `yaml:"write_files"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("not valid YAML: %v", err)
	}
	var script string
	for _, f := range parsed.WriteFiles {
		if f.Path == "/usr/local/bin/chief-reap" {
			script = f.Content
		}
	}
	if script == "" {
		t.Fatal("no reaper script in the cloud-config")
	}
	path := filepath.Join(t.TempDir(), "chief-reap")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", "-n", path).CombinedOutput(); err != nil {
		t.Errorf("the reaper is not valid sh: %v\n%s", err, out)
	}
}
