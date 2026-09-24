package box

import (
	"bytes"
	"io"
	"time"
)

// boxStampLayout is the time prefix of every line chief's headless mode logs.
// The box's clock runs on UTC — nothing ever sets a zone on it — so the stamp
// is UTC without saying so.
const boxStampLayout = "2006-01-02 15:04:05"

// localTimes rewrites the stamps in the box's log into the zone of the person
// reading it. A run started in the evening that reports its first story at
// "14:46" is two hours off for anyone in Berlin, and doing that arithmetic on
// every line of a ten-hour log is the job this does instead.
//
// Lines that do not start with a stamp — continuation lines, output from
// before chief took over the unit — pass through untouched.
type localTimes struct {
	out     io.Writer
	loc     *time.Location
	pending []byte
}

func newLocalTimes(out io.Writer) *localTimes {
	return &localTimes{out: out, loc: time.Local}
}

func (w *localTimes) Write(p []byte) (int, error) {
	w.pending = append(w.pending, p...)
	for {
		i := bytes.IndexByte(w.pending, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := w.pending[:i+1]
		if _, err := w.out.Write(w.localize(line)); err != nil {
			return len(p), err
		}
		w.pending = w.pending[i+1:]
	}
}

// Flush writes out a last line that never got its newline.
func (w *localTimes) Flush() error {
	if len(w.pending) == 0 {
		return nil
	}
	_, err := w.out.Write(w.localize(w.pending))
	w.pending = nil
	return err
}

func (w *localTimes) localize(line []byte) []byte {
	n := len(boxStampLayout)
	if len(line) < n {
		return line
	}
	t, err := time.ParseInLocation(boxStampLayout, string(line[:n]), time.UTC)
	if err != nil {
		return line
	}
	return append([]byte(t.In(w.loc).Format(boxStampLayout)), line[n:]...)
}

// localizeLog is localTimes for a log that has already been read whole.
func localizeLog(log string) string {
	var buf bytes.Buffer
	w := newLocalTimes(&buf)
	_, _ = w.Write([]byte(log))
	_ = w.Flush()
	return buf.String()
}
