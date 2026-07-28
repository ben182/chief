package tui

import (
	"strconv"
	"strings"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/loop"
	"github.com/charmbracelet/lipgloss"
)

// SettingsItemType represents the type of a settings item.
type SettingsItemType int

const (
	SettingsItemBool SettingsItemType = iota
	SettingsItemString
	// SettingsItemTriBool edits a `*bool` config switch, where nil is a third,
	// meaningful state rather than a missing value: the review and consolidation
	// passes fall back to deriving their on/off from whether a skill or
	// instructions are configured. Rendering those as a plain Yes/No would erase
	// that state the first time anything in the overlay was saved.
	SettingsItemTriBool
	// SettingsItemInt edits a whole number. It shares the inline text editor with
	// SettingsItemString but only accepts digits, and treats 0 as "unset".
	SettingsItemInt
	// SettingsItemEnum picks one of a fixed set of strings by cycling through
	// them. Used where free text would let a typo through that only surfaces as a
	// failed run — the agent provider is resolved against a closed list.
	SettingsItemEnum
)

// SettingsItem represents a single editable setting.
type SettingsItem struct {
	Section   string
	Label     string
	Key       string // config key for identification
	Type      SettingsItemType
	BoolVal   bool
	StringVal string
	IntVal    int
	TriVal    *bool
	// Options are the selectable values of a SettingsItemEnum, in cycle order.
	// The empty string is prepended implicitly, so "leave it to chief" stays
	// reachable.
	Options []string
	// Placeholder is what the value column shows when the setting is unset, for
	// settings where empty does not mean "off" but "fall back to a default worth
	// naming" (e.g. an empty review.model runs on Sonnet). Defaults to "(not set)".
	Placeholder string
}

// SettingsOverlay manages the settings modal overlay state.
type SettingsOverlay struct {
	width  int
	height int

	items         []SettingsItem
	selectedIndex int

	// scrollOffset is the first visible row of the item list. The full list is
	// taller than the modal on a normal terminal, so Render keeps this just large
	// enough to hold the selected row in view.
	scrollOffset int

	// Inline text editing
	editing    bool
	editBuffer string

	// GH CLI validation error
	ghError     string
	showGHError bool
}

// NewSettingsOverlay creates a new settings overlay.
func NewSettingsOverlay() *SettingsOverlay {
	return &SettingsOverlay{}
}

// SetSize sets the overlay dimensions.
func (s *SettingsOverlay) SetSize(width, height int) {
	s.width = width
	s.height = height
}

// agentProviders is the closed set of provider names agent.Resolve accepts, in
// the order the Provider setting cycles through them. Empty (the implicit first
// value) means "claude".
var agentProviders = []string{"claude", "codex", "opencode", "cursor", "gemini"}

// copyTri returns a copy of a tri-state switch, so editing the overlay never
// writes through the pointer the config still holds.
func copyTri(b *bool) *bool {
	if b == nil {
		return nil
	}
	v := *b
	return &v
}

