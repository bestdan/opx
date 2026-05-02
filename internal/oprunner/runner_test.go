package oprunner_test

import (
	"context"
	"testing"

	"github.com/bestdan/opx/internal/oprunner"
)

func TestIsOPURI(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"op://Vault/Item/field", true},
		{"op://My Vault/My Item/password", true},
		{"op://v/i/f/extra", true},        // extra segment is fine
		{"op://vault/item/", false},        // empty field
		{"op://vault//field", false},       // empty item
		{"op:///item/field", false},        // empty vault
		{"op://vault/item", false},         // only two segments
		{"op://", false},                   // empty
		{"http://vault/item/field", false}, // wrong scheme
		{"", false},
		{"op:/vault/item/field", false}, // single slash
	}
	for _, tt := range tests {
		got := oprunner.IsOPURI(tt.input)
		if got != tt.want {
			t.Errorf("IsOPURI(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// FakeRunner is a test double for Runner.
type FakeRunner struct {
	ReadSecretFunc   func(ctx context.Context, uri string) ([]byte, error)
	ForgetSessionErr error
	ForgetCalled     int
}

func (f *FakeRunner) ReadSecret(ctx context.Context, uri string) ([]byte, error) {
	return f.ReadSecretFunc(ctx, uri)
}

func (f *FakeRunner) ForgetSession() error {
	f.ForgetCalled++
	return f.ForgetSessionErr
}
