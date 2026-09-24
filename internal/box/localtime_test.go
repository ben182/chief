package box

import (
	"bytes"
	"testing"
	"time"
)

func TestLocalTimesRewritesTheBoxStamps(t *testing.T) {
	var buf bytes.Buffer
	w := newLocalTimes(&buf)
	w.loc = time.FixedZone("CEST", 2*60*60)

	// Written in pieces that split lines, the way ssh hands the stream over.
	for _, chunk := range []string{
		"2026-09-24 14:31:31  run        default · 49 stor",
		"ies\n                     continued\nnot a stamp\n2026-09-24 23:59:59  story ",
		"     US-1 done",
	} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "2026-09-24 16:31:31  run        default · 49 stories\n" +
		"                     continued\n" +
		"not a stamp\n" +
		"2026-09-25 01:59:59  story      US-1 done"
	if got := buf.String(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}
