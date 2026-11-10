package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"herdr-sandbox/internal/installerartifact"
	"herdr-sandbox/internal/productidentity"
	"herdr-sandbox/internal/sandbox"
)

const (
	workstationInstallerName = "herdr-workstation_setup.exe"
	workstationEvidenceName  = "herdr-workstation_setup.json"
)

func nativeInstallerSandbox(ctx context.Context, stdout, stderr io.Writer) error {
	if runtime.GOOS != "windows" || strings.EqualFold(os.Getenv("USERNAME"), "WDAGUtilityAccount") {
		return errors.New("native-installer-sandbox must run on the Windows host, not inside the running Sandbox; no guest was replaced")
	}
	evidence, err := installerartifact.Read(filepath.Join("build", "dist", workstationEvidenceName))
	if err != nil {
		return err
	}
	if evidence.Product != productidentity.WorkstationApplicationName {
		return errors.New("workstation candidate required")
	}
	if err := validateLocalInstallerSource(ctx, evidence.Identity.Revision); err != nil {
		return err
	}
	identity := buildIdentity{Version: evidence.Identity.Version, Revision: evidence.Identity.Revision, Freshness: evidence.Identity.Freshness}
	if err := buildWithIdentity(ctx, identity, stdout, stderr); err != nil {
		return err
	}
	installer, err := filepath.Abs(filepath.Join("build", "dist", workstationInstallerName))
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Fresh installer-only Sandbox acceptance. An existing Sandbox is preserved and blocks this command. No automatic down/recreation or normal-provisioning repair is performed.")
	return runCommand(ctx, stdout, stderr, filepath.Join("build", "bin", productidentity.ExecutableName), "up", "--provisioning", "installer", "--installer", installer, "--no-attach")
}

func packageWorkstation(ctx context.Context, tag string, stdout, stderr io.Writer) (resultErr error) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return errors.New("workstation packaging requires Windows x64")
	}
	version, err := parseReleaseVersion(tag)
	if err != nil {
		return err
	}
	revision, err := cleanPackageSource(ctx)
	if err != nil {
		return err
	}
	identity := buildIdentity{Version: version.Display, Revision: revision, Freshness: productidentity.FormatBuildFreshness(time.Now())}
	if err := checkPowerShell(ctx, stdout, stderr); err != nil {
		return err
	}
	retained := map[string]bool{canonicalLocalInstallerName: true, canonicalLocalEvidenceName: true, workstationInstallerName: true, workstationEvidenceName: true}
	if err := cleanBuildOutputs(".", retained); err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, cleanBuildOutputs(".", retained)) }()
	if err := os.MkdirAll("build", 0o755); err != nil {
		return err
	}
	// Personal settings must never become a source-tree staging file.
	temporary, err := os.MkdirTemp("", "herdr-workstation-package-")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, removeGeneratedPath(temporary)) }()
	paths := releasePackagePaths{Stage: filepath.Join(temporary, "stage"), Installer: filepath.Join(temporary, workstationInstallerName)}
	if err := buildWithIdentity(ctx, identity, stdout, stderr); err != nil {
		return err
	}
	if err := stageReleasePackage(filepath.Join("build", "bin"), paths.Stage); err != nil {
		return err
	}
	profilePath := filepath.Join(paths.Stage, "personal-profile.json")
	if err := runCommand(ctx, stdout, stderr, filepath.Join(paths.Stage, productidentity.ExecutableName), "workstation", "export", "--output", profilePath, "--include-settings"); err != nil {
		return err
	}
	profileData, err := os.ReadFile(profilePath)
	if err != nil {
		return err
	}
	defer clear(profileData)
	var profile sandbox.WorkstationProfile
	if err := json.Unmarshal(profileData, &profile); err != nil {
		return err
	}
	defer func() {
		for _, file := range profile.Files {
			clear(file.Data)
		}
	}()
	if len(profile.Files) == 0 {
		return errors.New("no selected personal configuration was found; refusing to label an empty installer as personalized")
	}
	compiler, err := findMakeNSIS()
	if err != nil {
		return err
	}
	stage, err := filepath.Abs(paths.Stage)
	if err != nil {
		return err
	}
	output, err := filepath.Abs(paths.Installer)
	if err != nil {
		return err
	}
	script, err := filepath.Abs(filepath.Join("packaging", "windows", "workstation.nsi"))
	if err != nil {
		return err
	}
	arguments := []string{"/WX", "/V2", "/NOCONFIG", "/DPACKAGE_DIR=" + stage, "/DOUTPUT_FILE=" + output,
		"/DFIXED_VERSION=" + version.Fixed, "/DBUILD_FRESHNESS=" + identity.Freshness,
		"/DAPP_DISPLAY_NAME=" + productidentity.WorkstationDisplayName,
		"/DAPP_APPLICATION_NAME=" + productidentity.WorkstationApplicationName, script}
	if err := runCommand(ctx, stdout, stderr, compiler, arguments...); err != nil {
		return err
	}
	if err := verifyInstallerArtifact(paths.Installer); err != nil {
		return err
	}
	current, err := cleanPackageSource(ctx)
	if err != nil {
		return err
	}
	if current != revision {
		return errors.New("source changed during workstation packaging")
	}
	evidence, err := captureLocalInstallerEvidence(identity, paths)
	if err != nil {
		return err
	}
	evidence.Product = productidentity.WorkstationApplicationName
	evidence.ProfileSHA256 = fmt.Sprintf("%x", sha256.Sum256(profileData))
	evidence.ConfigurationFiles = len(profile.Files)
	evidence.ConfigurationSource = "current-windows-user"
	if strings.EqualFold(os.Getenv("USERNAME"), "WDAGUtilityAccount") {
		evidence.ConfigurationSource = "current-sandbox-user"
	}
	for _, entry := range profile.Packages {
		evidence.PackageIDs = append(evidence.PackageIDs, entry.ID)
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	evidencePath := filepath.Join(temporary, workstationEvidenceName)
	if err := os.WriteFile(evidencePath, append(data, '\n'), 0o600); err != nil {
		return err
	}
	directory := filepath.Join("build", "dist")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	canonical := filepath.Join(directory, workstationInstallerName)
	if err := replaceGeneratedFile(paths.Installer, canonical); err != nil {
		return err
	}
	if err := replaceGeneratedFile(evidencePath, filepath.Join(directory, workstationEvidenceName)); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "PRIVATE personalized setup: %d configuration files embedded (%s). Do not publish or upload this installer. Native acceptance pending.\n", evidence.ConfigurationFiles, evidence.ConfigurationSource)
	return writeArtifactEvidenceWithHash(stdout, "workstation-installer", canonical, evidence.InstallerSHA256)
}
