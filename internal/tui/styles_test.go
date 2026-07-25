package tui

import "testing"

func TestExitCodeStyle(t *testing.T) {
	tests := []struct {
		name string
		code int
		want string // rendered output of the expected style, for comparison
	}{
		{"zero exit uses success style", 0, successStyle.Render("x")},
		{"nonzero exit uses error style", 1, errorStyle.Render("x")},
		{"negative exit uses error style", -1, errorStyle.Render("x")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := exitCodeStyle(tt.code).Render("x")
			if got != tt.want {
				t.Errorf("exitCodeStyle(%d).Render(x) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}
