// Package box runs a PRD on a throwaway cloud instance.
//
// A chief run takes hours, and for those hours it owns the machine it runs on:
// the CPU, the rate limit window, and the laptop that has to stay open. This
// package moves the run onto a machine created for it and destroyed afterwards.
// At current Hetzner prices a five-hour run costs about five cents of computer,
// which is two orders of magnitude less than the tokens it spends.
//
// The sequence is: create the instance from a generated cloud-config, upload the
// chief binary this process is running, clone the project, copy over the files
// git does not carry (the PRD itself, the .env), and start the run as a systemd
// unit. From that point the run owes nothing to the connection that started it.
package box

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/prd"
)

// Defaults for a box. Each can be overridden per run; these are the answers
// that suit a chief run rather than a server — a German region, and enough
// cores that a test suite is not the slow part.
const (
	// DefaultType is 4 vCPU and 8 GB for about a cent an hour — enough that a
	// test suite is not the slow part, on the cheaper of the two CPU lines.
	//
	// Server types are generational: a line that is current today stops being
	// bookable in a location when its successor arrives, while still appearing
	// in the price list. If creating a box starts failing with "unsupported
	// location", this is the constant to raise — the error says which types the
	// location actually offers.
	DefaultType = "cx33"
	// DefaultLocation is Falkenstein.
	DefaultLocation = "fsn1"
)

// remoteUser is the unprivileged account a run happens under, and remoteProject
// is where the clone lands. Both are fixed by the cloud-config.
const (
	remoteUser    = "chief"
	remoteProject = "/home/chief/project"
	readyMarker   = "/var/lib/cloud/chief-ready"
)

// How long each phase is given. Provisioning is apt working through a few
// hundred megabytes and is by far the slowest.
const (
	bootTimeout      = 3 * time.Minute
	provisionTimeout = 20 * time.Minute
)

// UpOptions describes the box to create and the run to start on it.
type UpOptions struct {
	// PRD is the name of the PRD to run. Its directory is copied to the box.
	PRD string
	// BaseDir is the project root: the git repository that gets cloned.
	BaseDir string
	// Type, Image and Location are the Hetzner instance. Empty values take the
	// defaults above.
	Type, Image, Location string
	// Worktree runs the PRD in its own worktree on the box, which is what a
	// project with a `worktree.setup` it depends on wants.
	Worktree bool
	// MaxIterations caps the run, zero letting chief size it from the PRD.
	MaxIterations int
	// Verbose puts the agent's narration and tool calls in the box's log.
	Verbose bool
	// ExtraFiles are paths, relative to the project, that git does not carry but
	// the run needs. Empty takes ".env".
	ExtraFiles []string
	// ExtraPackages are apt packages the project needs on top of the base.
	ExtraPackages []string
	// Secrets are the tokens, already resolved.
	Secrets Secrets
	// Out is where progress is reported.
	Out io.Writer
}

// reporter writes the running commentary. Creating a box takes several minutes,
// most of it waiting, and silence for that long is indistinguishable from a
// hang.
type reporter struct{ out io.Writer }

func (r reporter) step(format string, args ...any) {
	if r.out != nil {
		_, _ = fmt.Fprintf(r.out, "==> "+format+"\n", args...)
	}
}

func (r reporter) detail(format string, args ...any) {
	if r.out != nil {
		_, _ = fmt.Fprintf(r.out, "    "+format+"\n", args...)
	}
}

// Preflight checks everything about this machine and this project that can be
// checked without credentials and without creating anything.
//
// It is separate from Up, and called before the secrets are resolved, because
// resolving them can open a browser: sending someone through a login only to
// tell them afterwards that their project has no origin remote is a bad way to
// spend their attention. Everything here fails in milliseconds.
func Preflight(opts UpOptions) error {
	if existing, ok := LoadState(opts.BaseDir); ok {
		return fmt.Errorf(
			"this project already has a box (%s at %s, %s old).\n"+
				"  Destroy it first with 'chief box down', or watch it with 'chief box logs'",
			existing.Name, existing.IP, existing.Age())
	}

	prdDir := prd.PRDDir(opts.BaseDir, opts.PRD)
	if _, err := os.Stat(filepath.Join(prdDir, "prd.md")); err != nil {
		return fmt.Errorf("no PRD at .chief/prds/%s/prd.md — create one with 'chief new %s'", opts.PRD, opts.PRD)
	}

	if _, _, err := projectOrigin(opts.BaseDir); err != nil {
		return err
	}

	if err := needTools("ssh", "scp", "rsync"); err != nil {
		return err
	}

	// The key is read rather than uploaded here: a machine with no SSH key
	// cannot be let into the box it is about to pay for, and finding that out
	// after the login is the same bad trade as the checks above.
	if _, _, err := localPublicKey(); err != nil {
		return err
	}
	return nil
}

