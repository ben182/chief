package box

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeProject lays out a project directory from a map of relative path to
// content, so a test can describe the project it wants in a few lines.
func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// answers is a prober that plays back canned command output, keyed by the
// command line ("herd isolated") or just the command ("php"), and knows
// nothing about anything else.
func answers(m map[string]string) prober {
	return func(_, name string, args ...string) string {
		if out, ok := m[strings.TrimSpace(name+" "+strings.Join(args, " "))]; ok {
			return out
		}
		return m[name]
	}
}

// silent is a machine with none of the tools installed.
var silent = answers(nil)

func TestDiscoverReadsALaravelAppTheWayItsFirstRealBoxNeeded(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"composer.json": `{
			"require": {"php": "^8.3", "ext-imagick": "*", "laravel/framework": "^13.0", "laravel/horizon": "^5.0"},
			"require-dev": {"pestphp/pest-plugin-browser": "^4.0"}
		}`,
		"composer.lock": `{"packages": [
			{"name": "some/pkg", "require": {"ext-dom": "*", "ext-pdo_pgsql": "*", "ext-ctype": "*", "ext-sodium": "*"}}
		], "packages-dev": [{"name": "dev/pkg", "require": {"ext-xdebug": "*"}}]}`,
		"package.json": `{"devDependencies": {"vite": "^7"}}`,
		"bun.lock":     "",
		".env":         "DB_CONNECTION=pgsql\nDB_HOST=127.0.0.1\nDB_DATABASE=agency_os\nQUEUE_CONNECTION=database\nSCOUT_DRIVER=database\n",
		"phpunit.xml":  `<phpunit><php><env name="DB_CONNECTION" value="sqlite"/></php></phpunit>`,
	})
	machine := answers(map[string]string{"php": "8.5", "node": "v24.21.0"})

	p := discoverWith(dir, DiscoverOptions{}, machine)

	if p.Stack != StackLaravel || p.Framework != "Laravel 13" {
		t.Errorf("stack = %q %q, want Laravel 13", p.Stack, p.Framework)
	}
	// This machine's PHP, because that is what the project is developed with.
	if p.PHP != "8.5" || !strings.Contains(p.PHPFrom, "this machine") {
		t.Errorf("PHP = %q from %q, want 8.5 from this machine", p.PHP, p.PHPFrom)
	}
	got := strings.Join(p.Extensions, " ")
	// imagick from composer.json; pdo_pgsql from the lock, under the name apt
	// knows; sqlite3 because the tests use it; redis because Horizon is there.
	for _, want := range []string{"imagick", "pgsql", "sqlite3", "redis", "xdebug", "xml"} {
		if !strings.Contains(" "+got+" ", " "+want+" ") {
			t.Errorf("extensions %q lack %s", got, want)
		}
	}
	// What php-cli already ships is not asked for again, and "dom" arrives as
	// the package that carries it rather than under its own name.
	for _, unwanted := range []string{"ctype", "sodium", "dom", "pdo_pgsql"} {
		if strings.Contains(" "+got+" ", " "+unwanted+" ") {
			t.Errorf("extensions %q list %s, which is built in or an alias", got, unwanted)
		}
	}
	if p.Database != "pgsql" || p.DatabaseName != "agency_os" {
		t.Errorf("database = %q %q", p.Database, p.DatabaseName)
	}
	if !p.Redis {
		t.Error("Horizon is installed but Redis was not detected")
	}
	if p.Meilisearch {
		t.Error("Meilisearch detected though the Scout driver is the database")
	}
	if p.Node != "24" || p.PackageManager != "bun" {
		t.Errorf("node = %q with %q, want 24 with bun", p.Node, p.PackageManager)
	}
	if !p.Browser || p.Chrome {
		t.Errorf("browser = %v chrome = %v, want Playwright without Chrome", p.Browser, p.Chrome)
	}
	if p.Go != "" {
		t.Errorf("Go = %q for a PHP project", p.Go)
	}
}

