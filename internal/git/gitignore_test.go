package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsChiefIgnored(t *testing.T) {
	t.Run("false in a repo that does not ignore .chief", func(t *testing.T) {
		dir := initTestRepo(t)
		if IsChiefIgnored(dir) {
			t.Error("IsChiefIgnored() = true, want false (no .gitignore entry)")
		}
	})

	t.Run("true once .chief is added to .gitignore", func(t *testing.T) {
		dir := initTestRepo(t)
		if err := AddChiefToGitignore(dir); err != nil {
			t.Fatalf("AddChiefToGitignore() error = %v", err)
		}
		// The pattern AddChiefToGitignore writes is ".chief/" (directory-only), so
		// git check-ignore only matches when .chief actually exists as a directory.
		if err := os.MkdirAll(filepath.Join(dir, ".chief"), 0755); err != nil {
			t.Fatal(err)
		}
		if !IsChiefIgnored(dir) {
			t.Error("IsChiefIgnored() = false, want true after adding .chief/ to .gitignore")
		}
	})
}
