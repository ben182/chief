// Package prd provides types and utilities for working with Product
// Requirements Documents (PRDs). It includes loading, saving, watching
// for changes, and converting between prd.md and prd.json formats.
package prd

import (
	"encoding/json"
	"fmt"
	"strings"
)

// UserStory represents a single user story in a PRD.
type UserStory struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria []string `json:"acceptanceCriteria"`
	Priority           float64  `json:"priority"`
	Passes             bool     `json:"passes"`
	InProgress         bool     `json:"inProgress,omitempty"`
	NeedsReview        bool     `json:"needsReview,omitempty"` // parked after repeated failures; skipped by NextStory
	// BlockedBy lists the IDs of stories that must have Passes==true before this
	// story becomes eligible (see Frontier). Empty means the story can start
	// immediately. Unknown/typo IDs are ignored so they can never deadlock the loop.
	BlockedBy []string `json:"blockedBy,omitempty"`
}

// PRD represents a Product Requirements Document.
type PRD struct {
	Project     string      `json:"project"`
	Description string      `json:"description"`
	UserStories []UserStory `json:"userStories"`
}

// ExtractIDPrefix returns the ID prefix used by the stories in this PRD.
// For example, "US" from "US-001", "MFR" from "MFR-001", "T" from "T-001".
// Returns "US" as the default when the PRD has no stories or IDs lack a hyphen.
func (p *PRD) ExtractIDPrefix() string {
	for _, story := range p.UserStories {
		if idx := strings.LastIndex(story.ID, "-"); idx > 0 {
			return story.ID[:idx]
		}
	}
	return "US"
}

// AllComplete returns true when all stories have passes: true.
func (p *PRD) AllComplete() bool {
	if len(p.UserStories) == 0 {
		return true
	}
	for _, story := range p.UserStories {
		if !story.Passes {
			return false
		}
	}
	return true
}

// CompletedCount returns the number of stories that have passed.
func (p *PRD) CompletedCount() int {
	n := 0
	for _, story := range p.UserStories {
		if story.Passes {
			n++
		}
	}
	return n
}

// Completed returns the stories that have passed, in PRD order.
func (p *PRD) Completed() []UserStory {
	var out []UserStory
	for _, story := range p.UserStories {
		if story.Passes {
			out = append(out, story)
		}
	}
	return out
}

// Incomplete returns the stories that have not yet passed, in PRD order.
func (p *PRD) Incomplete() []UserStory {
	var out []UserStory
	for _, story := range p.UserStories {
		if !story.Passes {
			out = append(out, story)
		}
	}
	return out
}

// Frontier returns, in PRD order, every story that is eligible to be worked on
// next: not passed, not parked (NeedsReview), and with every blocker satisfied.
//
// A blocker ID (from BlockedBy) is "satisfied" when it either refers to a story
// in this PRD that has Passes==true, or refers to no story at all. Robustness
// rules that guarantee the loop can never deadlock on authoring mistakes:
//   - An unknown/typo blocker ID (matches no story) is treated as satisfied.
//   - A self-reference (a story listing its own ID) is ignored.
//   - Duplicate IDs are handled safely (checked more than once is harmless).
func (p *PRD) Frontier() []*UserStory {
	passed := make(map[string]bool, len(p.UserStories))
	exists := make(map[string]bool, len(p.UserStories))
	for i := range p.UserStories {
		exists[p.UserStories[i].ID] = true
		if p.UserStories[i].Passes {
			passed[p.UserStories[i].ID] = true
		}
	}

	var out []*UserStory
	for i := range p.UserStories {
		story := &p.UserStories[i]
		if story.Passes || story.NeedsReview {
			continue
		}
		if blockersSatisfied(story, exists, passed) {
			out = append(out, story)
		}
	}
	return out
}

