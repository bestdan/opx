// Package prompt shows a native macOS confirmation dialog before opx reads a
// secret.  The dialog mimics the biometric unlock UI by displaying:
//   - which op:// URI(s) are being requested
//   - which environment variable each URI will be bound to (when applicable)
//   - which process is requesting it
//
// It also beeps three times, so an access attempt is audible — and
// recognizably opx — even when the dialog is on another Space or behind a
// fullscreen window.  See dialogScript for why that is unconditional.
//
// The dialog is drawn by osascript (AppleScript).  opx is macOS-only; there
// is no fallback backend.
//
// No binary on the dialog path is located via PATH. PATH belongs to the
// process opx is prompting the user about, so a caller able to put a binary
// named "osascript" ahead of the real one would choose what the dialog
// answers — and a helper that exits 0 is indistinguishable from the user
// clicking Allow. Helpers are resolved from compiled-in absolute paths; see
// resolveHelper. That covers osascript and the `defaults` appearance query
// isDarkMode runs before the dialog is drawn.
package prompt

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode"
)

// ErrDenied is returned by Confirm when the user explicitly denies access,
// when no UI is available to ask, or when the prompt tool itself fails.
// All failure modes collapse to ErrDenied so the caller fails closed: a
// secret-gating prompt that can't ask the user must not proceed.
var ErrDenied = errors.New("access denied by user")

// ErrUndisplayable is returned by Confirm when the request contains a rune
// sanitizeDisplay deliberately refuses to render — see altersSurroundingText.
// The dialog is not drawn and the read does not proceed, so it fails closed
// exactly like ErrDenied; it is a separate sentinel only so the failure names
// its own cause. Reporting "access denied by user" for a dialog the user was
// never shown blames them for a refusal they did not make, and sends whoever
// hits it looking for the wrong thing.
var ErrUndisplayable = errors.New("request contains text that cannot be displayed safely")

// Binding pairs an op:// URI with an optional shell variable name.  Name is
// empty in single-URI mode; non-empty when invoked via --env NAME=op://...
type Binding struct {
	Name string // shell variable name; "" when not in --env mode
	URI  string // op:// URI
}

// Request describes a single batch of secrets the user is being asked to
// authorize.  All bindings in one Request are approved or denied together.
type Request struct {
	Bindings []Binding
	// Caller is the short label for the requesting process: the executable
	// name of the nearest ancestor that is not a shell, terminal emulator, or
	// multiplexer — frequently not the immediate parent (see caller.Name).
	Caller string

	// CallerDetail is the fully-rendered detail line shown under the dialog
	// header (e.g. "via claude › python3 script.py" or "to run: python3
	// script.py --team X"). Callers are responsible for wording their own
	// prefix — confirmAndRead uses "via " + caller.Describe(), runSubcommand
	// uses "to run: " + the child argv — since only the caller knows whether
	// it is describing an ancestor or a child about to be spawned.
	// A "via " line is suppressed by callerDetailLine when, after stripping
	// that prefix and any "X › " ancestor prefixes, what remains is equal to
	// Caller: that means the line adds nothing beyond what the dialog
	// header's %q already shows. A "to run: " line is never suppressed — it
	// names the child that will receive the secrets, which is a different
	// process from the header's even when the two render identically.
	CallerDetail string

	// CallerOrigin is an optional fully-rendered line naming where the calling
	// executable lives (e.g. "from /Users/x/.cache/claude"), shown above
	// CallerDetail. Like CallerDetail, the caller words its own prefix.
	//
	// It states the path and stops. opx does not classify a location as
	// expected or suspicious — see caller.Identity for why an allowlist fires
	// on legitimate callers — so this line must not grow judgment text.
	//
	// It exists because Caller is a basename and CallerDetail does not always
	// carry a path: in `opx run` the detail line describes the *child* about to
	// receive the secrets, so without this line the dialog would identify the
	// requesting process only by a name it chose for itself. Empty means the
	// path is unknown — the line is then omitted entirely rather than rendered
	// as a reassuring blank.
	CallerOrigin string

	// CallerThrough is an optional fully-rendered line naming the processes
	// between the named caller and opx that the identity walk passed over (e.g.
	// "through /Users/x/.cache/tools/bash › /bin/zsh"), shown below
	// CallerOrigin. Like the other two, the caller words its own prefix.
	//
	// It is what stops the dialog's subject from being a laundering target.
	// Which process gets named is a heuristic that cannot be made sound — a
	// genuine shell is a legitimate ancestor and a plausible attacker — so the
	// property opx offers instead is that nothing between the subject and opx is
	// silently absorbed. See caller.Identity.Through for what is on it and what
	// is deliberately suppressed.
	//
	// It must be set in **both** input modes. In `opx run` the detail line
	// describes the child about to receive the secrets, so this line and the
	// header are the only account of who asked.
	CallerThrough string
}

