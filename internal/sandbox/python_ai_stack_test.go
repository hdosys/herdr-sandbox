package sandbox

import (
	"strings"
	"testing"
)

func TestPythonAIStackReusesPythonAndOwnsUvWithoutFrameworks(t *testing.T) {
	text := readDefaultStackProvisioning(t)
	uvStart := strings.Index(text, "function Install-Uv")
	aiStart := strings.Index(text, "function Install-PythonAIStack")
	zigStart := strings.Index(text, "function Install-ZigStack")
	if uvStart < 0 || aiStart <= uvStart || zigStart <= aiStart {
		t.Fatalf("Python AI stack function ordering is invalid: uv=%d ai=%d zig=%d", uvStart, aiStart, zigStart)
	}
	uvSection := text[uvStart:aiStart]
	for _, required := range []string{
		"-Id 'astral-sh.uv'",
		"-InstallerType 'zip' -Adapter 'Portable' -ExecutableName 'uv.exe'",
		"C:\\HerdrSandbox\\cache\\uv",
		"Assert-ProvisioningCacheTree -Path $uvCacheRoot",
		"UV_CACHE_DIR",
		"UV_NO_MANAGED_PYTHON",
		"@('cache', 'dir')",
		"uv ready:",
	} {
		if !strings.Contains(uvSection, required) {
			t.Fatalf("uv stack is missing %q", required)
		}
	}
	for _, forbidden := range []string{"Invoke-WebRequest", "pip install", "cargo install", "--force"} {
		if strings.Contains(strings.ToLower(uvSection), strings.ToLower(forbidden)) {
			t.Fatalf("uv stack contains alternate installer or bypass %q", forbidden)
		}
	}

	aiSection := text[aiStart:zigStart]
	python := strings.Index(aiSection, "Install-PythonStack -Series '3.13'")
	uv := strings.Index(aiSection, "Install-Uv")
	ready := strings.Index(aiSection, "Python AI development toolchain ready.")
	if python < 0 || uv <= python || ready <= uv {
		t.Fatalf("Python AI composition is incomplete or unordered: %s", aiSection)
	}
	for _, forbidden := range []string{"torch", "jupyter", "transformers", "cuda", "conda", "huggingface"} {
		if strings.Contains(strings.ToLower(aiSection), forbidden) {
			t.Fatalf("Python AI stack bundles project or GPU dependency %q", forbidden)
		}
	}
	if effectiveStackPackageOwner(stackUV) != packageUV || !projectStackOwnsPackage(packageUV) {
		t.Fatal("uv is not owned by the project-stack package path")
	}
}
