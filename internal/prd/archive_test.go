package prd

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTestPRD creates .chief/prds/<name>/prd.md under baseDir.
func writeTestPRD(t *testing.T, baseDir, name string) string {
	t.Helper()
	dir := filepath.Join(PrdsDir(baseDir), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "prd.md")
	if err := os.WriteFile(path, []byte("# "+name+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

func TestArchiveAndRestorePRD(t *testing.T) {
	base := t.TempDir()
	writeTestPRD(t, base, "old-feature")

	if err := ArchivePRD(base, "old-feature"); err != nil {
		t.Fatalf("ArchivePRD: %v", err)
	}

	// Gone from prds/
	if _, err := os.Stat(filepath.Join(PrdsDir(base), "old-feature")); !os.IsNotExist(err) {
		t.Fatalf("expected PRD removed from prds/, got err=%v", err)
	}
	// Present in archive/
	if _, err := os.Stat(filepath.Join(ArchiveDir(base), "old-feature", "prd.md")); err != nil {
		t.Fatalf("expected PRD in archive/, got err=%v", err)
	}

	archived, err := ListArchived(base)
	if err != nil {
		t.Fatalf("ListArchived: %v", err)
	}
	if len(archived) != 1 || archived[0] != "old-feature" {
		t.Fatalf("ListArchived = %v, want [old-feature]", archived)
	}

	if err := RestorePRD(base, "old-feature"); err != nil {
		t.Fatalf("RestorePRD: %v", err)
	}
	if _, err := os.Stat(filepath.Join(PrdsDir(base), "old-feature", "prd.md")); err != nil {
		t.Fatalf("expected PRD restored to prds/, got err=%v", err)
	}
	if archived, _ := ListArchived(base); len(archived) != 0 {
		t.Fatalf("expected no archived PRDs after restore, got %v", archived)
	}
}

func TestArchivePRDMissing(t *testing.T) {
	base := t.TempDir()
	if err := ArchivePRD(base, "nope"); err == nil {
		t.Fatal("expected error archiving missing PRD")
	}
}

func TestArchivePRDConflict(t *testing.T) {
	base := t.TempDir()
	writeTestPRD(t, base, "dup")
	if err := ArchivePRD(base, "dup"); err != nil {
		t.Fatalf("first archive: %v", err)
	}
	// Recreate an active PRD with the same name, then archiving again must fail.
	writeTestPRD(t, base, "dup")
	if err := ArchivePRD(base, "dup"); err == nil {
		t.Fatal("expected conflict error when archive target already exists")
	}
}

func TestRestorePRDConflict(t *testing.T) {
	base := t.TempDir()
	writeTestPRD(t, base, "dup")
	if err := ArchivePRD(base, "dup"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	// An active PRD with the same name now exists again.
	writeTestPRD(t, base, "dup")
	if err := RestorePRD(base, "dup"); err == nil {
		t.Fatal("expected conflict error when restore target already exists")
	}
}

func TestListArchivedIgnoresNonPRDDirs(t *testing.T) {
	base := t.TempDir()
	// A directory without prd.md must not be reported.
	if err := os.MkdirAll(filepath.Join(ArchiveDir(base), "junk"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	archived, err := ListArchived(base)
	if err != nil {
		t.Fatalf("ListArchived: %v", err)
	}
	if len(archived) != 0 {
		t.Fatalf("expected no archived PRDs, got %v", archived)
	}
}
