package productidentity

import "testing"

func TestVersionSummaryIncludesBoundedRevision(t *testing.T) {
	originalVersion, originalRevision := Version, Revision
	t.Cleanup(func() { Version, Revision = originalVersion, originalRevision })
	Version = "0.0.7"
	Revision = "0123456789abcdef0123456789abcdef01234567"
	if got := VersionSummary(); got != "0.0.7 (0123456789ab)" {
		t.Fatalf("VersionSummary = %q", got)
	}
	Version = ""
	Revision = "unknown"
	if got := VersionSummary(); got != "devel (revision unknown)" {
		t.Fatalf("development VersionSummary = %q", got)
	}
}
