// Package caller identifies the process that invoked opx.
package caller

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// maxWalk bounds how many ancestors we climb looking for a non-shell process,
// and how many `ps` calls we're willing to make.
const maxWalk = 6

// uninteresting is the set of executable names treated as "just a container
// for a human's session" — shells, terminal emulators, and multiplexers —
// not interesting enough to show in a dialog on their own. Matched
// case-insensitively (see isUninteresting) since macOS `ps` reports mixed
// case for some of these ("Terminal", "iTerm2").
var uninteresting = map[string]bool{
	"sh":             true,
	"bash":           true,
	"zsh":            true,
	"fish":           true,
	"dash":           true,
	"ksh":            true,
	"csh":            true,
	"tcsh":           true,
	"login":          true,
	"ghostty":        true,
	"iterm2":         true,
	"iterm":          true,
	"terminal":       true,
	"alacritty":      true,
	"kitty":          true,
	"wezterm":        true,
	"wezterm-gui":    true,
	"warp":           true,
	"warpterminal":   true,
	"hyper":          true,
	"tmux":           true,
	"tmux: server":   true,
	"tmux: client":   true,
	"screen":         true,
	"konsole":        true,
	"gnome-terminal": true,
	"xterm":          true,
	"urxvt":          true,
	"st":             true,
	"foot":           true,
	"code":           true,
	"cursor":         true,
	"windsurf":       true,
}

// isUninteresting reports whether comm names a shell, terminal emulator, or
// multiplexer — a container process not worth showing in a dialog on its
// own. Matching is case-insensitive.
func isUninteresting(comm string) bool {
	return uninteresting[strings.ToLower(comm)]
}

// process is one ancestor: its pid, executable basename, and (when known)
// its argv.
type process struct {
	pid  int
	comm string
	argv []string // nil if unavailable
}

// Name returns a short label for the dialog title: the basename of the
// nearest ancestor process that is not a shell/terminal/multiplexer, walking
// up to maxWalk levels. If every ancestor in range is uninteresting (or the
// walk fails), it falls back to the immediate parent's executable name, and
// finally to "unknown".
func Name() string {
	chain := ancestorChain(os.Getppid(), maxWalk)
	if p := firstInteresting(chain); p != nil {
		return p.comm
	}
	if len(chain) > 0 {
		return chain[0].comm
	}
	return "unknown"
}

// Describe returns a one-line human label for the dialog body: the subject
// process's argv (path-shortened), optionally prefixed with a further
// non-shell/terminal/multiplexer ancestor above it, truncated to 120
// characters. When argv is unavailable it falls back to the subject's
// executable name — the same value Name() picks, derived from the chain
// already in hand rather than by walking again. When the subject's argv is a
// single token (no arguments), there is nothing to describe beyond the name
// Name() already returns, so no ancestor prefix is added — that leaves the
// caller (callerDetailLine) able to suppress the line entirely as
// duplicative.
func Describe() string {
	chain := ancestorChain(os.Getppid(), maxWalk)
	subject := firstInteresting(chain)
	if subject == nil {
		if len(chain) > 0 {
			subject = &chain[0]
		} else {
			return "unknown"
		}
	}
	if len(subject.argv) == 0 {
		return subject.comm
	}

	// aboveComms is the comm names of ancestors above the subject, nearest
	// first — used to look for a further non-shell/terminal/multiplexer
	// ancestor worth prefixing onto the description.
	var aboveComms []string
	for i := range chain {
		if chain[i].pid == subject.pid {
			for j := i + 1; j < len(chain); j++ {
				aboveComms = append(aboveComms, chain[j].comm)
			}
			break
		}
	}

	return describeArgv(subject.argv, aboveComms)
}

