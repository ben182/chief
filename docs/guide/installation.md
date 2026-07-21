---
description: Install Chief on macOS or Linux via Homebrew, install script, manual download, or from source. Single binary with no runtime dependencies.
---

# Installation

Chief is distributed as a single binary with no runtime dependencies. Choose your preferred installation method below.

## Prerequisites

Chief needs an agent CLI: **Claude Code** (default), **Codex**, **OpenCode**, **Cursor**, or **Gemini**. Install at least one and authenticate.

### Option A: Claude Code CLI (default)

::: code-group

```bash [npm (recommended)]
# Install Claude Code globally
npm install -g @anthropic-ai/claude-code

# Authenticate (opens browser)
claude login
```

```bash [npx (no install)]
# Run directly without installing
npx @anthropic-ai/claude-code login
```

:::

### Option B: Codex CLI

To use [OpenAI Codex CLI](https://developers.openai.com/codex/cli/reference) instead of Claude:

1. Install Codex per the [official reference](https://developers.openai.com/codex/cli/reference).
2. Ensure `codex` is on your PATH, or set `agent.cliPath` in `.chief/config.yaml` (see [Configuration](/reference/configuration#agent)).
3. Run Chief with `chief --agent codex` or set `CHIEF_AGENT=codex`, or set `agent.provider: codex` in `.chief/config.yaml`.

### Option C: OpenCode CLI

To use [OpenCode CLI](https://opencode.ai) as an alternative:

1. Install OpenCode per the [official docs](https://opencode.ai/docs/).
2. Ensure `opencode` is on your PATH, or set `agent.cliPath` in `.chief/config.yaml` (see [Configuration](/reference/configuration#agent)).
3. Run Chief with `chief --agent opencode` or set `CHIEF_AGENT=opencode`, or set `agent.provider: opencode` in `.chief/config.yaml`.

### Option D: Cursor CLI

To use [Cursor CLI](https://cursor.com/docs/cli/overview) as the agent:

1. Install Cursor CLI per the [official docs](https://cursor.com/docs/cli/overview)
2. Ensure `agent` is on your PATH, or set `agent.cliPath` in `.chief/config.yaml`.
3. Run `agent login` for authentication.
4. Run Chief with `chief --agent cursor` or set `CHIEF_AGENT=cursor`, or set `agent.provider: cursor` in `.chief/config.yaml`.

### Option E: Gemini CLI

To use [Gemini CLI](https://github.com/google-gemini/gemini-cli) as the agent:

1. Install Gemini CLI per the [official docs](https://github.com/google-gemini/gemini-cli).
2. Ensure `gemini` is on your PATH, or set `agent.cliPath` in `.chief/config.yaml` (see [Configuration](/reference/configuration#agent)).
3. Run Chief with `chief --agent gemini` or set `CHIEF_AGENT=gemini`, or set `agent.provider: gemini` in `.chief/config.yaml`.

### Optional: GitHub CLI (`gh`)

If you want Chief to automatically create pull requests when a PRD completes, install the [GitHub CLI](https://cli.github.com/):

```bash
# macOS
brew install gh

# Linux
# See https://github.com/cli/cli/blob/trunk/docs/install_linux.md

# Authenticate
gh auth login
```

The `gh` CLI is only required for automatic PR creation. All other features work without it.

## Homebrew (Recommended)

The easiest way to install Chief on **macOS** or **Linux**:

```bash
brew install ben182/chief/chief
```

This method:
- Automatically handles updates via `brew upgrade`
- Installs to `/opt/homebrew/bin/chief` (Apple Silicon) or `/usr/local/bin/chief` (Intel/Linux)
- Works on macOS (Apple Silicon and Intel) and Linux (x64 and ARM64)

### Updating

```bash
brew upgrade chief
```

## Install Script

Download and install with a single command:

```bash
curl -fsSL https://raw.githubusercontent.com/ben182/chief/main/install.sh | bash
```

The script automatically detects your platform, downloads the matching archive, verifies its checksum, and installs the binary to `/usr/local/bin` (or `~/.local/bin` when it can't write there without sudo).

### Script Options

| Option | Description | Example |
|--------|-------------|---------|
| `--version`, `-v` | Install a specific version | `--version v0.1.0` |
| `--help`, `-h` | Show all available options | `--help` |

To install into a custom directory, set the `CHIEF_INSTALL_DIR` environment variable (the script creates it if needed).

**Examples:**

```bash
# Install a specific version
curl -fsSL https://raw.githubusercontent.com/ben182/chief/main/install.sh | bash -s -- --version v0.1.0

# Install to a custom directory
curl -fsSL https://raw.githubusercontent.com/ben182/chief/main/install.sh | CHIEF_INSTALL_DIR=~/.local/bin bash

# Custom directory + specific version
curl -fsSL https://raw.githubusercontent.com/ben182/chief/main/install.sh | CHIEF_INSTALL_DIR=/opt/chief bash -s -- --version v0.1.0
```

::: info Custom Directory
If you install to a custom directory, make sure it's in your `PATH`:
```bash
export PATH="$HOME/.local/bin:$PATH"
```
Add this to your shell profile (`.bashrc`, `.zshrc`, etc.) to persist it.
:::

## Manual Binary Download

Releases are published as compressed archives (with a `checksums.txt`) on the [GitHub Releases page](https://github.com/ben182/chief/releases). Each archive is named `chief_<version>_<os>_<arch>.tar.gz` (Windows uses a `.zip`) and contains the `chief` binary plus the LICENSE and README.

### Platform Matrix

| Platform | Architecture | Archive Name | Notes |
|----------|-------------|--------------|-------|
| macOS | Apple Silicon (M1/M2/M3) | `chief_<version>_darwin_arm64.tar.gz` | Recommended for modern Macs |
| macOS | Intel (x64) | `chief_<version>_darwin_amd64.tar.gz` | For older Intel-based Macs |
| Linux | x64 (AMD64) | `chief_<version>_linux_amd64.tar.gz` | Most common Linux servers |
| Linux | ARM64 | `chief_<version>_linux_arm64.tar.gz` | Raspberry Pi 4, AWS Graviton |
| Windows | x64 (AMD64) | `chief_<version>_windows_amd64.zip` | Extract and place `chief.exe` on your PATH |

### Installation Steps

The archive name embeds the version (e.g. `chief_0.1.0_darwin_arm64.tar.gz`), so pick your version and platform from the releases page. On macOS/Linux:

```bash
# Set the version and platform you want (see the releases page)
VERSION=0.1.0            # without the leading "v"
OS=darwin               # darwin or linux
ARCH=arm64              # arm64 or amd64

# Download and extract the archive
curl -LO "https://github.com/ben182/chief/releases/download/v${VERSION}/chief_${VERSION}_${OS}_${ARCH}.tar.gz"
tar -xzf "chief_${VERSION}_${OS}_${ARCH}.tar.gz"

# Move the binary to a directory in your PATH
sudo mv chief /usr/local/bin/chief
```

::: tip Detect Your Architecture
Not sure which binary you need? Run these commands:
```bash
# macOS
uname -m  # arm64 = Apple Silicon, x86_64 = Intel

# Linux
uname -m  # x86_64 = AMD64, aarch64 = ARM64
```
:::

## Building from Source

Build Chief from source if you want the latest development version or need to customize the build.

### Prerequisites

- **Go 1.24** or later ([install Go](https://go.dev/doc/install))
- **Git** for cloning the repository

### Build Steps

```bash
# Clone the repository
git clone https://github.com/ben182/chief.git
cd chief

# Build the binary
go build -o chief ./cmd/chief

# Optionally install to your GOPATH/bin
go install ./cmd/chief
```

### Build with Version Info

For a release-quality build with version information embedded:

```bash
go build -ldflags "-X main.Version=$(git describe --tags --always)" -o chief ./cmd/chief
```

### Verify the Build

```bash
./chief --version
```

## Verifying Installation

After installing via any method, verify Chief is working correctly:

```bash
# Check the version
chief --version

# View help
chief --help

# Check that your agent CLI is accessible (Claude default, or codex if configured)
claude --version
# or: codex --version
```

Expected output (example with Claude):

```
$ chief --version
chief version vX.Y.Z

$ claude --version
Claude Code vX.Y.Z
```

::: warning Troubleshooting
If `chief` is not found after installation:
1. Check that the installation directory is in your `PATH`
2. Open a new terminal window/tab to reload your shell
3. Run `which chief` to see if it's found and where

See the [Troubleshooting Guide](/troubleshooting/common-issues) for more help.
:::

## Next Steps

Now that Chief is installed:

1. **[Quick Start Guide](/guide/quick-start)** - Get running with your first PRD
2. **[How Chief Works](/concepts/how-it-works)** - Understand the autonomous agent concept
3. **[CLI Reference](/reference/cli)** - Explore all available commands
