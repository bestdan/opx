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

// New returns the default Confirmer for the current platform.
func New() Confirmer { return &systemConfirmer{} }

type systemConfirmer struct{}

func (s *systemConfirmer) Confirm(req Request) error {
	switch runtime.GOOS {
	case "darwin":
		return confirmDarwin(req)
	default:
		return confirmLinux(req)
	}
}

// message returns the human-readable body shown in the dialog.  For a single
// binding it preserves the original "X wants to read: op://..." phrasing; for
// multiple bindings it lists each URI on its own line, with the bound variable
// name appended when present.
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
	for _, bind := range req.Bindings {
		if bind.Name != "" {
			fmt.Fprintf(&b, "\n  • %s  →  $%s", bind.URI, bind.Name)
		} else {
			fmt.Fprintf(&b, "\n  • %s", bind.URI)
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
func confirmDarwin(req Request) error {
	script := fmt.Sprintf(
		`display dialog %q with title "opx - Secret Access Request" `+
			`buttons {"Deny", "Allow"} default button "Allow" cancel button "Deny" `+
			`with icon caution`,
		message(req),
	)
	cmd := exec.Command("osascript", "-e", script)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Non-zero exit means the user clicked Deny, pressed Escape, or
		// osascript itself failed (missing binary, etc.). All treated as denial.
		return ErrDenied
	}
	return nil
}

// confirmLinux tries zenity first, then falls back to a /dev/tty prompt.
func confirmLinux(req Request) error {
	if _, err := exec.LookPath("zenity"); err == nil {
		return confirmZenity(req)
	}
	return confirmTTY(req)
}

// confirmZenity shows a GTK dialog using the zenity helper.
func confirmZenity(req Request) error {
	cmd := exec.Command(
		"zenity",
		"--question",
		"--title=opx - Secret Access Request",
		"--text="+message(req),
		"--ok-label=Allow",
		"--cancel-label=Deny",
		"--width=500",
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return ErrDenied
	}
	return nil
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
