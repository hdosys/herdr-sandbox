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

type releaseSource struct {
	Commit   string
	Upstream string
}

func release(ctx context.Context, tag string, stdout, stderr io.Writer) error {
	version, err := parseReleaseVersion(tag)
	if err != nil {
		return err
	}
	if _, err := immediatePredecessorVersion(version); err != nil {
		return err
	}
	if err := writeReleaseNotes(version.Tag, io.Discard); err != nil {
		return fmt.Errorf("release changelog: %w", err)
	}
	source, err := inspectReleaseSource(ctx, "")
	if err != nil {
		return err
	}
	remote, err := releaseRemoteForUpstream(source.Upstream)
	if err != nil {
		return err
	}
	if err := ensureReleaseTagAbsent(ctx, version.Tag, remote); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Release acceptance for %s at %s, contained in %s.\n", version.Tag, source.Commit, source.Upstream); err != nil {
		return err
	}

	requireFrozen := func(checkContext context.Context) error {
		_, err := inspectReleaseSource(checkContext, source.Commit)
		return err
	}
	gateErr := runReleaseGates(
		ctx,
		version.Tag,
		stdout,
		stderr,
		currentSandboxProvisioningPreflight,
		func(ctx context.Context, stdout, stderr io.Writer) error { return verify(ctx, stdout, stderr, false) },
		requireFrozen,
		packageCurrentSandbox,
	)
	finalContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	freezeErr := requireFrozen(finalContext)
	if err := errors.Join(gateErr, freezeErr); err != nil {
		return err
	}
	if err := ensureReleaseTagAbsent(finalContext, version.Tag, remote); err != nil {
		return err
	}
	if err := publishAcceptedReleaseTag(finalContext, version.Tag, source.Commit, remote); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Accepted release tag %s for %s was pushed to %s.\n", version.Tag, source.Commit, remote)
	return err
}

func releaseRemoteForUpstream(upstream string) (string, error) {
	upstream = strings.TrimSpace(upstream)
	remote, _, found := strings.Cut(upstream, "/")
	if !found || remote == "" || strings.ContainsAny(remote, "\x00\r\n\t ") {
		return "", fmt.Errorf("release requires a named remote branch upstream, got %q", upstream)
	}
	return remote, nil
}

func acceptedReleaseTagAnnotation(tag, commit string) string {
	return fmt.Sprintf("Herdr Sandbox installed candidate accepted: %s %s", tag, commit)
}

func validateAcceptedReleaseTag(ctx context.Context, tag string) error {
	version, err := parseReleaseVersion(tag)
	if err != nil {
		return err
	}
	ref := "refs/tags/" + version.Tag
	tagType, err := hiddenCommandContext(ctx, "git", "cat-file", "-t", ref).CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect release tag object %s: %w: %s", version.Tag, err, strings.TrimSpace(string(tagType)))
	}
	if strings.TrimSpace(string(tagType)) != "tag" {
		return fmt.Errorf("release tag %s is not annotated", version.Tag)
	}
	commitOutput, err := hiddenCommandContext(ctx, "git", "rev-parse", version.Tag+"^{commit}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("resolve release tag commit %s: %w: %s", version.Tag, err, strings.TrimSpace(string(commitOutput)))
	}
	commit, err := normalizeSourceRevision(strings.TrimSpace(string(commitOutput)))
	if err != nil {
		return fmt.Errorf("validate release tag commit %s: %w", version.Tag, err)
	}
	head, err := sourceRevision(ctx)
	if err != nil {
		return fmt.Errorf("resolve release workflow source commit: %w", err)
	}
	if commit != head {
		return fmt.Errorf("release tag %s targets %s, not checked-out commit %s", version.Tag, commit, head)
	}
	annotationOutput, err := hiddenCommandContext(ctx, "git", "for-each-ref", "--format=%(contents)", ref).CombinedOutput()
	if err != nil {
		return fmt.Errorf("read release tag annotation %s: %w: %s", version.Tag, err, strings.TrimSpace(string(annotationOutput)))
	}
	if annotation := strings.TrimSpace(string(annotationOutput)); annotation != acceptedReleaseTagAnnotation(version.Tag, commit) {
		return fmt.Errorf("release tag %s is not the exact installed-candidate acceptance output", version.Tag)
	}
	return nil
}

func ensureReleaseTagAbsent(ctx context.Context, tag, remote string) error {
	command := hiddenCommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/tags/"+tag)
	if err := command.Run(); err == nil {
		return fmt.Errorf("release tag already exists locally: %s", tag)
	} else {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return fmt.Errorf("inspect local release tag %s: %w", tag, err)
		}
	}
	output, err := hiddenCommandContext(ctx, "git", "ls-remote", "--tags", remote, "refs/tags/"+tag).CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect remote release tag %s: %w: %s", tag, err, strings.TrimSpace(string(output)))
	}
	if strings.TrimSpace(string(output)) != "" {
		return fmt.Errorf("release tag already exists on %s: %s", remote, tag)
	}
	return nil
}

