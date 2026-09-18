package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/ben182/chief/internal/box"
)

func TestBoxNoticeSaysWhatTheBoxHasCost(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	s := box.State{
		Name:      "chief-shop-auth-101500",
		PRD:       "auth",
		Created:   now.Add(-2 * time.Hour),
		HourlyEUR: 0.0603,
	}
	text, stale := boxNotice(s, now)
	for _, want := range []string{"chief-shop-auth-101500", "auth", "2h 0m", "€0.12", "chief box down"} {
		if !strings.Contains(text, want) {
			t.Errorf("notice = %q, want %q in it", text, want)
		}
	}
	if stale {
		t.Error("a two-hour-old box is not yet the kind you have forgotten")
	}
}

func TestBoxNoticeTurnsIntoAWarningOnceItShouldHaveGone(t *testing.T) {
	now := time.Now()
	s := box.State{Name: "chief-old", PRD: "auth", Created: now.Add(-3 * 24 * time.Hour), HourlyEUR: 0.0603}
	text, stale := boxNotice(s, now)
	if !stale {
		t.Error("a three-day-old box is exactly the one nobody remembers")
	}
	// The box was meant to destroy itself; one that did not is either kept on
	// purpose or gone already, and the line has to send you somewhere that knows.
	if !strings.Contains(text, "chief box status") {
		t.Errorf("notice = %q, want it to point at the command that can tell", text)
	}
	if !strings.Contains(text, "3d 0h") {
		t.Errorf("notice = %q, want the age in days", text)
	}
}

func TestBoxNoticeNeverImpliesABoxIsFree(t *testing.T) {
	now := time.Now()
	// A box created before chief recorded prices, or one whose price list did
	// not answer. "€0.00" would be a lie about the one thing this line is for.
	s := box.State{Name: "chief-unpriced", Created: now.Add(-90 * time.Minute)}
	text, _ := boxNotice(s, now)
	if strings.Contains(text, "0.00") || strings.Contains(text, "0 cents") {
		t.Errorf("notice = %q, which reads as free", text)
	}
	if !strings.Contains(text, "cost unknown") {
		t.Errorf("notice = %q, want it to admit the price is not known", text)
	}
}

func TestBoxNoticeIsNothingWithoutABox(t *testing.T) {
	if text, _ := boxNotice(box.State{}, time.Now()); text != "" {
		t.Errorf("notice = %q for a project with no box", text)
	}
}

func TestTheHeaderGrowsForEachOptionalLine(t *testing.T) {
	plain := joinHeader("head", "tabs", "", "", "border")
	if strings.Count(plain, "\n") != 2 {
		t.Errorf("a header with no optional lines = %q", plain)
	}
	both := joinHeader("head", "tabs", "branch", "box", "border")
	if strings.Count(both, "\n") != 4 {
		t.Errorf("a header with both optional lines = %q", both)
	}
}

func TestTheDashboardMakesRoomForTheBoxLine(t *testing.T) {
	dir := t.TempDir()
	app := &App{prdName: "auth", baseDir: dir}
	if app.hasBoxInfo() {
		t.Fatal("a project with no box should not reserve the line")
	}

	if err := box.SaveState(dir, box.State{
		ServerID: 1, Name: "chief-shop-auth", IP: "203.0.113.4", PRD: "auth",
		Created: time.Now().Add(-time.Hour), HourlyEUR: 0.0603,
	}); err != nil {
		t.Fatal(err)
	}
	// The cache is what a render path reads; a box created a moment ago has to
	// reach the header without waiting for it to expire.
	app.boxCheckedAt = time.Time{}

	if !app.hasBoxInfo() {
		t.Fatal("the box line is missing for a project that has a box")
	}
	if got := app.effectiveHeaderHeight(); got != headerHeight+1 {
		t.Errorf("effectiveHeaderHeight() = %d, want %d", got, headerHeight+1)
	}
	if line := app.renderBoxInfoLine(); !strings.Contains(line, "chief-shop-auth") {
		t.Errorf("box line = %q", line)
	}
}
