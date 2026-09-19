package sandbox

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const voxcpm2PublicLatestTestEnvironment = "HERDR_SANDBOX_VOXCPM2_PUBLIC_LATEST_MODELS"

func TestPrepareHyperFramesVoxCPM2AgainstPublicLatest(t *testing.T) {
	modelRoot := strings.TrimSpace(os.Getenv(voxcpm2PublicLatestTestEnvironment))
	if modelRoot == "" {
		t.Skip("explicit public HyperFrames VoxCPM2 latest-release gate")
	}
	localBundle := os.Getenv("HERDR_SANDBOX_TTS_BUNDLE")
	if err := prepareHyperFramesVoxCPM2(t.Context(), modelRoot, localBundle, os.Stdout); err != nil {
		t.Fatalf("prepare public latest HyperFrames VoxCPM2 release: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(modelRoot, ".herdr-sandbox", voxcpm2CacheDirectoryName, voxcpm2CurrentDescriptorName))
	if err != nil {
		t.Fatal(err)
	}
	var descriptor voxcpm2ReleaseDescriptor
	if err := decodeStrictJSON(data, &descriptor); err != nil {
		t.Fatal(err)
	}
	if !voxcpm2ReleaseTagPattern.MatchString(descriptor.Tag) && !(localBundle != "" && ttsLocalTagPattern.MatchString(descriptor.Tag)) {
		t.Fatalf("public latest release tag = %q", descriptor.Tag)
	}
}

func TestReadVoxCPM2ModelCompletionRejectsOversizedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), voxcpm2ModelCompletionName)
	if err := os.WriteFile(path, make([]byte, maximumVoxCPM2MetadataBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readVoxCPM2ModelCompletion(path); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("oversized completion error = %v", err)
	}
}

func TestSecureVoxCPM2CurlArgumentsRestrictTransport(t *testing.T) {
	arguments := secureVoxCPM2CurlArguments("https://example.com/file", 2*time.Minute, "Accept: application/json")
	if len(arguments) == 0 || arguments[0] != "--disable" || arguments[len(arguments)-1] != "https://example.com/file" {
		t.Fatalf("secure curl arguments have unsafe boundaries: %q", arguments)
	}
	for _, required := range []string{"--fail", "--location", "--proto", "=https", "--proto-redir", "--tlsv1.2", "--max-time", "120"} {
		if !slices.Contains(arguments, required) {
			t.Fatalf("secure curl arguments are missing %q: %q", required, arguments)
		}
	}
	for _, forbidden := range []string{"--insecure", "-k", "--ssl-no-revoke"} {
		if slices.Contains(arguments, forbidden) {
			t.Fatalf("secure curl arguments contain %q: %q", forbidden, arguments)
		}
	}
}

func TestSecureVoxCPM2CurlEnvironmentDropsTrustOverrides(t *testing.T) {
	got := secureVoxCPM2CurlEnvironment([]string{
		"Path=C:\\Windows\\System32",
		"CURL_CA_BUNDLE=C:\\untrusted.pem",
		"curl_ssl_backend=openssl",
		"SSL_CERT_DIR=C:\\certs",
		"ssl_cert_file=C:\\untrusted.pem",
		"SSLKEYLOGFILE=C:\\tls.keys",
		"HTTPS_PROXY=https://proxy.example",
	})
	want := []string{"Path=C:\\Windows\\System32", "HTTPS_PROXY=https://proxy.example"}
	if !slices.Equal(got, want) {
		t.Fatalf("secure curl environment = %q, want %q", got, want)
	}
}

func TestValidateVoxCPM2GitHubReleaseRequiresExactAssets(t *testing.T) {
	tag := "v1.2.3"
	archiveName := "hyperframes-voxcpm2-" + tag + "-windows-x64.zip"
	release := voxcpm2GitHubRelease{
		TagName: tag,
		Assets: []voxcpm2GitHubAsset{
			{Name: archiveName, Size: 100, Digest: "sha256:" + strings.Repeat("a", 64), BrowserDownloadURL: "https://github.com/hdosys/hyperframes-voxcpm2/releases/download/" + tag + "/" + archiveName},
			{Name: archiveName + ".sha256", Size: 109, Digest: "sha256:" + strings.Repeat("b", 64), BrowserDownloadURL: "https://github.com/hdosys/hyperframes-voxcpm2/releases/download/" + tag + "/" + archiveName + ".sha256"},
		},
	}
	if _, _, err := validateVoxCPM2GitHubRelease(release); err != nil {
		t.Fatalf("validate exact release: %v", err)
	}
	release.Assets[0].BrowserDownloadURL = "https://example.com/" + archiveName
	if _, _, err := validateVoxCPM2GitHubRelease(release); err == nil {
		t.Fatal("release accepted an archive from an unexpected host")
	}
}

func TestParseVoxCPM2SidecarBindsArchiveName(t *testing.T) {
	archive := "hyperframes-voxcpm2-v1.2.3-windows-x64.zip"
	digest := strings.Repeat("a", 64)
	if got, err := parseVoxCPM2Sidecar([]byte(digest+"  "+archive+"\n"), archive); err != nil || got != digest {
		t.Fatalf("parse sidecar = %q, %v", got, err)
	}
	if _, err := parseVoxCPM2Sidecar([]byte(digest+"  other.zip\n"), archive); err == nil {
		t.Fatal("sidecar accepted a different archive name")
	}
}

