package sandbox

import "testing"

func TestPlaywrightCLIStackPlanningUsesLatestPackageOwner(t *testing.T) {
	if !stackPlaywrightCLI.valid() {
		t.Fatal("Playwright CLI stack is not a valid inspected stack")
	}
	if got := effectiveStackPackageOwner(stackPlaywrightCLI); got != "OpenJS.NodeJS.LTS + @playwright/cli@latest" {
		t.Fatalf("Playwright CLI package owner = %q", got)
	}
}
