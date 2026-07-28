package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ben182/chief/internal/config"
	"github.com/ben182/chief/internal/loop"
	"github.com/charmbracelet/lipgloss"
)

// configLeafKeys walks config.Config and returns every leaf setting as its
// dotted yaml path (e.g. "review.model").
func configLeafKeys(t *testing.T, typ reflect.Type, prefix string) []string {
	t.Helper()

	var keys []string
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			t.Fatalf("field %s.%s has no yaml tag", typ.Name(), field.Name)
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		if field.Type.Kind() == reflect.Struct {
			keys = append(keys, configLeafKeys(t, field.Type, path)...)
			continue
		}
		keys = append(keys, path)
	}
	return keys
}

// TestSettingsOverlay_CoversEveryConfigKey pins the overlay to the config
// struct. The overlay used to expose a hand-picked seven of the config's
// settings, so every key added since — the per-phase review and consolidation
// models among them — was reachable only by editing .chief/config.yaml by hand.
// Adding a field to config.Config now fails here until it has a row.
func TestSettingsOverlay_CoversEveryConfigKey(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())

	covered := make(map[string]bool, len(s.items))
	for _, item := range s.items {
		if covered[item.Key] {
			t.Errorf("duplicate settings item for key %q", item.Key)
		}
		covered[item.Key] = true
	}

	wanted := configLeafKeys(t, reflect.TypeOf(config.Config{}), "")
	for _, key := range wanted {
		if !covered[key] {
			t.Errorf("config key %q has no settings item — add one to LoadFromConfig", key)
		}
		delete(covered, key)
	}
	for key := range covered {
		t.Errorf("settings item %q does not match any config key", key)
	}
}

// TestSettingsOverlay_RoundTripsEveryConfigKey checks the other half of the
// contract: a value edited in the overlay has to land back in the config. A key
// with a row in LoadFromConfig but no case in ApplyToConfig would render and
// edit fine, then silently discard the change on save.
func TestSettingsOverlay_RoundTripsEveryConfigKey(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())

	// Give every item a value that differs from the zero value it loaded.
	for i := range s.items {
		switch s.items[i].Type {
		case SettingsItemBool:
			s.items[i].BoolVal = !s.items[i].BoolVal
		case SettingsItemTriBool:
			s.items[i].TriVal = config.Bool(true)
		case SettingsItemInt:
			s.items[i].IntVal = 4242
		case SettingsItemEnum:
			s.items[i].StringVal = s.items[i].Options[len(s.items[i].Options)-1]
		default:
			s.items[i].StringVal = "value-for-" + s.items[i].Key
		}
	}

	cfg := config.Default()
	s.ApplyToConfig(cfg)

	// Read the applied config back through a second overlay: if ApplyToConfig
	// missed a key, the reloaded item still carries the original value.
	reloaded := NewSettingsOverlay()
	reloaded.LoadFromConfig(cfg)

	for i, want := range s.items {
		got := reloaded.items[i]
		if got.Key != want.Key {
			t.Fatalf("item %d: key changed across reload: %q vs %q", i, want.Key, got.Key)
		}
		switch want.Type {
		case SettingsItemBool:
			if got.BoolVal != want.BoolVal {
				t.Errorf("%s: not applied (got %v, want %v)", want.Key, got.BoolVal, want.BoolVal)
			}
		case SettingsItemTriBool:
			if got.TriVal == nil || *got.TriVal != *want.TriVal {
				t.Errorf("%s: not applied (got %v)", want.Key, got.TriVal)
			}
		case SettingsItemInt:
			if got.IntVal != want.IntVal {
				t.Errorf("%s: not applied (got %d, want %d)", want.Key, got.IntVal, want.IntVal)
			}
		default:
			if got.StringVal != want.StringVal {
				t.Errorf("%s: not applied (got %q, want %q)", want.Key, got.StringVal, want.StringVal)
			}
		}
	}
}

