package sandbox

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

// Validate the same selected effects for either archive transport. Provider
// expectations come from the already-bound archive, never from success wording.
func verifyDevelopmentConfigurationResult(data []byte, actual developmentConfigurationSyncResult, localTarget, tradingViewEnabled bool) error {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return errors.New("configuration expectation archive is invalid")
	}
	read := func(name string) ([]byte, error) {
		entry, err := archive.Open(name)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		defer entry.Close()
		return io.ReadAll(io.LimitReader(entry, maximumConfigurationFileSize+1))
	}
	planData, err := read(configurationPackagePlanArchivePath)
	if err != nil {
		return err
	}
	var plan wingetPackagePlan
	if json.Unmarshal(planData, &plan) != nil || plan.SchemaVersion != 1 {
		return errors.New("configuration expectation package plan is invalid")
	}
	manifestData, err := read(codingAgentSyncManifestArchivePath)
	if err != nil {
		return err
	}
	var manifest codingAgentSyncManifest
	if json.Unmarshal(manifestData, &manifest) != nil || manifest.SchemaVersion != 5 {
		return errors.New("configuration expectation credential selection is invalid")
	}
	count, err := configurationArchivePayloadFileCount(data)
	if err != nil {
		return err
	}
	terminal, err := read(windowsTerminalEditionArchivePath)
	if err != nil {
		return err
	}
	preset, err := read(starshipPresetArchivePath)
	if err != nil {
		return err
	}
	expected := developmentConfigurationSyncResult{
		SchemaVersion: 9, ArchiveSHA256: fmt.Sprintf("%x", sha256.Sum256(data)), CopiedFiles: count,
		WindowsTerminalEdition: strings.TrimSpace(string(terminal)), StarshipPreset: strings.TrimSpace(string(preset)),
		StarshipConfigured: plan.enabled(packageStarship), HerdrConfigurationPublished: true,
		GitHubAuthenticationVerified: plan.enabled(packageGitHubCLI) && manifest.CredentialSync.GitHubCLI,
	}
	githubData, err := read(githubCLIAuthenticationArchivePath)
	if err != nil {
		return err
	}
	defer clear(githubData)
	if githubData != nil {
		authentication, err := decodeGitHubCLIAuthentication(githubData)
		if err != nil {
			return errors.New("captured GitHub authentication is invalid")
		}
		expected.GitHubAuthenticatedAccounts = len(authentication.Accounts)
	}
	tradingViewData, err := read(tradingViewAuthenticationArchivePath)
	if err != nil {
		return err
	}
	defer clear(tradingViewData)
	if tradingViewData != nil {
		authentication, err := decodeTradingViewAuthentication(tradingViewData)
		if err != nil {
			return errors.New("captured TradingView authentication is invalid")
		}
		expected.TradingViewAuthenticatedCookies = len(authentication.Cookies)
		expected.TradingViewAuthenticationVerified = manifest.CredentialSync.TradingView
	}
	expected.OpenCodeTVControlMCPConfigured = tradingViewEnabled && (plan.enabled(packageOpenCode) || actual.OpenCodePermissionVerified)
	if tradingViewEnabled && localTarget && !plan.enabled(packageOpenCode) {
		// An existing, unselected OpenCode may also receive the selected MCP.
		expected.OpenCodeTVControlMCPConfigured = actual.OpenCodeTVControlMCPConfigured
	}
	if localTarget && actual.OpenCodePermissionVerified {
		return errors.New("local configuration unexpectedly applied Sandbox permission policy")
	}
	if !localTarget && plan.enabled(packageOpenCode) && !actual.OpenCodePermissionVerified {
		return errors.New("selected Sandbox OpenCode permissions were not verified")
	}
	expected.OpenCodePermissionVerified = actual.OpenCodePermissionVerified
	if actual != expected {
		return errors.New("configuration application did not match selected Terminal, Starship, Herdr or authentication expectations")
	}
	return nil
}
