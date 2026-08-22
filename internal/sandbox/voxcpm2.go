package sandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	voxcpm2LatestReleaseURL      = "https://api.github.com/repos/hdosys/hyperframes-voxcpm2/releases/latest"
	voxcpm2CacheDirectoryName    = "hyperframes-voxcpm2"
	voxcpm2CurrentDescriptorName = "current.json"
	voxcpm2ModelCompletionName   = "herdr-sandbox-voxcpm2.json"
	maximumVoxCPM2MetadataBytes  = 1024 * 1024
	maximumVoxCPM2ArchiveBytes   = 256 * 1024 * 1024
	maximumVoxCPM2ArchiveEntries = 4096
	maximumVoxCPM2ExtractedBytes = 1024 * 1024 * 1024
	maximumVoxCPM2CurlErrorBytes = 4096
	voxcpm2MetadataTimeout       = 2 * time.Minute
	voxcpm2DownloadTimeout       = 2 * time.Hour
)

var (
	voxcpm2ReleaseTagPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	voxcpm2SHA256Pattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	voxcpm2RevisionPattern   = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type voxcpm2GitHubRelease struct {
	TagName    string               `json:"tag_name"`
	Draft      bool                 `json:"draft"`
	Prerelease bool                 `json:"prerelease"`
	Assets     []voxcpm2GitHubAsset `json:"assets"`
}

type voxcpm2GitHubAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type voxcpm2Artifact struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	URL    string `json:"url"`
}

type voxcpm2ModelSet struct {
	Repository string            `json:"repository"`
	Revision   string            `json:"revision"`
	Files      []voxcpm2Artifact `json:"files"`
}

type voxcpm2ManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type voxcpm2CoreManifest struct {
	SchemaVersion  int    `json:"schemaVersion"`
	ReleaseVersion string `json:"releaseVersion"`
	Platform       string `json:"platform"`
	HyperFrames    struct {
		Version string `json:"version"`
	} `json:"hyperframes"`
	Runtime struct {
		Commit string `json:"commit"`
	} `json:"runtime"`
	Models         voxcpm2ModelSet       `json:"models"`
	ReferenceAudio voxcpm2Artifact       `json:"referenceAudio"`
	Files          []voxcpm2ManifestFile `json:"files"`
}

type voxcpm2ReleaseDescriptor struct {
	SchemaVersion      int             `json:"schemaVersion"`
	Tag                string          `json:"tag"`
	ArchiveName        string          `json:"archiveName"`
	ArchiveSize        int64           `json:"archiveSize"`
	ArchiveSHA256      string          `json:"archiveSha256"`
	HyperFramesVersion string          `json:"hyperframesVersion"`
	RuntimeCommit      string          `json:"runtimeCommit"`
	Models             voxcpm2ModelSet `json:"models"`
	ReferenceAudio     voxcpm2Artifact `json:"referenceAudio"`
}

type voxcpm2ModelCompletion struct {
	SchemaVersion  int             `json:"schemaVersion"`
	Models         voxcpm2ModelSet `json:"models"`
	ReferenceAudio voxcpm2Artifact `json:"referenceAudio"`
}

func prepareHyperFramesVoxCPM2(ctx context.Context, modelDirectory string, output io.Writer) error {
	if !filepath.IsAbs(modelDirectory) {
		return errors.New("shared models directory must be absolute")
	}
	modelDirectory, err := canonicalMappedDirectory(modelDirectory)
	if err != nil {
		return fmt.Errorf("validate shared models directory: %w", err)
	}
	releaseRoot, err := ensurePhysicalDirectory(filepath.Join(modelDirectory, ".herdr-sandbox", voxcpm2CacheDirectoryName), "HyperFrames VoxCPM2 release store")
	if err != nil {
		return err
	}
	curl, err := windowsCurlExecutable()
	if err != nil {
		return err
	}
	descriptor, err := prepareVoxCPM2CoreRelease(ctx, curl, releaseRoot, output)
	if err != nil {
		return err
	}
	if err := prepareVoxCPM2Models(ctx, curl, modelDirectory, descriptor, output); err != nil {
		return err
	}
	if err := writeVoxCPM2JSONAtomically(filepath.Join(releaseRoot, voxcpm2CurrentDescriptorName), descriptor); err != nil {
		return fmt.Errorf("publish HyperFrames VoxCPM2 current release descriptor: %w", err)
	}
	return nil
}