func TestSettingsOverlay_LoadFromConfig(t *testing.T) {
	s := NewSettingsOverlay()
	cfg := &config.Config{
		Worktree: config.WorktreeConfig{
			Setup: "npm install",
		},
		OnComplete: config.OnCompleteConfig{
			Push:         true,
			CreatePR:     false,
			PRBaseBranch: "develop",
		},
	}
	s.LoadFromConfig(cfg)

	if s.items[0].Key != "worktree.setup" || s.items[0].StringVal != "npm install" {
		t.Errorf("worktree.setup item: got key=%s val=%s", s.items[0].Key, s.items[0].StringVal)
	}
	if s.items[1].Key != "onComplete.push" || !s.items[1].BoolVal {
		t.Errorf("onComplete.push item: got key=%s val=%v", s.items[1].Key, s.items[1].BoolVal)
	}
	if s.items[2].Key != "onComplete.createPR" || s.items[2].BoolVal {
		t.Errorf("onComplete.createPR item: got key=%s val=%v", s.items[2].Key, s.items[2].BoolVal)
	}
	if s.items[3].Key != "onComplete.prBaseBranch" || s.items[3].StringVal != "develop" {
		t.Errorf("onComplete.prBaseBranch item: got key=%s val=%s", s.items[3].Key, s.items[3].StringVal)
	}
	if s.items[4].Key != "onComplete.summary" {
		t.Errorf("onComplete.summary item: got key=%s", s.items[4].Key)
	}
	if s.items[5].Key != "onComplete.notify" {
		t.Errorf("onComplete.notify item: got key=%s", s.items[5].Key)
	}
	if s.items[6].Key != "loop.keepAwake" {
		t.Errorf("loop.keepAwake item: got key=%s", s.items[6].Key)
	}
	if s.selectedIndex != 0 {
		t.Errorf("expected selectedIndex=0, got %d", s.selectedIndex)
	}
}

func TestSettingsOverlay_ApplyToConfig(t *testing.T) {
	s := NewSettingsOverlay()
	cfg := config.Default()
	s.LoadFromConfig(cfg)

	// Modify items
	s.items[0].StringVal = "go mod download"
	s.items[1].BoolVal = true
	s.items[2].BoolVal = true
	s.items[3].StringVal = "develop"

	resultCfg := config.Default()
	s.ApplyToConfig(resultCfg)

	if resultCfg.Worktree.Setup != "go mod download" {
		t.Errorf("expected setup='go mod download', got '%s'", resultCfg.Worktree.Setup)
	}
	if !resultCfg.OnComplete.Push {
		t.Error("expected push=true")
	}
	if !resultCfg.OnComplete.CreatePR {
		t.Error("expected createPR=true")
	}
	if resultCfg.OnComplete.PRBaseBranch != "develop" {
		t.Errorf("expected prBaseBranch='develop', got '%s'", resultCfg.OnComplete.PRBaseBranch)
	}
}

func TestSettingsOverlay_Navigation(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())

	if s.selectedIndex != 0 {
		t.Fatalf("expected initial index=0, got %d", s.selectedIndex)
	}

	s.MoveDown()
	if s.selectedIndex != 1 {
		t.Errorf("expected index=1 after MoveDown, got %d", s.selectedIndex)
	}

	s.MoveDown()
	if s.selectedIndex != 2 {
		t.Errorf("expected index=2 after second MoveDown, got %d", s.selectedIndex)
	}

	s.MoveDown()
	if s.selectedIndex != 3 {
		t.Errorf("expected index=3 after third MoveDown, got %d", s.selectedIndex)
	}

	// Walk to the last item, then one past it: the selection must clamp. Expressed
	// against len(items) so adding a setting doesn't break this test.
	last := len(s.items) - 1
	for s.selectedIndex < last {
		s.MoveDown()
	}
	s.MoveDown()
	if s.selectedIndex != last {
		t.Errorf("expected index=%d (clamped), got %d", last, s.selectedIndex)
	}

	s.MoveUp()
	if s.selectedIndex != last-1 {
		t.Errorf("expected index=%d after MoveUp, got %d", last-1, s.selectedIndex)
	}

	// Can't go before first item
	for s.selectedIndex > 0 {
		s.MoveUp()
	}
	s.MoveUp()
	if s.selectedIndex != 0 {
		t.Errorf("expected index=0 (clamped), got %d", s.selectedIndex)
	}
}

func TestSettingsOverlay_ToggleBool(t *testing.T) {
	s := NewSettingsOverlay()
	cfg := &config.Config{
		OnComplete: config.OnCompleteConfig{Push: false},
	}
	s.LoadFromConfig(cfg)

	// Select "Push to remote" (index 1)
	s.MoveDown()

	key, val := s.ToggleBool()
	if key != "onComplete.push" {
		t.Errorf("expected key='onComplete.push', got '%s'", key)
	}
	if !val {
		t.Error("expected val=true after toggle")
	}

	// Toggle back
	key, val = s.ToggleBool()
	if val {
		t.Error("expected val=false after second toggle")
	}
	_ = key
}

