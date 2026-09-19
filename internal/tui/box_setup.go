package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ben182/chief/internal/box"
)

// estimateHours is the run length the picker prices everything against.
//
// A number rather than a range, because the point of showing money here is to
// make two machines comparable, and five hours is what a chief run actually
// takes. It is also the number that makes the decision feel correct: every
// option on the list costs less than a coffee, and knowing that is what stops
// someone picking the smallest box and waiting all evening for a test suite.
const estimateHours = 5

// boxSetupStep is which question the picker is on.
type boxSetupStep int

const (
	stepLocation boxSetupStep = iota
	stepType
)

// maxVisibleRows caps how much of a list is drawn at once, so a Hetzner that
// grows a dozen new machine sizes does not push the footer off the terminal.
const maxVisibleRows = 12

// BoxSetup asks the two questions a box cannot be created without: where it
// runs, and on what.
//
// Location comes first and is the reason this screen exists at all. The machine
// receives the project's source, its .env and a token that can push to it, and
// keeps them for the length of a run. Which jurisdiction that happens in is not
// a performance tuning knob, and it should not be a default nobody was shown.
type BoxSetup struct {
	width, height int

	catalog box.Catalog
	step    boxSetupStep

	locationIndex int
	typeIndex     int
	typeOffset    int

	location  string
	typeName  string
	cancelled bool
}

// NewBoxSetup builds the picker, opening on what the project already uses.
func NewBoxSetup(catalog box.Catalog, currentLocation, currentType string) *BoxSetup {
	s := &BoxSetup{catalog: catalog}
	if currentLocation == "" {
		currentLocation = box.DefaultLocation
	}
	for i, l := range catalog.Locations {
		if l.Name == currentLocation {
			s.locationIndex = i
			break
		}
	}
	// The type is looked up in the location that was just selected, because the
	// same machine is not offered everywhere. A project that has never chosen
	// one opens on index zero, which is the cheapest the location sells — the
	// same machine `box up` would create unasked.
	for i, t := range catalog.TypesIn(s.currentLocation().Name) {
		if t.Name == currentType {
			s.typeIndex = i
			break
		}
	}
	s.scrollTypeIntoView()
	return s
}

func (m BoxSetup) currentLocation() box.Location {
	if len(m.catalog.Locations) == 0 {
		return box.Location{}
	}
	return m.catalog.Locations[m.locationIndex]
}

func (m BoxSetup) currentTypes() []box.ServerType {
	return m.catalog.TypesIn(m.currentLocation().Name)
}

// Init implements tea.Model.
func (m BoxSetup) Init() tea.Cmd { return tea.EnterAltScreen }

// Update implements tea.Model.
func (m BoxSetup) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.cancelled = true
			return m, tea.Quit

		case "esc":
			if m.step == stepType {
				m.step = stepLocation
				return m, nil
			}
			m.cancelled = true
			return m, tea.Quit

		case "up", "k":
			m.moveBy(-1)
			return m, nil

		case "down", "j":
			m.moveBy(1)
			return m, nil

		case "enter":
			return m.advance()
		}
	}
	return m, nil
}

func (m *BoxSetup) moveBy(delta int) {
	if m.step == stepLocation {
		next := m.locationIndex + delta
		if next < 0 || next >= len(m.catalog.Locations) {
			return
		}
		m.locationIndex = next
		// Moving to a location that does not offer the highlighted machine would
		// leave the next step pointing past the end of its own list.
		m.typeIndex, m.typeOffset = 0, 0
		return
	}

	next := m.typeIndex + delta
	if next < 0 || next >= len(m.currentTypes()) {
		return
	}
	m.typeIndex = next
	m.scrollTypeIntoView()
}

func (m *BoxSetup) scrollTypeIntoView() {
	if m.typeIndex < m.typeOffset {
		m.typeOffset = m.typeIndex
	}
	if m.typeIndex >= m.typeOffset+maxVisibleRows {
		m.typeOffset = m.typeIndex - maxVisibleRows + 1
	}
}

func (m BoxSetup) advance() (tea.Model, tea.Cmd) {
	if m.step == stepLocation {
		m.step = stepType
		return m, nil
	}
	types := m.currentTypes()
	if len(types) == 0 {
		m.cancelled = true
		return m, tea.Quit
	}
	m.location = m.currentLocation().Name
	m.typeName = types[m.typeIndex].Name
	return m, tea.Quit
}

