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