func TestSettingsOverlay_ToggleBool_OnStringItem(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())

	// Selected item is "Setup command" (string type)
	key, _ := s.ToggleBool()
	if key != "" {
		t.Errorf("expected empty key for string item toggle, got '%s'", key)
	}
}

func TestSettingsOverlay_RevertToggle(t *testing.T) {
	s := NewSettingsOverlay()
	cfg := &config.Config{
		OnComplete: config.OnCompleteConfig{Push: false},
	}
	s.LoadFromConfig(cfg)

	s.MoveDown() // Select "Push to remote"
	s.ToggleBool()
	if !s.items[1].BoolVal {
		t.Fatal("expected true after toggle")
	}

	s.RevertToggle()
	if s.items[1].BoolVal {
		t.Error("expected false after revert")
	}
}

func TestSettingsOverlay_StringEditing(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())

	// Selected item is "Setup command" (index 0)
	if s.IsEditing() {
		t.Fatal("should not be editing initially")
	}

	s.StartEditing()
	if !s.IsEditing() {
		t.Fatal("should be editing after StartEditing")
	}
	if s.editBuffer != "" {
		t.Errorf("expected empty edit buffer, got '%s'", s.editBuffer)
	}

	s.AddEditChar('n')
	s.AddEditChar('p')
	s.AddEditChar('m')
	if s.editBuffer != "npm" {
		t.Errorf("expected 'npm', got '%s'", s.editBuffer)
	}

	s.DeleteEditChar()
	if s.editBuffer != "np" {
		t.Errorf("expected 'np' after delete, got '%s'", s.editBuffer)
	}

	s.ConfirmEdit()
	if s.IsEditing() {
		t.Fatal("should not be editing after ConfirmEdit")
	}
	if s.items[0].StringVal != "np" {
		t.Errorf("expected StringVal='np', got '%s'", s.items[0].StringVal)
	}
}

func TestSettingsOverlay_CancelEdit(t *testing.T) {
	s := NewSettingsOverlay()
	cfg := &config.Config{
		Worktree: config.WorktreeConfig{Setup: "original"},
	}
	s.LoadFromConfig(cfg)

	s.StartEditing()
	s.AddEditChar('x')
	s.CancelEdit()

	if s.IsEditing() {
		t.Fatal("should not be editing after CancelEdit")
	}
	if s.items[0].StringVal != "original" {
		t.Errorf("expected 'original' preserved, got '%s'", s.items[0].StringVal)
	}
}

func TestSettingsOverlay_StartEditingOnBoolItem(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())
	s.MoveDown() // Select "Push to remote" (bool)

	s.StartEditing()
	if s.IsEditing() {
		t.Error("should not start editing on a bool item")
	}
}

func TestSettingsOverlay_GHError(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())

	if s.HasGHError() {
		t.Fatal("should not have GH error initially")
	}

	s.SetGHError("gh not found")
	if !s.HasGHError() {
		t.Fatal("should have GH error after SetGHError")
	}

	s.DismissGHError()
	if s.HasGHError() {
		t.Fatal("should not have GH error after dismiss")
	}
}

func TestSettingsOverlay_Render(t *testing.T) {
	s := NewSettingsOverlay()
	cfg := &config.Config{
		Worktree: config.WorktreeConfig{Setup: "npm install"},
		OnComplete: config.OnCompleteConfig{
			Push:     true,
			CreatePR: false,
		},
	}
	s.LoadFromConfig(cfg)
	s.SetSize(80, 24)

	rendered := s.Render()

	// Check header
	if !strings.Contains(rendered, "Settings") {
		t.Error("expected 'Settings' in header")
	}
	if !strings.Contains(rendered, ".chief/config.yaml") {
		t.Error("expected config path in header")
	}

	// Check section headers
	if !strings.Contains(rendered, "Worktree") {
		t.Error("expected 'Worktree' section")
	}
	if !strings.Contains(rendered, "On Complete") {
		t.Error("expected 'On Complete' section")
	}

	// Check values
	if !strings.Contains(rendered, "npm install") {
		t.Error("expected 'npm install' value")
	}
	if !strings.Contains(rendered, "Yes") {
		t.Error("expected 'Yes' for push")
	}
	if !strings.Contains(rendered, "No") {
		t.Error("expected 'No' for createPR")
	}

	// Check footer
	if !strings.Contains(rendered, "Esc: close") {
		t.Error("expected 'Esc: close' in footer")
	}
}