// LoadFromConfig populates settings items from a config.
//
// Every key in config.Config is represented here. The overlay used to carry a
// hand-picked subset, which meant each new config key silently became reachable
// only by editing the YAML by hand.
func (s *SettingsOverlay) LoadFromConfig(cfg *config.Config) {
	s.items = []SettingsItem{
		{Section: "Worktree", Label: "Setup command", Key: "worktree.setup", Type: SettingsItemString, StringVal: cfg.Worktree.Setup},
		{Section: "On Complete", Label: "Push to remote", Key: "onComplete.push", Type: SettingsItemBool, BoolVal: cfg.OnComplete.Push},
		{Section: "On Complete", Label: "Create pull request", Key: "onComplete.createPR", Type: SettingsItemBool, BoolVal: cfg.OnComplete.CreatePR},
		{Section: "On Complete", Label: "PR base branch", Key: "onComplete.prBaseBranch", Type: SettingsItemString, StringVal: cfg.OnComplete.PRBaseBranch, Placeholder: "(branch it came from)"},
		{Section: "On Complete", Label: "Write run summary", Key: "onComplete.summary", Type: SettingsItemBool, BoolVal: cfg.OnComplete.Summary},
		{Section: "On Complete", Label: "Desktop notification", Key: "onComplete.notify", Type: SettingsItemBool, BoolVal: cfg.OnComplete.Notify},
		{Section: "Loop", Label: "Keep machine awake", Key: "loop.keepAwake", Type: SettingsItemBool, BoolVal: cfg.Loop.KeepAwake},
		{Section: "Loop", Label: "Watchdog timeout (s)", Key: "loop.watchdogTimeoutSeconds", Type: SettingsItemInt, IntVal: cfg.Loop.WatchdogTimeoutSeconds, Placeholder: defaultWatchdogLabel()},
		{Section: "Agent", Label: "Provider", Key: "agent.provider", Type: SettingsItemEnum, StringVal: cfg.Agent.Provider, Options: agentProviders, Placeholder: "claude (default)"},
		{Section: "Agent", Label: "CLI path", Key: "agent.cliPath", Type: SettingsItemString, StringVal: cfg.Agent.CLIPath, Placeholder: "(found in PATH)"},
		{Section: "Agent", Label: "Model", Key: "agent.model", Type: SettingsItemString, StringVal: cfg.Agent.Model, Placeholder: "(CLI default)"},
		{Section: "Review", Label: "Enabled", Key: "review.enabled", Type: SettingsItemTriBool, TriVal: copyTri(cfg.Review.Enabled)},
		{Section: "Review", Label: "Model", Key: "review.model", Type: SettingsItemString, StringVal: cfg.Review.Model, Placeholder: "sonnet (default)"},
		{Section: "Review", Label: "Skill", Key: "review.skill", Type: SettingsItemString, StringVal: cfg.Review.Skill},
		{Section: "Review", Label: "Instructions", Key: "review.instructions", Type: SettingsItemString, StringVal: cfg.Review.Instructions},
		{Section: "Consolidate", Label: "Enabled", Key: "consolidate.enabled", Type: SettingsItemTriBool, TriVal: copyTri(cfg.Consolidate.Enabled)},
		{Section: "Consolidate", Label: "Model", Key: "consolidate.model", Type: SettingsItemString, StringVal: cfg.Consolidate.Model, Placeholder: "sonnet (default)"},
		{Section: "Consolidate", Label: "Skill", Key: "consolidate.skill", Type: SettingsItemString, StringVal: cfg.Consolidate.Skill},
		{Section: "Consolidate", Label: "Instructions", Key: "consolidate.instructions", Type: SettingsItemString, StringVal: cfg.Consolidate.Instructions},
	}
	s.selectedIndex = 0
	s.scrollOffset = 0
	s.editing = false
	s.editBuffer = ""
	s.ghError = ""
	s.showGHError = false
}

// defaultWatchdogLabel names the built-in watchdog timeout that a zero value
// falls back to, so the value column reads "300 (default)" rather than "(not set)".
func defaultWatchdogLabel() string {
	return strconv.Itoa(int(loop.DefaultWatchdogTimeout.Seconds())) + " (default)"
}