func TestDiscoverPrefersTheVersionHerdIsolatesTheSiteTo(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"composer.json": `{"require": {"php": "^8.2", "laravel/framework": "^12.0"}}`,
	})
	site := filepath.Base(dir) + ".test"
	machine := answers(map[string]string{
		"php":           "8.5",
		"herd tld":      "test",
		"herd isolated": "+------+-----+\n| Path | PHP Version |\n| other.test | 8.1 |\n| " + site + " | 8.4 |\n+------+-----+",
	})

	p := discoverWith(dir, DiscoverOptions{}, machine)
	if p.PHP != "8.4" || !strings.Contains(p.PHPFrom, "Herd") {
		t.Errorf("PHP = %q from %q, want Herd's 8.4 over the global 8.5", p.PHP, p.PHPFrom)
	}

	// A site Herd has not isolated falls through to the php on PATH.
	other := answers(map[string]string{"php": "8.5", "herd tld": "test", "herd isolated": "| other.test | 8.1 |"})
	if p := discoverWith(dir, DiscoverOptions{}, other); p.PHP != "8.5" {
		t.Errorf("PHP = %q, want the global php when the site is not isolated", p.PHP)
	}
}

func TestDiscoverFallsBackThroughTheProjectsOwnAnswers(t *testing.T) {
	// No php on this machine: composer.json's platform pin wins.
	dir := writeProject(t, map[string]string{
		"composer.json": `{"require": {"php": "^8.2"}, "config": {"platform": {"php": "8.3.0"}}}`,
	})
	if p := discoverWith(dir, DiscoverOptions{}, silent); p.PHP != "8.3" || !strings.Contains(p.PHPFrom, "platform") {
		t.Errorf("PHP = %q from %q, want 8.3 from the platform pin", p.PHP, p.PHPFrom)
	}

	// No pin either: the lowest version the constraint accepts.
	dir = writeProject(t, map[string]string{
		"composer.json": `{"require": {"php": ">=8.2 <8.5"}}`,
	})
	if p := discoverWith(dir, DiscoverOptions{}, silent); p.PHP != "8.2" {
		t.Errorf("PHP = %q, want 8.2 from the constraint", p.PHP)
	}

	// Nothing at all: the default, and it says so.
	dir = writeProject(t, map[string]string{"composer.json": `{"require": {}}`})
	if p := discoverWith(dir, DiscoverOptions{}, silent); p.PHP != phpSeries || !strings.Contains(p.PHPFrom, "default") {
		t.Errorf("PHP = %q from %q, want the default", p.PHP, p.PHPFrom)
	}

	// The config beats everything, including this machine.
	if p := discoverWith(dir, DiscoverOptions{PHP: "8.1"}, answers(map[string]string{"php": "8.5"})); p.PHP != "8.1" {
		t.Errorf("PHP = %q, want the configured 8.1", p.PHP)
	}
}

func TestDiscoverReadsTheDatabaseOutOfTheEnv(t *testing.T) {
	cases := []struct {
		name, env, composer string
		driver, database    string
	}{
		{"mysql", "DB_CONNECTION=mysql\nDB_DATABASE=shop\n", `{"require": {"laravel/framework": "^12.0"}}`, "mysql", "shop"},
		{"mariadb is mysql", "DB_CONNECTION=mariadb\n", `{"require": {}}`, "mysql", "laravel"},
		{"quoted", `DB_CONNECTION="pgsql"` + "\nDB_DATABASE='my_db'\n", `{"require": {}}`, "pgsql", "my_db"},
		{"sqlite has no server", "DB_CONNECTION=sqlite\nDB_DATABASE=/Users/x/db.sqlite\n", `{"require": {}}`, "sqlite", ""},
		{"laravel 11 defaults to sqlite", "APP_NAME=x\n", `{"require": {"laravel/framework": "^11.0"}}`, "sqlite", ""},
		{"laravel 10 defaulted to mysql", "APP_NAME=x\n", `{"require": {"laravel/framework": "^10.0"}}`, "mysql", "laravel"},
		{"a name with a space is not a database", "DB_CONNECTION=pgsql\nDB_DATABASE=my db\n", `{"require": {}}`, "pgsql", "laravel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeProject(t, map[string]string{"composer.json": tc.composer, ".env": tc.env})
			p := discoverWith(dir, DiscoverOptions{}, silent)
			if p.Database != tc.driver || p.DatabaseName != tc.database {
				t.Errorf("database = %q %q, want %q %q", p.Database, p.DatabaseName, tc.driver, tc.database)
			}
		})
	}

	// Without a .env, the example the project ships is the shape of it.
	dir := writeProject(t, map[string]string{
		"composer.json": `{"require": {}}`,
		".env.example":  "DB_CONNECTION=mysql\n",
	})
	if p := discoverWith(dir, DiscoverOptions{}, silent); p.Database != "mysql" {
		t.Errorf("database = %q, want mysql from .env.example", p.Database)
	}
}

