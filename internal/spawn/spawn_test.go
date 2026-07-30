package spawn_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bestdan/opx/internal/spawn"
)

func TestSpawn_EmptyArgv(t *testing.T) {
	_, err := spawn.New().Spawn(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("Spawn(nil) returned no error")
	}
}

func TestSpawn_RunsRealBinary(t *testing.T) {
	// A smoke test of the exec wiring, not a platform compatibility check.
	// `true` is POSIX and present on every host CI runs on, so a failure to
	// find it is a real problem worth failing on.
	code, err := spawn.New().Spawn(context.Background(), []string{"true"}, nil)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			t.Skip("context cancelled unexpectedly")
		}
		t.Fatalf("Spawn(true): %v", err)
	}
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
}

func TestSpawn_PropagatesNonZero(t *testing.T) {
	code, err := spawn.New().Spawn(context.Background(), []string{"false"}, nil)
	if err != nil {
		t.Fatalf("Spawn(false): %v", err)
	}
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
}
