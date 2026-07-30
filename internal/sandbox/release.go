package sandbox

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

//go:embed assets/bootstrap-release.json
var bootstrapReleaseJSON []byte

type bootstrapRelease struct {
	SchemaVersion            int    `json:"schemaVersion"`
	VCRuntimeURL             string `json:"vcRuntimeUrl"`
	VCRuntimeSHA256          string `json:"vcRuntimeSha256"`
	WinGetVersion            string `json:"wingetVersion"`
	WinGetBundleURL          string `json:"wingetBundleUrl"`
	WinGetBundleSHA256       string `json:"wingetBundleSha256"`
	WinGetDependenciesURL    string `json:"wingetDependenciesUrl"`
	WinGetDependenciesSHA256 string `json:"wingetDependenciesSha256"`
	OpenSSHVersion           string `json:"openSSHVersion"`
	OpenSSHMSIURL            string `json:"openSSHMSIUrl"`
	OpenSSHMSISHA256         string `json:"openSSHMSISha256"`
}

func loadBootstrapRelease() (bootstrapRelease, error) {
	var release bootstrapRelease
	decoder := json.NewDecoder(bytes.NewReader(bootstrapReleaseJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&release); err != nil {
		return bootstrapRelease{}, fmt.Errorf("decode bootstrap release metadata: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return bootstrapRelease{}, errors.New("bootstrap release metadata contains trailing JSON")
	}
	if err := release.validate(); err != nil {
		return bootstrapRelease{}, fmt.Errorf("validate bootstrap release metadata: %w", err)
	}
	return release, nil
}

func (release bootstrapRelease) validate() error {
	if release.SchemaVersion != 1 {
		return fmt.Errorf("schemaVersion = %d, want 1", release.SchemaVersion)
	}
	parsedRuntimeURL, err := url.Parse(release.VCRuntimeURL)
	if err != nil {
		return fmt.Errorf("parse vcRuntimeUrl: %w", err)
	}
	if parsedRuntimeURL.Scheme != "https" || parsedRuntimeURL.Host != "download.visualstudio.microsoft.com" || !strings.HasPrefix(parsedRuntimeURL.Path, "/download/pr/") || !strings.HasSuffix(parsedRuntimeURL.Path, "/VC_redist.x64.exe") {
		return fmt.Errorf("vcRuntimeUrl %q is not an immutable Microsoft x64 VC++ runtime URL", release.VCRuntimeURL)
	}
	if err := validateSHA256("vcRuntimeSha256", release.VCRuntimeSHA256); err != nil {
		return err
	}
	if !strings.HasPrefix(release.WinGetVersion, "v") || strings.ContainsAny(release.WinGetVersion, "\r\n/") {
		return fmt.Errorf("invalid wingetVersion %q", release.WinGetVersion)
	}
	winGetReleasePrefix := "/microsoft/winget-cli/releases/download/" + release.WinGetVersion + "/"
	if err := validateReleaseURL("wingetBundleUrl", release.WinGetBundleURL, "github.com", winGetReleasePrefix, "Microsoft.DesktopAppInstaller_8wekyb3d8bbwe.msixbundle"); err != nil {
		return err
	}
	if err := validateSHA256("wingetBundleSha256", release.WinGetBundleSHA256); err != nil {
		return err
	}
	if err := validateReleaseURL("wingetDependenciesUrl", release.WinGetDependenciesURL, "github.com", winGetReleasePrefix, "DesktopAppInstaller_Dependencies.zip"); err != nil {
		return err
	}
	if err := validateSHA256("wingetDependenciesSha256", release.WinGetDependenciesSHA256); err != nil {
		return err
	}
	if release.OpenSSHVersion == "" || strings.ContainsAny(release.OpenSSHVersion, "\r\n/") {
		return fmt.Errorf("invalid openSSHVersion %q", release.OpenSSHVersion)
	}
	openSSHReleasePrefix := "/PowerShell/Win32-OpenSSH/releases/download/" + release.OpenSSHVersion + "/"
	if err := validateReleaseURL("openSSHMSIUrl", release.OpenSSHMSIURL, "github.com", openSSHReleasePrefix, "OpenSSH-Win64-v10.0.0.0.msi"); err != nil {
		return err
	}
	if err := validateSHA256("openSSHMSISha256", release.OpenSSHMSISHA256); err != nil {
		return err
	}
	return nil
}

func validateReleaseURL(name, rawURL, host, pathPrefix, fileName string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed.Scheme != "https" || parsed.Host != host || parsed.Path != pathPrefix+fileName {
		return fmt.Errorf("%s %q is not the expected pinned release asset", name, rawURL)
	}
	return nil
}

func validateSHA256(name, digest string) error {
	if digest != strings.ToLower(digest) || len(digest) != 64 {
		return fmt.Errorf("%s must be 64 lowercase hexadecimal characters", name)
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	return nil
}