func TestDiscoverReadsRedisAndSearchFromTheEnv(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"composer.json": `{"require": {"laravel/scout": "^11"}}`,
		".env":          "CACHE_STORE=redis\nSCOUT_DRIVER=meilisearch\n",
	})
	p := discoverWith(dir, DiscoverOptions{}, silent)
	if !p.Redis || !p.Meilisearch {
		t.Errorf("redis = %v meilisearch = %v, want both", p.Redis, p.Meilisearch)
	}
	if !strings.Contains(strings.Join(p.Extensions, " "), "redis") {
		t.Error("Redis in use but the phpredis extension is not installed")
	}
}

func TestDiscoverReadsNodeAndItsPackageManager(t *testing.T) {
	// This machine's node, and the lockfile's tool.
	dir := writeProject(t, map[string]string{"package.json": `{}`, "pnpm-lock.yaml": ""})
	p := discoverWith(dir, DiscoverOptions{}, answers(map[string]string{"node": "v22.3.1"}))
	if p.Stack != StackNode || p.Node != "22" || p.PackageManager != "pnpm" {
		t.Errorf("got %q node %q with %q, want a Node project on 22 with pnpm", p.Stack, p.Node, p.PackageManager)
	}

	// No node here: the project's own pin.
	dir = writeProject(t, map[string]string{"package.json": `{}`, ".nvmrc": "v20.11.0\n", "yarn.lock": ""})
	p = discoverWith(dir, DiscoverOptions{}, silent)
	if p.Node != "20" || p.NodeFrom != ".nvmrc" || p.PackageManager != "yarn" {
		t.Errorf("node = %q from %q with %q", p.Node, p.NodeFrom, p.PackageManager)
	}

	// The packageManager field names the tool and the version, out loud.
	dir = writeProject(t, map[string]string{
		"package.json": `{"packageManager": "pnpm@9.1.0+sha512.abc", "engines": {"node": ">=18"}}`,
	})
	p = discoverWith(dir, DiscoverOptions{}, silent)
	if p.PackageManager != "pnpm" || p.PackageManagerVersion != "9.1.0" {
		t.Errorf("package manager = %q %q", p.PackageManager, p.PackageManagerVersion)
	}
	if p.Node != "18" {
		t.Errorf("node = %q, want 18 from engines", p.Node)
	}

	// Playwright in package.json is a browser test suite.
	dir = writeProject(t, map[string]string{"package.json": `{"devDependencies": {"@playwright/test": "^1.50"}}`})
	if p := discoverWith(dir, DiscoverOptions{}, silent); !p.Browser {
		t.Error("Playwright in package.json was not seen as a browser suite")
	}
}

func TestDiscoverGivesABrowserSuiteNodeEvenWithoutPackageJSON(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"composer.json": `{"require-dev": {"laravel/dusk": "^8.0"}}`,
	})
	p := discoverWith(dir, DiscoverOptions{}, silent)
	if !p.Browser || !p.Chrome {
		t.Errorf("browser = %v chrome = %v, want Dusk to bring Chrome", p.Browser, p.Chrome)
	}
	// Playwright's system libraries arrive through npx, which needs Node.
	if p.Node == "" {
		t.Error("a browser suite without package.json got no Node to install the libraries with")
	}
}

