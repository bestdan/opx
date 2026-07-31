package prompt

import (
	"strings"
	"testing"
)

func TestMessage_SingleLegacy(t *testing.T) {
	got := message(Request{
		Bindings: []Binding{{URI: "op://V/I/f"}},
		Caller:   "bash",
	})
	want := "\"bash\" wants to read:\n\nop://V/I/f"
	if got != want {
		t.Errorf("message =\n%q\nwant\n%q", got, want)
	}
}

func TestMessage_SingleEnvBindingShowsName(t *testing.T) {
	got := message(Request{
		Bindings: []Binding{{Name: "TOKEN", URI: "op://V/I/f"}},
		Caller:   "bash",
	})
	if !strings.Contains(got, "op://V/I/f") {
		t.Errorf("message must disclose URI; got %q", got)
	}
	if !strings.Contains(got, "$TOKEN") {
		t.Errorf("message must disclose bound variable name; got %q", got)
	}
	if !strings.Contains(got, "1 secret") || strings.Contains(got, "1 secrets") {
		t.Errorf("singular phrasing wrong; got %q", got)
	}
}

func TestMessage_BatchListsEveryURIAndName(t *testing.T) {
	bindings := []Binding{
		{Name: "A", URI: "op://V/A/f"},
		{Name: "B", URI: "op://V/B/f"},
		{Name: "C", URI: "op://V/C/f"},
	}
	got := message(Request{Bindings: bindings, Caller: "deploy.sh"})

	if !strings.Contains(got, "\"deploy.sh\"") {
		t.Errorf("caller missing from message: %q", got)
	}
	if !strings.Contains(got, "3 secrets") {
		t.Errorf("plural count missing: %q", got)
	}
	for _, b := range bindings {
		if !strings.Contains(got, b.URI) {
			t.Errorf("URI %q missing from dialog text — security invariant: user must see every URI before approving.\nfull message:\n%s", b.URI, got)
		}
		if !strings.Contains(got, "$"+b.Name) {
			t.Errorf("var name %q missing from dialog text — user must see what each URI binds to.\nfull message:\n%s", b.Name, got)
		}
	}
}

func TestMessage_CallerDetailSetSingle(t *testing.T) {
	got := message(Request{
		Bindings:     []Binding{{URI: "op://V/I/f"}},
		Caller:       "python3",
		CallerDetail: "via claude › python3 linear-archive.py --team PreThink",
	})
	want := "\"python3\" wants to read:\n\nvia claude › python3 linear-archive.py --team PreThink\n\nop://V/I/f"
	if got != want {
		t.Errorf("message =\n%q\nwant\n%q", got, want)
	}
}

func TestMessage_CallerDetailRunModeSingle(t *testing.T) {
	got := message(Request{
		Bindings:     []Binding{{URI: "op://V/I/f"}},
		Caller:       "claude",
		CallerDetail: "to run: python3 linear-archive.py --team PreThink --older-than 1",
	})
	want := "\"claude\" wants to read:\n\nto run: python3 linear-archive.py --team PreThink --older-than 1\n\nop://V/I/f"
	if got != want {
		t.Errorf("message =\n%q\nwant\n%q", got, want)
	}
}

func TestMessage_CallerDetailEscapesTerminalControlCharacters(t *testing.T) {
	got := message(Request{
		Bindings:     []Binding{{URI: "op://V/I/f"}},
		Caller:       "python3",
		CallerDetail: "via python3 report.go\x1b[2J\rforged",
	})
	if strings.ContainsAny(got, "\x1b\r") {
		t.Errorf("message must not contain terminal control characters from CallerDetail: %q", got)
	}
	if !strings.Contains(got, `\x1b[2J\x0dforged`) {
		t.Errorf("message must render removed control characters visibly: %q", got)
	}
}

// controlChars are the bytes that must never reach /dev/tty from a
// caller-controlled field: ESC begins an ANSI sequence, CR rewrites the
// current line, and DEL (0x7f) sits just below the C1 range (0x80-0x9f)
// that sanitizeDisplay escapes alongside it.
const controlChars = "\x1b\r\x7f"

