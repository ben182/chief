package git

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ben182/chief/internal/prd"
)

// DefaultWorktreeDir is the worktree.dir template chief uses when none is
// configured: every PRD gets its own directory below the main checkout, which
// keeps worktrees inside the project and out of the way.
const DefaultWorktreeDir = ".chief/worktrees/{prd}"

// BranchForPRD names the branch chief gives a PRD's worktree. It lives here
// rather than in the TUI because the CLI resolves the same worktree: both have
// to arrive at one branch name, or a {branch} placeholder in the path template
// would point them at two different directories.
func BranchForPRD(prdName string) string {
	return "chief/" + prdName
}

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

// SamePath reports whether two paths name the same directory, however each is
// spelled — the question normalizePath exists to answer, asked from outside the
// package.
func SamePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return normalizePath(a) == normalizePath(b)
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

// LivePRDDir answers where a PRD's working files — prd.md, progress.md, the
// follow-up inbox — currently live. A worktree run mirrors them into its own
// checkout so the project's copy stays as the user left it, which means a caller
// outside the run (the follow-up command, the picker listing a PRD nobody has
// started this session) has to look there too, or it reads a prd.md that is
// missing everything the run has recorded.
//
// homeDir is the PRD's directory in the project and stays the answer whenever
// there is no worktree holding a copy: the template resolves nowhere, the
// directory is not a worktree, or it has no prd.md of its own. Because the
// answer is derived from what is on disk rather than remembered, a worktree
// removed behind chief's back quietly falls back to the project.
func LivePRDDir(baseDir, dirTemplate, prdName, homeDir string) string {
	worktreePath, err := WorktreePathForPRD(baseDir, dirTemplate, prdName, BranchForPRD(prdName))
	if err != nil || !IsWorktree(worktreePath) {
		return homeDir
	}
	mapped, ok := prd.PathIn(baseDir, homeDir, worktreePath)
	if !ok {
		return homeDir
	}
	if _, err := os.Stat(filepath.Join(mapped, "prd.md")); err != nil {
		return homeDir
	}
	return mapped
}
