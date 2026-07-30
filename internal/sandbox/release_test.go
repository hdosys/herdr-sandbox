package sandbox

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoadBootstrapReleaseExcludesHerdrRuntimePins(t *testing.T) {
	release, err := loadBootstrapRelease()
	if err != nil {
		t.Fatalf("loadBootstrapRelease: %v", err)
	}
	for _, forbidden := range [][]byte{[]byte(`"version"`), []byte(`"protocol"`), []byte(`"archiveUrl"`), []byte(`"archiveSha256"`), []byte("hdosys/herdr-win")} {
		if bytes.Contains(bootstrapReleaseJSON, forbidden) {
			t.Fatalf("bootstrap release metadata still contains Herdr runtime pin %q", forbidden)
		}
	}
	if !strings.Contains(release.VCRuntimeURL, "download.visualstudio.microsoft.com/download/pr/") {
		t.Fatalf("VC++ runtime URL = %q", release.VCRuntimeURL)
	}
	if release.WinGetVersion == "" || !strings.Contains(release.WinGetBundleURL, "/microsoft/winget-cli/releases/download/"+release.WinGetVersion+"/") {
		t.Fatalf("WinGet release = %#v", release)
	}
	if release.OpenSSHVersion == "" || !strings.Contains(release.OpenSSHMSIURL, "/PowerShell/Win32-OpenSSH/releases/download/"+release.OpenSSHVersion+"/") {
		t.Fatalf("OpenSSH release = %#v", release)
	}
}

func TestBootstrapReleaseRejectsUnpinnedVCRuntime(t *testing.T) {
	release, err := loadBootstrapRelease()
	if err != nil {
		t.Fatal(err)
	}
	release.VCRuntimeURL = "https://aka.ms/vc14/vc_redist.x64.exe"
	if err := release.validate(); err == nil {
		t.Fatal("validate unexpectedly accepted a mutable VC++ runtime URL")
	}
}

func TestBootstrapReleaseRejectsMismatchedWinGetAssets(t *testing.T) {
	release, err := loadBootstrapRelease()
	if err != nil {
		t.Fatal(err)
	}
	release.WinGetDependenciesURL = strings.Replace(release.WinGetDependenciesURL, release.WinGetVersion, "v0.0.0", 1)
	if err := release.validate(); err == nil {
		t.Fatal("validate unexpectedly accepted mismatched WinGet assets")
	}
}

func TestBootstrapReleaseRejectsMismatchedOpenSSHAsset(t *testing.T) {
	release, err := loadBootstrapRelease()
	if err != nil {
		t.Fatal(err)
	}
	release.OpenSSHMSIURL = strings.Replace(release.OpenSSHMSIURL, release.OpenSSHVersion, "other", 1)
	if err := release.validate(); err == nil {
		t.Fatal("validate unexpectedly accepted a mismatched OpenSSH asset")
	}
}
