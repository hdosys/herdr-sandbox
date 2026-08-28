package productidentity

import (
	"testing"
	"time"
)

func TestVersionSummaryIncludesBoundedRevision(t *testing.T) {
	originalVersion, originalRevision, originalFreshness := Version, Revision, BuildFreshness
	t.Cleanup(func() { Version, Revision, BuildFreshness = originalVersion, originalRevision, originalFreshness })
	Version = "0.0.7"
	Revision = "0123456789abcdef0123456789abcdef01234567"
	BuildFreshness = "2026.08.28.0927Z"
	if got := VersionSummary(); got != "0.0.7 2026.08.28.0927Z (0123456789ab)" {
		t.Fatalf("VersionSummary = %q", got)
	}
	Version = ""
	Revision = "unknown"
	BuildFreshness = "invalid"
	if got := VersionSummary(); got != "devel build time unknown (revision unknown)" {
		t.Fatalf("development VersionSummary = %q", got)
	}
}

func TestBuildFreshnessIsSortableUTC(t *testing.T) {
	first := FormatBuildFreshness(time.Date(2026, 8, 28, 9, 27, 59, 0, time.FixedZone("fixture", 2*60*60)))
	second := FormatBuildFreshness(time.Date(2026, 8, 28, 9, 28, 0, 0, time.UTC))
	if first != "2026.08.28.0727Z" || second != "2026.08.28.0928Z" || first >= second || !ValidBuildFreshness(first) || ValidBuildFreshness("2026.8.28.0727Z") {
		t.Fatalf("freshness values = %q, %q", first, second)
	}
}
