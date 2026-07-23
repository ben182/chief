package prd

import (
	"os"
	"path/filepath"
)

// FollowupInboxNames lists the accepted follow-up inbox filenames, in preference
// order. A follow-up inbox is a flat markdown checklist the user fills in by hand
// while reviewing a finished PRD; `chief followup` converts its open items into
// user stories and flips the ingested ones to "- [x] (<story-id>)".
//
// It lives in the prd package so the loop's per-story commit and the end-of-run
// summary sweep can commit the inbox alongside prd.md/progress.md — otherwise the
// followup's edits (checked-off items) linger as an uncommitted change after the
// run finishes. The cmd package (which owns `chief followup`) reads the same list,
// keeping the accepted names in one place; putting it here rather than in cmd
// avoids an import cycle (cmd already imports prd; loop/summary can't import cmd).
var FollowupInboxNames = []string{"todos.md", "followups.md", "follow-ups.md"}

// FollowupInboxPath returns the path to the first existing follow-up inbox file
// in prdDir (see FollowupInboxNames), or "" when none exists.
func FollowupInboxPath(prdDir string) string {
	for _, name := range FollowupInboxNames {
		path := filepath.Join(prdDir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}
