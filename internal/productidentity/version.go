package productidentity

import (
	"strings"
	"time"
)

const BuildFreshnessLayout = "2006.01.02.1504Z"

var (
	Version        = "devel"
	Revision       = "unknown"
	BuildFreshness = "unknown"
)

func VersionSummary() string {
	return IdentitySummary(Version, BuildFreshness, Revision)
}

func FormatBuildFreshness(value time.Time) string {
	return value.UTC().Format(BuildFreshnessLayout)
}

func ValidBuildFreshness(value string) bool {
	value = strings.TrimSpace(value)
	parsed, err := time.Parse(BuildFreshnessLayout, value)
	return err == nil && parsed.Format(BuildFreshnessLayout) == value
}

func IdentitySummary(value, freshness, revision string) string {
	version := strings.TrimSpace(value)
	if version == "" {
		version = "devel"
	}
	freshness = strings.TrimSpace(freshness)
	if !ValidBuildFreshness(freshness) {
		freshness = "build time unknown"
	}
	revision = strings.TrimSpace(revision)
	if revision == "" || revision == "unknown" {
		return version + " " + freshness + " (revision unknown)"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	return version + " " + freshness + " (" + revision + ")"
}
