package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBuildDiffStat(t *testing.T) {
	numstat := "12\t3\tinternal/api/user.go\n" +
		"40\t0\tinternal/api/user_test.go\n" +
		"5\t1\tREADME.md\n" +
		"0\t9\told/legacy.go\n" +
		"-\t-\tassets/logo.png\n"
	nameStatus := "M\tinternal/api/user.go\n" +
		"A\tinternal/api/user_test.go\n" +
		"M\tREADME.md\n" +
		"D\told/legacy.go\n" +
		"A\tassets/logo.png\n"

	d := buildDiffStat(numstat, nameStatus)

	if d.Insertions != 57 || d.Deletions != 13 {
		t.Errorf("lines = +%d −%d, want +57 −13", d.Insertions, d.Deletions)
	}
	if d.FilesAdded != 2 || d.FilesModified != 2 || d.FilesDeleted != 1 {
		t.Errorf("files = %d added, %d modified, %d deleted; want 2/2/1",
			d.FilesAdded, d.FilesModified, d.FilesDeleted)
	}
	if d.FilesChanged() != 5 {
		t.Errorf("FilesChanged() = %d, want 5", d.FilesChanged())
	}
	if d.TestInsertions != 40 || d.TestFiles != 1 {
		t.Errorf("tests = %d lines in %d files, want 40 in 1", d.TestInsertions, d.TestFiles)
	}

	// Go leads (12 + 40), Markdown trails. The binary file contributes to neither,
	// and the deletion-only Go file adds no insertions and no file count.
	want := []LanguageStat{
		{Name: "Go", Insertions: 52, Files: 2},
		{Name: "Markdown", Insertions: 5, Files: 1},
	}
	if len(d.Languages) != len(want) {
		t.Fatalf("Languages = %+v, want %+v", d.Languages, want)
	}
	for i, w := range want {
		if d.Languages[i] != w {
			t.Errorf("Languages[%d] = %+v, want %+v", i, d.Languages[i], w)
		}
	}
}

func TestBuildDiffStatRenameCountsAsModified(t *testing.T) {
	// A rename is a change to an existing file, not a new one — counting it as
	// added would inflate "new files" on every refactor that moves code around.
	d := buildDiffStat("3\t3\tinternal/b.go\n", "R096\tinternal/a.go\tinternal/b.go\n")
	if d.FilesAdded != 0 || d.FilesModified != 1 {
		t.Errorf("rename counted as %d added / %d modified, want 0/1", d.FilesAdded, d.FilesModified)
	}
}

func TestBuildDiffStatEmpty(t *testing.T) {
	d := buildDiffStat("", "")
	if !d.IsZero() {
		t.Errorf("buildDiffStat(\"\", \"\") = %+v, want zero", d)
	}
	if len(d.Languages) != 0 {
		t.Errorf("Languages = %+v, want empty", d.Languages)
	}
}

func TestDiffStatTestShare(t *testing.T) {
	d := DiffStat{Insertions: 200, TestInsertions: 50}
	if got := d.TestShare(); got != 0.25 {
		t.Errorf("TestShare() = %v, want 0.25", got)
	}
	// A run that wrote nothing must not divide by zero.
	if got := (DiffStat{}).TestShare(); got != 0 {
		t.Errorf("TestShare() on zero stat = %v, want 0", got)
	}
}

func TestDiffStatTopLanguages(t *testing.T) {
	d := DiffStat{Languages: []LanguageStat{{Name: "Go"}, {Name: "YAML"}}}
	if got := d.TopLanguages(5); len(got) != 2 {
		t.Errorf("TopLanguages(5) returned %d, want 2 (all of them)", len(got))
	}
	if got := d.TopLanguages(1); len(got) != 1 || got[0].Name != "Go" {
		t.Errorf("TopLanguages(1) = %+v, want [Go]", got)
	}
}

func TestIsTestPath(t *testing.T) {
	tests := map[string]bool{
		"internal/api/user_test.go":        true,
		"src/components/Button.test.tsx":   true,
		"src/components/Button.spec.ts":    true,
		"tests/Feature/LoginTest.php":      true,
		"spec/models/user_spec.rb":         true,
		"__tests__/helpers.js":             true,
		"app/Services/UserServiceTest.php": true,
		"tests/test_parser.py":             true,
		"e2e/checkout.ts":                  true,
		"internal/api/user.go":             false,
		"src/components/Button.tsx":        false,
		"README.md":                        false,
		// "latest" contains "test" but is not a test directory, and the segment
		// match rather than a substring match is what keeps it out.
		"docs/latest/guide.md":             false,
		"app/Http/Controllers/Contest.php": false,
	}
	for file, want := range tests {
		if got := isTestPath(file); got != want {
			t.Errorf("isTestPath(%q) = %v, want %v", file, got, want)
		}
	}
}

