package sandbox

import "testing"

func TestProjectStackPackageOwnershipReservesModernDotNetSDK(t *testing.T) {
	for _, id := range []string{"Microsoft.DotNet.SDK.10", "microsoft.dotnet.sdk.10"} {
		if !projectStackOwnsPackage(id) {
			t.Fatalf("modern .NET package is not stack-owned: %s", id)
		}
	}
	for _, id := range []string{"Microsoft.DotNet.SDK.8", "Microsoft.DotNet.SDK.9", "Microsoft.DotNet.SDK.Preview"} {
		if projectStackOwnsPackage(id) {
			t.Fatalf("legacy or preview .NET package unexpectedly became a stack path: %s", id)
		}
	}
}