func windowsCurlExecutable() (string, error) {
	windowsDirectory := strings.TrimSpace(os.Getenv("SystemRoot"))
	if windowsDirectory == "" || !filepath.IsAbs(windowsDirectory) {
		return "", errors.New("SystemRoot does not identify the Windows directory")
	}
	curl := filepath.Join(windowsDirectory, "System32", "curl.exe")
	exists, err := regularFileExists(curl)
	if err != nil {
		return "", fmt.Errorf("inspect Windows curl.exe: %w", err)
	}
	if !exists {
		return "", fmt.Errorf("required Windows curl.exe is unavailable: %s", curl)
	}
	return curl, nil
}

func secureVoxCPM2CurlArguments(uri string, timeout time.Duration, headers ...string) []string {
	seconds := int64(timeout / time.Second)
	arguments := []string{
		"--disable",
		"--fail",
		"--silent",
		"--show-error",
		"--location",
		"--proto", "=https",
		"--proto-redir", "=https",
		"--tlsv1.2",
		"--max-redirs", "5",
		"--connect-timeout", "30",
		"--speed-limit", "1024",
		"--speed-time", "120",
		"--max-time", strconv.FormatInt(seconds, 10),
		"--user-agent", "Herdr-Sandbox",
	}
	for _, header := range headers {
		arguments = append(arguments, "--header", header)
	}
	return append(arguments, uri)
}

func secureVoxCPM2CurlEnvironment(parent []string) []string {
	environment := make([]string, 0, len(parent))
	for _, entry := range parent {
		name, _, found := strings.Cut(entry, "=")
		if found && (strings.EqualFold(name, "CURL_CA_BUNDLE") ||
			strings.EqualFold(name, "CURL_SSL_BACKEND") ||
			strings.EqualFold(name, "SSL_CERT_DIR") ||
			strings.EqualFold(name, "SSL_CERT_FILE") ||
			strings.EqualFold(name, "SSLKEYLOGFILE")) {
			continue
		}
		environment = append(environment, entry)
	}
	return environment
}

func runVoxCPM2CurlBytes(ctx context.Context, curl, uri string, maximum int64, headers ...string) ([]byte, error) {
	commandContext, cancel := context.WithTimeout(ctx, voxcpm2MetadataTimeout+30*time.Second)
	defer cancel()
	command := hiddenCommandContext(commandContext, curl, secureVoxCPM2CurlArguments(uri, voxcpm2MetadataTimeout, headers...)...)
	command.Env = secureVoxCPM2CurlEnvironment(command.Env)
	stderr := boundedCommandOutput{maximum: maximumVoxCPM2CurlErrorBytes}
	command.Stderr = &stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("capture secure curl output: %w", err)
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start secure curl: %w", err)
	}
	payload, readErr := io.ReadAll(io.LimitReader(stdout, maximum+1))
	if int64(len(payload)) > maximum {
		terminateErr := command.Terminate()
		waitErr := command.Wait()
		return nil, errors.Join(fmt.Errorf("secure curl output exceeds %d bytes", maximum), terminateErr, waitErr)
	}
	waitErr := command.Wait()
	if readErr != nil {
		return nil, fmt.Errorf("read secure curl output: %w", readErr)
	}
	if waitErr != nil {
		return nil, fmt.Errorf("secure curl failed: %w: %s", waitErr, stderr.text())
	}
	return payload, nil
}

