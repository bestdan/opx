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
// trusted program sits above it in the chain. Treat additions to
// `uninteresting` as widening what can be laundered through, not as tidying a
// display list.
//
// Membership is no longer sufficient to skip a process — see skippable, which
// requires the kernel to corroborate the claim — and skipping is no longer
// silent, since Identity.Through discloses what was walked past.
func isUninteresting(comm string) bool {
	return uninteresting[strings.ToLower(comm)]
}

// skippable reports whether p is a container process the subject walk may pass
// over: it claims to be a shell/terminal/multiplexer *and* the kernel does not
// contradict the claim.
//
// The gate is disagreement, not list membership. `comm` comes from the
// process's own arguments (see parsePPIDComm), so membership alone lets any
// binary opt into being skipped by calling itself `bash`. Requiring
// basename(exe) to match closes the pure argv[0]-forgery case: a process whose
// comm says `bash` while the kernel says /tmp/evil is not skipped, it becomes
// the subject and is named at its real path.
//
// An unreadable path skips. That is deliberate and it is the weaker half:
// `lsof` returns nothing for a process owned by another user, so a strict
// "unconfirmed ⇒ interesting" rule would make an ordinary `zsh ← login`
// terminal attribute reads to "login" (verified: login runs as uid 0 and
// returns no txt vnode to a same-user caller). The cost is that a process which
// unlinks its own binary after exec skips on the same terms. Neither branch is
// a security control; what makes skipping safe to get wrong is that the skipped
// process is disclosed, not hidden — see throughPaths.
func (p process) skippable() bool {
	if !isUninteresting(p.comm) {
		return false
	}
	if p.exe == "" {
		return true
	}
	return strings.EqualFold(basename(p.exe), p.comm)
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

	// Through is the chain of processes between the named subject and opx that
	// the subject walk passed over, at their kernel-reported paths, ordered
	// nearest-to-opx last. An entry whose path could not be read is the literal
	// "unknown"; entries under SIP-sealed prefixes are omitted (see
	// throughPaths). Empty means nothing was walked past that the user needs to
	// see — not that nothing was walked past.
	//
	// It exists because subject selection cannot be made trustworthy: being a
	// real shell is compatible with being the malware, so no rule over
	// executable identity answers "is this ancestor just a container?". What can
	// be made true is that nothing between the subject and opx is *silently*
	// absorbed. Every entry here is kernel-reported or explicitly unknown —
	// nothing on this line is self-asserted, so a reader does not have to
	// adjudicate which entries to believe.
	//
	// Entries are display strings, not raw paths: each is bounded and quoted so
	// that one entry cannot read as two. See throughPaths.
	Through []string
}

// Current resolves the calling process once and returns everything the dialog
// needs to describe it. One walk per call: Name, Detail and Through are
// guaranteed to describe the same process and the same chain, which independent
// walks could not promise.
func Current() Identity {
	return identityFromChain(ancestorChain(os.Getppid(), maxWalk))
}

// identityFromChain picks the subject out of chain and assembles its Identity.
// Split out of Current so the whole selection-and-rendering path is exercisable
// without a live process tree — the test hook calls this, not a copy of it.
func identityFromChain(chain []process) Identity {
	idx := subjectIdx(chain)
	if idx < 0 {
		return Identity{Name: "unknown", Detail: "unknown"}
	}
	return identityOf(chain[idx], chain, idx)
}

// identityOf assembles the Identity for the subject at chain[idx].
func identityOf(subject process, chain []process, idx int) Identity {
	return Identity{
		Name:    identityName(subject.exe, subject.comm),
		Path:    subject.exe,
		Detail:  describeSubject(subject, chain),
		Through: throughPaths(chain, idx),
	}
}

// sipSealedPrefixes are the location prefixes on the SIP-sealed system volume.
// Verified on macOS 26.4: a write to /bin, /sbin, /usr/bin, /usr/sbin,
// /usr/libexec or /System fails with "Operation not permitted" even for root.
// /usr/local is deliberately absent — it is merely root-owned ("Permission
// denied"), so an attacker with root, or a Homebrew-writable prefix, can occupy
// it.
var sipSealedPrefixes = []string{
	"/bin/",
	"/sbin/",
	"/usr/bin/",
	"/usr/sbin/",
	"/usr/libexec/",
	"/System/",
}

