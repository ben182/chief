package agent

import (
	"encoding/json"
	"strings"
)

// eachJSONLine unmarshals each non-empty, trimmed line of NDJSON (stream-json)
// output into a fresh T and invokes fn with it. Lines that fail to unmarshal are
// skipped. It is the shared primitive behind the providers' CleanOutput parsing.
func eachJSONLine[T any](output string, fn func(T)) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var v T
		if json.Unmarshal([]byte(line), &v) == nil {
			fn(v)
		}
	}
}

// lastMatchingLine returns the mapped value of the last NDJSON line for which
// match reports ok. Each line is unmarshalled into a T; match maps a parsed
// value to (value, ok). Returns "" when no line matches.
func lastMatchingLine[T any](output string, match func(T) (string, bool)) string {
	var last string
	eachJSONLine(output, func(v T) {
		if s, ok := match(v); ok {
			last = s
		}
	})
	return last
}
