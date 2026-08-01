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
	"strings"
)

// ErrDenied is returned by Confirm when the user explicitly denies access,
// when no UI is available to ask, or when the prompt tool itself fails.
// All failure modes collapse to ErrDenied so the caller fails closed: a
// secret-gating prompt that can't ask the user must not proceed.
var ErrDenied = errors.New("access denied by user")

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
	// executable lives (e.g. "from /Users/x/.cache/claude (unverified
	// location)"), shown above CallerDetail. Like CallerDetail, the caller
	// words its own prefix.
	//
	// It exists because Caller is a basename and CallerDetail does not always
	// carry a path: in `opx run` the detail line describes the *child* about to
	// receive the secrets, so without this line the dialog would identify the
	// requesting process only by a name it chose for itself. Empty means the
	// path is unknown — the line is then omitted entirely rather than rendered
	// as a reassuring blank.
	CallerOrigin string
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
// line-break the text the user is approving). The URI is attacker-controlled
// in every input mode — argv, --env, and --env-file values — since
// uri.IsOPURI checks only the op:// prefix and three non-empty segments.
// Anything added to this function must be sanitized too — and the same
// applies outside it: dialogTitle is the other interpolation the user sees,
// and it sanitizes for the same reasons.
func message(req Request) string {
	detail := callerDetailLine(req)
	// The origin line is caller-controlled too — a process chooses where its
	// executable sits — so it is sanitized like every other interpolation.
	// When present it precedes the detail line, and the two are joined into
	// one block so a mode that has both renders them adjacent.
	if origin := sanitizeDisplay(req.CallerOrigin); origin != "" {
		if detail != "" {
			detail = origin + "\n" + detail
		} else {
			detail = origin
		}
	}
	caller := sanitizeDisplay(req.Caller)
	if len(req.Bindings) == 1 && req.Bindings[0].Name == "" {
		// %q stays on top of the sanitized name: it escapes the non-printable
		// runes sanitizeDisplay does not cover (bidi overrides and other format
		// characters). It also re-escapes the backslashes sanitizeDisplay
		// emits, which is cosmetic — a caller name carrying control characters
		// renders as \\x1b rather than \x1b, and is not passed through either
		// way.
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

// sanitizeDisplay prevents process-controlled text from changing the
// confirmation UI. C0 and C1 control characters are rendered as visible
// escapes instead of being passed through to the AppleScript source that
// confirmDarwin builds — see message and dialogTitle for what each of them
// would otherwise break.
//
// Every dialog-body interpolation goes through this — caller name, caller
// detail, URIs, and bound variable names alike. It was introduced for the
// CallerDetail line alone, which left the URI (equally caller-controlled, and
// the thing the user is actually being asked to approve) rendering raw.
func sanitizeDisplay(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			fmt.Fprintf(&b, `\x%02x`, r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
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
func confirmDarwin(req Request, stderr io.Writer) error {
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
