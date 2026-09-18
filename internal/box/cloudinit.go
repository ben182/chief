package box

import (
	"fmt"
	"strings"
)

// What a box is built from when the project says nothing. These are constants
// rather than "whatever is current at the time", because a run has to be
// reproducible and a surprise major version is not a thing to discover four
// hours in.
//
// Keep them current. A box is created fresh for every run and thrown away
// afterwards, so raising one of these costs nothing, and letting them rot means
// every run for months works against a runtime nobody develops against any more.
const (
	// DefaultImage is the Hetzner image name: Ubuntu 24.04 LTS (Noble Numbat).
	//
	// Not the newest LTS, and on purpose. PHP comes from Ondřej Surý's PPA, the
	// one place every series from 8.0 to 8.5 is packaged for the same release
	// under the same names — which is what lets a project be given the PHP its
	// developer's machine runs rather than whichever one an image happens to
	// ship. The PPA publishes for noble; the release after it had no pocket
	// there when this was chosen. Raise this once `dists/<codename>/` exists
	// under https://ppa.launchpadcontent.net/ondrej/php/ubuntu/.
	DefaultImage = "ubuntu-24.04"
	// phpSeries is the PHP a project gets when neither its config, nor this
	// machine, nor its composer.json says otherwise.
	phpSeries = "8.5"
	// nodeMajor is the active Node LTS line, from NodeSource because Ubuntu's
	// own Node is a release behind. Claude Code does not need it — it installs
	// as a native binary — so this is here purely for the projects that do,
	// which for a Laravel app means the asset pipeline.
	nodeMajor = "24"
	// claudeChannel is the Claude Code release channel. "stable" runs about a
	// week behind and skips releases with known major regressions, which is the
	// right side of that trade for a machine that will run unattended for hours
	// with permissions skipped.
	claudeChannel = "stable"
)

// cloudInitOptions are the things about a box that are not the same for every
// one of them.
type cloudInitOptions struct {
	// Hostname is what the box calls itself, which is what the prompt shows when
	// you ssh in wondering which machine you are on.
	Hostname string
	// Profile is what discovery read out of the project: the runtimes and
	// servers this box exists to provide. A zero profile builds the base alone.
	Profile Profile
	// ExtraPackages are apt packages a project asks for on top of what the
	// profile brings.
	ExtraPackages []string
	// HostKey is the SSH host identity the instance should come up with, so the
	// first connection can be checked rather than trusted. A zero value renders
	// no key section and lets the instance invent its own.
	HostKey hostKey
	// SelfDestruct builds the box so that it destroys itself: once its run has
	// finished and the commits are on origin, and in any case once MaxHours have
	// passed since it booted. False renders none of it, and the box then bills
	// until somebody runs 'chief box down'.
	SelfDestruct bool
	// MaxHours is the outside limit for a self-destructing box. Zero takes
	// defaultMaxHours.
	MaxHours int
}

