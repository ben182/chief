package git

import (
	"path"
	"sort"
	"strconv"
	"strings"
)

// DiffStat describes what a range of commits did to the code: how many lines it
// added and removed, how the files it touched split between new, changed and
// deleted, which languages those lines were in, and how much of it was tests.
//
// It answers the question the completion screen is asked first — "what did two
// hours of agent time actually produce?" — which duration cannot. On its own the
// line count is a weak measure (a run that scaffolds a hundred files scores high
// without having thought hard), which is why the breakdown matters more than the
// total: a run that wrote four thousand lines of YAML and no tests and a run
// that wrote four thousand lines of Go and tests both report "+4,000" and are
// not the same afternoon.
type DiffStat struct {
	Insertions int
	Deletions  int

	FilesAdded    int
	FilesModified int
	FilesDeleted  int

	// Languages holds the insertions per language, most-written first. Only
	// languages with insertions appear: a run that merely deleted a language's
	// files says nothing about having written it.
	Languages []LanguageStat

	// TestInsertions and TestFiles count the part of the work that went into
	// tests, recognised from file paths. Tests are the one part of an unattended
	// run that is worth seeing separately — an agent that wrote nine hundred
	// lines and no test did something different from one that wrote nine hundred
	// lines and forty tests, and nobody was watching which.
	TestInsertions int
	TestFiles      int
}

// LanguageStat is the share one language had in a run's written lines.
type LanguageStat struct {
	Name       string
	Insertions int
	Files      int
}

// FilesChanged is the total number of files the range touched.
func (d DiffStat) FilesChanged() int {
	return d.FilesAdded + d.FilesModified + d.FilesDeleted
}

// IsZero reports whether the range changed nothing at all. A run can legitimately
// end here — every story parked, or an agent that committed nothing — and the
// caller then has no numbers worth showing.
func (d DiffStat) IsZero() bool {
	return d.Insertions == 0 && d.Deletions == 0 && d.FilesChanged() == 0
}

// TestShare returns the fraction of written lines that went into test files,
// 0 when nothing was written.
func (d DiffStat) TestShare() float64 {
	if d.Insertions == 0 {
		return 0
	}
	return float64(d.TestInsertions) / float64(d.Insertions)
}

// TopLanguages returns at most n languages, most-written first.
func (d DiffStat) TopLanguages(n int) []LanguageStat {
	if n > len(d.Languages) {
		n = len(d.Languages)
	}
	return d.Languages[:n]
}

// DiffStatSince returns the code stats for the commits a run added, given the
// branch HEAD captured before it started. It is the same scoping the run summary
// and the consolidation pass use (sinceRef..HEAD), so all three describe exactly
// this run's work and not an earlier one's.
//
// dir must be the directory the run committed in — the worktree for a worktree
// run, the project root otherwise — because HEAD is read from it.
//
// An empty sinceRef returns a zero stat rather than an error: a run whose start
// could not be captured (a repository with no commits yet) has nothing to
// compare against, and that is not a failure worth reporting to the user.
func DiffStatSince(dir, sinceRef string) (DiffStat, error) {
	if strings.TrimSpace(sinceRef) == "" {
		return DiffStat{}, nil
	}
	rang := sinceRef + "..HEAD"

	// --numstat carries the line counts per file, --name-status whether each file
	// was added, modified or deleted. Git offers no single format with both, so
	// the two are read together and joined on the path.
	nums, err := runGit(dir, "diff", "--numstat", rang)
	if err != nil {
		return DiffStat{}, err
	}
	names, err := runGit(dir, "diff", "--name-status", rang)
	if err != nil {
		return DiffStat{}, err
	}
	return buildDiffStat(nums, names), nil
}

// buildDiffStat assembles the stat from git's --numstat and --name-status output.
func buildDiffStat(numstat, nameStatus string) DiffStat {
	var d DiffStat

	byLanguage := map[string]*LanguageStat{}
	for _, line := range strings.Split(numstat, "\n") {
		file, ins, del, ok := parseNumstatLine(line)
		if !ok {
			continue
		}
		d.Insertions += ins
		d.Deletions += del

		if isTestPath(file) {
			d.TestInsertions += ins
			d.TestFiles++
		}
		// A file that only lost lines says nothing about what was written in it.
		if ins == 0 {
			continue
		}
		name := languageOf(file)
		if stat, found := byLanguage[name]; found {
			stat.Insertions += ins
			stat.Files++
		} else {
			byLanguage[name] = &LanguageStat{Name: name, Insertions: ins, Files: 1}
		}
	}

	for _, line := range strings.Split(nameStatus, "\n") {
		switch statusOf(line) {
		case 'A':
			d.FilesAdded++
		case 'D':
			d.FilesDeleted++
		case 0:
			// Not a status line.
		default:
			// M, and the renames and copies (R100, C75) that are changes to an
			// existing file by another name.
			d.FilesModified++
		}
	}

	d.Languages = make([]LanguageStat, 0, len(byLanguage))
	for _, stat := range byLanguage {
		d.Languages = append(d.Languages, *stat)
	}
	// Most-written first; ties by name so the order is stable across runs rather
	// than following Go's map iteration.
	sort.Slice(d.Languages, func(i, j int) bool {
		if d.Languages[i].Insertions != d.Languages[j].Insertions {
			return d.Languages[i].Insertions > d.Languages[j].Insertions
		}
		return d.Languages[i].Name < d.Languages[j].Name
	})
	return d
}

