package box

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	name := fmt.Sprintf("chief-%s-%s", purpose, time.Now().Format("150405"))
	srv, err := api.createServer(ctx, createServerOpts{
		Name: name, Type: DefaultType, Image: DefaultImage, Location: DefaultLocation,
		SSHKeyID: key.ID, FirewallID: fw.ID,
		UserData: cloudInit(cloudInitOptions{Hostname: name, HostKey: host}),
		Labels:   map[string]string{"managed-by": "chief"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("created %s (%s) at %s", srv.Name, DefaultType, srv.IP())

	project := t.TempDir()
	state := State{
		ServerID: srv.ID, Name: srv.Name, IP: srv.IP(), PRD: "probe",
		Type: DefaultType, Location: DefaultLocation,
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
	if err := root.waitFile(ctx, readyMarker, provisionTimeout); err != nil {
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
	for _, want := range []string{srv.Name, DefaultType, DefaultLocation, "this project"} {
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