// ApplyToConfig writes the current settings values back to a config.
func (s *SettingsOverlay) ApplyToConfig(cfg *config.Config) {
	for _, item := range s.items {
		switch item.Key {
		case "worktree.setup":
			cfg.Worktree.Setup = item.StringVal
		case "onComplete.push":
			cfg.OnComplete.Push = item.BoolVal
		case "onComplete.createPR":
			cfg.OnComplete.CreatePR = item.BoolVal
		case "onComplete.prBaseBranch":
			cfg.OnComplete.PRBaseBranch = item.StringVal
		case "onComplete.summary":
			cfg.OnComplete.Summary = item.BoolVal
		case "onComplete.notify":
			cfg.OnComplete.Notify = item.BoolVal
		case "loop.keepAwake":
			cfg.Loop.KeepAwake = item.BoolVal
		case "loop.watchdogTimeoutSeconds":
			cfg.Loop.WatchdogTimeoutSeconds = item.IntVal
		case "agent.provider":
			cfg.Agent.Provider = item.StringVal
		case "agent.cliPath":
			cfg.Agent.CLIPath = item.StringVal
		case "agent.model":
			cfg.Agent.Model = item.StringVal
		case "review.enabled":
			cfg.Review.Enabled = copyTri(item.TriVal)
		case "review.model":
			cfg.Review.Model = item.StringVal
		case "review.skill":
			cfg.Review.Skill = item.StringVal
		case "review.instructions":
			cfg.Review.Instructions = item.StringVal
		case "consolidate.enabled":
			cfg.Consolidate.Enabled = copyTri(item.TriVal)
		case "consolidate.model":
			cfg.Consolidate.Model = item.StringVal
		case "consolidate.skill":
			cfg.Consolidate.Skill = item.StringVal
		case "consolidate.instructions":
			cfg.Consolidate.Instructions = item.StringVal
		}
	}
}

// MoveUp moves the selection up.
func (s *SettingsOverlay) MoveUp() {
	if s.selectedIndex > 0 {
		s.selectedIndex--
	}
}

// MoveDown moves the selection down.
func (s *SettingsOverlay) MoveDown() {
	if s.selectedIndex < len(s.items)-1 {
		s.selectedIndex++
	}
}

// IsEditing returns true if a string value is being edited.
func (s *SettingsOverlay) IsEditing() bool {
	return s.editing
}

// StartEditing begins inline editing of the selected string or int value.
func (s *SettingsOverlay) StartEditing() {
	if s.selectedIndex >= len(s.items) {
		return
	}
	switch s.items[s.selectedIndex].Type {
	case SettingsItemString:
		s.editing = true
		s.editBuffer = s.items[s.selectedIndex].StringVal
	case SettingsItemInt:
		s.editing = true
		s.editBuffer = ""
		if v := s.items[s.selectedIndex].IntVal; v > 0 {
			s.editBuffer = strconv.Itoa(v)
		}
	}
}

// ConfirmEdit saves the edit buffer to the selected item. An int buffer that
// does not parse (only possible when it is empty, since AddEditChar rejects
// non-digits) clears the setting back to its built-in default.
func (s *SettingsOverlay) ConfirmEdit() {
	if !s.editing || s.selectedIndex >= len(s.items) {
		return
	}
	item := &s.items[s.selectedIndex]
	switch item.Type {
	case SettingsItemInt:
		n, err := strconv.Atoi(strings.TrimSpace(s.editBuffer))
		if err != nil || n < 0 {
			n = 0
		}
		item.IntVal = n
	default:
		item.StringVal = s.editBuffer
	}
	s.editing = false
	s.editBuffer = ""
}

// CancelEdit discards the edit buffer.
func (s *SettingsOverlay) CancelEdit() {
	s.editing = false
	s.editBuffer = ""
}

// AddEditChar adds a character to the edit buffer. Int settings ignore anything
// that is not a digit, so the buffer always parses.
func (s *SettingsOverlay) AddEditChar(ch rune) {
	if s.selectedIndex < len(s.items) && s.items[s.selectedIndex].Type == SettingsItemInt {
		if ch < '0' || ch > '9' {
			return
		}
	}
	s.editBuffer += string(ch)
}

// DeleteEditChar removes the last character from the edit buffer.
func (s *SettingsOverlay) DeleteEditChar() {
	if len(s.editBuffer) > 0 {
		runes := []rune(s.editBuffer)
		s.editBuffer = string(runes[:len(runes)-1])
	}
}