// Confirmer presents the user with a confirmation dialog.
type Confirmer interface {
	// Confirm asks the user whether to allow Caller to read every URI in the
	// request.  Returns nil on Allow, ErrDenied on Deny/Cancel/no-UI.
	Confirm(req Request) error
}

// New returns the default Confirmer.  osascript's stderr is forwarded to
// os.Stderr.
func New() Confirmer { return &systemConfirmer{stderr: os.Stderr} }

// NewWithStderr returns a Confirmer that writes osascript's stderr to w.
// Pass io.Discard to silence the dialog backend's diagnostic output.
func NewWithStderr(w io.Writer) Confirmer { return &systemConfirmer{stderr: w} }

type systemConfirmer struct {
	stderr io.Writer
}

// These are the only locations a helper on the dialog path is accepted from.
// Absolute by construction: the point is to never consult PATH.
// defaultsCandidates covers isDarkMode's appearance query — it cannot answer
// the dialog, but it is exec'd before the dialog is drawn, so it is resolved
// the same way the backend itself is.
var (
	osascriptCandidates = []string{"/usr/bin/osascript"}
	defaultsCandidates  = []string{"/usr/bin/defaults"}
)

// resolveHelper returns the first candidate that is a regular, executable file
// which is not group- or world-writable, or "" when none qualifies.
//
// The check rejects a helper another account could have written to directly.
// It does not reach two other ways one could be swapped, and neither is an
// oversight — both are bounded by where the candidates actually live:
//
//   - The containing directory is not examined, so a clean file in a
//     writable directory is accepted even though it could be replaced by
//     unlink-and-recreate. TestResolveHelper_AcceptsCleanFileInWritableDir
//     pins that, so this comment cannot drift back into claiming otherwise.
//   - A helper the invoking user owns and rewrote themselves is
//     indistinguishable from a packaged one.
//
// Every real candidate is under /usr/bin on the SIP-sealed system volume,
// which no account can write to or unlink from — so reaching either gap
// means SIP is already defeated, and the mode check is not what is holding
// the line at that point.
func resolveHelper(candidates []string) string {
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

func (s *systemConfirmer) Confirm(req Request) error {
	return confirmDarwin(req, s.stderr)
}

// message returns the human-readable body shown in the dialog.  For a single
// binding it preserves the original "X wants to read: op://..." phrasing; for
// multiple bindings it lists each URI on its own line separated by blank
// lines, with the bound variable name appended when present.
//
// Invariant: every value interpolated into the returned body has passed
// through sanitizeDisplay. confirmDarwin embeds this string in AppleScript
// source via %q, so an unsanitized control character either breaks the script
// (a raw ESC renders as \x1b, which AppleScript rejects — the dialog never
// appears and opx reports a denial) or survives into the rendered body (\r,
// \n, and \t are real escapes to AppleScript, letting a caller pad or
// line-break the text the user is approving). The URI is still
// attacker-controlled in every input mode — argv, --env, and --env-file
// values: uri.IsOPURI now rejects control characters and invalid UTF-8, but it
// accepts any printable text inside a segment, and it is a separate package
// that this one does not call. Do not read that rule as making this one
// redundant — it is the other half of the same defense, and this half is what
// still holds if a future input path reaches the dialog without passing
// through the validator.
// Anything added to this function must be sanitized too — and the same
// applies outside it: dialogTitle is the other interpolation the user sees,
// and it sanitizes for the same reasons.
func message(req Request) string {
	// The origin and through lines are caller-controlled too — a process chooses
	// where its executable sits, and chooses what it launches opx underneath —
	// so they are sanitized like every other interpolation. Whichever are
	// present are joined into one block, in the order origin, through, detail,
	// so a mode that has several renders them adjacent.
	var lines []string
	for _, line := range []string{
		sanitizeDisplay(req.CallerOrigin),
		sanitizeDisplay(req.CallerThrough),
		callerDetailLine(req),
	} {
		if line != "" {
			lines = append(lines, line)
		}
	}
	detail := strings.Join(lines, "\n")
	caller := sanitizeDisplay(req.Caller)
	if len(req.Bindings) == 1 && req.Bindings[0].Name == "" {
		// %q stays on top of the sanitized name: it escapes the runes
		// sanitizeDisplay deliberately leaves raw (see altersSurroundingText),
		// which is what makes those fail closed. It also re-escapes the
		// backslashes sanitizeDisplay emits, which is cosmetic — a caller name
		// carrying control characters renders as \\x1b rather than \x1b, and is
		// not passed through either way.
		header := fmt.Sprintf("%q wants to read:", caller)
		if detail != "" {
			return fmt.Sprintf("%s\n\n%s\n\n%s", header, detail, sanitizeDisplay(req.Bindings[0].URI))
		}
		return fmt.Sprintf("%s\n\n%s", header, sanitizeDisplay(req.Bindings[0].URI))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%q wants to read %d secret", caller, len(req.Bindings))
	if len(req.Bindings) != 1 {
		b.WriteByte('s')
	}
	b.WriteString(":\n")
	if detail != "" {
		b.WriteString("\n" + detail)
	}
	// Blank line between bullets so long URI lists don't pile up. The leading
	// "\n\n" before each bullet adds one separator line above the bullet; the
	// first bullet's leading blank line also separates the list from the
	// "wants to read N secrets:" header (or the via line, when present).
	for _, bind := range req.Bindings {
		if bind.Name != "" {
			fmt.Fprintf(&b, "\n\n  • %s  →  $%s", sanitizeDisplay(bind.URI), sanitizeDisplay(bind.Name))
		} else {
			fmt.Fprintf(&b, "\n\n  • %s", sanitizeDisplay(bind.URI))
		}
	}
	return b.String()
}

// dialogTitle returns the macOS dialog's title bar text.  Split out of
// confirmDarwin so the sanitizing can be tested without shelling out to
// osascript.
//
// The sanitize is not cosmetic.  confirmDarwin embeds this string in
// AppleScript source via %q, and Go's %q renders a raw ESC as \x1b — which
// AppleScript does not accept as an escape, so the script fails to parse and
// osascript exits non-zero.  confirmDarwin reads that as a denial, meaning a
// caller name carrying an ESC would make opx unusable on macOS while
// reporting "access denied by user".  \r, \n, and \t have the opposite
// problem: AppleScript *does* interpret them, so they would survive %q as
// real control characters and let a caller pad or line-break the title.
// sanitizeDisplay defuses both by turning the byte into visible text before
// %q ever sees it.
func dialogTitle(req Request) string {
	return fmt.Sprintf("opx — %s requesting secret access", sanitizeDisplay(req.Caller))
}

// viaPrefix is the wording prefix confirmAndRead puts on a CallerDetail line
// describing the calling ancestry. Stripped before comparing against Caller
// so the duplicate check below isn't fooled by the wording. Only "via "
// details are ever suppressed — a "to run: " detail names the child about to
// receive the secrets, which is a different process from the header's %q even
// when the two render identically.
const viaPrefix = "via "

// callerDetailLine returns req.CallerDetail with terminal control characters
// rendered visibly, or "" when it is empty or adds nothing beyond the dialog
// header's %q. "Adds nothing" means: it is a "via " line and, after stripping
// that prefix and any "X › " ancestor prefixes, what remains equals Caller,
// case-insensitively.
func callerDetailLine(req Request) string {
	if req.CallerDetail == "" {
		return ""
	}
	stripped, isVia := strings.CutPrefix(req.CallerDetail, viaPrefix)
	if !isVia {
		return sanitizeDisplay(req.CallerDetail)
	}
	if idx := strings.LastIndex(stripped, " › "); idx >= 0 {
		stripped = stripped[idx+len(" › "):]
	}
	if strings.EqualFold(stripped, req.Caller) {
		return ""
	}
	return sanitizeDisplay(req.CallerDetail)
}

// altersSurroundingText reports whether r's effect is on the text around it
// rather than on itself: the Unicode explicit bidirectional formatting
// characters (LRM/RLM/ALM, the embeddings and overrides, the isolates) and the
// line and paragraph separators. These are the runes sanitizeDisplay leaves
// raw so that dialogScript's %q escapes them, AppleScript's parser rejects the
// escape, and the request fails closed — see the layering note on
// sanitizeDisplay.
//
// The set is that principle stated exactly, not a hand-kept list: escaping a
// rune visibly is a sufficient answer whenever the rune's damage is confined
// to itself, and no answer at all when the rune re-orders or breaks the text
// on either side of it — the case where an escaped rendering and a raw one
// disagree about what the *rest* of the URI says. unicode.Bidi_Control, Zl and
// Zp are Unicode's own names for that class, so the boundary moves with the Go
// release rather than with anyone's memory of which code points are involved.
// An enumerated version of this drafted for the same purpose omitted U+061C,
// U+200E and U+200F, which is how quickly that goes wrong.
func altersSurroundingText(r rune) bool {
	return unicode.Is(unicode.Bidi_Control, r) ||
		unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r)
}

// sanitizeDisplay prevents process-controlled text from changing the
// confirmation UI, by rendering as visible escapes the runes that would
// otherwise act on it. Every dialog-body interpolation goes through this —
// caller name, caller detail, origin and through lines, URIs, and bound
// variable names alike. It was introduced for the CallerDetail line alone,
// which left the URI (equally caller-controlled, and the thing the user is
// actually being asked to approve) rendering raw.
//
// Three classes, and the third is the one that is easy to get backwards:
//
//   - C0 and C1 control characters (`\x1b`, `\x0d`, …) — see message and
//     dialogTitle for what each would otherwise break.
//   - Every other rune Go does not consider printable — U+200D, U+00A0,
//     U+E0067, … — *except* the class below. (Named rather than shown: a raw
//     one in this file would be invisible to whoever reads it next.) These
//     have no effect beyond themselves, so showing an escape is a complete
//     answer: the user sees precisely which code point is in the name, and
//     the read proceeds with the real string. This is the class ordinary item names actually contain
//     — U+200D joins an emoji sequence, U+00A0 is what option-space types, the
//     U+E0000 tag characters build flag emoji — and before it was escaped here
//     it was caught downstream by dialogScript's %q, which made any item whose
//     name carries an emoji ZWJ sequence unreadable through opx, and reported
//     that as a denial.
//   - The runes altersSurroundingText names are deliberately **not** escaped:
//     they are left raw for %q to escape and AppleScript to reject. That is a
//     fail-closed, not an omission, and dropping them into the class above
//     would defeat it.
//
// The layering is therefore: this function renders visibly everything whose
// damage is self-contained, and %q fails closed on the rest. The two halves
// have swapped some ground — %q used to hold the whole non-printable range —
// but they still fail differently on purpose, and %q remains the backstop for
// any text that reaches dialogScript without passing through here. See
// AGENTS.md invariant 6.
func sanitizeDisplay(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
			fmt.Fprintf(&b, `\x%02x`, r)
		case altersSurroundingText(r):
			b.WriteRune(r)
		case !strconv.IsPrint(r):
			// Go's own escape spelling, so the dialog shows what a reader would
			// paste into a Go or Python literal to reproduce the rune.
			if r > 0xffff {
				fmt.Fprintf(&b, `\U%08x`, r)
			} else {
				fmt.Fprintf(&b, `\u%04x`, r)
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// undisplayableRune returns the first rune in ss that sanitizeDisplay left raw
// for the AppleScript parser to reject, so confirmDarwin can name the cause
// instead of reporting a denial the user never made.
//
// It is scanned over the *assembled* body and title — what dialogScript is
// about to %q — rather than over Request's fields, so a field added later is
// covered without this having to be revisited.
//
// It is a diagnostic, not a guard. If it ever disagreed with sanitizeDisplay,
// what would still stop the rune reaching the rendered dialog is %q, exactly
// as before; this only decides which error the user is told about.
func undisplayableRune(ss ...string) (rune, bool) {
	for _, s := range ss {
		for _, r := range s {
			if altersSurroundingText(r) {
				return r, true
			}
		}
	}
	return 0, false
}

// dialogScript builds the AppleScript source confirmDarwin runs.  Split out
// of confirmDarwin, like message and dialogTitle, so the assembled source can
// be asserted on without shelling out to osascript.
//
// The leading `beep 3` announces the dialog audibly.  Three is for
// recognizability: it is still the user's own system alert sound, so opx is
// distinguishable from every other alert on the machine by rhythm rather than
// by timbre — which costs nothing, where a distinct sound file would have
// meant giving up the alert-volume behavior described below.  The repeat
// count is fixed; AppleScript does not expose the interval.
//
// `beep` is synchronous, so the count is paid before the dialog renders:
// measured 0.86s for `beep 3` against 0.35s for a single beep, i.e. ~0.5s of
// added delay per read (including in run mode, ahead of the child spawn).
// That is deliberate, not an oversight — the rhythm is the recognizability,
// and half a second in front of a dialog the user must read and click is not
// a cost worth trading it for.  Don't quietly drop it back to `beep`.
//
// The beep is deliberately unconditional and deliberately not configurable.
// The dialog already fails closed when nobody is watching (`giving up after
// 60` below), so the sound adds no leak prevention; what it adds is tamper
// evidence — without it, a process can probe for a secret while the user is
// away, absorb a silent 60-second timeout, and leave no trace the attempt
// happened.  A detection signal is only worth what it costs to suppress, and
// any opx-level switch — env var or config file — would be readable and
// writable by the very process opx is gating, i.e. an alarm with the off
// switch on the outside.  The user-facing configuration is macOS's own:
// `beep` plays the system alert sound at the system alert volume, which
// System Settings › Sound owns.
//
// A caller can reach that too — `set volume alert volume 0` is unprivileged
// — so this is not an unsuppressible signal, and nothing here should be read
// as claiming otherwise.  What it costs to suppress is the point: silencing
// the machine's alert volume is global, persistent, and user-visible, and it
// outlives the read that motivated it.  Flipping an OPX_SOUND=0 would be
// targeted, invisible, and gone by the next invocation.  Tamper evidence in
// the sense a broken seal is: not proof against tampering, but not free to
// defeat quietly either.
//
// It is also a constant token, which is the other reason to prefer it over
// afplay: it introduces no new user-influenced interpolation into this
// AppleScript source, so the sanitizeDisplay invariant documented on message
// is untouched.  A user-chosen sound name or path would land squarely in that
// hazard.
func dialogScript(req Request, iconClause string) string {
	return fmt.Sprintf(
		`beep 3`+"\n"+
			`display dialog %q with title %q `+
			`buttons {"Deny", "Allow"} default button "Allow" cancel button "Deny" `+
			`%s giving up after 60`,
		message(req), dialogTitle(req), iconClause,
	)
}

// confirmDarwin shows a native macOS dialog via osascript.
//
// `cancel button "Deny"` is load-bearing: without it, AppleScript exits 0
// for *every* button click and only records which one was pressed in stdout.
// Marking Deny as the cancel button makes osascript exit non-zero when the
// user clicks Deny (or presses Escape), which is what we check below.
//
// `giving up after 60` auto-dismisses (treated as denial) if the user walks
// away — fail-closed safety net for unattended terminals.
//
// An unresolvable osascript is a denial. ErrDenied already covers "no UI
// available to ask", and there is no other backend to fall back to.
//
// A request carrying a rune sanitizeDisplay refuses to render is the one
// failure that is not a denial: nothing is shown, so there is no user to
// attribute the refusal to. It fails closed all the same — see
// ErrUndisplayable.
func confirmDarwin(req Request, stderr io.Writer) error {
	// Before osascript, not instead of it: %q would reject these too, but
	// as an unattributable non-zero exit. Checking here is what lets the
	// error name the code point instead.
	if r, bad := undisplayableRune(message(req), dialogTitle(req)); bad {
		return fmt.Errorf("%w: U+%04X", ErrUndisplayable, r)
	}
	osascript := resolveHelper(osascriptCandidates)
	if osascript == "" {
		return ErrDenied
	}
	iconClause := "with icon caution"
	if path := writeIconFile(); path != "" {
		// AppleScript string-escape the path: backslash + quote.
		esc := strings.ReplaceAll(path, `\`, `\\`)
		esc = strings.ReplaceAll(esc, `"`, `\"`)
		iconClause = fmt.Sprintf(`with icon file (POSIX file "%s")`, esc)
	}
	cmd := exec.Command(osascript, "-e", dialogScript(req, iconClause))
	cmd.Stderr = stderr
	out, err := cmd.Output()
	if err != nil {
		// Non-zero exit means the user clicked Deny, pressed Escape, or
		// osascript itself failed (missing binary, etc.). All treated as denial.
		return ErrDenied
	}
	// `giving up after` makes osascript exit 0 even on timeout; the result
	// record contains `gave up:true`. Detect that and fail closed.
	if strings.Contains(string(out), "gave up:true") {
		return ErrDenied
	}
	return nil
}
