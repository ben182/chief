package box

import (
	"fmt"
	"strings"

	"github.com/ben182/chief/internal/config"
)

// Environment describes the machine a box run happens on, for `chief box prep`
// to judge the PRD against: what is always there, what this project gets on top
// right now, and the knobs that change it.
//
// It is generated from the same sources the cloud-config is — the profile
// Discover reads from the project and the project's box settings — so what prep
// is told and what the box is built with cannot drift apart. The base list
// mirrors cloudInit; keep the two in step.
func Environment(baseDir string, cfg *config.Config) string {
	if cfg == nil {
		cfg = config.Default()
	}
	profile := Discover(baseDir, DiscoverOptions{PHP: cfg.Box.PHP, Node: cfg.Box.Node})

	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	line("The box is a fresh Hetzner cloud server running %s (Linux, x86_64), created for this run", describeImage(cfg.Box.Image))
	line("and destroyed after it. The run is unattended and headless: no display, no desktop, no")
	line("person to answer a prompt. It runs as the user `chief`, with passwordless sudo, in a clone")
	line("of the project's branch. A story's `**Braucht:**` line is compared with `%s` there.", OS)
	line("")
	line("There is no macOS on it: no Xcode, no `codesign`, no Keychain, no Homebrew, no macOS")
	line("privacy permissions, no Dock, no GUI apps. There is no 1Password or other password manager,")
	line("no signing key for commits, and no hardware beyond the server itself (no USB, camera,")
	line("audio, GPU).")
	line("")
	line("Always installed: git, curl, jq, unzip, rsync, build-essential, ripgrep, gnupg,")
	line("software-properties-common, Claude Code, the GitHub CLI (`gh`, logged in, so pushing and")
	line("opening pull requests work), and 4 GB of swap.")
	line("")
	line("Read from this project right now, and installed on top:")
	for _, s := range profile.Summary() {
		line("- %s", s)
	}
	if len(cfg.Box.Packages) > 0 {
		line("- apt packages from `box.packages`: %s", strings.Join(cfg.Box.Packages, " "))
	}
	if len(cfg.Box.Files) > 0 {
		line("- files copied from this machine (`box.files`): %s", strings.Join(cfg.Box.Files, ", "))
	} else {
		line("- the project's `.env` is copied from this machine, pointed at the box's own services")
	}
	if strings.TrimSpace(cfg.Worktree.Setup) != "" {
		line("- `worktree.setup` runs in the checkout before the agent starts: `%s`", strings.TrimSpace(cfg.Worktree.Setup))
	}
	line("")
	line("What changes the box, and nothing else does:")
	line("- `box.packages` in `.chief/config.yaml`: a list of extra apt packages, by their Ubuntu")
	line("  %s names. This is the place for a missing command-line tool that apt carries.", describeImage(cfg.Box.Image))
	line("- `box.php` / `box.node`: pin the PHP series or Node major (otherwise this machine's).")
	line("- `box.files`: untracked files the run needs copied over (default: `.env` alone).")
	line("- Services follow the project's `.env` (read for PHP projects): `DB_CONNECTION=pgsql` gives")
	line("  PostgreSQL with pgvector, `mysql`/`mariadb` gives MariaDB, `redis` as `CACHE_STORE`,")
	line("  `QUEUE_CONNECTION`, `SESSION_DRIVER` or `BROADCAST_CONNECTION` (or laravel/horizon) gives")
	line("  Redis, `SCOUT_DRIVER=meilisearch` gives Meilisearch. A browser dependency (Playwright,")
	line("  Puppeteer, pest-plugin-browser, browsershot) brings a headless browser's libraries,")
	line("  laravel/dusk Google Chrome. Go comes from go.mod, Node from package.json.")
	line("- Anything that is neither apt nor one of those services — a proprietary SDK, a login to a")
	line("  third-party service, a secret the .env does not hold — cannot be arranged from the config")
	line("  and belongs on the checklist for the person.")
	return b.String()
}

// describeImage names the image a box is built from the way a person would.
func describeImage(image string) string {
	if strings.TrimSpace(image) == "" || image == DefaultImage {
		return "Ubuntu 24.04 LTS"
	}
	return image
}
