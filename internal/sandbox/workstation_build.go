package sandbox

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"herdr-sandbox/internal/installerartifact"
	"herdr-sandbox/internal/productidentity"
)

//go:embed assets/workstation.nsi
var workstationInstallerScript []byte

var workstationNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func IsCurrentWindowsSandbox() bool {
	return runtime.GOOS == "windows" && strings.EqualFold(os.Getenv("USERNAME"), "WDAGUtilityAccount")
}

// WorkstationInstallationDirectory owns saved personal installations, outside
// disposable Sandbox run data and application uninstall/configuration cleanup.
func WorkstationInstallationDirectory(name string) (string, error) {
	if !workstationNamePattern.MatchString(name) {
		return "", errors.New("installation name must contain 1-64 letters, digits, hyphens or underscores")
	}
	local := os.Getenv("LOCALAPPDATA")
	if !filepath.IsAbs(local) {
		return "", errors.New("LOCALAPPDATA must be absolute")
	}
	path := filepath.Join(local, "Herdr Installations", name)
	return path, validateWorkstationLocalPath(path)
}

func CreateWorkstationInstallation(ctx context.Context, name string, output io.Writer) (result string, resultErr error) {
	directory, err := WorkstationInstallationDirectory(name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(directory), 0o700); err != nil {
		return "", err
	}
	if _, err := os.Lstat(directory); err == nil {
		return "", errors.New("installation already exists; choose a new name")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	staging, err := os.MkdirTemp(filepath.Dir(directory), ".installation-")
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, os.RemoveAll(staging)) }()
	profile := filepath.Join(staging, "profile.json")
	if err := ExportWorkstationProfile(ctx, profile, true, false, output); err != nil {
		return "", err
	}
	selection, data, err := readWorkstationProfile(profile)
	if err != nil {
		return "", err
	}
	defer clear(data)
	defer func() {
		for _, file := range selection.Files {
			clear(file.Data)
		}
	}()
	if len(selection.Files) == 0 {
		return "", errors.New("no selected personal configuration was found; no installation was created")
	}
	if err := os.Rename(staging, directory); err != nil {
		return "", fmt.Errorf("publish new installation without replacing existing data: %w", err)
	}
	fmt.Fprintf(output, "Installation saved privately: %s\nNext: sandbox workstation build %s\n", directory, name)
	return directory, nil
}

type WorkstationBuildOptions struct {
	Profile, PayloadDirectory, OutputDirectory, ConfigurationSource string
	Identity                                                        installerartifact.Identity
	Output, Errors                                                  io.Writer
}

