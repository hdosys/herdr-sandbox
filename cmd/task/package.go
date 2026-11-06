package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"debug/pe"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"herdr-sandbox/internal/productidentity"
)

const (
	installerUninstallerName    = "uninstall.exe"
	canonicalLocalInstallerName = "herdr-sandbox_setup.exe"
	canonicalLocalEvidenceName  = "herdr-sandbox_setup.json"
)

const installerBuildValidatorName = "validate-build.ps1"

var releaseTagPattern = regexp.MustCompile(`^v0\.0\.(0|[1-9][0-9]*)$`)

type releasePackageFile struct {
	Name string
	Mode os.FileMode
}

var releasePackageFiles = []releasePackageFile{
	{Name: productidentity.ExecutableName, Mode: 0o755},
	{Name: productidentity.BaseScriptName, Mode: 0o644},
	{Name: productidentity.StackScriptName, Mode: 0o644},
	{Name: productidentity.LicenseName, Mode: 0o644},
}

type releaseVersion struct {
	ReleaseID uint16
	Tag       string
	Display   string
	Fixed     string
}

type releasePackagePaths struct {
	Stage     string
	ZIP       string
	Installer string
}

type releaseArtifactEvidence struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func packageWindowsRelease(ctx context.Context, tag string, releaseMode bool, stdout, stderr io.Writer) (resultErr error) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return fmt.Errorf("package requires windows/amd64, got %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	version, err := parseReleaseVersion(tag)
	if err != nil {
		return err
	}
	if err := validateWingetManifestProductCodes(); err != nil {
		return err
	}
	revision, err := cleanPackageSource(ctx)
	if err != nil {
		return fmt.Errorf("resolve release source revision: %w", err)
	}
	identity := buildIdentity{
		Version:   version.Display,
		Revision:  revision,
		Freshness: productidentity.FormatBuildFreshness(time.Now()),
	}
	if releaseMode {
		return packageReleaseArtifacts(ctx, version, identity, stdout, stderr)
	}
	return packageLocalInstaller(ctx, version, identity, stdout, stderr)
}

func packageLocalInstaller(ctx context.Context, version releaseVersion, identity buildIdentity, stdout, stderr io.Writer) (resultErr error) {
	canonical := canonicalLocalInstallerPath(".")
	retained := map[string]bool{canonicalLocalInstallerName: true, canonicalLocalEvidenceName: true}
	if err := cleanBuildOutputs(".", retained); err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, cleanBuildOutputs(".", retained)) }()
	buildRoot := filepath.Join(".", "build")
	if err := os.MkdirAll(buildRoot, 0o755); err != nil {
		return fmt.Errorf("create local package workspace root: %w", err)
	}
	temporaryRoot, err := os.MkdirTemp(buildRoot, ".local-package-*")
	if err != nil {
		return fmt.Errorf("create local package workspace: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, removeGeneratedPath(temporaryRoot)) }()
	paths := releasePaths(temporaryRoot, version)
	paths.ZIP = ""
	if err := buildWindowsPackage(ctx, version, identity, paths, stdout, stderr); err != nil {
		return err
	}
	evidence, err := captureLocalInstallerEvidence(identity, paths)
	if err != nil {
		return err
	}
	evidencePath := filepath.Join(temporaryRoot, canonicalLocalEvidenceName)
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(evidencePath, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write local installer evidence: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(canonical), 0o755); err != nil {
		return fmt.Errorf("create canonical local artifact directory: %w", err)
	}
	if err := replaceGeneratedFile(paths.Installer, canonical); err != nil {
		return fmt.Errorf("publish canonical local installer: %w", err)
	}
	if err := replaceGeneratedFile(evidencePath, filepath.Join(filepath.Dir(canonical), canonicalLocalEvidenceName)); err != nil {
		return fmt.Errorf("publish local installer evidence: %w", err)
	}
	return writeArtifactEvidenceWithHash(stdout, "local-installer", canonical, evidence.InstallerSHA256)
}

func packageReleaseArtifacts(ctx context.Context, version releaseVersion, identity buildIdentity, stdout, stderr io.Writer) (resultErr error) {
	paths := releasePaths(".", version)
	retained := map[string]bool{filepath.Base(paths.ZIP): true, filepath.Base(paths.Installer): true}
	if err := cleanBuildOutputs(".", retained); err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, cleanBuildOutputs(".", retained))
	}()
	if err := buildWindowsPackage(ctx, version, identity, paths, stdout, stderr); err != nil {
		return err
	}
	return writeReleaseArtifactEvidence(stdout, paths)
}