// TestMessage_SingleURIEscapesTerminalControlCharacters covers the
// single-binding path. The URI is caller-controlled in every input mode —
// uri.IsOPURI accepts any bytes inside its three segments — so an ESC here
// clears the screen of the one dialog that authorizes the read.
func TestMessage_SingleURIEscapesTerminalControlCharacters(t *testing.T) {
	got := message(Request{
		Bindings: []Binding{{URI: "op://V/I/f\x1b[2J\x1b[Hopx — routine sync\rAllow? [y/N]: "}},
		Caller:   "python3",
	})
	if strings.ContainsAny(got, controlChars) {
		t.Errorf("message must not pass control characters through from a URI: %q", got)
	}
	if !strings.Contains(got, `\x1b[2J`) {
		t.Errorf("message must render the escape visibly: %q", got)
	}
}

// TestMessage_BatchURIAndNameEscaped covers the --env / --env-file bullet
// list, the sink an attacker reaches by controlling a committed .env.
func TestMessage_BatchURIAndNameEscaped(t *testing.T) {
	got := message(Request{
		Bindings: []Binding{
			{Name: "A", URI: "op://V/A/f"},
			{Name: "B\x1bfake", URI: "op://Private/prod/root\x1b[2K\r  • op://Demo/test/token"},
		},
		Caller: "deploy.sh",
	})
	if strings.ContainsAny(got, controlChars) {
		t.Errorf("message must not pass control characters through in batch mode: %q", got)
	}
	if !strings.Contains(got, "op://Private/prod/root") {
		t.Errorf("the real URI must still be disclosed: %q", got)
	}
}

// TestMessage_CallerEscaped closes the last interpolation site. The caller
// name comes from a self-asserted process name, so it is caller-controlled
// too.
func TestMessage_CallerEscaped(t *testing.T) {
	got := message(Request{
		Bindings: []Binding{{URI: "op://V/I/f"}},
		Caller:   "python3\x1b[2Jclaude",
	})
	if strings.ContainsAny(got, controlChars) {
		t.Errorf("message must not pass control characters through from Caller: %q", got)
	}
	// Doubled backslash: sanitizeDisplay emits \x1b, then the header's %q
	// escapes that backslash again. Asserting the escape is *present* — not
	// just that the raw byte is gone — is what distinguishes rendering the
	// escape visibly from silently dropping it.
	if !strings.Contains(got, `\\x1b[2J`) {
		t.Errorf("message must render the escape visibly: %q", got)
	}
}

// TestDialogTitle_Escaped covers the one dialog interpolation that is not in
// message()'s body. An unsanitized ESC here does not repaint a terminal — the
// title only reaches the macOS GUI dialog — but %q renders it as \x1b, which
// AppleScript rejects, so the script fails to parse and opx reports a denial
// for a request the user never saw.
func TestDialogTitle_Escaped(t *testing.T) {
	got := dialogTitle(Request{Caller: "python3\x1b[2J\rclaude"})
	if strings.ContainsAny(got, controlChars) {
		t.Errorf("dialogTitle must not pass control characters through: %q", got)
	}
	if !strings.Contains(got, `\x1b[2J`) {
		t.Errorf("dialogTitle must render the escape visibly: %q", got)
	}
}

// TestDialogTitle_BenignUnchanged is the counterpart regression guard: an
// ordinary caller name must render byte-for-byte as before.
func TestDialogTitle_BenignUnchanged(t *testing.T) {
	got := dialogTitle(Request{Caller: "deploy.sh"})
	want := "opx — deploy.sh requesting secret access"
	if got != want {
		t.Errorf("dialogTitle = %q, want %q", got, want)
	}
}