// Up creates a box, puts the project on it, and starts the run.
//
// It returns as soon as the run is going. Everything after that — watching it,
// destroying the box — is a separate command, because the whole point is that
// the run no longer needs this process.
//
// Callers should run Preflight first; Up repeats its checks so that a caller
// which forgets still cannot create a box it has nowhere to put a project.
func Up(ctx context.Context, opts UpOptions) (State, error) {
	rep := reporter{out: opts.Out}
	var state State

	if err := Preflight(opts); err != nil {
		return state, err
	}
	prdDir := prd.PRDDir(opts.BaseDir, opts.PRD)
	cloneURL, branch, err := projectOrigin(opts.BaseDir)
	if err != nil {
		return state, err
	}

	api := newHetzner(opts.Secrets.HetznerToken)
	if err := api.checkToken(ctx); err != nil {
		return state, err
	}

	// Build before creating anything: a compile error should not cost an
	// instance, and the box runs the chief in this working copy rather than the
	// last one that was released.
	rep.step("Building chief for the box")
	binary, err := buildForLinux(ctx)
	if err != nil {
		return state, err
	}
	defer func() { _ = os.Remove(binary) }()
	if info, err := os.Stat(binary); err == nil {
		rep.detail("%.0f MB", float64(info.Size())/(1<<20))
	}

	key, err := ensureSSHKey(ctx, api, rep)
	if err != nil {
		return state, err
	}

	name := serverName(opts.BaseDir, opts.PRD)
	instanceType, image, location := opts.Type, opts.Image, opts.Location
	if instanceType == "" {
		instanceType = DefaultType
	}
	if image == "" {
		image = DefaultImage
	}
	if location == "" {
		location = DefaultLocation
	}

	rep.step("Creating %s (%s in %s)", name, instanceType, location)
	srv, err := api.createServer(ctx, createServerOpts{
		Name:     name,
		Type:     instanceType,
		Image:    image,
		Location: location,
		SSHKeyID: key.ID,
		UserData: cloudInit(cloudInitOptions{Hostname: name, ExtraPackages: opts.ExtraPackages}),
		Labels:   map[string]string{"managed-by": "chief"},
	})
	if err != nil {
		// A refused type/location pair is the one failure worth turning into an
		// answer: the price list still advertises superseded generations, so
		// "unsupported" reads like a bug in chief rather than a type to change.
		if unsupportedCombination(err) {
			if types, listErr := api.availableTypes(ctx, location); listErr == nil && len(types) > 0 {
				return state, fmt.Errorf("%w\n  %s does not offer %s. It does offer:\n    %s\n  Pick one with --type",
					err, location, instanceType, strings.Join(types, "\n    "))
			}
		}
		return state, err
	}

	state = State{ServerID: srv.ID, Name: srv.Name, IP: srv.IP(), PRD: opts.PRD, Created: time.Now()}
	// Record it before anything else can fail: an instance that exists but was
	// never written down is one the user pays for and cannot find again.
	if err := SaveState(opts.BaseDir, state); err != nil {
		return state, err
	}
	rep.detail("%s", state.IP)

	// From here a failure leaves a box behind, and the user has to be told what
	// it is called and how to get rid of it.
	fail := func(err error) (State, error) {
		return state, fmt.Errorf("%w\n  The box %s is still running — destroy it with 'chief box down'", err, state.Name)
	}

	r := remote{user: remoteUser, host: state.IP}
	root := remote{user: "root", host: state.IP}

	rep.step("Waiting for the box to boot")
	if err := root.waitReachable(ctx, bootTimeout); err != nil {
		return fail(err)
	}

	rep.step("Waiting for provisioning (PHP, Node, Postgres, Claude Code)")
	if err := root.waitFile(ctx, readyMarker, provisionTimeout); err != nil {
		if log, logErr := root.run(ctx, "tail -30 /var/log/cloud-init-output.log"); logErr == nil {
			rep.detail("last lines of the provisioning log:")
			for _, line := range strings.Split(log, "\n") {
				rep.detail("  %s", line)
			}
		}
		return fail(err)
	}
	rep.detail("provisioned")

	rep.step("Installing chief and the credentials")
	if err := root.copyFile(ctx, binary, "/usr/local/bin/chief"); err != nil {
		return fail(err)
	}
	if _, err := root.run(ctx, "chmod 0755 /usr/local/bin/chief"); err != nil {
		return fail(err)
	}
	// The tokens go over stdin into a file that is already 0600, so they never
	// appear in a command line another process could read.
	env := fmt.Sprintf("CLAUDE_CODE_OAUTH_TOKEN=%s\nGH_TOKEN=%s\nCHIEF_RUN_FLAGS=%s\n",
		opts.Secrets.ClaudeToken, opts.Secrets.GitHubToken, runFlags(opts))
	if err := r.runWith(ctx, "cat > ~/.chief-env && chmod 600 ~/.chief-env", env); err != nil {
		return fail(err)
	}
	if version, err := r.run(ctx, "/usr/local/bin/chief --version"); err == nil {
		rep.detail("%s", version)
	}

	rep.step("Cloning the project")
	if err := r.runWith(ctx, cloneScript(cloneURL, branch), gitIdentity(opts.BaseDir)); err != nil {
		return fail(err)
	}
	rep.detail("%s on %s", cloneURL, branch)

	// The PRD lives under .chief, which is gitignored in most projects, so the
	// clone does not have it. It is the one thing the run cannot start without.
	rep.step("Copying the PRD and the untracked files")
	if err := r.sync(ctx, prdDir+"/", remoteProject+"/.chief/prds/"+opts.PRD+"/", "*.log"); err != nil {
		return fail(err)
	}
	configPath := filepath.Join(opts.BaseDir, ".chief", "config.yaml")
	if _, err := os.Stat(configPath); err == nil {
		if err := r.sync(ctx, configPath, remoteProject+"/.chief/config.yaml"); err != nil {
			return fail(err)
		}
	}
	extras := opts.ExtraFiles
	if len(extras) == 0 {
		extras = []string{".env"}
	}
	for _, f := range extras {
		local := filepath.Join(opts.BaseDir, f)
		if _, err := os.Stat(local); err != nil {
			continue
		}
		if err := r.sync(ctx, local, remoteProject+"/"+f); err != nil {
			return fail(err)
		}
		rep.detail("%s", f)
	}

	rep.step("Starting the run")
	if _, err := r.run(ctx, "sudo systemctl start --no-block chief-run@"+shellQuote(opts.PRD)); err != nil {
		return fail(err)
	}

	rep.step("The run is going — it survives this terminal")
	rep.detail("chief box logs     follow along")
	rep.detail("chief box status   is it still running")
	rep.detail("chief box down     destroy the box; this is what stops the billing")
	return state, nil
}

