package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// commitFile writes a file and commits it on the current branch.
func commitFile(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	for _, args := range [][]string{{"add", name}, {"commit", "-m", msg}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, string(out))
		}
	}
}

func TestCommitLogForStories(t *testing.T) {
	dir := initTestRepo(t)
	if err := CreateBranch(dir, "chief/feature"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	commitFile(t, dir, "a.txt", "a", "feat: US-001 - add a")
	commitFile(t, dir, "b.txt", "b", "feat: US-002 - add b")

	stories := []StoryRef{
		{ID: "US-001", Title: "add a"},
		{ID: "US-002", Title: "add b"},
	}
	log, err := CommitLogForStories(dir, stories, "")
	if err != nil {
		t.Fatalf("CommitLogForStories: %v", err)
	}

	lines := strings.Split(log, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 commits, got %d: %q", len(lines), log)
	}
	if !strings.Contains(lines[0], "feat: US-001 - add a") {
		t.Errorf("first line = %q, want US-001 first", lines[0])
	}
	if !strings.Contains(lines[1], "feat: US-002 - add b") {
		t.Errorf("second line = %q, want US-002 second", lines[1])
	}

	// Order follows the passed slice, not commit date: passing the newer commit
	// (US-002) first must put it first, even though it is the more recent commit.
	rev, err := CommitLogForStories(dir, []StoryRef{{ID: "US-002", Title: "add b"}, {ID: "US-001", Title: "add a"}}, "")
	if err != nil {
		t.Fatalf("CommitLogForStories (reversed): %v", err)
	}
	if first := strings.Split(rev, "\n")[0]; !strings.Contains(first, "feat: US-002 - add b") {
		t.Errorf("reversed first line = %q, want US-002 first (order must follow the slice)", first)
	}
}

// TestCommitLogForStories_ExcludesUnrelatedWork is the crux: a branch may carry
// commits from other PRDs (even reusing the same story IDs) plus unrelated
// churn. Only commits whose "feat: <ID> - <Title>" matches a story of *this*
// PRD may appear — a same-numbered story with a different title must not leak in.
func TestCommitLogForStories_ExcludesUnrelatedWork(t *testing.T) {
	dir := initTestRepo(t)
	if err := CreateBranch(dir, "chief/feature"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	// Prior work from another PRD that reuses US-001, plus noise.
	commitFile(t, dir, "other.txt", "x", "feat: US-001 - some other feature")
	commitFile(t, dir, "noise.txt", "y", "chore: unrelated cleanup")
	// This PRD's actual work.
	commitFile(t, dir, "mine.txt", "z", "feat: US-001 - my real feature")

	log, err := CommitLogForStories(dir, []StoryRef{{ID: "US-001", Title: "my real feature"}}, "")
	if err != nil {
		t.Fatalf("CommitLogForStories: %v", err)
	}

	if !strings.Contains(log, "my real feature") {
		t.Errorf("log missing this PRD's commit: %q", log)
	}
	if strings.Contains(log, "some other feature") || strings.Contains(log, "unrelated cleanup") {
		t.Errorf("log leaked unrelated commits: %q", log)
	}
	if lines := strings.Split(log, "\n"); len(lines) != 1 {
		t.Errorf("expected exactly 1 commit, got %d: %q", len(lines), log)
	}
}

func TestCommitLogForStories_EmptyWhenNoMatch(t *testing.T) {
	dir := initTestRepo(t)
	if err := CreateBranch(dir, "chief/empty"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	log, err := CommitLogForStories(dir, []StoryRef{{ID: "US-001", Title: "never committed"}}, "")
	if err != nil {
		t.Fatalf("CommitLogForStories: %v", err)
	}
	if log != "" {
		t.Errorf("expected empty log when no story commit matches, got %q", log)
	}
}

// TestCommitLogForStories_ScopesToSinceRef is the followup case: a branch already
// carries a story an earlier run committed. Passing the HEAD captured at the start
// of this run as sinceRef must exclude that already-landed story, so the summary
// describes only what this run added.
func TestCommitLogForStories_ScopesToSinceRef(t *testing.T) {
	dir := initTestRepo(t)
	if err := CreateBranch(dir, "chief/feature"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	// Previous run's work.
	commitFile(t, dir, "a.txt", "a", "feat: US-001 - add a")
	// The followup run starts here; capture the tip it will build on top of.
	sinceRef, err := HeadHash(dir)
	if err != nil {
		t.Fatalf("HeadHash: %v", err)
	}
	// This run's work.
	commitFile(t, dir, "b.txt", "b", "feat: US-002 - add b")

	stories := []StoryRef{
		{ID: "US-001", Title: "add a"},
		{ID: "US-002", Title: "add b"},
	}
	log, err := CommitLogForStories(dir, stories, sinceRef)
	if err != nil {
		t.Fatalf("CommitLogForStories: %v", err)
	}
	if strings.Contains(log, "add a") {
		t.Errorf("log leaked the previous run's story (US-001): %q", log)
	}
	if !strings.Contains(log, "add b") {
		t.Errorf("log missing this run's story (US-002): %q", log)
	}
	if lines := strings.Split(log, "\n"); len(lines) != 1 {
		t.Errorf("expected exactly 1 commit for this run, got %d: %q", len(lines), log)
	}
}

// TestFindCommitForStory_NamespaceDisambiguates is the case the old title-only
// matcher could not solve: two PRDs on the same branch each committed a story
// with the *same* ID AND the *same* title. Only the PRD namespace in the commit
// subject ("feat: <prdName>/<ID> - …") tells them apart.
func TestFindCommitForStory_NamespaceDisambiguates(t *testing.T) {
	dir := initTestRepo(t)
	if err := CreateBranch(dir, "chief/billing"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	// Another PRD's story, then this PRD's story: identical ID and title.
	commitFile(t, dir, "auth.txt", "a", "feat: auth/US-001 - Login form")
	commitFile(t, dir, "billing.txt", "b", "feat: billing/US-001 - Login form")
	want, err := HeadHash(dir) // the billing commit is HEAD
	if err != nil {
		t.Fatalf("HeadHash: %v", err)
	}

	got, err := FindCommitForStory(dir, "billing", "US-001", "Login form", "")
	if err != nil {
		t.Fatalf("FindCommitForStory: %v", err)
	}
	if got != want {
		t.Errorf("matched %q, want the billing commit %q (namespace must disambiguate)", got, want)
	}

	// The auth PRD's same-numbered, same-titled story must resolve to the *other*
	// commit, never the billing one.
	authHash, err := FindCommitForStory(dir, "auth", "US-001", "Login form", "")
	if err != nil {
		t.Fatalf("FindCommitForStory (auth): %v", err)
	}
	if authHash == "" || authHash == want {
		t.Errorf("auth story matched %q, want the auth commit (distinct from billing %q)", authHash, want)
	}
}

// TestFindCommitForStory_LegacyFallback covers commits authored before the PRD
// namespace existed ("feat: <ID> - <title>"): with no namespaced match, the
// lookup falls back to the exact ID+title subject so old work is still found.
func TestFindCommitForStory_LegacyFallback(t *testing.T) {
	dir := initTestRepo(t)
	if err := CreateBranch(dir, "chief/legacy"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	commitFile(t, dir, "old.txt", "x", "feat: US-042 - Old style commit")
	want, err := HeadHash(dir)
	if err != nil {
		t.Fatalf("HeadHash: %v", err)
	}

	got, err := FindCommitForStory(dir, "someprd", "US-042", "Old style commit", "")
	if err != nil {
		t.Fatalf("FindCommitForStory: %v", err)
	}
	if got != want {
		t.Errorf("legacy fallback matched %q, want %q", got, want)
	}
}

func TestCommitPaths_ForceAddsGitignoredFile(t *testing.T) {
	dir := initTestRepo(t)
	// Ignore .chief, then write a summary underneath it.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".chief/\n"), 0644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	prdDir := filepath.Join(dir, ".chief", "prds", "default")
	if err := os.MkdirAll(prdDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	summaryPath := filepath.Join(prdDir, "summary.md")
	if err := os.WriteFile(summaryPath, []byte("# Summary\n"), 0644); err != nil {
		t.Fatalf("write summary: %v", err)
	}

	if err := CommitPaths(dir, "docs: add run summary", summaryPath); err != nil {
		t.Fatalf("CommitPaths: %v", err)
	}

	// The gitignored file must now be tracked.
	cmd := exec.Command("git", "ls-files", "--", summaryPath)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	if strings.TrimSpace(string(out)) == "" {
		t.Errorf("summary was not committed despite force-add; ls-files empty")
	}
}

// TestCommittablePaths verifies the filter that keeps commits from overriding
// the user's .gitignore: ignored-and-untracked paths are dropped, while normal
// paths and tracked files (which gitignore has no effect on) stay in.
func TestCommittablePaths(t *testing.T) {
	dir := initTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".chief/\n"), 0644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	prdDir := filepath.Join(dir, ".chief", "prds", "default")
	if err := os.MkdirAll(prdDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	plain := filepath.Join(dir, "notes.md")
	ignored := filepath.Join(prdDir, "prd.md")
	trackedIgnored := filepath.Join(prdDir, "progress.md")
	for _, p := range []string{plain, ignored, trackedIgnored} {
		if err := os.WriteFile(p, []byte("x\n"), 0644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	// progress.md was force-added in the past (the pre-filter behavior); once
	// tracked, gitignore no longer applies and the filter must keep it.
	if err := CommitPaths(dir, "chore: track progress", trackedIgnored); err != nil {
		t.Fatalf("CommitPaths: %v", err)
	}

	got := CommittablePaths(dir, plain, ignored, trackedIgnored)
	want := []string{plain, trackedIgnored}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("CommittablePaths = %v, want %v", got, want)
	}
}

// A story that was blocked halfway commits its finished part as wip:, and that
// commit is not the story being done — nor is a revert of the done commit, or a
// body that quotes the subject. Before, any message containing the done subject
// counted, so a wip: commit whose body said what it was for, or a revert, made
// the story done on the next attempt.
func TestFindCommitForStory_OnlyADoneSubjectCounts(t *testing.T) {
	dir := initTestRepo(t)
	if err := CreateBranch(dir, "chief/app"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	commitFile(t, dir, "a.txt", "a", "wip: app/US-001 - Sign the app")
	commitFile(t, dir, "b.txt", "b", "Revert \"feat: app/US-001 - Sign the app\"")
	commitFile(t, dir, "c.txt", "c", "chore: notes\n\nThe rest follows as feat: app/US-001 - Sign the app")
	got, err := FindCommitForStory(dir, "app", "US-001", "Sign the app", "")
	if err != nil {
		t.Fatalf("FindCommitForStory: %v", err)
	}
	if got != "" {
		t.Fatalf("matched %s (%s), want no done commit", got, commitSubject(t, dir, got))
	}

	commitFile(t, dir, "d.txt", "d", "feat: app/US-001 - Sign the app")
	want, _ := HeadHash(dir)
	commitFile(t, dir, "e.txt", "e", "wip: app/US-001 - Sign the app")
	if got, _ := FindCommitForStory(dir, "app", "US-001", "Sign the app", ""); got != want {
		t.Errorf("matched %q, want the feat: commit %q behind the newer wip:", got, want)
	}
}

func commitSubject(t *testing.T, dir, hash string) string {
	t.Helper()
	out, _ := runGit(dir, "log", "-1", "--format=%s", hash)
	return out
}

// A blocked story's leftovers go into a named stash, untracked files included,
// and what is excluded stays in the tree.
func TestStashUncommitted(t *testing.T) {
	dir := initTestRepo(t)
	if ok, err := StashUncommitted(dir, "chief: app/US-001 blocked", ".chief"); err != nil || ok {
		t.Fatalf("clean tree: stashed=%v err=%v, want nothing stashed", ok, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".chief", "prds", "app"), 0755); err != nil {
		t.Fatal(err)
	}
	// A project that keeps .chief/ out of git, and a run log that is ignored:
	// as pathspec exclusions, either made the stash fail halfway.
	if err := os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte(".chief/\n*.log\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		"half.go":                     "package half\n",
		"run.log":                     "log\n",
		".chief/prds/app/progress.md": "## entry\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	ok, err := StashUncommitted(dir, "chief: app/US-001 blocked", ".chief", filepath.Join(dir, "run.log"))
	if err != nil || !ok {
		t.Fatalf("stashed=%v err=%v, want the leftovers stashed", ok, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "half.go")); !os.IsNotExist(err) {
		t.Error("half.go is still in the tree")
	}
	if _, err := os.Stat(filepath.Join(dir, ".chief", "prds", "app", "progress.md")); err != nil {
		t.Errorf("chief's own file was stashed: %v", err)
	}
	if status, _ := runGit(dir, "status", "--porcelain"); status != "" {
		t.Errorf("the tree is not clean after the stash:\n%s", status)
	}
	if list, _ := runGit(dir, "stash", "list"); !strings.Contains(list, "chief: app/US-001 blocked") {
		t.Errorf("stash list = %q", list)
	}
	if files, _ := runGit(dir, "show", "--name-only", "--format=", "stash@{0}^3"); !strings.Contains(files, "half.go") {
		t.Errorf("the untracked file is not in the stash: %q", files)
	}
}

// `chief <name>` started in the project finds its PRD as a relative path, so
// the directory and the keeps arrive relative too. They must still be kept: a
// dropped keep stashed prd.md and progress.md with the leftovers, which reset
// every earlier story's status to its last commit and sent the loop round the
// same stories again.
func TestStashUncommitted_RelativePaths(t *testing.T) {
	dir := initTestRepo(t)
	prdDir := filepath.Join(dir, ".chief", "prds", "app")
	if err := os.MkdirAll(prdDir, 0755); err != nil {
		t.Fatal(err)
	}
	progress := filepath.Join(prdDir, "progress.md")
	if err := os.WriteFile(progress, []byte("## committed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runInDir(t, dir, "git", "add", ".chief")
	runInDir(t, dir, "git", "commit", "-q", "-m", "chore: track progress")
	for path, body := range map[string]string{
		"half.go":                     "package half\n",
		".chief/prds/app/progress.md": "## committed\n## blocked story\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)

	ok, err := StashUncommitted(".", "chief: app/US-002 blocked", ".chief", filepath.Join(".chief", "prds", "app"))
	if err != nil || !ok {
		t.Fatalf("stashed=%v err=%v, want the leftovers stashed", ok, err)
	}
	if body, _ := os.ReadFile(progress); string(body) != "## committed\n## blocked story\n" {
		t.Errorf("progress.md = %q, want the uncommitted entry kept", body)
	}
	if _, err := os.Stat(filepath.Join(dir, "half.go")); !os.IsNotExist(err) {
		t.Error("half.go is still in the tree")
	}
}
