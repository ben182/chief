package box

import (
	"sort"
	"strings"
)

// remoteDatabaseUser is the one account every database server on a box has,
// created by cloud-init with the same name as its password. The .env that
// arrives from the developer's machine is pointed at it, because the
// alternative — creating whatever account that .env names — means running a
// password from somebody's laptop through a cloud-config.
const remoteDatabaseUser = "chief"

// envOverrides are the keys in a project's .env that describe this machine and
// have to describe the box instead.
//
// The .env is copied over because it is the one file a Laravel app cannot run
// without and git does not carry. But it was written for the laptop it came
// from: the database is Herd's or DBngin's, on this host, with this user. On
// the box the same database exists — cloud-init made it, from what discovery
// read out of this very file — under the box's own account. This is the
// translation between the two, and nothing else about the file is touched.
func envOverrides(p Profile, current map[string]string) map[string]string {
	over := map[string]string{}

	switch p.Database {
	case "pgsql":
		over["DB_HOST"] = "127.0.0.1"
		over["DB_PORT"] = "5432"
		over["DB_USERNAME"] = remoteDatabaseUser
		over["DB_PASSWORD"] = remoteDatabaseUser
		over["DB_DATABASE"] = p.DatabaseName
	case "mysql":
		over["DB_HOST"] = "127.0.0.1"
		over["DB_PORT"] = "3306"
		over["DB_USERNAME"] = remoteDatabaseUser
		over["DB_PASSWORD"] = remoteDatabaseUser
		over["DB_DATABASE"] = p.DatabaseName
	case "sqlite":
		// An absolute path is a path on the laptop. Laravel's own default is
		// the project's database directory, and `migrate --force` creates the
		// file there when it is missing.
		if strings.HasPrefix(current["DB_DATABASE"], "/") {
			over["DB_DATABASE"] = "database/database.sqlite"
		}
	}
	if p.Redis {
		over["REDIS_HOST"] = "127.0.0.1"
		over["REDIS_PORT"] = "6379"
		over["REDIS_PASSWORD"] = "null"
	}
	if p.Meilisearch {
		// Development mode: no master key, and the client must not send one.
		over["MEILISEARCH_HOST"] = "http://127.0.0.1:7700"
		over["MEILISEARCH_KEY"] = ""
	}
	return over
}

// applyEnv rewrites the keys in overrides inside a dotenv file, in place where
// they exist and appended where they do not. Everything else — comments, blank
// lines, the order, the keys not mentioned — stays exactly as it was, because
// the file is somebody's and a diff of it should show only what chief meant.
func applyEnv(content string, overrides map[string]string) string {
	if len(overrides) == 0 {
		return content
	}
	seen := map[string]bool{}
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	for i, line := range lines {
		key, _, ok := envLine(line)
		if !ok {
			continue
		}
		value, wanted := overrides[key]
		if !wanted {
			continue
		}
		lines[i] = key + "=" + quoteEnv(value)
		seen[key] = true
	}

	missing := make([]string, 0, len(overrides))
	for key := range overrides {
		if !seen[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		lines = append(lines, "", "# set by chief for the box")
		for _, key := range missing {
			lines = append(lines, key+"="+quoteEnv(overrides[key]))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// quoteEnv wraps a value in quotes when dotenv would otherwise misread it.
func quoteEnv(v string) string {
	if v == "" || strings.ContainsAny(v, " #\"'$") {
		return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
	}
	return v
}
