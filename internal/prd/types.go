// Package prd provides types and utilities for working with Product
// Requirements Documents (PRDs). It includes loading, saving, watching
// for changes, and converting between prd.md and prd.json formats.
package prd

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"unicode"
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
	// Needs is the story's "**Braucht:**" line as written, e.g. "macOS (Xcode,
	// signing)": the operating system the story can only be done on, and why.
	// Only the system is read (see NeededOS); a story that names none runs
	// anywhere.
	Needs string `json:"needs,omitempty"`
	// BlockedBy lists the IDs of stories that must have Passes==true before this
	// story becomes eligible (see Frontier). Empty means the story can start
	// immediately. Unknown/typo IDs are ignored so they can never deadlock the loop.
	BlockedBy []string `json:"blockedBy,omitempty"`
	// Body is the story's markdown as the PRD has it, everything below the
	// heading except the status line. It is what the agent is given: the fields
	// above keep only what chief acts on, and a story's sub-bullets, thresholds
	// and "manual" blocks are in none of them.
	Body string `json:"-"`
}

// Parked reports whether the loop has set the story aside for a person: parked
// for review after repeated failures, or blocked on something only a person can
// resolve.
func (s *UserStory) Parked() bool {
	return s.NeedsReview || s.Blocked
}

// NeededOS is the operating system the story's Needs line names, as Go spells it
// in runtime.GOOS ("darwin", "linux", "windows"), or "" when it names none that
// chief knows — which makes the story runnable anywhere, the way an unknown
// blocker ID is ignored: a typo must never keep a story from running at all.
func (s *UserStory) NeededOS() string {
	return ParseOS(s.Needs)
}

// RunsOn reports whether the story can be worked on under the operating system
// goos (a runtime.GOOS value).
func (s *UserStory) RunsOn(goos string) bool {
	need := s.NeededOS()
	return need == "" || need == goos
}

// ParseOS reads the operating system a "**Braucht:**" value names — "macOS
// (Xcode)", "macOS 14+", "Xcode (macOS)", "macOS/Xcode", "Linux (systemd)" —
// and returns it as Go spells it in runtime.GOOS. It returns "" when the value
// names none, or more than one (see CheckNeeds for the reason).
func ParseOS(needs string) string {
	goos, _ := CheckNeeds(needs)
	return goos
}

// CheckNeeds is ParseOS with the reason it came back empty: problem is "" for a
// value that names exactly one system, and says what is wrong otherwise. An
// empty value is no problem — the story runs anywhere.
//
// The system is found as a word anywhere in the value, case-insensitive:
// macOS, Mac, Mac OS (X), OS X, OSX, darwin; Linux, Ubuntu, Debian; Windows.
// A value naming two different systems is read as naming none, so the story
// runs anywhere, like one that names none: a line chief cannot read must never
// keep a story from running at all.
func CheckNeeds(needs string) (goos, problem string) {
	if strings.TrimSpace(needs) == "" {
		return "", ""
	}
	words := strings.FieldsFunc(strings.ToLower(needs), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	found := map[string]bool{}
	var order []string
	add := func(g string) {
		if !found[g] {
			found[g] = true
			order = append(order, g)
		}
	}
	for i, w := range words {
		next := ""
		if i+1 < len(words) {
			next = words[i+1]
		}
		switch w {
		case "macos", "mac", "osx", "darwin":
			add("darwin")
		case "os":
			if next == "x" {
				add("darwin")
			}
		case "linux", "ubuntu", "debian":
			add("linux")
		case "windows":
			add("windows")
		}
	}
	switch len(order) {
	case 0:
		return "", "names no operating system chief knows (macOS, Linux, Windows), so the story runs anywhere"
	case 1:
		return order[0], ""
	}
	names := make([]string, len(order))
	for i, g := range order {
		names[i] = OSName(g)
	}
	return "", fmt.Sprintf("names more than one operating system (%s), so the story runs anywhere", strings.Join(names, ", "))
}

// NeedsWarnings lists, one line per story, the open stories whose
// "**Braucht:**" line chief cannot read — no system it knows, or several — so a
// run can say why a story it was meant to skip is running after all.
func (p *PRD) NeedsWarnings() []string {
	var out []string
	for i := range p.UserStories {
		s := &p.UserStories[i]
		if s.Passes {
			continue
		}
		if _, problem := CheckNeeds(s.Needs); problem != "" {
			out = append(out, fmt.Sprintf("%s: **Braucht:** %q %s", s.ID, s.Needs, problem))
		}
	}
	return out
}

// OSName is how a person writes the operating system goos: "macOS" for
// "darwin". Unknown values are returned as they are.
func OSName(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	}
	return goos
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
	return p.frontierOn(runtime.GOOS)
}

