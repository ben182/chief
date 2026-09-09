package git

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultWorktreeDir is the worktree.dir template chief uses when none is
// configured: every PRD gets its own directory below the main checkout, which
// keeps worktrees inside the project and out of the way.
const DefaultWorktreeDir = ".chief/worktrees/{prd}"

// WorktreePathForPRD resolves the worktree.dir template into the absolute path
// of a PRD's worktree. An empty template means DefaultWorktreeDir.
//
// The template understands {prd} (the PRD name), {repo} (the basename of the
// main checkout) and {branch} (the worktree's branch with "/" replaced by "-",
// because a branch name is a path of its own). A relative template is resolved
// against the main checkout.
//
// A template that resolves to the main checkout itself or to its parent
// directory is rejected: git would refuse the first and the second would turn
// the directory holding the project into a worktree.
func WorktreePathForPRD(baseDir, template, prdName, branch string) (string, error) {
	path, err := expandWorktreeDir(baseDir, template, prdName, branch)
	if err != nil {
		return "", err
	}

	repoAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve repository path: %w", err)
	}
	switch normalizePath(path) {
	case normalizePath(repoAbs):
		return "", fmt.Errorf("worktree.dir %q resolves to the main checkout itself (%s); give each worktree a directory of its own, for example %q",
			worktreeTemplate(template), path, DefaultWorktreeDir)
	case normalizePath(filepath.Dir(repoAbs)):
		return "", fmt.Errorf("worktree.dir %q resolves to the parent of the main checkout (%s); add a subdirectory, for example %q",
			worktreeTemplate(template), path, "../{repo}-worktrees/{prd}")
	}
	return path, nil
}

// worktreeTemplate returns the template actually in force: the configured one,
// or the default when nothing is configured.
func worktreeTemplate(configured string) string {
	if t := strings.TrimSpace(configured); t != "" {
		return t
	}
	return DefaultWorktreeDir
}

// expandWorktreeDir substitutes the placeholders and makes the result absolute,
// without judging where it ends up.
func expandWorktreeDir(baseDir, template, prdName, branch string) (string, error) {
	repoAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve repository path: %w", err)
	}
	expanded := strings.NewReplacer(
		"{prd}", prdName,
		"{repo}", filepath.Base(repoAbs),
		"{branch}", strings.ReplaceAll(branch, "/", "-"),
	).Replace(worktreeTemplate(template))
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(repoAbs, expanded)
	}
	return filepath.Clean(expanded), nil
}

// normalizePath resolves symlinks in the longest existing prefix of path, so
// two spellings of the same location compare equal. It matters because git
// reports worktree paths with symlinks resolved while chief builds them from
// the template — on macOS /var is a symlink to /private/var, so the same
// worktree would otherwise look like two.
func normalizePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	rest := ""
	for cur := abs; ; {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// prdNameFromWorktree answers which PRD a registered worktree belongs to by
// matching its path against the template with {prd} left open. The branch comes
// from git rather than from the template, so a {branch} placeholder is a known
// value here and only {prd} has to be recovered.
func prdNameFromWorktree(baseDir, template string, wt Worktree) (string, bool) {
	// A NUL byte cannot occur in a path, so nothing in the expanded template
	// can be mistaken for the placeholder.
	const sentinel = "\x00prd\x00"

	// Expand against the resolved repository path so the pattern is in the same
	// spelling as the paths git reports.
	repoNorm := normalizePath(baseDir)
	pattern, err := expandWorktreeDir(repoNorm, template, sentinel, wt.Branch)
	if err != nil {
		return "", false
	}
	parts := strings.Split(pattern, sentinel)
	if len(parts) < 2 {
		// A template without {prd} cannot say which PRD a directory belongs to.
		return "", false
	}

	var b strings.Builder
	b.WriteString("^")
	for i, part := range parts {
		if i > 0 {
			b.WriteString("([^" + regexp.QuoteMeta(string(filepath.Separator)) + "]+)")
		}
		b.WriteString(regexp.QuoteMeta(part))
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return "", false
	}

	candidate := normalizePath(wt.Path)
	match := re.FindStringSubmatch(candidate)
	if match == nil {
		return "", false
	}

	// With {prd} used more than once each occurrence matched independently;
	// expanding the name back out proves they agree.
	expanded, err := expandWorktreeDir(repoNorm, template, match[1], wt.Branch)
	if err != nil || expanded != candidate {
		return "", false
	}
	return match[1], true
}
