package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTradingViewStackOwnsExactDesktopAndGuestLocalTVControl(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "provisioning", stackProvisioningName))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	start := strings.Index(source, "function Install-TradingViewStack")
	end := strings.Index(source, "function Resolve-StackPythonPackage")
	if start < 0 || end <= start {
		t.Fatal("TradingView stack function boundary is missing")
	}
	block := source[start:end]
	for _, required := range []string{
		"$minimumWindowsBuild = 19042",
		"TradingView.TradingViewDesktop",
		"-InstallerType 'msix'",
		"-Adapter 'MSIX'",
		"-RequireAuthenticodeSignature",
		"Get-AppxPackage -Name 'TradingView.Desktop'",
		"Install-NodeRuntime -Version $NodeVersion",
		"@ferroxlabs/tvcontrol@latest",
		"@ferroxlabs/tvcontrol@$TVControlVersion",
		"$toolRoot = $tvControlRoot",
		"'--ignore-scripts'",
		"'--omit=optional'",
		"'tv.cmd'",
		"'tvcontrol.cmd'",
		"'tv.ps1'",
		"'tvcontrol.ps1'",
		"Usage: tv <command> [options]",
		"TradingView remains stopped with CDP disabled",
	} {
		if !strings.Contains(block, required) {
			t.Fatalf("TradingView stack is missing %q", required)
		}
	}
	if strings.Index(block, "$minimumWindowsBuild = 19042") > strings.Index(block, "Install-NodeRuntime") {
		t.Fatal("TradingView minimum Windows build check occurs after Node mutation")
	}
	for _, forbidden := range []string{
		"Start-Process", "taskkill", "--remote-debugging-port", "launch_tv_debug.bat",
		"Install-NodeStack", "npm ci", "npm link", "ProjectDirectory",
		"Join-Path $tvControlRoot $TVControlVersion",
	} {
		if strings.Contains(block, forbidden) {
			t.Fatalf("TradingView stack contains forbidden runtime/project path %q", forbidden)
		}
	}
}