func publishAcceptedReleaseTag(ctx context.Context, tag, commit, remote string) error {
	annotation := acceptedReleaseTagAnnotation(tag, commit)
	output, err := hiddenCommandContext(ctx, "git", "tag", "--annotate", tag, commit, "--message", annotation).CombinedOutput()
	if err != nil {
		return fmt.Errorf("create accepted release tag %s: %w: %s", tag, err, strings.TrimSpace(string(output)))
	}
	ref := "refs/tags/" + tag
	output, err = hiddenCommandContext(ctx, "git", "push", remote, ref+":"+ref).CombinedOutput()
	if err != nil {
		return fmt.Errorf("push accepted release tag %s: %w: %s", tag, err, strings.TrimSpace(string(output)))
	}
	output, err = hiddenCommandContext(ctx, "git", "ls-remote", "--tags", remote, ref, ref+"^{}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify accepted release tag %s: %w: %s", tag, err, strings.TrimSpace(string(output)))
	}
	foundTag := false
	foundCommit := false
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return fmt.Errorf("verify accepted release tag %s: malformed remote response", tag)
		}
		switch fields[1] {
		case ref:
			foundTag = true
		case ref + "^{}":
			foundCommit = fields[0] == commit
		}
	}
	if !foundTag || !foundCommit {
		return fmt.Errorf("accepted release tag %s did not resolve to commit %s on %s", tag, commit, remote)
	}
	return nil
}

func runReleaseGates(
	ctx context.Context,
	tag string,
	stdout, stderr io.Writer,
	resourcePreflight func(context.Context, io.Writer, io.Writer) error,
	verifyIntegration func(context.Context, io.Writer, io.Writer) error,
	requireFrozen func(context.Context) error,
	installedAcceptance func(context.Context, string, io.Writer, io.Writer) error,
) error {
	if err := resourcePreflight(ctx, stdout, stderr); err != nil {
		return fmt.Errorf("release resource preflight: %w", err)
	}
	if err := verifyIntegration(ctx, stdout, stderr); err != nil {
		return fmt.Errorf("release integration verification: %w", err)
	}
	if err := requireFrozen(ctx); err != nil {
		return fmt.Errorf("release source changed after integration verification: %w", err)
	}
	if err := installedAcceptance(ctx, tag, stdout, stderr); err != nil {
		return fmt.Errorf("release installed-candidate acceptance: %w", err)
	}
	return nil
}

func inspectReleaseSource(ctx context.Context, expectedCommit string) (releaseSource, error) {
	commit, err := sourceRevision(ctx)
	if err != nil {
		return releaseSource{}, fmt.Errorf("resolve release source commit: %w", err)
	}
	statusOutput, err := hiddenCommandContext(ctx, "git", "status", "--porcelain=v1", "--untracked-files=all").CombinedOutput()
	if err != nil {
		return releaseSource{}, fmt.Errorf("inspect release source state: %w: %s", err, strings.TrimSpace(string(statusOutput)))
	}
	upstreamOutput, err := hiddenCommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}").CombinedOutput()
	if err != nil {
		return releaseSource{}, fmt.Errorf("resolve configured upstream for release: %w: %s", err, strings.TrimSpace(string(upstreamOutput)))
	}
	upstream := strings.TrimSpace(string(upstreamOutput))
	contained, err := releaseCommitContainedInUpstream(ctx, commit, upstream)
	if err != nil {
		return releaseSource{}, err
	}
	return validateReleaseSource(commit, expectedCommit, string(statusOutput), upstream, contained)
}

func validateReleaseSource(commit, expectedCommit, status, upstream string, contained bool) (releaseSource, error) {
	if expectedCommit != "" && commit != expectedCommit {
		return releaseSource{}, fmt.Errorf("release source commit changed from %s to %s", expectedCommit, commit)
	}
	if strings.TrimSpace(status) != "" {
		return releaseSource{}, errors.New("release requires a clean committed working tree")
	}
	upstream = strings.TrimSpace(upstream)
	if upstream == "" || strings.ContainsAny(upstream, "\r\n\x00") {
		return releaseSource{}, errors.New("release requires one configured branch upstream")
	}
	if !contained {
		return releaseSource{}, fmt.Errorf("release commit %s is not contained in configured upstream %s", commit, upstream)
	}
	return releaseSource{Commit: commit, Upstream: upstream}, nil
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
		return false, fmt.Errorf("check release upstream containment: %w: %s", err, strings.TrimSpace(output.String()))
	}
	return true, nil
}
