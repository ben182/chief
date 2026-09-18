// Package box runs a PRD on a throwaway cloud instance.
//
// A chief run takes hours, and for those hours it owns the machine it runs on:
// the CPU, the rate limit window, and the laptop that has to stay open. This
// package moves the run onto a machine created for it and destroyed afterwards.
// At current Hetzner prices a five-hour run costs under thirty cents of
// computer, which is two orders of magnitude less than the tokens it spends —
// and less than five if the run is happy on two cores.
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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ben182/chief/internal/git"
	"github.com/ben182/chief/internal/prd"
)

// Defaults for a box. Each can be overridden per run; these are the answers
// that suit a chief run rather than a server — a German region, and enough
// cores that a test suite is not the slow part.
const (
	// DefaultType is 4 vCPU and 8 GB for about six cents an hour — enough that a
	// test suite is not the slow part. A five-hour run is under thirty cents,
	// which is still two orders of magnitude below what it spends on tokens.
	//
	// Server types are generational: a line that is current today stops being
	// bookable in a location when its successor arrives, while still appearing
	// in the price list. This constant was cx33 until the cx line shrank to a
	// single 2-core machine in every European location, at which point every
	// `chief box up` that had not been given a `--type` failed with "unsupported
	// location". If that happens again, `chief box config` lists what is
	// actually creatable today, and so does the error itself.
	DefaultType = "cpx32"
	// DefaultLocation is Falkenstein.
	DefaultLocation = "fsn1"
)

// remoteUser is the unprivileged account a run happens under, and remoteProject
// is where the clone lands. Both are fixed by the cloud-config.
const (
	remoteUser    = "chief"
	remoteProject = "/home/chief/project"
	readyMarker   = "/var/lib/cloud/chief-ready"
	// failedMarker is what the cloud-config leaves behind when a provisioning
	// step fails, so the wait below can stop at once rather than at its timeout.
	failedMarker = "/var/lib/cloud/chief-failed"
)

// How long each phase is given. Provisioning is apt working through a few
// hundred megabytes and is by far the slowest.
const (
	bootTimeout = 3 * time.Minute
	// baseProvision covers a box with no profile at all: apt's own update, the
	// base packages, Claude Code and the GitHub CLI.
	baseProvision = 10 * time.Minute
	// maxProvision is the ceiling. Past this a box is not slow, it is stuck, and
	// waiting longer only delays the report — the failed marker catches a step
	// that dies, and this catches one that never returns.
	maxProvision = 35 * time.Minute
)

// provisionTimeout is how long the profile's own installation is given on top
// of the base, roughly what each part takes on a fresh instance with a fast
// mirror, doubled — a timeout that trips on a slow morning is a box destroyed
// for no reason, while one that is generous costs nothing on the normal run
// because the ready marker ends the wait the moment it appears.
//
// A fixed twenty minutes was the same number for a bare Go box and for PHP plus
// Node plus Chrome plus Playwright's system libraries, which is a few hundred
// megabytes apart.
func provisionTimeout(p Profile) time.Duration {
	d := baseProvision
	if p.PHP != "" {
		// The PPA, the interpreter, its extensions, and Composer.
		d += 5 * time.Minute
	}
	if p.Node != "" {
		d += 3 * time.Minute
	}
	if p.PackageManager == "bun" || p.PackageManager == "pnpm" || p.PackageManager == "yarn" {
		d += time.Minute
	}
	if p.Go != "" {
		// A toolchain tarball, around 80 MB.
		d += 3 * time.Minute
	}
	if p.Database != "" && p.Database != "sqlite" {
		d += 2 * time.Minute
	}
	if p.Redis {
		d += time.Minute
	}
	if p.Meilisearch {
		d += 2 * time.Minute
	}
	if p.Browser {
		// playwright install-deps pulls in the widest single thing a box installs.
		d += 8 * time.Minute
	}
	if p.Chrome {
		d += 4 * time.Minute
	}
	if d > maxProvision {
		return maxProvision
	}
	return d
}

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
	// Profile is what Discover read out of the project: which runtimes, at
	// which versions, with which servers the box is built. A zero profile
	// builds the base alone.
	Profile Profile
	// ExtraPackages are apt packages the project needs on top of the profile.
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
// checked without creating anything.
//
// It is separate from Up, and called before the secrets are resolved, because
// resolving them can open a browser: sending someone through a login only to
// tell them afterwards that their project has no origin remote is a bad way to
// spend their attention. Everything here fails in seconds — the one check that
// leaves this machine asks origin for a list of branch names.
func Preflight(opts UpOptions) error {
	if existing, ok := LoadState(opts.BaseDir); ok {
		return fmt.Errorf(
			"this project already has a box (%s at %s, %s old).\n"+
				"  Put the project on it again with 'chief box retry', destroy it with\n"+
				"  'chief box down', or watch it with 'chief box logs'",
			existing.Name, existing.IP, existing.Age())
	}
	return preflightProject(opts)
}