func buildWindowsPackage(ctx context.Context, version releaseVersion, identity buildIdentity, paths releasePackagePaths, stdout, stderr io.Writer) error {
	revision, err := normalizeSourceRevision(identity.Revision)
	if err != nil {
		return fmt.Errorf("validate release source revision: %w", err)
	}
	identity.Version = version.Display
	identity.Revision = revision
	if err := buildWithIdentity(ctx, identity, stdout, stderr); err != nil {
		return err
	}
	if err := stageReleasePackage(filepath.Join("build", "bin"), paths.Stage); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(paths.Installer), 0o755); err != nil {
		return fmt.Errorf("create release output directory: %w", err)
	}
	temporaryDirectory, err := os.MkdirTemp(filepath.Dir(paths.Installer), ".release-*")
	if err != nil {
		return fmt.Errorf("create release output stage: %w", err)
	}
	defer func() { _ = os.RemoveAll(temporaryDirectory) }()
	generated := releasePackagePaths{
		Stage:     paths.Stage,
		Installer: filepath.Join(temporaryDirectory, filepath.Base(paths.Installer)),
	}
	if paths.ZIP != "" {
		generated.ZIP = filepath.Join(temporaryDirectory, filepath.Base(paths.ZIP))
		if err := writeReleaseZIP(generated.Stage, generated.ZIP); err != nil {
			return err
		}
	}
	if err := buildNSISInstaller(ctx, version, identity, generated.Stage, generated.Installer, stdout, stderr); err != nil {
		return err
	}
	current, err := cleanPackageSource(ctx)
	if err != nil {
		return err
	}
	if current != identity.Revision {
		return errors.New("package source changed during build")
	}
	if err := publishReleaseArtifactSet(generated, paths); err != nil {
		return err
	}
	return nil
}

func validateWingetManifestProductCodes() error {
	pattern := filepath.Join("packaging", "winget", "manifests", "*", "*", "*", "*", "*.installer.yaml")
	manifests, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("find WinGet installer manifests: %w", err)
	}
	if len(manifests) == 0 {
		return errors.New("package requires at least one WinGet installer manifest")
	}
	expected := "ProductCode: '" + productidentity.UninstallKeyName + "'"
	for _, manifest := range manifests {
		contents, err := os.ReadFile(manifest)
		if err != nil {
			return fmt.Errorf("read WinGet installer manifest %s: %w", manifest, err)
		}
		count := 0
		for line := range strings.SplitSeq(string(contents), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "ProductCode:") {
				count++
				if strings.TrimSpace(line) != expected {
					return fmt.Errorf("WinGet installer manifest %s ProductCode must equal %s", manifest, productidentity.UninstallKeyName)
				}
			}
		}
		if count != 1 {
			return fmt.Errorf("WinGet installer manifest %s has %d ProductCode fields, want 1", manifest, count)
		}
	}
	return nil
}

func releaseOutputPaths(paths releasePackagePaths) []string {
	if paths.ZIP == "" {
		return []string{paths.Installer}
	}
	return []string{paths.ZIP, paths.Installer}
}

func writeReleaseArtifactEvidence(stdout io.Writer, paths releasePackagePaths) error {
	for _, path := range releaseOutputPaths(paths) {
		if err := writeArtifactEvidence(stdout, "release-artifact", path); err != nil {
			return err
		}
	}
	return nil
}

func writeArtifactEvidence(stdout io.Writer, kind, path string) error {
	hash, err := fileSHA256(path)
	if err != nil {
		return err
	}
	return writeArtifactEvidenceWithHash(stdout, kind, path, fmt.Sprintf("%x", hash))
}

func writeArtifactEvidenceWithHash(stdout io.Writer, kind, path, hash string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect published artifact: %w", err)
	}
	evidence := releaseArtifactEvidence{Kind: kind, Path: filepath.Clean(path), Bytes: info.Size(), SHA256: hash}
	if err := json.NewEncoder(stdout).Encode(evidence); err != nil {
		return fmt.Errorf("write artifact evidence: %w", err)
	}
	return nil
}

