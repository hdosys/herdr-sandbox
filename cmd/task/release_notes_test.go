package main

import (
	"strings"
	"testing"
)

func TestReleaseNotesForVersionExtractsOnlyCuratedSection(t *testing.T) {
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
	for _, required := range []string{"### Added", "Shipped user value", "setup and usage guide"} {
		if !strings.Contains(notes, required) {
			t.Fatalf("release notes are missing %q: %s", required, notes)
		}
	}
	if strings.Contains(notes, "Prior release detail") || strings.Contains(notes, "## v0.0.8") {
		t.Fatalf("release notes include another version: %s", notes)
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
