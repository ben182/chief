package loop

import "strings"

// reviewer holds the configuration for the optional post-commit review agent.
// When either field is non-empty, a separate agent reviews (and fixes) each
// story's committed changes with a fresh context before the story is marked done.
type reviewer struct {
	skill        string
	instructions string
}

// enabled reports whether a review agent should run after a story commits.
func (r reviewer) enabled() bool {
	return strings.TrimSpace(r.skill) != "" || strings.TrimSpace(r.instructions) != ""
}

// iterationMode distinguishes the build agent (the default that implements a
// story) from the review agent that runs afterwards. It is threaded explicitly
// through runIterationWithRetry -> runIteration -> processOutput so a
// <chief-done/> can be attributed to the right agent without a shared mode flag.
type iterationMode int

const (
	modeBuild iterationMode = iota
	modeReview
)
