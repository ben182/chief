package box

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ben182/chief/internal/config"
)

// What `chief box prep` is told comes from the same project reading and the
// same config the box is built from.
func TestEnvironmentDescribesWhatTheBoxIsBuiltWith(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.27.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Box.Packages = []string{"ffmpeg", "sqlite3"}

	got := Environment(dir, cfg)
	for _, want := range []string{
		"Ubuntu 24.04 LTS (Linux, x86_64)",
		"compared with `linux`",
		"no Xcode, no `codesign`, no Keychain",
		"- Go 1.27.1",
		"- apt packages from `box.packages`: ffmpeg sqlite3",
		"`box.packages` in `.chief/config.yaml`",
		"`DB_CONNECTION=pgsql`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
