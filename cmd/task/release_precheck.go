package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

type releasePrecheckSource struct {
	Commit   string
	Upstream string
}

func releasePrecheck(ctx context.Context, tag string, stdout, stderr io.Writer) error {
	version, err := parseReleaseVersion(tag)
	if err != nil {
		return err
	}
	if _, err := immediatePredecessorVersion(version); err != nil {
		return err
	}
	if err := writeReleaseNotes(version.Tag, io.Discard); err != nil {
		return fmt.Errorf("release precheck changelog: %w", err)
	}
	source, err := inspectReleasePrecheckSource(ctx, "")
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Manual pre-tag release precheck for %s at %s, contained in %s.\n", version.Tag, source.Commit, source.Upstream); err != nil {
		return err
	}

	requireFrozen := func(checkContext context.Context) error {
		_, err := inspectReleasePrecheckSource(checkContext, source.Commit)
		return err
	}
	gateErr := runReleasePrecheckGates(
		ctx,
		version.Tag,
		stdout,
		stderr,
		func(ctx context.Context, stdout, stderr io.Writer) error { return verify(ctx, stdout, stderr, false) },
		requireFrozen,
		packageCurrentSandbox,
	)
	finalContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	freezeErr := requireFrozen(finalContext)
	if err := errors.Join(gateErr, freezeErr); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Manual pre-tag release precheck passed for %s at %s.\n", version.Tag, source.Commit)
	return err
}

func runReleasePrecheckGates(
	ctx context.Context,
	tag string,
	stdout, stderr io.Writer,
	verifyIntegration func(context.Context, io.Writer, io.Writer) error,
	requireFrozen func(context.Context) error,
	installedAcceptance func(context.Context, string, io.Writer, io.Writer) error,
) error {
	if err := verifyIntegration(ctx, stdout, stderr); err != nil {
		return fmt.Errorf("release precheck integration verification: %w", err)
	}
	if err := requireFrozen(ctx); err != nil {
		return fmt.Errorf("release precheck source changed after integration verification: %w", err)
	}
	if err := installedAcceptance(ctx, tag, stdout, stderr); err != nil {
		return fmt.Errorf("release precheck installed-candidate acceptance: %w", err)
	}
	return nil
}

func inspectReleasePrecheckSource(ctx context.Context, expectedCommit string) (releasePrecheckSource, error) {
	commit, err := sourceRevision(ctx)
	if err != nil {
		return releasePrecheckSource{}, fmt.Errorf("resolve release-precheck source commit: %w", err)
	}
	statusOutput, err := hiddenCommandContext(ctx, "git", "status", "--porcelain=v1", "--untracked-files=all").CombinedOutput()
	if err != nil {
		return releasePrecheckSource{}, fmt.Errorf("inspect release-precheck source state: %w: %s", err, strings.TrimSpace(string(statusOutput)))
	}
	upstreamOutput, err := hiddenCommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}").CombinedOutput()
	if err != nil {
		return releasePrecheckSource{}, fmt.Errorf("resolve configured upstream for release-precheck: %w: %s", err, strings.TrimSpace(string(upstreamOutput)))
	}
	upstream := strings.TrimSpace(string(upstreamOutput))
	contained, err := releaseCommitContainedInUpstream(ctx, commit, upstream)
	if err != nil {
		return releasePrecheckSource{}, err
	}
	return validateReleasePrecheckSource(commit, expectedCommit, string(statusOutput), upstream, contained)
}

func validateReleasePrecheckSource(commit, expectedCommit, status, upstream string, contained bool) (releasePrecheckSource, error) {
	if expectedCommit != "" && commit != expectedCommit {
		return releasePrecheckSource{}, fmt.Errorf("release-precheck source commit changed from %s to %s", expectedCommit, commit)
	}
	if strings.TrimSpace(status) != "" {
		return releasePrecheckSource{}, errors.New("release-precheck requires a clean committed working tree")
	}
	upstream = strings.TrimSpace(upstream)
	if upstream == "" || strings.ContainsAny(upstream, "\r\n\x00") {
		return releasePrecheckSource{}, errors.New("release-precheck requires one configured branch upstream")
	}
	if !contained {
		return releasePrecheckSource{}, fmt.Errorf("release-precheck commit %s is not contained in configured upstream %s", commit, upstream)
	}
	return releasePrecheckSource{Commit: commit, Upstream: upstream}, nil
}

func releaseCommitContainedInUpstream(ctx context.Context, commit, upstream string) (bool, error) {
	command := hiddenCommandContext(ctx, "git", "merge-base", "--is-ancestor", commit, upstream)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("check release-precheck upstream containment: %w: %s", err, strings.TrimSpace(output.String()))
	}
	return true, nil
}
