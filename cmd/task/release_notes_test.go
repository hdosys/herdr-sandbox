package main

import (
	"strings"
	"testing"
)

func TestReleaseNotesForVersionLinksTaggedChangelogWithoutDuplication(t *testing.T) {
	changelog := []byte(`# Changelog

## Unreleased

## v0.0.9 - 2026-08-05

### Added

- Shipped user value.

## v0.0.8 - 2026-08-04

### Fixed

- Prior release detail.
`)
	notes, err := releaseNotesForVersion(changelog, "v0.0.9")
	if err != nil {
		t.Fatal(err)
	}
	want := "See [CHANGELOG.md for v0.0.9](https://github.com/hdosys/herdr-sandbox/blob/v0.0.9/CHANGELOG.md).\n"
	if notes != want {
		t.Fatalf("release notes = %q, want %q", notes, want)
	}
	for _, duplicated := range []string{"### Added", "Shipped user value", "Prior release detail"} {
		if strings.Contains(notes, duplicated) {
			t.Fatalf("release notes duplicate changelog content %q: %s", duplicated, notes)
		}
	}
}

func TestReleaseNotesForVersionRejectsMissingDuplicateEmptyOrNegativeCopy(t *testing.T) {
	for name, changelog := range map[string]string{
		"missing":   "## Unreleased\n",
		"duplicate": "## v0.0.9 - 2026-08-05\n### Added\n- Value.\n## v0.0.9 - 2026-08-05\n### Added\n- Value.\n",
		"empty":     "## v0.0.9 - 2026-08-05\n\n## v0.0.8 - 2026-08-04\n",
		"negative":  "## v0.0.9 - 2026-08-05\n### Known limitations\n- Not tested.\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := releaseNotesForVersion([]byte(changelog), "v0.0.9"); err == nil {
				t.Fatal("invalid release notes unexpectedly passed")
			}
		})
	}
}