func TestDiscoverReadsAGoModule(t *testing.T) {
	dir := writeProject(t, map[string]string{"go.mod": "module example.com/x\n\ngo 1.27\n"})
	p := discoverWith(dir, DiscoverOptions{}, silent)
	if p.Stack != StackGo || p.Go != "1.27.0" {
		t.Errorf("got %q %q, want a Go project on 1.27.0", p.Stack, p.Go)
	}
	if p.PHP != "" || p.Node != "" || p.Database != "" {
		t.Errorf("a Go module was given PHP %q, Node %q, database %q", p.PHP, p.Node, p.Database)
	}

	// The toolchain line is the more precise answer when there is one.
	dir = writeProject(t, map[string]string{"go.mod": "module x\n\ngo 1.26\n\ntoolchain go1.27.1\n"})
	if p := discoverWith(dir, DiscoverOptions{}, silent); p.Go != "1.27.1" {
		t.Errorf("Go = %q, want the toolchain's 1.27.1", p.Go)
	}
}

func TestDiscoverSaysSoWhenItRecognisesNothing(t *testing.T) {
	p := discoverWith(t.TempDir(), DiscoverOptions{}, silent)
	if p.Stack != StackUnknown {
		t.Errorf("stack = %q for an empty directory", p.Stack)
	}
	summary := strings.Join(p.Summary(), "\n")
	if !strings.Contains(summary, "no stack recognised") {
		t.Errorf("the summary does not admit it found nothing:\n%s", summary)
	}
	if !strings.Contains(p.provisions(), "Claude Code") {
		t.Error("the wait step does not name the one thing every box gets")
	}
}

func TestSummaryNamesWhatWillBeInstalledAndWhy(t *testing.T) {
	summary := strings.Join(laravelProfile().Summary(), "\n")
	for _, want := range []string{"Laravel 13 on PHP 8.3", "Herd, isolated for demo.test", "imagick", `PostgreSQL, database "agency_os"`, "Redis", "Node 24", "bun", "Playwright"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary lacks %q:\n%s", want, summary)
		}
	}
}

func TestLowerBoundReadsWhatComposerWouldAccept(t *testing.T) {
	cases := map[string]string{
		"^8.3": "8.3", "~8.2.0": "8.2.0", ">=8.2 <8.5": "8.2", "8.3.*": "8.3",
		"^8.2|^8.3": "8.2", "^8.2 || ^8.3": "8.2", "*": "", "": "",
	}
	for in, want := range cases {
		if got := lowerBound(in); got != want {
			t.Errorf("lowerBound(%q) = %q, want %q", in, got, want)
		}
	}
	if got := series("8.3.12"); got != "8.3" {
		t.Errorf("series = %q", got)
	}
	if got := majorOf("v24.1.0"); got != "24" {
		t.Errorf("majorOf = %q", got)
	}
}

func TestApplyEnvRewritesInPlaceAndAppendsTheRest(t *testing.T) {
	in := "# app\nAPP_NAME=Demo\nDB_CONNECTION=pgsql\nDB_HOST=db.local\nDB_USERNAME=\"ben\" # me\n\nMAIL_MAILER=smtp\n"
	out := applyEnv(in, map[string]string{
		"DB_HOST": "127.0.0.1", "DB_USERNAME": "chief", "DB_PASSWORD": "chief", "REDIS_HOST": "127.0.0.1",
	})

	// Rewritten where they were, so the diff against the original is the change
	// and nothing else.
	for _, want := range []string{"# app\nAPP_NAME=Demo\n", "DB_HOST=127.0.0.1\n", "DB_USERNAME=chief\n", "MAIL_MAILER=smtp\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "db.local") || strings.Contains(out, `"ben"`) {
		t.Errorf("the old values survived:\n%s", out)
	}
	// Appended, under a line that says who did it, in a stable order.
	tail := out[strings.Index(out, "# set by chief"):]
	if !strings.Contains(tail, "DB_PASSWORD=chief\nREDIS_HOST=127.0.0.1\n") {
		t.Errorf("missing keys were not appended in order:\n%s", tail)
	}
	if strings.Count(out, "DB_HOST=") != 1 {
		t.Error("a rewritten key was also appended")
	}

	// Nothing to change means the file is left byte for byte.
	if got := applyEnv(in, nil); got != in {
		t.Error("an empty override set changed the file")
	}
	// An empty value is quoted so dotenv reads it as set-to-nothing.
	if got := applyEnv("A=1\n", map[string]string{"MEILISEARCH_KEY": ""}); !strings.Contains(got, `MEILISEARCH_KEY=""`) {
		t.Errorf("empty value not quoted:\n%s", got)
	}
}

