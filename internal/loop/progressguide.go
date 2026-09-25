package loop

import (
	"fmt"
	"strings"

	"github.com/ben182/chief/internal/prd"
)

const (
	// readChunkBytes is the most one read of progress.md is sized to. The Read
	// tool stops at 25,000 tokens and prefixes every line with its number; on a
	// real progress file that came to about 2.7 bytes a token (95 KB were 35,000
	// tokens), so 40 KB is about 15,000 — clear of the limit with room to spare.
	readChunkBytes = 40_000
	// patternsBudgetBytes is how large the Codebase Patterns section may grow,
	// about 6,000 tokens. Every iteration reads it before anything else, so
	// every byte of it is paid for once per story; past this the agent is asked
	// to condense it before it starts.
	patternsBudgetBytes = 24_000
	// lastEntries is how many story entries the agent is pointed at.
	lastEntries = 2
)

// progressGuide tells the agent exactly which lines of progress.md to read.
// Left to guess, an agent asks for "the first 150 lines" of a file whose lines
// run to thousands of characters, has the read refused, and tries again with
// smaller numbers, several times an iteration. Empty when there is no file.
func progressGuide(path string) string {
	m, ok := prd.MapProgress(path)
	if !ok || m.Lines == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "   Where things are in it right now (%d lines) — read these ranges with offset and limit, one read per range:\n", m.Lines)
	if m.Patterns != nil {
		chunks := m.Chunks(*m.Patterns, readChunkBytes)
		fmt.Fprintf(&b, "   - `## Codebase Patterns`: %s\n", ranges(chunks))
	}
	if n := len(m.Entries); n > 0 {
		from := n - lastEntries
		if from < 0 {
			from = 0
		}
		for _, e := range m.Entries[from:] {
			fmt.Fprintf(&b, "   - `%s`: %s\n", e.Heading, ranges(m.Chunks(e.LineRange, readChunkBytes)))
		}
	}
	if m.Patterns != nil && m.Patterns.Bytes > patternsBudgetBytes {
		fmt.Fprintf(&b, "\n   **The Codebase Patterns section is %d KB, over its %d KB budget.** Before you\n"+
			"   start the story, rewrite that section in place so it fits: merge patterns that\n"+
			"   say the same thing, drop what is story-specific or plain from the code, and keep\n"+
			"   each pattern to one line. Change nothing below the section.\n",
			kb(m.Patterns.Bytes), kb(patternsBudgetBytes))
	}
	return b.String()
}

func ranges(chunks []prd.LineRange) string {
	parts := make([]string, len(chunks))
	for i, c := range chunks {
		parts[i] = fmt.Sprintf("lines %d–%d", c.Start, c.End)
	}
	return strings.Join(parts, ", ")
}

func kb(n int) int { return (n + 1023) / 1024 }
