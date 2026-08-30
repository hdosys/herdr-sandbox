package main

import (
	"strings"
	"testing"
)

func TestValidateReleaseSourceRequiresCleanFrozenUpstreamCommit(t *testing.T) {
	commit := "0123456789abcdef0123456789abcdef01234567"
	source, err := validateReleaseSource(commit, commit, "", " origin/main\r\n", true)
	if err != nil {
		t.Fatal(err)
	}
	if source.Commit != commit || source.Upstream != "origin/main" {
		t.Fatalf("release source = %#v", source)
	}
	for name, test := range map[string]struct {
		expected  string
		status    string
		upstream  string
		contained bool
		want      string
	}{
		"changed":       {expected: strings.Repeat("a", 40), upstream: "origin/main", contained: true, want: "source commit changed"},
		"dirty":         {expected: commit, status: " M cmd/task/main.go", upstream: "origin/main", contained: true, want: "clean committed"},
		"no upstream":   {expected: commit, contained: true, want: "configured branch upstream"},
		"not contained": {expected: commit, upstream: "origin/main", want: "not contained"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := validateReleaseSource(commit, test.expected, test.status, test.upstream, test.contained)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validation error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestReleaseRemoteRequiresNamedUpstream(t *testing.T) {
	if remote, err := releaseRemoteForUpstream("origin/main"); err != nil || remote != "origin" {
		t.Fatalf("release remote = %q, %v", remote, err)
	}
	for _, upstream := range []string{"", "main", "origin /main", "origin\t/main"} {
		if _, err := releaseRemoteForUpstream(upstream); err == nil {
			t.Fatalf("unsafe release upstream %q unexpectedly validated", upstream)
		}
	}
}
