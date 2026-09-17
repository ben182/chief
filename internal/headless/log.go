package headless

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// logger writes the run's log: one line per thing worth knowing, each stamped
// with the wall-clock time it happened at.
//
// The timestamp is absolute rather than relative to the start because these
// lines are read hours later, next to other logs on the same machine, and a run
// that began yesterday evening and ended this morning has to line up with them.
// It carries the date for the same reason: a five-hour run crosses midnight
// often enough that "03:12:44" alone is a question rather than an answer.
type logger struct {
	// mu serialises writes. Setup output arrives on the goroutine reading the
	// command's pipe while the run's own lines come from the caller's, and two
	// Fprintf calls racing on one writer interleave mid-line.
	mu      sync.Mutex
	out     io.Writer
	verbose bool
}

// newLogger returns a logger writing to out. A nil out discards everything,
// which keeps a caller that wants no log from having to supply a sink.
func newLogger(out io.Writer, verbose bool) *logger {
	return &logger{out: out, verbose: verbose}
}

// timestampLayout is the log's time prefix: sortable, and unambiguous next to
// whatever else writes to the same journal.
const timestampLayout = "2006-01-02 15:04:05"

// event writes a line that always belongs in the log.
func (l *logger) event(kind, format string, args ...any) {
	l.write(kind, fmt.Sprintf(format, args...))
}

// detail writes a line only a verbose run wants: the agent's narration, its
// tool calls, the thousands of lines a setup command produces. They are the
// difference between a log you can read after five hours and one you grep.
func (l *logger) detail(kind, format string, args ...any) {
	if !l.verbose {
		return
	}
	l.write(kind, fmt.Sprintf(format, args...))
}

// write emits one line, with multi-line messages folded onto continuation lines
// so a stack trace or an agent's paragraph keeps its shape without every line
// pretending to be its own event.
//
// A message that is nothing but whitespace is dropped rather than stamped. It is
// not a hypothetical: a setup command's output is full of blank lines, and every
// one of them would otherwise become a timestamped entry saying nothing.
func (l *logger) write(kind, msg string) {
	if l.out == nil {
		return
	}
	msg = strings.TrimRight(msg, " \t\n")
	if strings.TrimSpace(msg) == "" {
		return
	}

	stamp := time.Now().Format(timestampLayout)
	lines := strings.Split(msg, "\n")

	l.mu.Lock()
	defer l.mu.Unlock()
	// A log that cannot be written to is not a thing this package can do anything
	// about, and the run it is describing is worth more than the description.
	_, _ = fmt.Fprintf(l.out, "%s  %-10s %s\n", stamp, kind, lines[0])
	for _, line := range lines[1:] {
		// Indent by the width of the stamp and kind columns so continuations sit
		// under the text they belong to.
		_, _ = fmt.Fprintf(l.out, "%s  %-10s %s\n", strings.Repeat(" ", len(stamp)), "", line)
	}
}