func (p *PRD) frontierOn(goos string) []*UserStory {
	passed := make(map[string]bool, len(p.UserStories))
	exists := make(map[string]bool, len(p.UserStories))
	for i := range p.UserStories {
		exists[p.UserStories[i].ID] = true
		if p.UserStories[i].Passes {
			passed[p.UserStories[i].ID] = true
		}
	}

	var out []*UserStory
	for _, story := range p.ActionableOn(goos) {
		if blockersSatisfied(story, exists, passed) {
			out = append(out, story)
		}
	}
	return out
}

// Actionable returns, in PRD order, every story the loop may still work on on
// this machine: see ActionableOn.
func (p *PRD) Actionable() []*UserStory {
	return p.ActionableOn(runtime.GOOS)
}

// ActionableOn returns, in PRD order, every story the loop may still work on
// under the operating system goos: not passed, not parked, runnable on goos,
// and not waiting on a story that is blocked or needs another system. A story
// waits when it depends — directly or through other stories — on one of those
// that has not passed; its work would stand on something that cannot happen
// here. A story parked for review holds nothing back, since its dependents may
// well still be doable (see NextStory's fallback).
func (p *PRD) ActionableOn(goos string) []*UserStory {
	waiting := p.waiting(goos)
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

// OtherOS returns, in PRD order, the unpassed stories that need an operating
// system other than goos.
func (p *PRD) OtherOS(goos string) []*UserStory {
	var out []*UserStory
	for i := range p.UserStories {
		if s := &p.UserStories[i]; !s.Passes && !s.RunsOn(goos) {
			out = append(out, s)
		}
	}
	return out
}

// waiting returns the IDs of the stories that cannot start under goos: the
// blocked stories and those needing another system, and every unpassed story
// that depends on one of them, however indirectly. Unknown and
// self-referencing blocker IDs are ignored exactly as in Frontier.
func (p *PRD) waiting(goos string) map[string]bool {
	waiting := make(map[string]bool)
	for i := range p.UserStories {
		if s := &p.UserStories[i]; !s.Passes && (s.Blocked || !s.RunsOn(goos)) {
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
// instead of retrying a stuck one forever, and so are the stories that need
// another operating system and the stories waiting on a blocked one or one of
// those (see Actionable) — the fallback included.
func (p *PRD) NextStory() *UserStory {
	return p.NextStoryOn(runtime.GOOS)
}

// NextStoryOn is NextStory for a run under the operating system goos. Stories
// that need another system are skipped like parked ones, and so is everything
// that depends on them.
func (p *PRD) NextStoryOn(goos string) *UserStory {
	actionable := p.ActionableOn(goos)

	// 1. In-progress (interrupted) story resumes first.
	for _, story := range actionable {
		if story.InProgress {
			return story
		}
	}

	// 2. Lowest-priority eligible frontier story.
	if next := lowestPriority(p.frontierOn(goos)); next != nil {
		return next
	}

	// 3. Graceful fallback: no eligible frontier story, but actionable work
	//    remains. 4. lowestPriority returns nil when nothing remains.
	return lowestPriority(actionable)
}

// AllResolved returns true when the loop has no more actionable work on this
// machine: every story is done, parked, needs another operating system, or
// waits on one of those.
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

// storyContext formats a story for inlining into an agent prompt: the markdown
// the PRD has for it when it was parsed from one, otherwise JSON (with a
// plain-text fallback). Returns nil for a nil story.
func storyContext(story *UserStory) *string {
	if story == nil {
		return nil
	}
	if story.Body != "" {
		result := "### " + story.ID + ": " + story.Title + "\n\n" + story.Body
		return &result
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

// AcceptanceCriteriaMarkdown is the story's acceptance criteria as the PRD
// writes them: the checkbox list in Body, with the sub-bullets indented under
// a criterion. AcceptanceCriteria keeps only the checkbox lines, so a criterion
// that is a heading for its sub-bullets loses everything it says. The list ends
// at the first unindented line that is neither a checkbox nor blank. Returns ""
// when the story was not parsed from markdown or has no checkboxes.
func (s *UserStory) AcceptanceCriteriaMarkdown() string {
	var out []string
	for _, line := range strings.Split(s.Body, "\n") {
		trimmed := strings.TrimSpace(line)
		isCheckbox := checkboxRegex.MatchString(trimmed)
		if out == nil {
			if isCheckbox && line == trimmed {
				out = append(out, line)
			}
			continue
		}
		indented := line != "" && (line[0] == ' ' || line[0] == '\t')
		if trimmed == "" || indented || isCheckbox {
			out = append(out, line)
			continue
		}
		break
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
