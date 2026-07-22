package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    Options
		wantErr bool
	}{
		{
			name: "empty",
			args: nil,
			want: Options{},
		},
		{
			name: "prd name becomes path",
			args: []string{"auth"},
			want: Options{PRDPath: filepath.Join(".chief", "prds", "auth", "prd.md")},
		},
		{
			name: "direct md path used verbatim",
			args: []string{"./my-prd.md"},
			want: Options{PRDPath: "./my-prd.md"},
		},
		{
			name: "verbose and no-retry",
			args: []string{"--verbose", "--no-retry"},
			want: Options{Verbose: true, NoRetry: true},
		},
		{
			name: "max-iterations space form",
			args: []string{"--max-iterations", "20"},
			want: Options{MaxIterations: 20},
		},
		{
			name: "max-iterations equals form",
			args: []string{"--max-iterations=5"},
			want: Options{MaxIterations: 5},
		},
		{
			name: "short n equals form",
			args: []string{"-n=7"},
			want: Options{MaxIterations: 7},
		},
		{
			name: "short n space form",
			args: []string{"-n", "3"},
			want: Options{MaxIterations: 3},
		},
		{
			name: "agent flags extracted",
			args: []string{"--agent", "codex", "--model", "gpt", "auth"},
			want: Options{Agent: "codex", Model: "gpt", PRDPath: filepath.Join(".chief", "prds", "auth", "prd.md")},
		},
		{
			name: "agent equals form does not eat positional",
			args: []string{"--agent=cursor", "auth"},
			want: Options{Agent: "cursor", PRDPath: filepath.Join(".chief", "prds", "auth", "prd.md")},
		},
		{
			name: "help stops parsing",
			args: []string{"--help", "auth"},
			want: Options{ShowHelp: true},
		},
		{
			name: "version stops parsing",
			args: []string{"-v"},
			want: Options{ShowVersion: true},
		},
		{
			name:    "unknown flag errors",
			args:    []string{"--bogus"},
			wantErr: true,
		},
		{
			name:    "max-iterations non-numeric errors",
			args:    []string{"--max-iterations", "abc"},
			wantErr: true,
		},
		{
			name:    "max-iterations zero errors",
			args:    []string{"--max-iterations=0"},
			wantErr: true,
		},
		{
			name:    "max-iterations missing value errors",
			args:    []string{"--max-iterations"},
			wantErr: true,
		},
		{
			name:    "agent missing value errors",
			args:    []string{"--agent"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseArgs(%v) = %+v, want error", tt.args, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseArgs(%v) unexpected error: %v", tt.args, err)
			}
			if *got != tt.want {
				t.Errorf("ParseArgs(%v) = %+v, want %+v", tt.args, *got, tt.want)
			}
		})
	}
}

func TestPRDPathFromArg(t *testing.T) {
	tests := []struct {
		arg  string
		want string
	}{
		{"auth", filepath.Join(".chief", "prds", "auth", "prd.md")},
		{"./x.md", "./x.md"},
		{"foo.json", "foo.json"},
		{"some/dir/", "some/dir/"},
	}
	for _, tt := range tests {
		if got := PRDPathFromArg(tt.arg); got != tt.want {
			t.Errorf("PRDPathFromArg(%q) = %q, want %q", tt.arg, got, tt.want)
		}
	}
}

func TestFindAndListAvailablePRDs(t *testing.T) {
	base := t.TempDir()
	// A valid PRD (has prd.md), a directory without prd.md, and a stray file.
	mustMkPRD(t, base, "auth")
	mustMkPRD(t, base, "billing")
	if err := os.MkdirAll(filepath.Join(base, ".chief", "prds", "empty"), 0755); err != nil {
		t.Fatal(err)
	}

	names := ListAvailablePRDs(base)
	if len(names) != 2 {
		t.Fatalf("ListAvailablePRDs = %v, want 2 entries (auth, billing)", names)
	}

	found := FindAvailablePRD(base)
	if found == "" {
		t.Fatal("FindAvailablePRD returned empty, want a prd.md path")
	}
	if _, err := os.Stat(found); err != nil {
		t.Errorf("FindAvailablePRD returned %q which does not exist: %v", found, err)
	}

	// A directory with no PRDs at all yields nothing.
	if got := ListAvailablePRDs(t.TempDir()); got != nil {
		t.Errorf("ListAvailablePRDs(empty base) = %v, want nil", got)
	}
	if got := FindAvailablePRD(t.TempDir()); got != "" {
		t.Errorf("FindAvailablePRD(empty base) = %q, want empty", got)
	}
}

func mustMkPRD(t *testing.T, base, name string) {
	t.Helper()
	dir := filepath.Join(base, ".chief", "prds", name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prd.md"), []byte("# "+name+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
}
