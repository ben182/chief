package prd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What a run changes in a PRD — statuses, the blocked block, ticked boxes — does
// not make a prep stale; a change to what the PRD asks for does.
func TestCheckPrep_IgnoresWhatARunChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prd.md")
	original := "# App\n\n### US-001: Sign\n- [ ] Signed\n\n### US-002: Page\n- [ ] Shown\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	if fresh, reason := CheckPrep(path, "darwin"); fresh || reason != "it was never prepared" {
		t.Fatalf("before any prep: fresh=%v reason=%q", fresh, reason)
	}
	if err := RecordPrep(path, "darwin", time.Now()); err != nil {
		t.Fatal(err)
	}

	for _, change := range []func() error{
		func() error { return SetStoryStatus(path, "US-002", "in-progress") },
		func() error { return SetStoryStatus(path, "US-002", "done") },
		func() error { return SetStoryBlocked(path, "US-001", "1Password is locked") },
	} {
		if err := change(); err != nil {
			t.Fatal(err)
		}
		if fresh, reason := CheckPrep(path, "darwin"); !fresh {
			data, _ := os.ReadFile(path)
			t.Fatalf("a status change made the prep stale (%s):\n%s", reason, data)
		}
	}

	if fresh, reason := CheckPrep(path, PrepTargetBox); fresh || reason != "it was prepared for another environment" {
		t.Errorf("box: fresh=%v reason=%q", fresh, reason)
	}

	data, _ := os.ReadFile(path)
	edited := strings.Replace(string(data), "- [x] Shown", "- [x] Shown in the sidebar", 1)
	if err := os.WriteFile(path, []byte(edited), 0644); err != nil {
		t.Fatal(err)
	}
	if fresh, reason := CheckPrep(path, "darwin"); fresh || reason != "it changed since it was prepared" {
		t.Errorf("after editing a criterion: fresh=%v reason=%q", fresh, reason)
	}
}

// A prep for a second target keeps the first.
func TestRecordPrep_KeepsOtherTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prd.md")
	if err := os.WriteFile(path, []byte("# App\n\n### US-001: A\n- [ ] a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"darwin", PrepTargetBox} {
		if err := RecordPrep(path, target, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	for _, target := range []string{"darwin", PrepTargetBox} {
		if fresh, reason := CheckPrep(path, target); !fresh {
			t.Errorf("%s: %s", target, reason)
		}
	}
}
