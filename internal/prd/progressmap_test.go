package prd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChunksKeepEveryRangeUnderTheLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.md")
	// Ten lines of 100 bytes each, newline included.
	line := make([]byte, 99)
	for i := range line {
		line[i] = 'x'
	}
	var data []byte
	for i := 0; i < 10; i++ {
		data = append(append(data, line...), '\n')
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	m, ok := MapProgress(path)
	if !ok {
		t.Fatal("no map")
	}
	chunks := m.Chunks(LineRange{Start: 1, End: 10}, 350)
	want := [][2]int{{1, 3}, {4, 6}, {7, 9}, {10, 10}}
	if len(chunks) != len(want) {
		t.Fatalf("chunks = %+v, want %v", chunks, want)
	}
	for i, c := range chunks {
		if c.Start != want[i][0] || c.End != want[i][1] || c.Bytes > 350 {
			t.Errorf("chunk %d = %+v, want lines %v under 350 bytes", i, c, want[i])
		}
	}
}