// parseNumstatLine reads one "<insertions>\t<deletions>\t<path>" line. Binary
// files report "-" for both counts and are skipped: an image has no lines, and
// counting it as zero would still let it into the language breakdown.
//
// A renamed path arrives as "old => new" or with a braced common prefix; the
// path is taken as written, which only ever affects which language bucket a
// rename lands in.
func parseNumstatLine(line string) (file string, insertions, deletions int, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(line), "\t", 3)
	if len(parts) != 3 {
		return "", 0, 0, false
	}
	ins, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", 0, 0, false // binary file ("-")
	}
	del, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, 0, false
	}
	return parts[2], ins, del, true
}

// statusOf returns the status letter of a --name-status line, or 0 when the line
// carries none.
func statusOf(line string) byte {
	line = strings.TrimSpace(line)
	if line == "" {
		return 0
	}
	status := line[0]
	if status < 'A' || status > 'Z' {
		return 0
	}
	return status
}

// testPathMarkers are directory names that hold tests in most ecosystems.
var testPathMarkers = []string{"test", "tests", "spec", "specs", "__tests__", "e2e", "cypress"}

// testFileMarkers mark a file name as a test wherever they appear in it. Each
// one carries its own delimiters — Go's user_test.go, JS/TS's Button.test.tsx
// and .spec.ts, Ruby's user_spec.rb — so a plain substring match cannot be
// fooled by a word that merely contains "test".
var testFileMarkers = []string{"_test.", ".test.", "_spec.", ".spec."}

// camelTestExtensions are the languages that name a test class after the thing
// it tests and the file after the class: UserServiceTest.php. The capital T is
// the whole signal, which is why this check alone runs on the original spelling.
var camelTestExtensions = []string{".php", ".java", ".kt", ".cs"}

// isTestPath reports whether a repository path looks like a test, by directory
// or by file name. It is a heuristic over naming conventions, so it aims to be
// generous but not credulous: the number it feeds is "roughly how much of this
// went into tests", where a missed file costs less than a wrong one, but
// "contest.php" and "latest_news.md" must not count as tests.
func isTestPath(file string) bool {
	lower := strings.ToLower(file)
	dir, base := path.Split(lower)
	for _, segment := range strings.Split(dir, "/") {
		for _, marker := range testPathMarkers {
			if segment == marker {
				return true
			}
		}
	}
	for _, marker := range testFileMarkers {
		if strings.Contains(base, marker) {
			return true
		}
	}
	// Python puts the marker in front: test_parser.py. Only as a prefix, or
	// "latest_news.md" would qualify.
	if strings.HasPrefix(base, "test_") {
		return true
	}
	// The CamelCase convention, checked on the original spelling so that
	// "UserServiceTest.php" counts and "Contest.php" does not.
	originalBase := path.Base(file)
	for _, ext := range camelTestExtensions {
		if strings.HasSuffix(originalBase, "Test"+ext) {
			return true
		}
	}
	return false
}

// languageNames maps a file extension to the name a developer would use for it.
// Extensions missing here fall back to the extension itself in upper case, which
// reads fine for the long tail (".toml" -> "TOML") and keeps the table to the
// cases where the name and the extension genuinely differ.
var languageNames = map[string]string{
	".go":     "Go",
	".ts":     "TypeScript",
	".tsx":    "TypeScript",
	".js":     "JavaScript",
	".jsx":    "JavaScript",
	".mjs":    "JavaScript",
	".py":     "Python",
	".rb":     "Ruby",
	".php":    "PHP",
	".rs":     "Rust",
	".java":   "Java",
	".kt":     "Kotlin",
	".swift":  "Swift",
	".cs":     "C#",
	".c":      "C",
	".h":      "C",
	".cpp":    "C++",
	".cc":     "C++",
	".hpp":    "C++",
	".md":     "Markdown",
	".mdx":    "Markdown",
	".yml":    "YAML",
	".yaml":   "YAML",
	".sh":     "Shell",
	".bash":   "Shell",
	".zsh":    "Shell",
	".vue":    "Vue",
	".svelte": "Svelte",
	".blade":  "Blade",
	".tf":     "Terraform",
}

// languageOf names the language of a repository path. Files with no extension
// are named after themselves (Makefile, Dockerfile), which is how they are
// referred to anyway.
func languageOf(file string) string {
	base := path.Base(file)
	// Laravel's .blade.php and similar double extensions read better by their
	// first half than as PHP.
	if strings.HasSuffix(strings.ToLower(base), ".blade.php") {
		return "Blade"
	}
	ext := strings.ToLower(path.Ext(base))
	if ext == "" {
		return base
	}
	if name, found := languageNames[ext]; found {
		return name
	}
	return strings.ToUpper(strings.TrimPrefix(ext, "."))
}
