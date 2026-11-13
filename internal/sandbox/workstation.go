package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"herdr-sandbox/internal/productidentity"
)

const workstationProfileLimit = 64 * 1024 * 1024

//go:embed assets/workstation.ps1
var workstationScript []byte

//go:embed assets/workstation-winget.ps1
var workstationWinGetScript []byte

//go:embed assets/workstation-action.ps1
var workstationActionScript []byte

//go:embed assets/workstation-root.cs
var workstationRootSource []byte

func ensureWorkstationSecurity(ctx context.Context, root string) error {
	powerShell, err := windowsPowerShellExecutable()
	if err != nil {
		return err
	}
	checkContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	script := `$ErrorActionPreference = 'Stop'; $env:PSModulePath = $PSHOME + '\Modules'; Add-Type -TypeDefinition ([Console]::In.ReadToEnd()); [HerdrWorkstation.Directories]::Ensure($env:HERDR_WORKSTATION_SETUP_ROOT)`
	command := hiddenCommandContext(checkContext, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", encodePowerShell(script))
	command.Env = append(os.Environ(), "HERDR_WORKSTATION_SETUP_ROOT="+root)
	command.Stdin = bytes.NewReader(workstationRootSource)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("secure direct setup state: %w: %s", err, sanitizeTerminalText(string(output), 2000))
	}
	return nil
}

// WorkstationProfile is an explicit portable selection, not a machine backup.
// Files include the private shared configuration/authentication archive.
type WorkstationProfile struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Packages      []wingetPackagePlanEntry `json:"packages"`
	Stacks        []string                 `json:"stacks"`
	Tools         []resolvedToolVersion    `json:"tools"`
	Files         []WorkstationFile        `json:"files"`
}

type WorkstationFile struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Data   []byte `json:"data"`
}

type WorkstationOptions struct {
	Profile string
	Root    string
	Refresh bool
	Inputs  string
	Output  io.Writer
}

type WorkstationStep struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type WorkstationReport struct {
	SchemaVersion int               `json:"schemaVersion"`
	ProfileSHA256 string            `json:"profileSha256"`
	StartedAtUTC  string            `json:"startedAtUTC"`
	FinishedAtUTC string            `json:"finishedAtUTC"`
	Complete      bool              `json:"complete"`
	Steps         []WorkstationStep `json:"steps"`
}

// WorkstationStateRoot keeps resumable machine state separate for each intended user.
func WorkstationStateRoot() (string, error) {
	current, err := user.Current()
	if err != nil {
		return "", err
	}
	if !windowsSIDPattern.MatchString(current.Uid) || slices.Contains([]string{"S-1-5-18", "S-1-5-19", "S-1-5-20"}, current.Uid) {
		return "", errors.New("workstation setup requires an intended Windows user, not a service account")
	}
	root := os.Getenv("ProgramData")
	if !filepath.IsAbs(root) {
		return "", errors.New("ProgramData is not an absolute directory")
	}
	return filepath.Join(root, productidentity.WorkstationApplicationName, current.Uid), nil
}

func resolveWorkstationSelection(root, requested string) (WorkstationProfile, []byte, string, error) {
	selectionPath := filepath.Join(root, "selection.json")
	if err := validateWorkstationLocalPath(selectionPath); err != nil {
		return WorkstationProfile{}, nil, "", err
	}
	if requested == "" {
		stored, found, err := readBoundedRegularFile(selectionPath, workstationProfileLimit)
		if err != nil {
			return WorkstationProfile{}, nil, "", err
		}
		if found {
			profile, err := decodeWorkstationProfile(stored)
			return profile, stored, "", err
		}
	}
	profile, data, err := readWorkstationProfile(requested)
	if err != nil {
		return profile, data, "", err
	}
	// The already ACL-protected installation root owns the exact personal
	// snapshot, including resume after NSIS removes its extraction directory.
	if err := writeFileAtomically(selectionPath, data, 0o600); err != nil {
		return profile, data, "", err
	}
	return profile, data, "", nil
}