func TestSettingsOverlay_RenderGHError(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())
	s.SetSize(80, 24)

	s.SetGHError("gh not found")
	rendered := s.Render()

	if !strings.Contains(rendered, "GitHub CLI Error") {
		t.Error("expected 'GitHub CLI Error' in rendered output")
	}
	if !strings.Contains(rendered, "gh not found") {
		t.Error("expected error message in rendered output")
	}
	if !strings.Contains(rendered, "Press any key to dismiss") {
		t.Error("expected dismiss hint in footer")
	}
}

func TestSettingsOverlay_RenderEditing(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())
	s.SetSize(80, 24)

	s.StartEditing()
	rendered := s.Render()

	if !strings.Contains(rendered, "Enter: save") {
		t.Error("expected 'Enter: save' in footer during editing")
	}
}

func TestSettingsOverlay_RenderSelectedIndicator(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())
	s.SetSize(80, 24)

	rendered := s.Render()

	// The selected item should have a ">" indicator
	if !strings.Contains(rendered, ">") {
		t.Error("expected '>' cursor indicator for selected item")
	}
}

func TestSettingsOverlay_RenderEmptyStringValue(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())
	s.SetSize(80, 24)

	rendered := s.Render()

	if !strings.Contains(rendered, "(not set)") {
		t.Error("expected '(not set)' for empty setup command")
	}
}

// selectKey moves the selection onto the item with the given config key.
func selectKey(t *testing.T, s *SettingsOverlay, key string) {
	t.Helper()
	for i, item := range s.items {
		if item.Key == key {
			s.selectedIndex = i
			return
		}
	}
	t.Fatalf("no settings item for key %q", key)
}

func TestSettingsOverlay_CycleTriBool(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())
	selectKey(t, s, "review.enabled")

	item := s.GetSelectedItem()
	if item.TriVal != nil {
		t.Fatalf("expected review.enabled to start unset, got %v", *item.TriVal)
	}

	s.CycleTriBool()
	if item = s.GetSelectedItem(); item.TriVal == nil || !*item.TriVal {
		t.Fatalf("expected true after first cycle, got %v", item.TriVal)
	}

	s.CycleTriBool()
	if item = s.GetSelectedItem(); item.TriVal == nil || *item.TriVal {
		t.Fatalf("expected false after second cycle, got %v", item.TriVal)
	}

	// Third cycle returns to the derived default rather than sticking on false.
	s.CycleTriBool()
	if item = s.GetSelectedItem(); item.TriVal != nil {
		t.Fatalf("expected unset after third cycle, got %v", *item.TriVal)
	}
}

// A tri-state switch left alone must stay nil through a save, or opening the
// overlay and toggling something unrelated would freeze the review pass at
// whatever it happened to be deriving at the time.
func TestSettingsOverlay_ApplyKeepsUntouchedTriStateUnset(t *testing.T) {
	s := NewSettingsOverlay()
	cfg := config.Default()
	cfg.Review.Skill = "/code-quality"
	s.LoadFromConfig(cfg)

	selectKey(t, s, "onComplete.push")
	s.ToggleBool()
	s.ApplyToConfig(cfg)

	if cfg.Review.Enabled != nil {
		t.Errorf("expected review.enabled to stay unset, got %v", *cfg.Review.Enabled)
	}
	if cfg.Consolidate.Enabled != nil {
		t.Errorf("expected consolidate.enabled to stay unset, got %v", *cfg.Consolidate.Enabled)
	}
	if !cfg.Review.Active() {
		t.Error("expected the review to stay derived-on from its skill")
	}
}

