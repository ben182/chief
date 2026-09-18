---
description: Run a PRD on a throwaway Hetzner instance. Chief builds the machine to your project, starts the run as a service, pushes what it built, and destroys the box when it is done.
---

# Running on a Box

A Chief run takes hours, and for those hours it owns the machine it runs on: the CPU, the rate limit window, and the laptop that has to stay open. `chief box` moves the run onto a machine created for it and destroyed afterwards.

```bash
chief box token          # once, ever: the credentials a box needs
chief box up auth        # create a box, put the project on it, start the run
```

That is the whole thing. The run keeps going when you close the terminal, when you close the lid, and when you go to bed. In the morning there is a branch on origin, a pull request if your project opens them, and no machine.

::: tip What it costs
The default machine is about six cents an hour, so a five-hour run is under thirty cents — two orders of magnitude less than the same run spends on tokens. The box is not the expensive part; **forgetting** the box is, which is why it destroys itself.
:::

## Why a box

Three things are wrong with running a long PRD on the machine you work on:

- **It owns the laptop.** Your fans, your CPU, and a terminal you cannot close. Chief holds a wake assertion so the machine does not sleep mid-run, but on battery a closed lid still suspends it, and a suspended laptop is a dead run.
- **It owns your rate limit window.** The run is the only thing the account is doing for the next five hours.
- **It is not reproducible.** The run happens against whatever your machine has installed today.

A box turns all three into one number on a bill. It is a machine with exactly the runtimes your project needs, created from nothing, and thrown away when the work is on origin.

## What happens on `chief box up`

```
==> Read from the project
    Laravel on PHP 8.4 (from composer.json)
    extensions: bcmath curl dom mbstring pgsql xml zip
    PostgreSQL, Redis
    Node 24 (from .nvmrc), bun
==> Building chief for the box
    18 MB
==> Creating chief-shop-auth-101500 (cpx32 in fsn1)
    203.0.113.42
==> Waiting for the box to boot
==> Armed the box's own shutoff (12h at the outside)
==> Waiting for provisioning (PHP 8.4, Node 24, PostgreSQL, Redis)
    up to 20m0s
    provisioned
==> Installing chief and the credentials
==> Cloning the project
==> Copying the PRD and the untracked files
    .env (pointed at the box: DB_HOST, DB_USERNAME, DB_PASSWORD, REDIS_HOST)
==> Starting the run
==> The run is going — it survives this terminal
    what it builds is pushed to origin when it ends, and a pull request is opened
    the box destroys itself 20min after a finished run, and 12h from boot whatever happens
```

In order:

1. **Everything checkable is checked first**, before anything is created and before any login can open a browser: the PRD exists, the project has an `origin`, the current branch is on `origin` and not ahead of it, `ssh`/`scp`/`rsync` are installed, and this machine has an SSH public key.
2. **Chief cross-compiles itself** for the box from your checkout, rather than downloading a release. The box runs the Chief you have, not the last tag.
3. **The instance is created** behind a shared firewall (inbound: SSH and ping, nothing else) with an SSH host key Chief generated *before* the machine existed — so the first connection, the one carrying your tokens and your `.env`, is verified rather than trusted.
4. **cloud-init provisions it** to the profile read from your project (below).
5. **The project is cloned** over HTTPS with the GitHub token, on the branch you are on.
6. **The files git does not carry go over**: the PRD directory, `.chief/config.yaml`, and `.env`.
7. **The run starts** as a systemd unit, `chief-run@<prd>`, and owes nothing to the connection that started it.

::: warning The branch has to be on origin
The box clones `origin` and checks out your branch by name. A branch that was never pushed does not exist for the clone, and one that is ahead of `origin` exists at the *wrong commit* — the run would build on a version of the project missing the last thing you did. Chief refuses both before creating anything and tells you to `git push`.
:::

## What the box is built with

Chief reads the project rather than provisioning one fixed machine, and prints what it found before creating anything — a wrong guess is cheapest to catch while it is still a line on a screen.

| Read from | What it decides |
|---|---|
| `composer.json` | PHP series, whether it is Laravel |
| `composer.lock` | every `ext-*` required transitively, mapped onto apt packages |
| `.env` (`DB_CONNECTION`) and `phpunit.xml` | PostgreSQL, MariaDB, or nothing for SQLite |
| `.env`, Horizon, Scout | Redis, Meilisearch |
| lockfile (`bun.lock`, `pnpm-lock.yaml`, `yarn.lock`) and `packageManager` | which JavaScript package manager, at which version |
| Pest browser plugin, Playwright, Puppeteer, Browsershot, Dusk | Chromium's system libraries, or Google Chrome itself |
| `go.mod` | the Go toolchain, and no PHP at all |

**The PHP and Node versions are the ones this machine runs**, because that is what the project is developed against: a site Herd isolates to a version gets that version, otherwise the `php` and `node` found in the project directory, then the project's own pins (`composer.json` platform, `.nvmrc`, `engines`), then the lowest version the constraints accept. Each answer is reported with where it came from.

