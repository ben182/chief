package box

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// State records the box a project is currently using, so the commands that come
// after `up` — logs, status, ssh, down — know which machine they mean without
// being told.
type State struct {
	// ServerID is what the box is destroyed by. The name would do, but an ID
	// cannot be claimed by a different machine later.
	ServerID int64 `json:"serverId"`
	// Name and IP are how the box is found and reached.
	Name string `json:"name"`
	IP   string `json:"ip"`
	// PRD is the run the box was created for, which is also the systemd unit
	// instance its log lives under.
	PRD string `json:"prd"`
	// Created is when the box started billing.
	Created time.Time `json:"created"`
}

// Age is how long the box has existed, which is what it has cost so far.
func (s State) Age() time.Duration { return time.Since(s.Created).Round(time.Second) }

// stateDir is where a project's box state lives.
//
// Under .chief, which chief's own setup adds to .gitignore — but not every
// project takes that offer, and a machine's address has no business in a commit
// either way, so the directory carries its own .gitignore.
func stateDir(baseDir string) string { return filepath.Join(baseDir, ".chief", "box") }

func statePath(baseDir string) string { return filepath.Join(stateDir(baseDir), "current.json") }

// SaveState records the box this project is using.
func SaveState(baseDir string, s State) error {
	dir := stateDir(baseDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("recording the box: %w", err)
	}
	ignore := "# a throwaway machine's address is nobody's business but this checkout's\n*\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(ignore), 0o600); err != nil {
		return fmt.Errorf("recording the box: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("recording the box: %w", err)
	}
	if err := os.WriteFile(statePath(baseDir), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("recording the box: %w", err)
	}
	return nil
}

// LoadState returns the box this project is using, reporting false when it has
// none.
func LoadState(baseDir string) (State, bool) {
	data, err := os.ReadFile(statePath(baseDir)) //nolint:gosec // a fixed path under the project's own .chief
	if err != nil {
		return State{}, false
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil || s.IP == "" {
		return State{}, false
	}
	return s, true
}

// ForgetState drops the record, after the box it described is gone.
func ForgetState(baseDir string) error {
	err := os.Remove(statePath(baseDir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
