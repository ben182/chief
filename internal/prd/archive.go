package prd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// PrdsDir returns the directory holding active PRDs (.chief/prds).
func PrdsDir(baseDir string) string {
	return filepath.Join(baseDir, ".chief", "prds")
}

// PRDDir returns the directory of a single PRD (.chief/prds/<name>).
func PRDDir(baseDir, name string) string {
	return filepath.Join(PrdsDir(baseDir), name)
}

// PRDPath returns the prd.md path of a single PRD (.chief/prds/<name>/prd.md).
func PRDPath(baseDir, name string) string {
	return filepath.Join(PRDDir(baseDir, name), "prd.md")
}

// ArchiveDir returns the directory holding archived PRDs (.chief/archive).
func ArchiveDir(baseDir string) string {
	return filepath.Join(baseDir, ".chief", "archive")
}

// ArchivePRD moves an active PRD directory from .chief/prds/<name> to
// .chief/archive/<name>. It is reversible via RestorePRD.
func ArchivePRD(baseDir, name string) error {
	src := filepath.Join(PrdsDir(baseDir), name)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("PRD %q not found: %w", name, err)
	}

	archiveDir := ArchiveDir(baseDir)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return fmt.Errorf("failed to create archive directory: %w", err)
	}

	dst := filepath.Join(archiveDir, name)
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("an archived PRD named %q already exists", name)
	}

	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("failed to archive PRD %q: %w", name, err)
	}
	return nil
}

// RestorePRD moves an archived PRD back from .chief/archive/<name> to
// .chief/prds/<name>.
func RestorePRD(baseDir, name string) error {
	src := filepath.Join(ArchiveDir(baseDir), name)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("archived PRD %q not found: %w", name, err)
	}

	prdsDir := PrdsDir(baseDir)
	if err := os.MkdirAll(prdsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create prds directory: %w", err)
	}

	dst := filepath.Join(prdsDir, name)
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("an active PRD named %q already exists", name)
	}

	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("failed to restore PRD %q: %w", name, err)
	}
	return nil
}

// ListArchived returns the sorted names of archived PRDs. An entry only counts
// as an archived PRD if its directory contains a prd.md file.
func ListArchived(baseDir string) ([]string, error) {
	archiveDir := ArchiveDir(baseDir)
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(archiveDir, entry.Name(), "prd.md")); err != nil {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}
