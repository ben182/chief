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
	// Blocked marks a story the agent could not finish for a reason only a
	// person can resolve — a system dialog nobody answers, a locked password
	// manager, a contradiction in the PRD. BlockedReason says what it is and what
	// to do about it. Unlike NeedsReview, the stories that depend on it wait for
	// it instead of running anyway: their work would stand on something missing.
	// Not to be confused with BlockedBy, which is about other stories.
	Blocked       bool   `json:"blocked,omitempty"`
	BlockedReason string `json:"blockedReason,omitempty"`
	// BlockedBy lists the IDs of stories that must have Passes==true before this
	// story becomes eligible (see Frontier). Empty means the story can start
	// immediately. Unknown/typo IDs are ignored so they can never deadlock the loop.
	BlockedBy []string `json:"blockedBy,omitempty"`
}

// Parked reports whether the loop has set the story aside for a person: parked
// for review after repeated failures, or blocked on something only a person can
// resolve.
func (s *UserStory) Parked() bool {
	return s.NeedsReview || s.Blocked
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
// next: actionable (see Actionable), and with every blocker satisfied.
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
	for _, story := range p.Actionable() {
		if blockersSatisfied(story, exists, passed) {
			out = append(out, story)
		}
	}
	return out
}

// Actionable returns, in PRD order, every story the loop may still work on:
// not passed, not parked, and not waiting on a blocked story. A story waits
// when it depends — directly or through other stories — on a blocked story that
// has not passed; its work would stand on something a person has yet to
// provide. A story parked for review holds nothing back, since its dependents
// may well still be doable (see NextStory's fallback).
func (p *PRD) Actionable() []*UserStory {
	waiting := p.waiting()
	var out []*UserStory
	for i := range p.UserStories {
		story := &p.UserStories[i]
		if story.Passes || story.Parked() || waiting[story.ID] {
			continue
		}
		out = append(out, story)
	}
	return out
}

// waiting returns the IDs of the stories that cannot start before a person has
// acted: the blocked stories themselves, and every unpassed story that depends
// on one of them, however indirectly. Unknown and self-referencing blocker IDs
// are ignored exactly as in Frontier.
func (p *PRD) waiting() map[string]bool {
	waiting := make(map[string]bool)
	for i := range p.UserStories {
		if s := &p.UserStories[i]; s.Blocked && !s.Passes {
			waiting[s.ID] = true
		}
	}
	// Spread to dependents until nothing changes. PRDs have tens of stories, so
	// the repeated passes cost nothing, and a cycle simply stops spreading.
	for changed := len(waiting) > 0; changed; {
		changed = false
		for i := range p.UserStories {
			s := &p.UserStories[i]
			if s.Passes || waiting[s.ID] {
				continue
			}
			for _, dep := range s.BlockedBy {
				if dep != s.ID && waiting[dep] {
					waiting[s.ID] = true
					changed = true
					break
				}
			}
		}
	}
	return waiting
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
//  1. The first in-progress, actionable story (interrupted work resumes), or
//  2. the lowest-priority eligible frontier story — one whose blockers are all
//     satisfied (see Frontier); ties break by PRD order, or
//  3. as a graceful fallback when nothing on the frontier is eligible but
//     actionable work still remains (a dependency cycle, or every remaining
//     story is blocked by a story parked for review), the lowest-priority
//     actionable story — so the loop can never hang on an authoring bug, or
//  4. nil when there are no actionable stories left at all.
//
// Parked stories (NeedsReview, Blocked) are always skipped so the loop moves on
// instead of retrying a stuck one forever, and so are the stories waiting on a
// blocked one (see Actionable) — the fallback included.
func (p *PRD) NextStory() *UserStory {
	actionable := p.Actionable()

	// 1. In-progress (interrupted) story resumes first.
	for _, story := range actionable {
		if story.InProgress {
			return story
		}
	}

	// 2. Lowest-priority eligible frontier story.
	if next := lowestPriority(p.Frontier()); next != nil {
		return next
	}

	// 3. Graceful fallback: no eligible frontier story, but actionable work
	//    remains. 4. lowestPriority returns nil when nothing remains.
	return lowestPriority(actionable)
}

// AllResolved returns true when the loop has no more actionable work: every
// story is done, parked, or waiting on a blocked story.
func (p *PRD) AllResolved() bool {
	return len(p.Actionable()) == 0
}

// ParkedLabels names every story the loop set aside for a person, in PRD order:
// "ID - Title" for one parked for review, and "ID - Title (blocked: reason)" for
// a blocked one, since the reason is what the person has to act on.
func (p *PRD) ParkedLabels() []string {
	var out []string
	for _, s := range p.UserStories {
		switch {
		case s.Blocked:
			label := s.ID + " - " + s.Title + " (blocked"
			if s.BlockedReason != "" {
				label += ": " + s.BlockedReason
			}
			out = append(out, label+")")
		case s.NeedsReview:
			out = append(out, s.ID+" - "+s.Title)
		}
	}
	return out
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