Override any of it with `--php`, `--node`, `--package` for one run, or `box.php`, `box.node`, `box.packages` for the project. Anything project-specific beyond runtimes — dependencies, schema, seeds — belongs in [`worktree.setup`](/reference/configuration#config-keys), which Chief runs inside the checkout before the agent starts.

### The `.env` is pointed at the box

Your `.env` describes your laptop: Herd's or DBngin's database, on that host, as that user. On the box the same database exists — created by cloud-init from `DB_DATABASE`, on the server `DB_CONNECTION` named, under the box's own `chief`/`chief` account — so `DB_HOST`, `DB_PORT`, `DB_USERNAME`, `DB_PASSWORD`, plus `REDIS_HOST` and `MEILISEARCH_HOST` where they are in use, are rewritten on the way over. An absolute SQLite path becomes the project's `database/database.sqlite`. Every other line, comments and order included, is left exactly as it was, and `up` lists the keys it changed.

The rewritten copy goes over stdin, never through a temporary file, so it does not exist on disk on either machine.

## Credentials

Three, and Chief works out two of them:

| Token | Where it comes from | What it does |
|---|---|---|
| **GitHub** | `gh auth token` | clone the project, push the branch, open the pull request |
| **Claude** | `claude setup-token`, saved after the first time | log the agent in on a machine with no browser |
| **Hetzner** | you, once | create and destroy the machine |

Run `chief box token` once. It asks only for what is missing, verifies the Hetzner token before saving it, and is the only place a browser ever opens — `chief box up` is never interactive, because a login prompt in a background shell is a hang, which is worse than a failure because it looks like work.

The Hetzner token is also read from `CHIEF_BOX_HETZNER_TOKEN`, `HCLOUD_TOKEN`, or the `hcloud` CLI's own config if you already have that set up.

## The run ends: what leaves the box

The box is temporary, so everything the run produces has to be somewhere else by the time it goes:

- **The branch is pushed, always.** A box run sets `--push` and pushes whatever `onComplete.push` says, because that setting is about a laptop, where the commits are still there in the morning either way. The log says when it went against your config.
- **A pull request is opened** when `onComplete.createPR` is on, assigned to the account `gh` is authenticated as — so an unattended run lands in your "Assigned to you" list rather than among the branches.
- **The run's log is committed next to the PRD** (`--log-to-branch`, always on for a box). The journal it would otherwise live in dies with the machine.
- **The run summary** is written and committed when `onComplete.summary` is on, so it rides along in the push.

## The box switches itself off

`--down-when-done` only destroys a box while `chief box run` is still watching it. That is the wrong shape for the case boxes exist for: the run finishes at three in the morning, and your laptop is asleep.

So a box is built with its own shutoff:

- **When the run finishes successfully**, a timer starts. Twenty minutes later a script on the box pushes anything still only there, checks that nothing is, and deletes the machine through the Hetzner API.
- **Whatever else happens**, `box.maxHours` after boot — twelve by default — the run is stopped (SIGTERM, so Chief ends cleanly and its commits stay) and the same script runs. This is the backstop for the run that hangs rather than ends, and for provisioning that never finished.
- **A run that ends with stories unresolved** does not trigger the first timer. That is the box you want to look at, `chief box ssh` into, or `chief box retry` — so it lives until the deadline instead.
- **Work that exists nowhere else is never destroyed.** If commits are still only on the box after the rescue push, the script says so in the journal and tries again in an hour. A box that bills is a smaller loss than a night of work.

`--keep` (or `box.keep`) builds the old behaviour, where `chief box down` is the only thing that stops the bill. `--max-hours N` moves the deadline.

::: warning The Hetzner token is on the box
A machine cannot delete itself without a token that can delete it. Chief writes it root-only, over stdin, and never into the cloud-config — which is readable from the instance's own metadata service by anything that can make an HTTP request. What it cannot do is hide it from the agent: a run happens under an account with passwordless sudo, so an agent that went looking could read the file and delete every box in the project.

**Keep Chief's boxes in a Hetzner project of their own.** That is the whole blast radius.
:::

## Watching, or not

| Command | What it does |
|---|---|
| `chief box up <prd>` | create the box and start the run, then hand the terminal back |
| `chief box run <prd>` | the same, then follow the log until the run *ends* and say how it went |
| `chief box logs` | follow the log of a box that is already running |
| `chief box status` | what the unit is doing, plus the last fifteen lines |
| `chief box ssh [cmd]` | a shell on the box in the project directory, or one command there |

`run` waits for the run rather than for the reader: `journalctl` keeps following a unit that has ended, so a finished run would otherwise look exactly like a quiet one. When it ends, `run` reports the outcome — *done — every story resolved*, *ended with work left*, *killed*, *timed out* — and sends a desktop notification if `onComplete.notify` is on. That notification comes from your machine, not the box: a server with no display is not somewhere a banner reaches anyone.

Both `logs` and `run` **reconnect when the connection drops** and pick up exactly where they left off, so a laptop that slept for a minute does not end the log. Interrupting either stops the watching, never the run.

`chief box run --down-when-done` destroys the box as soon as the run ends, rather than waiting out the twenty-minute grace period.

## Nothing lets you forget a box

Two places say a box is billing, without being asked:

- **The dashboard header**, whenever the project has one: its name, the PRD, how long it has been up and what that has cost so far. Past twelve hours the line turns into a warning — a box that old is one Chief was no longer expecting to go on its own.
- **`chief box list`**, which asks Hetzner rather than your checkout, so it finds the box created from a directory nobody has opened in three weeks. It prices every box and totals them.

The record that makes `down` work lives in one checkout, and a box is forgotten precisely when that checkout is gone. For those:

```bash
chief box down --name chief-shop-auth-101500   # one box, wherever it came from
chief box down --all                           # every box in the Hetzner project
```

Both ask about commits that exist nowhere else before destroying anything — all of them in one question, because a prompt per machine is a prompt people answer without reading. `--force` skips the question.

## When something goes wrong

**A provisioning step failed.** cloud-init writes a marker when everything finished and a different one when a step died, so `up` reports the failure within seconds instead of waiting out the timeout, and prints the last thirty lines of the provisioning log. The box stays up so you can look.

**The clone failed, or a file was not where you said.** `chief box retry` puts the project on the box that is already there and starts the run again — no second machine, no waiting through provisioning twice. Every step it repeats is written to be repeated. It refuses while a run is still active.

**You want to see the machine.** `chief box ssh` lands you in the project directory. `chief box ssh 'php artisan test'` runs one command there and streams the output back.

**The box is gone.** A box that destroyed itself leaves its record behind; `up`, `logs` and `status` notice the machine no longer exists, say so, and clear it, so the next run is not blocked by a machine that has not existed since three in the morning.

## Security

- **Inbound: SSH and ping.** Every box sits behind one shared firewall named `chief`. It outlives the boxes, costs nothing between them, and a box is attached to it in the create call, so there is no moment where a machine is up and unprotected. If its rules have drifted — somebody opened a port in the console — they are put back and the change is reported. A firewall without Chief's `managed-by=chief` label is never read and never touched.
- **The host key is pinned.** Chief generates the instance's SSH identity before the machine exists, hands it over in the cloud-config, and writes it into a `known_hosts` file belonging to the project. Every connection is checked against a key that was known in advance, and the file goes when the box does — Hetzner hands that address to somebody else within hours.
- **Secrets travel over stdin** into files that are already `0600`, so they never appear in a command line, a process list, or a shell history.
- **Work does not happen as root.** The run is a `chief` user. That user does have passwordless sudo — the agent needs to install things — so the box's isolation is the box itself, not the account.
- **What is on the machine:** your source, your `.env`, a GitHub token that can push what you can push, a Claude token, and the Hetzner token unless you passed `--keep`. It exists for hours and is then deleted.

## Choosing where and what

```bash
chief box config
```

Asks Hetzner what it offers **right now** and lets you pick a location and a machine size, once, for the project. The list is not a table in Chief's source: a hard-coded one is wrong the week Hetzner retires a generation, and the failure it causes — "unsupported location" on a machine you have been creating for months — reads like a bug in Chief.

Two of these are decisions rather than preferences: the location decides which country your source and your `.env` spend the run in, and the type decides what it costs. A project that has never been asked gets asked the first time it runs `box up` at a terminal.

::: info IPv4 is not optional
GitHub publishes no IPv6 address for `github.com`, `api.github.com` or `codeload.github.com`, and neither does the apt repository Claude Code installs from. An IPv6-only box could not clone, push, open a pull request, or install the agent. The IPv4 address costs 0.08 cents an hour — under half a cent for a five-hour run.
:::

## Limits

- **One box per project.** The record is a single file; a second `box up` in the same checkout is refused while the first box exists.
- **Hetzner only.** The provisioning is cloud-init and the API calls are Hetzner's.
- **Ubuntu 24.04**, because PHP comes from Ondřej Surý's PPA and that is the release it packages every series from 8.0 to 8.5 for.
- **amd64.** The binary Chief builds for the box is `GOOS=linux GOARCH=amd64`.

## Reference

Full flag list: [`chief box`](/reference/cli#chief-box). Project settings: [`box.*`](/reference/configuration#config-keys).

| Setting | Flag | Default |
|---|---|---|
| `box.location` | `--location` | `fsn1` |
| `box.type` | `--type` | `cpx32` (4 vCPU, 8 GB, ~6 ct/h) |
| `box.image` | `--image` | `ubuntu-24.04` |
| `box.php` / `box.node` | `--php` / `--node` | detected from the project |
| `box.packages` | `--package` | none |
| `box.files` | `--file` | `.env` |
| `box.keep` | `--keep` | off — the box destroys itself |
| `box.maxHours` | `--max-hours` | `12` |
