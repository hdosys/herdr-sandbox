package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHyperFramesVoxCPM2MetadataHelpersRunInWindowsPowerShell51(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 HyperFrames VoxCPM2 metadata contract")
	}
	modelRoot := t.TempDir()
	releaseRoot := filepath.Join(modelRoot, ".herdr-sandbox", voxcpm2CacheDirectoryName)
	if err := os.MkdirAll(releaseRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	descriptor := voxcpm2ReleaseDescriptor{
		SchemaVersion:      1,
		Tag:                "v1.2.3",
		ArchiveName:        "hyperframes-voxcpm2-v1.2.3-windows-x64.zip",
		ArchiveSize:        100,
		ArchiveSHA256:      strings.Repeat("a", 64),
		HyperFramesVersion: "0.8.6",
		RuntimeCommit:      strings.Repeat("b", 40),
		Models: voxcpm2ModelSet{
			Repository: "DennisHuang648/VoxCPM2-GGUF",
			Revision:   strings.Repeat("c", 40),
			Files: []voxcpm2Artifact{
				{Name: "VoxCPM2-BaseLM-F16.gguf", Size: 10, SHA256: strings.Repeat("d", 64), URL: "https://huggingface.co/example/VoxCPM2-BaseLM-F16.gguf"},
				{Name: "VoxCPM2-Acoustic-F16.gguf", Size: 11, SHA256: strings.Repeat("e", 64), URL: "https://huggingface.co/example/VoxCPM2-Acoustic-F16.gguf"},
			},
		},
		ReferenceAudio: voxcpm2Artifact{Name: "reference_speaker.wav", Size: 12, SHA256: strings.Repeat("f", 64), URL: "https://raw.githubusercontent.com/example/reference_speaker.wav"},
	}
	payload, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(releaseRoot, voxcpm2CurrentDescriptorName), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	stackPath := defaultProvisioningPath(t, stackProvisioningName)
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('Test-StackHyperFramesVoxCPM2ArchiveEntry', 'Assert-StackHyperFramesVoxCPM2Artifact', 'Get-StackHyperFramesVoxCPM2Descriptor')) {
    $definition = $ast.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -ceq $name }, $true)
    if ($null -eq $definition) { throw "Missing HyperFrames VoxCPM2 helper: $name" }
    Invoke-Expression $definition.Extent.Text
}
Test-StackHyperFramesVoxCPM2ArchiveEntry -Entry 'runtime/cpu/llama-tts-server.exe'
$vulkanRejected = $false
try { Test-StackHyperFramesVoxCPM2ArchiveEntry -Entry 'runtime/vulkan/llama-tts-server.exe' } catch { $vulkanRejected = $true }
if (-not $vulkanRejected) { throw 'HyperFrames VoxCPM2 accepted a Vulkan runtime.' }
$rejected = $false
try { Test-StackHyperFramesVoxCPM2ArchiveEntry -Entry 'runtime/../escape.exe' } catch { $rejected = $true }
if (-not $rejected) { throw 'Unsafe HyperFrames VoxCPM2 archive entry was accepted.' }
$descriptor = Get-StackHyperFramesVoxCPM2Descriptor -ModelRoot '%s'
if ([string]$descriptor.tag -cne 'v1.2.3') { throw 'HyperFrames VoxCPM2 descriptor identity changed.' }
`, quote(stackPath), quote(modelRoot))
	scriptPath := filepath.Join(t.TempDir(), "hyperframes-voxcpm2-metadata.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	command := hiddenCommand(mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	command.Env = append(os.Environ(), "PSModulePath="+os.Getenv("PSModulePath"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("HyperFrames VoxCPM2 PowerShell metadata contract: %v: %s", err, output)
	}
}
