package main

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"herdr-sandbox/internal/productidentity"
)

var (
	changelogReleaseHeadingPattern = regexp.MustCompile(`^## (v0\.0\.(?:0|[1-9][0-9]*)) - (\d{4}-\d{2}-\d{2})$`)
	releaseNoteFillerPattern       = regexp.MustCompile(`(?i)known limitations?|not tested|not included|not bundled|not a claim|internal diagnostic|local incident`)
)

func writeReleaseNotes(tag string, stdout io.Writer) error {
	version, err := parseReleaseVersion(tag)
	if err != nil {
		return err
	}
	changelog, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		return fmt.Errorf("read changelog: %w", err)
	}
	notes, err := releaseNotesForVersion(changelog, version.Tag)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(stdout, notes); err != nil {
		return fmt.Errorf("write release notes: %w", err)
	}
	return nil
}

func releaseNotesForVersion(changelog []byte, tag string) (string, error) {
	lines := strings.Split(strings.ReplaceAll(string(changelog), "\r\n", "\n"), "\n")
	start := -1
	for index, line := range lines {
		match := changelogReleaseHeadingPattern.FindStringSubmatch(line)
		if len(match) != 3 || match[1] != tag {
			continue
		}
		if start >= 0 {
			return "", fmt.Errorf("changelog contains duplicate release heading %s", tag)
		}
		if _, err := time.Parse("2006-01-02", match[2]); err != nil {
			return "", fmt.Errorf("changelog release date for %s is invalid: %w", tag, err)
		}
		start = index + 1
	}
	if start < 0 {
		return "", fmt.Errorf("changelog release heading is missing: %s", tag)
	}
	end := len(lines)
	for index := start; index < len(lines); index++ {
		if strings.HasPrefix(lines[index], "## ") {
			end = index
			break
		}
	}
	body := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
	if body == "" || !strings.HasPrefix(body, "### ") {
		return "", fmt.Errorf("changelog release section is empty or unstructured: %s", tag)
	}
	if match := releaseNoteFillerPattern.FindString(body); match != "" {
		return "", fmt.Errorf("changelog release section contains portfolio-irrelevant copy %q: %s", match, tag)
	}
	return body + "\n\nSee the [setup and usage guide](" + productidentity.ProductURL + "#readme).\n", nil
}
