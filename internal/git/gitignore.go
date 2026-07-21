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

// AddChiefToGitignore adds .chief to the local .gitignore file.
// Creates the file if it doesn't exist.
func AddChiefToGitignore(dir string) error {
	gitignorePath := filepath.Join(dir, ".gitignore")

	// Check if .gitignore exists and if .chief is already in it
	if _, err := os.Stat(gitignorePath); err == nil {
		// File exists, check if .chief is already there
		content, err := os.ReadFile(gitignorePath)
		if err != nil {
			return fmt.Errorf("failed to read .gitignore: %w", err)
		}

		// Check each line for .chief entry
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == ".chief" || trimmed == ".chief/" {
				// Already present
				return nil
			}
		}

		// Append to existing file
		f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to open .gitignore: %w", err)
		}
		defer f.Close()

		// Add newline before if file doesn't end with one
		if len(content) > 0 && content[len(content)-1] != '\n' {
			if _, err := f.WriteString("\n"); err != nil {
				return fmt.Errorf("failed to write to .gitignore: %w", err)
			}
		}

		if _, err := f.WriteString(".chief/\n"); err != nil {
			return fmt.Errorf("failed to write to .gitignore: %w", err)
		}
	} else if os.IsNotExist(err) {
		// Create new .gitignore file
		if err := os.WriteFile(gitignorePath, []byte(".chief/\n"), 0644); err != nil {
			return fmt.Errorf("failed to create .gitignore: %w", err)
		}
	} else {
		return fmt.Errorf("failed to check .gitignore: %w", err)
	}

	return nil
}

// IgnoreLogsIn ensures dir's .gitignore carries the `*.log` pattern so chief's
// per-run log files (claude-<timestamp>.log) stay out of version control. It is
// scoped to the PRD directory the logs live in, so it works regardless of
// whether the project tracks or ignores `.chief/` as a whole and needs no
// pattern in the user's root .gitignore (the historical `claude.log` pattern
// never matched the timestamped names). Best-effort and idempotent: it writes
// only when the pattern is missing, and returns silently on any I/O error.
func IgnoreLogsIn(dir string) {
	const pattern = "*.log"
	path := filepath.Join(dir, ".gitignore")
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			_ = os.WriteFile(path, []byte("# chief run logs — regenerated each run\n"+pattern+"\n"), 0o644)
		}
		return
	}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == pattern {
			return // already ignored
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(content) > 0 && content[len(content)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			return
		}
	}
	_, _ = f.WriteString(pattern + "\n")
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
