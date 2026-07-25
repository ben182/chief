package loop

import "strings"

// reviewer holds the configuration for the optional post-commit review agent.
// When it is explicitly enabled or either free-form field is non-empty, a
// separate agent reviews (and fixes) each story's committed changes with a fresh
// context before the story is marked done.
type reviewer struct {
	enabled      bool
	skill        string
	instructions string
}

// active reports whether a review agent should run after a story commits: true
// when explicitly enabled, or when a skill or instructions are configured.
func (r reviewer) active() bool {
	return r.enabled ||
		strings.TrimSpace(r.skill) != "" ||
		strings.TrimSpace(r.instructions) != ""
}

// iterationMode distinguishes the agents an iteration can run: the build agent
// (the default that implements a story), the review agent that runs after each
// story commits, and the consolidation agent that runs once at the end of the
// run. It is threaded explicitly through runIterationWithRetry -> runIteration ->
// processOutput so a <chief-done/> can be attributed to the right agent without a
// shared mode flag.
type iterationMode int

const (
	modeBuild iterationMode = iota
	modeReview
	modeConsolidate
)

// isStoryAgent reports whether the mode runs the build agent, whose
// <chief-done/> is the story-completion signal the loop gates on. The review and
// consolidation agents signal their own completion on separate flags, and theirs
// must not be forwarded as a story-done event.
func (m iterationMode) isStoryAgent() bool {
	return m == modeBuild
}