// describeArgv renders a subject's argv, optionally prefixed with the first
// non-shell/terminal/multiplexer entry in aboveComms, truncated to 120
// characters. When argv is a single token (no arguments), there is nothing
// to describe beyond the name Name() already returns, so no ancestor prefix
// is added regardless of aboveComms — that leaves the caller
// (callerDetailLine) able to suppress the line entirely as duplicative.
func describeArgv(argv []string, aboveComms []string) string {
	desc := renderAncestorArgv(argv)
	if len(argv) == 1 {
		return truncate(desc, 120)
	}
	for _, comm := range aboveComms {
		if !isUninteresting(comm) {
			desc = comm + " › " + desc
			break
		}
	}
	return truncate(desc, 120)
}

// maxChildCommand bounds the arguments rendered after argv[0] — not the whole
// line. Once appending the next argument would push the string past it, the
// remaining arguments are replaced by an explicit " … +N more argument(s)"
// count. Two deliberate exemptions put the final string above it: argv[0] is
// never truncated, and the elision suffix is appended after the check.
//
// Wider than the 120 used for an ancestor description because this line is an
// authorization decision rather than context: macOS `display dialog` scrolls a
// long body, so the extra width is cheap. It stays bounded because an
// unreadable wall of text is its own denial of review.
const maxChildCommand = 300

// RenderCommand renders a to-be-spawned command's argv for the `opx run`
// confirmation dialog.
//
// This rendering is an authorization statement, not a summary: it is the only
// place the user learns which process is about to receive the plaintext
// secrets. It therefore keeps full paths and full argument values, quoting
// anything whose boundaries would otherwise be ambiguous, and when it must
// drop arguments it says how many rather than trailing off. Do NOT reunify it
// with renderAncestorArgv — abbreviation is a readability win when describing
// a process that already ran, and a lie when describing one about to be
// handed secrets. `/tmp/evil/curl` shortened to `curl`, or
// `https://evil.example/collect` shortened to `collect`, is exactly the part
// of the decision the user needed.
//
// Control characters are not escaped here; prompt.callerDetailLine runs the
// whole detail line through sanitizeDisplay before it reaches the terminal.
func RenderCommand(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	// argv[0] is the single most decision-relevant token, so it is never
	// truncated: a long path is shown whole even if no arguments fit beside it.
	rendered := quoteForDisplay(argv[0])
	for i := 1; i < len(argv); i++ {
		next := rendered + " " + quoteForDisplay(argv[i])
		if len([]rune(next)) > maxChildCommand {
			return fmt.Sprintf("%s … +%d more argument%s", rendered, len(argv)-i, plural(len(argv)-i))
		}
		rendered = next
	}
	return rendered
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// quoteForDisplay wraps s in double quotes when its boundaries would
// otherwise be unreadable — whitespace, an embedded quote or backslash, or an
// empty argument. A `sh -c '<script>'` payload has to read as one argument, or
// the dialog implies a shape the child does not have.
//
// Backslashes are escaped alongside quotes, and their presence alone is enough
// to trigger quoting. Escaping `"` while passing `\` through unchanged renders
// an argument containing `\"` as `\\"`, which a shell-literate reader parses as
// an escaped backslash followed by a closing quote — so a single argument
// reads as two, and the token that looks like the destination is not the one
// the child receives. That is the same lie about the child's shape this
// function exists to prevent, told one level down.
func quoteForDisplay(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\n\"'\\") {
		return s
	}
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	return `"` + strings.ReplaceAll(escaped, `"`, `\"`) + `"`
}

// renderAncestorArgv renders a subject's argv as: basename of argv[0], then
// the remaining args with any path-looking argument (contains "/") reduced to
// its basename, space-joined.
//
// Only for describing an ancestor, where the process has already run and
// brevity aids readability. See RenderCommand for the child case.
func renderAncestorArgv(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if i == 0 || strings.Contains(a, "/") {
			parts[i] = basename(a)
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}

// truncate cuts s to at most n characters (runes), appending "…" when cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 0 {
		return ""
	}
	return string(r[:n-1]) + "…"
}

// firstInteresting returns the first process in chain whose comm is not a
// shell/terminal/multiplexer, or nil if none qualifies.
func firstInteresting(chain []process) *process {
	for i := range chain {
		if !isUninteresting(chain[i].comm) {
			return &chain[i]
		}
	}
	return nil
}

