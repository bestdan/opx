// Package prompt shows a platform-native confirmation dialog before opx reads
// a secret.  The dialog mimics the biometric unlock UI by displaying:
//   - which op:// URI(s) are being requested
//   - which environment variable each URI will be bound to (when applicable)
//   - which process is requesting it
//
// On macOS it uses osascript (AppleScript); on Linux it tries zenity and falls
// back to a /dev/tty prompt.
package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ErrDenied is returned by Confirm when the user explicitly denies access,
// when no UI/TTY is available to ask, or when the prompt tool itself fails.
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
	Caller   string // executable name of the parent process
}

// Confirmer presents the user with a confirmation dialog.
type Confirmer interface {
	// Confirm asks the user whether to allow Caller to read every URI in the
	// request.  Returns nil on Allow, ErrDenied on Deny/Cancel/no-UI.
	Confirm(req Request) error
}

// New returns the default Confirmer for the current platform.  Subprocess
// stderr (osascript / zenity) is forwarded to os.Stderr.
func New() Confirmer { return &systemConfirmer{stderr: os.Stderr} }

// NewWithStderr returns a Confirmer that writes osascript/zenity stderr to w.
// Pass io.Discard to silence the dialog backend's diagnostic output.
func NewWithStderr(w io.Writer) Confirmer { return &systemConfirmer{stderr: w} }

type systemConfirmer struct {
	stderr io.Writer
}

func (s *systemConfirmer) Confirm(req Request) error {
	switch runtime.GOOS {
	case "darwin":
		return confirmDarwin(req, s.stderr)
	default:
		return confirmLinux(req, s.stderr)
	}
}

// message returns the human-readable body shown in the dialog.  For a single
// binding it preserves the original "X wants to read: op://..." phrasing; for
// multiple bindings it lists each URI on its own line separated by blank
// lines, with the bound variable name appended when present.
func message(req Request) string {
	if len(req.Bindings) == 1 && req.Bindings[0].Name == "" {
		return fmt.Sprintf("%q wants to read:\n\n%s", req.Caller, req.Bindings[0].URI)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%q wants to read %d secret", req.Caller, len(req.Bindings))
	if len(req.Bindings) != 1 {
		b.WriteByte('s')
	}
	b.WriteString(":\n")
	// Blank line between bullets so long URI lists don't pile up. The leading
	// "\n\n" before each bullet adds one separator line above the bullet; the
	// first bullet's leading blank line also separates the list from the
	// "wants to read N secrets:" header.
	for _, bind := range req.Bindings {
		if bind.Name != "" {
			fmt.Fprintf(&b, "\n\n  • %s  →  $%s", bind.URI, bind.Name)
		} else {
			fmt.Fprintf(&b, "\n\n  • %s", bind.URI)
		}
	}
	return b.String()
}

// confirmDarwin shows a native macOS dialog via osascript.
//
// `cancel button "Deny"` is load-bearing: without it, AppleScript exits 0
// for *every* button click and only records which one was pressed in stdout.
// Marking Deny as the cancel button makes osascript exit non-zero when the
// user clicks Deny (or presses Escape), which is what we check below.
//
// `giving up after 60` auto-dismisses (treated as denial) if the user walks
// away — fail-closed safety net for unattended terminals. macOS-only: the
// zenity and /dev/tty paths block until the user responds.
func confirmDarwin(req Request, stderr io.Writer) error {
	iconClause := "with icon caution"
	if path := writeIconFile(); path != "" {
		// AppleScript string-escape the path: backslash + quote.
		esc := strings.ReplaceAll(path, `\`, `\\`)
		esc = strings.ReplaceAll(esc, `"`, `\"`)
		iconClause = fmt.Sprintf(`with icon file (POSIX file "%s")`, esc)
	}
	title := fmt.Sprintf("opx — %s requesting secret access", req.Caller)
	script := fmt.Sprintf(
		`display dialog %q with title %q `+
			`buttons {"Deny", "Allow"} default button "Allow" cancel button "Deny" `+
			`%s giving up after 60`,
		message(req), title, iconClause,
	)
	cmd := exec.Command("osascript", "-e", script)
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

// confirmLinux tries zenity first, then falls back to a /dev/tty prompt.
func confirmLinux(req Request, stderr io.Writer) error {
	if _, err := exec.LookPath("zenity"); err == nil {
		return confirmZenity(req, stderr)
	}
	return confirmTTY(req)
}

// confirmZenity shows a GTK dialog using the zenity helper.
//
// zenity interprets Pango markup in --text, so &, <, and > in the URI or
// caller name would either render wrong or cause zenity to refuse the
// dialog. Escape them before passing the message in.
func confirmZenity(req Request, stderr io.Writer) error {
	cmd := exec.Command(
		"zenity",
		"--question",
		"--title=opx - Secret Access Request",
		"--text="+pangoEscape(message(req)),
		"--ok-label=Allow",
		"--cancel-label=Deny",
		"--width=500",
	)
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return ErrDenied
	}
	return nil
}

// pangoEscape replaces the three characters Pango treats as markup with
// their entity equivalents. Order matters: & must be replaced first so the
// entities introduced for < and > aren't double-escaped.
func pangoEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return r.Replace(s)
}

// confirmTTY prompts directly on the controlling terminal, bypassing any
// stdin/stdout redirection.  This handles headless or SSH sessions where no
// GUI is available.
func confirmTTY(req Request) error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return ErrDenied
	}
	defer tty.Close()

	fmt.Fprintf(tty, "\n+-----------------------------------------+\n")
	fmt.Fprintf(tty, "|       opx - Secret Access Request       |\n")
	fmt.Fprintf(tty, "+-----------------------------------------+\n\n")
	fmt.Fprintln(tty, message(req))
	fmt.Fprintf(tty, "\nAllow? [y/N]: ")

	scanner := bufio.NewScanner(tty)
	if !scanner.Scan() {
		return ErrDenied
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if answer == "y" || answer == "yes" {
		return nil
	}
	return ErrDenied
}
