package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
		{parseVersion("b6a3299"), "v0.1.0", "no tag metadata"},
		{parseVersion("v0.1.0-dirty"), "v0.1.0", "uncommitted changes"},
		{parseVersion("v0.1.0-2-gabcdef-dirty"), "v0.1.0", "past the latest release"},
	}
	for _, tc := range cases {
		got := compareToRelease(tc.v, tc.latest)
		if !strings.Contains(got, tc.mustHas) {
			t.Errorf("compareToRelease(%+v, %q) = %q; want substring %q", tc.v, tc.latest, got, tc.mustHas)
		}
	}
}

func TestFetchLatestReleaseTag(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantTag string
		wantErr string // substring; "" means no error
	}{
		{"ok", http.StatusOK, `{"tag_name":"v1.2.3"}`, "v1.2.3", ""},
		{"empty tag", http.StatusOK, `{"tag_name":""}`, "", "missing tag_name"},
		{"not found", http.StatusNotFound, ``, "", "no releases"},
		{"server error", http.StatusInternalServerError, ``, "", "GitHub API returned"},
		{"bad json", http.StatusOK, `{not json`, "", ""}, // json error message varies
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got, want := r.URL.Path, "/repos/o/r/releases/latest"; got != want {
					t.Errorf("path = %q, want %q", got, want)
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			orig := githubAPIBase
			githubAPIBase = srv.URL
			defer func() { githubAPIBase = orig }()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			tag, err := fetchLatestReleaseTag(ctx, "o", "r")
			if tag != tc.wantTag {
				t.Errorf("tag = %q, want %q", tag, tc.wantTag)
			}
			if tc.wantErr == "" && tc.name != "bad json" && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Errorf("err = %v, want substring %q", err, tc.wantErr)
			}
			if tc.name == "bad json" && err == nil {
				t.Error("expected json decode error, got nil")
			}
		})
	}
}

func TestPrintVersion_CheckSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v0.2.0"}`))
	}))
	defer srv.Close()
	orig := githubAPIBase
	githubAPIBase = srv.URL
	defer func() { githubAPIBase = orig }()

	var out, errOut bytes.Buffer
	printVersion(&out, &errOut, parseVersion("v0.1.0"), true)
	if !strings.Contains(out.String(), "latest release: v0.2.0") {
		t.Errorf("stdout missing latest line: %q", out.String())
	}
	if !strings.Contains(out.String(), "behind") {
		t.Errorf("stdout missing comparison: %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("errOut should be empty on success: %q", errOut.String())
	}
}

func TestPrintVersion_CheckFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	orig := githubAPIBase
	githubAPIBase = srv.URL
	defer func() { githubAPIBase = orig }()

	var out, errOut bytes.Buffer
	printVersion(&out, &errOut, parseVersion("v0.1.0"), true)
	// Local label still goes to stdout.
	if !strings.Contains(out.String(), "v0.1.0") {
		t.Errorf("stdout missing local label: %q", out.String())
	}
	// Failure warning goes to stderr only.
	if strings.Contains(out.String(), "check failed") {
		t.Errorf("failure warning leaked to stdout: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "check failed") {
		t.Errorf("errOut missing failure warning: %q", errOut.String())
	}
}

func TestPrintVersion_Local(t *testing.T) {
	var out, errOut bytes.Buffer
	printVersion(&out, &errOut, parseVersion("v0.1.0"), false)
	got := out.String()
	if !strings.Contains(got, "v0.1.0") || !strings.Contains(got, "(release)") {
		t.Errorf("printVersion output missing release label: %q", got)
	}
	if strings.Contains(got, "latest release") {
		t.Errorf("printVersion(check=false) leaked a remote-check line: %q", got)
	}
	if errOut.Len() != 0 {
		t.Errorf("printVersion(check=false) wrote to errOut: %q", errOut.String())
	}
}
