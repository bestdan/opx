//go:build integration

// Integration tests for oprunner. These hit the real `op` binary and a real
// 1Password vault — they cannot run on CI. Run with:
//
//	make test-integration
//
// or directly:
//
//	go test -tags=integration ./internal/oprunner/...
//
// Fixture URIs are read from scripts/.env.test (NAME=op://... per line).
// At least one URI named CREDS is required; tests skip cleanly if absent.
//
// Heads up: running these will trigger one biometric prompt per read.
package oprunner_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bestdan/opx/internal/oprunner"
)

// envFixtures loads NAME→URI pairs from scripts/.env.test, walking up from the
// test working directory until it finds the repo root.  Returns nil if the
// file does not exist.
func envFixtures(t *testing.T) map[string]string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "scripts", ".env.test")
		if _, err := os.Stat(candidate); err == nil {
			return parseEnvFile(t, candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

func parseEnvFile(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	out := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		out[line[:eq]] = line[eq+1:]
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return out
}

func requireURI(t *testing.T, name string) string {
	t.Helper()
	fix := envFixtures(t)
	if fix == nil {
		t.Skip("scripts/.env.test not found; set up fixtures to run integration tests")
	}
	uri, ok := fix[name]
	if !ok {
		t.Skipf("scripts/.env.test missing entry %q", name)
	}
	return uri
}

// withTimeout returns a context that cancels if op hangs (e.g. unresponsive
// biometric prompt).  Long enough for a human to respond, short enough not to
// wedge a CI-less local run.
func withTimeout(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// TestIntegration_SignoutFromCleanState pins the regression that motivated
// these tests: the wrapper used to call `op session forget --all`, which has
// not existed since op v2.  `op signout --all` must exit cleanly even when
// no session is active.
func TestIntegration_SignoutFromCleanState(t *testing.T) {
	r := oprunner.NewWithStderr(io.Discard)
	// Run twice — second call has definitively nothing to forget.
	if err := r.ForgetSession(); err != nil {
		t.Fatalf("first ForgetSession: %v", err)
	}
	if err := r.ForgetSession(); err != nil {
		t.Fatalf("second ForgetSession (no active session): %v", err)
	}
}

// TestIntegration_ReadSecret reads a known fixture URI and verifies the
// returned bytes are non-empty.  The test does not assert the value itself
// because that would require checking secrets into the repo.
func TestIntegration_ReadSecret(t *testing.T) {
	uri := requireURI(t, "CREDS")
	ctx, cancel := withTimeout(t)
	defer cancel()

	r := oprunner.New()
	got, err := r.ReadSecret(ctx, uri)
	if err != nil {
		t.Fatalf("ReadSecret(%s): %v", uri, err)
	}
	if len(got) == 0 {
		t.Errorf("ReadSecret returned empty bytes; expected non-empty secret")
	}
	t.Cleanup(func() { _ = r.ForgetSession() })
}

// TestIntegration_ReadSecretBogusURI verifies that a syntactically valid but
// non-existent op:// URI fails with a non-nil error and no stdout content.
func TestIntegration_ReadSecretBogusURI(t *testing.T) {
	// Skip unless fixtures are present — this test still hits real op, so we
	// gate it on the same setup as the others.
	if envFixtures(t) == nil {
		t.Skip("scripts/.env.test not found; set up fixtures to run integration tests")
	}
	ctx, cancel := withTimeout(t)
	defer cancel()

	r := oprunner.NewWithStderr(io.Discard)
	got, err := r.ReadSecret(ctx, "op://opx/this-item-does-not-exist/password")
	if err == nil {
		t.Fatalf("ReadSecret(bogus): want error, got %d bytes", len(got))
	}
	if len(got) != 0 {
		t.Errorf("ReadSecret(bogus): got %d bytes of stdout; want 0", len(got))
	}
	// Sanity: the error should not be a context deadline (i.e. op should
	// fail fast on a bad URI, not hang).
	if errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("op hung on bogus URI until timeout — unexpected")
	}
	t.Cleanup(func() { _ = r.ForgetSession() })
}
