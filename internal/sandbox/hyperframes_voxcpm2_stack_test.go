package sandbox

import (
	"crypto/sha256"
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
	modelPayloads := map[string][]byte{
		"VoxCPM2-BaseLM-F16.gguf":   []byte("base-model"),
		"VoxCPM2-Acoustic-F16.gguf": []byte("audio-model"),
		"reference_speaker.wav":     []byte("reference-12"),
	}
	artifact := func(name, host string) voxcpm2Artifact {
		payload := modelPayloads[name]
		digest := sha256.Sum256(payload)
		return voxcpm2Artifact{
			Name: name, Size: int64(len(payload)), SHA256: fmt.Sprintf("%x", digest),
			URL: "https://" + host + "/example/" + name,
		}
	}
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
				artifact("VoxCPM2-BaseLM-F16.gguf", "huggingface.co"),
				artifact("VoxCPM2-Acoustic-F16.gguf", "huggingface.co"),
			},
		},
		ReferenceAudio: artifact("reference_speaker.wav", "raw.githubusercontent.com"),
	}
	payload, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(releaseRoot, voxcpm2CurrentDescriptorName), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	completionPayload, err := json.Marshal(voxcpm2ModelCompletion{SchemaVersion: 1, Models: descriptor.Models, ReferenceAudio: descriptor.ReferenceAudio})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelRoot, voxcpm2ModelCompletionName), completionPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, modelPayload := range modelPayloads {
		if err := os.WriteFile(filepath.Join(modelRoot, name), modelPayload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	stackPath := defaultProvisioningPath(t, stackProvisioningName)
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$tokens = $null
$errors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile('%s', [ref]$tokens, [ref]$errors)
if ($errors.Count -ne 0) { throw $errors[0].Message }
foreach ($name in @('Test-StackHyperFramesVoxCPM2ArchiveEntry', 'Assert-StackHyperFramesVoxCPM2Artifact', 'Get-StackHyperFramesVoxCPM2Descriptor', 'Assert-StackHyperFramesVoxCPM2Models')) {
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
Assert-StackHyperFramesVoxCPM2Models -ModelRoot '%s' -Descriptor $descriptor
$corruptPath = Join-Path '%s' 'VoxCPM2-BaseLM-F16.gguf'
$corrupt = [IO.File]::ReadAllBytes($corruptPath)
$corrupt[0] = $corrupt[0] -bxor 1
[IO.File]::WriteAllBytes($corruptPath, $corrupt)
$checksumRejected = $false
try { Assert-StackHyperFramesVoxCPM2Models -ModelRoot '%s' -Descriptor $descriptor } catch { $checksumRejected = $true }
if (-not $checksumRejected) { throw 'HyperFrames VoxCPM2 accepted a same-size modified model.' }
`, quote(stackPath), quote(modelRoot), quote(modelRoot), quote(modelRoot), quote(modelRoot))
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
