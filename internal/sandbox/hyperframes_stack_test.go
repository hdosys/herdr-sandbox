package sandbox

import (
	"regexp"
	"strings"
	"testing"
)

func TestHyperFramesStackOwnsLatestGlobalAuthoringAndSoftwareRenderEnvironment(t *testing.T) {
	source := readDefaultStackProvisioning(t)
	start := strings.Index(source, "function Assert-StackHyperFramesSoftwareEncode")
	end := strings.Index(source, "function Get-TradingViewDesktopPortableMetadata")
	if start < 0 || end <= start {
		t.Fatal("HyperFrames stack owner is missing")
	}
	section := source[start:end]
	for _, required := range []string{
		"function Install-HyperFramesStack",
		"Get-ProvisioningToolVersion -Tool 'OpenJS.NodeJS.LTS'",
		"Get-ProvisioningToolVersion -Tool 'Gyan.FFmpeg'",
		"Get-ProvisioningToolVersion -Tool 'hyperframes'",
		"Install-NodeRuntime -Version $NodeVersion",
		"HyperFrames requires Node.js 22 or newer",
		"-Id 'Gyan.FFmpeg'",
		"-InstallerType 'zip'",
		"full_build.zip",
		"-Adapter 'Portable' -ExecutableName 'ffmpeg.exe'",
		"hyperframes@latest",
		"\"hyperframes@$Version\"",
		"C:\\HerdrSandbox\\tools\\hyperframes",
		"hyperframes.cmd",
		"'browser', 'ensure'",
		"chrome-headless-shell.exe",
		"HyperFrames managed browser launch check",
		"'skills'",
		"'skills', 'check', '--json'",
		".config\\opencode\\skills",
		".claude\\skills",
		".codex\\skills",
		".copilot\\skills",
		".pi\\agent\\skills",
		".agents\\skills",
		"'doctor', '--json'",
		"'Node.js', 'FFmpeg', 'FFprobe', 'Chrome'",
		"'libx264'",
		"software encoding",
		"FFmpeg hardware encoding is not claimed",
	} {
		if !strings.Contains(section, required) {
			t.Errorf("HyperFrames stack is missing %q", required)
		}
	}
	if pin := regexp.MustCompile(`hyperframes@\d+\.\d+\.\d+`).FindString(section); pin != "" {
		t.Errorf("HyperFrames stack contains an agent-selected default pin %q", pin)
	}
	for _, forbidden := range []string{"h264_amf", "h264_qsv", "h264_nvenc", "nvcuda.dll", "amfrt64.dll"} {
		if strings.Contains(section, forbidden) {
			t.Errorf("HyperFrames stack contains forbidden pin or hardware-encoder promise %q", forbidden)
		}
	}
	if got := effectiveStackPackageOwner(stackHyperFrames); got != "OpenJS.NodeJS.LTS + Gyan.FFmpeg full + hyperframes@latest + global HyperFrames skills" {
		t.Fatalf("HyperFrames package owner = %q", got)
	}
	if !projectStackOwnsPackage(packageFFmpeg) {
		t.Fatal("Gyan.FFmpeg is not reserved to the project stack owner")
	}
}
