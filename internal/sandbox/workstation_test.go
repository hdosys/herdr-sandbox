package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	settings := []byte("{\"theme\":\"system\"}")
	profile.Files = []WorkstationFile{{Kind: "opencode", Path: "opencode.json", Data: settings, SHA256: fmt.Sprintf("%x", sha256.Sum256(settings))}}
	data, err = json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "extracted-profile.json")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := resolveWorkstationSelection(root, input); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(input); err != nil {
		t.Fatal(err)
	}
	resumed, _, problem, err := resolveWorkstationSelection(root, "")
	if err != nil || problem != "" || len(resumed.Files) != 1 || !bytes.Equal(resumed.Files[0].Data, settings) {
		t.Fatal("embedded configuration cannot resume after extraction cleanup")
	}
}

func TestWorkstationPrivateConfigurationRoundTripAndConflict(t *testing.T) {
	source := t.TempDir()
	nu := filepath.Join(source, "nushell")
	opencode := filepath.Join(source, "opencode")
	fragments := filepath.Join(source, "fragments")
	for _, directory := range []string{filepath.Join(nu, "scripts"), opencode, filepath.Join(fragments, "nu"), filepath.Join(fragments, "personal")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for name, contents := range map[string]string{
		filepath.Join(nu, "config.nu"):                     "source scripts/helper.nu\n",
		filepath.Join(nu, "scripts", "helper.nu"):          "def hello [] { 'hello' }\n",
		filepath.Join(opencode, "opencode.json"):           "{\"theme\":\"system\"}\n",
		filepath.Join(opencode, "auth.json"):               "secret fixture must not export",
		filepath.Join(opencode, ".usage-status.env"):       "private fixture must not export",
		filepath.Join(fragments, "nu", "nu.json"):          `{"profiles":[{"commandline":"machine-specific Nushell installation"}]}`,
		filepath.Join(fragments, "personal", "shell.json"): `{"profiles":[{"commandline":"nu.exe"}]}`,
	} {
		if err := os.WriteFile(name, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := exportWorkstationSettings(t.Context(), map[string]string{"nushell": nu, "opencode": opencode, "terminal-fragments": fragments})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 {
		t.Fatalf("exported %d files, want authored config, nested Nu script and personal fragment only", len(files))
	}
	if slices.ContainsFunc(files, func(file WorkstationFile) bool { return file.Kind == "terminal-fragments" && file.Path == "nu/nu.json" }) {
		t.Fatal("export captured Nushell's machine-specific installer fragment")
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
			Name        string   `json:"name"`
			Seconds     int      `json:"seconds"`
			Fingerprint string   `json:"fingerprint"`
			Requires    []string `json:"requires"`
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
	for _, job := range plan.Jobs {
		if job.Name == "Windows desktop preferences" && len(job.Requires) != 0 {
			t.Fatal("desktop preferences depend on package installation")
		}
		if job.Name == "Visual Studio layout" {
			t.Fatal("target installation must consume the prepared layout, not materialize it again")
		}
		if job.Name == "Handy native headers" && (!slices.Equal(job.Requires, []string{"Vulkan SDK", "C and C++"})) {
			t.Fatalf("unrelated tools can suppress Handy header preparation: %v", job.Requires)
		}
		if job.Name == "Handy native headers" {
			dependencies, err := json.Marshal(job.Requires)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("WORKSTATION_TEST_DEPENDENCIES", string(dependencies))
		}
	}
	setup := provisioningPowerShellFunctionSetup(t, provisioningPowerShellFunctionSource{path: base, names: []string{"Get-ProvisioningBoundedDiagnosticText", "Get-ProvisioningStepFingerprint", "Initialize-ProvisioningSteps", "Save-ProvisioningSteps", "Invoke-ProvisioningStep", "Invoke-ProvisioningIndependentStep", "Test-ProvisioningTerminalError", "Set-ProvisioningDesktopUsability"}})
	fixture, err := os.ReadFile(filepath.Join("testdata", "workstation-dependencies.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(root, "dependencies.ps1")
	if err := os.WriteFile(fixturePath, []byte("$ErrorActionPreference = 'Stop'\n"+setup+string(fixture)), 0o600); err != nil {
		t.Fatal(err)
	}
	dependencyCheck := hiddenCommandContext(ctx, mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", fixturePath)
	dependencyCheck.Env = slices.DeleteFunc(os.Environ(), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		return strings.EqualFold(name, "PSModulePath")
	})
	if output, err := dependencyCheck.CombinedOutput(); err != nil || !strings.Contains(string(output), "verified actual Handy dependency effects") {
		t.Fatalf("dependency effects: %v %s", err, output)
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
	if err := os.MkdirAll(filepath.Join(root, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "state", "steps.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	dependencyChanged := readPlan()
	if fingerprint(changed, "C and C++") == fingerprint(dependencyChanged, "C and C++") {
		t.Fatal("dependent C++ step retained stale prerequisite evidence")
	}
}

// Opt-in real installer boundary. It installs only a previously absent small
// selected utility, then removes that exact task-owned registration. No existing
// Herdr, Sandbox, user configuration or tool installation is replaced.
func TestWorkstationInstallerContinuesAndResumes(t *testing.T) {
	installer := os.Getenv("HERDR_WORKSTATION_TEST_INSTALLER")
	if installer == "" || !IsCurrentWindowsSandbox() {
		t.Skip("set HERDR_WORKSTATION_TEST_INSTALLER and its SHA256 for native acceptance")
	}
	if runtime.GOOS != "windows" {
		t.Skip("Windows installer boundary")
	}
	expectedHash := os.Getenv("HERDR_WORKSTATION_TEST_INSTALLER_SHA256")
	data, err := os.ReadFile(installer)
	if err != nil || expectedHash == "" || fmt.Sprintf("%x", sha256.Sum256(data)) != expectedHash {
		t.Fatalf("installer identity not established: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Minute)
	defer cancel()
	winget, err := exec.LookPath("winget.exe")
	if err != nil {
		t.Fatal(err)
	}
	command := hiddenCommandContext(ctx, winget, "list", "--id", "rhysd.actionlint", "--exact", "--source", "winget", "--accept-source-agreements", "--disable-interactivity")
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	noApplicationCode := uint32(0x8A150014)
	if !errors.As(err, &exit) || uint32(exit.ExitCode()) != noApplicationCode {
		t.Fatalf("actionlint is not proven absent; preserving existing installation: %v %s", err, output)
	}
	packagePatterns := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Packages", "rhysd.actionlint_*", "actionlint.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "WinGet", "Packages", "rhysd.actionlint_*", "actionlint.exe"),
	}
	installedFiles := func() []string {
		t.Helper()
		var files []string
		for _, pattern := range packagePatterns {
			matches, err := filepath.Glob(pattern)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, matches...)
		}
		return files
	}
	if len(installedFiles()) != 0 {
		t.Fatal("unregistered actionlint payload exists; preserved as unknown state")
	}
	root, err := os.MkdirTemp(os.Getenv("ProgramData"), "herdr-workstation-acceptance-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	private := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(private, "config"))
	profilePath := filepath.Join(private, "profile.json")
	settings := []byte("{\"theme\":\"system\"}\n")
	profile := WorkstationProfile{SchemaVersion: 1, Packages: []wingetPackagePlanEntry{
		{ID: "HerdrWorkstation.NonexistentAcceptancePackage"}, {ID: "rhysd.actionlint"},
	}, Stacks: []string{}, Tools: []resolvedToolVersion{}, Files: []WorkstationFile{{Kind: "opencode", Path: "opencode.json", Data: settings, SHA256: fmt.Sprintf("%x", sha256.Sum256(settings))}}}
	writeProfile := func() {
		t.Helper()
		encoded, err := json.Marshal(profile)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(profilePath, encoded, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeProfile()
	var installedHash string
	t.Cleanup(func() {
		files := installedFiles()
		if len(files) == 1 {
			contents, readErr := os.ReadFile(files[0])
			if readErr != nil || (installedHash != "" && fmt.Sprintf("%x", sha256.Sum256(contents)) != installedHash) {
				t.Errorf("task package changed externally; cleanup preserved it")
				return
			}
			cleanupContext, stop := context.WithTimeout(context.Background(), 90*time.Second)
			defer stop()
			cleanup := hiddenCommandContext(cleanupContext, winget, "uninstall", "--id", "rhysd.actionlint", "--exact", "--source", "winget", "--silent", "--disable-interactivity")
			if out, err := cleanup.CombinedOutput(); err != nil {
				t.Errorf("task package cleanup: %v %s", err, out)
				return
			}
		}
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("task state cleanup: %v", err)
		}
	})
	runInstaller := func(want int) {
		t.Helper()
		command := hiddenCommandContext(ctx, installer, "/S", "/PROFILE="+profilePath, "/ROOT="+root)
		output, err := command.CombinedOutput()
		actual := 0
		if err != nil {
			var status *exec.ExitError
			if !errors.As(err, &status) {
				t.Fatalf("installer invocation: %v %s", err, output)
			}
			actual = status.ExitCode()
		}
		if actual != want {
			t.Fatalf("installer exit=%d, want%d: %s", actual, want, output)
		}
	}
	readReport := func() WorkstationReport {
		t.Helper()
		contents, err := os.ReadFile(filepath.Join(root, "report.json"))
		if err != nil {
			t.Fatal(err)
		}
		var report WorkstationReport
		if err := json.Unmarshal(contents, &report); err != nil {
			t.Fatal(err)
		}
		return report
	}
	runInstaller(1)
	first := readReport()
	if first.Complete {
		t.Fatal("failed package was reported complete")
	}
	statuses := map[string]string{}
	for _, step := range first.Steps {
		statuses[step.Name] = step.Status
	}
	if statuses["Package HerdrWorkstation.NonexistentAcceptancePackage"] != "failed" || statuses["Package rhysd.actionlint"] != "succeeded" || statuses["Configuration opencode/opencode.json"] != "succeeded" {
		t.Fatalf("independent work was not completed: %+v", first.Steps)
	}
	files := installedFiles()
	if len(files) != 1 {
		t.Fatalf("new actionlint payload not unique: %v", files)
	}
	contents, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	installedHash = fmt.Sprintf("%x", sha256.Sum256(contents))
	probe := hiddenCommandContext(ctx, files[0], "--version")
	if output, err := probe.CombinedOutput(); err != nil {
		t.Fatalf("installed utility capability: %v %s", err, output)
	} else {
		t.Logf("installed utility reports: %s", strings.TrimSpace(string(output)))
	}
	imported, err := os.ReadFile(filepath.Join(private, "config", "opencode", "opencode.json"))
	if err != nil || !bytes.Equal(imported, settings) {
		t.Fatalf("actual configuration import: %v", err)
	}
	profile.Packages = profile.Packages[1:]
	writeProfile()
	runInstaller(0)
	second := readReport()
	if !second.Complete {
		t.Fatalf("corrected setup incomplete: %+v", second.Steps)
	}
	if !slices.ContainsFunc(second.Steps, func(step WorkstationStep) bool {
		return step.Name == "Package rhysd.actionlint" && step.Status == "retained"
	}) {
		t.Fatal("completed utility was not retained")
	}
	after, err := os.ReadFile(files[0])
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(after)) != installedHash {
		t.Fatal("retained executable changed")
	}
	installedVersion := func(id string) string {
		t.Helper()
		query := hiddenCommandContext(ctx, winget, "list", "--id", id, "--exact", "--source", "winget", "--accept-source-agreements", "--disable-interactivity")
		text, err := query.CombinedOutput()
		if err != nil {
			t.Fatalf("inspect existing prerequisite %s: %v %s", id, err, text)
		}
		matches := regexp.MustCompile(regexp.QuoteMeta(id)+`\s+(\S+)`).FindAllStringSubmatch(string(text), -1)
		if len(matches) != 1 {
			t.Fatalf("existing prerequisite version is ambiguous: %s", text)
		}
		return matches[0][1]
	}
	// Use the real shared cached-installer/manifest path without upgrading the
	// current compiler or shared VC runtime. The stack compiles its owned smoke.
	profile.Packages = append(profile.Packages, wingetPackagePlanEntry{ID: "Microsoft.VCRedist.2015+.x64", Version: installedVersion("Microsoft.VCRedist.2015+.x64")})
	profile.Stacks = []string{"nsis"}
	profile.Tools = []resolvedToolVersion{{Tool: "NSIS.NSIS", Version: installedVersion("NSIS.NSIS"), Source: toolVersionSourceExplicit, Owners: []string{"native-acceptance"}}}
	writeProfile()
	runInstaller(0)
	third := readReport()
	if !third.Complete || !slices.ContainsFunc(third.Steps, func(step WorkstationStep) bool { return step.Name == "Stack nsis" && step.Status == "succeeded" }) {
		t.Fatalf("shared native stack failed: %+v", third.Steps)
	}
}
