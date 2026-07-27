package loop

// consolidator holds the configuration for the optional consolidation pass that
// runs once at the end of a run, after every story has been built and reviewed.
//
// It exists because the per-story review agent has a structural blind spot: each
// story is implemented by a separate agent with a fresh context, so two stories
// can each grow their own helper for the same job, or introduce competing
// patterns for one concern, and both commits still look correct on their own. The
// consolidation agent is the only one that sees the whole run and refactors those
// seams away in one separate commit.
//
// enabled is the whole decision: whether a skill or instructions alone should
// switch the pass on is resolved once by config.ConsolidateConfig.Active(), so a
// caller that turns the pass off here gets it off, skill or not.
type consolidator struct {
	enabled      bool
	skill        string
	instructions string
}

// active reports whether the consolidation pass should run at the end of a run.
func (c consolidator) active() bool {
	return c.enabled
}