func prepareVoxCPM2CoreRelease(ctx context.Context, curl, cacheRoot string, output io.Writer) (voxcpm2ReleaseDescriptor, error) {
	payload, err := runVoxCPM2CurlBytes(ctx, curl, voxcpm2LatestReleaseURL, maximumVoxCPM2MetadataBytes,
		"Accept: application/vnd.github+json", "X-GitHub-Api-Version: 2022-11-28")
	if err != nil {
		return voxcpm2ReleaseDescriptor{}, fmt.Errorf("resolve latest HyperFrames VoxCPM2 release: %w", err)
	}
	var release voxcpm2GitHubRelease
	if err := json.Unmarshal(payload, &release); err != nil {
		return voxcpm2ReleaseDescriptor{}, fmt.Errorf("decode latest HyperFrames VoxCPM2 release: %w", err)
	}
	archiveAsset, sidecarAsset, err := validateVoxCPM2GitHubRelease(release)
	if err != nil {
		return voxcpm2ReleaseDescriptor{}, err
	}
	releaseDirectory, err := ensurePhysicalDirectory(filepath.Join(cacheRoot, "releases", release.TagName), "HyperFrames VoxCPM2 release cache")
	if err != nil {
		return voxcpm2ReleaseDescriptor{}, err
	}
	sidecarPath := filepath.Join(releaseDirectory, sidecarAsset.Name)
	if err := downloadVoxCPM2File(ctx, curl, sidecarAsset.BrowserDownloadURL, sidecarPath, sidecarAsset.Size, strings.TrimPrefix(sidecarAsset.Digest, "sha256:")); err != nil {
		return voxcpm2ReleaseDescriptor{}, fmt.Errorf("download HyperFrames VoxCPM2 checksum sidecar: %w", err)
	}
	sidecar, err := os.ReadFile(sidecarPath)
	if err != nil {
		return voxcpm2ReleaseDescriptor{}, fmt.Errorf("read HyperFrames VoxCPM2 checksum sidecar: %w", err)
	}
	archiveSHA256, err := parseVoxCPM2Sidecar(sidecar, archiveAsset.Name)
	if err != nil {
		return voxcpm2ReleaseDescriptor{}, err
	}
	if archiveSHA256 != strings.TrimPrefix(archiveAsset.Digest, "sha256:") {
		return voxcpm2ReleaseDescriptor{}, errors.New("GitHub release digest and checksum sidecar disagree")
	}
	archivePath := filepath.Join(releaseDirectory, archiveAsset.Name)
	cached, err := voxcpm2FileMatches(archivePath, archiveAsset.Size, archiveSHA256)
	if err != nil {
		return voxcpm2ReleaseDescriptor{}, err
	}
	if !cached {
		fmt.Fprintf(output, "  Downloading HyperFrames VoxCPM2 %s (%d bytes)...\n", release.TagName, archiveAsset.Size)
		if err := downloadVoxCPM2File(ctx, curl, archiveAsset.BrowserDownloadURL, archivePath, archiveAsset.Size, archiveSHA256); err != nil {
			return voxcpm2ReleaseDescriptor{}, fmt.Errorf("download HyperFrames VoxCPM2 release: %w", err)
		}
	} else {
		fmt.Fprintf(output, "  HyperFrames VoxCPM2 release cache hit: %s\n", release.TagName)
	}
	manifest, err := inspectVoxCPM2CoreArchive(archivePath, release.TagName)
	if err != nil {
		return voxcpm2ReleaseDescriptor{}, err
	}
	return voxcpm2ReleaseDescriptor{
		SchemaVersion:      1,
		Tag:                release.TagName,
		ArchiveName:        archiveAsset.Name,
		ArchiveSize:        archiveAsset.Size,
		ArchiveSHA256:      archiveSHA256,
		HyperFramesVersion: manifest.HyperFrames.Version,
		RuntimeCommit:      manifest.Runtime.Commit,
		Models:             manifest.Models,
		ReferenceAudio:     manifest.ReferenceAudio,
	}, nil
}