// BuildWorkstationInstaller is the one NSIS build used by the installed product
// and source packaging. It consumes a saved snapshot, not live settings or Git.
func BuildWorkstationInstaller(ctx context.Context, options WorkstationBuildOptions) (result installerartifact.Evidence, resultErr error) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return result, errors.New("personal installer creation requires Windows x64")
	}
	if options.Output == nil {
		options.Output = io.Discard
	}
	if options.Errors == nil {
		options.Errors = io.Discard
	}
	profile, data, err := readWorkstationProfile(options.Profile)
	if err != nil {
		return result, err
	}
	defer clear(data)
	defer func() {
		for _, file := range profile.Files {
			clear(file.Data)
		}
	}()
	if len(profile.Files) == 0 {
		return result, errors.New("personal installation contains no selected configuration")
	}
	output, err := filepath.Abs(options.OutputDirectory)
	if err != nil {
		return result, err
	}
	if err := validateWorkstationLocalPath(output); err != nil {
		return result, err
	}
	compiler, err := installerartifact.FindNSIS()
	if err != nil {
		return result, err
	}
	result = installerartifact.Evidence{Product: productidentity.WorkstationApplicationName, Identity: options.Identity, ProfileSHA256: fmt.Sprintf("%x", sha256.Sum256(data)), ConfigurationFiles: len(profile.Files), ConfigurationSource: options.ConfigurationSource, PayloadSHA256: map[string]string{}}
	for _, entry := range profile.Packages {
		result.PackageIDs = append(result.PackageIDs, entry.ID)
	}
	stage, err := os.MkdirTemp("", "herdr-workstation-profile-")
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, os.RemoveAll(stage)) }()
	for _, name := range []string{productidentity.ExecutableName, productidentity.BaseScriptName, productidentity.StackScriptName, productidentity.LicenseName} {
		source := filepath.Join(options.PayloadDirectory, name)
		if err := validateWorkstationLocalPath(source); err != nil {
			return result, err
		}
		contents, found, err := readBoundedRegularFile(source, 512*1024*1024)
		if err != nil {
			return result, err
		}
		if !found {
			return result, fmt.Errorf("installed build payload is missing: %s", name)
		}
		if err := os.WriteFile(filepath.Join(stage, name), contents, 0o600); err != nil {
			return result, err
		}
		result.PayloadSHA256[name] = fmt.Sprintf("%x", sha256.Sum256(contents))
	}
	if err := os.WriteFile(filepath.Join(stage, "personal-profile.json"), data, 0o600); err != nil {
		return result, err
	}
	script := filepath.Join(stage, "workstation.nsi")
	if err := os.WriteFile(script, workstationInstallerScript, 0o600); err != nil {
		return result, err
	}
	// Validate source identity before invoking a compiler. The executable digest
	// is filled from the real output after compilation.
	result.InstallerSHA256 = strings.Repeat("0", 64)
	encoded, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if _, err := installerartifact.Decode(encoded); err != nil {
		return result, fmt.Errorf("installed builder identity: %w", err)
	}
	if err := os.MkdirAll(output, 0o700); err != nil {
		return result, err
	}
	temporary, err := os.MkdirTemp(output, ".installer-build-")
	if err != nil {
		return result, err
	}
	retainPublicationFiles := false
	defer func() {
		if !retainPublicationFiles {
			resultErr = errors.Join(resultErr, os.RemoveAll(temporary))
		}
	}()
	installer := filepath.Join(temporary, productidentity.WorkstationInstallerName)
	materializedData, materializedDigest, err := prepareWorkstationInputs(ctx, stage, temporary, profile, options.Output)
	if err != nil {
		return result, err
	}
	result.DataFileName = filepath.Base(materializedData)
	result.DataSHA256 = materializedDigest
	buildContext, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	command := hiddenCommandContext(buildContext, compiler, "/WX", "/V2", "/NOCONFIG", "/DPACKAGE_DIR="+stage, "/DOUTPUT_FILE="+installer, "/DFIXED_VERSION="+options.Identity.Version+".0", "/DBUILD_FRESHNESS="+options.Identity.Freshness, "/DAPP_DISPLAY_NAME="+productidentity.WorkstationDisplayName, "/DAPP_APPLICATION_NAME="+productidentity.WorkstationApplicationName, "/DINPUTS_FILENAME="+result.DataFileName, script)
	command.Stdout, command.Stderr = options.Output, options.Errors
	fmt.Fprintln(options.Output, "Building the private installer with verified materialized data, without recompression.")
	if err := command.Run(); err != nil {
		return result, fmt.Errorf("compile personal installer: %w", err)
	}
	if err := installerartifact.VerifyPE(installer); err != nil {
		return result, err
	}
	result.InstallerSHA256, err = workstationFileDigest(installer)
	if err != nil {
		return result, err
	}
	encoded, err = json.MarshalIndent(result, "", "  ")
	if err != nil {
		return result, err
	}
	evidenceFile := filepath.Join(temporary, productidentity.WorkstationEvidenceName)
	if err := os.WriteFile(evidenceFile, encoded, 0o600); err != nil {
		return result, err
	}
	lockContext, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	release, err := acquireNamedLifecycleLock(lockContext, fmt.Sprintf(`Local\HerdrWorkstationArtifact-%x`, sha256.Sum256([]byte(strings.ToLower(output)))))
	if err != nil {
		return result, err
	}
	defer release()
	canonical := filepath.Join(output, productidentity.WorkstationInstallerName)
	canonicalEvidence := filepath.Join(output, productidentity.WorkstationEvidenceName)
	hasPreviousData := false
	if _, err := os.Lstat(canonical); err == nil {
		old, err := installerartifact.Read(canonicalEvidence)
		if err != nil {
			return result, fmt.Errorf("preserve existing installer without valid provenance: %w", err)
		}
		actual, err := workstationFileDigest(canonical)
		if err != nil {
			return result, err
		}
		if old.Product != result.Product || actual != old.InstallerSHA256 {
			return result, errors.New("existing installer changed or has another owner; preserved")
		}
		if old.DataFileName != "" {
			hasPreviousData = true
			actualData, err := workstationInputDigest(filepath.Join(output, old.DataFileName))
			if err != nil || actualData != old.DataSHA256 {
				return result, errors.New("existing materialized data changed; previous installer preserved")
			}
		}
		if old.Identity.Freshness > result.Identity.Freshness {
			return result, errors.New("a newer installer is already present; preserved")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	} else if _, err := os.Lstat(canonicalEvidence); err == nil {
		return result, errors.New("orphan installer provenance was preserved")
	}
	if !hasPreviousData {
		if _, err := os.Lstat(filepath.Join(output, result.DataFileName)); err == nil {
			return result, errors.New("unowned materialized installer data was preserved")
		} else if !errors.Is(err, os.ErrNotExist) {
			return result, err
		}
	}
	retainPublicationFiles, err = publishWorkstationSet(installer, evidenceFile, materializedData, canonical, canonicalEvidence, filepath.Join(output, result.DataFileName))
	if err != nil {
		return result, err
	}
	fmt.Fprintf(options.Output, "PRIVATE installer: %s\nSHA-256: %s\nRequired data: %s\nData SHA-256: %s\nFor installation, keep only the EXE and data ZIP together and private. JSON provenance is for development and Sandbox testing only.\n", canonical, result.InstallerSHA256, filepath.Join(output, result.DataFileName), result.DataSHA256)
	return result, nil
}