// runFlags are the chief flags the systemd unit adds to the headless run.
func runFlags(opts UpOptions) string {
	var flags []string
	if opts.Worktree {
		flags = append(flags, "--worktree")
	}
	if opts.Verbose {
		flags = append(flags, "--verbose")
	}
	if opts.MaxIterations > 0 {
		flags = append(flags, "-n", fmt.Sprint(opts.MaxIterations))
	}
	return strings.Join(flags, " ")
}

// cloneScript is what runs on the box to get the project there. The git
// identity arrives on stdin rather than baked into the script, so a name with a
// quote in it cannot break out of it.
func cloneScript(cloneURL, branch string) string {
	return `set -e
# set -a, because sourcing alone makes these shell variables rather than
# environment ones, and the credential helper below runs as a child of git —
# which would see no password at all and fail with "Invalid username or token".
#
# The file cannot simply say "export" instead: systemd reads the same file as an
# EnvironmentFile, where every line has to be a bare KEY=VALUE.
set -a
. ~/.chief-env
set +a
read -r NAME
read -r EMAIL
git config --global user.name "$NAME"
git config --global user.email "$EMAIL"
git config --global credential.helper '!f() { echo username=x-access-token; echo password=$GH_TOKEN; }; f'
rm -rf ` + remoteProject + `
git clone --quiet ` + shellQuote(cloneURL) + ` ` + remoteProject + `
cd ` + remoteProject + ` && git checkout --quiet ` + shellQuote(branch) + ` 2>/dev/null || true`
}

// gitIdentity is the committer the box should use: this project's, so commits
// made there are indistinguishable from commits made here.
func gitIdentity(baseDir string) string {
	name := gitConfig(baseDir, "user.name")
	email := gitConfig(baseDir, "user.email")
	if name == "" {
		name = "chief"
	}
	if email == "" {
		email = "chief@localhost"
	}
	return name + "\n" + email + "\n"
}