func TestInspectVoxCPM2CoreArchiveVerifiesPayload(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "release.zip")
	files := map[string][]byte{
		"bin/download-supertonic.py":                    []byte("download"),
		"versions.json":                                 []byte("versions"),
		"requirements.txt":                              []byte("requirements"),
		"engine/audio/scripts/versions.json":            []byte("versions"),
		"engine/audio/scripts/lib/supertonic.mjs":       []byte("provider"),
		"engine/audio/scripts/lib/supertonic-runner.py": []byte("runner"),
		"THIRD_PARTY_NOTICES.md":                        []byte("notices"),
		"bin/tts.ps1":                                   []byte("wrapper"),
		"reference/herdr-narrator-de.wav":               []byte("narrator"),
		"engine/audio/scripts/audio.mjs":                []byte("audio"),
		"engine/audio/scripts/lib/tts.mjs":              []byte("tts"),
		"engine/audio/scripts/lib/voxcpm2-cli.mjs":      []byte("cli"),
		"engine/audio/scripts/lib/voxcpm2.mjs":          []byte("provider"),
		"runtime/cpu/llama-tts-server.exe":              []byte("cpu"),
	}
	manifest := voxcpm2CoreManifest{
		SchemaVersion:  1,
		ReleaseVersion: "1.2.3",
		Platform:       "windows-x64",
		Models: voxcpm2ModelSet{
			Repository: "DennisHuang648/VoxCPM2-GGUF",
			Revision:   strings.Repeat("1", 40),
			Files: []voxcpm2Artifact{
				{Name: "VoxCPM2-BaseLM-F16.gguf", Size: 10, SHA256: strings.Repeat("a", 64), URL: "https://huggingface.co/example/resolve/" + strings.Repeat("1", 40) + "/VoxCPM2-BaseLM-F16.gguf"},
				{Name: "VoxCPM2-Acoustic-F16.gguf", Size: 11, SHA256: strings.Repeat("b", 64), URL: "https://huggingface.co/example/resolve/" + strings.Repeat("1", 40) + "/VoxCPM2-Acoustic-F16.gguf"},
			},
		},
		ReferenceAudio: voxcpm2Artifact{Name: "reference_speaker.wav", Size: 12, SHA256: strings.Repeat("c", 64), URL: "https://raw.githubusercontent.com/example/repository/" + strings.Repeat("2", 40) + "/reference_speaker.wav"},
	}
	manifest.HyperFrames.Version = "0.8.6"
	manifest.Supertonic.Repository = "supertone-oss-archive/supertonic-3"
	manifest.Supertonic.Revision = strings.Repeat("4", 40)
	manifest.Runtime.Commit = strings.Repeat("3", 40)
	for name, payload := range files {
		digest := sha256.Sum256(payload)
		manifest.Files = append(manifest.Files, voxcpm2ManifestFile{Path: name, Size: int64(len(payload)), SHA256: hex.EncodeToString(digest[:])})
	}
	manifestPayload, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(output)
	for name, payload := range files {
		entry, createErr := writer.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write(payload); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	entry, err := writer.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(manifestPayload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectVoxCPM2CoreArchive(archive, "v1.2.3"); err != nil {
		t.Fatalf("inspect exact archive: %v", err)
	}
	qwenManifest := manifest
	qwenManifest.Files = slices.Clone(manifest.Files)
	qwenManifest.Qwen3 = &voxcpm2ModelSet{
		Repository: "khimaros/Qwen3-TTS-12Hz-0.6B-CustomVoice-GGUF", Revision: strings.Repeat("5", 40),
	}
	for _, name := range []string{"Qwen3-TTS-12Hz-0.6B-CustomVoice-Q8_0.gguf", "Qwen3-TTS-Tokenizer-12Hz-F16.gguf"} {
		qwenManifest.Qwen3.Files = append(qwenManifest.Qwen3.Files, voxcpm2Artifact{
			Name: name, Size: 100, SHA256: strings.Repeat("6", 64), URL: "https://huggingface.co/example/" + name,
		})
	}
	qwenManifest.Files = append(qwenManifest.Files,
		voxcpm2ManifestFile{Path: "engine/audio/scripts/lib/qwen3.mjs"},
		voxcpm2ManifestFile{Path: "runtime/qwen3/qwen3-tts-cli.exe"})
	if err := validateVoxCPM2CoreManifest(qwenManifest, "v1.2.3"); err != nil {
		t.Fatalf("optional Qwen3 payload rejected: %v", err)
	}
	qwenManifest.Qwen3 = nil
	if err := validateVoxCPM2CoreManifest(qwenManifest, "v1.2.3"); err == nil {
		t.Fatal("Qwen3 executable accepted without its model contract")
	}
	manifest.Files = append(manifest.Files, voxcpm2ManifestFile{
		Path: "runtime/vulkan/llama-tts-server.exe", Size: 1, SHA256: strings.Repeat("d", 64),
	})
	if err := validateVoxCPM2CoreManifest(manifest, "v1.2.3"); err == nil || !strings.Contains(err.Error(), "non-CPU runtime") {
		t.Fatalf("Vulkan runtime error = %v", err)
	}
}
