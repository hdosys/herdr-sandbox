package sandbox

import (
	"strings"
	"testing"
)

func TestPortablePackageVersionArgumentsDefaultAndZigOverride(t *testing.T) {
	base := readDefaultBaseProvisioning(t)
	for _, required := range []string{
		"[string[]]$PortableVersionArguments = @('--version')",
		"& $commands[0].FullName @VersionArguments",
		"-PortableVersionArguments $PortableVersionArguments",
	} {
		if !strings.Contains(base, required) {
			t.Fatalf("portable package version contract is missing %q", required)
		}
	}
	payloadStart := strings.Index(base, "function Install-ProvisioningPackagePayload")
	cachedStart := strings.Index(base, "function Install-ProvisioningCachedPackage")
	winGetStart := strings.Index(base, "function Install-ProvisioningWinGetPackage")
	if payloadStart < 0 || cachedStart <= payloadStart || winGetStart <= cachedStart {
		t.Fatal("portable package function ordering is unavailable")
	}
	if strings.Contains(base[payloadStart:cachedStart], "$PortableVersionArguments") {
		t.Fatal("payload helper owns the post-install portable version arguments")
	}
	if !strings.Contains(base[cachedStart:winGetStart], "[string[]]$PortableVersionArguments = @('--version')") {
		t.Fatal("cached-package helper does not own the portable version arguments")
	}
	stacks := readDefaultStackProvisioning(t)
	if !strings.Contains(stacks, "-PortableVersionArguments @('version')") {
		t.Fatal("Zig does not select its supported version subcommand")
	}
}
