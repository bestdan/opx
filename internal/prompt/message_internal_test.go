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
