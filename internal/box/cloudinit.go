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
	// finished and the commits are on origin, or once MaxHours have passed since
	// it booted and the run has stopped committing. False renders none of it, and the box then bills
	// until somebody runs 'chief box down'.
	SelfDestruct bool
	// MaxHours is when a self-destructing box starts checking on its run. Zero
	// takes DefaultMaxHours.
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
  # Every download of a binary in this file retries. apt did not, and apt is the
  # one that has competition: an Ubuntu image starts its own unattended upgrade
  # in the same minutes cloud-init is installing, and whichever asks second gets
  # "Could not get lock /var/lib/dpkg/lock-frontend" — under the set -e below,
  # that is a box thrown away for something that would have passed thirty
  # seconds later. Waiting is the whole fix; five minutes is far longer than any
  # of it takes.
  #
  # Written here rather than passed as -o flags because it also covers the apt
  # that cloud-init runs for the package list above, and the one that
  # add-apt-repository and playwright's installer call underneath themselves.
  - path: /etc/apt/apt.conf.d/99chief
    permissions: "0644"
    content: |
      Acquire::Retries "3";
      DPkg::Lock::Timeout "300";

  # Swap is there to catch a spike, not to run on. The default of 60 would have
  # a build paging steadily to a network disk long before it has to.
  - path: /etc/sysctl.d/99-chief.conf
    permissions: "0644"
    content: |
      vm.swappiness = 10

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

  # Swap. A box is created on the cheapest machine its location sells, which
  # today means four gigabytes, and a Vite build, a browser under test and the
  # agent itself can want more than that between them. What happens then is the
  # worst failure this whole thing has: the kernel kills the run at three in the
  # morning, the run therefore never reports success, so the box never destroys
  # itself, and the night is gone with the machine still billing for it.
  #
  # Swap does not make the box fast — swappiness above keeps it as the buffer it
  # is meant to be. It makes the difference between a run that slows down for a
  # minute and a run that is dead, and the disk it costs is free.
  #
  # Here rather than through cloud-init's own swap module because this is under
  # set -e: a box that could not get its swap says so and is thrown away, rather
  # than coming up short of it with a warning in a log nobody reads.
  - fallocate -l 4G /swapfile
  - chmod 600 /swapfile
  - mkswap -q /swapfile
  - swapon /swapfile
  - printf '/swapfile none swap sw 0 0\n' >> /etc/fstab
  - sysctl -q -p /etc/sysctl.d/99-chief.conf
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
%[11]s%[12]s  # The marker chief waits for. Written last, so its existence means every step
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
		verifySteps(p),
	)
}

