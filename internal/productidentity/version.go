package productidentity

import "strings"

var (
	Version  = "devel"
	Revision = "unknown"
)

func VersionSummary() string {
	version := strings.TrimSpace(Version)
	if version == "" {
		version = "devel"
	}
	revision := strings.TrimSpace(Revision)
	if revision == "" || revision == "unknown" {
		return version
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	return version + " (" + revision + ")"
}