func DefaultWorkstationProfile() WorkstationProfile {
	profile := WorkstationProfile{SchemaVersion: 1, Packages: []wingetPackagePlanEntry{}, Stacks: []string{}, Tools: []resolvedToolVersion{}, Files: []WorkstationFile{}}
	for _, id := range append(slices.Clone(basePackageIDs), defaultCodingAgentPackageIDs()...) {
		if id != packageTerminalPreview {
			profile.Packages = append(profile.Packages, wingetPackagePlanEntry{ID: id})
		}
	}
	for _, stack := range allProjectInitStacks {
		profile.Stacks = append(profile.Stacks, string(stack))
	}
	profile.Packages = append(profile.Packages, wingetPackagePlanEntry{ID: "hdosys.herdr-win"})
	profile.Packages = append(profile.Packages, wingetPackagePlanEntry{ID: "Microsoft.VCRedist.2015+.x64"})
	profile.Stacks = append(profile.Stacks, "opensrc")
	profile.Stacks = append(profile.Stacks, "font")
	return profile
}

func decodeWorkstationProfile(data []byte) (WorkstationProfile, error) {
	var result WorkstationProfile
	if len(data) > workstationProfileLimit {
		return result, errors.New("workstation profile exceeds 64 MiB")
	}
	if err := validateExactJSONObjectShape(data, "workstation profile", []string{"schemaVersion", "packages", "stacks", "tools", "files"}); err != nil {
		return result, err
	}
	if err := decodeStrictJSON(data, &result); err != nil {
		return result, fmt.Errorf("decode workstation profile: %w", err)
	}
	if result.SchemaVersion != 1 || result.Packages == nil || result.Stacks == nil || result.Tools == nil || result.Files == nil || len(result.Packages) > 64 || len(result.Stacks) > 32 || len(result.Tools) > 64 || len(result.Files) > 1 {
		return result, errors.New("unsupported or oversized workstation profile")
	}
	// Reject ambiguous objects before crossing into PowerShell's JSON decoder.
	var raw struct {
		Packages []json.RawMessage `json:"packages"`
		Tools    []json.RawMessage `json:"tools"`
		Files    []json.RawMessage `json:"files"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return result, err
	}
	for _, entry := range raw.Packages {
		if err := validateExactJSONObjectShape(entry, "package", []string{"id", "version"}); err != nil {
			return result, err
		}
	}
	for _, entry := range raw.Tools {
		if err := validateExactJSONObjectShape(entry, "tool", []string{"tool", "version", "series", "source", "owners"}); err != nil {
			return result, err
		}
	}
	for _, entry := range raw.Files {
		if err := validateExactJSONObjectShape(entry, "configuration file", []string{"kind", "path", "sha256", "data"}); err != nil {
			return result, err
		}
	}
	seen := map[string]bool{}
	for _, entry := range result.Packages {
		key := strings.ToLower(entry.ID)
		if !validWingetPackageID(entry.ID) || (entry.Version != "" && !validWingetPackageVersion(entry.Version)) || seen[key] {
			return result, errors.New("invalid or duplicate workstation package")
		}
		seen[key] = true
	}
	allowedStacks := []string{"android", "audio", "bun", "cargo-nextest", "cpp", "dotnet", "font", "git-sh", "go", "handy", "herdr", "hyperframes", "java", "just", "node", "nsis", "opensrc", "playwright-cli", "python", "python-ai", "rust-msvc", "tradingview", "uv", "zig"}
	seen = map[string]bool{}
	for _, stack := range result.Stacks {
		if !slices.Contains(allowedStacks, stack) || seen[stack] {
			return result, errors.New("invalid or duplicate workstation stack")
		}
		seen[stack] = true
	}
	seen = map[string]bool{}
	for _, tool := range result.Tools {
		key := strings.ToLower(tool.Tool)
		if !projectToolTokenPattern.MatchString(tool.Tool) || (tool.Version != "" && !projectToolValuePattern.MatchString(tool.Version)) || (tool.Series != "" && !projectToolValuePattern.MatchString(tool.Series)) || seen[key] {
			return result, errors.New("invalid or duplicate workstation tool selection")
		}
		seen[key] = true
	}
	for _, file := range result.Files {
		if file.Kind != "configuration-archive" || file.Path != "configuration.zip" || len(file.Data) > maximumConfigurationFileSize || fmt.Sprintf("%x", sha256.Sum256(file.Data)) != file.SHA256 {
			return result, errors.New("invalid or hash-mismatched workstation configuration archive")
		}
	}
	return result, nil
}

func readWorkstationProfile(path string) (WorkstationProfile, []byte, error) {
	if path == "" {
		profile := DefaultWorkstationProfile()
		data, err := json.MarshalIndent(profile, "", "  ")
		return profile, data, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return WorkstationProfile{}, nil, err
	}
	if err := validateWorkstationLocalPath(absolute); err != nil {
		return WorkstationProfile{}, nil, err
	}
	data, found, err := readBoundedRegularFile(absolute, workstationProfileLimit)
	if err != nil {
		return WorkstationProfile{}, nil, err
	}
	if !found {
		return WorkstationProfile{}, nil, fmt.Errorf("read workstation profile: %w", os.ErrNotExist)
	}
	profile, err := decodeWorkstationProfile(data)
	return profile, data, err
}

func validateWorkstationLocalPath(path string) error {
	if !filepath.IsAbs(path) || strings.HasPrefix(path, `\\`) || filepath.Dir(path) == path {
		return errors.New("workstation path must be an absolute local directory, not a drive root or network share")
	}
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			reparse, reparseErr := fileInfoIsReparsePoint(info)
			if reparseErr != nil {
				return reparseErr
			}
			if reparse || (!info.IsDir() && current != path) {
				return errors.New("workstation path contains a reparse point or non-directory ancestor")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return nil
}

func ensureWorkstationRoot(root string) error {
	if err := validateWorkstationLocalPath(root); err != nil {
		return err
	}
	marker := filepath.Join(root, "workstation-owner.json")
	if err := validateWorkstationLocalPath(marker); err != nil {
		return err
	}
	if data, found, err := readBoundedRegularFile(marker, 1024); err == nil && found {
		if string(data) != "{\"product\":\"herdr-workstation\",\"schemaVersion\":1}\n" {
			return errors.New("workstation root has an unknown owner")
		}
		return rejectMappedPathReparsePoints(marker)
	} else if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(entries) != 0 {
		return errors.New("workstation root is nonempty and unowned; existing files were preserved")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(file, "{\"product\":\"herdr-workstation\",\"schemaVersion\":1}\n")
	return errors.Join(writeErr, file.Close())
}

// SetupWorkstation runs trusted shared provisioning against this Windows machine.
// It never enters the Sandbox lifecycle or applies the Sandbox security profile.
func SetupWorkstation(ctx context.Context, options WorkstationOptions) (reportFile string, setupErr error) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return "", errors.New("direct setup requires Windows x64")
	}
	if options.Output == nil {
		options.Output = io.Discard
	}
	if options.Inputs == "" {
		return "", errors.New("setup requires the installer data ZIP")
	}
	if options.Root == "" {
		var err error
		options.Root, err = WorkstationStateRoot()
		if err != nil {
			return "", err
		}
	}
	root, err := filepath.Abs(options.Root)
	if err != nil {
		return "", err
	}
	if err := validateWorkstationLocalPath(root); err != nil {
		return "", err
	}
	if err := ensureWorkstationSecurity(ctx, root); err != nil {
		return "", err
	}
	root, err = canonicalMappedDirectory(root)
	if err != nil {
		return "", err
	}
	lockContext, stopLock := context.WithTimeout(ctx, 5*time.Second)
	defer stopLock()
	release, err := acquireNamedLifecycleLock(lockContext, fmt.Sprintf(`Global\HerdrWorkstation-%x`, sha256.Sum256([]byte(strings.ToLower(root)))))
	if err != nil {
		return "", fmt.Errorf("another setup may be using this root: %w", err)
	}
	defer release()
	if err := ensureWorkstationRoot(root); err != nil {
		return "", err
	}
	logPath := filepath.Join(root, "setup.log")
	if err := validateWorkstationLocalPath(logPath); err != nil {
		return "", err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("open setup log: %w", err)
	}
	options.Output = io.MultiWriter(logFile, options.Output)
	defer func() {
		if setupErr != nil {
			_, _ = fmt.Fprintf(options.Output, "Setup failed: %v\n", setupErr)
		}
		setupErr = errors.Join(setupErr, logFile.Sync(), logFile.Close())
	}()
	fmt.Fprintf(options.Output, "\nSetup attempt started %s\nComplete installer output: %s\n", time.Now().UTC().Format(time.RFC3339), logPath)
	profile, data, configurationProblem, err := resolveWorkstationSelection(root, options.Profile)
	if err != nil {
		return "", err
	}
	defer clear(data)
	defer func() {
		for _, file := range profile.Files {
			clear(file.Data)
		}
	}()
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	base := filepath.Join(filepath.Dir(executable), baseProvisioningName)
	stacks := filepath.Join(filepath.Dir(executable), stackProvisioningName)
	if err := validateBaseProvisioningContract(base); err != nil {
		return "", err
	}
	if err := validateStackProvisioningContract(stacks); err != nil {
		return "", err
	}
	input := filepath.Join(root, "input")
	if err := os.MkdirAll(input, 0o700); err != nil {
		return "", err
	}
	if err := rejectMappedPathReparsePoints(input); err != nil {
		return "", err
	}
	if err := applyWorkstationInputs(ctx, root, options.Inputs, options.Output); err != nil {
		return "", err
	}
	preparedPath := filepath.Join(root, "prepared-inputs.json")
	preparedData, found, err := readBoundedRegularFile(preparedPath, 16*1024*1024)
	if err != nil || !found {
		return "", errors.New("materialized input metadata is unavailable")
	}
	var prepared preparedWorkstationInputs
	if err := decodeStrictJSON(preparedData, &prepared); err != nil || prepared.SchemaVersion != 1 {
		return "", errors.New("materialized input metadata is invalid")
	}
	baseData, err := readProvisioningScript(base, "base", maximumBaseScriptSize)
	if err != nil {
		return "", err
	}
	stackData, err := readProvisioningScript(stacks, "stacks", maximumStackScriptSize)
	if err != nil {
		return "", err
	}
	// Do not stage personal settings into the elevated tool runner's input.
	selection := profile
	selection.Files = []WorkstationFile{}
	selection.Packages = append([]wingetPackagePlanEntry(nil), profile.Packages...)
	for index, requested := range selection.Packages {
		resolved := ""
		for _, entry := range prepared.Packages {
			if strings.EqualFold(entry.Metadata.ID, requested.ID) {
				if resolved != "" && resolved != entry.Metadata.Version {
					return "", errors.New("materialized package versions conflict")
				}
				resolved = entry.Metadata.Version
			}
		}
		if resolved == "" || (requested.Version != "" && requested.Version != resolved) {
			return "", fmt.Errorf("materialized package does not match the selected %s version", requested.ID)
		}
		selection.Packages[index].Version = resolved
	}
	selection.Tools = prepared.Tools
	selectionData, err := json.Marshal(selection)
	if err != nil {
		return "", err
	}
	for name, contents := range map[string][]byte{
		baseProvisioningName: baseData, stackProvisioningName: stackData,
		playwrightAccessName: playwrightAccessScript, apifyAccessName: apifyAccessScript,
		"profile.json": selectionData, "workstation.ps1": workstationScript,
		"prepared-inputs.json":   preparedData,
		"workstation-winget.ps1": workstationWinGetScript, "workstation-action.ps1": workstationActionScript, provisioningProcessName: provisioningProcessSource,
	} {
		path := filepath.Join(input, name)
		if err := validateWorkstationLocalPath(path); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			return "", err
		}
	}
	base, stacks = filepath.Join(input, baseProvisioningName), filepath.Join(input, stackProvisioningName)
	started := time.Now().UTC().Format(time.RFC3339)
	profileHash := fmt.Sprintf("%x", sha256.Sum256(data))
	reportPath := filepath.Join(root, "report.json")
	if err := validateWorkstationLocalPath(reportPath); err != nil {
		return "", err
	}
	// An old successful report must never stand in for a failed new invocation.
	if err := os.WriteFile(reportPath, []byte("{}\n"), 0o600); err != nil {
		return "", err
	}
	htmlPath := filepath.Join(root, "report.html")
	if err := validateWorkstationLocalPath(htmlPath); err != nil {
		return "", err
	}
	if err := os.WriteFile(htmlPath, []byte("<!doctype html><meta charset=utf-8><title>Setup in progress</title><h1>Setup has not completed</h1><p>This run is starting. No successful result is claimed.</p>"), 0o600); err != nil {
		return "", err
	}
	powerShell, err := windowsPowerShellExecutable()
	if err != nil {
		return "", err
	}
	arguments := []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(input, "workstation.ps1"), "-NativeProfilePath", filepath.Join(input, "profile.json"), "-NativeRoot", root, "-BasePath", base, "-StacksPath", stacks, "-RunnerPath", executable}
	if options.Refresh {
		arguments = append(arguments, "-Refresh")
	}
	environment := slices.DeleteFunc(os.Environ(), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		return strings.EqualFold(name, "PSModulePath") || strings.HasPrefix(strings.ToUpper(name), "HERDR_")
	})
	planContext, stopPlan := context.WithTimeout(ctx, time.Minute)
	planCommand := hiddenCommandContext(planContext, powerShell, append(slices.Clone(arguments), "-PlanOnly")...)
	planCommand.Env = environment
	planData, planErr := planCommand.CombinedOutput()
	stopPlan()
	var budget struct {
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
	if planErr == nil {
		planErr = decodeStrictJSON(planData, &budget)
	}
	if planErr != nil || budget.SchemaVersion != 1 || budget.Seconds < 1 || budget.Seconds > 48*60*60 || budget.Steps < 1 || budget.Steps > 128 {
		return "", fmt.Errorf("cannot establish the complete bounded setup plan: %v; %s", planErr, sanitizeTerminalText(string(planData), 2000))
	}
	fmt.Fprintf(options.Output, "Attempting %d steps; total safety deadline %s includes every step deadline and cleanup allowance.\n", budget.Steps, time.Duration(budget.Seconds)*time.Second)
	runContext, stop := context.WithTimeout(ctx, time.Duration(budget.Seconds)*time.Second)
	defer stop()
	command := hiddenCommandContext(runContext, powerShell, arguments...)
	command.Env = environment
	command.Stdout, command.Stderr = options.Output, options.Output
	runErr := command.Run()
	report := WorkstationReport{SchemaVersion: 1, StartedAtUTC: started, ProfileSHA256: profileHash, Steps: []WorkstationStep{}}
	resultData, found, readErr := readBoundedRegularFile(reportPath, 1024*1024)
	var native struct {
		SchemaVersion int               `json:"schemaVersion"`
		Steps         []WorkstationStep `json:"steps"`
	}
	if readErr == nil && found {
		readErr = decodeStrictJSON(resultData, &native)
	}
	if readErr != nil || native.SchemaVersion != 1 || len(native.Steps) == 0 || len(native.Steps) > 128 {
		report.Steps = append(report.Steps, WorkstationStep{"Provisioning runtime", "failed", "The provisioning process did not produce a complete report. See the setup log."})
	} else {
		report.Steps = native.Steps
	}
	if runErr != nil && !slices.ContainsFunc(report.Steps, func(step WorkstationStep) bool { return step.Status != "succeeded" && step.Status != "retained" }) {
		report.Steps = append(report.Steps, WorkstationStep{"Provisioning process", "failed", "The provisioning process exited unsuccessfully."})
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		for _, file := range profile.Files {
			step := WorkstationStep{Name: "Configuration and authentication", Status: "succeeded", Message: "Applied and verified through the shared configuration owner."}
			if err := applyInstallerConfiguration(ctx, root, file, slices.Contains(profile.Stacks, string(stackTradingView)), options.Output); err != nil {
				step.Status, step.Message = "failed", err.Error()
			}
			report.Steps = append(report.Steps, step)
		}
	}
	if configurationProblem != "" {
		report.Steps = append(report.Steps, WorkstationStep{"Configuration source", "failed", configurationProblem})
	}
	report.FinishedAtUTC = time.Now().UTC().Format(time.RFC3339)
	report.Complete = len(report.Steps) > 0 && !slices.ContainsFunc(report.Steps, func(step WorkstationStep) bool { return step.Status != "succeeded" && step.Status != "retained" })
	return publishWorkstationReport(root, report, options.Output)
}

func publishWorkstationReport(root string, report WorkstationReport, output io.Writer) (string, error) {
	reportPath := filepath.Join(root, "report.json")
	if err := validateWorkstationLocalPath(reportPath); err != nil {
		return "", err
	}
	if err := writeVoxCPM2JSONAtomically(reportPath, report); err != nil {
		return "", err
	}
	htmlPath := filepath.Join(root, "report.html")
	if err := validateWorkstationLocalPath(htmlPath); err != nil {
		return "", err
	}
	var html bytes.Buffer
	if err := workstationReportTemplate.Execute(&html, report); err != nil {
		return "", err
	}
	if err := os.WriteFile(htmlPath, html.Bytes(), 0o600); err != nil {
		return "", err
	}
	fmt.Fprintf(output, "Setup report: %s\n", htmlPath)
	if !report.Complete {
		return htmlPath, errors.New("setup finished with incomplete steps; independent steps were attempted; rerun to resume")
	}
	return htmlPath, nil
}

var workstationReportTemplate = template.Must(template.New("report").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><title>Herdr Workstation setup</title><style>body{font:16px system-ui;background:#fafafa;color:#222;max-width:1000px;margin:3rem auto;padding:1rem}table{border-collapse:collapse;width:100%}td,th{border:1px solid #ccc;padding:.6rem;text-align:left}pre{white-space:pre-wrap}h1{font-size:1.6rem}</style><h1>Herdr Workstation setup: {{if .Complete}}complete{{else}}incomplete{{end}}</h1><p>Finished {{.FinishedAtUTC}}. Re-run the installer to resume unfinished steps. Existing successful unchanged steps are retained. Applications requiring accounts or licences need your separate sign-in.</p><p><a href="setup.log">Open the complete installer output</a>. Attempts are appended with their start time; this log is private.</p><table><tr><th>Step</th><th>Result</th><th>Details</th></tr>{{range .Steps}}<tr><td>{{.Name}}</td><td>{{.Status}}</td><td><pre>{{.Message}}</pre></td></tr>{{end}}</table></html>`))

// PrepareWorkstationTTS reuses the existing public bundle preparation owner.
func PrepareWorkstationTTS(ctx context.Context, root string, output io.Writer) error {
	if err := ensureWorkstationRoot(root); err != nil {
		return err
	}
	models := filepath.Join(root, "models")
	if err := os.MkdirAll(models, 0o700); err != nil {
		return err
	}
	return prepareHyperFramesVoxCPM2(ctx, models, "", output)
}