func gitConfig(dir, key string) string {
	cmd := exec.Command("git", "-C", dir, "config", "--get", key)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// projectOrigin returns a URL the box can clone and the branch to check out.
//
// An SSH remote is rewritten to HTTPS: the box has no key for GitHub and should
// not be given one, but it does have a token, and a token only works over HTTPS.
func projectOrigin(baseDir string) (cloneURL, branch string, err error) {
	if !git.IsGitRepo(baseDir) {
		return "", "", fmt.Errorf("%s is not a git repository", baseDir)
	}
	cmd := exec.Command("git", "-C", baseDir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("this project has no 'origin' remote for the box to clone")
	}
	cloneURL = httpsRemote(strings.TrimSpace(string(out)))

	branch, err = git.GetCurrentBranch(baseDir)
	if err != nil || branch == "" {
		branch = "HEAD"
	}
	return cloneURL, branch, nil
}

// httpsRemote turns a git remote into one a token can authenticate against.
func httpsRemote(url string) string {
	switch {
	case strings.HasPrefix(url, "git@"):
		// git@github.com:owner/repo.git -> https://github.com/owner/repo.git
		rest := strings.TrimPrefix(url, "git@")
		host, path, found := strings.Cut(rest, ":")
		if !found {
			return url
		}
		url = "https://" + host + "/" + path
	case strings.HasPrefix(url, "ssh://git@"):
		url = "https://" + strings.TrimPrefix(url, "ssh://git@")
	}
	return strings.TrimSuffix(url, ".git") + ".git"
}

// serverName builds a name that says which project and which PRD the box is
// for, within what Hetzner accepts: letters, digits and dashes.
func serverName(baseDir, prdName string) string {
	name := fmt.Sprintf("chief-%s-%s-%s", filepath.Base(baseDir), prdName, time.Now().Format("150405"))
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 60 {
		out = strings.Trim(out[:60], "-")
	}
	return out
}

// buildForLinux cross-compiles the chief binary the box will run, and returns
// the path to it.
//
// Building rather than downloading a release is what keeps the box honest: the
// headless mode it depends on may well be newer than the last tag, and a box
// running a chief that predates the working copy would fail in ways that look
// like the project's fault.
func buildForLinux(ctx context.Context) (string, error) {
	if _, err := exec.LookPath("go"); err != nil {
		return "", fmt.Errorf("Go is needed to build the chief the box runs. Install it with: brew install go")
	}
	src, err := moduleRoot(ctx)
	if err != nil {
		return "", err
	}

	f, err := os.CreateTemp("", "chief-linux-*")
	if err != nil {
		return "", err
	}
	path := f.Name()
	_ = f.Close()

	version := describeVersion(ctx, src)
	// -s -w drops the symbol table and DWARF: nobody debugs this binary on the
	// box, and it takes a third off what has to go over the wire.
	cmd := exec.CommandContext(ctx, "go", "build",
		"-ldflags", "-s -w -X main.Version="+version,
		"-o", path, "./cmd/chief")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("building chief for the box failed:\n%s", strings.TrimSpace(string(out)))
	}
	return path, nil
}

// moduleRoot finds the chief source to build the box's binary from.
//
// This is the awkward part of building rather than downloading: `chief box` is
// normally run from the project being built — a Laravel app, say — which is not
// a Go module and knows nothing about where chief's source lives. So the source
// is looked for in the places it plausibly is, and once found, remembered: the
// search happens on somebody's first box and never again.
func moduleRoot(ctx context.Context) (string, error) {
	if dir := strings.TrimSpace(os.Getenv("CHIEF_SRC")); dir != "" {
		if !isChiefSource(dir) {
			return "", fmt.Errorf("CHIEF_SRC is set to %s, which is not a chief checkout", dir)
		}
		return dir, nil
	}

	// What a previous box worked out. Checked before the search so a machine with
	// several checkouts keeps using the one it used last time.
	if dir := readToken("src-path"); dir != "" && isChiefSource(dir) {
		return dir, nil
	}

	for _, dir := range candidateSources(ctx) {
		if isChiefSource(dir) {
			// Best-effort: a path that cannot be saved just means searching again.
			_ = writeToken("src-path", dir)
			return dir, nil
		}
	}

	return "", fmt.Errorf(
		"cannot find the chief source to build the box's binary from.\n" +
			"  The box runs the chief you have rather than the last release, so it has\n" +
			"  to be built. Point CHIEF_SRC at your chief checkout:\n" +
			"    export CHIEF_SRC=~/Code/chief")
}

