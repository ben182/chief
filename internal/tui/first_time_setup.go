package tui

import (
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FirstTimeSetupResult contains the result of the first-time setup flow.
type FirstTimeSetupResult struct {
	PRDName   string
	Cancelled bool
}

// FirstTimeSetup asks for the name of a project's first PRD. The project's
// settings are asked before it, by 'chief setup'.
type FirstTimeSetup struct {
	width  int
	height int

	prdName      string
	prdNameError string

	result FirstTimeSetupResult

	baseDir string
}

// NewFirstTimeSetup creates a new first-time setup TUI.
func NewFirstTimeSetup(baseDir string) *FirstTimeSetup {
	return &FirstTimeSetup{
		baseDir: baseDir,
		prdName: "default",
	}
}

// Init initializes the model.
func (f FirstTimeSetup) Init() tea.Cmd {
	return tea.EnterAltScreen
}

// Update handles messages.
func (f FirstTimeSetup) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		f.width = msg.Width
		f.height = msg.Height
		return f, nil

	case tea.KeyMsg:
		return f.handlePRDNameKeys(msg)
	}
	return f, nil
}

func (f FirstTimeSetup) handlePRDNameKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		f.result.Cancelled = true
		return f, tea.Quit

	case "esc":
		f.result.Cancelled = true
		return f, tea.Quit

	case "enter":
		// Validate PRD name
		name := strings.TrimSpace(f.prdName)
		if name == "" {
			f.prdNameError = "Name cannot be empty"
			return f, nil
		}
		if !isValidPRDName(name) {
			f.prdNameError = "Name can only contain letters, numbers, hyphens, and underscores"
			return f, nil
		}
		f.result.PRDName = name
		return f, tea.Quit

	case "backspace":
		if len(f.prdName) > 0 {
			f.prdName = f.prdName[:len(f.prdName)-1]
			f.prdNameError = ""
		}
		return f, nil

	default:
		// Handle character input
		if len(msg.String()) == 1 {
			r := rune(msg.String()[0])
			// Only allow valid characters
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9') || r == '-' || r == '_' {
				f.prdName += string(r)
				f.prdNameError = ""
			}
		}
		return f, nil
	}
}

// prdNameRegex matches valid PRD names (letters, digits, hyphens, underscores).
// Compiled once at package init because isValidPRDName runs on every keystroke.
var prdNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// isValidPRDName checks if a name is valid for a PRD.
func isValidPRDName(name string) bool {
	return prdNameRegex.MatchString(name)
}

// View renders the TUI.
func (f FirstTimeSetup) View() string {
	return f.renderPRDNameStep()
}

func (f FirstTimeSetup) renderPRDNameStep() string {
	modalWidth := min(60, f.width-10)
	if modalWidth < 45 {
		modalWidth = 45
	}

	var content strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(PrimaryColor)

	content.WriteString(titleStyle.Render("Create Your First PRD"))
	content.WriteString("\n")
	content.WriteString(dividerLine(modalWidth))
	content.WriteString("\n\n")

	// Message
	messageStyle := lipgloss.NewStyle().Foreground(TextColor)
	content.WriteString(messageStyle.Render("Enter a name for your PRD:"))
	content.WriteString("\n\n")

	// Input field
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(PrimaryColor).
		Padding(0, 1).
		Width(modalWidth - 8)

	displayName := f.prdName
	if displayName == "" {
		displayName = " " // Show cursor position
	}
	content.WriteString(inputStyle.Render(displayName + "█"))
	content.WriteString("\n")

	// Error message
	if f.prdNameError != "" {
		errorStyle := lipgloss.NewStyle().Foreground(ErrorColor)
		content.WriteString("\n")
		content.WriteString(errorStyle.Render(f.prdNameError))
	}

	// Hint
	content.WriteString("\n")
	hintStyle := lipgloss.NewStyle().Foreground(MutedColor)
	content.WriteString(hintStyle.Render("PRD will be created at: .chief/prds/" + f.prdName + "/"))

	// Footer
	content.WriteString("\n\n")
	content.WriteString(dividerLine(modalWidth))
	content.WriteString("\n")

	footerStyle := lipgloss.NewStyle().Foreground(MutedColor)
	content.WriteString(footerStyle.Render("Enter: Create PRD  Esc/Ctrl+C: Cancel"))

	// Modal box
	modalStyle := modalBoxStyle(PrimaryColor).Width(modalWidth)

	modal := modalStyle.Render(content.String())

	return centerModal(modal, f.width, f.height)
}

// GetResult returns the setup result.
func (f FirstTimeSetup) GetResult() FirstTimeSetupResult {
	return f.result
}

// RunFirstTimeSetup runs the first-time setup TUI and returns the result.
func RunFirstTimeSetup(baseDir string) (FirstTimeSetupResult, error) {
	setup := NewFirstTimeSetup(baseDir)
	p := tea.NewProgram(setup, tea.WithAltScreen())

	model, err := p.Run()
	if err != nil {
		return FirstTimeSetupResult{Cancelled: true}, err
	}

	if finalSetup, ok := model.(FirstTimeSetup); ok {
		return finalSetup.GetResult(), nil
	}

	return FirstTimeSetupResult{Cancelled: true}, nil
}