func validateVoxCPM2GitHubRelease(release voxcpm2GitHubRelease) (voxcpm2GitHubAsset, voxcpm2GitHubAsset, error) {
	if !voxcpm2ReleaseTagPattern.MatchString(release.TagName) || release.Draft || release.Prerelease {
		return voxcpm2GitHubAsset{}, voxcpm2GitHubAsset{}, errors.New("latest HyperFrames VoxCPM2 release is not a stable semantic version")
	}
	archiveName := "hyperframes-voxcpm2-" + release.TagName + "-windows-x64.zip"
	sidecarName := archiveName + ".sha256"
	var archive, sidecar voxcpm2GitHubAsset
	archiveCount, sidecarCount := 0, 0
	for _, asset := range release.Assets {
		switch asset.Name {
		case archiveName:
			archive, archiveCount = asset, archiveCount+1
		case sidecarName:
			sidecar, sidecarCount = asset, sidecarCount+1
		}
	}
	if archiveCount != 1 || sidecarCount != 1 {
		return voxcpm2GitHubAsset{}, voxcpm2GitHubAsset{}, errors.New("latest HyperFrames VoxCPM2 release does not contain one exact Windows archive and checksum sidecar")
	}
	for _, asset := range []voxcpm2GitHubAsset{archive, sidecar} {
		if asset.Size <= 0 || asset.Digest == "" || !voxcpm2SHA256Pattern.MatchString(strings.TrimPrefix(asset.Digest, "sha256:")) ||
			!strings.HasPrefix(asset.Digest, "sha256:") {
			return voxcpm2GitHubAsset{}, voxcpm2GitHubAsset{}, fmt.Errorf("GitHub release asset %s has invalid size or digest", asset.Name)
		}
		if err := validateVoxCPM2GitHubAssetURL(asset.BrowserDownloadURL, release.TagName, asset.Name); err != nil {
			return voxcpm2GitHubAsset{}, voxcpm2GitHubAsset{}, err
		}
	}
	if archive.Size > maximumVoxCPM2ArchiveBytes || sidecar.Size > 1024 {
		return voxcpm2GitHubAsset{}, voxcpm2GitHubAsset{}, errors.New("HyperFrames VoxCPM2 release assets exceed their size limits")
	}
	return archive, sidecar, nil
}

func validateVoxCPM2GitHubAssetURL(raw, tag, name string) error {
	parsed, err := parseHTTPSURL(raw)
	if err != nil {
		return fmt.Errorf("GitHub release asset %s: %w", name, err)
	}
	expectedPath := "/hdosys/hyperframes-voxcpm2/releases/download/" + tag + "/" + name
	if !strings.EqualFold(parsed.Host, "github.com") || parsed.Path != expectedPath || parsed.RawQuery != "" {
		return fmt.Errorf("GitHub release asset %s has an unexpected URL", name)
	}
	return nil
}

func parseVoxCPM2Sidecar(payload []byte, archiveName string) (string, error) {
	expectedSuffix := "  " + archiveName + "\n"
	text := strings.ReplaceAll(string(payload), "\r\n", "\n")
	if len(text) != 64+len(expectedSuffix) || !strings.HasSuffix(text, expectedSuffix) {
		return "", errors.New("HyperFrames VoxCPM2 checksum sidecar has an invalid format")
	}
	digest := text[:64]
	if !voxcpm2SHA256Pattern.MatchString(digest) {
		return "", errors.New("HyperFrames VoxCPM2 checksum sidecar has an invalid SHA-256")
	}
	return digest, nil
}

