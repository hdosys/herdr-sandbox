package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestWorkstationDefaultProfileAndSavedSelectionRoundTrip(t *testing.T) {
	profile := DefaultWorkstationProfile()
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeWorkstationProfile(data)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(decoded.Packages, func(entry wingetPackagePlanEntry) bool { return entry.ID == "Microsoft.VCRedist.2015+.x64" }) {
		t.Fatal("VC++ prerequisite missing")
	}
	root := t.TempDir()
	first, _, problem, err := resolveWorkstationSelection(root, "")
	if err != nil || problem != "" {
		t.Fatalf("initial selection: %v %s", err, problem)
	}
	second, _, problem, err := resolveWorkstationSelection(root, "")
	if err != nil || problem != "" || len(first.Packages) != len(second.Packages) || len(first.Stacks) != len(second.Stacks) {
		t.Fatalf("resume selection: %v %s", err, problem)
	}
}

func TestWorkstationPrivateConfigurationRoundTripAndConflict(t *testing.T) {
	source := t.TempDir()
	nu := filepath.Join(source, "nushell")
	opencode := filepath.Join(source, "opencode")
	for _, directory := range []string{filepath.Join(nu, "scripts"), opencode, filepath.Join(opencode, ".git")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for name, contents := range map[string]string{
		filepath.Join(nu, "config.nu"):               "source scripts/helper.nu\n",
		filepath.Join(nu, "scripts", "helper.nu"):    "def hello [] { 'hello' }\n",
		filepath.Join(opencode, "opencode.json"):     "{\"theme\":\"system\"}\n",
		filepath.Join(opencode, "auth.json"):         "secret fixture must not export",
		filepath.Join(opencode, ".usage-status.env"): "private fixture must not export",
		filepath.Join(opencode, ".git", "config"):    "private history fixture must not export",
	} {
		if err := os.WriteFile(name, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := exportWorkstationSettings(map[string]string{"nushell": nu, "opencode": opencode})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("exported %d files, want exactly authored config plus nested Nu script", len(files))
	}
	target := t.TempDir()
	for _, file := range files {
		root := filepath.Join(target, file.Kind)
		if err := importWorkstationFile(root, file); err != nil {
			t.Fatal(err)
		}
		if err := importWorkstationFile(root, file); err != nil {
			t.Fatalf("unchanged retry: %v", err)
		}
		actual, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil || !bytes.Equal(actual, file.Data) {
			t.Fatalf("import differs: %v", err)
		}
	}
	file := files[0]
	destination := filepath.Join(target, file.Kind, filepath.FromSlash(file.Path))
	if err := os.WriteFile(destination, []byte("user change"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := importWorkstationFile(filepath.Join(target, file.Kind), file); err == nil {
		t.Fatal("different user configuration was overwritten")
	}
	actual, _ := os.ReadFile(destination)
	if string(actual) != "user change" {
		t.Fatal("user change lost")
	}
	file.Data = []byte("tampered")
	if err := importWorkstationFile(filepath.Join(target, file.Kind), file); err == nil {
		t.Fatal("hash mismatch accepted")
	}
}

func TestWorkstationProfileRejectsUnsafeFilesAndDuplicateInputs(t *testing.T) {
	profile := DefaultWorkstationProfile()
	for _, name := range []string{"../escape.json", "a/../../escape.json", "C:/escape.json", "a.json:stream", "NUL.json", "x./a.json", "auth.json", ".env", ".git/config"} {
		profile.Files = []WorkstationFile{{Kind: "opencode", Path: name, Data: []byte("{}"), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("{}")))}}
		data, _ := json.Marshal(profile)
		if _, err := decodeWorkstationProfile(data); err == nil {
			t.Errorf("accepted unsafe file %q", name)
		}
	}
	profile.Files = []WorkstationFile{}
	profile.Packages = append(profile.Packages, wingetPackagePlanEntry{ID: "HDOSYS.HERDR-WIN"})
	data, _ := json.Marshal(profile)
	if _, err := decodeWorkstationProfile(data); err == nil {
		t.Fatal("case-colliding package selections accepted")
	}
}

func TestWorkstationReportFailureDoesNotReturnOldSuccess(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows read-only file publication boundary")
	}
	root := t.TempDir()
	old := filepath.Join(root, "report.html")
	if err := os.WriteFile(old, []byte("old successful report"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(old, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(old, 0o600) })
	path, err := publishWorkstationReport(root, WorkstationReport{SchemaVersion: 1, Complete: false, Steps: []WorkstationStep{{"failed action", "failed", "fixture"}}}, io.Discard)
	if err == nil || path != "" {
		t.Fatalf("stale report offered as current: %q %v", path, err)
	}
}

func TestWorkstationSelectedManifestIntegrityBoundary(t *testing.T) {
	root := t.TempDir()
	payload := []byte("controlled installer fixture")
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	manifest := "PackageIdentifier: Example.Tool\nPackageVersion: '1.2.3'\nInstallers:\n- Architecture: x64\n  InstallerType: nullsoft\n  Scope: machine\n  InstallerUrl: https://example.com/tool.exe\n  InstallerSha256: " + hash + "\n"
	if err := os.WriteFile(filepath.Join(root, "payload.exe"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	selection := DownloadedPackageSelection{Directory: root, ID: "Example.Tool", Version: "1.2.3", Architecture: "x64", InstallerType: "nullsoft", Scope: "machine"}
	for _, test := range []struct {
		name, yaml string
		valid      bool
	}{
		{"valid", manifest, true},
		{"wrong identity", strings.Replace(manifest, "Example.Tool", "Other.Tool", 1), false},
		{"wrong hash", strings.Replace(manifest, hash, strings.Repeat("0", 64), 1), false},
		{"wrong architecture", strings.Replace(manifest, "Architecture: x64", "Architecture: x86", 1), false},
		{"duplicate field", manifest + "PackageIdentifier: Other.Tool\n", false},
		{"trailing document", manifest + "---\nPackageIdentifier: Other.Tool\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(root, "manifest.yaml"), []byte(test.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			metadata, err := inspectDownloadedPackage(selection)
			if (err == nil) != test.valid {
				t.Fatalf("manifest validity: %v", err)
			}
			if test.valid && metadata.SHA256 != strings.ToUpper(hash) {
				t.Fatal("download hash not bound to returned metadata")
			}
		})
	}
}

func TestWorkstationPlanIncludesHandyAndCompleteDeadline(t *testing.T) {
	t.Setenv(fastTestsEnvironment, "")
	root := t.TempDir()
	profile := DefaultWorkstationProfile()
	profile.Stacks = []string{"handy"}
	input := filepath.Join(root, "input")
	if err := os.Mkdir(input, 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(profile)
	for name, contents := range map[string][]byte{
		"profile.json": data, "workstation.ps1": workstationScript, "workstation-action.ps1": workstationActionScript,
		"workstation-winget.ps1": workstationWinGetScript, provisioningProcessName: provisioningProcessSource,
	} {
		if err := os.WriteFile(filepath.Join(input, name), contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	base, err := filepath.Abs(filepath.Join("..", "..", "provisioning", "base.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	stacks, err := filepath.Abs(filepath.Join("..", "..", "provisioning", "stacks.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	type actualPlan struct {
		SchemaVersion int `json:"schemaVersion"`
		Seconds       int `json:"seconds"`
		Steps         int `json:"steps"`
		Jobs          []struct {
			Name        string `json:"name"`
			Seconds     int    `json:"seconds"`
			Fingerprint string `json:"fingerprint"`
		} `json:"jobs"`
	}
	readPlan := func() actualPlan {
		t.Helper()
		command := hiddenCommandContext(ctx, mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(input, "workstation.ps1"), "-NativeProfilePath", filepath.Join(input, "profile.json"), "-NativeRoot", root, "-BasePath", base, "-StacksPath", stacks, "-RunnerPath", "not-executed.exe", "-PlanOnly")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("actual plan: %v: %s", err, output)
		}
		var plan actualPlan
		if err := json.Unmarshal(output, &plan); err != nil {
			t.Fatalf("plan JSON: %v: %s", err, output)
		}
		return plan
	}
	plan := readPlan()
	names := []string{}
	seconds := 120 + plan.Steps*30
	for _, job := range plan.Jobs {
		names = append(names, job.Name)
		seconds += job.Seconds
	}
	for _, name := range []string{"Vulkan SDK", "WebView2", "Handy native headers", "Rust MSVC", "C and C++", "Stack bun"} {
		if !slices.Contains(names, name) {
			t.Errorf("missing Handy prerequisite %s", name)
		}
	}
	if plan.SchemaVersion != 1 || len(plan.Jobs) != plan.Steps || plan.Seconds != seconds {
		t.Fatalf("deadline does not cover the whole actual plan: %+v", plan)
	}
	fingerprint := func(plan actualPlan, name string) string {
		t.Helper()
		for _, job := range plan.Jobs {
			if job.Name == name {
				return job.Fingerprint
			}
		}
		t.Fatalf("missing job %s", name)
		return ""
	}
	profile.Tools = []resolvedToolVersion{{Tool: "rust-toolchain", Version: "1.90.0", Source: toolVersionSourceExplicit, Owners: []string{"fixture"}}}
	data, _ = json.Marshal(profile)
	if err := os.WriteFile(filepath.Join(input, "profile.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	changed := readPlan()
	if fingerprint(plan, "Rust MSVC") == fingerprint(changed, "Rust MSVC") {
		t.Fatal("changed Rust toolchain retained the old fingerprint")
	}
	if fingerprint(plan, "Package Git.Git") != fingerprint(changed, "Package Git.Git") {
		t.Fatal("unrelated package was invalidated by a Rust selection")
	}
	state := `{"schemaVersion":1,"steps":[{"name":"Visual Studio Build Tools","fingerprint":"` + strings.Repeat("a", 64) + `"}]}`
	if err := os.WriteFile(filepath.Join(root, "state", "steps.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	dependencyChanged := readPlan()
	if fingerprint(changed, "C and C++") == fingerprint(dependencyChanged, "C and C++") {
		t.Fatal("dependent C++ step retained stale prerequisite evidence")
	}
}