// ToggleBool toggles the selected boolean value.
// Returns the key and new value for the caller to act on.
func (s *SettingsOverlay) ToggleBool() (key string, newVal bool) {
	if s.selectedIndex < len(s.items) && s.items[s.selectedIndex].Type == SettingsItemBool {
		s.items[s.selectedIndex].BoolVal = !s.items[s.selectedIndex].BoolVal
		return s.items[s.selectedIndex].Key, s.items[s.selectedIndex].BoolVal
	}
	return "", false
}

// CycleTriBool advances the selected tri-state switch through
// default → yes → no → default, so the derived state stays reachable instead of
// being lost the moment the setting is touched once.
func (s *SettingsOverlay) CycleTriBool() {
	if s.selectedIndex >= len(s.items) || s.items[s.selectedIndex].Type != SettingsItemTriBool {
		return
	}
	item := &s.items[s.selectedIndex]
	switch {
	case item.TriVal == nil:
		item.TriVal = config.Bool(true)
	case *item.TriVal:
		item.TriVal = config.Bool(false)
	default:
		item.TriVal = nil
	}
}

// CycleEnum advances the selected enum setting to its next option, wrapping back
// round to the empty "use the default" value after the last one.
func (s *SettingsOverlay) CycleEnum() {
	if s.selectedIndex >= len(s.items) || s.items[s.selectedIndex].Type != SettingsItemEnum {
		return
	}
	item := &s.items[s.selectedIndex]
	for i, opt := range item.Options {
		if opt == item.StringVal {
			if i+1 < len(item.Options) {
				item.StringVal = item.Options[i+1]
			} else {
				item.StringVal = ""
			}
			return
		}
	}
	// Unset, or a value not in the list (hand-edited YAML): start over at the top.
	if len(item.Options) > 0 {
		item.StringVal = item.Options[0]
	}
}

// RevertToggle reverts the last toggle (used when validation fails).
func (s *SettingsOverlay) RevertToggle() {
	if s.selectedIndex < len(s.items) && s.items[s.selectedIndex].Type == SettingsItemBool {
		s.items[s.selectedIndex].BoolVal = !s.items[s.selectedIndex].BoolVal
	}
}

// SetGHError sets the GH CLI error message.
func (s *SettingsOverlay) SetGHError(msg string) {
	s.ghError = msg
	s.showGHError = true
}

// HasGHError returns true if a GH CLI error is being displayed.
func (s *SettingsOverlay) HasGHError() bool {
	return s.showGHError
}

// DismissGHError clears the GH CLI error.
func (s *SettingsOverlay) DismissGHError() {
	s.showGHError = false
	s.ghError = ""
}

// GetSelectedItem returns the currently selected settings item.
func (s *SettingsOverlay) GetSelectedItem() *SettingsItem {
	if s.selectedIndex >= 0 && s.selectedIndex < len(s.items) {
		return &s.items[s.selectedIndex]
	}
	return nil
}

// derivedActive mirrors config.ReviewConfig.Active / ConsolidateConfig.Active for
// a tri-state switch left on "default": the pass is on when a skill or
// instructions are configured. It reads the sibling items rather than the config
// so the label updates as soon as those fields are edited in the overlay.
func (s *SettingsOverlay) derivedActive(prefix string) bool {
	for _, it := range s.items {
		if it.Key == prefix+".skill" || it.Key == prefix+".instructions" {
			if strings.TrimSpace(it.StringVal) != "" {
				return true
			}
		}
	}
	return false
}

// settingsRowKind distinguishes the three kinds of line the item list renders.
type settingsRowKind int

const (
	settingsRowItem settingsRowKind = iota
	settingsRowSection
	settingsRowBlank
)

// settingsRow is one rendered line of the item list. Building the list as flat
// rows first is what makes scrolling straightforward: section headers and the
// blank lines between sections scroll along with the items they belong to.
type settingsRow struct {
	kind      settingsRowKind
	section   string
	itemIndex int
}

