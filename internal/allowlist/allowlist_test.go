package allowlist_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/bestdan/opx/internal/allowlist"
)

// writeConfig writes content to a temp file with the given permission and
// returns its path.
func writeConfig(t *testing.T, content []byte, perm os.FileMode) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.json")
	if err := os.WriteFile(path, content, perm); err != nil {
		t.Fatalf("writeConfig: %v", err)
	}
	return path
}

func TestLoad_ValidFile(t *testing.T) {
	data, _ := json.Marshal(map[string]string{
		"github-token": "op://Personal/GitHub/token",
		"aws-key":      "op://Work/AWS/access_key_id",
	})
	path := writeConfig(t, data, 0o600)

	al, err := allowlist.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, name := range []string{"github-token", "aws-key"} {
		if _, ok := al.Resolve(name); !ok {
			t.Errorf("expected name %q to be resolvable", name)
		}
	}
}

func TestLoad_WorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits not applicable on Windows")
	}
	data, _ := json.Marshal(map[string]string{"k": "op://v/i/f"})
	path := writeConfig(t, data, 0o644) // world-readable

	_, err := allowlist.Load(path)
	if err == nil {
		t.Fatal("expected error for world-readable file, got nil")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := allowlist.Load("/nonexistent/path/allowlist.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	path := writeConfig(t, []byte("not json"), 0o600)
	_, err := allowlist.Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoad_InvalidURI(t *testing.T) {
	data, _ := json.Marshal(map[string]string{
		"bad-entry": "https://not-an-op-uri/",
	})
	path := writeConfig(t, data, 0o600)
	_, err := allowlist.Load(path)
	if err == nil {
		t.Fatal("expected error for invalid URI, got nil")
	}
}

func TestResolve_Found(t *testing.T) {
	want := "op://Personal/GitHub/token"
	data, _ := json.Marshal(map[string]string{"github-token": want})
	path := writeConfig(t, data, 0o600)

	al, err := allowlist.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got, ok := al.Resolve("github-token")
	if !ok {
		t.Fatal("expected Resolve to find github-token")
	}
	if got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
}

func TestResolve_NotFound(t *testing.T) {
	data, _ := json.Marshal(map[string]string{"k": "op://v/i/f"})
	path := writeConfig(t, data, 0o600)

	al, err := allowlist.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, ok := al.Resolve("nonexistent"); ok {
		t.Error("expected Resolve to return false for unknown name")
	}
}