// Loading and applying must both copy the tri-state switch rather than share
// the pointer. Sharing it works only for as long as every edit path happens to
// replace the pointer instead of writing through it; the moment one writes
// `*item.TriVal = false`, a config the overlay merely displayed would change
// underneath the caller — with no save, and no way to cancel out of it.
func TestSettingsOverlay_TriStateIsNeverSharedWithConfig(t *testing.T) {
	s := NewSettingsOverlay()
	cfg := config.Default()
	cfg.Review.Enabled = config.Bool(true)
	cfg.Consolidate.Enabled = config.Bool(false)
	s.LoadFromConfig(cfg)

	loaded := map[string]*bool{
		"review.enabled":      cfg.Review.Enabled,
		"consolidate.enabled": cfg.Consolidate.Enabled,
	}
	for key, fromCfg := range loaded {
		selectKey(t, s, key)
		item := s.GetSelectedItem()
		if item.TriVal == nil {
			t.Fatalf("%s: expected the loaded value, got nil", key)
		}
		if item.TriVal == fromCfg {
			t.Errorf("%s: overlay shares the config's pointer after LoadFromConfig", key)
		}
		if *item.TriVal != *fromCfg {
			t.Errorf("%s: loaded %v, config holds %v", key, *item.TriVal, *fromCfg)
		}
	}

	s.ApplyToConfig(cfg)

	for key, want := range map[string]bool{"review.enabled": true, "consolidate.enabled": false} {
		selectKey(t, s, key)
		item := s.GetSelectedItem()
		var applied *bool
		if key == "review.enabled" {
			applied = cfg.Review.Enabled
		} else {
			applied = cfg.Consolidate.Enabled
		}
		if applied == nil || *applied != want {
			t.Errorf("%s: expected %v after apply, got %v", key, want, applied)
		}
		if applied == item.TriVal {
			t.Errorf("%s: config shares the overlay's pointer after ApplyToConfig", key)
		}
	}

	// And the round trip leaves the values themselves alone.
	selectKey(t, s, "review.enabled")
	s.CycleTriBool() // true -> false
	if cfg.Review.Enabled == nil || !*cfg.Review.Enabled {
		t.Error("cycling the overlay changed the config before ApplyToConfig")
	}
}

func TestSettingsOverlay_IntEditing(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())
	selectKey(t, s, "loop.watchdogTimeoutSeconds")

	s.StartEditing()
	if !s.IsEditing() {
		t.Fatal("expected an int item to be editable")
	}
	if s.editBuffer != "" {
		t.Errorf("expected an empty buffer for an unset timeout, got %q", s.editBuffer)
	}

	// Non-digits are dropped so the buffer always parses.
	for _, ch := range "9x0m0" {
		s.AddEditChar(ch)
	}
	if s.editBuffer != "900" {
		t.Errorf("expected buffer '900', got %q", s.editBuffer)
	}

	s.ConfirmEdit()
	if got := s.GetSelectedItem().IntVal; got != 900 {
		t.Errorf("expected 900, got %d", got)
	}

	// Clearing the field falls back to the built-in default.
	s.StartEditing()
	if s.editBuffer != "900" {
		t.Errorf("expected the current value in the buffer, got %q", s.editBuffer)
	}
	for range "900" {
		s.DeleteEditChar()
	}
	s.ConfirmEdit()
	if got := s.GetSelectedItem().IntVal; got != 0 {
		t.Errorf("expected 0 after clearing, got %d", got)
	}
}

func TestSettingsOverlay_CycleEnum(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())
	selectKey(t, s, "agent.provider")

	if got := s.GetSelectedItem().StringVal; got != "" {
		t.Fatalf("expected provider to start unset, got %q", got)
	}

	seen := []string{}
	for range agentProviders {
		s.CycleEnum()
		seen = append(seen, s.GetSelectedItem().StringVal)
	}
	for i, want := range agentProviders {
		if seen[i] != want {
			t.Errorf("cycle %d: expected %q, got %q", i, want, seen[i])
		}
	}

	// Past the last provider it wraps back to "use the default".
	s.CycleEnum()
	if got := s.GetSelectedItem().StringVal; got != "" {
		t.Errorf("expected wrap back to unset, got %q", got)
	}
}

// A provider name that is not in the list (hand-edited YAML, or one dropped in a
// later version) must not trap the cycle.
func TestSettingsOverlay_CycleEnumFromUnknownValue(t *testing.T) {
	s := NewSettingsOverlay()
	cfg := config.Default()
	cfg.Agent.Provider = "aider"
	s.LoadFromConfig(cfg)
	selectKey(t, s, "agent.provider")

	s.CycleEnum()
	if got := s.GetSelectedItem().StringVal; got != agentProviders[0] {
		t.Errorf("expected %q, got %q", agentProviders[0], got)
	}
}

