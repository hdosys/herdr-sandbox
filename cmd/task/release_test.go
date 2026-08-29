package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReleaseRunsEachGateOnceInPreTagOrder(t *testing.T) {
	var events []string
	verifyCalls := 0
	installedCalls := 0
	err := runReleaseGates(
		t.Context(),
		"v0.0.42",
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(context.Context, io.Writer, io.Writer) error {
			events = append(events, "resource-preflight")
			return nil
		},
		func(context.Context, io.Writer, io.Writer) error {
			verifyCalls++
			events = append(events, "verify-integration")
			return nil
		},
		func(context.Context) error {
			events = append(events, "frozen-source")
			return nil
		},
		func(_ context.Context, tag string, _, _ io.Writer) error {
			installedCalls++
			events = append(events, "installed-candidate:"+tag)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if verifyCalls != 1 || installedCalls != 1 {
		t.Fatalf("gate calls = verify:%d installed:%d", verifyCalls, installedCalls)
	}
	want := "resource-preflight,verify-integration,frozen-source,installed-candidate:v0.0.42"
	if strings.Join(events, ",") != want {
		t.Fatalf("release events = %q, want %q", events, want)
	}
}

func TestReleaseStopsBeforeInstalledAcceptanceOnEarlierFailure(t *testing.T) {
	installedCalled := false
	err := runReleaseGates(
		t.Context(),
		"v0.0.42",
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(context.Context, io.Writer, io.Writer) error { return nil },
		func(context.Context, io.Writer, io.Writer) error { return errors.New("integration failed") },
		func(context.Context) error { return nil },
		func(context.Context, string, io.Writer, io.Writer) error {
			installedCalled = true
			return nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "integration verification") || installedCalled {
		t.Fatalf("release failure = %v, installed called = %t", err, installedCalled)
	}
}

func TestReleaseStopsBeforeIntegrationOnResourceFailure(t *testing.T) {
	verifyCalled := false
	err := runReleaseGates(
		t.Context(),
		"v0.0.42",
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(context.Context, io.Writer, io.Writer) error { return errors.New("insufficient capacity") },
		func(context.Context, io.Writer, io.Writer) error {
			verifyCalled = true
			return nil
		},
		func(context.Context) error { return nil },
		func(context.Context, string, io.Writer, io.Writer) error { return nil },
	)
	if err == nil || !strings.Contains(err.Error(), "resource preflight") || verifyCalled {
		t.Fatalf("release failure = %v, integration called = %t", err, verifyCalled)
	}
}

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

func TestAcceptedReleaseTagAnnotationBindsTagAndCommit(t *testing.T) {
	commit := "0123456789abcdef0123456789abcdef01234567"
	if got, want := acceptedReleaseTagAnnotation("v0.0.42", commit), "Herdr Sandbox installed candidate accepted: v0.0.42 "+commit; got != want {
		t.Fatalf("release tag annotation = %q, want %q", got, want)
	}
	if remote, err := releaseRemoteForUpstream("origin/main"); err != nil || remote != "origin" {
		t.Fatalf("release remote = %q, %v", remote, err)
	}
	for _, upstream := range []string{"", "main", "origin /main", "origin\t/main"} {
		if _, err := releaseRemoteForUpstream(upstream); err == nil {
			t.Fatalf("unsafe release upstream %q unexpectedly validated", upstream)
		}
	}
}