func inspectVoxCPM2CoreArchive(archivePath, tag string) (voxcpm2CoreManifest, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return voxcpm2CoreManifest{}, fmt.Errorf("open HyperFrames VoxCPM2 release archive: %w", err)
	}
	defer reader.Close()
	if len(reader.File) == 0 || len(reader.File) > maximumVoxCPM2ArchiveEntries {
		return voxcpm2CoreManifest{}, fmt.Errorf("HyperFrames VoxCPM2 archive entry count is invalid: %d", len(reader.File))
	}
	entries := make(map[string]*zip.File, len(reader.File))
	var total uint64
	for _, entry := range reader.File {
		if err := validateVoxCPM2ArchivePath(entry.Name); err != nil {
			return voxcpm2CoreManifest{}, err
		}
		if _, exists := entries[entry.Name]; exists {
			return voxcpm2CoreManifest{}, fmt.Errorf("HyperFrames VoxCPM2 archive contains duplicate entry %s", entry.Name)
		}
		if entry.FileInfo().Mode()&os.ModeSymlink != 0 {
			return voxcpm2CoreManifest{}, fmt.Errorf("HyperFrames VoxCPM2 archive entry is a symbolic link: %s", entry.Name)
		}
		total += entry.UncompressedSize64
		if total > maximumVoxCPM2ExtractedBytes {
			return voxcpm2CoreManifest{}, errors.New("HyperFrames VoxCPM2 archive exceeds its extracted-size limit")
		}
		entries[entry.Name] = entry
	}
	manifestEntry := entries["manifest.json"]
	if manifestEntry == nil || manifestEntry.UncompressedSize64 == 0 || manifestEntry.UncompressedSize64 > maximumVoxCPM2MetadataBytes {
		return voxcpm2CoreManifest{}, errors.New("HyperFrames VoxCPM2 archive manifest is missing or too large")
	}
	manifestPayload, err := readVoxCPM2ZipEntry(manifestEntry, int64(manifestEntry.UncompressedSize64))
	if err != nil {
		return voxcpm2CoreManifest{}, err
	}
	var manifest voxcpm2CoreManifest
	if err := json.Unmarshal(manifestPayload, &manifest); err != nil {
		return voxcpm2CoreManifest{}, fmt.Errorf("decode HyperFrames VoxCPM2 archive manifest: %w", err)
	}
	if err := validateVoxCPM2CoreManifest(manifest, tag); err != nil {
		return voxcpm2CoreManifest{}, err
	}
	if len(manifest.Files) != len(entries)-1 {
		return voxcpm2CoreManifest{}, errors.New("HyperFrames VoxCPM2 archive file list is incomplete")
	}
	seen := make(map[string]bool, len(manifest.Files))
	for _, expected := range manifest.Files {
		if seen[expected.Path] || expected.Path == "manifest.json" || expected.Size < 0 || !voxcpm2SHA256Pattern.MatchString(expected.SHA256) {
			return voxcpm2CoreManifest{}, fmt.Errorf("HyperFrames VoxCPM2 manifest file record is invalid: %s", expected.Path)
		}
		seen[expected.Path] = true
		entry := entries[expected.Path]
		if entry == nil || int64(entry.UncompressedSize64) != expected.Size {
			return voxcpm2CoreManifest{}, fmt.Errorf("HyperFrames VoxCPM2 archive file identity does not match: %s", expected.Path)
		}
		digest, err := hashVoxCPM2ZipEntry(entry, expected.Size)
		if err != nil {
			return voxcpm2CoreManifest{}, err
		}
		if digest != expected.SHA256 {
			return voxcpm2CoreManifest{}, fmt.Errorf("HyperFrames VoxCPM2 archive file checksum mismatch: %s", expected.Path)
		}
	}
	return manifest, nil
}

