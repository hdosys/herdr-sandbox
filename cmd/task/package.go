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

const installerEngineVersion = "3.12"

const installerUninstallerName = "uninstall.exe"

const installerBuildValidatorName = "validate-build.ps1"

var releaseTagPattern = regexp.MustCompile(`^v0\.0\.(0|[1-9][0-9]*)$`)
var installerProductGUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

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
	Tag     string
	Display string
	Fixed   string
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

func packageWindowsRelease(ctx context.Context, tag string, stdout, stderr io.Writer) error {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return fmt.Errorf("package requires windows/amd64, got %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	version, err := parseReleaseVersion(tag)
	if err != nil {
		return err
	}
	if err := buildRelease(ctx, version, stdout, stderr); err != nil {
		return err
	}
	paths := releasePaths(".", version)
	if err := stageReleasePackage(filepath.Join("build", "bin"), paths.Stage); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(paths.ZIP), 0o755); err != nil {
		return fmt.Errorf("create release output directory: %w", err)
	}
	temporaryDirectory, err := os.MkdirTemp(filepath.Dir(paths.ZIP), ".release-*")
	if err != nil {
		return fmt.Errorf("create release output stage: %w", err)
	}
	defer func() { _ = os.RemoveAll(temporaryDirectory) }()
	generated := releasePackagePaths{
		Stage:     paths.Stage,
		ZIP:       filepath.Join(temporaryDirectory, filepath.Base(paths.ZIP)),
		Installer: filepath.Join(temporaryDirectory, filepath.Base(paths.Installer)),
	}
	if err := writeReleaseZIP(generated.Stage, generated.ZIP); err != nil {
		return err
	}
	if err := buildNSISInstaller(ctx, version, generated.Stage, generated.Installer, stdout, stderr); err != nil {
		return err
	}
	if err := verifyReleaseArtifactSet(generated); err != nil {
		return err
	}
	if err := publishReleaseArtifactSet(generated, paths); err != nil {
		return err
	}
	if err := writeReleaseArtifactEvidence(stdout, paths); err != nil {
		return err
	}
	return nil
}

func releaseOutputPaths(paths releasePackagePaths) []string {
	return []string{paths.ZIP, paths.Installer}
}

func writeReleaseArtifactEvidence(stdout io.Writer, paths releasePackagePaths) error {
	encoder := json.NewEncoder(stdout)
	for _, path := range releaseOutputPaths(paths) {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("inspect published release artifact: %w", err)
		}
		hash, err := fileSHA256(path)
		if err != nil {
			return err
		}
		evidence := releaseArtifactEvidence{
			Kind:   "candidate-artifact",
			Path:   filepath.Clean(path),
			Bytes:  info.Size(),
			SHA256: fmt.Sprintf("%x", hash),
		}
		if err := encoder.Encode(evidence); err != nil {
			return fmt.Errorf("write release artifact evidence: %w", err)
		}
	}
	return nil
}

func verifyReleaseArtifactSet(paths releasePackagePaths) error {
	for _, artifact := range releaseOutputPaths(paths) {
		info, err := os.Stat(artifact)
		if err != nil {
			return fmt.Errorf("inspect generated release artifact: %w", err)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("generated release artifact must be a nonempty regular file: %s", artifact)
		}
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
	return releaseVersion{Tag: tag, Display: display, Fixed: display + ".0"}, nil
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
	if err := validateReleasePackage(stageDirectory); err != nil {
		return err
	}
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
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, destination)
}

func installerOwnedFiles() []string {
	return []string{
		productidentity.BaseScriptName,
		productidentity.LicenseName,
		productidentity.StackScriptName,
		productidentity.QuietUninstallHelperName,
		installerUninstallerName,
		productidentity.ExecutableName,
	}
}

