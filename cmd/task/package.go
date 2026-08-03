package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"debug/pe"
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
	Tag     string
	Display string
	Fixed   string
}

type releasePackagePaths struct {
	Stage             string
	ZIP               string
	ZIPChecksum       string
	Installer         string
	InstallerChecksum string
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
		Stage:             paths.Stage,
		ZIP:               filepath.Join(temporaryDirectory, filepath.Base(paths.ZIP)),
		ZIPChecksum:       filepath.Join(temporaryDirectory, filepath.Base(paths.ZIPChecksum)),
		Installer:         filepath.Join(temporaryDirectory, filepath.Base(paths.Installer)),
		InstallerChecksum: filepath.Join(temporaryDirectory, filepath.Base(paths.InstallerChecksum)),
	}
	if err := writeReleaseZIP(generated.Stage, generated.ZIP); err != nil {
		return err
	}
	if err := writeSHA256File(generated.ZIP, generated.ZIPChecksum); err != nil {
		return err
	}
	if err := buildNSISInstaller(ctx, version, generated.Stage, generated.Installer, stdout, stderr); err != nil {
		return err
	}
	if err := writeSHA256File(generated.Installer, generated.InstallerChecksum); err != nil {
		return err
	}
	if err := verifyReleaseArtifactSet(generated); err != nil {
		return err
	}
	if err := publishReleaseArtifactSet(generated, paths); err != nil {
		return err
	}
	for _, path := range []string{paths.ZIP, paths.ZIPChecksum, paths.Installer, paths.InstallerChecksum} {
		fmt.Fprintf(stdout, "Release artifact: %s\n", filepath.Clean(path))
	}
	return nil
}

func releaseOutputPaths(paths releasePackagePaths) []string {
	return []string{paths.ZIP, paths.ZIPChecksum, paths.Installer, paths.InstallerChecksum}
}

func verifyReleaseArtifactSet(paths releasePackagePaths) error {
	for _, pair := range [][2]string{
		{paths.ZIP, paths.ZIPChecksum},
		{paths.Installer, paths.InstallerChecksum},
	} {
		artifact, checksum := pair[0], pair[1]
		info, err := os.Stat(artifact)
		if err != nil {
			return fmt.Errorf("inspect generated release artifact: %w", err)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("generated release artifact must be a nonempty regular file: %s", artifact)
		}
		hash, err := fileSHA256(artifact)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(checksum)
		if err != nil {
			return fmt.Errorf("read generated release checksum: %w", err)
		}
		expected := fmt.Sprintf("%x  %s\n", hash, filepath.Base(artifact))
		if string(data) != expected {
			return fmt.Errorf("generated release checksum is invalid: %s", checksum)
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
		Stage:             filepath.Join(root, "build", "package", baseName),
		ZIP:               zipPath,
		ZIPChecksum:       zipPath + ".sha256",
		Installer:         installerPath,
		InstallerChecksum: installerPath + ".sha256",
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
		header := &zip.FileHeader{Name: file.Name, Method: zip.Deflate}
		header.SetModTime(modTime)
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

func writeSHA256File(artifactPath, outputPath string) error {
	hash, err := fileSHA256(artifactPath)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("%x  %s\n", hash, filepath.Base(artifactPath))
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create checksum output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(outputPath), ".checksum-*.tmp")
	if err != nil {
		return fmt.Errorf("create release checksum: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if _, err := io.WriteString(temporary, line); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write release checksum: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close release checksum: %w", err)
	}
	if err := replaceGeneratedFile(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("publish release checksum: %w", err)
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

func buildNSISInstaller(ctx context.Context, version releaseVersion, stageDirectory, outputPath string, stdout, stderr io.Writer) error {
	if err := validateReleasePackage(stageDirectory); err != nil {
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
	script, err := filepath.Abs(filepath.Join("packaging", "windows", "installer.nsi"))
	if err != nil {
		return fmt.Errorf("resolve installer source: %w", err)
	}
	if err := os.Remove(outputPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove previous installer output: %w", err)
	}
	args := []string{
		"/V2",
		"/NOCONFIG",
		"/DRELEASE_TAG=" + version.Tag,
		"/DVERSION=" + version.Display,
		"/DFIXED_VERSION=" + version.Fixed,
		"/DAPP_NAME=" + productidentity.ApplicationName,
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
		"/DAPP_UNINSTALL_KEY=" + productidentity.UninstallKeyName,
		"/DAPP_COPYRIGHT=" + productidentity.Copyright,
		"/DPACKAGE_DIR=" + stageDirectory,
		"/DPATH_HELPER=" + pathHelper,
		"/DOUTPUT_FILE=" + outputPath,
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
