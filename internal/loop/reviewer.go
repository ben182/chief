package loop

// reviewer holds the configuration for the optional post-commit review agent.
// When it is enabled, a separate agent reviews (and fixes) each story's
// committed changes with a fresh context before the story is marked done.
//
// enabled is the whole decision: whether a skill or instructions alone should
// switch the review on is resolved once by config.ReviewConfig.Active(), so a
// caller that turns the review off here gets it off, skill or not.
type reviewer struct {
	enabled      bool
	skill        string
	instructions string
}

// active reports whether a review agent should run after a story commits.
func (r reviewer) active() bool {
	return r.enabled
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
