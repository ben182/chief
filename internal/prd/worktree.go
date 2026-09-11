package prd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// PathIn maps a path that lives under baseDir into targetDir, keeping the
// relative layout. It is how a PRD's working files follow a run into its
// worktree: `.chief/prds/auth/prd.md` in the project becomes
// `<worktree>/.chief/prds/auth/prd.md`, and the legacy `.chief/prd.md` and
// direct-path layouts map just as faithfully instead of being straightened out
// into a location their PRD never used.
//
// It reports false when path is not under baseDir — a PRD kept outside the
// project has no counterpart in a worktree, and inventing one would put the run's
// state somewhere the user never asked for.
func PathIn(baseDir, path, targetDir string) (string, bool) {
	rel, ok := relativeTo(baseDir, path)
	if !ok {
		return "", false
	}
	return filepath.Join(targetDir, rel), true
}

// IsUnder reports whether path lies inside dir. It answers the question a
// mapping has to ask first — whether a path has already been moved into the
// checkout it is about to be moved into — so a second mapping can't nest a
// worktree path inside itself.
func IsUnder(dir, path string) bool {
	_, ok := relativeTo(dir, path)
	return ok
}

// relativeTo expresses path relative to dir, reporting false when path lies
// outside dir.
func relativeTo(dir, path string) (string, bool) {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// Mirror copies a PRD directory's working files from srcDir to dstDir, creating
// dstDir when it doesn't exist. Existing files in dstDir are overwritten: the
// caller decides which side is authoritative — the project when a worktree run
// starts, the worktree when it is torn down — and half-copying would leave a PRD
// whose status and progress disagree.
//
// Run logs are deliberately left behind. They are per-checkout, reach hundreds of
// megabytes, and the one thing nobody wants copied twice. Subdirectories are
// skipped too; a PRD directory is flat by construction.
//
// A missing srcDir is not an error: there is simply nothing to mirror.
func Mirror(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !mirrored(entry.Name()) {
			continue
		}
		if err := copyFile(filepath.Join(srcDir, entry.Name()), filepath.Join(dstDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// mirrored reports whether a file in a PRD directory travels with the run.
func mirrored(name string) bool {
	return !strings.HasSuffix(name, ".log")
}

// CopyFile copies src to dst, replacing dst and creating the destination
// directory when it is missing. It writes to a temporary file beside dst and
// renames it into place, so a reader — the TUI watching prd.md, the agent
// reading it — never sees a half-written file.
func CopyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return copyFile(src, dst)
}

// copyFile copies src to dst, replacing dst. It writes to a temporary file in
// the destination directory and renames it into place, so a reader — the TUI
// watching prd.md, the agent reading it — never sees a half-written file.
func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".chief-mirror-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	// A write path: a failing Close means the copy did not land.
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmpName, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}
