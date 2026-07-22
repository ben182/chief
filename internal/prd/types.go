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

// NextStory returns the next story to work on.
// It returns:
//   - First story with inProgress: true (interrupted story), or
//   - Lowest priority story with passes: false, or
//   - nil if all stories are complete
//
// Stories parked for human review (NeedsReview) are skipped so the loop moves
// on to other unblocked stories instead of retrying a stuck one forever.
func (p *PRD) NextStory() *UserStory {
	// First, check for any in-progress story (interrupted)
	for i := range p.UserStories {
		if p.UserStories[i].InProgress && !p.UserStories[i].NeedsReview {
			return &p.UserStories[i]
		}
	}

	// Find the lowest priority story that hasn't passed and isn't parked
	var next *UserStory
	for i := range p.UserStories {
		story := &p.UserStories[i]
		if !story.Passes && !story.NeedsReview {
			if next == nil || story.Priority < next.Priority {
				next = story
			}
		}
	}
	return next
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
