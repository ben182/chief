package prd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PrepStampFile is the file next to prd.md that records which environments the
// PRD was prepared for by `chief prep`, and what the PRD said at the time.
//
// It is local on purpose: it says "this PRD, as it stands on this machine, was
// gone through with its author for this target", which is a fact about this
// checkout rather than about the branch. It is kept out of git by the PRD
// directory's own .gitignore.
const PrepStampFile = "prep.json"

// PrepTargetBox is the target a `chief box prep` records: the box, whatever
// machine the run is started from. A local prep records runtime.GOOS instead.
const PrepTargetBox = "box"

// prepRecord is one target's entry in the stamp.
type prepRecord struct {
	Hash string    `json:"hash"`
	At   time.Time `json:"at"`
}

// PrepStampPath is where the stamp for the PRD at prdPath lives.
func PrepStampPath(prdPath string) string {
	return filepath.Join(filepath.Dir(prdPath), PrepStampFile)
}

// PrepHash fingerprints what a PRD asks for, leaving out what a run changes
// while it works through it: status lines, the "**Blockiert (Ben):**" block,
// ticked checkboxes, and blank lines. Without that, the first story a run
// finished would make the prep look stale, and the next start would ask the
// same questions again about a PRD nobody has touched.
func PrepHash(content string) string {
	h := sha256.New()
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || statusLineRegex.MatchString(trimmed) || blockedReasonLineRegex.MatchString(trimmed) {
			continue
		}
		if m := checkboxRegex.FindStringSubmatch(trimmed); m != nil {
			trimmed = "- [ ] " + strings.TrimSpace(m[2])
		}
		h.Write([]byte(trimmed))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// CheckPrep reports whether the PRD at prdPath was prepared for target and has
// not changed since. When it was not, reason says why in a few words, for the
// line that announces the prep.
func CheckPrep(prdPath, target string) (fresh bool, reason string) {
	content, err := os.ReadFile(prdPath)
	if err != nil {
		return false, "the PRD could not be read"
	}
	stamp := loadPrepStamp(prdPath)
	rec, ok := stamp[target]
	switch {
	case len(stamp) == 0:
		return false, "it was never prepared"
	case !ok:
		return false, "it was prepared for another environment"
	case rec.Hash != PrepHash(string(content)):
		return false, "it changed since it was prepared"
	}
	return true, ""
}

// RecordPrep writes the stamp for target, fingerprinting the PRD as it is now.
// Other targets' entries are kept: a PRD prepared for the box and for this Mac
// is prepared for both.
func RecordPrep(prdPath, target string, now time.Time) error {
	content, err := os.ReadFile(prdPath)
	if err != nil {
		return fmt.Errorf("failed to read PRD file: %w", err)
	}
	stamp := loadPrepStamp(prdPath)
	stamp[target] = prepRecord{Hash: PrepHash(string(content)), At: now.UTC()}
	data, err := json.MarshalIndent(stamp, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(PrepStampPath(prdPath), append(data, '\n'))
}

// loadPrepStamp reads the stamp, or returns an empty one when there is none or
// it cannot be read: an unreadable stamp means "prepare again", never a failure.
func loadPrepStamp(prdPath string) map[string]prepRecord {
	stamp := map[string]prepRecord{}
	data, err := os.ReadFile(PrepStampPath(prdPath))
	if err != nil {
		return stamp
	}
	if err := json.Unmarshal(data, &stamp); err != nil || stamp == nil {
		return map[string]prepRecord{}
	}
	return stamp
}
