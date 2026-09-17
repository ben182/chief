# Running chief on a throwaway box

A chief run takes hours. For those hours it owns the machine: the CPU, the rate
limit window, and the laptop that has to stay open. This directory moves the run
to a Hetzner instance you create for it and destroy afterwards.

Cost, for calibration: a `cpx31` (4 vCPU, 8 GB) bills at about 2.7 cents an
hour. A five-hour run costs roughly 14 cents of machine time — the tokens are
two orders of magnitude more expensive than the computer.

## The pieces

| | |
|---|---|
| `chief start <prd> --headless` | chief without the TUI. Logs one line per event to stdout, survives a dropped SSH connection, turns SIGTERM into a clean stop, and exits 0 only when every story is resolved. |
| `cloud-init.yaml` | Provisions a bare Ubuntu 24.04 instance into something a run can happen on: PHP 8.3 with the usual extensions, Composer, Node, Postgres, Redis, the GitHub CLI, Claude Code, and the systemd unit a run is started as. |
| `chief-box` | The command you actually type. Creates the instance, uploads a freshly built chief, clones the project, copies the files git does not carry, and starts the run. |

## One-time setup

**1. The Hetzner CLI and a project token**

```bash
brew install hcloud
hcloud context create chief      # paste an API token from the Hetzner console
```

You also need an SSH key in the Hetzner project (console → Security → SSH keys).
With exactly one key there, `chief-box` finds it by itself; with several, name
the one to use in `CHIEF_BOX_SSH_KEY`.

**2. The credentials the box runs with**

```bash
mkdir -p ~/.config/chief-box
claude setup-token                # prints a long-lived token
gh auth token                     # or create a fine-grained PAT with repo access

cat > ~/.config/chief-box/env <<'EOF'
CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-...
GH_TOKEN=ghp_...
EOF
chmod 600 ~/.config/chief-box/env
```

`claude setup-token` is the piece that makes this work at all: it is how a
machine with no browser and nobody sitting at it stays authenticated. The token
reaches the box over stdin into a file that is already `0600`, so it never
appears in a command line or a shell history.

`GH_TOKEN` does double duty — the box clones with it, and the finished run
pushes and opens its pull request with it.

**3. Put `chief-box` on your PATH**

```bash
ln -s ~/Code/chief/deploy/chief-box /usr/local/bin/chief-box
```

The symlink matters: the script builds the chief binary from the checkout it
lives in, so the box always runs the chief you have rather than the last one
that was released.

## Using it

From inside the project you want built:

```bash
chief new refactor-billing      # write the PRD here, interactively
chief-box up refactor-billing   # create the box and start the run
```

`up` returns once the run is going. From then on it is a systemd unit and owes
nothing to your terminal:

```bash
chief-box logs      # follow along; Ctrl-C detaches, the run keeps going
chief-box status    # still running?
chief-box ssh       # a shell in the project on the box
chief-box down      # destroy it — this is the one that stops the billing
```

`chief-box run <prd>` is `up` followed by `logs` in one blocking command, for
when you do want to watch the first few minutes.

### What gets copied, and why

The clone gives the box everything git tracks. Two things it does not:

- **The PRD.** `.chief/` is gitignored in most projects, so the PRD you just
  wrote is not in the clone. `chief-box` rsyncs `.chief/prds/<name>/` and
  `.chief/config.yaml` across.
- **Your `.env`.** Named in `CHIEF_BOX_EXTRA_FILES`, which defaults to `.env`.
  Set it to a space-separated list for a project that needs more.

### Where a project's own setup goes

`cloud-init.yaml` installs the *base*: the runtimes and the database. Everything
specific to a project — `composer install`, the schema, the queue worker —
belongs in that project's `worktree.setup`, which chief runs inside the checkout
before the agent starts:

```yaml
# .chief/config.yaml
worktree:
  setup: |
    composer install --no-interaction
    cp .env.example .env
    php artisan key:generate
    createdb "chief_${CHIEF_PRD_NAME}"
    php artisan migrate
  teardown: dropdb --if-exists "chief_${CHIEF_PRD_NAME}"
  setupTimeoutSeconds: 900

onComplete:
  summary: true
  push: true
  createPR: true
```

The split is deliberate: cloud-init is baked into the instance at creation time,
and a project's needs change weekly. Keeping them in the project means the same
box works for every project you point it at, and a setup that breaks is fixed in
a commit rather than in a YAML file you have to remember exists.

Note that `worktree.setup` only runs for a run started with `--worktree`. Add it
to the systemd unit (`chief start %i --headless --worktree`) if you want the
box to work that way; the default runs in the clone itself, which on a machine
created for exactly one run is usually what you want.

## Things worth knowing before the first run

**Your rate limit is shared.** The box authenticates as you. A five-hour run
there eats the same usage window your laptop draws from, and chief will sit out
a limit it hits rather than failing — the log says so, and the run simply takes
longer.

**`down` is the only thing that stops the billing.** An instance nobody
destroyed bills until someone does. `chief-box down` warns and asks first if the
box is holding commits that were never pushed.

**The box is trusted with a lot.** It holds a token that can act as you on
GitHub and a token that can spend your Claude subscription, and the agent runs
on it with `--dangerously-skip-permissions`. That is the deal a headless run
makes everywhere; it is worth being deliberate about which repositories the
`GH_TOKEN` can reach. A fine-grained PAT scoped to the one repository is a
better fit here than a classic token with `repo`.

**Set `agent.mcp: none` for an unattended run.** A run nobody is watching
inherits every MCP server the machine has configured. On the box that is close
to nothing, which is the point — but if you copy a `.mcp.json` across, the agent
gets those servers with permissions skipped.

## Running it somewhere else

Nothing above is Hetzner-specific except `chief-box` itself. The pieces that do
the work — `--headless`, the cloud-init, the systemd unit — apply to any Ubuntu
machine. On a home server, the whole thing reduces to installing chief and
Claude Code once and then:

```bash
ssh homeserver 'cd ~/project && nohup chief start auth --headless > run.log 2>&1 &'
ssh homeserver 'tail -f ~/project/run.log'
```