func publishReleaseArtifactSet(generated, destination releasePackagePaths) error {
	sources := releaseOutputPaths(generated)
	destinations := releaseOutputPaths(destination)
	for _, path := range destinations {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr := removeReleaseArtifactSet(destinations)
			return fmt.Errorf("prepare release artifact set: %w", errors.Join(err, cleanupErr))
		}
	}
	for index, source := range sources {
		if filepath.Base(source) != filepath.Base(destinations[index]) {
			cleanupErr := removeReleaseArtifactSet(destinations)
			return fmt.Errorf("publish release artifact set: %w", errors.Join(errors.New("artifact name changed"), cleanupErr))
		}
		if err := os.Rename(source, destinations[index]); err != nil {
			cleanupErr := removeReleaseArtifactSet(destinations)
			return fmt.Errorf("publish release artifact set: %w", errors.Join(err, cleanupErr))
		}
	}
	return nil
}

func removeReleaseArtifactSet(paths []string) error {
	var cleanupErr error
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove release artifact %s: %w", path, err))
		}
	}
	return cleanupErr
}

func parseReleaseVersion(tag string) (releaseVersion, error) {
	tag = strings.TrimSpace(tag)
	match := releaseTagPattern.FindStringSubmatch(tag)
	if len(match) != 2 {
		return releaseVersion{}, fmt.Errorf("release version must match v0.0.RELEASE_ID without leading zeroes: %q", tag)
	}
	releaseID, err := strconv.ParseUint(match[1], 10, 16)
	if err != nil {
		return releaseVersion{}, fmt.Errorf("release ID must fit the Windows version segment 0..65535: %q", match[1])
	}
	display := fmt.Sprintf("0.0.%d", releaseID)
	return releaseVersion{ReleaseID: uint16(releaseID), Tag: tag, Display: display, Fixed: display + ".0"}, nil
}

func immediatePredecessorVersion(version releaseVersion) (releaseVersion, error) {
	if version.ReleaseID == 0 {
		return releaseVersion{}, errors.New("installed upgrade acceptance requires a release ID greater than zero")
	}
	return parseReleaseVersion(fmt.Sprintf("v0.0.%d", version.ReleaseID-1))
}

func releasePaths(root string, version releaseVersion) releasePackagePaths {
	baseName := productidentity.ApplicationName + "_" + version.Tag + "_windows_amd64"
	dist := filepath.Join(root, "build", "dist")
	zipPath := filepath.Join(dist, baseName+".zip")
	installerPath := filepath.Join(dist, baseName+"_setup.exe")
	return releasePackagePaths{
		Stage:     filepath.Join(root, "build", "package", baseName),
		ZIP:       zipPath,
		Installer: installerPath,
	}
}

func canonicalLocalInstallerPath(root string) string {
	return filepath.Join(root, "build", "dist", canonicalLocalInstallerName)
}

func cleanBuildOutputs(root string, retainedDistFiles map[string]bool) error {
	buildRoot := filepath.Join(root, "build")
	info, err := os.Lstat(buildRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect build root before cleanup: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("build root is not a regular directory: %s", buildRoot)
	}
	entries, err := os.ReadDir(buildRoot)
	if err != nil {
		return fmt.Errorf("enumerate build root before cleanup: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() != "tools" && entry.Name() != "videos" && entry.Name() != "dist" && !knownGeneratedBuildEntry(entry.Name()) {
			return fmt.Errorf("refusing unknown build-root entry during cleanup: %s", filepath.Join(buildRoot, entry.Name()))
		}
	}
	for _, entry := range entries {
		path := filepath.Join(buildRoot, entry.Name())
		switch entry.Name() {
		case "tools", "videos":
			continue
		case "dist":
			if err := cleanDistributionOutputs(path, retainedDistFiles); err != nil {
				return err
			}
		default:
			if err := removeGeneratedPath(path); err != nil {
				return fmt.Errorf("remove generated build output %s: %w", path, err)
			}
		}
	}
	return nil
}

func knownGeneratedBuildEntry(name string) bool {
	switch name {
	case "bin", "candidates", "installer-welcome-finish-preview.png", "native-all-stacks", "package", "verification", "verified":
		return true
	}
	return strings.HasPrefix(name, "acceptance-all-") || strings.HasPrefix(name, "release-v") || strings.HasPrefix(name, ".local-package-")
}