func validateInstallerLeaf(role, value string) error {
	if value == "" || strings.Trim(value, " ") != value || value == "." || value == ".." || len(value) > 120 {
		return fmt.Errorf("%s must be a nonempty bounded leaf name: %q", role, value)
	}
	if filepath.Base(value) != value || strings.ContainsAny(value, `<>:"/\|?*`) || strings.HasSuffix(value, ".") || strings.HasSuffix(value, " ") {
		return fmt.Errorf("%s must not contain a path, reserved character, or trailing dot/space: %q", role, value)
	}
	for _, character := range value {
		if character < 0x20 {
			return fmt.Errorf("%s contains a control character: %q", role, value)
		}
	}
	base := strings.ToUpper(strings.SplitN(value, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" ||
		(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
		return fmt.Errorf("%s uses a reserved Windows device name: %q", role, value)
	}
	return nil
}

func canonicalInstallerLeaf(value string) string {
	return strings.ToLower(strings.TrimRight(value, " ."))
}

func validateInstallerBuildInputs(version releaseVersion, outputPath string) error {
	if !installerProductGUIDPattern.MatchString(productidentity.ProductGUID) {
		return fmt.Errorf("installer product GUID is invalid: %q", productidentity.ProductGUID)
	}
	expectedRegistryKey := "{" + strings.ToUpper(productidentity.ProductGUID) + "}"
	if productidentity.UninstallKeyName != expectedRegistryKey {
		return fmt.Errorf("installer registry key = %q, want fixed product GUID key %q", productidentity.UninstallKeyName, expectedRegistryKey)
	}
	for role, value := range map[string]string{
		"application name":       productidentity.ApplicationName,
		"executable":             productidentity.ExecutableName,
		"Base script":            productidentity.BaseScriptName,
		"Stacks script":          productidentity.StackScriptName,
		"license":                productidentity.LicenseName,
		"quiet uninstall helper": productidentity.QuietUninstallHelperName,
		"uninstaller":            installerUninstallerName,
	} {
		if err := validateInstallerLeaf(role, value); err != nil {
			return err
		}
	}
	if err := validateInstallerLeaf("install directory", productidentity.InstallDirectoryName); err != nil {
		return err
	}
	if len(productidentity.InstallDirectoryName) > 80 {
		return fmt.Errorf("install directory name is too long: %q", productidentity.InstallDirectoryName)
	}
	for role, value := range map[string]string{
		"display name": productidentity.DisplayName,
		"publisher":    productidentity.Publisher,
		"product URL":  productidentity.ProductURL,
		"copyright":    productidentity.Copyright,
	} {
		if value == "" || strings.TrimSpace(value) != value || len(value) > 200 || strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("installer %s is empty, malformed, or too long", role)
		}
	}
	if strings.Contains(productidentity.DisplayName, "&") {
		return errors.New("installer display name must not contain an unescaped ampersand")
	}
	seen := map[string]string{}
	for _, name := range installerOwnedFiles() {
		key := canonicalInstallerLeaf(name)
		if previous, exists := seen[key]; exists {
			return fmt.Errorf("installer file names collide case-insensitively: %q and %q", previous, name)
		}
		seen[key] = name
	}
	expectedOutput := productidentity.ApplicationName + "_" + version.Tag + "_windows_amd64_setup.exe"
	if filepath.Base(outputPath) != expectedOutput {
		return fmt.Errorf("installer output filename = %q, want %q", filepath.Base(outputPath), expectedOutput)
	}
	if version.Display != strings.TrimPrefix(version.Tag, "v") || version.Fixed != version.Display+".0" {
		return fmt.Errorf("installer version representations disagree: %#v", version)
	}
	return nil
}

func buildNSISInstaller(ctx context.Context, version releaseVersion, stageDirectory, outputPath string, stdout, stderr io.Writer) error {
	if err := validateReleasePackage(stageDirectory); err != nil {
		return err
	}
	if err := validateInstallerBuildInputs(version, outputPath); err != nil {
		return err
	}
	makensis, err := findMakeNSIS()
	if err != nil {
		return err
	}
	versionOutput, err := hiddenCommandContext(ctx, makensis, "/VERSION").CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect NSIS compiler version: %w: %s", err, strings.TrimSpace(string(versionOutput)))
	}
	if actual := strings.TrimSpace(string(versionOutput)); actual != "v"+installerEngineVersion {
		return fmt.Errorf("NSIS compiler version = %q, want v%s", actual, installerEngineVersion)
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
	return "", fmt.Errorf("NSIS v%s makensis.exe was not found; set MAKENSIS or install the pinned compiler", installerEngineVersion)
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