// candidateSources lists where a chief checkout might be, cheapest guess first.
func candidateSources(ctx context.Context) []string {
	var dirs []string

	// Run from inside the checkout itself, the toolchain answers directly. This
	// is the case when someone is working on chief and testing a box.
	cmd := exec.CommandContext(ctx, "go", "list", "-m", "-f", "{{.Dir}}")
	if out, err := cmd.Output(); err == nil {
		if dir := strings.TrimSpace(string(out)); dir != "" {
			dirs = append(dirs, dir)
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return dirs
	}
	// The places a checkout conventionally lives.
	for _, rel := range []string{
		filepath.Join("Code", "chief"),
		filepath.Join("code", "chief"),
		filepath.Join("src", "chief"),
		filepath.Join("Projects", "chief"),
		filepath.Join("dev", "chief"),
		"chief",
		filepath.Join("go", "src", "github.com", "ben182", "chief"),
	} {
		dirs = append(dirs, filepath.Join(home, rel))
	}
	return dirs
}

// isChiefSource reports whether dir holds the chief module, so a directory that
// merely happens to be called "chief" is not handed to the compiler.
func isChiefSource(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod")) //nolint:gosec // a candidate path being validated
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "module github.com/ben182/chief")
}

// describeVersion is the same stamp the Makefile applies, so `chief --version`
// on the box names the commit the run was built from rather than "dev".
func describeVersion(ctx context.Context, dir string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "describe", "--tags", "--always", "--dirty")
	out, err := cmd.Output()
	if err != nil {
		return "dev"
	}
	return strings.TrimSpace(string(out))
}

// ensureSSHKey finds the key to create the box with, uploading this machine's
// public key when the project does not have it yet.
//
// Uploading rather than asking is the difference between one command and a trip
// to the Hetzner console. The key is matched by fingerprint, not by name, so a
// key already there under someone else's naming scheme is reused rather than
// duplicated.
func ensureSSHKey(ctx context.Context, api *hetzner, rep reporter) (sshKey, error) {
	local, path, err := localPublicKey()
	if err != nil {
		return sshKey{}, err
	}
	want, err := fingerprint(local)
	if err != nil {
		return sshKey{}, fmt.Errorf("%s: %w", path, err)
	}

	keys, err := api.sshKeys(ctx)
	if err != nil {
		return sshKey{}, err
	}
	for _, k := range keys {
		if strings.EqualFold(k.Fingerprint, want) {
			return k, nil
		}
	}

	host, _ := os.Hostname()
	if host == "" {
		host = "chief"
	}
	name := fmt.Sprintf("chief-%s-%s", host, time.Now().Format("20060102"))
	rep.step("Registering this machine's SSH key with Hetzner (%s)", name)
	rep.detail("from %s", path)
	return api.createSSHKey(ctx, name, local)
}

// localPublicKey returns this machine's SSH public key, preferring the modern
// algorithm, and the path it came from.
func localPublicKey() (key, path string, err error) {
	if explicit := strings.TrimSpace(os.Getenv("CHIEF_BOX_SSH_KEY")); explicit != "" {
		data, err := os.ReadFile(explicit) //nolint:gosec // the path the user named
		if err != nil {
			return "", explicit, fmt.Errorf("CHIEF_BOX_SSH_KEY: %w", err)
		}
		return string(data), explicit, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	for _, name := range []string{"id_ed25519.pub", "id_ecdsa.pub", "id_rsa.pub"} {
		p := filepath.Join(home, ".ssh", name)
		data, err := os.ReadFile(p) //nolint:gosec // a fixed path in the user's own .ssh
		if err == nil && strings.TrimSpace(string(data)) != "" {
			return string(data), p, nil
		}
	}
	return "", "", fmt.Errorf(
		"no SSH public key in ~/.ssh — the box needs one to let you in.\n" +
			"  Create one with: ssh-keygen -t ed25519")
}

// needTools reports the first of the named binaries that is not installed.
func needTools(names ...string) error {
	for _, n := range names {
		if _, err := exec.LookPath(n); err != nil {
			return fmt.Errorf("%s is not installed, and the box is reached with it", n)
		}
	}
	return nil
}

// Logs follows the run's log until the caller stops watching. Interrupting it
// stops the watching, not the run.
func Logs(ctx context.Context, baseDir string, out io.Writer) error {
	s, ok := LoadState(baseDir)
	if !ok {
		return errNoBox
	}
	r := remote{user: remoteUser, host: s.IP}
	return r.stream(ctx, "journalctl -u chief-run@"+shellQuote(s.PRD)+" -f --no-hostname -o cat", out)
}

// Status says what the box is doing, and what it has cost so far.
func Status(ctx context.Context, baseDir string, out io.Writer) error {
	s, ok := LoadState(baseDir)
	if !ok {
		return errNoBox
	}
	rep := reporter{out: out}
	rep.step("%s at %s — PRD %s, %s old", s.Name, s.IP, s.PRD, s.Age())

	r := remote{user: remoteUser, host: s.IP}
	unit := shellQuote("chief-run@" + s.PRD)
	state, err := r.run(ctx, "systemctl is-active "+unit+"; systemctl show "+unit+" -p Result --value")
	if err != nil && state == "" {
		return fmt.Errorf("the box is not answering: %w", err)
	}
	for _, line := range strings.Split(state, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			rep.detail("%s", line)
		}
	}

	tail, err := r.run(ctx, "journalctl -u "+unit+" --no-hostname -o cat -n 15")
	if err == nil && tail != "" {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, tail)
	}
	return nil
}

