package sandbox

import "testing"

func TestProjectStackPackageOwnershipReservesNumericFamilies(t *testing.T) {
	for _, id := range []string{
		"Microsoft.DotNet.SDK.8",
		"microsoft.dotnet.sdk.10",
		"Microsoft.OpenJDK.25",
		"Python.Python.3.14",
	} {
		if !projectStackOwnsPackage(id) {
			t.Fatalf("numeric package family is not stack-owned: %s", id)
		}
	}
	for _, id := range []string{
		"Microsoft.DotNet.SDK.Preview",
		"Microsoft.OpenJDK.25.Preview",
		"Python.Python.3.15.Preview",
		"Python.Python.3",
	} {
		if projectStackOwnsPackage(id) {
			t.Fatalf("nonnumeric package unexpectedly became a stack path: %s", id)
		}
	}
}