func TestSettingsOverlay_RenderTriStateDefault(t *testing.T) {
	s := NewSettingsOverlay()
	cfg := config.Default()
	s.LoadFromConfig(cfg)
	s.SetSize(120, 60) // tall enough that nothing scrolls out of view

	if !strings.Contains(s.Render(), "Default (off)") {
		t.Error("expected an unconfigured review to render as 'Default (off)'")
	}

	// A skill turns the pass on without an explicit `enabled`, and the label has
	// to follow the setting as it is edited, not just as it was loaded.
	selectKey(t, s, "review.skill")
	s.StartEditing()
	for _, ch := range "/code-quality" {
		s.AddEditChar(ch)
	}
	s.ConfirmEdit()

	if !strings.Contains(s.Render(), "Default (on)") {
		t.Error("expected the derived default to flip to 'on' once a skill is set")
	}
}

func TestSettingsOverlay_RenderPlaceholders(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())
	s.SetSize(120, 60)

	rendered := s.Render()
	for _, want := range []string{"sonnet (default)", "claude (default)", defaultWatchdogLabel()} {
		if !strings.Contains(rendered, want) {
			t.Errorf("expected placeholder %q in the rendered overlay", want)
		}
	}
}

// The item list is taller than the modal on a normal terminal, so the selected
// row has to be scrolled into view — otherwise everything below the fold is
// invisible and unreachable.
func TestSettingsOverlay_ScrollsSelectionIntoView(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())
	s.SetSize(80, 24)

	if !strings.Contains(s.Render(), "Worktree") {
		t.Fatal("expected the first section to be visible initially")
	}

	last := len(s.items) - 1
	for s.selectedIndex < last {
		s.MoveDown()
	}

	rendered := s.Render()
	if !strings.Contains(rendered, "Consolidate") {
		t.Error("expected the last section to be visible after scrolling to the end")
	}
	if !strings.Contains(rendered, "Instructions") {
		t.Error("expected the last item to be visible after scrolling to the end")
	}
	if strings.Contains(rendered, "Worktree") {
		t.Error("expected the first section to have scrolled out of view")
	}
	if !strings.Contains(rendered, "⋯") {
		t.Error("expected a marker showing the list continues above")
	}

	// Back at the top the window has to follow the selection the other way.
	for s.selectedIndex > 0 {
		s.MoveUp()
	}
	if !strings.Contains(s.Render(), "Worktree") {
		t.Error("expected to scroll back to the first section")
	}
}

// A value wider than the value column must be shortened, not wrapped. lipgloss
// wraps an over-long line rather than letting it stick out, so an unclamped
// value costs the modal extra rows: the box grows past the height it sized
// itself to and pushes settings off the bottom. Free-form instructions are long
// enough to hit this in normal use.
func TestSettingsOverlay_LongValuesDoNotWrap(t *testing.T) {
	const screenW, screenH = 80, 40

	baseline := NewSettingsOverlay()
	baseline.LoadFromConfig(config.Default())
	baseline.SetSize(screenW, screenH)
	wantLines := len(strings.Split(baseline.Render(), "\n"))

	long := strings.Repeat("watch for N+1 queries and missing tests ", 8)

	s := NewSettingsOverlay()
	cfg := config.Default()
	cfg.Review.Instructions = long
	s.LoadFromConfig(cfg)
	s.SetSize(screenW, screenH)

	check := func(label string) {
		t.Helper()
		rendered := s.Render()
		if got := len(strings.Split(rendered, "\n")); got != wantLines {
			t.Errorf("%s: modal is %d lines, expected %d — the value wrapped", label, got, wantLines)
		}
		var widest int
		for _, line := range strings.Split(rendered, "\n") {
			if w := lipgloss.Width(line); w > widest {
				widest = w
			}
		}
		if widest > screenW {
			t.Errorf("%s: rendered %d columns wide, screen is %d", label, widest, screenW)
		}
	}

	check("long stored value")

	// Same again while that value is being typed: the edit buffer is rendered in
	// place of the stored value, on a path of its own.
	selectKey(t, s, "review.instructions")
	s.StartEditing()
	for _, ch := range long {
		s.AddEditChar(ch)
	}
	check("long edit buffer")
}