func isSIPSealed(path string) bool {
	for _, prefix := range sipSealedPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// throughPaths renders the ancestors between the subject at chain[idx] and opx
// — the ones the subject walk passed over — nearest-to-opx last, so the list
// reads in the direction control flowed towards opx.
//
// Two rules, and the second is a deliberate tradeoff worth stating rather than
// discovering:
//
//   - An ancestor whose path could not be read renders as "unknown", never
//     omitted. Omission is what makes laundering work; an entry the user cannot
//     identify is still an entry they can see.
//   - An ancestor under a SIP-sealed prefix is omitted, which keeps the common
//     case (`opx` typed into a terminal, every skipped ancestor a stock shell)
//     free of a line that would appear on every read and teach the user to skim
//     past it. That is not a location allowlist of the kind opx rejects
//     elsewhere: SIP paths are the one set of locations an attacker cannot
//     occupy, so suppressing exactly those is a fact about the platform rather
//     than a judgment about the user's layout.
//
// What suppression hides is `zsh evil.sh`: the kernel text vnode is genuinely
// /bin/zsh, so the laundering succeeds with no forgery anywhere and no entry on
// this line. Showing the skipped shell's argv instead (`through zsh evil.sh`)
// was considered and rejected. argv is self-asserted, so it would put a string
// the attacker chooses on the one line whose value is that everything on it is
// kernel-true — `through zsh deploy.sh` reads as reassurance while naming a
// script that need not exist. Its coverage is thin regardless (a script fed on
// stdin, or an argv[0] forged to a single token, evades it), and the underlying
// gap is not closable by disclosure at all: an interpreted script has no kernel
// identity to report. It needs an attestation primitive (code-signing identity
// via csops), which needs cgo, which `make cross` forbids.
// Entries are bounded and quoted rather than emitted raw, in that order.
// Neither step is cosmetic:
//
//   - The line is assembled by joining entries with " › ", and a path may
//     contain that separator. A shell planted in a directory the user created
//     as `.../Downloads › /bin` yields the kernel-true path
//     `/Users/x/Downloads › /bin/zsh`, which renders as *two* entries, the
//     second reading as a SIP-sealed /bin/zsh. Every path shown would still be
//     genuine; the entry count and one entry's apparent trustworthiness would
//     not be. quoteForDisplay triggers on the space in any forged separator and
//     escapes `\` alongside `"`, so the entry cannot forge its own closing
//     quote either. sanitizeDisplay does not cover this — `›` is printable.
//   - The path is attacker-influenced (a process chooses where it lives, and
//     how deeply nested), and every other caller-controlled display string here
//     is bounded: 120 runes for an ancestor description, maxChildCommand for
//     the child. The op:// URI the user is actually approving is rendered
//     *after* this block, so an unbounded line pushes the decision off the
//     visible dialog. Truncation precedes quoting so the closing quote is never
//     the character that gets cut.
func throughPaths(chain []process, idx int) []string {
	var out []string
	for i := idx - 1; i >= 0; i-- {
		switch {
		case chain[i].exe == "":
			out = append(out, "unknown")
		case isSIPSealed(chain[i].exe):
			continue
		default:
			out = append(out, quoteForDisplay(truncate(chain[i].exe, 120)))
		}
	}
	return out
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
// exePaths — never the forgeable one `ps` prints), since distinguishing
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

// subjectIdx returns the index of the process the dialog names: the nearest
// ancestor the walk will not pass over (see skippable). When every ancestor in
// range is skippable it falls back to the immediate parent — naming the process
// that actually invoked opx is more honest than naming nothing — and returns -1
// only when the walk found no process at all.
func subjectIdx(chain []process) int {
	for i := range chain {
		if !chain[i].skippable() {
			return i
		}
	}
	if len(chain) > 0 {
		return 0
	}
	return -1
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
// returning as much as it could determine. Every entry gets its executable path
// resolved from the kernel (see exePaths) — the subject's because the dialog
// names it, the rest because the subject *choice* now depends on the path (see
// skippable) and because the ones walked past are disclosed. Only the subject
// entry gets its argv populated; every other entry's argv stays nil, since the
// argv is only ever used to describe the subject. Failures degrade gracefully
// rather than aborting the walk. Bounded to at most n+1 `ps` calls plus 1
// `lsof` call: n for the ppid/comm walk, 1 for the subject's argv, and a single
// batched path lookup for the whole chain.
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
	if len(chain) == 0 {
		return chain
	}
	pids := make([]int, len(chain))
	for i := range chain {
		pids[i] = chain[i].pid
	}
	exes := exePaths(pids)
	for i := range chain {
		chain[i].exe = exes[chain[i].pid]
	}
	// The subject is resolved here only to decide whose argv to fetch; the
	// selection itself is redone by identityFromChain against the finished
	// chain, so the two cannot disagree about which process is the subject.
	if idx := subjectIdx(chain); idx >= 0 {
		if argv, ok := psArgv(chain[idx].pid); ok {
			chain[idx].argv = argv
		}
	}
	return chain
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
// process's own arguments and is forgeable — see exePaths, which is where the
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

// exePaths returns the executable path the kernel has for each of pids, keyed
// by pid. A pid whose path could not be read is absent from the map, which
// callers read as "" — the same honest gap a single lookup would produce.
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
// by another user. "" then flows through to an omitted dialog line or a
// "unknown" chain entry — never to a guess, and never to the `ps` value this
// function exists to distrust.
//
// The whole chain is resolved in one call. Measured on macOS 26.4: five pids in
// 37 ms, against the 0.86 s the dialog already spends on `beep 3`, so the batch
// is about not multiplying a cheap cost rather than about a cost that mattered.
func exePaths(pids []int) map[int]string {
	lsof := resolveTool(lsofCandidates)
	if lsof == "" || len(pids) == 0 {
		return nil
	}
	list := make([]string, len(pids))
	for i, pid := range pids {
		list[i] = strconv.Itoa(pid)
	}
	// -F fn emits machine-readable field lines; -d txt -a restricts them to
	// text-vnode entries. Apple's lsof (4.91) emits the `f` field whether or
	// not it is requested — it delimits each file set — so `-F n` and `-F fn`
	// are byte-identical there; `f` is named explicitly because the parser
	// depends on it and an implicit field is a silent dependency. `p` is what
	// splits the output per process, and unlike `f` it is only emitted for a
	// multi-pid request, so it is genuinely required here.
	cmd := exec.Command(lsof, "-p", strings.Join(list, ","), "-F", "pfn", "-a", "-d", "txt")
	cmd.Env = toolEnv
	out, _ := cmd.Output()
	// The exit status is deliberately ignored. Verified on macOS 26.4: lsof
	// exits 1 — silently, with nothing on stderr — when *any* requested pid
	// yields no matching file, while still printing complete sections for the
	// pids it did resolve. A root-owned `login` in an ordinary terminal chain is
	// enough to trigger it, so treating non-zero as failure would discard every
	// path in the chain in the common case. Partial output is the normal case,
	// not a degraded one; the parser accepts only well-formed absolute paths, so
	// a truncated or garbled stdout yields fewer entries rather than wrong ones.
	return parseLsofTxt(string(out))
}

// parseLsofTxt extracts each process's executable path from
// `lsof -F pfn -d txt` output, keyed by pid. The format is a `p<pid>` line
// introducing a section, followed by repeating `f<fd>` / `n<name>` pairs.
//
// The executable is the first txt entry in a section; everything after it is
// some other text-mapped file — dyld, shared libraries, and (observed on zsh)
// locale data such as /usr/share/locale/en_US.UTF-8/LC_COLLATE. So the first
// name after the first `ftxt` wins per process, and taking a later one would
// name a data file as the caller. Sections arrive in lsof's own order, not the
// order the pids were requested in, which is why the result is a map.
//
// Only absolute paths are accepted: anything else means the output was not
// what this parser expects, and a half-understood path is worse than none. A
// process whose section carries no usable name is left out of the map entirely
// rather than mapped to "", so there is one representation of "unknown".
//
// Split out from exePaths so the parsing is testable without a real lsof.
func parseLsofTxt(out string) map[int]string {
	paths := make(map[int]string)
	cur, seenTxt, done := 0, false, false
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "p") {
			pid, err := strconv.Atoi(line[1:])
			if err != nil {
				// An unparseable section header means the rest of the section
				// cannot be attributed to a pid; skip to the next header rather
				// than folding its files into the previous process.
				cur, seenTxt, done = 0, false, true
				continue
			}
			cur, seenTxt, done = pid, false, false
			continue
		}
		if done || cur == 0 {
			continue
		}
		switch {
		case line == "ftxt":
			// A second txt record before any name means the first one carried
			// no usable name. Falling through to this one would name a linked
			// library as the caller, so give up on this process instead — the
			// whole point of this lookup is to not misattribute the request.
			if seenTxt {
				done = true
				continue
			}
			seenTxt = true
		case seenTxt && strings.HasPrefix(line, "n"):
			if path := line[1:]; strings.HasPrefix(path, "/") {
				paths[cur] = path
			}
			done = true
		}
	}
	return paths
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