// preflightProject is the half of Preflight that is about the project rather
// than about whether it already owns a machine — which is everything Retry
// wants, since the machine it is about to use is the one that would fail that
// check.
func preflightProject(opts UpOptions) error {
	prdDir := prd.PRDDir(opts.BaseDir, opts.PRD)
	if _, err := os.Stat(filepath.Join(prdDir, "prd.md")); err != nil {
		return fmt.Errorf("no PRD at .chief/prds/%s/prd.md — create one with 'chief new %s'", opts.PRD, opts.PRD)
	}

	_, branch, err := projectOrigin(opts.BaseDir)
	if err != nil {
		return err
	}
	if err := branchIsOnOrigin(opts.BaseDir, branch); err != nil {
		return err
	}

	if err := needTools("ssh", "scp", "rsync", "ssh-keygen"); err != nil {
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

	// The box's own identity, made here so that it is known before the machine
	// it belongs to exists. Everything chief sends the box in its first minutes
	// — both tokens and the project's .env — is checked against this.
	host, err := generateHostKey(ctx)
	if err != nil {
		return state, err
	}

	fw, action, err := api.ensureFirewall(ctx)
	if err != nil {
		return state, err
	}
	switch action {
	case firewallCreated:
		rep.step("Created the %s firewall — inbound: ssh only", fw.Name)
	case firewallRulesRestored:
		rep.step("Put the %s firewall's rules back — inbound: ssh only", fw.Name)
	case firewallReused:
	}

	rep.step("Creating %s (%s in %s)", name, instanceType, location)
	srv, err := api.createServer(ctx, createServerOpts{
		Name:     name,
		Type:     instanceType,
		Image:    image,
		Location: location,
		SSHKeyID: key.ID,
		UserData: cloudInit(cloudInitOptions{
			Hostname:      name,
			Profile:       opts.Profile,
			ExtraPackages: opts.ExtraPackages,
			HostKey:       host,
		}),
		Labels:     map[string]string{"managed-by": "chief"},
		FirewallID: fw.ID,
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

	state = State{
		ServerID: srv.ID,
		Name:     srv.Name,
		IP:       srv.IP(),
		PRD:      opts.PRD,
		Type:     instanceType,
		Location: location,
		HostKey:  host.Public,
		Created:  time.Now(),
	}
	// Record it before anything else can fail: an instance that exists but was
	// never written down is one the user pays for and cannot find again.
	if err := SaveState(opts.BaseDir, state); err != nil {
		return state, err
	}
	rep.detail("%s", state.IP)

	// Now that the box has an address, the key it was built with can be written
	// against it, and every connection below is checked rather than trusted.
	knownHosts, err := writeKnownHosts(opts.BaseDir, state.IP, host.Public)
	if err != nil {
		return state, err
	}

	return settle(ctx, opts, rep, state, knownHosts, binary)
}

// Retry puts the project on a box that already exists and starts the run,
// without creating a second machine.
//
// It is for the box that provisioned perfectly and then fell over on the step
// after: a clone refused by a token that had expired, a file the run needed
// that was not where it was said to be. Everything up to that point took
// minutes and money, and throwing it away to repeat it identically is the wrong
// answer to a one-line failure. Every step it repeats is written to be repeated
// — the clone removes its directory first, the credentials overwrite, the
// copies mirror.
func Retry(ctx context.Context, opts UpOptions) (State, error) {
	rep := reporter{out: opts.Out}

	state, ok := LoadState(opts.BaseDir)
	if !ok {
		return state, errNoBox
	}
	if err := preflightProject(opts); err != nil {
		return state, err
	}
	opts.PRD = state.PRD

	knownHosts := knownHostsFor(opts.BaseDir, state)
	r := remote{user: remoteUser, host: state.IP, knownHosts: knownHosts}

	// A run that is already going must not be started a second time. The unit is
	// one-shot, so systemd would refuse anyway, but the steps before it are not:
	// re-cloning the project under a working run would pull the ground out from
	// under the agent.
	if active, err := r.run(ctx, "systemctl is-active "+shellQuote("chief-run@"+state.PRD)); err == nil &&
		strings.TrimSpace(active) == "active" {
		return state, fmt.Errorf(
			"the run on %s is still going — there is nothing to retry.\n"+
				"  Watch it with 'chief box logs', or end it with 'chief box down'", state.Name)
	}

	rep.step("Reusing %s at %s (%s old)", state.Name, state.IP, state.Age())

	rep.step("Building chief for the box")
	binary, err := buildForLinux(ctx)
	if err != nil {
		return state, err
	}
	defer func() { _ = os.Remove(binary) }()

	return settle(ctx, opts, rep, state, knownHosts, binary)
}

// settle is everything that happens once the machine exists: waiting for it,
// installing chief and the credentials on it, putting the project there, and
// starting the run. Up and Retry differ only in how the box in front of them
// came to be.
func settle(ctx context.Context, opts UpOptions, rep reporter, state State, knownHosts, binary string) (State, error) {
	prdDir := prd.PRDDir(opts.BaseDir, opts.PRD)
	cloneURL, branch, err := projectOrigin(opts.BaseDir)
	if err != nil {
		return state, err
	}

	// From here a failure leaves a box behind, and the user has to be told what
	// it is called and how to get rid of it.
	fail := func(err error) (State, error) {
		return state, fmt.Errorf(
			"%w\n  The box %s is still running — try again with 'chief box retry', "+
				"or destroy it with 'chief box down'", err, state.Name)
	}

	r := remote{user: remoteUser, host: state.IP, knownHosts: knownHosts}
	root := remote{user: "root", host: state.IP, knownHosts: knownHosts}

	rep.step("Waiting for the box to boot")
	if err := root.waitReachable(ctx, bootTimeout); err != nil {
		return fail(err)
	}

	budget := provisionTimeout(opts.Profile)
	rep.step("Waiting for provisioning (%s)", opts.Profile.provisions())
	rep.detail("up to %s", budget)
	if err := root.waitProvisioned(ctx, readyMarker, failedMarker, budget); err != nil {
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
	if note := gitHubTokenNote(opts.Secrets.GitHubToken); note != "" {
		rep.detail("note: %s", note)
	}
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
		// The .env is the one file that describes the machine it came from, so
		// it is the one file that is translated on the way rather than copied.
		if filepath.Base(f) == ".env" {
			changed, err := sendEnv(ctx, r, local, remoteProject+"/"+f, opts.Profile)
			if err != nil {
				return fail(err)
			}
			if len(changed) > 0 {
				rep.detail("%s (pointed at the box: %s)", f, strings.Join(changed, ", "))
			} else {
				rep.detail("%s", f)
			}
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
	rep.detail("chief box list     every box you have, and what it has cost")
	return state, nil
}

// sendEnv puts a project's .env on the box with the keys that named this
// machine's database, Redis and search server rewritten to name the box's. It
// returns the keys it changed, so the report can say what was touched.
//
// Over stdin rather than rsync: the file holds credentials, and this way the
// rewritten copy never exists on disk here.
func sendEnv(ctx context.Context, r remote, local, remotePath string, p Profile) ([]string, error) {
	data, err := os.ReadFile(local) //nolint:gosec // the project's own .env, named by its config
	if err != nil {
		return nil, err
	}
	overrides := envOverrides(p, readEnv(local))
	content := applyEnv(string(data), overrides)
	if err := r.runWith(ctx, "cat > "+shellQuote(remotePath)+" && chmod 600 "+shellQuote(remotePath), content); err != nil {
		return nil, err
	}
	changed := make([]string, 0, len(overrides))
	for key := range overrides {
		changed = append(changed, key)
	}
	sort.Strings(changed)
	return changed, nil
}

// runFlags are the chief flags the systemd unit adds to the headless run.
func runFlags(opts UpOptions) string {
	// Always, and not an option: this run's log lives in the journal of a machine
	// that exists to be destroyed. Committing it next to the PRD is the only way
	// anything it said is still readable tomorrow morning.
	flags := []string{"--log-to-branch"}
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
# No "|| true" here. Preflight established that origin carries this branch, so a
# checkout that fails now means the box got something other than the project it
# was created for — which is worth stopping for, not carrying on past.
cd ` + remoteProject + ` && git checkout --quiet ` + shellQuote(branch)
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

// branchIsOnOrigin refuses a run whose starting point the box would never see.
//
// The box does not copy the working copy; it clones origin and checks out a
// branch by name. Two states of this checkout make that quietly wrong, and both
// end the same way — hours of work against a base nobody chose:
//
// A branch that was never pushed does not exist for the clone. `git checkout`
// fails, and the box works on whatever the default branch is, which looks
// perfectly healthy in the log.
//
// A branch that is ahead of origin is worse, because it does exist: the box
// checks it out at origin's commit, and the run builds on top of a version of
// the project that is missing the last thing its author did.
//
// Neither is a state to guess about, and both are one `git push` from being
// fine. Asking origin costs a second and happens before anything is created.
func branchIsOnOrigin(baseDir, branch string) error {
	// "HEAD" is what projectOrigin falls back to for a checkout that is not on a
	// branch. The clone then stays on origin's default branch, which is the only
	// thing a detached HEAD could have meant anyway.
	if branch == "" || branch == "HEAD" {
		return nil
	}

	out, err := exec.Command("git", "-C", baseDir, "ls-remote", "--heads", "origin", "refs/heads/"+branch).Output()
	if err != nil {
		// origin could not be asked at all: no network, or a remote this machine
		// has no credentials for. The box authenticates as itself and may well
		// manage what this did not, so a failure here does not block a run.
		return nil //nolint:nilerr // an origin that cannot be asked is not an answer about the branch
	}

	remoteHead := ""
	if fields := strings.Fields(strings.TrimSpace(string(out))); len(fields) > 0 {
		remoteHead = fields[0]
	}
	if remoteHead == "" {
		return fmt.Errorf(
			"origin has no branch %q, and the box clones from origin.\n"+
				"  The run would start on origin's default branch instead — hours of work\n"+
				"  on a base you did not pick.\n"+
				"  Push it first:  git push -u origin %s", branch, branch)
	}

	if n := commitsAhead(baseDir, remoteHead); n > 0 {
		return fmt.Errorf(
			"%d commit(s) on %s are only on this machine, and the box clones from origin.\n"+
				"  The run would start without them.\n"+
				"  Push them first:  git push", n, branch)
	}
	return nil
}

// commitsAhead counts the commits this checkout has on top of ref, and returns
// zero whenever that cannot be established — a ref the local repository has
// never heard of, a shallow clone, a git that answered something unexpected.
// Being wrong in that direction lets a run start; being wrong in the other
// would block one over a question this check could not answer.
func commitsAhead(baseDir, ref string) int {
	out, err := exec.Command("git", "-C", baseDir, "rev-list", "--count", ref+"..HEAD").Output()
	if err != nil {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &n); err != nil {
		return 0
	}
	return n
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
	r := remote{user: remoteUser, host: s.IP, knownHosts: knownHostsFor(baseDir, s)}
	return r.stream(ctx, "journalctl -u chief-run@"+shellQuote(s.PRD)+" -f --no-hostname -o cat", out)
}

// Status says what the box is doing, and what it has cost so far.
func Status(ctx context.Context, baseDir string, out io.Writer) error {
	s, ok := LoadState(baseDir)
	if !ok {
		return errNoBox
	}
	rep := reporter{out: out}
	machine := ""
	if s.Type != "" {
		machine = fmt.Sprintf(" — %s in %s", s.Type, s.Location)
	}
	rep.step("%s at %s%s — PRD %s, %s old", s.Name, s.IP, machine, s.PRD, s.Age())

	r := remote{user: remoteUser, host: s.IP, knownHosts: knownHostsFor(baseDir, s)}
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
	r := remote{user: remoteUser, host: s.IP, knownHosts: knownHostsFor(baseDir, s)}
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
	// All destroys every box in the Hetzner project rather than this project's.
	// Name destroys one box by name, wherever it was created from. Both go
	// through Hetzner rather than through a checkout's own record, which is what
	// makes them the answer to a box nothing local remembers.
	All  bool
	Name string
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
	if opts.All || opts.Name != "" {
		return downFromHetzner(ctx, opts)
	}

	s, ok := LoadState(opts.BaseDir)
	if !ok {
		return errNoBox
	}
	rep := reporter{out: opts.Out}

	if !opts.Force {
		if n := unpushedCommits(ctx, opts.BaseDir, s); n > 0 {
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
	// The firewall is not touched. It is shared by every box, it costs nothing
	// between them, and the next one is created behind it — which is the whole
	// reason there is one of them rather than one per machine.
	if err := ForgetState(opts.BaseDir); err != nil {
		return err
	}
	rep.detail("gone")
	return nil
}

// downFromHetzner destroys boxes the current checkout does not own: all of
// them, or one by name.
//
// It exists because `chief box list` could already show you a machine you had
// forgotten and then had nothing to offer but a link to the Hetzner console.
// The record that lets `down` work lives in one checkout, and a box is
// forgotten precisely when that checkout is gone — the branch was deleted, the
// laptop was reinstalled, the run was started from a directory nobody has
// opened since. What the boxes still have in common is the label chief puts on
// every one of them.
func downFromHetzner(ctx context.Context, opts DownOptions) error {
	token, err := resolveHetznerToken()
	if err != nil {
		return err
	}
	return downWith(ctx, opts, newHetzner(token))
}

// downWith is downFromHetzner with the API client injected, so a test can
// destroy a project full of boxes without owning one.
func downWith(ctx context.Context, opts DownOptions, api *hetzner) error {
	rep := reporter{out: opts.Out}

	servers, err := api.listServers(ctx, chiefLabel)
	if err != nil {
		return err
	}
	if opts.Name != "" {
		var matched []server
		for _, s := range servers {
			if s.Name == opts.Name {
				matched = append(matched, s)
			}
		}
		if len(matched) == 0 {
			return fmt.Errorf("no box called %q — 'chief box list' shows the ones there are", opts.Name)
		}
		servers = matched
	}
	if len(servers) == 0 {
		rep.step("No boxes — nothing is billing")
		return nil
	}

	// Asked before anything is destroyed, and asked of every box rather than of
	// the first: the whole list goes in one question, because a prompt per
	// machine is a prompt people answer without reading.
	if !opts.Force {
		// Asked of every box at once. Each question is a connection that may
		// never be answered — a box that is gone, a machine that never came up —
		// and asking in turn would make the wait the sum of every one of them.
		counts := make([]int, len(servers))
		var wg sync.WaitGroup
		for i, s := range servers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				counts[i] = unpushedOn(ctx, opts.BaseDir, s)
			}()
		}
		wg.Wait()

		var holding []string
		for i, s := range servers {
			if counts[i] > 0 {
				holding = append(holding, fmt.Sprintf("%s (%d commit(s))", s.Name, counts[i]))
			}
		}
		prompt := fmt.Sprintf("Destroy %s?", plural(len(servers), "box", "boxes"))
		if len(holding) > 0 {
			prompt = fmt.Sprintf(
				"Work that was never pushed would go with them:\n    %s\n"+
					"  Destroy %s anyway?",
				strings.Join(holding, "\n    "), plural(len(servers), "box", "boxes"))
		}
		if opts.Confirm == nil || !opts.Confirm(prompt) {
			return fmt.Errorf("kept %s", plural(len(servers), "box", "boxes"))
		}
	}

	current, hasCurrent := LoadState(opts.BaseDir)
	var failed []string
	for _, s := range servers {
		rep.step("Destroying %s (%s old)", s.Name, time.Since(s.Created).Round(time.Second))
		if err := api.deleteServer(ctx, s.ID); err != nil {
			rep.detail("failed: %v", err)
			failed = append(failed, s.Name)
			continue
		}
		// The local record has to go with the machine it describes, or every
		// command afterwards talks to an address that is now somebody else's.
		if hasCurrent && s.ID == current.ServerID {
			if err := ForgetState(opts.BaseDir); err != nil {
				return err
			}
		}
		rep.detail("gone")
	}
	if len(failed) > 0 {
		return fmt.Errorf("could not destroy %s — try again, or use the Hetzner console", strings.Join(failed, ", "))
	}
	return nil
}

// unpushedOn asks a box that no checkout owns what it is holding. Without a
// pinned host key the connection accepts whatever answers, which is what asking
// a question carrying no secrets can afford — and the alternative is destroying
// a machine without knowing what was on it.
func unpushedOn(ctx context.Context, baseDir string, s server) int {
	state := State{ServerID: s.ID, Name: s.Name, IP: s.IP()}
	if current, ok := LoadState(baseDir); ok && current.ServerID == s.ID {
		state = current
	}
	return unpushedCommits(ctx, baseDir, state)
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
//
// The question is "is there a commit here that exists nowhere else", and
// unpushedProbe is the only form of it that answers correctly. The obvious
// spelling — `git log @{upstream}..HEAD` — answers zero in exactly the case
// that matters: a branch created on the box has no upstream at all, so git
// fails, the failure goes to /dev/null, and `wc -l` reports a reassuring 0.
// With the project default of onComplete.push being off, that is every run.
const unpushedProbe = "cd " + remoteProject + " && git rev-list --count --branches --not --remotes"

func unpushedCommits(ctx context.Context, baseDir string, s State) int {
	r := remote{user: remoteUser, host: s.IP, knownHosts: knownHostsFor(baseDir, s)}
	out, err := r.ask(ctx, unpushedProbe, 20*time.Second)
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

// pollInterval is how often Watch asks whether the run is still going. A run
// takes hours; asking more often than this costs a connection and buys nothing.
const pollInterval = 20 * time.Second

// startGrace is how long a unit is allowed to not be running yet before Watch
// concludes it never will. `systemctl start --no-block` returns before the unit
// is up, so an immediate "inactive" means "not yet", not "finished".
const startGrace = 2 * time.Minute

// Outcome is how the run ended, as the box's own service manager saw it.
//
// It is read rather than inferred because the alternative — deciding from the
// log — means parsing prose that exists to be read by a person. systemd has the
// answer already: what it made of the process, and what the process itself
// said on the way out. chief's headless mode exits non-zero when it ends with
// stories unresolved, so those two together are the whole verdict.
type Outcome struct {
	// Result is systemd's own word for it: "success", "exit-code", "signal",
	// "timeout", and a few more. Empty when the box could not be asked.
	Result string
	// Status is the exit status of the run itself: 0 when every story was
	// resolved, 1 when the run ended with work left.
	Status int
}

// Completed reports whether the run finished with everything resolved.
func (o Outcome) Completed() bool { return o.Result == "success" && o.Status == 0 }

// Describe is the outcome in the few words a notification has room for.
func (o Outcome) Describe() string {
	switch {
	case o.Result == "":
		// The run ended and the box did not say how — most likely it stopped
		// answering. Saying "finished" here would be a guess presented as a fact.
		return "ended — the box did not say how"
	case o.Completed():
		return "done — every story resolved"
	case o.Result == "exit-code":
		return "ended with work left"
	case o.Result == "signal" || o.Result == "core-dump":
		return "killed"
	case o.Result == "timeout":
		return "timed out"
	default:
		return "ended (" + o.Result + ")"
	}
}

// Watch follows the run's log and returns when the run itself has ended,
// reporting how it went.
//
// It is the difference between `box run`, which shows the log until you stop
// looking, and a command that can do something afterwards. Nothing about the
// box changes here: the run is on the machine, and interrupting this leaves it
// exactly as it was.
func Watch(ctx context.Context, baseDir string, out io.Writer) (Outcome, error) {
	s, ok := LoadState(baseDir)
	if !ok {
		return Outcome{}, errNoBox
	}
	r := remote{user: remoteUser, host: s.IP, knownHosts: knownHostsFor(baseDir, s)}
	unit := shellQuote("chief-run@" + s.PRD)

	// The log stream is its own context, so that finding the run has ended can
	// stop the stream without also cancelling the caller's context.
	streaming, stopStreaming := context.WithCancel(ctx)
	defer stopStreaming()
	go func() {
		_ = r.stream(streaming, "journalctl -u "+unit+" -f --no-hostname -o cat", out)
	}()

	started := time.Now()
	var everRan bool
	for {
		if !sleep(ctx, pollInterval) {
			return Outcome{}, ctx.Err()
		}
		state, err := r.run(ctx, "systemctl is-active "+unit)
		if err != nil && state == "" {
			// A box that stops answering mid-run is not a finished run, and
			// destroying it on that basis would throw away work. Keep watching.
			continue
		}
		switch strings.TrimSpace(state) {
		case "active", "activating", "reloading":
			everRan = true
		default:
			if everRan || time.Since(started) > startGrace {
				return unitOutcome(ctx, r, unit), nil
			}
		}
	}
}

// unitOutcome asks the box how the run ended. A box that will not answer gives
// a zero Outcome, which says exactly that rather than claiming success.
func unitOutcome(ctx context.Context, r remote, unit string) Outcome {
	out, err := r.ask(ctx, "systemctl show "+unit+" -p Result -p ExecMainStatus --value", 20*time.Second)
	if err != nil {
		return Outcome{}
	}
	var o Outcome
	fields := strings.Fields(out)
	if len(fields) > 0 {
		o.Result = fields[0]
	}
	if len(fields) > 1 {
		if _, err := fmt.Sscanf(fields[1], "%d", &o.Status); err != nil {
			o.Status = 0
		}
	}
	return o
}