func publishWorkstationSet(installer, evidence, data, canonical, canonicalEvidence, canonicalData string) (bool, error) {
	backup := filepath.Join(filepath.Dir(data), "previous-data.zip")
	_, err := os.Lstat(canonicalData)
	hadData := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if hadData {
		if err := os.Rename(canonicalData, backup); err != nil {
			return false, err
		}
	}
	restore := func() error {
		if hadData {
			return os.Rename(backup, canonicalData)
		}
		if err := os.Remove(canonicalData); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.Rename(data, canonicalData); err != nil {
		restoreErr := restore()
		return restoreErr != nil, errors.Join(err, restoreErr)
	}
	retain, publishErr := publishWorkstationPair(installer, evidence, canonical, canonicalEvidence)
	if publishErr != nil {
		restoreErr := restore()
		return retain || restoreErr != nil, errors.Join(publishErr, restoreErr)
	}
	return false, nil
}

// Publish provenance first so a locked/read-only sidecar cannot destroy the
// previous executable. If executable publication fails, restore the old sidecar.
// The caller holds the per-output lock. Retain recovery inputs if rollback itself
// fails; never report an unmatched pair as usable or delete its last provenance.
func publishWorkstationPair(installer, evidence, canonical, canonicalEvidence string) (retain bool, resultErr error) {
	previous, found, err := readBoundedRegularFile(canonicalEvidence, 16*1024)
	if err != nil {
		return false, err
	}
	backup := filepath.Join(filepath.Dir(evidence), "previous-provenance.json")
	if found {
		if err := os.WriteFile(backup, previous, 0o600); err != nil {
			return false, err
		}
	}
	if err := os.Rename(evidence, canonicalEvidence); err != nil {
		return false, fmt.Errorf("publish provenance; previous installer preserved: %w", err)
	}
	if err := os.Rename(installer, canonical); err != nil {
		var restoreErr error
		if found {
			restoreErr = os.Rename(backup, canonicalEvidence)
		} else {
			restoreErr = os.Remove(canonicalEvidence)
		}
		if restoreErr != nil {
			return true, errors.Join(err, fmt.Errorf("restore provenance failed; publication files retained at %s: %w", filepath.Dir(evidence), restoreErr))
		}
		return false, fmt.Errorf("publish installer; previous pair restored: %w", err)
	}
	return false, nil
}

func workstationFileDigest(path string) (string, error) {
	if err := validateWorkstationLocalPath(path); err != nil {
		return "", err
	}
	data, found, err := readBoundedRegularFile(path, 512*1024*1024)
	if err != nil {
		return "", err
	}
	if !found {
		return "", os.ErrNotExist
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func BuildSavedWorkstation(ctx context.Context, name string, output, stderr io.Writer) (string, error) {
	directory, err := WorkstationInstallationDirectory(name)
	if err != nil {
		return "", err
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	_, err = BuildWorkstationInstaller(ctx, WorkstationBuildOptions{Profile: filepath.Join(directory, "profile.json"), PayloadDirectory: filepath.Dir(executable), OutputDirectory: directory, ConfigurationSource: "saved-installation", Identity: installerartifact.Identity{Version: productidentity.Version, Revision: productidentity.Revision, Freshness: productidentity.FormatBuildFreshness(time.Now())}, Output: output, Errors: stderr})
	if err != nil {
		return "", err
	}
	fmt.Fprintf(output, "Next on the Windows host: sandbox workstation test %s\n", name)
	return filepath.Join(directory, productidentity.WorkstationInstallerName), nil
}

func SavedWorkstationInstaller(name string) (string, error) {
	directory, err := WorkstationInstallationDirectory(name)
	if err != nil {
		return "", err
	}
	evidence, err := installerartifact.Read(filepath.Join(directory, productidentity.WorkstationEvidenceName))
	if err != nil {
		return "", fmt.Errorf("build this installation first: %w", err)
	}
	profileHash, err := workstationFileDigest(filepath.Join(directory, "profile.json"))
	if err != nil {
		return "", err
	}
	if evidence.Product != productidentity.WorkstationApplicationName || profileHash != evidence.ProfileSHA256 {
		return "", errors.New("saved installation and built installer differ; rebuild before testing")
	}
	installer := filepath.Join(directory, productidentity.WorkstationInstallerName)
	actual, err := workstationFileDigest(installer)
	if err != nil {
		return "", err
	}
	if actual != evidence.InstallerSHA256 {
		return "", errors.New("built installer changed; preserved and not tested")
	}
	return installer, nil
}
