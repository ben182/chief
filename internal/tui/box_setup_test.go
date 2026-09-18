package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ben182/chief/internal/box"
)

func testCatalog() box.Catalog {
	return box.Catalog{
		Locations: []box.Location{
			{Name: "fsn1", City: "Falkenstein", Country: "DE", NetworkZone: "eu-central"},
			{Name: "ash", City: "Ashburn, VA", Country: "US", NetworkZone: "us-east"},
		},
		Types: map[string][]box.ServerType{
			"fsn1": {
				{Name: "cx22", Cores: 2, Memory: 4, Disk: 40, HourlyEUR: 0.0060},
				{Name: "cx33", Cores: 4, Memory: 8, Disk: 80, HourlyEUR: 0.0119},
				{Name: "cx43", Cores: 8, Memory: 16, Disk: 160, HourlyEUR: 0.0304, Deprecated: true},
			},
			"ash": {
				{Name: "cpx11", Cores: 2, Memory: 2, Disk: 40, HourlyEUR: 0.0080},
			},
		},
	}
}

// pressBoxSetup feeds keys to the picker and returns where it ended up. It uses
// the package's own key helper, so the picker is driven exactly as a terminal
// drives it.
func pressBoxSetup(m tea.Model, keys ...string) BoxSetup {
	for _, k := range keys {
		m, _ = m.Update(key(k))
	}
	return m.(BoxSetup)
}

func TestBoxSetupOpensOnTheCurrentChoice(t *testing.T) {
	m := NewBoxSetup(testCatalog(), "ash", "cpx11")
	if got := m.currentLocation().Name; got != "ash" {
		t.Errorf("opened on %s, want the location the project already uses", got)
	}
}

func TestBoxSetupAsksForLocationBeforeSize(t *testing.T) {
	// Location is the decision that matters — it is where the project's .env
	// spends the run — so it must not be buried behind the machine picker.
	m := NewBoxSetup(testCatalog(), "", "")
	view := m.View()
	if !strings.Contains(view, "Falkenstein") || strings.Contains(view, "vCPU") {
		t.Errorf("the first screen is not the location list:\n%s", view)
	}
}

func TestBoxSetupReturnsBothChoices(t *testing.T) {
	m := pressBoxSetup(NewBoxSetup(testCatalog(), "", ""), "enter", "down", "enter")
	if m.location != "fsn1" {
		t.Errorf("location = %q, want fsn1", m.location)
	}
	// Opened on cx33 (the default type), one down is cx43.
	if m.typeName != "cx43" {
		t.Errorf("type = %q, want cx43", m.typeName)
	}
	if m.cancelled {
		t.Error("reported cancelled after a completed selection")
	}
}

func TestChangingLocationResetsTheSize(t *testing.T) {
	// fsn1 offers three machines and ash offers one. Carrying the highlight
	// across would point past the end of the shorter list.
	m := pressBoxSetup(NewBoxSetup(testCatalog(), "", ""), "enter", "down", "down", "esc", "down", "enter", "enter")
	if m.location != "ash" || m.typeName != "cpx11" {
		t.Errorf("chose %s/%s, want ash/cpx11", m.location, m.typeName)
	}
}

func TestEscGoesBackBeforeItCancels(t *testing.T) {
	m := pressBoxSetup(NewBoxSetup(testCatalog(), "", ""), "enter", "esc")
	if m.cancelled {
		t.Error("esc on the second step cancelled instead of going back")
	}
	if m.step != stepLocation {
		t.Error("esc did not return to the location list")
	}

	m = pressBoxSetup(NewBoxSetup(testCatalog(), "", ""), "esc")
	if !m.cancelled {
		t.Error("esc on the first step should cancel")
	}
}

func TestSizeScreenShowsWhatARunCosts(t *testing.T) {
	m := pressBoxSetup(NewBoxSetup(testCatalog(), "", ""), "enter")
	m.width, m.height = 100, 40
	view := m.View()

	// 5 × 0.0119 is about six cents. A machine with no price beside it is a
	// machine chosen blind.
	if !strings.Contains(view, "6.0 cents") {
		t.Errorf("no five-hour estimate for cx33 in the view:\n%s", view)
	}
	if !strings.Contains(view, "being retired") {
		t.Errorf("a deprecated type is not marked:\n%s", view)
	}
}

func TestMoneyShowsCentsWhenEurosWouldRoundToNothing(t *testing.T) {
	if got := money(0.045); got != "4.5 cents" {
		t.Errorf("money(0.045) = %q, want cents", got)
	}
	if got := money(1.25); got != "€1.25" {
		t.Errorf("money(1.25) = %q", got)
	}
}
