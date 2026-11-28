package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"herdr-sandbox/internal/sandbox"
)

func TestRemotePastedKeyRequiresApprovalAndAppliesExactSavedSnapshot(t *testing.T) {
	for _, choice := range []string{"", "n\n", "y\n"} {
		deps := defaultCommandDependencies()
		deps.remoteSettings = func(context.Context) (sandbox.RemoteSettings, error) { return sandbox.RemoteSettings{}, nil }
		approved, applied := false, false
		deps.changeRemote = func(_ context.Context, change sandbox.RemoteChange, confirm func(sandbox.RemoteSettings) bool) (sandbox.RemoteSettings, bool, error) {
			if change.Action != "add" || change.Name != "Laptop" || change.PublicKey != "public-key-input" {
				t.Fatalf("wrong change: %#v", change)
			}
			result := sandbox.RemoteSettings{AuthorizedKeys: []string{"approved-device-key"}}
			approved = confirm(result)
			return result, approved, nil
		}
		deps.applyRemoteKeys = func(_ context.Context, keys []string) (bool, error) {
			if !approved || len(keys) != 1 || keys[0] != "approved-device-key" {
				t.Fatal("application lost the approved snapshot")
			}
			applied = true
			return true, nil
		}
		deps.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) {
			t.Fatal("device editing entered provisioning")
			return sandbox.HostHerdr{}, nil
		}
		var out, stderr bytes.Buffer
		code := runWithCommandDependencies(t.Context(), []string{"remote", "add", "--name", "Laptop"}, strings.NewReader("public-key-input\n"+choice), &out, &stderr, deps)
		if code != 0 || approved != (choice == "y\n") || applied != approved {
			t.Fatalf("code=%d approved=%t applied=%t err=%s", code, approved, applied, &stderr)
		}
	}
}

func TestRemoteChangeReportsPendingOrAppliedWithoutStartingGuest(t *testing.T) {
	for _, outcome := range []string{"no-guest", "applied", "failed"} {
		deps := defaultCommandDependencies()
		deps.remoteSettings = func(context.Context) (sandbox.RemoteSettings, error) { return sandbox.RemoteSettings{}, nil }
		deps.changeRemote = func(_ context.Context, _ sandbox.RemoteChange, confirm func(sandbox.RemoteSettings) bool) (sandbox.RemoteSettings, bool, error) {
			r := sandbox.RemoteSettings{}
			return r, confirm(r), nil
		}
		deps.applyRemoteKeys = func(context.Context, []string) (bool, error) {
			if outcome == "failed" {
				return false, errors.New("endpoint identity changed")
			}
			return outcome == "applied", nil
		}
		deps.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) {
			t.Fatal("remote command started provisioning")
			return sandbox.HostHerdr{}, nil
		}
		var out, stderr bytes.Buffer
		code := runWithCommandDependencies(t.Context(), []string{"remote", "enable", "--yes"}, strings.NewReader(""), &out, &stderr, deps)
		if outcome == "failed" {
			if code != 1 || !strings.Contains(stderr.String(), "live application was NOT confirmed") || strings.Contains(out.String(), "Applied and verified") {
				t.Fatalf("false success: %d %s %s", code, &out, &stderr)
			}
		} else if code != 0 || (outcome == "no-guest" && !strings.Contains(out.String(), "next sandbox up")) {
			t.Fatalf("outcome=%s code=%d %s %s", outcome, code, &out, &stderr)
		}
	}
}

func TestRemoteMenuSelectsStableFingerprintAndWarnsOnRemoval(t *testing.T) {
	deps := defaultCommandDependencies()
	deps.interactive = func(io.Reader) bool { return true }
	deps.remoteSettings = func(context.Context) (sandbox.RemoteSettings, error) {
		return sandbox.RemoteSettings{Enabled: true, Devices: []sandbox.RemoteDevice{{Name: "Laptop", Fingerprint: "SHA256:device"}}}, nil
	}
	deps.changeRemote = func(_ context.Context, change sandbox.RemoteChange, confirm func(sandbox.RemoteSettings) bool) (sandbox.RemoteSettings, bool, error) {
		if change.Action != "remove" || change.Device != "SHA256:device" {
			t.Fatalf("unstable device target: %#v", change)
		}
		result := sandbox.RemoteSettings{}
		return result, confirm(result), nil
	}
	deps.applyRemoteKeys = func(context.Context, []string) (bool, error) { return true, nil }
	var out, stderr bytes.Buffer
	code := runWithCommandDependencies(t.Context(), []string{"remote"}, strings.NewReader("4\n1\ny\n"), &out, &stderr, deps)
	if code != 0 || !strings.Contains(out.String(), "Existing SSH sessions are NOT disconnected") {
		t.Fatalf("code=%d %s %s", code, &out, &stderr)
	}
}
