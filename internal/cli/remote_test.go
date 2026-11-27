package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"herdr-sandbox/internal/sandbox"
)

func TestRemoteSetupAppliesApprovedKeysWithoutProvisioning(t *testing.T) {
	for _, choice := range []string{"", "n\n", "y\n"} {
		deps := defaultCommandDependencies()
		approved := false
		deps.setupRemote = func(_ context.Context, keyFile string, confirm func(sandbox.RemoteSetup) bool) (bool, error) {
			if keyFile != "device.pub" {
				t.Fatalf("wrong import path %q", keyFile)
			}
			approved = confirm(sandbox.RemoteSetup{AdditionalKeys: 1, Sources: []string{"device.pub"}, AuthorizedKeys: []string{"approved-device-key"}})
			return approved, nil
		}
		applied := false
		deps.applyRemoteKeys = func(_ context.Context, keys []string) (bool, error) {
			if !approved {
				t.Fatal("unapproved access applied")
			}
			if len(keys) != 1 || keys[0] != "approved-device-key" {
				t.Fatal("application lost the exact approved key snapshot")
			}
			applied = true
			return true, nil
		}
		deps.inspect = func(context.Context) (sandbox.SessionStatus, error) {
			return sandbox.SessionStatus{State: sandbox.SessionReady, RemoteAccess: &sandbox.RemoteAccess{
				DNSName: "herdr-sandbox.example.ts.net", SSHUser: "WDAGUtilityAccount", Port: 22,
			}}, nil
		}
		deps.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) {
			t.Fatal("key update must not enter provisioning")
			return sandbox.HostHerdr{}, nil
		}
		var stdout, stderr bytes.Buffer
		code := runWithCommandDependencies(t.Context(), []string{"remote", "setup", "--key", "device.pub"}, strings.NewReader(choice), &stdout, &stderr, deps)
		if code != 0 || approved != (choice == "y\n") || applied != approved {
			t.Fatalf("choice=%q approval=%t code=%d error=%s", choice, approved, code, &stderr)
		}
		if !approved && !strings.Contains(stdout.String(), "Cancelled") {
			t.Fatal("missing cancellation feedback")
		}
	}
}

func TestRemoteSetupReportsLiveFailureWithoutStartingSandbox(t *testing.T) {
	deps := defaultCommandDependencies()
	deps.setupRemote = func(_ context.Context, _ string, confirm func(sandbox.RemoteSetup) bool) (bool, error) {
		return confirm(sandbox.RemoteSetup{}), nil
	}
	deps.applyRemoteKeys = func(context.Context, []string) (bool, error) {
		return false, errors.New("endpoint identity changed")
	}
	deps.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) {
		t.Fatal("failed live update must not fall back to provisioning")
		return sandbox.HostHerdr{}, nil
	}
	var stdout, stderr bytes.Buffer
	code := runWithCommandDependencies(t.Context(), []string{"remote", "setup", "--yes"}, strings.NewReader(""), &stdout, &stderr, deps)
	if code != 1 || !strings.Contains(stderr.String(), "access was not confirmed") || strings.Contains(stdout.String(), "Remote access verified") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
}
