package prompt_test

import (
	"errors"
	"testing"

	"github.com/bestdan/opx/internal/prompt"
)

// fakeConfirmer is a test double for Confirmer.
type fakeConfirmer struct {
	err error
}

func (f *fakeConfirmer) Confirm(req prompt.Request) error {
	return f.err
}

// compile-time check.
var _ prompt.Confirmer = (*fakeConfirmer)(nil)

func req(uri string) prompt.Request {
	return prompt.Request{
		Bindings: []prompt.Binding{{URI: uri}},
		Caller:   "bash",
	}
}

func TestFakeConfirmer_Allow(t *testing.T) {
	fc := &fakeConfirmer{}
	if err := fc.Confirm(req("op://V/I/f")); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestFakeConfirmer_Deny(t *testing.T) {
	fc := &fakeConfirmer{err: prompt.ErrDenied}
	err := fc.Confirm(req("op://V/I/f"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, prompt.ErrDenied) {
		t.Errorf("expected errors.Is(err, ErrDenied) to be true, got %v", err)
	}
}

func TestNew_ReturnsConfirmer(t *testing.T) {
	c := prompt.New()
	if c == nil {
		t.Error("prompt.New() returned nil")
	}
}
