package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// modelOption is one entry in the model select. An empty Value means "use the
// Claude CLI's own default" (no --model flag is passed). The customValue
// sentinel switches the picker into a free-text entry step so any full model ID
// can be typed.
type modelOption struct {
	label string
	value string
	desc  string
}

// customValue is the sentinel Value that marks the "Custom…" entry.
const customValue = "\x00custom"

// modelOptions is the curated list of stable Claude aliases plus a custom entry.
// The Claude CLI has no command to enumerate models, so the aliases are fixed
// (they rarely change) and any newer/specific model is reachable via "Custom…".
var modelOptions = []modelOption{
	{label: "Default", value: "", desc: "Use the model from your Claude configuration"},
	{label: "Opus", value: "opus", desc: "Most capable, slowest"},
	{label: "Sonnet", value: "sonnet", desc: "Balanced capability and speed"},
	{label: "Haiku", value: "haiku", desc: "Fastest, lightest"},
	{label: "Fable", value: "fable", desc: "Claude 5 family"},
	{label: "Custom…", value: customValue, desc: "Enter a specific model ID (e.g. claude-opus-4-8)"},
}

// ModelSelect is a small TUI that lets the user pick which Claude model to use
// for interactive PRD creation/editing.
type ModelSelect struct {
	width  int
	height int

	title    string // e.g. "Create PRD" — shown above the picker
	selected int    // index into modelOptions

	customMode  bool   // true while entering a custom model ID
	customInput string // buffer for the custom model ID

	result    string // chosen model ("" = default)
	cancelled bool
}

// NewModelSelect creates the picker. current pre-selects the matching option
// (an alias pre-selects its row; any other non-empty value pre-fills Custom…).
func NewModelSelect(title, current string) *ModelSelect {
	m := &ModelSelect{title: title}
	matched := false
	for i, opt := range modelOptions {
		if opt.value == current && opt.value != customValue {
			m.selected = i
			matched = true
			break
		}
	}
	if !matched && current != "" {
		// A specific model ID that isn't one of the aliases: point at Custom…
		// and pre-fill it.
		for i, opt := range modelOptions {
			if opt.value == customValue {
				m.selected = i
				break
			}
		}
		m.customInput = current
	}
	return m
}

// Init implements tea.Model.
func (m ModelSelect) Init() tea.Cmd { return tea.EnterAltScreen }

// Update implements tea.Model.
func (m ModelSelect) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.customMode {
			return m.handleCustomKeys(msg)
		}
		return m.handleListKeys(msg)
	}
	return m, nil
}

func (m ModelSelect) handleListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c", "esc":
		m.cancelled = true
		return m, tea.Quit

	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
		return m, nil

	case "down", "j":
		if m.selected < len(modelOptions)-1 {
			m.selected++
		}
		return m, nil

	case "enter":
		opt := modelOptions[m.selected]
		if opt.value == customValue {
			m.customMode = true
			return m, nil
		}
		m.result = opt.value
		return m, tea.Quit
	}
	return m, nil
}

func (m ModelSelect) handleCustomKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.cancelled = true
		return m, tea.Quit

	case "esc":
		// Back to the list without losing the typed value.
		m.customMode = false
		return m, nil

	case "enter":
		trimmed := strings.TrimSpace(m.customInput)
		if trimmed == "" {
			// Empty custom entry behaves like Default.
			m.result = ""
		} else {
			m.result = trimmed
		}
		return m, tea.Quit

	case "backspace":
		if len(m.customInput) > 0 {
			m.customInput = m.customInput[:len(m.customInput)-1]
		}
		return m, nil

	default:
		if len(msg.String()) == 1 {
			m.customInput += msg.String()
		}
		return m, nil
	}
}

// View implements tea.Model.
func (m ModelSelect) View() string {
	modalWidth := min(64, m.width-10)
	if modalWidth < 48 {
		modalWidth = 48
	}

	var content strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(PrimaryColor)
	heading := "Select Claude model"
	if m.title != "" {
		heading = fmt.Sprintf("Select Claude model — %s", m.title)
	}
	content.WriteString(titleStyle.Render(heading))
	content.WriteString("\n")
	content.WriteString(dividerLine(modalWidth))
	content.WriteString("\n\n")

	if m.customMode {
		m.renderCustom(&content, modalWidth)
	} else {
		m.renderList(&content, modalWidth)
	}

	modalStyle := modalBoxStyle(PrimaryColor).Width(modalWidth)

	return centerModal(modalStyle.Render(content.String()), m.width, m.height)
}

func (m ModelSelect) renderList(content *strings.Builder, modalWidth int) {
	descStyle := lipgloss.NewStyle().Foreground(MutedColor)
	content.WriteString(descStyle.Render("Which model should drive this PRD session?"))
	content.WriteString("\n\n")

	optionStyle := lipgloss.NewStyle().Foreground(TextColor)
	selectedStyle := lipgloss.NewStyle().Foreground(PrimaryColor).Bold(true)

	for i, opt := range modelOptions {
		if i == m.selected {
			content.WriteString(selectedStyle.Render("▶ " + opt.label))
		} else {
			content.WriteString(optionStyle.Render("  " + opt.label))
		}
		if opt.desc != "" {
			content.WriteString("  ")
			content.WriteString(descStyle.Render(opt.desc))
		}
		content.WriteString("\n")
	}

	content.WriteString("\n")
	content.WriteString(dividerLine(modalWidth))
	content.WriteString("\n")
	footerStyle := lipgloss.NewStyle().Foreground(MutedColor)
	content.WriteString(footerStyle.Render("↑/↓: Navigate  Enter: Select  Esc: Cancel"))
}

func (m ModelSelect) renderCustom(content *strings.Builder, modalWidth int) {
	messageStyle := lipgloss.NewStyle().Foreground(TextColor)
	content.WriteString(messageStyle.Render("Enter a model ID:"))
	content.WriteString("\n\n")

	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(PrimaryColor).
		Padding(0, 1).
		Width(modalWidth - 8)
	content.WriteString(inputStyle.Render(m.customInput + "█"))
	content.WriteString("\n")

	hintStyle := lipgloss.NewStyle().Foreground(MutedColor)
	content.WriteString(hintStyle.Render("e.g. claude-opus-4-8, or an alias like sonnet"))

	content.WriteString("\n\n")
	content.WriteString(dividerLine(modalWidth))
	content.WriteString("\n")
	footerStyle := lipgloss.NewStyle().Foreground(MutedColor)
	content.WriteString(footerStyle.Render("Enter: Confirm  Esc: Back  Ctrl+C: Cancel"))
}

// RunModelSelect runs the picker and returns the chosen model ("" = use the
// CLI's default) and whether the user cancelled. title labels the flow (e.g.
// "Create PRD" or "Edit PRD"); current pre-selects a matching option.
func RunModelSelect(title, current string) (model string, cancelled bool, err error) {
	sel := NewModelSelect(title, current)
	p := tea.NewProgram(sel, tea.WithAltScreen())
	out, runErr := p.Run()
	if runErr != nil {
		return "", true, runErr
	}
	if final, ok := out.(ModelSelect); ok {
		return final.result, final.cancelled, nil
	}
	return "", true, nil
}
