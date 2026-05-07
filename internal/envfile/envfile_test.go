package envfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bestdan/opx/internal/envfile"
)

func TestParse_BasicAndComments(t *testing.T) {
	in := strings.NewReader(`
# leading comment
FOO=op://V/I/f
# another comment

BAR=plain-value
   # indented comment
BAZ=op://V/I/g
`)
	got, err := envfile.Parse(in, "test")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []envfile.Entry{
		{Name: "FOO", Value: "op://V/I/f", Line: 3},
		{Name: "BAR", Value: "plain-value", Line: 6},
		{Name: "BAZ", Value: "op://V/I/g", Line: 8},
	}
	if len(got) != len(want) {
		t.Fatalf("entries = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParse_Quotes(t *testing.T) {
	in := strings.NewReader(`A="op://V/I/f"
B='op://V/I/g'
C="unbalanced
D=plain
E=""
`)
	got, err := envfile.Parse(in, "test")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[string]string{
		"A": "op://V/I/f",
		"B": "op://V/I/g",
		"C": `"unbalanced`,
		"D": "plain",
		"E": "",
	}
	if len(got) != len(want) {
		t.Fatalf("entries = %d, want %d", len(got), len(want))
	}
	for _, e := range got {
		if want[e.Name] != e.Value {
			t.Errorf("%s = %q, want %q", e.Name, e.Value, want[e.Name])
		}
	}
}

func TestParse_ExportPrefix(t *testing.T) {
	in := strings.NewReader("export FOO=op://V/I/f\n")
	got, err := envfile.Parse(in, "test")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 1 || got[0].Name != "FOO" || got[0].Value != "op://V/I/f" {
		t.Errorf("got %+v, want single FOO entry", got)
	}
}

func TestParse_Errors(t *testing.T) {
	cases := map[string]string{
		"no equals":     "FOO\n",
		"empty name":    "=value\n",
		"bad name":      "1FOO=v\n",
		"hyphen name":   "FOO-BAR=v\n",
		"name with dot": "foo.bar=v\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := envfile.Parse(strings.NewReader(body), "test")
			if err == nil {
				t.Errorf("Parse(%q) returned nil error", body)
			}
		})
	}
}

func TestParseFile_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.env")
	body := "# header\nFOO=op://V/I/f\nBAR=literal\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := envfile.ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2", len(got))
	}
}

func TestParseFile_Missing(t *testing.T) {
	_, err := envfile.ParseFile(filepath.Join(t.TempDir(), "nope.env"))
	if err == nil {
		t.Fatal("ParseFile(missing) returned nil error")
	}
}