func cleanDistributionOutputs(directory string, retained map[string]bool) error {
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect distribution output root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("distribution output root is not a regular directory: %s", directory)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("enumerate distribution outputs: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if retained[entry.Name()] || entry.Name() == workstationInstallerName || entry.Name() == workstationEvidenceName {
			info, err := os.Lstat(path)
			if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return fmt.Errorf("retained local artifact is not a regular file: %s", path)
			}
			continue
		}
		if err := removeGeneratedPath(path); err != nil {
			return fmt.Errorf("remove stale distribution output %s: %w", path, err)
		}
	}
	return nil
}

func removeGeneratedPath(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing reparse-backed generated path: %s", path)
	}
	return os.RemoveAll(path)
}

func stageReleasePackage(sourceDirectory, stageDirectory string) error {
	if err := os.RemoveAll(stageDirectory); err != nil {
		return fmt.Errorf("remove previous release stage: %w", err)
	}
	if err := os.MkdirAll(stageDirectory, 0o755); err != nil {
		return fmt.Errorf("create release stage: %w", err)
	}
	for _, file := range releasePackageFiles {
		source := filepath.Join(sourceDirectory, file.Name)
		info, err := os.Lstat(source)
		if err != nil {
			return fmt.Errorf("inspect release input %s: %w", file.Name, err)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("release input must be a nonempty regular file: %s", source)
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return fmt.Errorf("read release input %s: %w", file.Name, err)
		}
		if err := os.WriteFile(filepath.Join(stageDirectory, file.Name), data, file.Mode); err != nil {
			return fmt.Errorf("stage release input %s: %w", file.Name, err)
		}
	}
	return validateReleasePackage(stageDirectory)
}

func validateReleasePackage(stageDirectory string) error {
	entries, err := os.ReadDir(stageDirectory)
	if err != nil {
		return fmt.Errorf("read release stage: %w", err)
	}
	want := make([]string, 0, len(releasePackageFiles))
	for _, file := range releasePackageFiles {
		want = append(want, file.Name)
	}
	sort.Strings(want)
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect staged release file %s: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("staged release entry must be a nonempty regular file: %s", entry.Name())
		}
		got = append(got, entry.Name())
	}
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		return fmt.Errorf("release stage contents = %v, want exactly %v", got, want)
	}
	return nil
}

