package loop

import "testing"

// TestReviewerActive verifies the predicate that gates the post-commit review
// agent: enabled decides on its own. Whether a skill or free-form instructions
// should switch the review on is resolved upstream in config.ReviewConfig.Active(),
// so a skill left in the config can never override an off switch here.
func TestReviewerActive(t *testing.T) {
	tests := []struct {
		name string
		r    reviewer
		want bool
	}{
		{"zero value is inactive", reviewer{}, false},
		{"enabled flag activates", reviewer{enabled: true}, true},
		{"skill alone does not activate", reviewer{skill: "/code-quality"}, false},
		{"instructions alone do not activate", reviewer{instructions: "watch for N+1"}, false},
		{"disabled beats skill and instructions", reviewer{skill: "/cq", instructions: "watch for N+1"}, false},
		{"enabled with blank fields", reviewer{enabled: true, skill: "  "}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.active(); got != tt.want {
				t.Errorf("reviewer%+v.active() = %v, want %v", tt.r, got, tt.want)
			}
		})
	}
}