// buildRows flattens the items into renderable lines, inserting a section header
// before each new section and a blank line between sections.
func (s *SettingsOverlay) buildRows() []settingsRow {
	rows := make([]settingsRow, 0, len(s.items)+8)
	currentSection := ""
	for i, item := range s.items {
		if item.Section != currentSection {
			if currentSection != "" {
				rows = append(rows, settingsRow{kind: settingsRowBlank})
			}
			rows = append(rows, settingsRow{kind: settingsRowSection, section: item.Section})
			currentSection = item.Section
		}
		rows = append(rows, settingsRow{kind: settingsRowItem, itemIndex: i})
	}
	return rows
}

// Render renders the settings overlay.
func (s *SettingsOverlay) Render() string {
	rows := s.buildRows()

	// chromeHeight is everything the modal spends on something other than the item
	// list: title, divider, blank, then blank, divider, footer.
	const chromeHeight = 6

	modalWidth := min(60, s.width-10)
	// Grow the modal to fit the whole list when the terminal is tall enough, and
	// scroll the list when it is not. The error dialog replaces the list, so it
	// keeps the small fixed box instead of inheriting the list's height.
	contentHeight := len(rows)
	if s.showGHError {
		contentHeight = ghErrorContentHeight
	}
	modalHeight := min(contentHeight+chromeHeight, s.height-6)

	if modalWidth < 40 {
		modalWidth = 40
	}
	if modalHeight < 12 {
		modalHeight = 12
	}

	var content strings.Builder

	// Header: "Settings" left-aligned, ".chief/config.yaml" right-aligned
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(PrimaryColor)
	pathStyle := lipgloss.NewStyle().
		Foreground(MutedColor)

	title := titleStyle.Render("Settings")
	path := pathStyle.Render(".chief/config.yaml")
	titleWidth := lipgloss.Width(title)
	pathWidth := lipgloss.Width(path)
	titlePadding := modalWidth - 4 - titleWidth - pathWidth
	if titlePadding < 1 {
		titlePadding = 1
	}
	content.WriteString(" ")
	content.WriteString(title)
	content.WriteString(strings.Repeat(" ", titlePadding))
	content.WriteString(path)
	content.WriteString("\n")
	content.WriteString(dividerLine(modalWidth))
	content.WriteString("\n\n")

	// GH error dialog overlay
	if s.showGHError {
		content.WriteString(s.renderGHError(modalWidth))
	} else {
		// Render settings items grouped by section
		content.WriteString(s.renderItems(modalWidth, modalHeight-chromeHeight, rows))
	}

	// Footer
	content.WriteString("\n")
	content.WriteString(dividerLine(modalWidth))
	content.WriteString("\n")

	footerStyle := lipgloss.NewStyle().
		Foreground(MutedColor).
		Padding(0, 1)

	if s.showGHError {
		content.WriteString(footerStyle.Render("Press any key to dismiss"))
	} else if s.editing {
		content.WriteString(footerStyle.Render("Enter: save  │  Esc: cancel"))
	} else {
		content.WriteString(footerStyle.Render("Enter: toggle/edit  │  j/k: navigate  │  Esc: close"))
	}

	// Modal box style
	modalStyle := modalBoxStyle(PrimaryColor).Width(modalWidth).Height(modalHeight)

	modal := modalStyle.Render(content.String())

	return centerModal(modal, s.width, s.height)
}

