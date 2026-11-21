package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"herdr-sandbox/internal/sandbox"
)

func TestRemoteSetupRequiresApprovalAndHonorsNoStart(t *testing.T) {
	for _, choice := range []string{"", "n\n", "y\n"} {
		deps := defaultCommandDependencies()
		approved := false
		deps.setupRemote = func(_ context.Context, keyFile string, confirm func(sandbox.RemoteSetup) bool) (bool, error) {
			if keyFile != "device.pub" {
				t.Fatalf("wrong import path %q", keyFile)
			}
			approved = confirm(sandbox.RemoteSetup{AdditionalKeys: 1, Sources: []string{"device.pub"}})
			return approved, nil
		}
		deps.resolveHerdr = func(context.Context) (sandbox.HostHerdr, error) {
			t.Fatal("no-start touched runtime")
			return sandbox.HostHerdr{}, nil
		}
		var stdout, stderr bytes.Buffer
		code := runWithCommandDependencies(t.Context(), []string{"remote", "setup", "--key", "device.pub", "--no-start"}, strings.NewReader(choice), &stdout, &stderr, deps)
		if code != 0 || approved != (choice == "y\n") {
			t.Fatalf("choice=%q approval=%t code=%d error=%s", choice, approved, code, &stderr)
		}
		if !approved && !strings.Contains(stdout.String(), "Cancelled") {
			t.Fatal("missing cancellation feedback")
		}
	}
}
