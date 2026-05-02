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

func (f *fakeConfirmer) Confirm(uri, callerName string) error {
	return f.err
}

// compile-time check.
var _ prompt.Confirmer = (*fakeConfirmer)(nil)

func TestFakeConfirmer_Allow(t *testing.T) {
	fc := &fakeConfirmer{}
	if err := fc.Confirm("op://V/I/f", "bash"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestFakeConfirmer_Deny(t *testing.T) {
	fc := &fakeConfirmer{err: errors.New("access denied by user")}
	if err := fc.Confirm("op://V/I/f", "bash"); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestNew_ReturnsConfirmer(t *testing.T) {
	c := prompt.New()
	if c == nil {
		t.Error("prompt.New() returned nil")
	}
}
