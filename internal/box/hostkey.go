package box

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// hostKey is the SSH host identity of one box: the key the machine proves
// itself with, and the public half chief checks it against.
//
// It exists because of what the first connection to a box carries. Within
// seconds of the machine booting, chief sends it the Claude OAuth token, a
// GitHub token that can push to the project, and the project's .env. Accepting
// whatever host key answers — which is what StrictHostKeyChecking=no means —
// hands all of that to anyone who can answer first.
//
// The usual reason to accept blindly is that a brand new machine's key cannot
// be known in advance. That is only true if you let the machine invent it: the
// key is generated here and put into the instance through cloud-init, so chief
// knows the fingerprint before the box has booted, before it has an address,
// before there is anything to impersonate.
type hostKey struct {
	// Private is the OpenSSH private key, which goes into the cloud-config and
	// nowhere else.
	Private string
	// Public is the "ssh-ed25519 AAAA..." line, which goes into the project's
	// known_hosts and into the box's state.
	Public string
}

// generateHostKey makes a fresh host identity for one box.
//
// ssh-keygen does the work rather than crypto/ed25519, for the same reason
// remote shells out to ssh: the OpenSSH private key format is a container
// format with its own padding and check-word rules, and hand-rolling a writer
// for it would add a dependency or a bug to save calling a binary that is
// already required for any of this to work at all.
func generateHostKey(ctx context.Context) (hostKey, error) {
	dir, err := os.MkdirTemp("", "chief-hostkey-")
	if err != nil {
		return hostKey{}, err
	}
	// The private key is on this disk for the length of this function. Removing
	// the directory is the only thing that has to happen on every path out.
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "key")
	cmd := exec.CommandContext(ctx, "ssh-keygen",
		"-t", "ed25519",
		"-N", "", // no passphrase: sshd has nobody to ask for one at boot
		"-C", "chief box",
		"-f", path,
		"-q",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return hostKey{}, fmt.Errorf("generating the box's host key: %w: %s", err, firstLines(string(out), 3))
	}

	private, err := os.ReadFile(path) //nolint:gosec // a file this function just created in its own temp dir
	if err != nil {
		return hostKey{}, err
	}
	public, err := os.ReadFile(path + ".pub") //nolint:gosec // same
	if err != nil {
		return hostKey{}, err
	}
	return hostKey{
		Private: strings.TrimRight(string(private), "\n") + "\n",
		Public:  strings.TrimSpace(string(public)),
	}, nil
}

// knownHostsPath is where a project keeps the host key of its current box.
//
// Per project rather than in ~/.ssh/known_hosts, because a box lives for hours
// and its address is recycled afterwards. Writing throwaway addresses into the
// file that guards someone's real servers means that file slowly fills with
// entries for machines that no longer exist, and one of those entries will one
// day match a host they actually care about.
func knownHostsPath(baseDir string) string {
	return filepath.Join(stateDir(baseDir), "known_hosts")
}

// writeKnownHosts records that this address is that key, and returns the file
// ssh should be pointed at.
func writeKnownHosts(baseDir, ip, publicKey string) (string, error) {
	path := knownHostsPath(baseDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("recording the box's host key: %w", err)
	}
	line := fmt.Sprintf("%s %s\n", ip, strings.TrimSpace(publicKey))
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		return "", fmt.Errorf("recording the box's host key: %w", err)
	}
	return path, nil
}

// knownHostsFor returns the file to check a box against, rebuilding it from the
// state when it has gone missing.
//
// Rebuilding matters because the file lives under .chief/box, which people
// delete: a project that was cleaned up between `up` and `logs` would otherwise
// be unreachable, and the fix — the key itself — is right there in the state
// that was just loaded.
//
// A box created before host keys existed has none recorded, and gets "", which
// the caller reads as the old permissive behaviour. That is not a security
// decision so much as an honest one: the box is already running and already
// holds the secrets, and refusing to show its log now protects nothing.
func knownHostsFor(baseDir string, s State) string {
	if strings.TrimSpace(s.HostKey) == "" {
		return ""
	}
	path := knownHostsPath(baseDir)
	if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), s.HostKey) { //nolint:gosec // a fixed path under the project's own .chief
		return path
	}
	rebuilt, err := writeKnownHosts(baseDir, s.IP, s.HostKey)
	if err != nil {
		return ""
	}
	return rebuilt
}
