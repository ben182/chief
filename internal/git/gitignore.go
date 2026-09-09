package git

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// IsChiefIgnored checks if .chief is gitignored either locally or globally.
// Returns true if .chief is already ignored, false otherwise.
func IsChiefIgnored(dir string) bool {
	return isPathIgnored(dir, ".chief")
}

// isPathIgnored asks git whether relPath — given relative to dir — is covered
// by an ignore rule. It goes through `git check-ignore` rather than reading
// .gitignore because that is the only way to see the whole picture: the repo's
// own file, nested .gitignore files, .git/info/exclude and the user's global
// core.excludesFile. The path need not exist; a rule on any of its parent
// directories counts.
func isPathIgnored(dir, relPath string) bool {
	cmd := exec.Command("git", "check-ignore", "-q", "--", relPath)
	cmd.Dir = dir
	// Exit code 0 means it IS ignored, exit code 1 means it's NOT ignored.
	return cmd.Run() == nil
}

// ensureLineInFile makes sure line appears on its own line in the file at path.
// If the file is missing it is created, prefixed with header (when non-empty)
// followed by line. If it exists, line is appended (with a separating newline
// when needed) unless line — or any of aliases — is already present as a
// trimmed line. It is idempotent and safe to call repeatedly.
func ensureLineInFile(path, line, header string, aliases ...string) (err error) {
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
	// A write path, so a failing Close means the entry did not land — reporting
	// that beats silently leaving the file unignored.
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

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

// ensureWorktreePathIgnored keeps a worktree that lives inside the main
// checkout out of that checkout's git status. A worktree is a second full copy
// of the tree, so without an ignore rule every file in it turns up as untracked
// in the project that contains it — with the default worktree.dir that is the
// normal case, not an exotic one.
//
// What gets ignored is the worktree's parent directory (".chief/worktrees/" for
// the default template), so the next PRD needs no second entry. Two locations
// are deliberately left alone: one outside the checkout, which git never looks
// at anyway, and one directly in the repository root, where the parent is the
// project itself and an entry would ignore everything.
//
// Best-effort and idempotent: it writes only when git says the path is not
// ignored yet, and a .gitignore it cannot write still leaves the caller with a
// working worktree.
func ensureWorktreePathIgnored(repoDir, worktreePath string) {
	rel, ok := repoRelativePath(repoDir, worktreePath)
	if !ok {
		return
	}
	parent := path.Dir(rel)
	if parent == "." {
		return
	}
	if isPathIgnored(repoDir, rel) {
		return
	}
	// The bare form is the alias: someone who wrote ".chief/worktrees" by hand
	// meant the same thing and should not get a near-duplicate line.
	_ = ensureLineInFile(filepath.Join(repoDir, ".gitignore"), parent+"/", "# chief worktrees", parent)
}

// repoRelativePath expresses target relative to repoDir in slash notation, and
// reports false when target lies outside repoDir. Both sides go through
// normalizePath first: on macOS a temporary directory is reached through a
// symlink, so the two spellings would otherwise never line up.
func repoRelativePath(repoDir, target string) (string, bool) {
	rel, err := filepath.Rel(normalizePath(repoDir), normalizePath(target))
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
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
