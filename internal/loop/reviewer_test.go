package loop

import "testing"

// TestReviewerActive verifies the predicate that gates the post-commit review
// agent: it is active when explicitly enabled, or when a skill or free-form
// instructions are configured (whitespace-only fields don't count).
func TestReviewerActive(t *testing.T) {
	tests := []struct {
		name string
		r    reviewer
		want bool
	}{
		{"zero value is inactive", reviewer{}, false},
		{"enabled flag activates", reviewer{enabled: true}, true},
		{"skill activates", reviewer{skill: "/code-quality"}, true},
		{"instructions activate", reviewer{instructions: "watch for N+1"}, true},
		{"whitespace-only skill is inactive", reviewer{skill: "   "}, false},
		{"whitespace-only instructions are inactive", reviewer{instructions: "\t \n"}, false},
		{"whitespace both is inactive", reviewer{skill: "  ", instructions: "  "}, false},
		{"enabled overrides blank fields", reviewer{enabled: true, skill: "  "}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.active(); got != tt.want {
				t.Errorf("reviewer%+v.active() = %v, want %v", tt.r, got, tt.want)
			}
		})
	}
}
