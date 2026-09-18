package tui

import (
	"strings"
	"time"

	"github.com/ben182/chief/internal/box"
	"github.com/charmbracelet/lipgloss"
)

// How the dashboard treats the box this project is paying for.
const (
	// boxNoticeRefresh is how often the record on disk is re-read. The line is
	// rendered many times a second and the file changes perhaps twice a day.
	boxNoticeRefresh = 15 * time.Second
	// boxNoticeStale is the age at which the line stops being information and
	// starts being a warning. A box destroys itself twelve hours after boot
	// unless it was asked to stay or is holding work it could not push, so one
	// older than that is one chief is no longer expecting to go on its own.
	boxNoticeStale = 12 * time.Hour
)

// boxNotice is the line the dashboard shows while this project owns a box.
//
// It exists because a forgotten box is not forgotten in any dramatic way: it is
// a machine that did its work, was never destroyed, and bills six cents an hour
// for a week. Nothing in chief used to mention it. `chief box list` finds it,
// but that command is typed by somebody who already suspects — and the moment
// you would suspect is the moment you open chief on the project it belongs to.
//
// Pure, with the clock passed in: what it says about a two-day-old box is
// testable without waiting two days.
func boxNotice(s box.State, now time.Time) (text string, stale bool) {
	if s.Name == "" || s.Created.IsZero() {
		return "", false
	}
	age := now.Sub(s.Created)

	parts := []string{s.Name}
	if s.PRD != "" {
		parts = append(parts, s.PRD)
	}
	parts = append(parts, box.FormatAge(age.Round(time.Minute)))
	// An unknown price is said as one. Rendering it as €0.00 would be the one
	// misreading this line cannot afford.
	if s.HourlyEUR > 0 {
		parts = append(parts, box.FormatEUR(s.HourlyEUR*age.Hours()))
	} else {
		parts = append(parts, "cost unknown")
	}

	stale = age >= boxNoticeStale
	hint := "'chief box down' stops the billing"
	if stale {
		hint = "still billing — 'chief box status' says whether it is even still there"
	}
	return strings.Join(parts, " · ") + " — " + hint, stale
}

// boxState is this project's box record, re-read at most every
// boxNoticeRefresh so the render path is not a file read.
func (a *App) boxState() (box.State, bool) {
	// No project, no box. Without this the record would be looked for relative
	// to whatever the working directory happens to be.
	if a.baseDir == "" {
		return box.State{}, false
	}
	if a.boxCheckedAt.IsZero() || time.Since(a.boxCheckedAt) >= boxNoticeRefresh {
		s, ok := box.LoadState(a.baseDir)
		a.boxCheckedAt = time.Now()
		if ok {
			a.box = &s
		} else {
			a.box = nil
		}
	}
	if a.box == nil {
		return box.State{}, false
	}
	return *a.box, true
}

// hasBoxInfo reports whether the header carries the box line, which is a line
// of height the panels below it do not get.
func (a *App) hasBoxInfo() bool {
	s, ok := a.boxState()
	if !ok {
		return false
	}
	text, _ := boxNotice(s, time.Now())
	return text != ""
}

// renderBoxInfoLine renders the box line, or nothing when this project has no
// box.
func (a *App) renderBoxInfoLine() string {
	s, ok := a.boxState()
	if !ok {
		return ""
	}
	text, stale := boxNotice(s, time.Now())
	if text == "" {
		return ""
	}
	style := SubtitleStyle
	if stale {
		style = lipgloss.NewStyle().Foreground(WarningColor)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, "  ", SubtitleStyle.Render("box:"), style.Render(" "+text))
}
