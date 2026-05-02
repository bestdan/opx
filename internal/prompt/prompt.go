// Package prompt shows a platform-native confirmation dialog before opx reads
// a secret.  The dialog mimics the biometric unlock UI by displaying:
//   - which op:// URI is being requested
//   - which process is requesting it
//
// On macOS it uses osascript (AppleScript); on Linux it tries zenity and falls
// back to a /dev/tty prompt.
package prompt

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Confirmer presents the user with a confirmation dialog.
type Confirmer interface {
	// Confirm asks the user whether to allow callerName to read the secret at
	// uri.  Returns nil on Allow, a non-nil error on Deny/Cancel.
	Confirm(uri, callerName string) error
}

// New returns the default Confirmer for the current platform.
func New() Confirmer { return &systemConfirmer{} }

type systemConfirmer struct{}

func (s *systemConfirmer) Confirm(uri, callerName string) error {
	switch runtime.GOOS {
	case "darwin":
		return confirmDarwin(uri, callerName)
	default:
		return confirmLinux(uri, callerName)
	}
}

// message returns the human-readable body shown in the dialog.
func message(uri, callerName string) string {
	return fmt.Sprintf("%q wants to read:\n\n%s", callerName, uri)
}

// confirmDarwin shows a native macOS dialog via osascript.
func confirmDarwin(uri, callerName string) error {
	script := fmt.Sprintf(
		`display dialog %q with title "opx - Secret Access Request" `+
			`buttons {"Deny", "Allow"} default button "Allow" with icon caution`,
		message(uri, callerName),
	)
	cmd := exec.Command("osascript", "-e", script)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// exit status 1 means the user clicked Deny or pressed Escape.
		return fmt.Errorf("access denied by user")
	}
	return nil
}

// confirmLinux tries zenity first, then falls back to a /dev/tty prompt.
func confirmLinux(uri, callerName string) error {
	if _, err := exec.LookPath("zenity"); err == nil {
		return confirmZenity(uri, callerName)
	}
	return confirmTTY(uri, callerName)
}

// confirmZenity shows a GTK dialog using the zenity helper.
func confirmZenity(uri, callerName string) error {
	msg := message(uri, callerName)
	cmd := exec.Command(
		"zenity",
		"--question",
		"--title=opx - Secret Access Request",
		"--text="+msg,
		"--ok-label=Allow",
		"--cancel-label=Deny",
		"--width=400",
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("access denied by user")
	}
	return nil
}

// confirmTTY prompts directly on the controlling terminal, bypassing any
// stdin/stdout redirection.  This handles headless or SSH sessions where no
// GUI is available.
func confirmTTY(uri, callerName string) error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		// No controlling terminal — deny by default.
		return fmt.Errorf("no display and no controlling terminal; access denied")
	}
	defer tty.Close()

	fmt.Fprintf(tty, "\n┌─────────────────────────────────────────┐\n")
	fmt.Fprintf(tty, "│       opx - Secret Access Request       │\n")
	fmt.Fprintf(tty, "└─────────────────────────────────────────┘\n")
	fmt.Fprintf(tty, "\n%q wants to read:\n  %s\n\n", callerName, uri)
	fmt.Fprintf(tty, "Allow? [y/N]: ")

	scanner := bufio.NewScanner(tty)
	if !scanner.Scan() {
		return fmt.Errorf("access denied (no input)")
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if answer == "y" || answer == "yes" {
		return nil
	}
	return fmt.Errorf("access denied by user")
}