// A running loop reads these settings from the manager, on its own goroutine.
// That only works if an edit publishes a *new* config instead of writing through
// the pointer the loop already has — otherwise every keystroke in the overlay is
// a data race against a live run.
func TestApp_PublishSettingsReplacesConfigRatherThanMutatingIt(t *testing.T) {
	original := config.Default()
	original.Review.Model = "sonnet"

	manager := loop.NewManager(1, nil)
	manager.SetConfig(original)

	app := &App{
		baseDir:         t.TempDir(),
		config:          original,
		manager:         manager,
		settingsOverlay: NewSettingsOverlay(),
	}
	app.settingsOverlay.LoadFromConfig(app.config)

	selectKey(t, app.settingsOverlay, "review.model")
	app.settingsOverlay.StartEditing()
	for range "sonnet" { // editing starts from the current value
		app.settingsOverlay.DeleteEditChar()
	}
	for _, ch := range "haiku" {
		app.settingsOverlay.AddEditChar(ch)
	}
	app.settingsOverlay.ConfirmEdit()

	app.publishSettings()

	if app.config == original {
		t.Error("expected a new config, got the same pointer back")
	}
	if original.Review.Model != "sonnet" {
		t.Errorf("the config in use was edited in place: review.model is now %q", original.Review.Model)
	}
	if app.config.Review.Model != "haiku" {
		t.Errorf("app config not updated: review.model = %q", app.config.Review.Model)
	}
	if got := manager.Config(); got != app.config {
		t.Error("the manager was not handed the new config, so a running loop would keep the old one")
	}

	// Settings the overlay didn't touch have to survive the round trip.
	if !app.config.OnComplete.Notify || !app.config.Loop.KeepAwake {
		t.Error("expected untouched defaults to be carried over to the new config")
	}
}

// Publishing has to write the file too — that is the only copy that survives a
// restart, and the manager's in-memory config would otherwise drift from it.
func TestApp_PublishSettingsWritesTheConfigFile(t *testing.T) {
	baseDir := t.TempDir()
	app := &App{
		baseDir:         baseDir,
		config:          config.Default(),
		settingsOverlay: NewSettingsOverlay(),
	}
	app.settingsOverlay.LoadFromConfig(app.config)

	selectKey(t, app.settingsOverlay, "consolidate.enabled")
	app.settingsOverlay.CycleTriBool() // unset -> true
	app.publishSettings()

	reloaded, err := config.Load(baseDir)
	if err != nil {
		t.Fatalf("loading the saved config failed: %v", err)
	}
	if reloaded.Consolidate.Enabled == nil || !*reloaded.Consolidate.Enabled {
		t.Errorf("expected consolidate.enabled: true on disk, got %v", reloaded.Consolidate.Enabled)
	}
}

func TestSettingsOverlay_GetSelectedItem(t *testing.T) {
	s := NewSettingsOverlay()
	s.LoadFromConfig(config.Default())

	item := s.GetSelectedItem()
	if item == nil {
		t.Fatal("expected non-nil selected item")
	}
	if item.Key != "worktree.setup" {
		t.Errorf("expected first item key='worktree.setup', got '%s'", item.Key)
	}

	s.MoveDown()
	item = s.GetSelectedItem()
	if item.Key != "onComplete.push" {
		t.Errorf("expected second item key='onComplete.push', got '%s'", item.Key)
	}
}

// The log's "story done" marker mirrors review.enabled and was only set at
// construction. The loop and the ETA follow a mid-run toggle live, so the
// marker must move with them — otherwise the log prints the final green
// "story done" while the run is still waiting on a review, or the reverse.
func TestApp_PublishSettingsUpdatesReviewPendingMarker(t *testing.T) {
	cfg := config.Default()
	app := &App{
		baseDir:         t.TempDir(),
		config:          cfg,
		logViewer:       NewLogViewer(),
		settingsOverlay: NewSettingsOverlay(),
	}
	app.logViewer.SetReviewPending(cfg.Review.Active())
	app.settingsOverlay.LoadFromConfig(app.config)

	if app.logViewer.reviewPending {
		t.Fatal("precondition: review is off by default")
	}

	selectKey(t, app.settingsOverlay, "review.enabled")
	app.settingsOverlay.CycleTriBool() // unset -> true
	app.publishSettings()

	if !app.config.Review.Active() {
		t.Fatal("precondition: the toggle should have enabled the review")
	}
	if !app.logViewer.reviewPending {
		t.Error("enabling the review mid-run must switch the story-done marker to review-pending")
	}

	app.settingsOverlay.CycleTriBool() // true -> false
	app.publishSettings()
	if app.logViewer.reviewPending {
		t.Error("disabling the review must switch the marker back")
	}
}