func writeReleaseZIP(stageDirectory, outputPath string) (resultErr error) {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create release output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(outputPath), ".release-*.zip")
	if err != nil {
		return fmt.Errorf("create release ZIP: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()
	archive := zip.NewWriter(temporary)
	defer func() {
		if resultErr != nil {
			_ = archive.Close()
			_ = temporary.Close()
		}
	}()
	modTime := time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, file := range releasePackageFiles {
		header := &zip.FileHeader{Name: file.Name, Method: zip.Deflate, Modified: modTime}
		header.SetMode(file.Mode)
		destination, err := archive.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("create release ZIP entry %s: %w", file.Name, err)
		}
		source, err := os.Open(filepath.Join(stageDirectory, file.Name))
		if err != nil {
			return fmt.Errorf("open staged release file %s: %w", file.Name, err)
		}
		_, copyErr := io.Copy(destination, source)
		closeErr := source.Close()
		if copyErr != nil || closeErr != nil {
			return fmt.Errorf("write release ZIP entry %s: %w", file.Name, errors.Join(copyErr, closeErr))
		}
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("finish release ZIP: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("flush release ZIP: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close release ZIP: %w", err)
	}
	if err := replaceGeneratedFile(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("publish release ZIP: %w", err)
	}
	return nil
}

func fileSHA256(path string) ([]byte, error) {
	artifact, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open release artifact for checksum: %w", err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, artifact)
	closeErr := artifact.Close()
	if copyErr != nil || closeErr != nil {
		return nil, fmt.Errorf("hash release artifact: %w", errors.Join(copyErr, closeErr))
	}
	return hash.Sum(nil), nil
}

func replaceGeneratedFile(source, destination string) error {
	return os.Rename(source, destination)
}

func buildNSISInstaller(ctx context.Context, version releaseVersion, identity buildIdentity, stageDirectory, outputPath string, stdout, stderr io.Writer) error {
	if !productidentity.ValidBuildFreshness(identity.Freshness) {
		return fmt.Errorf("installer build freshness must use %s UTC format: %q", productidentity.BuildFreshnessLayout, identity.Freshness)
	}
	revision, err := normalizeSourceRevision(identity.Revision)
	if err != nil {
		return fmt.Errorf("validate installer source revision: %w", err)
	}
	shortRevision := revision[:12]
	buildDisplay := productidentity.IdentitySummary(version.Display, identity.Freshness, revision)
	makensis, err := findMakeNSIS()
	if err != nil {
		return err
	}
	versionOutput, err := hiddenCommandContext(ctx, makensis, "/VERSION").CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect NSIS compiler version: %w: %s", err, strings.TrimSpace(string(versionOutput)))
	}
	if actual := strings.TrimSpace(string(versionOutput)); !regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*))?$`).MatchString(actual) {
		return fmt.Errorf("NSIS compiler returned invalid version %q", actual)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create installer output directory: %w", err)
	}
	stageDirectory, err = filepath.Abs(stageDirectory)
	if err != nil {
		return fmt.Errorf("resolve installer package directory: %w", err)
	}
	outputPath, err = filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("resolve installer output: %w", err)
	}
	pathHelper, err := filepath.Abs(filepath.Join("packaging", "windows", "path.ps1"))
	if err != nil {
		return fmt.Errorf("resolve installer PATH helper: %w", err)
	}
	if _, err := requireExecutable(pathHelper, "installer PATH helper"); err != nil {
		return err
	}
	quietUninstallHelper, err := filepath.Abs(filepath.Join("packaging", "windows", "uninstall.ps1"))
	if err != nil {
		return fmt.Errorf("resolve quiet uninstall helper: %w", err)
	}
	if _, err := requireExecutable(quietUninstallHelper, "quiet uninstall helper"); err != nil {
		return err
	}
	buildValidator, err := filepath.Abs(filepath.Join("packaging", "windows", installerBuildValidatorName))
	if err != nil {
		return fmt.Errorf("resolve installer build validator: %w", err)
	}
	if _, err := requireExecutable(buildValidator, "installer build validator"); err != nil {
		return err
	}
	script, err := filepath.Abs(filepath.Join("packaging", "windows", "installer.nsi"))
	if err != nil {
		return fmt.Errorf("resolve installer source: %w", err)
	}
	if err := os.Remove(outputPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove previous installer output: %w", err)
	}
	assetsDirectory, err := filepath.Abs(filepath.Join("packaging", "windows", "assets"))
	if err != nil {
		return fmt.Errorf("resolve installer assets directory: %w", err)
	}
	powerShell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	validatorArgs := []string{
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-WindowStyle", "Hidden",
		"-ExecutionPolicy", "Bypass",
		"-File", buildValidator,
		"-InstallerScript", script,
		"-Version", version.Display,
		"-FixedVersion", version.Fixed,
		"-BuildFreshness", identity.Freshness,
		"-BuildRevision", shortRevision,
		"-BuildDisplay", buildDisplay,
		"-AppName", productidentity.CommandName,
		"-AppApplicationName", productidentity.ApplicationName,
		"-AppDisplayName", productidentity.DisplayName,
		"-AppPublisher", productidentity.Publisher,
		"-AppProductUrl", productidentity.ProductURL,
		"-AppCopyright", productidentity.Copyright,
		"-AppConfigFile", productidentity.ConfigurationName,
		"-AppUserScript", productidentity.UserScriptName,
		"-AppProjectDirectory", productidentity.ProjectDirectoryName,
		"-OutputFile", outputPath,
		"-OutputFileName", filepath.Base(outputPath),
		"-PackageDirectory", stageDirectory,
		"-PathHelper", pathHelper,
		"-QuietUninstallHelper", quietUninstallHelper,
		"-AppProductGuid", productidentity.ProductGUID,
		"-AppUninstallKey", productidentity.UninstallKeyName,
		"-AppInstallDirectory", productidentity.InstallDirectoryName,
		"-AppExecutable", productidentity.ExecutableName,
		"-AppBaseScript", productidentity.BaseScriptName,
		"-AppStackScript", productidentity.StackScriptName,
		"-AppLicense", productidentity.LicenseName,
		"-AppQuietUninstallHelper", productidentity.QuietUninstallHelperName,
		"-AssetsDirectory", assetsDirectory,
	}
	if err := runCommand(ctx, stdout, stderr, powerShell, validatorArgs...); err != nil {
		return fmt.Errorf("validate NSIS installer inputs: %w", err)
	}
	args := []string{
		"/WX",
		"/V2",
		"/NOCONFIG",
		"/DRELEASE_TAG=" + version.Tag,
		"/DVERSION=" + version.Display,
		"/DFIXED_VERSION=" + version.Fixed,
		"/DBUILD_FRESHNESS=" + identity.Freshness,
		"/DBUILD_REVISION=" + shortRevision,
		"/DBUILD_DISPLAY=" + buildDisplay,
		"/DAPP_NAME=" + productidentity.CommandName,
		"/DAPP_APPLICATION_NAME=" + productidentity.ApplicationName,
		"/DAPP_DISPLAY_NAME=" + productidentity.DisplayName,
		"/DAPP_EXECUTABLE=" + productidentity.ExecutableName,
		"/DAPP_BASE_SCRIPT=" + productidentity.BaseScriptName,
		"/DAPP_STACK_SCRIPT=" + productidentity.StackScriptName,
		"/DAPP_LICENSE=" + productidentity.LicenseName,
		"/DAPP_CONFIG_FILE=" + productidentity.ConfigurationName,
		"/DAPP_USER_SCRIPT=" + productidentity.UserScriptName,
		"/DAPP_PROJECT_DIRECTORY=" + productidentity.ProjectDirectoryName,
		"/DAPP_INSTALL_DIRECTORY=" + productidentity.InstallDirectoryName,
		"/DAPP_PUBLISHER=" + productidentity.Publisher,
		"/DAPP_PRODUCT_URL=" + productidentity.ProductURL,
		"/DAPP_PRODUCT_GUID=" + productidentity.ProductGUID,
		"/DAPP_UNINSTALL_KEY=" + productidentity.UninstallKeyName,
		"/DAPP_QUIET_UNINSTALL_HELPER=" + productidentity.QuietUninstallHelperName,
		"/DAPP_COPYRIGHT=" + productidentity.Copyright,
		"/DPACKAGE_DIR=" + stageDirectory,
		"/DPATH_HELPER=" + pathHelper,
		"/DQUIET_UNINSTALL_HELPER=" + quietUninstallHelper,
		"/DASSETS_DIR=" + assetsDirectory,
		"/DOUTPUT_FILE=" + outputPath,
		"/DOUTPUT_FILE_NAME=" + filepath.Base(outputPath),
		script,
	}
	if err := runCommand(ctx, stdout, stderr, makensis, args...); err != nil {
		return fmt.Errorf("build NSIS installer: %w", err)
	}
	return verifyInstallerArtifact(outputPath)
}

func findMakeNSIS() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("MAKENSIS")); configured != "" {
		return requireExecutable(configured, "MAKENSIS")
	}
	if path, err := exec.LookPath("makensis.exe"); err == nil {
		return path, nil
	}
	for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if strings.TrimSpace(base) == "" {
			continue
		}
		candidate := filepath.Join(base, "NSIS", "makensis.exe")
		if path, err := requireExecutable(candidate, "NSIS compiler"); err == nil {
			return path, nil
		}
	}
	return "", errors.New("makensis.exe was not found; set MAKENSIS or provision the current stable NSIS compiler")
}

func requireExecutable(path, role string) (string, error) {
	if !filepath.IsAbs(path) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve %s path: %w", role, err)
		}
		path = absolute
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", role, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file: %s", role, path)
	}
	return path, nil
}

func verifyInstallerArtifact(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect installer artifact: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("installer artifact must be a nonempty regular file: %s", path)
	}
	file, err := pe.Open(path)
	if err != nil {
		return fmt.Errorf("inspect installer PE: %w", err)
	}
	defer file.Close()
	if file.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_I386 {
		return fmt.Errorf("NSIS installer PE machine = %#x, want %#x", file.FileHeader.Machine, pe.IMAGE_FILE_MACHINE_I386)
	}
	return nil
}
