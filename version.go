package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// versionInfo decomposes the embedded build version string (produced by
// `git describe --tags --always --dirty`) into the parts we care about for
// rendering and remote comparison.
//
// Supported inputs:
//
//	v0.1.0                       → tag=v0.1.0
//	v0.1.0-dirty                 → tag=v0.1.0,                 dirty
//	v0.1.0-5-gabcdef             → tag=v0.1.0, ahead=5, sha=abcdef
//	v0.1.0-5-gabcdef-dirty       → tag=v0.1.0, ahead=5, sha=abcdef, dirty
//	b6a3299                      → sha=b6a3299                  (no tags yet)
//	b6a3299-dirty                → sha=b6a3299, dirty           (no tags yet)
//	dev                          → raw="dev"                    (unstripped go run)
type versionInfo struct {
	raw   string // the original ldflag value
	tag   string // most recent semver tag, e.g. "v0.1.0"; empty if none
	ahead int    // commits past tag; 0 means "exactly on tag"
	sha   string // short commit sha; empty if not encoded
	dirty bool   // working tree had uncommitted changes at build time
}

// describeRE matches the full output of `git describe --tags --dirty`.
// Groups: 1=tag, 2=ahead count, 3=sha (without leading 'g'), 4=dirty marker.
var describeRE = regexp.MustCompile(`^(v[0-9]+\.[0-9]+\.[0-9]+)(?:-([0-9]+)-g([0-9a-f]+))?(-dirty)?$`)

// shaOnlyRE matches the no-tags-yet fallback: a bare sha optionally followed
// by `-dirty`. Tight bound on length stops it from swallowing the
// describe-form when the regex above misfires.
var shaOnlyRE = regexp.MustCompile(`^([0-9a-f]{7,40})(-dirty)?$`)

func parseVersion(s string) versionInfo {
	v := versionInfo{raw: s}
	if m := describeRE.FindStringSubmatch(s); m != nil {
		v.tag = m[1]
		if m[2] != "" {
			v.ahead, _ = strconv.Atoi(m[2])
			v.sha = m[3]
		}
		v.dirty = m[4] != ""
		return v
	}
	if m := shaOnlyRE.FindStringSubmatch(s); m != nil {
		v.sha = m[1]
		v.dirty = m[2] != ""
		return v
	}
	return v // raw only — covers "dev" and other oddballs
}

// label returns the human-readable rendering printed by --version.
//
// Format intentionally surfaces *why* the binary is dev vs release: an exact
// clean tag is a release; anything else (commits past tag, uncommitted
// changes, or no tag at all) is dev with the specific reason spelled out.
func (v versionInfo) label() string {
	switch {
	case v.tag != "" && v.ahead == 0 && !v.dirty:
		return fmt.Sprintf("opx %s (release)", v.tag)

	case v.tag != "" && v.ahead == 0 && v.dirty:
		return fmt.Sprintf("opx %s+dirty (dev — uncommitted changes)", v.tag)

	case v.tag != "" && v.ahead > 0:
		commits := "commits"
		if v.ahead == 1 {
			commits = "commit"
		}
		suffix := fmt.Sprintf("+%d.g%s", v.ahead, v.sha)
		extra := ""
		if v.dirty {
			suffix += ".dirty"
			extra = ", uncommitted changes"
		}
		return fmt.Sprintf("opx %s%s (dev — %d %s past %s%s)",
			v.tag, suffix, v.ahead, commits, v.tag, extra)

	case v.sha != "":
		s := "opx dev-" + v.sha
		if v.dirty {
			s += "-dirty"
			return s + " (pre-release dev build, uncommitted changes)"
		}
		return s + " (pre-release dev build)"

	default:
		return "opx " + v.raw
	}
}

// printVersion writes the local version label to out. With check=true it
// also queries the GitHub releases API and appends a comparison line to out
// on success, or a one-line warning to errOut on failure. The remote check
// is best-effort: failures do not affect the exit code.
func printVersion(out, errOut io.Writer, v versionInfo, check bool) {
	fmt.Fprintln(out, v.label())
	if !check {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	latest, err := fetchLatestReleaseTag(ctx, "bestdan", "opx")
	if err != nil {
		fmt.Fprintf(errOut, "latest release: check failed (%v)\n", err)
		return
	}
	fmt.Fprintf(out, "latest release: %s — %s\n", latest, compareToRelease(v, latest))
}

// compareToRelease renders the second line of `--version --check`. Returns
// human-language status, not a structured token, because this is end-user
// output and the semantics are easier to read in prose.
func compareToRelease(v versionInfo, latest string) string {
	if v.tag == "" {
		// No tag in the build — could be a fork, a shallow clone with tags
		// stripped, or a build before any release existed. We can't order
		// against `latest`, so report without claiming direction.
		return fmt.Sprintf("local build has no tag metadata; latest published release is %s", latest)
	}
	cmp, err := compareSemver(v.tag, latest)
	if err != nil {
		return "comparison unavailable: " + err.Error()
	}
	switch {
	case cmp < 0:
		return fmt.Sprintf("you are behind: install %s to update", latest)
	case cmp > 0:
		return fmt.Sprintf("you are on a tag newer than the published release (%s > %s)", v.tag, latest)
	default:
		// Same tag — distinguish "past" (commits ahead) from "on tag with
		// uncommitted changes" so a dirty rebuild of the release isn't
		// mislabelled as ahead.
		switch {
		case v.ahead > 0:
			return "you are on a dev build past the latest release"
		case v.dirty:
			return "you are on the latest release with uncommitted changes"
		default:
			return "you are up to date"
		}
	}
}

// githubAPIBase is the GitHub REST API root. Overridable so tests can point
// at an httptest.Server.
var githubAPIBase = "https://api.github.com"

// fetchLatestReleaseTag returns the tag_name of the most recent published
// release on GitHub. Public repos don't require auth, so we hit the REST
// API directly with stdlib net/http — no gh dependency, no token.
func fetchLatestReleaseTag(ctx context.Context, owner, repo string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", githubAPIBase, owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "opx-version-check")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", errors.New("no releases published yet")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if payload.TagName == "" {
		return "", errors.New("response missing tag_name")
	}
	return payload.TagName, nil
}

// compareSemver compares two semver tags of the form vX.Y.Z. Returns -1, 0,
// or 1. Pre-release suffixes (`v1.2.3-rc1`) are deliberately not supported —
// opx hasn't needed them and the simpler parser keeps the code honest.
func compareSemver(a, b string) (int, error) {
	pa, err := parseSemver(a)
	if err != nil {
		return 0, err
	}
	pb, err := parseSemver(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1, nil
			}
			return 1, nil
		}
	}
	return 0, nil
}

func parseSemver(s string) ([3]int, error) {
	var out [3]int
	s = strings.TrimPrefix(s, "v")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("not a vX.Y.Z tag: %q", s)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, fmt.Errorf("non-numeric component in %q", s)
		}
		out[i] = n
	}
	return out, nil
}