func validateVoxCPM2ArchivePath(name string) error {
	if name == "" || strings.Contains(name, `\`) || strings.HasPrefix(name, "/") || path.Clean(name) != name ||
		strings.Contains(strings.Split(name, "/")[0], ":") {
		return fmt.Errorf("HyperFrames VoxCPM2 archive entry is unsafe: %s", name)
	}
	for part := range strings.SplitSeq(name, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("HyperFrames VoxCPM2 archive entry is unsafe: %s", name)
		}
	}
	return nil
}

func readVoxCPM2ZipEntry(entry *zip.File, expectedSize int64) ([]byte, error) {
	stream, err := entry.Open()
	if err != nil {
		return nil, fmt.Errorf("open HyperFrames VoxCPM2 archive entry %s: %w", entry.Name, err)
	}
	defer stream.Close()
	payload, err := io.ReadAll(io.LimitReader(stream, expectedSize+1))
	if err != nil {
		return nil, fmt.Errorf("read HyperFrames VoxCPM2 archive entry %s: %w", entry.Name, err)
	}
	if int64(len(payload)) != expectedSize {
		return nil, fmt.Errorf("HyperFrames VoxCPM2 archive entry size changed: %s", entry.Name)
	}
	return payload, nil
}

func hashVoxCPM2ZipEntry(entry *zip.File, expectedSize int64) (string, error) {
	stream, err := entry.Open()
	if err != nil {
		return "", fmt.Errorf("open HyperFrames VoxCPM2 archive entry %s: %w", entry.Name, err)
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(hasher, io.LimitReader(stream, expectedSize+1))
	closeErr := stream.Close()
	if copyErr != nil || closeErr != nil {
		return "", fmt.Errorf("hash HyperFrames VoxCPM2 archive entry %s: %w", entry.Name, errors.Join(copyErr, closeErr))
	}
	if written != expectedSize {
		return "", fmt.Errorf("HyperFrames VoxCPM2 archive entry size changed: %s", entry.Name)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func validateVoxCPM2CoreManifest(manifest voxcpm2CoreManifest, tag string) error {
	version := strings.TrimPrefix(tag, "v")
	if manifest.SchemaVersion != 1 || manifest.ReleaseVersion != version || manifest.Platform != "windows-x64" ||
		!voxcpm2ReleaseTagPattern.MatchString("v"+manifest.ReleaseVersion) ||
		!voxcpm2ReleaseTagPattern.MatchString("v"+manifest.HyperFrames.Version) ||
		!voxcpm2RevisionPattern.MatchString(manifest.Runtime.Commit) {
		return errors.New("HyperFrames VoxCPM2 archive manifest identity is invalid")
	}
	required := []string{
		"THIRD_PARTY_NOTICES.md",
		"bin/tts.ps1",
		"engine/audio/scripts/audio.mjs",
		"engine/audio/scripts/lib/tts.mjs",
		"engine/audio/scripts/lib/voxcpm2-cli.mjs",
		"engine/audio/scripts/lib/voxcpm2.mjs",
		"runtime/cpu/llama-tts-server.exe",
	}
	paths := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		if strings.HasPrefix(file.Path, "runtime/") && !strings.HasPrefix(file.Path, "runtime/cpu/") {
			return fmt.Errorf("HyperFrames VoxCPM2 archive manifest contains a non-CPU runtime: %s", file.Path)
		}
		paths = append(paths, file.Path)
	}
	for _, requiredPath := range required {
		if !slices.Contains(paths, requiredPath) {
			return fmt.Errorf("HyperFrames VoxCPM2 archive manifest is missing %s", requiredPath)
		}
	}
	if manifest.Models.Repository == "" || !voxcpm2RevisionPattern.MatchString(manifest.Models.Revision) || len(manifest.Models.Files) != 2 {
		return errors.New("HyperFrames VoxCPM2 model manifest is invalid")
	}
	expectedNames := []string{"VoxCPM2-Acoustic-F16.gguf", "VoxCPM2-BaseLM-F16.gguf"}
	actualNames := make([]string, 0, len(manifest.Models.Files))
	for _, artifact := range manifest.Models.Files {
		if err := validateVoxCPM2Artifact(artifact, "huggingface.co"); err != nil {
			return err
		}
		actualNames = append(actualNames, artifact.Name)
	}
	slices.Sort(actualNames)
	if !slices.Equal(actualNames, expectedNames) {
		return errors.New("HyperFrames VoxCPM2 model file selection is invalid")
	}
	if err := validateVoxCPM2Artifact(manifest.ReferenceAudio, "raw.githubusercontent.com"); err != nil {
		return err
	}
	if manifest.ReferenceAudio.Name != "reference_speaker.wav" {
		return errors.New("HyperFrames VoxCPM2 reference audio name is invalid")
	}
	return nil
}

func validateVoxCPM2Artifact(artifact voxcpm2Artifact, expectedHost string) error {
	if artifact.Name == "" || filepath.Base(artifact.Name) != artifact.Name || artifact.Size <= 0 || artifact.Size > 16*1024*1024*1024 ||
		!voxcpm2SHA256Pattern.MatchString(artifact.SHA256) {
		return fmt.Errorf("HyperFrames VoxCPM2 artifact identity is invalid: %s", artifact.Name)
	}
	parsed, err := parseHTTPSURL(artifact.URL)
	if err != nil {
		return fmt.Errorf("HyperFrames VoxCPM2 artifact %s: %w", artifact.Name, err)
	}
	if !strings.EqualFold(parsed.Host, expectedHost) || path.Base(parsed.Path) != artifact.Name {
		return fmt.Errorf("HyperFrames VoxCPM2 artifact URL is invalid: %s", artifact.Name)
	}
	return nil
}

func parseHTTPSURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("URL must use credential-free HTTPS")
	}
	return parsed, nil
}

func prepareVoxCPM2Models(ctx context.Context, curl, modelDirectory string, descriptor voxcpm2ReleaseDescriptor, output io.Writer) error {
	expected := voxcpm2ModelCompletion{SchemaVersion: 1, Models: descriptor.Models, ReferenceAudio: descriptor.ReferenceAudio}
	completionPath := filepath.Join(modelDirectory, voxcpm2ModelCompletionName)
	completion, found, err := readVoxCPM2ModelCompletion(completionPath)
	if err != nil {
		return err
	}
	if found && voxcpm2ModelCompletionMatches(completion, expected) {
		if err := validateCompletedVoxCPM2Models(modelDirectory, expected); err != nil {
			return err
		}
		fmt.Fprintf(output, "  HyperFrames VoxCPM2 model cache hit: %s\n", descriptor.Models.Revision)
		return nil
	}
	artifacts := append([]voxcpm2Artifact(nil), descriptor.Models.Files...)
	artifacts = append(artifacts, descriptor.ReferenceAudio)
	for _, artifact := range artifacts {
		cached, err := voxcpm2FileMatches(filepath.Join(modelDirectory, artifact.Name), artifact.Size, artifact.SHA256)
		if err != nil {
			return fmt.Errorf("inspect HyperFrames VoxCPM2 artifact %s: %w", artifact.Name, err)
		}
		if cached {
			fmt.Fprintf(output, "  HyperFrames VoxCPM2 artifact cache hit: %s\n", artifact.Name)
			continue
		}
		fmt.Fprintf(output, "  Downloading %s (%d bytes)...\n", artifact.Name, artifact.Size)
		if err := downloadVoxCPM2File(ctx, curl, artifact.URL, filepath.Join(modelDirectory, artifact.Name), artifact.Size, artifact.SHA256); err != nil {
			return fmt.Errorf("admit HyperFrames VoxCPM2 artifact %s: %w", artifact.Name, err)
		}
	}
	if err := writeVoxCPM2JSONAtomically(completionPath, expected); err != nil {
		return fmt.Errorf("publish HyperFrames VoxCPM2 model completion: %w", err)
	}
	return nil
}

func readVoxCPM2ModelCompletion(path string) (voxcpm2ModelCompletion, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return voxcpm2ModelCompletion{}, false, nil
	}
	if err != nil {
		return voxcpm2ModelCompletion{}, false, fmt.Errorf("inspect HyperFrames VoxCPM2 model completion: %w", err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return voxcpm2ModelCompletion{}, false, fmt.Errorf("inspect HyperFrames VoxCPM2 model completion: %w", err)
	}
	if !info.Mode().IsRegular() || reparse || info.Size() <= 0 || info.Size() > maximumVoxCPM2MetadataBytes {
		return voxcpm2ModelCompletion{}, false, errors.New("HyperFrames VoxCPM2 model completion is unsafe")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return voxcpm2ModelCompletion{}, false, fmt.Errorf("read HyperFrames VoxCPM2 model completion: %w", err)
	}
	var completion voxcpm2ModelCompletion
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&completion); err != nil {
		return voxcpm2ModelCompletion{}, false, fmt.Errorf("decode HyperFrames VoxCPM2 model completion: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return voxcpm2ModelCompletion{}, false, errors.New("decode HyperFrames VoxCPM2 model completion: trailing data")
	}
	if completion.SchemaVersion != 1 {
		return voxcpm2ModelCompletion{}, false, fmt.Errorf("unsupported HyperFrames VoxCPM2 model completion schema %d", completion.SchemaVersion)
	}
	return completion, true, nil
}

func voxcpm2ModelCompletionMatches(left, right voxcpm2ModelCompletion) bool {
	leftPayload, leftErr := json.Marshal(left)
	rightPayload, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftPayload, rightPayload)
}

func validateCompletedVoxCPM2Models(directory string, completion voxcpm2ModelCompletion) error {
	artifacts := append([]voxcpm2Artifact(nil), completion.Models.Files...)
	artifacts = append(artifacts, completion.ReferenceAudio)
	for _, artifact := range artifacts {
		file := filepath.Join(directory, artifact.Name)
		info, err := os.Lstat(file)
		if err != nil {
			return fmt.Errorf("inspect completed HyperFrames VoxCPM2 artifact %s: %w", artifact.Name, err)
		}
		reparse, err := fileInfoIsReparsePoint(info)
		if err != nil {
			return fmt.Errorf("inspect completed HyperFrames VoxCPM2 artifact %s: %w", artifact.Name, err)
		}
		if !info.Mode().IsRegular() || reparse || info.Size() != artifact.Size {
			return fmt.Errorf("completed HyperFrames VoxCPM2 artifact identity changed: %s", artifact.Name)
		}
	}
	return nil
}

func voxcpm2FileMatches(path string, expectedSize int64, expectedSHA256 string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect cached HyperFrames VoxCPM2 file %s: %w", path, err)
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || reparse || info.Size() != expectedSize {
		return false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(hasher, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return false, errors.Join(copyErr, closeErr)
	}
	return hex.EncodeToString(hasher.Sum(nil)) == expectedSHA256, nil
}

func downloadVoxCPM2File(ctx context.Context, curl, uri, destination string, expectedSize int64, expectedSHA256 string) (resultErr error) {
	if expectedSize <= 0 || !voxcpm2SHA256Pattern.MatchString(expectedSHA256) {
		return errors.New("secure download expected identity is invalid")
	}
	if _, err := parseHTTPSURL(uri); err != nil {
		return err
	}
	partial := destination + ".partial"
	if err := os.Remove(partial); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale HyperFrames VoxCPM2 partial file: %w", err)
	}
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create HyperFrames VoxCPM2 partial file: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = os.Remove(partial)
		}
	}()
	commandContext, cancel := context.WithTimeout(ctx, voxcpm2DownloadTimeout+30*time.Second)
	defer cancel()
	command := hiddenCommandContext(commandContext, curl, secureVoxCPM2CurlArguments(uri, voxcpm2DownloadTimeout)...)
	command.Env = secureVoxCPM2CurlEnvironment(command.Env)
	stderr := boundedCommandOutput{maximum: maximumVoxCPM2CurlErrorBytes}
	command.Stderr = &stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("capture secure curl download: %w", err)
	}
	if err := command.Start(); err != nil {
		_ = file.Close()
		return fmt.Errorf("start secure curl download: %w", err)
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hasher), io.LimitReader(stdout, expectedSize+1))
	if written > expectedSize {
		terminateErr := command.Terminate()
		waitErr := command.Wait()
		closeErr := file.Close()
		return errors.Join(errors.New("secure download exceeds its expected size"), copyErr, terminateErr, waitErr, closeErr)
	}
	waitErr := command.Wait()
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil || waitErr != nil || syncErr != nil || closeErr != nil {
		return fmt.Errorf("secure download failed: %w: %s", errors.Join(copyErr, waitErr, syncErr, closeErr), stderr.text())
	}
	if written != expectedSize {
		return fmt.Errorf("secure download size = %d, want %d", written, expectedSize)
	}
	actualSHA256 := hex.EncodeToString(hasher.Sum(nil))
	if actualSHA256 != expectedSHA256 {
		return fmt.Errorf("secure download SHA-256 = %s, want %s", actualSHA256, expectedSHA256)
	}
	if info, err := os.Lstat(destination); err == nil {
		reparse, reparseErr := fileInfoIsReparsePoint(info)
		if reparseErr != nil {
			return reparseErr
		}
		if !info.Mode().IsRegular() || reparse {
			return fmt.Errorf("refusing to replace unsafe HyperFrames VoxCPM2 file: %s", destination)
		}
		if err := replaceFileAtomically(destination, partial, ""); err != nil {
			return err
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(partial, destination); err != nil {
			return fmt.Errorf("publish HyperFrames VoxCPM2 file: %w", err)
		}
	} else {
		return fmt.Errorf("inspect HyperFrames VoxCPM2 destination: %w", err)
	}
	return nil
}

func writeVoxCPM2JSONAtomically(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	partial := path + ".partial"
	if err := os.Remove(partial); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.WriteFile(partial, payload, 0o600); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		reparse, reparseErr := fileInfoIsReparsePoint(info)
		if reparseErr != nil {
			return reparseErr
		}
		if !info.Mode().IsRegular() || reparse {
			return fmt.Errorf("refusing to replace unsafe JSON file: %s", path)
		}
		return replaceFileAtomically(path, partial, "")
	} else if errors.Is(err, os.ErrNotExist) {
		return os.Rename(partial, path)
	} else {
		return err
	}
}
