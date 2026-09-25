package prd

import (
	"bufio"
	"bytes"
	"os"
	"strings"
)

// LineRange is a 1-based, inclusive range of lines in a file, with the bytes it
// spans. Bytes is what decides whether an agent can read it in one go: the Read
// tool refuses anything past its token limit, and progress.md has lines
// thousands of characters long.
type LineRange struct {
	Start, End int
	Bytes      int
}

// ProgressEntryRange is one "## ..." section of progress.md below the patterns.
type ProgressEntryRange struct {
	Heading string
	LineRange
}

// ProgressMap says where things are in progress.md, so the agent can be told
// which lines to read instead of guessing a limit and trying again.
type ProgressMap struct {
	Lines int
	// Patterns is the "## Codebase Patterns" section, heading included. Nil when
	// the file has none yet.
	Patterns *LineRange
	// Entries are the sections after the patterns, in file order.
	Entries []ProgressEntryRange
	// lineBytes holds each line's length including its newline, for Chunks.
	lineBytes []int
}

// MapProgress reads progress.md and maps its sections. ok is false when there
// is no file to map.
func MapProgress(path string) (m ProgressMap, ok bool) {
	data, err := os.ReadFile(path) //nolint:gosec // the PRD's own progress file
	if err != nil {
		return ProgressMap{}, false
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	type section struct {
		heading string
		start   int
	}
	var sections []section
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		m.lineBytes = append(m.lineBytes, len(line)+1)
		if strings.HasPrefix(line, "## ") {
			sections = append(sections, section{heading: strings.TrimSpace(line), start: n})
		}
	}
	m.Lines = n

	for i, s := range sections {
		end := n
		if i+1 < len(sections) {
			end = sections[i+1].start - 1
		}
		r := LineRange{Start: s.start, End: end, Bytes: m.bytesOf(s.start, end)}
		if m.Patterns == nil && strings.EqualFold(strings.TrimPrefix(s.heading, "## "), "Codebase Patterns") {
			m.Patterns = &r
			continue
		}
		m.Entries = append(m.Entries, ProgressEntryRange{Heading: s.heading, LineRange: r})
	}
	return m, true
}

func (m ProgressMap) bytesOf(start, end int) int {
	total := 0
	for i := start; i <= end && i <= len(m.lineBytes); i++ {
		total += m.lineBytes[i-1]
	}
	return total
}

// Chunks splits r into consecutive ranges of at most maxBytes each, so every
// one of them fits a single read. A line longer than maxBytes on its own still
// gets a chunk of its own; there is no smaller unit to offer.
func (m ProgressMap) Chunks(r LineRange, maxBytes int) []LineRange {
	var out []LineRange
	cur := LineRange{Start: r.Start}
	for i := r.Start; i <= r.End && i <= len(m.lineBytes); i++ {
		b := m.lineBytes[i-1]
		if cur.Bytes > 0 && cur.Bytes+b > maxBytes {
			cur.End = i - 1
			out = append(out, cur)
			cur = LineRange{Start: i}
		}
		cur.Bytes += b
	}
	if cur.Bytes > 0 || len(out) == 0 {
		cur.End = r.End
		out = append(out, cur)
	}
	return out
}
