package box

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Stack is the kind of project a box is being built for. It decides which
// runtimes are installed at all: a Go module gets no PHP, a Laravel app gets no
// Go toolchain, and a project chief does not recognise gets the base alone.
type Stack string

const (
	StackUnknown Stack = ""
	StackLaravel Stack = "laravel"
	StackPHP     Stack = "php"
	StackNode    Stack = "node"
	StackGo      Stack = "go"
)

// Profile is what chief worked out about a project before creating its box:
// the runtimes it runs on, the versions of them this machine runs, the
// extensions its dependencies declare, the database its .env points at.
//
// It exists because the alternative — one fixed list of packages for every
// project — is right for exactly one project. The first real box was built for
// a Laravel app whose composer.json requires ext-imagick, whose lockfile is
// Bun's, and whose test suite drives a browser through Playwright; the fixed
// list had none of the three, and `composer install` would have been the first
// thing on the box to fail. Everything here is read from files the project
// already has, so a project describes its box by existing.
type Profile struct {
	Stack Stack
	// Framework is a name for the report, e.g. "Laravel 13". Empty when there is
	// none to name.
	Framework string

	// PHP is the series to install, e.g. "8.3", and PHPFrom says where that
	// answer came from — the report names it, because a version pinned from the
	// wrong place is the kind of thing you want to be able to see. Empty means
	// the project has no PHP.
	PHP, PHPFrom string
	// Extensions are the PHP extension packages to install, by their apt suffix:
	// "imagick" means php<PHP>-imagick. Sorted, without duplicates.
	Extensions []string

	// Database is the server the project's .env asks for: "pgsql", "mysql",
	// "sqlite" or empty. DatabaseName is the database to create on it.
	Database, DatabaseName string
	// Redis and Meilisearch are the other servers a Laravel app may lean on.
	Redis, Meilisearch bool

	// Node is the major to install, e.g. "24", with NodeFrom as its provenance.
	// Empty means no Node.
	Node, NodeFrom string
	// PackageManager is what the lockfile says the project is installed with:
	// "npm", "bun", "pnpm" or "yarn". PackageManagerVersion is the pin from
	// package.json's packageManager field, when there is one.
	PackageManager, PackageManagerVersion string

	// Browser says the test suite drives a browser through Playwright or
	// Puppeteer, which needs a few hundred megabytes of system libraries no
	// package manager installs. Chrome says it is Dusk, which wants Google
	// Chrome itself.
	Browser, Chrome bool

	// Go is the toolchain version from go.mod, e.g. "1.27.1". Empty means none.
	Go string
}

// DiscoverOptions are the answers a project's config gives ahead of detection.
// Each one, when set, wins over whatever the project or this machine says.
type DiscoverOptions struct {
	PHP  string
	Node string
}

// prober runs a command in a directory and returns its trimmed output, or ""
// when it could not run. It is a function so a test can answer for `php -v` on
// a machine without PHP.
type prober func(dir, name string, args ...string) string