// TestDialogScript_NonPrintableNeverReachesAppleScriptSource pins the second
// half of the escaping guard, which sanitizeDisplay does not provide.
//
// sanitizeDisplay covers C0/C1 only. A Unicode bidi override (U+202E) is
// neither, so it reaches dialogScript intact — and a raw U+202E inside the
// AppleScript string literal would render, reordering the displayed path so a
// URI can lie about which secret is being requested. What stops it is the %q
// dialogScript wraps the body and title in: Go escapes any non-printable rune
// to a literal \uXXXX, AppleScript's parser rejects that token, osascript
// exits non-zero, and confirmDarwin maps that to ErrDenied. Fails closed.
//
// The assertion is on the generated source, not on osascript's behaviour, so
// this stays a hermetic unit test (AGENTS.md forbids shelling out to a real
// osascript). Replace either %q with plain interpolation and this fails.
func TestDialogScript_NonPrintableNeverReachesAppleScriptSource(t *testing.T) {
	// Written as a Go escape, not the literal character: a raw U+202E in this
	// file would reorder the source for whoever reads it next.
	const rlo = "\u202e"

	got := dialogScript(Request{
		Bindings: []Binding{{URI: "op://Private/prod/root" + rlo + "gnip"}},
		Caller:   "claude" + rlo + "hs.yolped",
	}, "with icon caution")

	if strings.Contains(got, rlo) {
		t.Errorf("raw U+202E reached the AppleScript source — a bidi override in the "+
			"dialog can reorder the displayed URI: %q", got)
	}
	if !strings.Contains(got, "\\u202e") {
		t.Errorf("expected the override to survive as an escaped \\u202e token "+
			"(which AppleScript rejects, so the request fails closed): %q", got)
	}
}

// TestMessage_BenignInputUnchanged is the regression guard on the sanitizer:
// normal URIs, unicode item names, and shell variable names must render
// byte-for-byte as before. A sanitizer that mangles ordinary output trains
// users to ignore the dialog.
func TestMessage_BenignInputUnchanged(t *testing.T) {
	got := message(Request{
		Bindings: []Binding{{Name: "TOKEN", URI: "op://Personal/Café/pässwörd"}},
		Caller:   "deploy.sh",
	})
	want := "\"deploy.sh\" wants to read 1 secret:\n\n\n  • op://Personal/Café/pässwörd  →  $TOKEN"
	if got != want {
		t.Errorf("message =\n%q\nwant\n%q", got, want)
	}
}

func TestMessage_CallerDetailEmpty(t *testing.T) {
	got := message(Request{
		Bindings:     []Binding{{URI: "op://V/I/f"}},
		Caller:       "bash",
		CallerDetail: "",
	})
	if strings.Contains(got, "via ") {
		t.Errorf("message must not contain a via line when CallerDetail is empty: %q", got)
	}
}

func TestMessage_CallerDetailEqualToCaller_NoDuplicateLine(t *testing.T) {
	got := message(Request{
		Bindings:     []Binding{{URI: "op://V/I/f"}},
		Caller:       "bash",
		CallerDetail: "bash",
	})
	if strings.Contains(got, "via ") {
		t.Errorf("message must not add a via line duplicating Caller: %q", got)
	}
}

func TestMessage_CallerDetailViaCallerOnly_NoDuplicateLine(t *testing.T) {
	// The realistic shape a caller produces when caller.Describe() falls back
	// to the bare Caller name (e.g. single-token argv suppressed any
	// ancestor prefix): "via <Caller>" adds nothing beyond the header.
	got := message(Request{
		Bindings:     []Binding{{URI: "op://V/I/f"}},
		Caller:       "claude",
		CallerDetail: "via claude",
	})
	if strings.Contains(got, "via ") {
		t.Errorf("message must not add a via line duplicating Caller: %q", got)
	}
}

func TestMessage_CallerDetailViaAncestorPrefixOfCaller_NoDuplicateLine(t *testing.T) {
	// "via ghostty › claude" strips to "claude", which equals Caller — no
	// information beyond the header.
	got := message(Request{
		Bindings:     []Binding{{URI: "op://V/I/f"}},
		Caller:       "claude",
		CallerDetail: "via ghostty › claude",
	})
	if strings.Contains(got, "via ") {
		t.Errorf("message must not add a via line duplicating Caller: %q", got)
	}
}

func TestMessage_CallerDetailCaseInsensitiveDuplicate(t *testing.T) {
	got := message(Request{
		Bindings:     []Binding{{URI: "op://V/I/f"}},
		Caller:       "Terminal",
		CallerDetail: "via terminal",
	})
	if strings.Contains(got, "via ") {
		t.Errorf("message must not add a via line duplicating Caller case-insensitively: %q", got)
	}
}