// blockersSatisfied reports whether every blocker of story is satisfied given
// the set of existing story IDs and the set of passed story IDs.
func blockersSatisfied(story *UserStory, exists, passed map[string]bool) bool {
	for _, dep := range story.BlockedBy {
		if dep == story.ID {
			continue // self-reference: ignore
		}
		if !exists[dep] {
			continue // unknown/typo ID: treat as satisfied so it can't deadlock
		}
		if !passed[dep] {
			return false
		}
	}
	return true
}

// lowestPriority returns the story with the lowest Priority, breaking ties by
// PRD order (the first story encountered wins on equal priority). Returns nil
// for an empty slice.
func lowestPriority(stories []*UserStory) *UserStory {
	var best *UserStory
	for _, s := range stories {
		if best == nil || s.Priority < best.Priority {
			best = s
		}
	}
	return best
}

// NextStory returns the next story to work on:
//
//  1. The first in-progress, non-parked story (interrupted work resumes), or
//  2. the lowest-priority eligible frontier story — one whose blockers are all
//     satisfied (see Frontier); ties break by PRD order, or
//  3. as a graceful fallback when nothing on the frontier is eligible but
//     unpassed, non-parked work still remains (a dependency cycle, or every
//     remaining story is blocked by a parked story), the lowest-priority
//     unpassed, non-parked story — so the loop can never hang on an authoring
//     bug, or
//  4. nil when there are no unpassed, non-parked stories left at all.
//
// Stories parked for human review (NeedsReview) are always skipped so the loop
// moves on instead of retrying a stuck one forever.
func (p *PRD) NextStory() *UserStory {
	// 1. In-progress (interrupted) story resumes first.
	for i := range p.UserStories {
		if p.UserStories[i].InProgress && !p.UserStories[i].NeedsReview {
			return &p.UserStories[i]
		}
	}

	// 2. Lowest-priority eligible frontier story.
	if next := lowestPriority(p.Frontier()); next != nil {
		return next
	}

	// 3. Graceful fallback: no eligible frontier story, but actionable work
	//    remains. Pick the lowest-priority unpassed, non-parked story.
	var remaining []*UserStory
	for i := range p.UserStories {
		story := &p.UserStories[i]
		if !story.Passes && !story.NeedsReview {
			remaining = append(remaining, story)
		}
	}
	// 4. lowestPriority returns nil when nothing remains.
	return lowestPriority(remaining)
}

// AllResolved returns true when every story is either done (passes) or parked
// for human review (NeedsReview) — i.e. the loop has no more actionable work.
func (p *PRD) AllResolved() bool {
	for _, story := range p.UserStories {
		if !story.Passes && !story.NeedsReview {
			return false
		}
	}
	return true
}

// NextStoryContext returns the next story to work on as a formatted string
// suitable for inlining into the agent prompt. Returns nil when all stories
// are complete.
func (p *PRD) NextStoryContext() *string {
	return storyContext(p.NextStory())
}

// StoryContextByID returns the story with the given ID formatted for inlining
// into an agent prompt (used by the review agent, which targets a specific
// already-built story rather than "the next one"). Returns nil if not found.
func (p *PRD) StoryContextByID(id string) *string {
	for i := range p.UserStories {
		if p.UserStories[i].ID == id {
			return storyContext(&p.UserStories[i])
		}
	}
	return nil
}

// storyContext formats a story as JSON (with a plain-text fallback) for
// inlining into an agent prompt. Returns nil for a nil story.
func storyContext(story *UserStory) *string {
	if story == nil {
		return nil
	}

	data, err := json.MarshalIndent(story, "", "  ")
	if err != nil {
		// Fallback to a simple text format
		var b strings.Builder
		fmt.Fprintf(&b, "ID: %s\nTitle: %s\nDescription: %s\n", story.ID, story.Title, story.Description)
		fmt.Fprintf(&b, "Acceptance Criteria:\n")
		for _, ac := range story.AcceptanceCriteria {
			fmt.Fprintf(&b, "- %s\n", ac)
		}
		result := b.String()
		return &result
	}

	result := string(data)
	return &result
}
