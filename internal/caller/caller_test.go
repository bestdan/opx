package caller_test

import (
	"strings"
	"testing"

	"github.com/bestdan/opx/internal/caller"
)

func TestName_NonEmpty(t *testing.T) {
	name := caller.Name()
	if name == "" {
		t.Error("caller.Name() returned empty string, want a process name")
	}
}

func TestName_NoSlash(t *testing.T) {
	// The returned name must be a plain executable name, not a path.
	name := caller.Name()
	if strings.Contains(name, "/") {
		t.Errorf("caller.Name() = %q, must not contain a path separator", name)
	}
}

func TestDescribe_NonEmpty(t *testing.T) {
	desc := caller.Describe()
	if desc == "" {
		t.Error("caller.Describe() returned empty string, want a label")
	}
}

func TestIsUninteresting(t *testing.T) {
	cases := []struct {
		comm string
		want bool
	}{
		{"sh", true},
		{"bash", true},
		{"zsh", true},
		{"fish", true},
		{"dash", true},
		{"ksh", true},
		{"csh", true},
		{"tcsh", true},
		{"login", true},
		{"ghostty", true},
		{"iterm2", true},
		{"iterm", true},
		{"Terminal", true},
		{"alacritty", true},
		{"kitty", true},
		{"wezterm", true},
		{"wezterm-gui", true},
		{"warp", true},
		{"warpterminal", true},
		{"hyper", true},
		{"tmux", true},
		{"tmux: server", true},
		{"tmux: client", true},
		{"screen", true},
		{"konsole", true},
		{"gnome-terminal", true},
		{"xterm", true},
		{"urxvt", true},
		{"st", true},
		{"foot", true},
		{"code", true},
		{"cursor", true},
		{"windsurf", true},
		{"python3", false},
		{"node", false},
		{"claude", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.comm, func(t *testing.T) {
			if got := caller.IsUninterestingForTest(tc.comm); got != tc.want {
				t.Errorf("IsUninterestingForTest(%q) = %v, want %v", tc.comm, got, tc.want)
			}
		})
	}
}

func TestIsUninteresting_CaseInsensitive(t *testing.T) {
	cases := []string{"GHOSTTY", "iTerm2", "ITERM2", "TERMINAL", "Alacritty", "KITTY", "Tmux", "CODE"}
	for _, comm := range cases {
		t.Run(comm, func(t *testing.T) {
			if !caller.IsUninterestingForTest(comm) {
				t.Errorf("IsUninterestingForTest(%q) = false, want true (case-insensitive match)", comm)
			}
		})
	}
}

func TestRenderArgv(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "basename argv0 only",
			argv: []string{"/usr/bin/python3"},
			want: "python3",
		},
		{
			name: "path args shortened",
			argv: []string{"/usr/bin/python3", "/home/user/linear-archive.py", "--team", "PreThink"},
			want: "python3 linear-archive.py --team PreThink",
		},
		{
			name: "non-path args kept as-is",
			argv: []string{"node", "--flag", "value"},
			want: "node --flag value",
		},
		{
			name: "single arg no path",
			argv: []string{"claude"},
			want: "claude",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := caller.RenderArgvForTest(tc.argv); got != tc.want {
				t.Errorf("RenderArgvForTest(%v) = %q, want %q", tc.argv, got, tc.want)
			}
		})
	}
}

func TestDescribeArgv_SingleTokenNoPrefix(t *testing.T) {
	// A single-token argv (no arguments) has nothing to describe beyond the
	// name the dialog header already shows, so no ancestor prefix should be
	// added even when an interesting ancestor is present above it.
	got := caller.DescribeArgvForTest([]string{"claude"}, []string{"ghostty", "login-wrapper"})
	if got != "claude" {
		t.Errorf("DescribeArgvForTest with single-token argv = %q, want %q (no ancestor prefix)", got, "claude")
	}
}

func TestDescribeArgv_MultiTokenGetsPrefix(t *testing.T) {
	got := caller.DescribeArgvForTest(
		[]string{"/usr/bin/python3", "/home/user/linear-archive.py", "--team", "PreThink"},
		[]string{"ghostty", "claude"},
	)
	want := "claude › python3 linear-archive.py --team PreThink"
	if got != want {
		t.Errorf("DescribeArgvForTest = %q, want %q", got, want)
	}
}

func TestDescribeArgv_SkipsUninterestingAncestors(t *testing.T) {
	got := caller.DescribeArgvForTest(
		[]string{"node", "--flag"},
		[]string{"ghostty", "tmux", "bash"},
	)
	want := "node --flag"
	if got != want {
		t.Errorf("DescribeArgvForTest = %q, want %q (all ancestors uninteresting)", got, want)
	}
}

func TestRenderCommand(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "shortens paths",
			argv: []string{"/usr/bin/python3", "/home/user/linear-archive.py", "--team", "PreThink", "--older-than", "1"},
			want: "python3 linear-archive.py --team PreThink --older-than 1",
		},
		{
			name: "truncates at 120",
			argv: []string{"cmd", strings.Repeat("x", 130)},
			want: "cmd " + strings.Repeat("x", 115) + "…",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := caller.RenderCommand(tc.argv)
			if got != tc.want {
				t.Errorf("RenderCommand(%v) = %q, want %q", tc.argv, got, tc.want)
			}
			if n := len([]rune(got)); n > 120 {
				t.Errorf("RenderCommand result has %d runes, want <= 120", n)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"under limit unchanged", "short string", 120, "short string"},
		{"exact limit unchanged", strings.Repeat("a", 120), 120, strings.Repeat("a", 120)},
		{"over limit cut with ellipsis", strings.Repeat("a", 130), 120, strings.Repeat("a", 119) + "…"},
		{"empty string", "", 120, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := caller.TruncateForTest(tc.s, tc.n)
			if got != tc.want {
				t.Errorf("TruncateForTest(%q, %d) = %q, want %q", tc.s, tc.n, got, tc.want)
			}
			if n := len([]rune(got)); n > tc.n {
				t.Errorf("TruncateForTest result has %d runes, want <= %d", n, tc.n)
			}
		})
	}
}
