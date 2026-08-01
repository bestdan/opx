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
//
// This is an **advisory display heuristic, not an identity check and not a
// security control**. It matches a self-asserted executable basename against a
// fixed list, so any process can opt into being skipped simply by being named
// `bash`. Doing so does not hide the request — it re-attributes it: the walk
// continues past the skipped process and the dialog names whatever genuinely
// trusted program sits above it in the chain. That is the point of showing
// Identity.Path alongside the name; the path comes from the kernel (see
// exePath) and is the one part of the identity the process cannot choose for
// itself. Treat additions to `uninteresting` as widening what can be laundered
// through, not as tidying a display list.
func isUninteresting(comm string) bool {
	return uninteresting[strings.ToLower(comm)]
}

// process is one ancestor: its pid, self-asserted executable basename, the
// kernel-reported executable path when it could be read, and (when known) its
// argv.
type process struct {
	pid  int
	comm string
	exe  string   // kernel-reported executable path; "" when unavailable
	argv []string // nil if unavailable
}

// Identity is the process opx attributes a secret request to. Name is short
// enough for the dialog header; Path is where the executable actually lives,
// which is what distinguishes /usr/local/bin/claude from ~/.cache/claude.
//
// Only Path is trustworthy. Name falls back to a self-asserted string when no
// path could be read, and Detail contains the process's own argv. Path is ""
// when the kernel would not tell us — an honest gap, and the dialog omits the
// line rather than implying a location was checked.
//
// opx deliberately does not classify Path as "expected" or "suspicious". A
// location allowlist gets this wrong in the direction that matters: real Claude
// Code lives under ~/.local/share/claude/versions/, npm tools under
// ~/.npm-global, cargo binaries under ~/.cargo/bin — so the marker would fire
// constantly on legitimate callers and teach the user to click through it. The
// path itself is the disclosure.
type Identity struct {
	Name   string
	Path   string
	Detail string
}

// Current resolves the calling process once and returns everything the dialog
// needs to describe it. One walk per call: Name and Detail are guaranteed to
// describe the same process, which two independent walks could not promise.
func Current() Identity {
	chain := ancestorChain(os.Getppid(), maxWalk)
	subject := firstInteresting(chain)
	if subject == nil {
		if len(chain) == 0 {
			return Identity{Name: "unknown", Detail: "unknown"}
		}
		subject = &chain[0]
	}
	return identityOf(*subject, chain)
}

// identityOf assembles the Identity for subject, given the chain it came from.
// Split out of Current so the whole rendering path is exercisable without a
// live process tree — the test hook calls this, not a copy of it.
func identityOf(subject process, chain []process) Identity {
	return Identity{
		Name:   identityName(subject.exe, subject.comm),
		Path:   subject.exe,
		Detail: describeSubject(subject, chain),
	}
}

// identityName picks the short header label. It prefers exe's basename over
// comm: comm is what the process says it is, exe is where the kernel says it
// lives. They agree for every honest caller — a disagreement is the
// impersonation signal, and the verifiable half wins.
func identityName(exe, comm string) string {
	if exe != "" {
		return basename(exe)
	}
	return comm
}

// Name returns a short label for the dialog title: the basename of the
// nearest ancestor process that is not a shell/terminal/multiplexer, walking
// up to maxWalk levels. If every ancestor in range is uninteresting (or the
// walk fails), it falls back to the immediate parent's executable name, and
// finally to "unknown".
//
// The label is derived from the kernel-reported executable path rather than
// from the process's self-asserted comm — see identityName. It stays short: the
// full path travels in Identity.Path, which is where the dialog shows it.
func Name() string {
	return Current().Name
}

// Describe returns a one-line human label for the dialog body: the subject
// process's argv, optionally prefixed with a further
// non-shell/terminal/multiplexer ancestor above it, truncated to 120
// characters. argv[0] renders as the kernel-reported executable path (see
// exePath — never the forgeable one `ps` prints), since distinguishing
// /usr/local/bin/claude from ~/.cache/claude is the entire point, while the
// remaining arguments stay path-shortened. When argv is
// unavailable it falls back to the executable path, or to the subject's comm
// when even that is unknown. When the subject's argv is a single token (no
// arguments), no ancestor prefix is added — that leaves the caller
// (callerDetailLine) able to suppress the line as duplicative, which it now
// only does when there is no path to add.
func Describe() string {
	return Current().Detail
}

