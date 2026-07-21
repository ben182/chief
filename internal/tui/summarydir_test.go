package tui

import (
	"path/filepath"
	"testing"
)

// TestSummaryDir_DerivesFromRegisteredPath verifies that the run-summary
// directory is derived from the PRD's registered path and mapped into the given
// gitDir, so the legacy .chief/prd.md and direct-path layouts land beside the
// PRD instead of in a phantom .chief/prds/<name>.
func TestSummaryDir_DerivesFromRegisteredPath(t *testing.T) {
	base := "/proj"

	tests := []struct {
		name    string
		prdName string
		prdPath string // registered path for the active PRD
		gitDir  string
		want    string
	}{
		{
			name:    "standard PRD in project root",
			prdName: "foo",
			prdPath: filepath.Join(base, ".chief", "prds", "foo", "prd.md"),
			gitDir:  base,
			want:    filepath.Join(base, ".chief", "prds", "foo"),
		},
		{
			name:    "legacy main PRD lands beside .chief/prd.md",
			prdName: "main",
			prdPath: filepath.Join(base, ".chief", "prd.md"),
			gitDir:  base,
			want:    filepath.Join(base, ".chief"),
		},
		{
			name:    "worktree run maps the PRD dir into the worktree",
			prdName: "foo",
			prdPath: filepath.Join(base, ".chief", "prds", "foo", "prd.md"),
			gitDir:  filepath.Join(base, ".chief", "worktrees", "foo"),
			want:    filepath.Join(base, ".chief", "worktrees", "foo", ".chief", "prds", "foo"),
		},
		{
			name:    "unknown PRD falls back to convention",
			prdName: "other", // != active prdName, no manager -> path unknown
			prdPath: filepath.Join(base, ".chief", "prds", "foo", "prd.md"),
			gitDir:  base,
			want:    filepath.Join(base, ".chief", "prds", "other"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{
				baseDir: base,
				prdName: "foo",
				prdPath: tt.prdPath,
			}
			// The "legacy main" case models main being the active PRD.
			if tt.prdName == "main" {
				a.prdName = "main"
			}
			if got := a.summaryDir(tt.prdName, tt.gitDir); got != tt.want {
				t.Errorf("summaryDir(%q, %q) = %q, want %q", tt.prdName, tt.gitDir, got, tt.want)
			}
		})
	}
}