// View implements tea.Model.
func (m BoxSetup) View() string {
	modalWidth := min(74, m.width-6)
	if modalWidth < 56 {
		modalWidth = 56
	}

	var content strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(PrimaryColor)
	content.WriteString(titleStyle.Render("Box setup"))
	content.WriteString("\n")
	content.WriteString(dividerLine(modalWidth))
	content.WriteString("\n\n")

	if m.step == stepLocation {
		m.renderLocations(&content)
	} else {
		m.renderTypes(&content)
	}

	content.WriteString("\n")
	content.WriteString(dividerLine(modalWidth))
	content.WriteString("\n")
	footer := lipgloss.NewStyle().Foreground(MutedColor)
	if m.step == stepLocation {
		content.WriteString(footer.Render("↑/↓: Navigate  Enter: Continue  Esc: Cancel"))
	} else {
		content.WriteString(footer.Render("↑/↓: Navigate  Enter: Save  Esc: Back"))
	}

	return centerModal(modalBoxStyle(PrimaryColor).Width(modalWidth).Render(content.String()), m.width, m.height)
}

func (m BoxSetup) renderLocations(content *strings.Builder) {
	muted := lipgloss.NewStyle().Foreground(MutedColor)
	text := lipgloss.NewStyle().Foreground(TextColor)
	selected := lipgloss.NewStyle().Foreground(PrimaryColor).Bold(true)

	content.WriteString(muted.Render("Where should the box run? It holds your source and your .env"))
	content.WriteString("\n")
	content.WriteString(muted.Render("for the length of the run."))
	content.WriteString("\n\n")

	for i, l := range m.catalog.Locations {
		row := fmt.Sprintf("%-6s %s, %s", l.Name, l.City, l.Country)
		if i == m.locationIndex {
			content.WriteString(selected.Render("▶ " + row))
		} else {
			content.WriteString(text.Render("  " + row))
		}
		if cheapest := m.catalog.TypesIn(l.Name); len(cheapest) > 0 {
			content.WriteString(muted.Render(fmt.Sprintf("   from %s", money(cheapest[0].HourlyEUR))))
		}
		content.WriteString("\n")
	}
}

func (m BoxSetup) renderTypes(content *strings.Builder) {
	muted := lipgloss.NewStyle().Foreground(MutedColor)
	text := lipgloss.NewStyle().Foreground(TextColor)
	selected := lipgloss.NewStyle().Foreground(PrimaryColor).Bold(true)
	warn := lipgloss.NewStyle().Foreground(WarningColor)

	l := m.currentLocation()
	content.WriteString(muted.Render(fmt.Sprintf("How big a machine, in %s, %s?", l.City, l.Country)))
	content.WriteString("\n")
	content.WriteString(muted.Render(fmt.Sprintf("The right-hand column is what a %d-hour run costs.", estimateHours)))
	content.WriteString("\n\n")

	types := m.currentTypes()
	end := min(m.typeOffset+maxVisibleRows, len(types))
	for i := m.typeOffset; i < end; i++ {
		t := types[i]
		specs := fmt.Sprintf("%d vCPU · %.0f GB · %d GB", t.Cores, t.Memory, t.Disk)
		row := fmt.Sprintf("%-8s %-26s", t.Name, specs)
		if i == m.typeIndex {
			content.WriteString(selected.Render("▶ " + row))
		} else {
			content.WriteString(text.Render("  " + row))
		}
		content.WriteString(muted.Render(fmt.Sprintf("%10s", money(t.Estimate(estimateHours)))))
		if t.Deprecated {
			// Worth saying out loud: a deprecated type creates today and stops
			// being creatable without notice, which surfaces as a box that
			// cannot be made on the morning somebody needs one.
			content.WriteString(warn.Render("  being retired"))
		}
		content.WriteString("\n")
	}
	if len(types) > end {
		content.WriteString(muted.Render(fmt.Sprintf("  … %d more\n", len(types)-end)))
	}
}

// money formats an amount in euros, in cents when it is small enough that euros
// would round it to nothing. "€0.00" next to a machine somebody is choosing is
// worse than no number at all.
func money(eur float64) string { return box.FormatRateEUR(eur) }

// RunBoxSetup shows the picker and returns the chosen location and type.
func RunBoxSetup(catalog box.Catalog, currentLocation, currentType string) (location, serverType string, cancelled bool, err error) {
	p := tea.NewProgram(NewBoxSetup(catalog, currentLocation, currentType), tea.WithAltScreen())
	out, runErr := p.Run()
	if runErr != nil {
		return "", "", true, runErr
	}
	final, ok := out.(BoxSetup)
	if !ok || final.cancelled {
		return "", "", true, nil
	}
	return final.location, final.typeName, false, nil
}
