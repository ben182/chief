package box

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The tests in this file create a real Hetzner server and destroy it again.
// They exist because the most important things this package does cannot be
// proven any other way: whether cloud-init puts the host key in place before
// sshd starts, whether the firewall actually stops traffic, and whether Hetzner
// lets go of a firewall in time for the destroy to remove it. Every one of
// those was assumed correct, tested against a fake that agreed with the
// assumption, and wrong.
//
// They are off by default and must stay that way. A test that spends money is a
// test nobody should run by accident — an earlier version of this file was not
// guarded, and a full `go test ./...` quietly created a machine each time.
//
//	CHIEF_LIVE_BOX_TEST=1 go test ./internal/box/ -run TestLive -v -timeout 20m
//
// One run is a few minutes of the cheapest machine, billed as one hour.

// requireLiveBox skips unless somebody deliberately asked to spend money.
func requireLiveBox(t *testing.T) {
	t.Helper()
	if os.Getenv("CHIEF_LIVE_BOX_TEST") != "1" {
		t.Skip("creates a real, billable Hetzner server — set CHIEF_LIVE_BOX_TEST=1 to run it")
	}
}

// liveBox creates a box and registers its destruction, returning the server and
// the project directory that holds its state.
func liveBox(t *testing.T, purpose string) (*hetzner, server, hostKey, string) {
	t.Helper()
	ctx := context.Background()

	token, err := resolveHetznerToken()
	if err != nil {
		t.Skipf("no Hetzner token: %v", err)
	}
	api := newHetzner(token)

	host, err := generateHostKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ensureSSHKey(ctx, api, reporter{out: os.Stderr})
	if err != nil {
		t.Fatal(err)
	}

	fw, _, err := api.ensureFirewall(ctx)
	if err != nil {
		t.Fatal(err)
	}
	machine := cheapestType(t, api)
	name := fmt.Sprintf("chief-%s-%s", purpose, time.Now().Format("150405"))
	srv, err := api.createServer(ctx, createServerOpts{
		Name: name, Type: machine, Image: DefaultImage, Location: DefaultLocation,
		SSHKeyID: key.ID, FirewallID: fw.ID,
		UserData: cloudInit(cloudInitOptions{Hostname: name, HostKey: host}),
		Labels:   map[string]string{"managed-by": "chief"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("created %s (%s) at %s", srv.Name, machine, srv.IP())

	project := t.TempDir()
	state := State{
		ServerID: srv.ID, Name: srv.Name, IP: srv.IP(), PRD: "probe",
		Type: machine, Location: DefaultLocation,
		HostKey: host.Public, Created: time.Now(),
	}
	if err := SaveState(project, state); err != nil {
		t.Fatal(err)
	}
	if _, err := writeKnownHosts(project, srv.IP(), host.Public); err != nil {
		t.Fatal(err)
	}

	// Registered before the test body runs, so nothing it does can leave a
	// machine behind. A cleanup that has to be reached is a cleanup that gets
	// skipped by the first t.Fatal.
	t.Cleanup(func() {
		if _, still := LoadState(project); !still {
			return // the test destroyed it itself, which is what it was checking
		}
		if err := api.deleteServer(context.Background(), srv.ID); err != nil {
			t.Errorf("SERVER %d (%s) NOT DELETED: %v", srv.ID, srv.Name, err)
		}
	})
	// The firewall is shared and outlives every box on purpose, so there is
	// nothing to clean up about it — which is the point of there being one.
	_ = fw
	return api, srv, host, project
}

// TestLiveHostKeyIsInPlaceBeforeSSHD proves the point of generating the host key
// locally: the very first connection, the one that carries both tokens and the
// project's .env, is checked rather than trusted.
func TestLiveHostKeyIsInPlaceBeforeSSHD(t *testing.T) {
	requireLiveBox(t)
	ctx := context.Background()
	_, srv, host, project := liveBox(t, "hostkey")

	state, _ := LoadState(project)
	root := remote{user: "root", host: srv.IP(), knownHosts: knownHostsFor(project, state)}

	// Strict from the first attempt. If cloud-init applied the key after sshd
	// had started with one of its own, this never succeeds.
	if err := root.waitReachable(ctx, 4*time.Minute); err != nil {
		t.Fatalf("never reachable with the pinned host key: %v", err)
	}
	t.Log("host key matched on the first connection")

	// And it still has to match once provisioning is done, which is when
	// anything that regenerates keys would have run.
	if err := root.waitFile(ctx, readyMarker, provisionTimeout(Profile{})); err != nil {
		t.Fatalf("provisioning did not finish: %v", err)
	}
	out, err := root.run(ctx, "cat /etc/ssh/ssh_host_ed25519_key.pub; ls /etc/ssh/ssh_host_*")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, strings.Fields(host.Public)[1]) {
		t.Errorf("the box answers with a key chief did not generate:\n%s", out)
	}
	if strings.Contains(out, "ssh_host_rsa") || strings.Contains(out, "ssh_host_ecdsa") {
		t.Errorf("the box kept identities chief knows nothing about:\n%s", out)
	}
	t.Log("only the generated ed25519 identity survives provisioning")

	user := remote{user: remoteUser, host: srv.IP(), knownHosts: knownHostsFor(project, state)}
	if who, err := user.run(ctx, "whoami && sudo -n true && echo sudo-ok"); err != nil {
		t.Errorf("the chief user cannot work: %v", err)
	} else {
		t.Logf("run user: %s", strings.ReplaceAll(strings.TrimSpace(who), "\n", " "))
	}
}

// TestLiveFirewallClosesEverythingButSSH starts a server on a port a project's
// setup might well open and checks that the internet cannot reach it.
func TestLiveFirewallClosesEverythingButSSH(t *testing.T) {
	requireLiveBox(t)
	ctx := context.Background()
	_, srv, _, project := liveBox(t, "firewall")

	state, _ := LoadState(project)
	r := remote{user: "root", host: srv.IP(), knownHosts: knownHostsFor(project, state)}
	if err := r.waitReachable(ctx, 4*time.Minute); err != nil {
		t.Fatalf("never reachable: %v", err)
	}

	if _, err := r.run(ctx, "nohup python3 -m http.server 8000 --bind 0.0.0.0 >/tmp/h.log 2>&1 & sleep 2; ss -ltn | grep ':8000'"); err != nil {
		t.Fatalf("could not start a listener to test against: %v", err)
	}

	if !portAnswers(srv.IP(), 22) {
		t.Error("port 22 is closed — the firewall shut the one port it must not")
	}
	if portAnswers(srv.IP(), 8000) {
		t.Error("port 8000 answers from the internet — the firewall is not doing its job")
	}
	// Proof that the check above tested the firewall and not a missing server.
	if out, err := r.run(ctx, "curl -fsS -m 3 http://127.0.0.1:8000/ >/dev/null && echo local-ok"); err != nil || !strings.Contains(out, "local-ok") {
		t.Errorf("the listener was never up, so the port check proved nothing: %v %s", err, out)
	}
	t.Log("ssh open, 8000 closed from outside while answering from inside")
}

// TestLiveListAndDown exercises the two commands that work against Hetzner
// rather than against the box: what the list says, and whether a destroy really
// takes the firewall with it.
func TestLiveListAndDown(t *testing.T) {
	requireLiveBox(t)
	ctx := context.Background()
	api, srv, _, project := liveBox(t, "lifecycle")

	var listed strings.Builder
	if err := List(ctx, project, &listed); err != nil {
		t.Fatalf("List: %v", err)
	}
	t.Logf("chief box list:\n%s", listed.String())
	// The location is the field that decoded empty for a while, and it takes the
	// cost down with it: the price is looked up by location.
	for _, want := range []string{srv.Name, srv.Type.Name, DefaultLocation, "this project"} {
		if !strings.Contains(listed.String(), want) {
			t.Errorf("the list does not mention %q", want)
		}
	}
	if strings.Contains(listed.String(), "cost unknown") {
		t.Error("the list could not price a box that is running right now")
	}

	var downOut strings.Builder
	if err := Down(ctx, DownOptions{
		BaseDir: project,
		Confirm: func(string) bool { return true },
		Out:     &downOut,
	}); err != nil {
		t.Fatalf("Down: %v", err)
	}
	t.Logf("chief box down:\n%s", downOut.String())

	if _, ok := LoadState(project); ok {
		t.Error("the box record survived the destroy")
	}
	if _, err := os.Stat(knownHostsPath(project)); !os.IsNotExist(err) {
		t.Error("the destroyed box's host key is still on disk")
	}
	if _, found, err := api.findServer(ctx, srv.Name); err == nil && found {
		t.Errorf("the server %s still exists", srv.Name)
	}
	// The shared firewall must survive: the next box is created behind it, and
	// deleting it here is what the per-box design kept failing to do.
	fws, err := api.firewalls(ctx, chiefLabel)
	if err != nil {
		t.Fatal(err)
	}
	var kept bool
	for _, f := range fws {
		if f.Name == SharedFirewallName {
			kept = true
		}
		if f.Name == srv.Name {
			t.Errorf("a firewall was created per box after all: %s", f.Name)
		}
	}
	if !kept {
		t.Error("the destroy took the shared firewall with it — the next box would have none")
	}
	t.Log("server, state and host key gone; the shared firewall kept")
}

// portAnswers reports whether a TCP port on the box answers from this machine.
// A firewalled port times out rather than refusing, so this needs a deadline
// rather than trusting connect to return.
func portAnswers(ip string, port int) bool {
	return exec.Command("nc", "-z", "-G", "6", "-w", "6", ip, fmt.Sprint(port)).Run() == nil
}

// TestLiveBoxIsBuiltToTheProject creates a box from what discovery reads out
// of a real project on this machine and checks, on the box, that everything the
// profile promised is actually there: the PHP series and its extensions, the
// package manager, the database with its account and database, Redis, the
// browser libraries. It is the only way to know the PPA has the packages, the
// installers still work unattended, and the SQL runs.
//
//	CHIEF_LIVE_BOX_TEST=1 CHIEF_LIVE_PROJECT=~/Herd/agency-os go test ./internal/box/ -run TestLiveBoxIsBuiltToTheProject -v -timeout 25m
func TestLiveBoxIsBuiltToTheProject(t *testing.T) {
	requireLiveBox(t)
	dir := os.Getenv("CHIEF_LIVE_PROJECT")
	if dir == "" {
		t.Skip("set CHIEF_LIVE_PROJECT to the project the box should be built for")
	}
	ctx := context.Background()

	profile := Discover(dir, DiscoverOptions{})
	for _, line := range profile.Summary() {
		t.Log(line)
	}

	// CHIEF_LIVE_BREAK=1 sabotages one provisioning step, to prove that a
	// failure stops the script and reaches chief as the failed marker rather
	// than as a ready box with a tool missing — which is what happened before.
	if os.Getenv("CHIEF_LIVE_BREAK") == "1" {
		profile.PackageManager, profile.PackageManagerVersion = "pnpm", "0.0.0-no-such-version"
		if profile.Node == "" {
			profile.Node = nodeMajor
		}
		t.Log("sabotaged: pnpm pinned to a version that does not exist")
	}

	// The project's name in the box's, so two of these started in the same
	// second for different projects do not collide on Hetzner's unique names.
	purpose := serverName(dir, "probe")
	purpose = strings.TrimPrefix(purpose[:len(purpose)-7], "chief-") // liveBoxFor adds its own time
	_, srv, host, project := liveBoxFor(t, purpose, profile)
	state, _ := LoadState(project)
	root := remote{user: "root", host: srv.IP(), knownHosts: knownHostsFor(project, state)}
	if err := root.waitReachable(ctx, 4*time.Minute); err != nil {
		t.Fatalf("never reachable: %v", err)
	}
	started := time.Now()
	err := root.waitProvisioned(ctx, readyMarker, failedMarker, provisionTimeout(profile))
	if os.Getenv("CHIEF_LIVE_BREAK") == "1" {
		if err == nil || !strings.Contains(err.Error(), "failed") {
			t.Fatalf("the sabotaged box was reported as %v after %s, want the failed marker", err, time.Since(started).Round(time.Second))
		}
		if out, runErr := root.run(ctx, "test -f "+readyMarker+" && echo ready-exists; ls "+failedMarker); runErr == nil {
			t.Logf("markers: %s", strings.ReplaceAll(out, "\n", " "))
		}
		t.Logf("the failure reached chief after %s: %v", time.Since(started).Round(time.Second), err)
		return
	}
	if err != nil {
		if log, logErr := root.run(ctx, "tail -40 /var/log/cloud-init-output.log"); logErr == nil {
			t.Logf("last lines of the provisioning log:\n%s", log)
		}
		t.Fatalf("provisioning did not finish: %v", err)
	}
	t.Logf("provisioned in %s", time.Since(started).Round(time.Second))
	_ = host

	// Everything below runs as the user the run will run as, because that is
	// who has to be able to reach all of it.
	user := remote{user: remoteUser, host: srv.IP(), knownHosts: knownHostsFor(project, state)}
	check := func(name, script, want string) {
		t.Helper()
		out, err := user.run(ctx, script)
		if err != nil {
			t.Errorf("%s: %v\n%s", name, err, out)
			return
		}
		if want != "" && !strings.Contains(strings.ToLower(out), strings.ToLower(want)) {
			t.Errorf("%s: want %q in:\n%s", name, want, out)
			return
		}
		t.Logf("%s: %s", name, firstLines(out, 2))
	}

	check("claude", "claude --version", "")
	check("gh", "gh --version", "gh version")

	if profile.PHP != "" {
		check("php", "php -v", "PHP "+profile.PHP)
		check("composer", "composer --version", "Composer")
		// php -m names modules rather than packages; the two differ where one
		// package carries several modules.
		modules := map[string]string{"xml": "dom", "mysql": "mysqli", "sqlite3": "sqlite3", "gd": "gd"}
		for _, ext := range profile.Extensions {
			module := ext
			if m, ok := modules[ext]; ok {
				module = m
			}
			check("extension "+ext, "php -m | grep -ix "+shellQuote(module), module)
		}
	}
	if profile.Node != "" {
		check("node", "node -v", "v"+profile.Node+".")
	}
	switch profile.PackageManager {
	case "bun":
		check("bun", "bun --version", "")
	case "pnpm", "yarn":
		check(profile.PackageManager, profile.PackageManager+" --version", "")
	}

	// The database, reached the way the rewritten .env would reach it: over
	// TCP, as the box's account, into the project's database.
	switch profile.Database {
	case "pgsql":
		check("postgres", "PGPASSWORD=chief psql -h 127.0.0.1 -U chief -d "+shellQuote(profile.DatabaseName)+" -tAc 'select current_database()'", profile.DatabaseName)
		check("postgres superuser", "PGPASSWORD=chief psql -h 127.0.0.1 -U chief -d postgres -tAc 'create database chief_probe' && echo created", "created")
	case "mysql":
		check("mariadb", "mariadb -h 127.0.0.1 -u chief -pchief "+shellQuote(profile.DatabaseName)+" -Nse 'select database()'", profile.DatabaseName)
		check("mariadb grants", "mariadb -h 127.0.0.1 -u chief -pchief -Nse 'create database chief_probe' && echo created", "created")
	}
	if profile.Redis {
		check("redis", "redis-cli -h 127.0.0.1 ping", "PONG")
	}
	if profile.Meilisearch {
		check("meilisearch", "curl -fsS http://127.0.0.1:7700/health", "available")
	}
	if profile.Browser {
		// One of the libraries Chromium cannot start without, and that nothing
		// but install-deps brings.
		check("browser libraries", "dpkg -s libnss3 | grep -i '^Status'", "installed")
	}
	if profile.Chrome {
		check("chrome", "google-chrome --version", "Google Chrome")
	}
	if profile.Go != "" {
		check("go", "go version", "go"+profile.Go)
	}

	// And the .env translation, end to end: a file shaped like the project's
	// goes over with the same rewrite Up applies, and the values in it have to
	// open a connection from the box.
	if profile.Database == "pgsql" || profile.Database == "mysql" {
		local := filepath.Join(project, ".env")
		if err := os.WriteFile(local, []byte("APP_NAME=probe\nDB_CONNECTION="+profile.Database+"\nDB_HOST=db.laptop.local\nDB_PORT=54329\nDB_DATABASE="+profile.DatabaseName+"\nDB_USERNAME=ben\nDB_PASSWORD=secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		changed, err := sendEnv(ctx, user, local, "/home/chief/probe.env", profile)
		if err != nil {
			t.Fatalf("sendEnv: %v", err)
		}
		t.Logf(".env keys rewritten: %s", strings.Join(changed, ", "))
		connect := "PGPASSWORD=$DB_PASSWORD psql -h $DB_HOST -p $DB_PORT -U $DB_USERNAME -d $DB_DATABASE -tAc 'select 1'"
		if profile.Database == "mysql" {
			connect = "mariadb -h $DB_HOST -P $DB_PORT -u $DB_USERNAME -p$DB_PASSWORD $DB_DATABASE -Nse 'select 1'"
		}
		check(".env opens the database", "set -a; . /home/chief/probe.env; set +a; "+connect, "1")
	}
}

// liveBoxFor is liveBox with a profile, so the box is built the way a project
// would have it rather than to the base.
func liveBoxFor(t *testing.T, purpose string, profile Profile) (*hetzner, server, hostKey, string) {
	t.Helper()
	return liveBoxWith(t, purpose, cloudInitOptions{Profile: profile})
}

// liveBoxWith is liveBox built from any cloud-config options; the hostname and
// the host key are filled in here.
func liveBoxWith(t *testing.T, purpose string, opts cloudInitOptions) (*hetzner, server, hostKey, string) {
	t.Helper()
	ctx := context.Background()

	token, err := resolveHetznerToken()
	if err != nil {
		t.Skipf("no Hetzner token: %v", err)
	}
	api := newHetzner(token)

	host, err := generateHostKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ensureSSHKey(ctx, api, reporter{out: os.Stderr})
	if err != nil {
		t.Fatal(err)
	}
	fw, _, err := api.ensureFirewall(ctx)
	if err != nil {
		t.Fatal(err)
	}
	machine := cheapestType(t, api)
	name := fmt.Sprintf("chief-%s-%s", purpose, time.Now().Format("150405"))
	srv, err := api.createServer(ctx, createServerOpts{
		Name: name, Type: machine, Image: DefaultImage, Location: DefaultLocation,
		SSHKeyID: key.ID, FirewallID: fw.ID,
		UserData: cloudInit(withIdentity(opts, name, host)),
		Labels:   map[string]string{"managed-by": "chief"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("created %s (%s, %s) at %s", srv.Name, machine, DefaultImage, srv.IP())

	project := t.TempDir()
	state := State{
		ServerID: srv.ID, Name: srv.Name, IP: srv.IP(), PRD: "probe",
		Type: machine, Location: DefaultLocation,
		HostKey: host.Public, Created: time.Now(),
	}
	if err := SaveState(project, state); err != nil {
		t.Fatal(err)
	}
	if _, err := writeKnownHosts(project, srv.IP(), host.Public); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := api.deleteServer(context.Background(), srv.ID); err != nil {
			t.Errorf("SERVER %d (%s) NOT DELETED: %v", srv.ID, srv.Name, err)
		} else {
			t.Logf("destroyed %s", srv.Name)
		}
	})
	return api, srv, host, project
}

// cheapestType is the least machine Hetzner will sell in the default location
// right now, which is also what a box created without a --type runs on. The
// test goes through the same code as the product so that a bug in choosing the
// machine shows up here rather than on somebody's real run.
func cheapestType(t *testing.T, api *hetzner) string {
	t.Helper()
	c, err := api.catalog(context.Background())
	if err != nil {
		t.Logf("could not read the catalogue (%v); using %s", err, FallbackType)
		return FallbackType
	}
	st, ok := c.Cheapest(DefaultLocation)
	if !ok {
		return FallbackType
	}
	return st.Name
}

func withIdentity(opts cloudInitOptions, name string, host hostKey) cloudInitOptions {
	opts.Hostname, opts.HostKey = name, host
	return opts
}

// TestLiveStartAt proves what --at leans on systemd for: a transient timer that
// starts the run at a wall-clock time given in UTC, a second schedule that
// replaces the first instead of adding to it, and a deadline drop-in that
// really replaces "hours after boot" — checked by letting it fire and stop the
// run.
func TestLiveStartAt(t *testing.T) {
	requireLiveBox(t)
	ctx := context.Background()
	// Built to destroy itself, so the deadline unit exists; there are no reap
	// credentials on it, so the reaper only says so and leaves the box alone.
	_, srv, _, project := liveBoxWith(t, "startat", cloudInitOptions{SelfDestruct: true})

	state, _ := LoadState(project)
	root := remote{user: "root", host: srv.IP(), knownHosts: knownHostsFor(project, state)}
	if err := root.waitReachable(ctx, 4*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := root.waitFile(ctx, readyMarker, provisionTimeout(Profile{})); err != nil {
		t.Fatalf("provisioning did not finish: %v", err)
	}
	// A stand-in for chief that does nothing but last, so "is the run going" has
	// a clear answer.
	if _, err := root.run(ctx, "printf '#!/bin/sh\\nexec sleep 900\\n' > /usr/local/bin/chief && chmod 755 /usr/local/bin/chief && "+
		"install -d -o chief -g chief /home/chief/project"); err != nil {
		t.Fatal(err)
	}
	unit := "chief-run@" + shellQuote("probe")
	isActive := func() string {
		out, _ := root.run(ctx, "systemctl is-active "+unit)
		return strings.TrimSpace(out)
	}

	// Nothing scheduled yet: cancelling has to be harmless.
	if out, err := root.run(ctx, cancelStartScript()); err != nil {
		t.Fatalf("cancelling nothing failed: %v\n%s", err, out)
	}
	// A first schedule far out, then the one that counts: only the second may
	// be armed afterwards.
	if out, err := root.run(ctx, scheduleStartScript("probe", time.Now().Add(time.Hour))); err != nil {
		t.Fatalf("scheduling: %v\n%s", err, out)
	}
	at := time.Now().Add(90 * time.Second).Truncate(time.Second)
	if out, err := root.run(ctx, cancelStartScript()+"; "+scheduleStartScript("probe", at)); err != nil {
		t.Fatalf("rescheduling: %v\n%s", err, out)
	}
	next, _ := root.run(ctx, "systemctl show chief-start.timer -p NextElapseUSecRealtime --value; date -u")
	t.Logf("armed: %s", strings.ReplaceAll(strings.TrimSpace(next), "\n", " · now "))
	if got := isActive(); unitRunning(got) {
		t.Fatalf("the run started before its time: %s", got)
	}

	// The deadline, moved to shortly after the start.
	deadline := at.Add(2 * time.Minute)
	if err := root.runWith(ctx, moveDeadlineScript, deadlineDropIn(deadline)); err != nil {
		t.Fatalf("moving the deadline: %v", err)
	}
	timers, _ := root.run(ctx, "systemctl show chief-deadline.timer -p TimersMonotonic -p TimersCalendar")
	t.Logf("deadline timer: %s", strings.TrimSpace(timers))
	if strings.Contains(timers, "OnBootUSec") {
		t.Errorf("the boot-based deadline is still there:\n%s", timers)
	}

	for time.Now().Before(at.Add(-5 * time.Second)) {
		time.Sleep(5 * time.Second)
	}
	if got := isActive(); unitRunning(got) {
		t.Fatalf("the run started early: %s", got)
	}
	time.Sleep(20 * time.Second)
	if got := isActive(); !unitRunning(got) {
		log, _ := root.run(ctx, "journalctl -u chief-start.service -u chief-start.timer -u "+unit+" --no-pager | tail -20")
		t.Fatalf("the run did not start at %s: %s\n%s", at.Format(time.TimeOnly), got, log)
	}
	started, _ := root.run(ctx, "systemctl show "+unit+" -p ExecMainStartTimestamp --value")
	t.Logf("the run started: %s (scheduled %s UTC)", strings.TrimSpace(started), at.UTC().Format(time.TimeOnly))
	if n, _ := root.run(ctx, "systemctl show "+unit+" -p NRestarts -p InvocationID --value"); n != "" {
		t.Logf("invocation: %s", strings.ReplaceAll(strings.TrimSpace(n), "\n", " "))
	}

	for time.Now().Before(deadline.Add(30 * time.Second)) {
		time.Sleep(5 * time.Second)
	}
	if got := isActive(); unitRunning(got) {
		t.Fatalf("the moved deadline did not stop the run: %s", got)
	}
	reap, _ := root.run(ctx, "journalctl -u chief-deadline.service --no-pager -o cat | tail -5")
	t.Logf("the deadline stopped the run:\n%s", strings.TrimSpace(reap))
	// The first, cancelled schedule must not have come back.
	if left, _ := root.run(ctx, "systemctl list-timers --all --no-pager chief-start.timer | head -3"); strings.Contains(left, "chief-start.timer") {
		t.Errorf("a start is still armed after the run:\n%s", left)
	}
}