// scrollRowsIntoView returns the window of rows to draw, moving scrollOffset by
// the smallest amount that keeps the selected item's row visible. When the list
// is clipped, the boundary line is given up to an ellipsis marker so it is
// obvious there is more above or below.
func (s *SettingsOverlay) scrollRowsIntoView(rows []settingsRow, visible int) (start, end int, moreAbove, moreBelow bool) {
	if visible >= len(rows) {
		s.scrollOffset = 0
		return 0, len(rows), false, false
	}

	selectedRow := 0
	for i, r := range rows {
		if r.kind == settingsRowItem && r.itemIndex == s.selectedIndex {
			selectedRow = i
			break
		}
	}

	// A clipped list spends one line on each visible ellipsis marker, so the
	// window that actually holds rows is smaller at the edges. Reserve both up
	// front: the selection can otherwise land exactly on a marker line.
	if s.scrollOffset > len(rows)-visible {
		s.scrollOffset = len(rows) - visible
	}
	if s.scrollOffset < 0 {
		s.scrollOffset = 0
	}

	topSlack, bottomSlack := 0, 0
	for i := 0; i < 2; i++ {
		if s.scrollOffset > 0 {
			topSlack = 1
		}
		if s.scrollOffset+visible < len(rows) {
			bottomSlack = 1
		}
		if selectedRow < s.scrollOffset+topSlack {
			s.scrollOffset = selectedRow - topSlack
		}
		if selectedRow >= s.scrollOffset+visible-bottomSlack {
			s.scrollOffset = selectedRow - visible + bottomSlack + 1
		}
		if s.scrollOffset > len(rows)-visible {
			s.scrollOffset = len(rows) - visible
		}
		if s.scrollOffset < 0 {
			s.scrollOffset = 0
		}
	}

	start = s.scrollOffset
	end = start + visible
	if end > len(rows) {
		end = len(rows)
	}
	return start, end, start > 0, end < len(rows)
}

// renderItems renders the visible slice of the settings items, grouped by section.
func (s *SettingsOverlay) renderItems(modalWidth, visible int, rows []settingsRow) string {
	if visible < 1 {
		visible = 1
	}

	var result strings.Builder

	sectionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(PrimaryColor).
		Padding(0, 1)
	mutedStyle := lipgloss.NewStyle().
		Foreground(MutedColor)

	start, end, moreAbove, moreBelow := s.scrollRowsIntoView(rows, visible)

	for i := start; i < end; i++ {
		if i == start && moreAbove {
			result.WriteString(mutedStyle.Render("    ⋯"))
			result.WriteString("\n")
			continue
		}
		if i == end-1 && moreBelow {
			result.WriteString(mutedStyle.Render("    ⋯"))
			result.WriteString("\n")
			continue
		}

		switch rows[i].kind {
		case settingsRowBlank:
			result.WriteString("\n")
		case settingsRowSection:
			result.WriteString(sectionStyle.Render(rows[i].section))
			result.WriteString("\n")
		case settingsRowItem:
			result.WriteString(s.renderItemLine(modalWidth, rows[i].itemIndex))
			result.WriteString("\n")
		}
	}

	return result.String()
}

// renderItemLine renders one setting as a label with its value right-aligned.
func (s *SettingsOverlay) renderItemLine(modalWidth, index int) string {
	item := s.items[index]
	isSelected := index == s.selectedIndex

	labelStyle := lipgloss.NewStyle().
		Foreground(TextColor)
	selectedLabelStyle := lipgloss.NewStyle().
		Foreground(TextBrightColor).
		Bold(true)
	cursorStyle := lipgloss.NewStyle().
		Foreground(PrimaryColor).
		Bold(true)

	var line strings.Builder

	// Cursor
	if isSelected {
		line.WriteString(cursorStyle.Render("  > "))
	} else {
		line.WriteString("    ")
	}

	// Label
	label := item.Label
	if isSelected {
		line.WriteString(selectedLabelStyle.Render(label))
	} else {
		line.WriteString(labelStyle.Render(label))
	}

	// The value column gets whatever the label leaves over: 4 border/padding
	// cells, the 4-cell cursor prefix, the label itself, and 4 cells of gutter.
	maxValWidth := modalWidth - 4 - 4 - lipgloss.Width(label) - 4
	if maxValWidth < 10 {
		maxValWidth = 10
	}

	valueStr := s.renderItemValue(item, isSelected, maxValWidth)

	// Calculate padding between label and value
	labelWidth := lipgloss.Width(label) + 4 // cursor prefix
	valWidth := lipgloss.Width(valueStr)
	padding := modalWidth - 4 - labelWidth - valWidth - 2
	if padding < 2 {
		padding = 2
	}
	line.WriteString(strings.Repeat(" ", padding))
	line.WriteString(valueStr)

	return line.String()
}

