package sandbox

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCurrentSandboxTailscaleVerificationPreservesService(t *testing.T) {
	if os.Getenv("HERDR_SANDBOX_CURRENT_NATIVE") != "1" {
		t.Skip("requires explicit current-Sandbox acceptance")
	}
	if !strings.EqualFold(os.Getenv("USERNAME"), "WDAGUtilityAccount") {
		t.Fatal("current-Sandbox verification refuses host execution")
	}
	powerShell := mustWindowsPowerShellPath(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	run := func(script string) []byte {
		t.Helper()
		command := hiddenCommandContext(ctx, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePowerShell(script))
		var stderr bytes.Buffer
		command.Stderr = &stderr
		output, err := command.Output()
		if err != nil {
			t.Fatalf("live Tailscale check: %v: %s", err, &stderr)
		}
		return output
	}
	serviceIdentity := `$ErrorActionPreference = 'Stop'
$service = Get-CimInstance Win32_Service -Filter "Name = 'Tailscale'"
$process = Get-Process -Id $service.ProcessId -ErrorAction Stop
[Console]::Out.Write(([string]$process.Id + ':' + $process.StartTime.ToUniversalTime().ToString('O')))`
	before := run(serviceIdentity)
	output := run(buildTailscaleVerifyLauncher())
	identity, err := decodeLiveTailscaleIdentity(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(identity.State) != 0 {
		t.Fatal("live verification captured private state")
	}
	if err := verifyStableTailscaleIdentity(identity, identity); err != nil {
		t.Fatal(err)
	}
	changed := identity
	changed.NodeID += "-changed"
	if err := verifyStableTailscaleIdentity(identity, changed); err == nil {
		t.Fatal("changed live identity was accepted")
	}
	if _, err := decodeLiveTailscaleIdentity(bytes.Replace(output, []byte(`"schemaVersion":`), []byte(`"state":"unexpected","schemaVersion":`), 1)); err == nil {
		t.Fatal("live verification accepted private state in its public result")
	}
	if after := run(serviceIdentity); !bytes.Equal(before, after) {
		t.Fatal("Tailscale service identity changed during verification")
	}
}
