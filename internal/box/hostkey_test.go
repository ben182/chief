package box

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGenerateHostKeyMakesAUsablePair(t *testing.T) {
	k, err := generateHostKey(context.Background())
	if err != nil {
		t.Fatalf("generateHostKey: %v", err)
	}
	if !strings.HasPrefix(k.Private, "-----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Errorf("private key is not in OpenSSH format:\n%s", firstLines(k.Private, 1))
	}
	if !strings.HasSuffix(k.Private, "\n") {
		t.Error("the private key must end in a newline, or sshd rejects the file it lands in")
	}
	if !strings.HasPrefix(k.Public, "ssh-ed25519 ") {
		t.Errorf("public key = %q, want an ed25519 key", firstLines(k.Public, 1))
	}
}

func TestGenerateHostKeyLeavesNothingOnDisk(t *testing.T) {
	// The private key of a machine that has not been created yet should not be
	// recoverable from the temp directory of the laptop that made it.
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), "chief-hostkey-*"))
	if _, err := generateHostKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, _ := filepath.Glob(filepath.Join(os.TempDir(), "chief-hostkey-*"))
	if len(after) > len(before) {
		t.Errorf("left %d temp director(ies) behind", len(after)-len(before))
	}
}

func TestGeneratedKeysDifferPerBox(t *testing.T) {
	a, err := generateHostKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, err := generateHostKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if a.Public == b.Public {
		t.Error("two boxes were given the same identity")
	}
}

func TestCloudInitCarriesTheHostKey(t *testing.T) {
	k, err := generateHostKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := cloudInit(cloudInitOptions{Hostname: "chief-demo", HostKey: k})

	var parsed struct {
		DeleteKeys bool     `yaml:"ssh_deletekeys"`
		GenTypes   []string `yaml:"ssh_genkeytypes"`
		SSHKeys    struct {
			Private string `yaml:"ed25519_private"`
			Public  string `yaml:"ed25519_public"`
		} `yaml:"ssh_keys"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("not valid YAML with a host key in it: %v", err)
	}

	// The block scalar has to survive the round trip byte for byte. An off-by-one
	// in the indentation yields a key that parses and does not work, which shows
	// up as every connection to the box being refused.
	if parsed.SSHKeys.Private != k.Private {
		t.Errorf("the private key did not survive rendering:\ngot  %q\nwant %q",
			firstLines(parsed.SSHKeys.Private, 2), firstLines(k.Private, 2))
	}
	if parsed.SSHKeys.Public != k.Public {
		t.Errorf("public key = %q, want %q", parsed.SSHKeys.Public, k.Public)
	}
	if !parsed.DeleteKeys {
		t.Error("ssh_deletekeys is off, so the instance keeps identities chief does not know")
	}
	if len(parsed.GenTypes) != 1 || parsed.GenTypes[0] != "ed25519" {
		t.Errorf("ssh_genkeytypes = %v, want only ed25519", parsed.GenTypes)
	}
}

func TestCloudInitWithoutAHostKeyRendersNone(t *testing.T) {
	out := cloudInit(cloudInitOptions{Hostname: "chief-demo"})
	if strings.Contains(out, "ssh_keys:") {
		t.Error("rendered a host key section for a box that was given no key")
	}
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("not valid YAML: %v", err)
	}
}

func TestKnownHostsPinsTheAddressToTheKey(t *testing.T) {
	dir := t.TempDir()
	path, err := writeKnownHosts(dir, "203.0.113.7", "ssh-ed25519 AAAAC3Nz key")
	if err != nil {
		t.Fatalf("writeKnownHosts: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "203.0.113.7 ssh-ed25519 AAAAC3Nz key" {
		t.Errorf("known_hosts = %q", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("known_hosts mode = %o, want 600", perm)
	}
}

func TestKnownHostsForRebuildsADeletedFile(t *testing.T) {
	dir := t.TempDir()
	s := State{IP: "203.0.113.7", HostKey: "ssh-ed25519 AAAAC3Nz key"}

	path := knownHostsFor(dir, s)
	if path == "" {
		t.Fatal("no known_hosts for a box that has a key")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	// Somebody cleaned out .chief/box. The key is in the state, so the file can
	// be put back rather than the box becoming unreachable.
	again := knownHostsFor(dir, s)
	if again == "" {
		t.Fatal("did not rebuild the known_hosts file")
	}
	data, err := os.ReadFile(again)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), s.HostKey) {
		t.Errorf("rebuilt file does not hold the key: %q", string(data))
	}
}

func TestKnownHostsForABoxWithoutAKey(t *testing.T) {
	// A box created before chief pinned host keys. It has to stay reachable.
	if got := knownHostsFor(t.TempDir(), State{IP: "203.0.113.7"}); got != "" {
		t.Errorf("knownHostsFor = %q, want empty so the old permissive mode applies", got)
	}
}

func TestSSHArgsCheckThePinnedKey(t *testing.T) {
	args := strings.Join(remote{user: "chief", host: "203.0.113.7", knownHosts: "/tmp/kh"}.sshArgs(), " ")
	if !strings.Contains(args, "StrictHostKeyChecking=yes") {
		t.Errorf("a pinned box is not checked strictly: %s", args)
	}
	if !strings.Contains(args, "UserKnownHostsFile=/tmp/kh") {
		t.Errorf("not pointed at the box's own known_hosts: %s", args)
	}
	if strings.Contains(args, "/dev/null") {
		t.Errorf("still throwing the host key away: %s", args)
	}
}

func TestSSHArgsWithoutAPinnedKey(t *testing.T) {
	args := strings.Join(remote{user: "chief", host: "203.0.113.7"}.sshArgs(), " ")
	if !strings.Contains(args, "StrictHostKeyChecking=no") {
		t.Errorf("an unpinned box should still be reachable: %s", args)
	}
	if !strings.Contains(args, "UserKnownHostsFile=/dev/null") {
		t.Errorf("an unpinned box must not write into the user's own known_hosts: %s", args)
	}
}

func TestForgetStateDropsTheHostKey(t *testing.T) {
	dir := t.TempDir()
	if err := SaveState(dir, State{ServerID: 1, Name: "b", IP: "203.0.113.7", HostKey: "ssh-ed25519 AAAA k"}); err != nil {
		t.Fatal(err)
	}
	if _, err := writeKnownHosts(dir, "203.0.113.7", "ssh-ed25519 AAAA k"); err != nil {
		t.Fatal(err)
	}
	if err := ForgetState(dir); err != nil {
		t.Fatalf("ForgetState: %v", err)
	}
	// The address is about to belong to somebody else's machine.
	if _, err := os.Stat(knownHostsPath(dir)); !os.IsNotExist(err) {
		t.Error("the destroyed box's host key is still on disk")
	}
}
