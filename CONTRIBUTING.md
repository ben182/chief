# Contributing to Chief

Thanks for your interest in contributing to Chief! Here's how to get started.

## Prerequisites

- [Go 1.24+](https://go.dev/dl/)
- [Claude Code CLI](https://code.claude.com/docs/en/setup) (for end-to-end testing)
- [golangci-lint](https://golangci-lint.run/welcome/install-local/) (optional, for linting)

## Getting Started

```bash
git clone https://github.com/ben182/chief.git
cd chief
make build
```

## Development Workflow

```bash
make build       # Build the binary to ./bin/chief
make test        # Run all tests
make lint        # Run linters
make fmt         # Format code
make vet         # Run go vet
make run         # Build and run the TUI
```

## Live Box Tests

`internal/box` has tests that create a real Hetzner server, check something
on it, and destroy it again. They are the only way to prove what a fake cannot
— that cloud-init puts the host key in place before sshd starts, that the
firewall blocks traffic, that the PPA has the packages a project's PHP needs,
that a failing provisioning step actually reaches chief. They cost money (a
few cents each: the cheapest machine Hetzner sells, billed as one hour) and
are **off by default**. `make test` never runs them.

```bash
# The three that need no project: host key, firewall, list/down lifecycle.
CHIEF_LIVE_BOX_TEST=1 go test ./internal/box/ -run TestLive -v -timeout 20m

# Build a box the way `chief box up` would for a real project on this machine,
# and check every runtime, extension and server discovery promised is there.
CHIEF_LIVE_BOX_TEST=1 CHIEF_LIVE_PROJECT=~/Herd/my-app \
  go test ./internal/box/ -run TestLiveBoxIsBuiltToTheProject -v -timeout 25m

# The same, with one provisioning step sabotaged: the test expects the box to
# report the failure within a couple of minutes rather than come up "ready".
CHIEF_LIVE_BOX_TEST=1 CHIEF_LIVE_BREAK=1 CHIEF_LIVE_PROJECT=~/Herd/my-app \
  go test ./internal/box/ -run TestLiveBoxIsBuiltToTheProject -v -timeout 25m
```

They need the Hetzner token `chief box token` stores. Each test destroys its
box in a cleanup that runs whatever else fails; if one is ever left behind,
`chief box list` finds it. Mind the project's server limit: a Hetzner project
with production machines in it may not have room for more than one or two
boxes at once.

## Using a Local Build Instead of the Homebrew Version

If you installed Chief via Homebrew but want the `chief` command to run your
local build, point the Homebrew symlink at your build:

```bash
make build                                              # builds ./bin/chief

brew unlink chief                                       # remove Homebrew's symlink
ln -sf "$(pwd)/bin/chief" /opt/homebrew/bin/chief       # link your local build
brew pin chief                                          # keep `brew upgrade` from relinking it
```

After this, `chief version` reports your git revision (e.g. `0e15c79-dirty`),
and each `make build` is picked up immediately since the symlink stays put.

`brew upgrade` still works for everything else; the pin only protects Chief.

To go back to the Homebrew version:

```bash
brew unpin chief
rm /opt/homebrew/bin/chief
brew link chief
```

> On Intel Macs the Homebrew prefix is `/usr/local` instead of `/opt/homebrew`.

## Submitting Changes

1. Fork the repository
2. Create a feature branch (`git checkout -b my-feature`)
3. Make your changes
4. Run `make test` and `make lint` to verify
5. Commit using [conventional commits](https://www.conventionalcommits.org/) (e.g. `feat:`, `fix:`, `docs:`)
6. Open a pull request against `main`

## Reporting Bugs

Open an issue on [GitHub Issues](https://github.com/ben182/chief/issues) with:

- Steps to reproduce
- Expected vs actual behavior
- Chief version (`chief --version` or `git describe --tags`)

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](README.md#license).
