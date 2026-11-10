package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"herdr-sandbox/internal/installerartifact"
	"herdr-sandbox/internal/productidentity"
	"herdr-sandbox/internal/sandbox"
)

const (
	workstationInstallerName = productidentity.WorkstationInstallerName
	workstationEvidenceName  = productidentity.WorkstationEvidenceName
)

func nativeInstallerSandbox(ctx context.Context, stdout, stderr io.Writer) error {
	if runtime.GOOS != "windows" || sandbox.IsCurrentWindowsSandbox() {
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
	if err := buildWithIdentity(ctx, evidence.Identity, stdout, stderr); err != nil {
		return err
	}
	installer, err := filepath.Abs(filepath.Join("build", "dist", workstationInstallerName))
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Fresh installer-only Sandbox acceptance. Existing guests are preserved; no automatic down or repair provisioning.")
	return runCommand(ctx, stdout, stderr, filepath.Join("build", "bin", productidentity.ExecutableName), "up", "--provisioning", "installer", "--installer", installer, "--no-attach")
}

// Developer packaging builds the application, then uses the same saved-profile
// NSIS builder shipped in that application. Product use needs neither Go nor Git.
func packageWorkstation(ctx context.Context, tag string, stdout, stderr io.Writer) (resultErr error) {
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
	if err := buildWithIdentity(ctx, identity, stdout, stderr); err != nil {
		return err
	}
	private, err := os.MkdirTemp("", "herdr-workstation-capture-")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, removeGeneratedPath(private)) }()
	profile := filepath.Join(private, "profile.json")
	if err := runCommand(ctx, stdout, stderr, filepath.Join("build", "bin", productidentity.ExecutableName), "workstation", "export", "--output", profile, "--include-settings"); err != nil {
		return err
	}
	payload, err := filepath.Abs(filepath.Join("build", "bin"))
	if err != nil {
		return err
	}
	source := "current-windows-user"
	if sandbox.IsCurrentWindowsSandbox() {
		source = "current-sandbox-user"
	}
	current, err := cleanPackageSource(ctx)
	if err != nil {
		return err
	}
	if current != revision {
		return errors.New("source changed during workstation packaging")
	}
	evidence, err := sandbox.BuildWorkstationInstaller(ctx, sandbox.WorkstationBuildOptions{Profile: profile, PayloadDirectory: payload, OutputDirectory: filepath.Join("build", "dist"), ConfigurationSource: source, Identity: identity, Output: stdout, Errors: stderr})
	if err != nil {
		return err
	}
	return writeArtifactEvidenceWithHash(stdout, "workstation-installer", filepath.Join("build", "dist", workstationInstallerName), evidence.InstallerSHA256)
}
