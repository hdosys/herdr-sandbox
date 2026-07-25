package sandbox

import (
	"strings"
	"testing"
)

func TestLoadHerdrReleaseUsesHerdrWin(t *testing.T) {
	release, err := loadHerdrRelease()
	if err != nil {
		t.Fatalf("loadHerdrRelease: %v", err)
	}
	if !strings.Contains(release.ArchiveURL, "github.com/hdosys/herdr-win/releases/") {
		t.Fatalf("archive URL = %q", release.ArchiveURL)
	}
	if release.Protocol < 1 || release.Version == "" {
		t.Fatalf("release = %#v", release)
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

func TestHerdrReleaseRejectsUnpinnedVCRuntime(t *testing.T) {
	release, err := loadHerdrRelease()
	if err != nil {
		t.Fatal(err)
	}
	release.VCRuntimeURL = "https://aka.ms/vc14/vc_redist.x64.exe"
	if err := release.validate(); err == nil {
		t.Fatal("validate unexpectedly accepted a mutable VC++ runtime URL")
	}
}

func TestHerdrReleaseRejectsMismatchedWinGetAssets(t *testing.T) {
	release, err := loadHerdrRelease()
	if err != nil {
		t.Fatal(err)
	}
	release.WinGetDependenciesURL = strings.Replace(release.WinGetDependenciesURL, release.WinGetVersion, "v0.0.0", 1)
	if err := release.validate(); err == nil {
		t.Fatal("validate unexpectedly accepted mismatched WinGet assets")
	}
}

func TestHerdrReleaseRejectsMismatchedOpenSSHAsset(t *testing.T) {
	release, err := loadHerdrRelease()
	if err != nil {
		t.Fatal(err)
	}
	release.OpenSSHMSIURL = strings.Replace(release.OpenSSHMSIURL, release.OpenSSHVersion, "other", 1)
	if err := release.validate(); err == nil {
		t.Fatal("validate unexpectedly accepted a mismatched OpenSSH asset")
	}
}

func TestHerdrReleaseRejectsOfficialUpstream(t *testing.T) {
	release, err := loadHerdrRelease()
	if err != nil {
		t.Fatal(err)
	}
	release.ArchiveURL = strings.Replace(release.ArchiveURL, "hdosys", "ogulcancelik", 1)
	if err := release.validate(); err == nil {
		t.Fatal("validate unexpectedly accepted the official upstream archive")
	}
}
