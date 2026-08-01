package uri_test

import (
	"strings"
	"testing"

	"github.com/bestdan/opx/internal/uri"
)

func TestIsOPURI(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"op://Vault/Item/field", true},
		{"op://My Vault/My Item/password", true},
		{"op://v/i/f/extra", true},         // extra segment is fine
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
		got := uri.IsOPURI(tt.input)
		if got != tt.want {
			t.Errorf("IsOPURI(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// TestIsOPURI_RejectsControlCharacters is the defense-in-depth half of F2/F4/F8.
// The URI is interpolated into the confirmation dialog — opx's whole trust
// boundary — and is attacker-controlled in every input mode. internal/prompt
// escapes these on the way out; this rejects them on the way in, so the dialog
// is never drawn at all.
func TestIsOPURI_RejectsControlCharacters(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"ESC in the vault segment", "op://v\x1b[2Kault/item/field"},
		{"CR in the field segment", "op://vault/item/field\rop://decoy/item/field"},
		{"LF anywhere", "op://vault/item/field\nextra"},
		{"NUL", "op://vault/item/fie\x00ld"},
		{"BEL", "op://vault/item/\x07field"},
		{"TAB", "op://vault/\titem/field"},
		{"DEL", "op://vault/item/field\x7f"},
		{"C1 CSI as a rune (U+009B)", "op://vault/item/field\u009b[2K"},
		{"C1 NEL as a rune (U+0085)", "op://vault\u0085/item/field"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if uri.IsOPURI(tc.input) {
				t.Errorf("IsOPURI(%q) = true, want false — a control character must fail before any prompt is drawn", tc.input)
			}
		})
	}
}

// TestIsOPURI_AcceptsRealVaultNames is the other half of the same rule: real
// 1Password vault, item and field names carry spaces, punctuation and unicode,
// so the check has to reject control characters specifically rather than
// "anything unusual". Several of these encode bytes in 0x80–0x9f as UTF-8
// continuation bytes, which is exactly why the C1 test is over runes and not
// over bytes — a byte scan would fail every case below with a non-ASCII
// character in it.
func TestIsOPURI_AcceptsRealVaultNames(t *testing.T) {
	cases := []string{
		"op://Personal/Café/pässwörd",
		"op://Team Vault/AWS (prod)/secret_key",
		"op://Personal/‘quoted’ item/field",
		"op://日本/項目/フィールド",
		"op://Personal/em—dash/field",
		"op://Personal/item.with.dots/field-with-dashes",
		"op://Personal/100% legit/n°1",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if !uri.IsOPURI(in) {
				t.Errorf("IsOPURI(%q) = false, want true — real vault names carry unicode and punctuation", in)
			}
		})
	}
}

// TestIsOPURI_RejectsInvalidUTF8 covers the gap a rune-only control check
// leaves. Ranging over a string decodes an invalid byte as U+FFFD, so a raw
// 0x9b passes a rune test. It cannot reach AppleScript — sanitizeDisplay ranges
// over runes too — but it would mean the user approves a dialog reading "�"
// while `op` receives the original byte. The approved text and the used text
// have to be the same string.
func TestIsOPURI_RejectsInvalidUTF8(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"raw C1 byte", "op://vault/item/field" + string([]byte{0x9b})},
		{"lone continuation byte", "op://vault/item/f" + string([]byte{0x80}) + "ield"},
		{"truncated multi-byte sequence", "op://vault/item/field" + string([]byte{0xe2, 0x80})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if uri.IsOPURI(tc.input) {
				t.Errorf("IsOPURI(%q) = true, want false — invalid UTF-8 renders as U+FFFD, so the approved text would differ from the bytes op receives", tc.input)
			}
		})
	}
}

// TestIsOPURI_LengthCap pins the one hard limit on otherwise legitimate input.
// A URI the dialog cannot display in full is one the user cannot review, and a
// truncation that hides the tail is how a URI lies about which secret it names.
func TestIsOPURI_LengthCap(t *testing.T) {
	const prefix = "op://vault/item/"
	atLimit := prefix + strings.Repeat("f", 1024-len(prefix))
	if len(atLimit) != 1024 {
		t.Fatalf("test setup: at-limit URI is %d bytes, want 1024", len(atLimit))
	}
	if !uri.IsOPURI(atLimit) {
		t.Errorf("IsOPURI rejected a %d-byte URI, want the limit itself to be accepted", len(atLimit))
	}
	overLimit := atLimit + "f"
	if uri.IsOPURI(overLimit) {
		t.Errorf("IsOPURI accepted a %d-byte URI, want it rejected", len(overLimit))
	}
}