// verifySteps asks every tool the profile promised to say its own version, and
// every server to answer once, immediately before the ready marker is written.
//
// The marker means "this box is ready for a run", and until now it meant
// "nothing exited non-zero", which is not the same sentence. A package that
// installs without enabling itself, a binary for the wrong architecture, a
// database whose role was created against a socket that was not up yet — each
// of those leaves a perfectly healthy-looking box that fails at the first thing
// the run does, hours after the terminal that could have retried it was closed.
//
// Everything here runs under the same set -e as the rest, so a check that fails
// leaves the failed marker and chief reports it within seconds. That is the
// trade: a box occasionally destroyed at minute twelve instead of a run
// occasionally lost at hour four.
func verifySteps(p Profile) string {
	var b strings.Builder
	b.WriteString(`
  # What the run cannot start without, asked to prove it is there. The versions
  # land in /var/log/cloud-init-output.log, which is what chief prints when a
  # box fails to provision.
  # HOME, because runcmd has none and the agent CLI wants one even to say its
  # own version.
  - HOME=/root claude --version
  - gh --version
`)
	if p.PHP != "" {
		fmt.Fprintf(&b, "  - php -v | head -1\n  - HOME=/root composer --version\n")
	}
	if p.Node != "" {
		b.WriteString("  - node -v\n  - npm -v\n")
	}
	switch p.PackageManager {
	case "bun":
		b.WriteString("  - bun --version\n")
	case "pnpm", "yarn":
		fmt.Fprintf(&b, "  - %s --version\n", p.PackageManager)
	}
	if p.Go != "" {
		b.WriteString("  - go version\n")
	}
	switch p.Database {
	case "pgsql":
		// The role and the database by name rather than a bare connection: those
		// two are what the .env chief rewrites points at, and they are the pair
		// created by statements allowed to fail.
		fmt.Fprintf(&b,
			"  - su - postgres -c \"psql -tAc \\\"SELECT 1 FROM pg_roles WHERE rolname='%[1]s'\\\"\" | grep -q 1\n"+
				"  - su - postgres -c \"psql -tAc \\\"SELECT 1 FROM pg_database WHERE datname='%[2]s'\\\"\" | grep -q 1\n",
			remoteDatabaseUser, p.DatabaseName)
	case "mysql":
		fmt.Fprintf(&b,
			"  - mariadb -u %[1]s -p%[1]s -e \"USE %[2]s\"\n",
			remoteDatabaseUser, p.DatabaseName)
	}
	if p.Redis {
		b.WriteString("  - redis-cli ping | grep -q PONG\n")
	}
	if p.Meilisearch {
		// Started a moment ago by systemd, so the first connection can arrive
		// before it is listening; --retry-connrefused is what makes that a wait
		// rather than a failure.
		b.WriteString("  - curl -fsS --retry 10 --retry-delay 1 --retry-connrefused http://127.0.0.1:7700/health\n")
	}
	if p.Browser {
		// Not the browser — that is the project's to install — but the loader
		// answering for the libraries that were the point of installing anything.
		b.WriteString("  - ldconfig -p | grep -q libnss3\n")
	}
	return b.String()
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
  # The server is started by its own package, and "started" is not "accepting
  # connections". Without this wait the two statements below race the socket,
  # which is what the "|| true" on them used to hide — and what it hid was a box
  # whose .env names a role that does not exist, found out by the first
  # migration of the run, four hours later.
  - for i in $(seq 60); do pg_isready -q && break; sleep 1; done
  # A database superuser named after the account that will use it, so a project's
  # setup script can create and drop its own databases without a password, and
  # the database the project's .env names. Both stay tolerant of already existing,
  # because a retry runs this again; the check after the wait is what proves they
  # are there.
  - su - postgres -c "psql -c \"CREATE ROLE %[1]s WITH LOGIN SUPERUSER PASSWORD '%[1]s'\"" || true
  - su - postgres -c "createdb -O %[1]s %[2]s" || true
`, remoteDatabaseUser, p.DatabaseName)
	case "mysql":
		b.WriteString(`
  # The account and database the project's .env is rewritten to name, once the
  # server is answering rather than merely installed.
  - for i in $(seq 60); do mariadb-admin ping --silent && break; sleep 1; done
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
	// stallHours is how long a run past its deadline may go without a commit
	// before it counts as hung. A run that is still committing is left to
	// finish: the deadline exists for the run that hangs, and stopping one four
	// minutes after its fortieth story leaves nine undone and no pull request.
	// Six hours is longer than the five-hour usage window a rate-limited run
	// sits out without committing anything.
	stallHours = 6
	// hardLimitFactor bounds even a run that keeps committing: past this many
	// times its limit, counted from the run's start, it is stopped regardless.
	hardLimitFactor = 4
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
  #
  # Only for the run that hangs, though. A run that is still committing is
  # making progress, and stopping it throws away what it would have finished;
  # it is looked at again every %[2]s until it stalls, ends, or reaches the
  # hard limit.
  - path: /usr/local/bin/chief-deadline
    permissions: "0755"
    content: |
      #!/bin/sh
      set -u
      say() { echo "chief-deadline: $1"; }
      now=$(date +%%s)

      unit=$(systemctl list-units --no-legend --plain --state=active,activating 'chief-run@*' | awk 'NR==1 {print $1}')
      if [ -n "$unit" ]; then
        started=$(date -d "$(systemctl show "$unit" -p ExecMainStartTimestamp --value)" +%%s 2>/dev/null || echo 0)
        # The newest commit in any branch or worktree. Only one made since the
        # run started counts: the clone's own history says nothing about it.
        last=$(su - chief -c 'cd ~/project && git log --all -1 --format=%%ct' 2>/dev/null || echo 0)
        case "$started" in '' | *[!0-9]*) started=0 ;; esac
        case "$last" in '' | *[!0-9]*) last=0 ;; esac
        if [ "$started" -gt 0 ] && [ "$last" -ge "$started" ] &&
          [ $((now - last)) -lt %[4]d ] && [ $((now - started)) -lt %[5]d ]; then
          say "the run is still committing (last commit $(((now - last) / 60)) min ago) — leaving it, looking again in %[2]s"
          exit 0
        fi
        if [ "$started" -gt 0 ] && [ $((now - started)) -ge %[5]d ]; then
          say "the run has been going for %[6]d hours, the hard limit — stopping it"
        else
          say "no commit from the run in %[7]d hours — it counts as hung, stopping it"
        fi
      fi

      # The run is stopped first, and stopped rather than killed: systemd sends
      # it SIGTERM, chief ends the iteration, and the commits it already made
      # are there for the push the reaper tries next.
      systemctl stop "chief-run@*.service" || true
      exec /usr/local/bin/chief-reap

  - path: /etc/systemd/system/chief-deadline.service
    permissions: "0644"
    content: |
      [Unit]
      Description=Stop a stalled run and destroy this box, from %[3]d hours on

      [Service]
      Type=oneshot
      ExecStart=/usr/local/bin/chief-deadline

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
`, reapGrace, reapRetry, maxHours(opts), stallHours*3600,
		hardLimitFactor*maxHours(opts)*3600, hardLimitFactor*maxHours(opts), stallHours)
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