func TestEnvOverridesPointTheEnvAtTheBox(t *testing.T) {
	pg := envOverrides(Profile{Database: "pgsql", DatabaseName: "agency_os", Redis: true}, nil)
	for key, want := range map[string]string{
		"DB_HOST": "127.0.0.1", "DB_PORT": "5432", "DB_USERNAME": "chief", "DB_PASSWORD": "chief",
		"DB_DATABASE": "agency_os", "REDIS_HOST": "127.0.0.1",
	} {
		if pg[key] != want {
			t.Errorf("%s = %q, want %q", key, pg[key], want)
		}
	}

	my := envOverrides(Profile{Database: "mysql", DatabaseName: "shop"}, nil)
	if my["DB_PORT"] != "3306" || my["DB_DATABASE"] != "shop" {
		t.Errorf("mysql overrides = %v", my)
	}
	if _, ok := my["REDIS_HOST"]; ok {
		t.Error("Redis keys set for a project without Redis")
	}

	// SQLite: only a path that names the laptop's disk is touched.
	sq := envOverrides(Profile{Database: "sqlite"}, map[string]string{"DB_DATABASE": "/Users/ben/db.sqlite"})
	if sq["DB_DATABASE"] != "database/database.sqlite" || len(sq) != 1 {
		t.Errorf("sqlite overrides = %v", sq)
	}
	if got := envOverrides(Profile{Database: "sqlite"}, map[string]string{"DB_DATABASE": "database/db.sqlite"}); len(got) != 0 {
		t.Errorf("a relative sqlite path was rewritten: %v", got)
	}

	me := envOverrides(Profile{Meilisearch: true}, nil)
	if me["MEILISEARCH_HOST"] != "http://127.0.0.1:7700" {
		t.Errorf("meilisearch overrides = %v", me)
	}
	if _, ok := me["MEILISEARCH_KEY"]; !ok {
		t.Error("the key is not cleared; the client would send the laptop's")
	}
}

// TestDiscoverARealProject prints what discovery makes of a directory on this
// machine. It is a way of looking, not a check: set CHIEF_DISCOVER_DIR to a
// project and run it with -v.
func TestDiscoverARealProject(t *testing.T) {
	dir := os.Getenv("CHIEF_DISCOVER_DIR")
	if dir == "" {
		t.Skip("set CHIEF_DISCOVER_DIR to a project to see what discovery reads out of it")
	}
	p := Discover(dir, DiscoverOptions{})
	for _, line := range p.Summary() {
		t.Log(line)
	}
	t.Logf("provisioning: %s", p.provisions())
	t.Logf("env overrides: %v", envOverrides(p, readEnv(filepath.Join(dir, ".env"))))

	// And the cloud-config it renders to, which Hetzner caps at 32 KiB.
	_, runcmd, _ := parseCloudInit(t, cloudInitOptions{Hostname: "probe", Profile: p})
	if size := len(cloudInit(cloudInitOptions{Hostname: "probe", Profile: p})); size > 32*1024 {
		t.Errorf("cloud-config is %d bytes, over Hetzner's 32 KiB limit", size)
	}
	t.Logf("runcmd:\n%s", runcmd)
}
