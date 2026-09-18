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
      Wants=network-online.target

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
%[4]s
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
  # The marker chief waits for. Written last, so its existence means every step
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
	)
}

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
		b.WriteString(`
  # Composer. HOME is set explicitly: runcmd runs without one, and the installer
  # refuses to do anything without it ("The HOME or COMPOSER_HOME environment
  # variable must be set"), which it reports on stdout while still exiting 0 —
  # so the failure is silent and composer is simply absent later.
  - curl -fsSL https://getcomposer.org/installer -o /tmp/composer-setup.php
  - HOME=/root php /tmp/composer-setup.php --install-dir=/usr/local/bin --filename=composer
  - rm -f /tmp/composer-setup.php
  # And a check, so a silent failure cannot reach the ready marker.
  - test -x /usr/local/bin/composer
`)
	}

	switch p.PackageManager {
	case "bun":
		b.WriteString(`
  # Bun, because that is what the lockfile was written by. Into /usr/local so
  # it is on PATH for the run's user as well as for root.
  - curl -fsSL https://bun.sh/install | BUN_INSTALL=/usr/local bash
  - test -x /usr/local/bin/bun
`)
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
		b.WriteString(`
  # Dusk drives Google Chrome itself rather than a bundled Chromium.
  - curl -fsSL https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb -o /tmp/chrome.deb
  - apt-get install -y /tmp/chrome.deb
  - rm -f /tmp/chrome.deb
`)
	}

	if p.Go != "" {
		fmt.Fprintf(&b, `
  # Go %[1]s, the toolchain go.mod asks for, from the official tarball.
  - curl -fsSL https://go.dev/dl/go%[1]s.linux-amd64.tar.gz -o /tmp/go.tgz
  - rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tgz && rm -f /tmp/go.tgz
  - ln -sf /usr/local/go/bin/go /usr/local/bin/go
  - ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
`, p.Go)
	}

	if p.Meilisearch {
		b.WriteString(`
  # Meilisearch, for Scout. The installer drops the binary where it runs.
  - cd /tmp && curl -fsSL https://install.meilisearch.com | sh
  - install -m 0755 /tmp/meilisearch /usr/local/bin/meilisearch
  - systemctl enable --now meilisearch
`)
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