// SSH opens a shell on the box in the project directory, or runs command there
// and streams its output back.
//
// The one-shot form is what makes the box inspectable without a round trip: a
// question about what is installed, what the working tree looks like, or what a
// setup script left behind is one line rather than a session.
func SSH(ctx context.Context, baseDir string, command string, out io.Writer) error {
	s, ok := LoadState(baseDir)
	if !ok {
		return errNoBox
	}
	r := remote{user: remoteUser, host: s.IP}
	if strings.TrimSpace(command) == "" {
		return r.shell(ctx, remoteProject)
	}
	return r.stream(ctx, "cd "+shellQuote(remoteProject)+" && "+command, out)
}

// DownOptions controls destroying a box.
type DownOptions struct {
	BaseDir string
	// Force destroys the box without asking about work that was never pushed.
	Force bool
	// Confirm is asked when the box is holding unpushed commits. Nil means the
	// answer is no, which is what a script that did not pass --force wants.
	Confirm func(prompt string) bool
	Out     io.Writer
}

// Down destroys the box.
//
// Committed work that was never pushed dies with it, so that is checked first
// and asked about — it is the only thing on the machine that does not exist
// anywhere else.
func Down(ctx context.Context, opts DownOptions) error {
	s, ok := LoadState(opts.BaseDir)
	if !ok {
		return errNoBox
	}
	rep := reporter{out: opts.Out}

	if !opts.Force {
		if n := unpushedCommits(ctx, s); n > 0 {
			prompt := fmt.Sprintf(
				"The box has %d commit(s) that were never pushed — they exist nowhere else.\n"+
					"  Look with: chief box ssh, then git log\n"+
					"  Destroy %s anyway?", n, s.Name)
			if opts.Confirm == nil || !opts.Confirm(prompt) {
				return fmt.Errorf("kept %s", s.Name)
			}
		}
	}

	rep.step("Destroying %s (%s old)", s.Name, s.Age())
	api := newHetzner(secretsOrEnvHetzner())
	if err := api.deleteServer(ctx, s.ServerID); err != nil {
		return fmt.Errorf("%w\n  The record is kept; destroy it in the Hetzner console if this persists", err)
	}
	if err := ForgetState(opts.BaseDir); err != nil {
		return err
	}
	rep.detail("gone")
	return nil
}

// secretsOrEnvHetzner re-resolves the Hetzner token for a command that only
// needs that one. Down is the case: it should work without a Claude login.
func secretsOrEnvHetzner() string {
	t, _ := resolveHetznerToken()
	return t
}

// unpushedCommits counts what the box has committed and not pushed. A box that
// cannot be reached reports zero: it cannot be asked, and refusing to destroy a
// machine that is not answering would leave it billing forever.
func unpushedCommits(ctx context.Context, s State) int {
	r := remote{user: remoteUser, host: s.IP}
	probe, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := r.run(probe, "cd "+remoteProject+" && git log --oneline @{upstream}..HEAD 2>/dev/null | wc -l")
	if err != nil {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "%d", &n); err != nil {
		return 0
	}
	return n
}

// errNoBox is what every command but `up` says when the project has none.
var errNoBox = fmt.Errorf("no box for this project — start one with 'chief box up <prd>'")