// renderItemValue renders the value column of a single setting.
func (s *SettingsOverlay) renderItemValue(item SettingsItem, isSelected bool, maxValWidth int) string {
	valueStyle := lipgloss.NewStyle().
		Foreground(SuccessColor)
	valueOffStyle := lipgloss.NewStyle().
		Foreground(MutedColor)

	if isSelected && s.editing && (item.Type == SettingsItemString || item.Type == SettingsItemInt) {
		editStyle := lipgloss.NewStyle().Foreground(TextBrightColor)
		cursorChar := lipgloss.NewStyle().Foreground(PrimaryColor).Render("█")
		if s.editBuffer == "" {
			return editStyle.Render("(empty)") + cursorChar
		}
		// Keep the tail of a long buffer in view — that is where the cursor is —
		// instead of letting it push the value column past the modal border.
		buf := []rune(s.editBuffer)
		if len(buf) > maxValWidth-1 {
			buf = append([]rune("…"), buf[len(buf)-(maxValWidth-2):]...)
		}
		return editStyle.Render(string(buf)) + cursorChar
	}

	switch item.Type {
	case SettingsItemBool:
		if item.BoolVal {
			return valueStyle.Render("Yes")
		}
		return valueOffStyle.Render("No")

	case SettingsItemTriBool:
		if item.TriVal == nil {
			state := "off"
			if s.derivedActive(strings.TrimSuffix(item.Key, ".enabled")) {
				state = "on"
			}
			return valueOffStyle.Render("Default (" + state + ")")
		}
		if *item.TriVal {
			return valueStyle.Render("Yes")
		}
		return valueOffStyle.Render("No")

	case SettingsItemInt:
		if item.IntVal <= 0 {
			return valueOffStyle.Render(item.placeholder())
		}
		return valueStyle.Render(strconv.Itoa(item.IntVal))

	case SettingsItemEnum:
		if item.StringVal == "" {
			return valueOffStyle.Render(item.placeholder())
		}
		return valueStyle.Render(item.StringVal)

	default:
		if item.StringVal == "" {
			return valueOffStyle.Render(item.placeholder())
		}
		val := item.StringVal
		if len(val) > maxValWidth {
			val = val[:maxValWidth-1] + "…"
		}
		return valueStyle.Render(val)
	}
}

// placeholder is what the value column shows for an unset setting.
func (i SettingsItem) placeholder() string {
	if i.Placeholder != "" {
		return i.Placeholder
	}
	return "(not set)"
}

// ghErrorContentHeight is the number of lines renderGHError writes: header,
// blank, message, blank, install hint, disabled note.
const ghErrorContentHeight = 6

// renderGHError renders the GH CLI error dialog.
func (s *SettingsOverlay) renderGHError(modalWidth int) string {
	var result strings.Builder

	errorHeaderStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ErrorColor).
		Padding(0, 1)
	errorMsgStyle := lipgloss.NewStyle().
		Foreground(TextColor).
		Padding(0, 1)

	result.WriteString(errorHeaderStyle.Render("GitHub CLI Error"))
	result.WriteString("\n\n")
	result.WriteString(errorMsgStyle.Render(s.ghError))
	result.WriteString("\n\n")

	hintStyle := lipgloss.NewStyle().
		Foreground(MutedColor).
		Padding(0, 1)
	result.WriteString(hintStyle.Render("Install: https://cli.github.com"))
	result.WriteString("\n")
	result.WriteString(hintStyle.Render("PR creation has been disabled."))

	_ = modalWidth
	return result.String()
}