func TestMessage_CallerDetailRunModeMatchingCaller_NotSuppressed(t *testing.T) {
	// `opx run ... -- claude` invoked from claude renders a child argv equal
	// to Caller, but the two are different processes: the header names who
	// asked, the detail names who receives the secrets. Suppressing this line
	// would make the dialog indistinguishable from a plain read.
	got := message(Request{
		Bindings:     []Binding{{URI: "op://V/I/f"}},
		Caller:       "claude",
		CallerDetail: "to run: claude",
	})
	if !strings.Contains(got, "to run: claude") {
		t.Errorf("message must keep a to-run line even when it matches Caller: %q", got)
	}
}

func TestMessage_CallerDetailSetBatch(t *testing.T) {
	bindings := []Binding{
		{Name: "A", URI: "op://V/A/f"},
		{Name: "B", URI: "op://V/B/f"},
	}
	got := message(Request{
		Bindings:     bindings,
		Caller:       "deploy.sh",
		CallerDetail: "via claude › deploy.sh --prod",
	})
	if !strings.Contains(got, "via claude › deploy.sh --prod") {
		t.Errorf("batch message missing via line: %q", got)
	}
	for _, b := range bindings {
		if !strings.Contains(got, b.URI) {
			t.Errorf("URI %q missing from batch message with CallerDetail: %q", b.URI, got)
		}
	}
}

func TestMessage_CallerDetailRunModeBatch(t *testing.T) {
	bindings := []Binding{
		{Name: "A", URI: "op://V/A/f"},
		{Name: "B", URI: "op://V/B/f"},
	}
	got := message(Request{
		Bindings:     bindings,
		Caller:       "claude",
		CallerDetail: "to run: python3 linear-archive.py --team PreThink --older-than 1",
	})
	if !strings.Contains(got, "to run: python3 linear-archive.py --team PreThink --older-than 1") {
		t.Errorf("batch message missing to-run detail line: %q", got)
	}
	for _, b := range bindings {
		if !strings.Contains(got, b.URI) {
			t.Errorf("URI %q missing from batch message with CallerDetail: %q", b.URI, got)
		}
	}
}

func TestMessage_BatchWithoutNamesStillListsURIs(t *testing.T) {
	// Defensive: even if Name is empty in batch mode the URI must still appear.
	bindings := []Binding{
		{URI: "op://V/A/f"},
		{URI: "op://V/B/f"},
	}
	got := message(Request{Bindings: bindings, Caller: "x"})
	for _, b := range bindings {
		if !strings.Contains(got, b.URI) {
			t.Errorf("URI %q missing: %q", b.URI, got)
		}
	}
	if strings.Contains(got, "$") {
		t.Errorf("no Name set, but message contained $: %q", got)
	}
}

func TestDialogScript_BeepsBeforeDisplayingDialog(t *testing.T) {
	// The beep is the tamper-evidence signal: it must fire as its own
	// statement ahead of the dialog, not somewhere inside the dialog's
	// arguments where AppleScript would treat it as text.
	got := dialogScript(Request{
		Bindings: []Binding{{URI: "op://V/I/f"}},
		Caller:   "bash",
	}, "with icon caution")

	if !strings.HasPrefix(got, "beep 3\n") {
		t.Fatalf("script does not open with a beep statement: %q", got)
	}
	beep := strings.Index(got, "beep")
	dialog := strings.Index(got, "display dialog")
	if dialog < 0 {
		t.Fatalf("script has no display dialog: %q", got)
	}
	if beep > dialog {
		t.Errorf("beep at %d comes after display dialog at %d: %q", beep, dialog, got)
	}
}

func TestDialogScript_BeepIsNotConfigurable(t *testing.T) {
	// Pins the narrow thing it can actually pin: dialogScript emits the beep
	// as a pure function of its arguments, consulting nothing else. It does
	// not — and cannot — prove the beep is unsuppressible overall; a switch
	// added in confirmDarwin, which is where one would naturally go, would
	// leave this green. The guard against that is the AGENTS.md entry and
	// review, not this test.
	got := dialogScript(Request{
		Bindings: []Binding{{URI: "op://V/I/f"}},
		Caller:   "bash",
	}, "with icon caution")

	if !strings.Contains(got, "beep") {
		t.Errorf("environment suppressed the beep: %q", got)
	}
}