// cloudInit renders the cloud-config that turns a bare Ubuntu instance into one
// a run can happen on.
//
// It installs the runtimes, the servers and the agent CLI the profile asks
// for, and the systemd unit a run is started as. Everything a specific project
// needs beyond that — its dependencies, its schema — belongs in that project's
// `worktree.setup`, which chief runs inside the checkout before the agent
// starts. The split matters because this is baked into the instance when it is
// created and a project's needs change weekly; the profile is read from the
// project at that moment, which is how the two stay in step.
func cloudInit(opts cloudInitOptions) string {
	p := opts.Profile

	var packages strings.Builder
	pkg := func(comment string, names ...string) {
		if comment != "" {
			fmt.Fprintf(&packages, "\n  # %s", comment)
		}
		for _, n := range names {
			if n = strings.TrimSpace(n); n != "" {
				fmt.Fprintf(&packages, "\n  - %s", n)
			}
		}
	}
	switch p.Database {
	case "pgsql":
		pkg("A run's test database is local to the instance; it has no business anywhere else.",
			"postgresql", "postgresql-contrib")
	case "mysql":
		pkg("A run's test database is local to the instance; it has no business anywhere else.",
			"mariadb-server", "mariadb-client")
	}
	if p.Redis {
		pkg("", "redis-server")
	}
	if len(opts.ExtraPackages) > 0 {
		pkg("What the project asked for on top (box.packages).", opts.ExtraPackages...)
	}

	var files strings.Builder
	if p.Database == "mysql" {
		fmt.Fprintf(&files, `
  # The one account the box's database has, matching the .env chief rewrites.
  - path: /etc/chief/mysql-setup.sql
    permissions: "0600"
    content: |
      CREATE USER IF NOT EXISTS '%[1]s'@'localhost' IDENTIFIED BY '%[1]s';
      CREATE USER IF NOT EXISTS '%[1]s'@'127.0.0.1' IDENTIFIED BY '%[1]s';
      GRANT ALL PRIVILEGES ON *.* TO '%[1]s'@'localhost' WITH GRANT OPTION;
      GRANT ALL PRIVILEGES ON *.* TO '%[1]s'@'127.0.0.1' WITH GRANT OPTION;
      CREATE DATABASE IF NOT EXISTS `+"`%[2]s`"+`;
`, remoteDatabaseUser, p.DatabaseName)
	}
	if p.Meilisearch {
		files.WriteString(`
  # Meilisearch in development mode: no master key, bound to the box itself.
  - path: /etc/systemd/system/meilisearch.service
    permissions: "0644"
    content: |
      [Unit]
      Description=Meilisearch
      After=network-online.target

      [Service]
      ExecStart=/usr/local/bin/meilisearch --env development --db-path /var/lib/meilisearch --http-addr 127.0.0.1:7700 --no-analytics
      Restart=on-failure

      [Install]
      WantedBy=multi-user.target
`)
	}

	// Rendered with Sprintf rather than text/template: the substitutions are a
	// hostname and a handful of blocks, and a template would put a layer of
	// {{ }} between this file and the shell it is trying to read as.
	return fmt.Sprintf(`#cloud-config
# Generated by chief (internal/box/cloudinit.go). Provisions a throwaway
# instance to the point where "chief start --headless" can run on it.

hostname: %[1]s
preserve_hostname: false
%[2]s
package_update: true
package_upgrade: false

packages:
  - git
  - curl
  - jq
  - unzip
  - rsync
  - build-essential
  - ripgrep
  - gnupg
  - software-properties-common%[3]s

write_files:
  - path: /etc/sudoers.d/chief
    permissions: "0440"
    content: |
      chief ALL=(ALL) NOPASSWD:ALL

  # Where the tokens land. Created empty and locked down first, so there is no
  # window in which it exists world-readable.
  #
  # deferred, because write_files runs before users exist: without it, cloud-init
  # creates /home/chief as root to hold this file, useradd then finds the
  # directory already there and leaves it alone, and the user ends up locked out
  # of their own home. Everything after that — the clone, the env file, the run —
  # fails with "Permission denied" on a machine that otherwise looks fine.
  - path: /home/chief/.chief-env
    owner: chief:chief
    permissions: "0600"
    defer: true
    content: |
      # filled in by chief once the instance is up

  # The unit a run is started as. One-shot rather than a service that restarts:
  # a chief run that ended has ended, and starting it again from the top is a
  # decision for a person to make.
  - path: /etc/systemd/system/chief-run@.service
    permissions: "0644"
    content: |
      [Unit]
      Description=chief headless run (%%i)
      After=network-online.target postgresql.service mariadb.service redis-server.service meilisearch.service
      Wants=network-online.target%[9]s

      [Service]
      Type=oneshot
      User=chief
      WorkingDirectory=/home/chief/project
      EnvironmentFile=/home/chief/.chief-env
      # A run is measured in hours and must never be cut short by a timeout.
      TimeoutStartSec=infinity
      # systemd stops a unit with SIGTERM, which chief turns into a clean stop:
      # the agent is killed, its commits stay. The grace period is for the agent
      # process tree to actually go away.
      KillSignal=SIGTERM
      TimeoutStopSec=120
      ExecStart=/bin/sh -c '/usr/local/bin/chief start %%i --headless $CHIEF_RUN_FLAGS'
      StandardOutput=journal
      StandardError=journal

      [Install]
      WantedBy=multi-user.target
%[10]s%[4]s
users:
  # Work does not happen as root: the agent runs with permissions skipped, and
  # there is no reason to hand it the whole machine as well.
  - name: chief
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    ssh_authorized_keys: []

runcmd:
  # The home has to belong to its user before anything is written into it. This
  # repeats what the deferred write above already arranges, because the cost of
  # being wrong here is a box that provisions perfectly and then cannot be worked
  # in.
  - chown chief:chief /home/chief
  # cloud-init runs these as one shell script and, by default, carries on past
  # a failing line. That was found out the hard way: a Bun download answered
  # 504, the "test -x" meant to catch it failed as well, and the ready marker
  # was written anyway — a box reported as provisioned with a tool missing.
  # From here on the first failure stops the script, and the trap leaves the
  # failed marker so chief stops waiting at once instead of at the timeout.
  - set -e
  - trap 'test -f /var/lib/cloud/chief-ready || touch /var/lib/cloud/chief-failed' EXIT
  # Give the chief user the key the instance was created with, so nothing has to
  # run as root to get work done.
  - mkdir -p /home/chief/.ssh
  - cp /root/.ssh/authorized_keys /home/chief/.ssh/authorized_keys
  - chown -R chief:chief /home/chief/.ssh
  - chmod 700 /home/chief/.ssh
  - chmod 600 /home/chief/.ssh/authorized_keys

  # Claude Code, from Anthropic's signed apt repository rather than the curl
  # installer or npm: unattended provisioning should not pipe a downloaded
  # script into a shell when a signed package is on offer. The key's fingerprint
  # is 31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE.
  - install -d -m 0755 /etc/apt/keyrings
  - curl -fsSL https://downloads.claude.ai/keys/claude-code.asc -o /etc/apt/keyrings/claude-code.asc
  - gpg --show-keys --with-colons /etc/apt/keyrings/claude-code.asc | grep -q 31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE
  - echo "deb [signed-by=/etc/apt/keyrings/claude-code.asc] https://downloads.claude.ai/claude-code/apt/%[5]s %[5]s main" > /etc/apt/sources.list.d/claude-code.list

  # The GitHub CLI, for the pull request a finished run opens.
  - curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /etc/apt/keyrings/githubcli.gpg
  - chmod 644 /etc/apt/keyrings/githubcli.gpg
  - echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list
%[6]s
  - apt-get update
  - apt-get install -y claude-code gh%[7]s
%[8]s
  - systemctl daemon-reload
%[11]s  # The marker chief waits for. Written last, so its existence means every step
  # above finished.
  - touch /var/lib/cloud/chief-ready

final_message: "chief box ready after $UPTIME seconds"
`,
		opts.Hostname,
		hostKeySection(opts.HostKey),
		packages.String(),
		files.String(),
		claudeChannel,
		repositorySteps(p),
		aptInstallList(p),
		toolSteps(p),
		reaperHook(opts),
		reaperFiles(opts),
		reaperSteps(opts),
	)
}