// describeSubject renders the detail line for subject, given the chain it came
// from. Split out of Current so the rendering is exercisable without a live
// process tree.
func describeSubject(subject process, chain []process) string {
	if len(subject.argv) == 0 {
		if subject.exe != "" {
			return truncate(subject.exe, 120)
		}
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

	return describeArgv(subject.exe, subject.argv, aboveComms)
}

// describeArgv renders a subject's argv, optionally prefixed with the first
// non-shell/terminal/multiplexer entry in aboveComms, truncated to 120
// characters. When argv is a single token (no arguments), no ancestor prefix
// is added regardless of aboveComms — that leaves the caller
// (callerDetailLine) able to suppress the line as duplicative, which with a
// path present it no longer will.
//
// exe, when non-empty, replaces argv[0]'s rendering with the full executable
// path: argv[0] is self-asserted (a process chooses its own argv), exe is
// where the kernel says it actually lives.
func describeArgv(exe string, argv []string, aboveComms []string) string {
	desc := renderAncestorArgv(argv)
	if exe != "" {
		// argv[1:] would panic on an empty argv. No production call site
		// reaches here with one — describeSubject checks first — but the test
		// hook does, and a helper that panics on an empty slice is a trap for
		// the next caller.
		var args []string
		if len(argv) > 0 {
			args = argv[1:]
		}
		desc = strings.TrimSpace(exe + " " + renderAncestorArgs(args))
	}
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
	if len(argv) == 0 {
		return ""
	}
	return strings.TrimSpace(basename(argv[0]) + " " + renderAncestorArgs(argv[1:]))
}

// renderAncestorArgs renders the arguments after argv[0], reducing any
// path-looking argument to its basename. Split out so a caller that has a
// verified executable path can substitute it for argv[0] without duplicating
// the argument handling — see describeArgv.
func renderAncestorArgs(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		if strings.Contains(a, "/") {
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
// used to describe the subject. The subject also gets its executable path
// resolved from the kernel (see exePath); no other entry does, since no other
// entry is named in the dialog. Failures degrade gracefully rather than
// aborting the walk. Bounded to at most n+1 `ps` calls plus 1 `lsof` call: n
// for the ppid/comm walk, 1 for the subject's argv, 1 for its path.
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
	subject := firstInterestingIdx(chain)
	if subject < 0 {
		if len(chain) == 0 {
			return chain
		}
		subject = 0
	}
	if argv, ok := psArgv(chain[subject].pid); ok {
		chain[subject].argv = argv
	}
	// Only the subject's path is resolved: it is the one the dialog names, and
	// one lsof call is the whole added cost of the lookup.
	chain[subject].exe = exePath(chain[subject].pid)
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

// psCandidates and lsofCandidates are the only locations each tool is accepted
// from. Absolute by construction: PATH belongs to the process opx is about to
// describe to the user, so a `ps` or `lsof` found there is the caller writing
// its own dialog. `ps` lives at /bin/ps on macOS and `lsof` at /usr/sbin/lsof;
// the second candidates are robustness and cost one stat each.
var (
	psCandidates   = []string{"/bin/ps", "/usr/bin/ps"}
	lsofCandidates = []string{"/usr/sbin/lsof", "/usr/bin/lsof"}
)

// toolEnv is the environment every identity lookup runs with, replacing the
// one opx inherited. LC_ALL=C additionally pins `ps` column formatting, which
// an inherited locale does not guarantee.
var toolEnv = []string{"PATH=/bin:/usr/bin", "LC_ALL=C"}

// resolveTool returns the first candidate that is a regular, executable file
// which is not group- or world-writable, or "" when none qualifies. Callers
// treat "" as a lookup failure, which degrades to a caller identity of
// "unknown" or an unknown executable path — never to a guess.
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
func resolveTool(candidates []string) string {
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
	ps := resolveTool(psCandidates)
	if ps == "" {
		return 0, "", false
	}
	cmd := exec.Command(ps, "-o", "ppid=,comm=", "-p", strconv.Itoa(pid))
	cmd.Env = toolEnv
	out, err := cmd.Output()
	if err != nil {
		return 0, "", false
	}
	return parsePPIDComm(string(out))
}

// parsePPIDComm splits one `ps -o ppid=,comm=` line into the parent pid and
// the executable basename.
//
// macOS `ps` prints comm as a path, but that path is derived from the
// process's own arguments and is forgeable — see exePath, which is where the
// displayed path actually comes from. What is taken from here is the ppid
// (needed to walk) and a basename (matched against `uninteresting`, itself an
// advisory heuristic). Both tolerate a self-asserted source; a displayed path
// would not.
//
// The remainder of the line is still taken verbatim rather than re-joined from
// whitespace fields: an executable can live somewhere with a space in it
// (/Applications/Alfred 5.app/Contents/MacOS/Alfred), and a Fields/Join round
// trip rewrites a run of spaces into one, which would corrupt the basename.
//
// Split out from psPPIDComm so the parsing is testable without a real `ps`.
func parsePPIDComm(out string) (ppid int, comm string, ok bool) {
	// One pid was requested, so one line is expected; take only the first so a
	// surprise second line cannot smuggle a newline into the dialog.
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	line = strings.TrimSpace(line)
	// ps right-aligns the ppid column, so the leading token ends at the first
	// space or tab; everything after it is the reported command.
	idx := strings.IndexAny(line, " \t")
	if idx < 0 {
		return 0, "", false
	}
	ppid, err := strconv.Atoi(line[:idx])
	if err != nil {
		return 0, "", false
	}
	reported := strings.TrimLeft(line[idx:], " \t")
	if reported == "" {
		return 0, "", false
	}
	return ppid, basename(reported), true
}

// exePath returns the executable path the kernel has for pid, or "" when it
// cannot be read.
//
// It exists because **macOS `ps -o comm=` is not a kernel fact**. Despite
// printing what looks like a full path, `ps` derives comm from the process's
// own arguments, so a binary at /tmp/x that execs itself with
// argv[0] = "/usr/local/bin/claude" is reported by `ps` as
// /usr/local/bin/claude. Verified directly with a forged argv[0]: `ps -o comm=`
// echoed the forgery while lsof reported the real path. Attributing a secret
// request to a path taken from `ps` would let the caller name itself, which is
// the finding this whole file is trying to close — so the path shown to the
// user comes from here, and nothing else.
//
// `lsof -d txt` reports the text (executable) vnode from the kernel's file
// table, which the process cannot rewrite. The direct route would be libproc's
// proc_pidpath(), but that needs cgo and `make cross` must stay CGO-free.
//
// Failure is normal and not an error: lsof returns nothing for a process owned
// by another user. "" then flows through to an omitted dialog line — never to
// a guess, and never to the `ps` value this function exists to distrust.
func exePath(pid int) string {
	lsof := resolveTool(lsofCandidates)
	if lsof == "" {
		return ""
	}
	// -F fn emits machine-readable field lines; -d txt -a restricts them to
	// text-vnode entries. Apple's lsof (4.91) emits the `f` field whether or
	// not it is requested — it delimits each file set — so `-F n` and `-F fn`
	// are byte-identical there; `f` is named explicitly because the parser
	// depends on it and an implicit field is a silent dependency.
	cmd := exec.Command(lsof, "-p", strconv.Itoa(pid), "-F", "fn", "-a", "-d", "txt")
	cmd.Env = toolEnv
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseLsofTxt(string(out))
}

// parseLsofTxt extracts the executable path from `lsof -F fn -d txt` output.
// The format is a `p<pid>` line followed by repeating `f<fd>` / `n<name>`
// pairs.
//
// The executable is the first txt entry; everything after it is some other
// text-mapped file — dyld, shared libraries, and (observed on zsh) locale data
// such as /usr/share/locale/en_US.UTF-8/LC_COLLATE. So the first name after
// the first `ftxt` wins, and taking a later one would name a data file as the
// caller.
//
// Only absolute paths are accepted: anything else means the output was not
// what this parser expects, and a half-understood path is worse than none.
//
// Split out from exePath so the parsing is testable without a real lsof.
func parseLsofTxt(out string) string {
	seenTxt := false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "ftxt":
			// A second txt record before any name means the first one carried
			// no usable name. Falling through to this one would name a linked
			// library as the caller, so give up instead — the whole point of
			// this lookup is to not misattribute the request.
			if seenTxt {
				return ""
			}
			seenTxt = true
		case seenTxt && strings.HasPrefix(line, "n"):
			path := line[1:]
			if strings.HasPrefix(path, "/") {
				return path
			}
			return ""
		}
	}
	return ""
}

func psArgv(pid int) ([]string, bool) {
	ps := resolveTool(psCandidates)
	if ps == "" {
		return nil, false
	}
	cmd := exec.Command(ps, "-o", "args=", "-p", strconv.Itoa(pid))
	cmd.Env = toolEnv
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