// basename returns the final path component of s, or s unchanged if it
// contains no "/".
func basename(s string) string {
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

// ancestorChain walks up to n ancestors starting at pid (inclusive) via `ps`,
// returning as much as it could determine. Only the subject entry — the first
// interesting process, or chain[0] when none qualifies — gets its argv
// populated; every other entry's argv stays nil, since the argv is only ever
// used to describe the subject. Failures degrade gracefully rather than
// aborting the walk. Bounded to at most n+1 `ps` calls total: n for the
// ppid/comm walk, plus 1 for the subject's argv.
func ancestorChain(pid, n int) []process {
	var chain []process
	cur := pid
	for i := 0; i < n && cur > 1; i++ {
		ppid, comm, ok := psPPIDComm(cur)
		if !ok {
			break
		}
		chain = append(chain, process{pid: cur, comm: comm})
		cur = ppid
	}
	if p := firstInterestingIdx(chain); p >= 0 {
		if argv, ok := psArgv(chain[p].pid); ok {
			chain[p].argv = argv
		}
	} else if len(chain) > 0 {
		if argv, ok := psArgv(chain[0].pid); ok {
			chain[0].argv = argv
		}
	}
	return chain
}

func firstInterestingIdx(chain []process) int {
	for i := range chain {
		if !isUninteresting(chain[i].comm) {
			return i
		}
	}
	return -1
}

// psCandidates is the only set of locations ps is accepted from. Absolute by
// construction: PATH belongs to the process opx is about to describe to the
// user, so a `ps` found there is the caller writing its own dialog. `ps` lives
// at /bin/ps on macOS; /usr/bin/ps is a second candidate for robustness and
// costs one stat.
var psCandidates = []string{"/bin/ps", "/usr/bin/ps"}

// psEnv is the environment both ps invocations run with, replacing the one opx
// inherited. LC_ALL=C additionally pins ps's column formatting, which an
// inherited locale does not guarantee.
var psEnv = []string{"PATH=/bin:/usr/bin", "LC_ALL=C"}

// psPath returns the first candidate that is a regular, executable file which
// is not group- or world-writable, or "" when none qualifies. Callers treat ""
// as a lookup failure, which degrades to a caller identity of "unknown".
//
// This is a deliberate duplicate of resolveHelper in internal/prompt rather
// than a shared helper: caller importing prompt would invert the dependency
// (main composes both), and the two candidate lists have no reason to move
// together.
//
// The check rejects a ps another account could have written to directly. It
// does not examine the containing directory, so a clean file in a writable
// directory is accepted even though it could be replaced by unlink-and-
// recreate. Both real candidates are on the SIP-sealed system volume, where no
// account can unlink or recreate anything, so reaching that gap means SIP is
// already defeated.
func psPath(candidates []string) string {
	for _, path := range candidates {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		mode := info.Mode().Perm()
		if mode&0o111 == 0 || mode&0o022 != 0 {
			continue
		}
		return path
	}
	return ""
}

func psPPIDComm(pid int) (ppid int, comm string, ok bool) {
	ps := psPath(psCandidates)
	if ps == "" {
		return 0, "", false
	}
	cmd := exec.Command(ps, "-o", "ppid=,comm=", "-p", strconv.Itoa(pid))
	cmd.Env = psEnv
	out, err := cmd.Output()
	if err != nil {
		return 0, "", false
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 2 {
		return 0, "", false
	}
	ppid, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, "", false
	}
	comm = basename(strings.Join(fields[1:], " "))
	return ppid, comm, true
}

func psArgv(pid int) ([]string, bool) {
	ps := psPath(psCandidates)
	if ps == "" {
		return nil, false
	}
	cmd := exec.Command(ps, "-o", "args=", "-p", strconv.Itoa(pid))
	cmd.Env = psEnv
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return nil, false
	}
	return strings.Fields(s), true
}