// curlRetry is what every download of a binary gets. Release CDNs answer with
// the occasional 504, and without --retry-all-errors curl treats that as a
// final answer rather than a transient one.
const curlRetry = "--retry 5 --retry-all-errors --retry-delay 3"

// repositorySteps adds the package sources the profile needs before the one
// apt-get install below can find them.
func repositorySteps(p Profile) string {
	var b strings.Builder
	if p.PHP != "" {
		fmt.Fprintf(&b, `
  # PHP %[1]s from Ondřej Surý's PPA, which packages every series under one
  # naming scheme; -n skips the update it would run, since one follows anyway.
  - add-apt-repository -y -n ppa:ondrej/php
`, p.PHP)
	}
	if p.Node != "" {
		fmt.Fprintf(&b, `
  # Node %[1]s, from NodeSource.
  - curl -fsSL https://deb.nodesource.com/setup_%[1]s.x | bash -
`, p.Node)
	}
	return b.String()
}

// aptInstallList is what goes after "apt-get install -y claude-code gh": the
// runtime packages the repositories above made available.
func aptInstallList(p Profile) string {
	var names []string
	if p.Node != "" {
		names = append(names, "nodejs")
	}
	if p.PHP != "" {
		names = append(names, "php"+p.PHP+"-cli")
		for _, e := range p.Extensions {
			names = append(names, "php"+p.PHP+"-"+e)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return " " + strings.Join(names, " ")
}

// toolSteps installs what apt does not carry, and sets up the servers apt did.
func toolSteps(p Profile) string {
	var b strings.Builder

	if p.PHP != "" {
		fmt.Fprintf(&b, `
  # Composer. HOME is set explicitly: runcmd runs without one, and the installer
  # refuses to do anything without it ("The HOME or COMPOSER_HOME environment
  # variable must be set"), which it reports on stdout while still exiting 0 —
  # so the failure is silent and composer is simply absent later.
  - curl -fsSL %[1]s https://getcomposer.org/installer -o /tmp/composer-setup.php
  - HOME=/root php /tmp/composer-setup.php --install-dir=/usr/local/bin --filename=composer
  - rm -f /tmp/composer-setup.php
  # And a check, so a silent failure cannot reach the ready marker.
  - test -x /usr/local/bin/composer
`, curlRetry)
	}

	switch p.PackageManager {
	case "bun":
		fmt.Fprintf(&b, `
  # Bun, because that is what the lockfile was written by — the release zip
  # rather than the install script, which downloads the same file without
  # retrying and once met a 504 from GitHub. Into /usr/local so it is on PATH
  # for the run's user as well as for root.
  - curl -fsSL %[1]s https://github.com/oven-sh/bun/releases/latest/download/bun-linux-x64.zip -o /tmp/bun.zip
  - unzip -oq /tmp/bun.zip -d /tmp/bun && install -m 0755 /tmp/bun/bun-linux-x64/bun /usr/local/bin/bun
  - rm -rf /tmp/bun /tmp/bun.zip
  - test -x /usr/local/bin/bun
`, curlRetry)
	case "pnpm", "yarn":
		if p.PackageManagerVersion != "" {
			fmt.Fprintf(&b, `
  # %[1]s %[2]s, the version package.json pins, through corepack.
  - npm install -g corepack
  - corepack enable
  - corepack prepare %[1]s@%[2]s --activate
`, p.PackageManager, p.PackageManagerVersion)
		} else {
			fmt.Fprintf(&b, `
  # %[1]s, because that is what the lockfile was written by.
  - npm install -g %[1]s
`, p.PackageManager)
		}
	}

	if p.Browser {
		b.WriteString(`
  # The system libraries a headless Chromium needs. The browser itself is the
  # project's to install — its version has to match the project's Playwright —
  # but the libraries are the machine's, and no package manager brings them.
  - npx --yes playwright install-deps chromium
`)
	}
	if p.Chrome {
		fmt.Fprintf(&b, `
  # Dusk drives Google Chrome itself rather than a bundled Chromium.
  - curl -fsSL %[1]s https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb -o /tmp/chrome.deb
  - apt-get install -y /tmp/chrome.deb
  - rm -f /tmp/chrome.deb
`, curlRetry)
	}

	if p.Go != "" {
		fmt.Fprintf(&b, `
  # Go %[1]s, the toolchain go.mod asks for, from the official tarball.
  - curl -fsSL %[2]s https://go.dev/dl/go%[1]s.linux-amd64.tar.gz -o /tmp/go.tgz
  - rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tgz && rm -f /tmp/go.tgz
  - ln -sf /usr/local/go/bin/go /usr/local/bin/go
  - ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
`, p.Go, curlRetry)
	}

	if p.Meilisearch {
		fmt.Fprintf(&b, `
  # Meilisearch, for Scout. The installer drops the binary where it runs.
  - cd /tmp && curl -fsSL %[1]s https://install.meilisearch.com | sh
  - install -m 0755 /tmp/meilisearch /usr/local/bin/meilisearch
  - systemctl enable --now meilisearch
`, curlRetry)
	}

	switch p.Database {
	case "pgsql":
		fmt.Fprintf(&b, `
  # A database superuser named after the account that will use it, so a project's
  # setup script can create and drop its own databases without a password, and
  # the database the project's .env names.
  - su - postgres -c "psql -c \"CREATE ROLE %[1]s WITH LOGIN SUPERUSER PASSWORD '%[1]s'\"" || true
  - su - postgres -c "createdb -O %[1]s %[2]s" || true
`, remoteDatabaseUser, p.DatabaseName)
	case "mysql":
		b.WriteString(`
  # The account and database the project's .env is rewritten to name.
  - mariadb < /etc/chief/mysql-setup.sql
`)
	}

	return b.String()
}

// What a self-destructing box waits for before it goes.
const (
	// DefaultMaxHours is the outside limit on a box's life. It is measured from
	// boot rather than from the start of the run because that is the clock that
	// keeps running when everything else has stopped — including a run that
	// never started because provisioning hung.
	//
	// Twelve hours covers a long night's run with room to spare, and it is short
	// enough that a box forgotten before bedtime is gone before the morning
	// rather than still billing at lunchtime.
	DefaultMaxHours = 12
	// reapGrace is how long a finished box waits before destroying itself, so
	// that somebody who was watching the log as it ended has a moment to look
	// around on the machine.
	reapGrace = "20min"
	// reapRetry is how long the box waits before trying again after a reap it
	// refused to carry out, which happens only when commits on it are still
	// nowhere else.
	reapRetry = "1h"
)

// maxHours is the limit to build a box with, in hours.
func maxHours(opts cloudInitOptions) int {
	if opts.MaxHours > 0 {
		return opts.MaxHours
	}
	return DefaultMaxHours
}

// reaperHook is the line in the run unit that starts the countdown to the box's
// own destruction. It is OnSuccess rather than OnFailure as well, and that is
// the whole policy: a run that ended with stories unresolved is one somebody
// will want to look at, retry, or ssh into, and the deadline below is what
// stops that box from billing forever. A run that finished has nothing left on
// the machine that is not also on origin.
func reaperHook(opts cloudInitOptions) string {
	if !opts.SelfDestruct {
		return ""
	}
	return "\n      OnSuccess=chief-reap.timer"
}

// reaperFiles renders the script and the units that let a box destroy itself.
//
// The credentials the script needs are not here: they arrive over SSH once the
// instance is up, into a file this script reads. A Hetzner token in the
// cloud-config would be a token in the instance's own metadata, readable by
// anything on the machine that can reach 169.254.169.254 — which is every
// process, without a password.
func reaperFiles(opts cloudInitOptions) string {
	if !opts.SelfDestruct {
		return ""
	}
	return fmt.Sprintf(`
  # What makes a box stop billing without anybody being awake for it.
  #
  # Two things start this: the run unit finishing successfully, and the deadline
  # below. Both go through the same script, which refuses to destroy a machine
  # whose commits are still only on it.
  - path: /usr/local/bin/chief-reap
    permissions: "0755"
    content: |
      #!/bin/sh
      # Destroys this box through the Hetzner API, once what the run built is
      # somewhere other than here.
      set -u
      say() { echo "chief-reap: $1"; }

      if [ ! -f /etc/chief/reap.env ]; then
        say "no credentials — this box was not built to destroy itself"
        exit 0
      fi
      . /etc/chief/reap.env

      # The last thing this machine can do for the run: get its commits off
      # itself. Normally there is nothing to do here, because the run pushes
      # when it ends — this is for the run that was cut short by the deadline,
      # or whose own push failed.
      su - chief -c 'set -a; . ~/.chief-env; set +a; cd ~/project && git push --quiet --all origin' ||
        say "the rescue push did not work"

      left=$(su - chief -c 'cd ~/project && git rev-list --count --branches --not --remotes' 2>/dev/null || echo 0)
      case "$left" in '' | *[!0-9]*) left=0 ;; esac
      if [ "$left" -gt 0 ]; then
        # Destroying the machine now would delete the only copy of this work.
        # The timer tries again later, and 'chief box list' still shows it.
        say "$left commit(s) exist only here — keeping the box, trying again in %[2]s"
        exit 1
      fi

      say "the work is on origin; destroying this box"
      curl -fsS --retry 5 --retry-all-errors --retry-delay 3 -X DELETE \
        -H "Authorization: Bearer $CHIEF_HETZNER_TOKEN" \
        "https://api.hetzner.cloud/v1/servers/$CHIEF_SERVER_ID"

  - path: /etc/systemd/system/chief-reap.service
    permissions: "0644"
    content: |
      [Unit]
      Description=Destroy this box now that its work is on origin

      [Service]
      Type=oneshot
      ExecStart=/usr/local/bin/chief-reap

  # Started by the run unit when the run succeeds, not enabled at boot: a box
  # whose run has not finished has no business destroying itself.
  - path: /etc/systemd/system/chief-reap.timer
    permissions: "0644"
    content: |
      [Unit]
      Description=Countdown to destroying this box

      [Timer]
      OnActiveSec=%[1]s
      OnUnitActiveSec=%[2]s
      AccuracySec=1min
      Unit=chief-reap.service

  # The backstop. A run that hangs never succeeds, so nothing above would ever
  # fire, and the box would bill until somebody remembered it.
  - path: /etc/systemd/system/chief-deadline.service
    permissions: "0644"
    content: |
      [Unit]
      Description=Stop the run and destroy this box after %[3]d hours

      [Service]
      Type=oneshot
      # The run is stopped first, and stopped rather than killed: systemd sends
      # it SIGTERM, chief ends the iteration, and the commits it already made
      # are there for the push the reaper tries next.
      ExecStart=/bin/sh -c 'systemctl stop "chief-run@*.service" || true; /usr/local/bin/chief-reap'

  - path: /etc/systemd/system/chief-deadline.timer
    permissions: "0644"
    content: |
      [Unit]
      Description=This box's outside limit

      [Timer]
      OnBootSec=%[3]dh
      OnUnitActiveSec=%[2]s
      AccuracySec=1min
      Unit=chief-deadline.service

      [Install]
      WantedBy=timers.target
`, reapGrace, reapRetry, maxHours(opts))
}

// reaperSteps arms the deadline. The reap timer is deliberately left alone —
// the run unit starts it when it has something to report.
func reaperSteps(opts cloudInitOptions) string {
	if !opts.SelfDestruct {
		return ""
	}
	return "  - systemctl enable --now chief-deadline.timer\n"
}

// hostKeySection is the part of the cloud-config that gives the instance the
// identity chief generated for it.
//
// ssh_deletekeys throws away whatever the image was built with or generated on
// first boot, which is the half that actually matters: leaving those in place
// would mean the instance has several identities and answers with whichever
// sshd negotiates, only one of which chief is expecting. ssh_genkeytypes then
// keeps it from inventing replacements for the ones it deleted.
//
// An empty key renders nothing at all, so an instance created without one
// behaves exactly as it did before any of this existed.
func hostKeySection(k hostKey) string {
	if strings.TrimSpace(k.Private) == "" || strings.TrimSpace(k.Public) == "" {
		return ""
	}
	// The private key is a block scalar, so every one of its lines carries the
	// indentation of the block. Getting this wrong does not fail loudly: the
	// instance boots with an identity chief does not recognise, and every
	// connection to it is refused as a possible attack.
	var indented strings.Builder
	for _, line := range strings.Split(strings.TrimRight(k.Private, "\n"), "\n") {
		indented.WriteString("\n      " + line)
	}

	return fmt.Sprintf(`
# The instance's SSH identity, generated by chief before this machine existed
# and checked on every connection to it. Without this the first connection —
# the one that carries the agent's token, the GitHub token and the project's
# .env — would have to trust whatever answered at the address.
ssh_deletekeys: true
ssh_genkeytypes: ['ed25519']
ssh_keys:
  ed25519_private: |%s
  ed25519_public: %s
`, indented.String(), strings.TrimSpace(k.Public))
}