func runProbe(dir, name string, args ...string) string {
	if _, err := exec.LookPath(name); err != nil {
		return ""
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Discover reads a project and returns the profile its box should be built to.
// It never fails: a project it cannot make sense of gets the base tools alone,
// and the report says so.
func Discover(baseDir string, opts DiscoverOptions) Profile {
	return discoverWith(baseDir, opts, runProbe)
}

func discoverWith(dir string, opts DiscoverOptions, probe prober) Profile {
	var p Profile

	composer := readJSON(filepath.Join(dir, "composer.json"))
	pkg := readJSON(filepath.Join(dir, "package.json"))
	goMod := readText(filepath.Join(dir, "go.mod"))
	// The .env is the project's own; without one, .env.example is what the
	// project ships as the shape of it, and for detection the shape is enough.
	env := readEnv(filepath.Join(dir, ".env"))
	if env == nil {
		env = readEnv(filepath.Join(dir, ".env.example"))
	}

	switch {
	case composer != nil:
		p.Stack = StackPHP
		if v, ok := composerRequirement(composer, "laravel/framework"); ok {
			p.Stack = StackLaravel
			p.Framework = "Laravel"
			if major := majorOf(lowerBound(v)); major != "" {
				p.Framework += " " + major
			}
		}
	case goMod != "":
		p.Stack = StackGo
	case pkg != nil:
		p.Stack = StackNode
	}

	if composer != nil {
		p.PHP, p.PHPFrom = phpVersion(dir, opts.PHP, composer, probe)
		deps := composerDependencies(composer)

		p.Database, p.DatabaseName = databaseFromEnv(env, p.Stack, composer)
		p.Redis = wantsRedis(env, deps)
		p.Meilisearch = strings.EqualFold(env["SCOUT_DRIVER"], "meilisearch")
		p.Browser = deps["pestphp/pest-plugin-browser"] || deps["spatie/browsershot"] || deps["laravel/dusk"]
		p.Chrome = deps["laravel/dusk"]

		p.Extensions = phpExtensions(dir, composer, p)
	}

	if pkg != nil {
		p.Node, p.NodeFrom = nodeVersion(dir, opts.Node, pkg, probe)
		p.PackageManager, p.PackageManagerVersion = packageManager(dir, pkg)
		deps := nodeDependencies(pkg)
		if deps["playwright"] || deps["@playwright/test"] || deps["puppeteer"] || deps["puppeteer-core"] {
			p.Browser = true
		}
	}
	// Playwright's system libraries are installed through npx, so a PHP project
	// that drives a browser needs Node even when it has no package.json.
	if p.Browser && p.Node == "" {
		p.Node, p.NodeFrom = nodeMajor, "needed for Playwright"
	}

	if goMod != "" {
		p.Go = goVersion(goMod)
	}
	return p
}

// Summary is the report `chief box up` prints before it creates anything, so
// what is about to be installed is visible while it is still cheap to stop.
func (p Profile) Summary() []string {
	var lines []string

	switch {
	case p.PHP != "":
		head := "PHP " + p.PHP
		if p.Framework != "" {
			head = p.Framework + " on " + head
		}
		if p.PHPFrom != "" {
			head += " (" + p.PHPFrom + ")"
		}
		lines = append(lines, head)
		if len(p.Extensions) > 0 {
			lines = append(lines, "extensions: "+strings.Join(p.Extensions, " "))
		}
	case p.Stack == StackGo:
		lines = append(lines, "Go "+p.Go)
	case p.Stack == StackNode:
		lines = append(lines, "Node project")
	default:
		lines = append(lines, "no stack recognised — base tools only (git, gh, Claude Code)")
	}

	var services []string
	switch p.Database {
	case "pgsql":
		services = append(services, `PostgreSQL with pgvector, database "`+p.DatabaseName+`"`)
	case "mysql":
		services = append(services, `MariaDB, database "`+p.DatabaseName+`"`)
	case "sqlite":
		services = append(services, "SQLite")
	}
	if p.Redis {
		services = append(services, "Redis")
	}
	if p.Meilisearch {
		services = append(services, "Meilisearch")
	}
	if len(services) > 0 {
		lines = append(lines, strings.Join(services, "; "))
	}

	if p.Node != "" {
		line := "Node " + p.Node
		if p.NodeFrom != "" {
			line += " (" + p.NodeFrom + ")"
		}
		if p.PackageManager != "" && p.PackageManager != "npm" {
			line += " with " + p.PackageManager
			if p.PackageManagerVersion != "" {
				line += " " + p.PackageManagerVersion
			}
		}
		lines = append(lines, line)
	}
	if p.Go != "" && p.Stack != StackGo {
		lines = append(lines, "Go "+p.Go)
	}

	switch {
	case p.Chrome:
		lines = append(lines, "browser tests: Google Chrome and Playwright's system libraries")
	case p.Browser:
		lines = append(lines, "browser tests: Playwright's system libraries")
	}
	return lines
}

// provisions is the short list the wait step names, so that "waiting" says
// what for.
func (p Profile) provisions() string {
	var parts []string
	if p.PHP != "" {
		parts = append(parts, "PHP "+p.PHP)
	}
	if p.Node != "" {
		parts = append(parts, "Node "+p.Node)
	}
	if p.Go != "" {
		parts = append(parts, "Go "+p.Go)
	}
	switch p.Database {
	case "pgsql":
		parts = append(parts, "PostgreSQL")
	case "mysql":
		parts = append(parts, "MariaDB")
	}
	if p.Redis {
		parts = append(parts, "Redis")
	}
	if p.Browser {
		parts = append(parts, "browser libraries")
	}
	parts = append(parts, "Claude Code")
	return strings.Join(parts, ", ")
}

// --- PHP ---------------------------------------------------------------------

var seriesPattern = regexp.MustCompile(`^\d+\.\d+$`)

// phpVersion decides which PHP the box gets, and says why.
//
// The order is the order of how deliberate each answer is. The config is a
// person deciding. Herd's isolation is a person deciding for this one site.
// The php on PATH is what the project is actually run with here, which is what
// "the same as my machine" means. composer.json is what the project would
// accept, which is not the same as what it is developed against. And the
// default is the default.
func phpVersion(dir, configured string, composer map[string]any, probe prober) (version, from string) {
	if v := strings.TrimSpace(configured); v != "" {
		return v, "box.php in .chief/config.yaml"
	}

	if v, site := herdIsolated(dir, probe); v != "" {
		return v, "Herd, isolated for " + site
	}

	if v := probe(dir, "php", "-r", "echo PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION;"); seriesPattern.MatchString(v) {
		return v, "the php on this machine"
	}

	if platform, ok := composer["config"].(map[string]any); ok {
		if raw, ok := platform["platform"].(map[string]any); ok {
			if v, ok := raw["php"].(string); ok {
				if s := series(lowerBound(v)); s != "" {
					return s, "composer.json's platform"
				}
			}
		}
	}
	if v, ok := composerRequirement(composer, "php"); ok {
		if s := series(lowerBound(v)); s != "" {
			return s, "the lowest composer.json accepts"
		}
	}
	return phpSeries, "chief's default"
}

// herdIsolated returns the PHP version Herd runs this project's site on, when
// the site has been isolated to one.
//
// Herd's `php` on PATH is the global version, whatever the site is isolated
// to, so asking it is wrong for exactly the projects that bothered to pin one.
// `herd isolated` lists the sites that did.
func herdIsolated(dir string, probe prober) (version, site string) {
	table := probe(dir, "herd", "isolated")
	if table == "" {
		return "", ""
	}
	tld := probe(dir, "herd", "tld")
	if tld == "" {
		tld = "test"
	}
	site = filepath.Base(dir) + "." + tld

	for _, line := range strings.Split(table, "\n") {
		// | agency-os.test | 8.4 |
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			continue
		}
		if strings.TrimSpace(cells[1]) != site {
			continue
		}
		if v := strings.TrimSpace(cells[2]); seriesPattern.MatchString(v) {
			return v, site
		}
	}
	return "", ""
}

// phpBuiltins are the extensions that come with php-cli and php-common on
// Ubuntu, so a composer.json asking for them asks for nothing extra.
var phpBuiltins = map[string]bool{
	"calendar": true, "core": true, "ctype": true, "date": true, "exif": true,
	"ffi": true, "fileinfo": true, "filter": true, "ftp": true, "gettext": true,
	"hash": true, "iconv": true, "json": true, "libxml": true, "openssl": true,
	"pcntl": true, "pcre": true, "pdo": true, "phar": true, "posix": true,
	"random": true, "readline": true, "reflection": true, "session": true,
	"shmop": true, "sockets": true, "sodium": true, "spl": true, "standard": true,
	"sysvmsg": true, "sysvsem": true, "sysvshm": true, "tokenizer": true, "zlib": true,
}

// phpAliases map the names composer knows extensions by onto the apt packages
// that carry them, where the two differ.
var phpAliases = map[string]string{
	"pdo_mysql": "mysql", "mysqli": "mysql", "mysqlnd": "mysql",
	"pdo_pgsql":  "pgsql",
	"pdo_sqlite": "sqlite3", "sqlite": "sqlite3",
	"dom": "xml", "simplexml": "xml", "xmlreader": "xml", "xmlwriter": "xml",
	"zend-opcache": "opcache",
}

// phpBaseline is installed for every PHP project regardless of what it
// declares: what Composer needs to unpack anything, and what a Laravel app
// fails to boot without.
var phpBaseline = []string{"bcmath", "curl", "gd", "intl", "mbstring", "sqlite3", "xml", "zip"}

// phpExtensions collects the extensions a project's dependencies declare —
// from the lockfile, so the transitive ones count — and adds the driver for
// the database it uses.
func phpExtensions(dir string, composer map[string]any, p Profile) []string {
	want := map[string]bool{}
	for _, e := range phpBaseline {
		want[e] = true
	}

	add := func(name string) {
		name = strings.ToLower(strings.TrimPrefix(name, "ext-"))
		if alias, ok := phpAliases[name]; ok {
			name = alias
		}
		if name == "" || phpBuiltins[name] {
			return
		}
		want[name] = true
	}
	for name := range composerDependencies(composer) {
		if strings.HasPrefix(name, "ext-") {
			add(name)
		}
	}
	if lock := readJSON(filepath.Join(dir, "composer.lock")); lock != nil {
		for _, section := range []string{"packages", "packages-dev"} {
			list, _ := lock[section].([]any)
			for _, item := range list {
				pkg, _ := item.(map[string]any)
				require, _ := pkg["require"].(map[string]any)
				for name := range require {
					if strings.HasPrefix(name, "ext-") {
						add(name)
					}
				}
			}
		}
	}

	for _, driver := range []string{p.Database, phpunitDatabase(dir)} {
		switch driver {
		case "pgsql":
			want["pgsql"] = true
		case "mysql":
			want["mysql"] = true
		case "sqlite":
			want["sqlite3"] = true
		}
	}
	if p.Redis {
		// Laravel's default Redis client is phpredis, the extension.
		want["redis"] = true
	}

	out := make([]string, 0, len(want))
	for e := range want {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

var phpunitEnv = regexp.MustCompile(`<env\s+name="DB_CONNECTION"\s+value="([^"]*)"`)

// phpunitDatabase is the driver the test suite uses, which is often not the
// one the app does: many Laravel projects test against SQLite and run against
// something else, and the box needs both.
func phpunitDatabase(dir string) string {
	for _, name := range []string{"phpunit.xml", "phpunit.xml.dist", "phpunit.dist.xml"} {
		if m := phpunitEnv.FindStringSubmatch(readText(filepath.Join(dir, name))); m != nil {
			return normaliseDriver(m[1])
		}
	}
	return ""
}

// --- Database and services ----------------------------------------------------

var databaseNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// databaseFromEnv reads which database server the project runs against and
// what its database is called.
//
// A Laravel app that says nothing has a default, and the default changed: up
// to Laravel 10 it was MySQL, from 11 on it is SQLite. The framework version in
// composer.json decides which silence this is.
func databaseFromEnv(env map[string]string, stack Stack, composer map[string]any) (driver, name string) {
	driver = normaliseDriver(env["DB_CONNECTION"])
	if driver == "" && stack == StackLaravel {
		driver = "sqlite"
		if v, ok := composerRequirement(composer, "laravel/framework"); ok {
			if major, err := strconv.Atoi(majorOf(lowerBound(v))); err == nil && major < 11 {
				driver = "mysql"
			}
		}
	}
	if driver == "" || driver == "sqlite" {
		return driver, ""
	}
	name = env["DB_DATABASE"]
	if !databaseNamePattern.MatchString(name) {
		// Laravel's own fallback when DB_DATABASE is unset.
		name = "laravel"
	}
	return driver, name
}

func normaliseDriver(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "pgsql", "postgres", "postgresql":
		return "pgsql"
	case "mysql", "mariadb":
		return "mysql"
	case "sqlite":
		return "sqlite"
	}
	return ""
}

// wantsRedis reports whether the project actually talks to Redis: any of the
// stores that can be Redis is, or Horizon is installed, which cannot run
// without it.
func wantsRedis(env map[string]string, deps map[string]bool) bool {
	for _, key := range []string{"CACHE_STORE", "CACHE_DRIVER", "QUEUE_CONNECTION", "SESSION_DRIVER", "BROADCAST_CONNECTION"} {
		if strings.EqualFold(env[key], "redis") {
			return true
		}
	}
	return deps["laravel/horizon"]
}

// --- Node ---------------------------------------------------------------------

var nodeVersionPattern = regexp.MustCompile(`^v?(\d+)(\.\d+)*$`)

// nodeVersion decides the Node major, in the same order of deliberateness as
// PHP: config, this machine, the project's own pin, what package.json would
// accept, the default.
func nodeVersion(dir, configured string, pkg map[string]any, probe prober) (major, from string) {
	if v := strings.TrimSpace(configured); v != "" {
		return majorOf(v), "box.node in .chief/config.yaml"
	}
	if v := probe(dir, "node", "-v"); nodeVersionPattern.MatchString(v) {
		return majorOf(v), "the node on this machine"
	}
	for _, name := range []string{".nvmrc", ".node-version"} {
		v := strings.TrimSpace(readText(filepath.Join(dir, name)))
		if nodeVersionPattern.MatchString(v) {
			return majorOf(v), name
		}
	}
	if engines, ok := pkg["engines"].(map[string]any); ok {
		if v, ok := engines["node"].(string); ok {
			if m := majorOf(lowerBound(v)); m != "" {
				return m, "the lowest package.json accepts"
			}
		}
	}
	return nodeMajor, "chief's default"
}

// packageManager reads which tool installs the project's JavaScript: the
// lockfile is the proof of what was actually used, and the packageManager
// field in package.json is the project saying so out loud, version and all.
func packageManager(dir string, pkg map[string]any) (name, version string) {
	switch {
	case exists(filepath.Join(dir, "bun.lock")), exists(filepath.Join(dir, "bun.lockb")):
		name = "bun"
	case exists(filepath.Join(dir, "pnpm-lock.yaml")):
		name = "pnpm"
	case exists(filepath.Join(dir, "yarn.lock")):
		name = "yarn"
	default:
		name = "npm"
	}
	if field, ok := pkg["packageManager"].(string); ok {
		// "pnpm@9.1.0", possibly with a "+sha512..." integrity suffix.
		field, _, _ = strings.Cut(field, "+")
		if n, v, found := strings.Cut(field, "@"); found && n != "" {
			name, version = n, v
		} else if field != "" {
			name = field
		}
	}
	return name, version
}

// --- Go -----------------------------------------------------------------------

var (
	goToolchainLine = regexp.MustCompile(`(?m)^toolchain\s+go(\d+\.\d+(?:\.\d+)?)`)
	goDirectiveLine = regexp.MustCompile(`(?m)^go\s+(\d+\.\d+(?:\.\d+)?)`)
)

// goVersion is the toolchain go.mod asks for, in the three-part form the
// download page uses: "1.27" is served as go1.27.0.
func goVersion(goMod string) string {
	v := ""
	if m := goToolchainLine.FindStringSubmatch(goMod); m != nil {
		v = m[1]
	} else if m := goDirectiveLine.FindStringSubmatch(goMod); m != nil {
		v = m[1]
	}
	if v != "" && strings.Count(v, ".") == 1 {
		v += ".0"
	}
	return v
}

// --- Version constraints ------------------------------------------------------

var versionInConstraint = regexp.MustCompile(`\d+(\.\d+)*`)

// lowerBound is the lowest version a constraint accepts, as best as can be
// read without a resolver: the first number in the first alternative. "^8.3"
// is 8.3, ">=8.2 <8.5" is 8.2, "8.3.*" is 8.3, "^8.2|^8.3" is 8.2.
func lowerBound(constraint string) string {
	first := constraint
	if i := strings.IndexAny(first, "|"); i >= 0 {
		first = first[:i]
	}
	return versionInConstraint.FindString(first)
}

// series is the major.minor of a version, which is what PHP packages are named
// by.
func series(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		if len(parts) == 1 && parts[0] != "" {
			return parts[0] + ".0"
		}
		return ""
	}
	return parts[0] + "." + parts[1]
}

var digits = regexp.MustCompile(`^\d+$`)

// majorOf is the leading number of a version, with or without a "v".
func majorOf(version string) string {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	major, _, _ := strings.Cut(version, ".")
	if !digits.MatchString(major) {
		return ""
	}
	return major
}

// --- Reading the project ------------------------------------------------------

func readJSON(path string) map[string]any {
	data, err := os.ReadFile(path) //nolint:gosec // a fixed name under the project being inspected
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func readText(path string) string {
	data, err := os.ReadFile(path) //nolint:gosec // a fixed name under the project being inspected
	if err != nil {
		return ""
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readEnv parses a dotenv file into a map: KEY=VALUE per line, quotes
// stripped, comments and blank lines ignored. Nil when there is no file, which
// is different from an empty one.
func readEnv(path string) map[string]string {
	text := readText(path)
	if text == "" {
		if !exists(path) {
			return nil
		}
		return map[string]string{}
	}
	env := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := envLine(line)
		if ok {
			env[key] = value
		}
	}
	return env
}

// envLine splits one dotenv line into its key and unquoted value.
func envLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	key, value, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	} else if i := strings.Index(value, " #"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	return key, value, key != ""
}

// composerDependencies is every package composer.json names, in require and
// require-dev, as a set.
func composerDependencies(composer map[string]any) map[string]bool {
	deps := map[string]bool{}
	for _, section := range []string{"require", "require-dev"} {
		list, _ := composer[section].(map[string]any)
		for name := range list {
			deps[strings.ToLower(name)] = true
		}
	}
	return deps
}

// composerRequirement is the constraint composer.json puts on one package.
func composerRequirement(composer map[string]any, name string) (string, bool) {
	require, _ := composer["require"].(map[string]any)
	v, ok := require[name].(string)
	return v, ok
}

// nodeDependencies is every package package.json names, as a set.
func nodeDependencies(pkg map[string]any) map[string]bool {
	deps := map[string]bool{}
	for _, section := range []string{"dependencies", "devDependencies"} {
		list, _ := pkg[section].(map[string]any)
		for name := range list {
			deps[strings.ToLower(name)] = true
		}
	}
	return deps
}