func TestLanguageOf(t *testing.T) {
	tests := map[string]string{
		"internal/api/user.go":           "Go",
		"src/App.tsx":                    "TypeScript",
		"config/app.yml":                 "YAML",
		"resources/views/home.blade.php": "Blade",
		"Makefile":                       "Makefile",
		"Dockerfile":                     "Dockerfile",
		"pyproject.toml":                 "TOML",
		"main.py":                        "Python",
	}
	for file, want := range tests {
		if got := languageOf(file); got != want {
			t.Errorf("languageOf(%q) = %q, want %q", file, got, want)
		}
	}
}

func TestDiffStatSince(t *testing.T) {
	// commit writes files and commits them, returning the resulting HEAD hash.
	commit := func(t *testing.T, dir string, files map[string]string) string {
		t.Helper()
		for name, body := range files {
			full := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
				t.Fatalf("mkdir for %s: %v", name, err)
			}
			if err := os.WriteFile(full, []byte(body), 0644); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}
		for _, args := range [][]string{{"add", "-A"}, {"commit", "-m", "change"}} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v failed: %s", args, string(out))
			}
		}
		head, err := HeadHash(dir)
		if err != nil {
			t.Fatalf("HeadHash: %v", err)
		}
		return head
	}

	t.Run("counts only the commits after the start ref", func(t *testing.T) {
		dir := initTestRepo(t)
		// Work that predates the run must not be counted.
		startRef := commit(t, dir, map[string]string{"before.go": "package a\n\nvar x = 1\n"})

		commit(t, dir, map[string]string{
			"api/user.go":      "package api\n\nfunc New() {}\n",
			"api/user_test.go": "package api\n\nfunc TestNew(t *testing.T) {}\n",
		})

		stat, err := DiffStatSince(dir, startRef)
		if err != nil {
			t.Fatalf("DiffStatSince() error = %v", err)
		}
		if stat.Insertions != 6 || stat.Deletions != 0 {
			t.Errorf("lines = +%d −%d, want +6 −0", stat.Insertions, stat.Deletions)
		}
		if stat.FilesAdded != 2 || stat.FilesChanged() != 2 {
			t.Errorf("files = %d added of %d, want 2 of 2", stat.FilesAdded, stat.FilesChanged())
		}
		if stat.TestFiles != 1 || stat.TestInsertions != 3 {
			t.Errorf("tests = %d lines in %d files, want 3 in 1", stat.TestInsertions, stat.TestFiles)
		}
		if len(stat.Languages) != 1 || stat.Languages[0].Name != "Go" {
			t.Errorf("Languages = %+v, want [Go]", stat.Languages)
		}
	})

	t.Run("counts deletions and deleted files", func(t *testing.T) {
		dir := initTestRepo(t)
		startRef := commit(t, dir, map[string]string{"a.go": "1\n2\n3\n4\n"})
		if err := os.Remove(filepath.Join(dir, "a.go")); err != nil {
			t.Fatalf("remove: %v", err)
		}
		commit(t, dir, nil)

		stat, err := DiffStatSince(dir, startRef)
		if err != nil {
			t.Fatalf("DiffStatSince() error = %v", err)
		}
		if stat.Deletions != 4 || stat.FilesDeleted != 1 {
			t.Errorf("got %d deletions in %d deleted files, want 4 in 1", stat.Deletions, stat.FilesDeleted)
		}
		// Nothing was written, so no language should claim the run.
		if len(stat.Languages) != 0 {
			t.Errorf("Languages = %+v, want empty", stat.Languages)
		}
	})

	t.Run("no commits since the start ref", func(t *testing.T) {
		dir := initTestRepo(t)
		startRef := commit(t, dir, map[string]string{"a.go": "1\n"})

		stat, err := DiffStatSince(dir, startRef)
		if err != nil {
			t.Fatalf("DiffStatSince() error = %v", err)
		}
		if !stat.IsZero() {
			t.Errorf("DiffStatSince() = %+v, want zero", stat)
		}
	})

	t.Run("empty start ref is not an error", func(t *testing.T) {
		dir := initTestRepo(t)
		stat, err := DiffStatSince(dir, "")
		if err != nil {
			t.Fatalf("DiffStatSince() error = %v", err)
		}
		if !stat.IsZero() {
			t.Errorf("DiffStatSince() = %+v, want zero", stat)
		}
	})

	t.Run("unknown start ref reports an error", func(t *testing.T) {
		dir := initTestRepo(t)
		if _, err := DiffStatSince(dir, "definitely-not-a-ref"); err == nil {
			t.Error("DiffStatSince() with an unknown ref should error")
		}
	})
}

// TestIsTestPathNearMisses pins the names that look like tests to a substring
// matcher but are not, since each one was a real false positive at some point.
func TestIsTestPathNearMisses(t *testing.T) {
	for _, file := range []string{
		"app/Http/Controllers/Contest.php",
		"content/latest_news.md",
		"src/manifest.json",
		"docs/latest/guide.md",
		"internal/protest.go",
	} {
		if isTestPath(file) {
			t.Errorf("isTestPath(%q) = true, want false", file)
		}
	}
}
