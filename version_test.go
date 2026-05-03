package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in    string
		tag   string
		ahead int
		sha   string
		dirty bool
	}{
		{"v0.1.0", "v0.1.0", 0, "", false},
		{"v0.1.0-dirty", "v0.1.0", 0, "", true},
		{"v0.1.0-5-gabcdef", "v0.1.0", 5, "abcdef", false},
		{"v0.1.0-5-gabcdef-dirty", "v0.1.0", 5, "abcdef", true},
		{"v1.10.2-100-gdeadbeef", "v1.10.2", 100, "deadbeef", false},
		{"b6a3299", "", 0, "b6a3299", false},
		{"b6a3299-dirty", "", 0, "b6a3299", true},
		{"dev", "", 0, "", false}, // unknown shape — preserved in raw only
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := parseVersion(tc.in)
			if got.tag != tc.tag || got.ahead != tc.ahead || got.sha != tc.sha || got.dirty != tc.dirty {
				t.Errorf("parseVersion(%q) = {tag:%q ahead:%d sha:%q dirty:%v}, want {tag:%q ahead:%d sha:%q dirty:%v}",
					tc.in, got.tag, got.ahead, got.sha, got.dirty, tc.tag, tc.ahead, tc.sha, tc.dirty)
			}
		})
	}
}

func TestVersionLabel(t *testing.T) {
	cases := []struct {
		in           string
		mustContain  []string
		mustNotMatch []string
	}{
		{"v0.1.0", []string{"v0.1.0", "(release)"}, []string{"dev"}},
		{"v0.1.0-dirty", []string{"v0.1.0+dirty", "uncommitted"}, []string{"(release)"}},
		{"v0.1.0-5-gabcdef", []string{"v0.1.0+5.gabcdef", "5 commits past v0.1.0"}, []string{"(release)", "uncommitted"}},
		{"v0.1.0-1-gabcdef", []string{"1 commit past v0.1.0"}, []string{"1 commits"}}, // singular
		{"v0.1.0-5-gabcdef-dirty", []string{"v0.1.0+5.gabcdef.dirty", "uncommitted"}, []string{"(release)"}},
		{"b6a3299", []string{"dev-b6a3299", "pre-release"}, []string{"(release)", "uncommitted"}},
		{"b6a3299-dirty", []string{"dev-b6a3299-dirty", "uncommitted"}, []string{"(release)"}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := parseVersion(tc.in).label()
			for _, want := range tc.mustContain {
				if !strings.Contains(got, want) {
					t.Errorf("label(%q) = %q; missing substring %q", tc.in, got, want)
				}
			}
			for _, banned := range tc.mustNotMatch {
				if strings.Contains(got, banned) {
					t.Errorf("label(%q) = %q; should not contain %q", tc.in, got, banned)
				}
			}
		})
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.1.0", "v0.1.0", 0},
		{"v0.1.0", "v0.2.0", -1},
		{"v0.2.0", "v0.1.0", 1},
		{"v1.0.0", "v0.99.99", 1},
		{"v0.1.10", "v0.1.2", 1}, // numeric, not lexical
	}
	for _, tc := range cases {
		got, err := compareSemver(tc.a, tc.b)
		if err != nil {
			t.Errorf("compareSemver(%q,%q): unexpected error %v", tc.a, tc.b, err)
			continue
		}
		if got != tc.want {
			t.Errorf("compareSemver(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
	if _, err := compareSemver("v1.0", "v1.0.0"); err == nil {
		t.Error("expected error on malformed semver, got nil")
	}
}

func TestCompareToRelease(t *testing.T) {
	cases := []struct {
		v       versionInfo
		latest  string
		mustHas string
	}{
		{parseVersion("v0.1.0"), "v0.1.0", "up to date"},
		{parseVersion("v0.1.0-3-gabcdef"), "v0.1.0", "dev build past"},
		{parseVersion("v0.1.0"), "v0.2.0", "behind"},
		{parseVersion("v0.3.0"), "v0.2.0", "newer than the published release"},
		{parseVersion("b6a3299"), "v0.1.0", "pre-release dev build"},
	}
	for _, tc := range cases {
		got := compareToRelease(tc.v, tc.latest)
		if !strings.Contains(got, tc.mustHas) {
			t.Errorf("compareToRelease(%+v, %q) = %q; want substring %q", tc.v, tc.latest, got, tc.mustHas)
		}
	}
}

func TestPrintVersion_Local(t *testing.T) {
	var buf bytes.Buffer
	printVersion(&buf, parseVersion("v0.1.0"), false)
	got := buf.String()
	if !strings.Contains(got, "v0.1.0") || !strings.Contains(got, "(release)") {
		t.Errorf("printVersion output missing release label: %q", got)
	}
	if strings.Contains(got, "latest release") {
		t.Errorf("printVersion(check=false) leaked a remote-check line: %q", got)
	}
}
