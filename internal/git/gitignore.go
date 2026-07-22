package git

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsChiefIgnored checks if .chief is gitignored either locally or globally.
// Returns true if .chief is already ignored, false otherwise.
func IsChiefIgnored(dir string) bool {
	// Use git check-ignore which respects both local and global gitignore
	cmd := exec.Command("git", "check-ignore", "-q", ".chief")
	cmd.Dir = dir
	err := cmd.Run()
	// Exit code 0 means it IS ignored, exit code 1 means it's NOT ignored
	return err == nil
}

// ensureLineInFile makes sure line appears on its own line in the file at path.
// If the file is missing it is created, prefixed with header (when non-empty)
// followed by line. If it exists, line is appended (with a separating newline
// when needed) unless line — or any of aliases — is already present as a
// trimmed line. It is idempotent and safe to call repeatedly.
func ensureLineInFile(path, line, header string, aliases ...string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			body := line + "\n"
			if header != "" {
				body = header + "\n" + body
			}
			return os.WriteFile(path, []byte(body), 0o644)
		}
		return err
	}

	for _, existing := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(existing)
		if trimmed == line {
			return nil
		}
		for _, a := range aliases {
			if trimmed == a {
				return nil
			}
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	// Add a newline before ours if the file doesn't end with one.
	if len(content) > 0 && content[len(content)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	_, err = f.WriteString(line + "\n")
	return err
}

// AddChiefToGitignore adds .chief to the local .gitignore file.
// Creates the file if it doesn't exist. A pre-existing bare ".chief" entry
// (without trailing slash) counts as already present.
func AddChiefToGitignore(dir string) error {
	return ensureLineInFile(filepath.Join(dir, ".gitignore"), ".chief/", "", ".chief")
}

// IgnoreLogsIn ensures dir's .gitignore carries the `*.log` pattern so chief's
// per-run log files (claude-<timestamp>.log) stay out of version control. It is
// scoped to the PRD directory the logs live in, so it works regardless of
// whether the project tracks or ignores `.chief/` as a whole and needs no
// pattern in the user's root .gitignore (the historical `claude.log` pattern
// never matched the timestamped names). Best-effort and idempotent: it writes
// only when the pattern is missing, and returns silently on any I/O error.
func IgnoreLogsIn(dir string) {
	_ = ensureLineInFile(filepath.Join(dir, ".gitignore"), "*.log", "# chief run logs — regenerated each run")
}

// PromptAddChiefToGitignore asks the user if they want to add .chief to .gitignore.
// Returns true if the user wants to add it, false otherwise.
func PromptAddChiefToGitignore() bool {
	fmt.Println("Would you like to add .chief to .gitignore?")
	fmt.Println("This keeps your PRD plans local and out of version control.")
	fmt.Println("(Not required, but recommended if you prefer local-only plans)")
	fmt.Print("\nAdd .chief to .gitignore? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}
