package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"herdr-sandbox/internal/productidentity"
)

const (
	taskTimeout                = 5 * time.Minute
	integrationTaskTimeout     = 30 * time.Minute
	currentSandboxTaskTimeout  = 45 * time.Minute
	currentPackageTaskTimeout  = 60 * time.Minute
	releaseTaskTimeout         = integrationTaskTimeout + currentPackageTaskTimeout + 2*time.Minute
	nativeAllStacksTaskTimeout = 2 * time.Hour
	fastTestsEnvironment       = "HERDR_SANDBOX_FAST_TESTS"
)

var usage = fmt.Sprintf(`Usage: go run ./cmd/task <task>

Tasks:
  fmt              format Go source
  test [args...]   run fast product tests with optional go test arguments
  test-integration [args...]  run all Go external-boundary tests
  build            build intermediate CLI output at build/bin/%s
  provisioning-preflight  validate current guest inputs before an expensive native or installed-candidate gate
  native-current-sandbox  provision and verify all stacks inside this active Sandbox without touching SSH or Herdr
  package-current-sandbox VERSION  package and exercise fresh install, repair, upgrade, provisioning, and uninstall in this Sandbox
  native-all-stacks build and test all built-in stacks in one real Windows Sandbox
  release VERSION    accept the frozen installed candidate, create its annotated tag, and push it
  validate-release-tag VERSION  validate the accepted annotated tag in release automation
  release-notes VERSION  validate the matching CHANGELOG section and print its tagged link
  package VERSION [--release]  build one canonical local installer or the versioned release pair
  verify           verify format, modernization, analysis, tests, and the stable build
  verify-integration run the full nightly/release verification
`, productidentity.ExecutableName)

func main() {
	interruptContext, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	timeout := taskTimeoutFor(os.Args[1:])
	ctx, cancel := context.WithTimeout(interruptContext, timeout)
	defer cancel()

	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("exceeded %s: %w", timeout, err)
		}
		fmt.Fprintln(os.Stderr, "task:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		_, err := io.WriteString(stdout, usage)
		return err
	}

	switch args[0] {
	case "fmt":
		if len(args) != 1 {
			return errors.New("fmt accepts no arguments")
		}
		return runCommand(ctx, stdout, stderr, "go", "fmt", "./...")
	case "test":
		extra := args[1:]
		if len(extra) > 0 && extra[0] == "--" {
			extra = extra[1:]
		}
		return runGoTests(ctx, stdout, stderr, true, extra)
	case "test-integration":
		extra := args[1:]
		if len(extra) > 0 && extra[0] == "--" {
			extra = extra[1:]
		}
		return runGoTests(ctx, stdout, stderr, false, extra)
	case "build":
		if len(args) != 1 {
			return errors.New("build accepts no arguments")
		}
		return build(ctx, stdout, stderr)
	case "provisioning-preflight":
		if len(args) != 1 {
			return errors.New("provisioning-preflight accepts no arguments")
		}
		return currentSandboxProvisioningPreflight(ctx, stdout, stderr)
	case "native-all-stacks":
		if len(args) != 1 {
			return errors.New("native-all-stacks accepts no arguments")
		}
		return nativeAllStacks(ctx, stdout, stderr)
	case "native-current-sandbox":
		if len(args) != 1 {
			return errors.New("native-current-sandbox accepts no arguments")
		}
		return nativeCurrentSandbox(ctx, stdout, stderr, "")
	case "package-current-sandbox":
		if len(args) != 2 {
			return errors.New("package-current-sandbox requires one v0.0.RELEASE_ID version")
		}
		return packageCurrentSandbox(ctx, args[1], stdout, stderr)
	case "release":
		if len(args) != 2 {
			return errors.New("release requires one v0.0.RELEASE_ID version")
		}
		return release(ctx, args[1], stdout, stderr)
	case "validate-release-tag":
		if len(args) != 2 {
			return errors.New("validate-release-tag requires one v0.0.RELEASE_ID version")
		}
		return validateAcceptedReleaseTag(ctx, args[1])
	case "package":
		if len(args) != 2 && !(len(args) == 3 && args[2] == "--release") {
			return errors.New("package requires one v0.0.RELEASE_ID version and optional --release")
		}
		return packageWindowsRelease(ctx, args[1], len(args) == 3, stdout, stderr)
	case "release-notes":
		if len(args) != 2 {
			return errors.New("release-notes requires one v0.0.RELEASE_ID version")
		}
		return writeReleaseNotes(args[1], stdout)
	case "verify":
		if len(args) != 1 {
			return errors.New("verify accepts no arguments")
		}
		return verify(ctx, stdout, stderr, true)
	case "verify-integration":
		if len(args) != 1 {
			return errors.New("verify-integration accepts no arguments")
		}
		return verify(ctx, stdout, stderr, false)
	default:
		return fmt.Errorf("unknown task %q\n\n%s", args[0], usage)
	}
}

func taskTimeoutFor(args []string) time.Duration {
	if len(args) > 0 && args[0] == "native-all-stacks" {
		return nativeAllStacksTaskTimeout
	}
	if len(args) > 0 && args[0] == "native-current-sandbox" {
		return currentSandboxTaskTimeout
	}
	if len(args) > 0 && args[0] == "package-current-sandbox" {
		return currentPackageTaskTimeout
	}
	if len(args) > 0 && args[0] == "release" {
		return releaseTaskTimeout
	}
	if len(args) > 0 && (args[0] == "test-integration" || args[0] == "verify-integration") {
		return integrationTaskTimeout
	}
	return taskTimeout
}

func verify(ctx context.Context, stdout, stderr io.Writer, fast bool) error {
	fmt.Fprintln(stdout, "Checking Go formatting...")
	if err := checkGoFormat(ctx, stderr); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Checking Go modernization...")
	if err := runCommand(ctx, stdout, stderr, "go", "fix", "-diff", "./..."); err != nil {
		return fmt.Errorf("source has pending Go modernization fixes; run `go fix ./...` and resolve any reported conflicts: %w", err)
	}
	fmt.Fprintln(stdout, "Running Staticcheck...")
	if err := runCommand(ctx, stdout, stderr, "go", "tool", "staticcheck", "-checks=all", "./..."); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Running nilness analysis...")
	if err := runCommand(ctx, stdout, stderr, "go", "tool", "nilness", "./..."); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Checking Windows PowerShell syntax...")
	if err := checkPowerShell(ctx, stdout, stderr); err != nil {
		return err
	}
	if err := runGoTests(ctx, stdout, stderr, fast, nil); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Running go vet...")
	if err := runCommand(ctx, stdout, stderr, "go", "vet", "./..."); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Building sandbox...")
	return build(ctx, stdout, stderr)
}

func runGoTests(ctx context.Context, stdout, stderr io.Writer, fast bool, extra []string) error {
	if fast {
		fmt.Fprintln(stdout, "Running fast product Go tests...")
		fmt.Fprintln(stdout, "External Windows PowerShell and Git test processes are skipped; use `verify-integration` for that matrix.")
	} else {
		fmt.Fprintln(stdout, "Running the full Go test matrix, including external Windows PowerShell and Git processes...")
	}
	goArgs := append([]string{"test", "./..."}, extra...)
	command := hiddenCommandContext(ctx, "go", goArgs...)
	command.Env = goTestEnvironment(fast)
	command.Stdout = stdout
	command.Stderr = stderr
	command.Stdin = os.Stdin
	if err := command.Run(); err != nil {
		return fmt.Errorf("run %s: %w", commandText("go", goArgs), err)
	}
	return nil
}

func goTestEnvironment(fast bool) []string {
	prefix := strings.ToUpper(fastTestsEnvironment) + "="
	environment := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), prefix) {
			environment = append(environment, entry)
		}
	}
	if fast {
		environment = append(environment, fastTestsEnvironment+"=1")
	}
	return environment
}

func checkGoFormat(ctx context.Context, stderr io.Writer) error {
	var files []string
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".agent", "build", "node_modules", "reference", "references":
				if path != "." {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("find Go files: %w", err)
	}
	if len(files) == 0 {
		return errors.New("no Go files found")
	}
	sort.Strings(files)

	var output bytes.Buffer
	command := hiddenCommandContext(ctx, "gofmt", append([]string{"-l"}, files...)...)
	command.Stdout = &output
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("check Go formatting: %w", err)
	}
	if unformatted := strings.TrimSpace(output.String()); unformatted != "" {
		return fmt.Errorf("files need Go formatting:\n%s", unformatted)
	}
	return nil
}

func checkPowerShell(ctx context.Context, stdout, stderr io.Writer) error {
	scripts := []string{}
	for _, pattern := range []string{filepath.Join("cmd", "task", "assets", "*.ps1"), filepath.Join("internal", "sandbox", "assets", "*.ps1"), filepath.Join("provisioning", "*.ps1"), filepath.Join(".herdr-sandbox", "*.ps1"), filepath.Join("packaging", "windows", "*.ps1")} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("find PowerShell scripts matching %s: %w", pattern, err)
		}
		scripts = append(scripts, matches...)
	}
	sort.Strings(scripts)

	powerShell := "powershell.exe"
	if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
		candidate := filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if _, err := os.Stat(candidate); err == nil {
			powerShell = candidate
		}
	}
	absoluteScripts := make([]string, 0, len(scripts))
	for _, relative := range scripts {
		script, err := filepath.Abs(relative)
		if err != nil {
			return fmt.Errorf("resolve PowerShell script %s: %w", relative, err)
		}
		if _, err := os.Stat(script); err != nil {
			return fmt.Errorf("find PowerShell script %s: %w", relative, err)
		}
		absoluteScripts = append(absoluteScripts, script)
	}
	if err := runPowerShellSyntaxCheck(ctx, powerShell, absoluteScripts, stdout, stderr); err != nil {
		if runtime.GOOS != "windows" {
			return fmt.Errorf("validate Windows PowerShell scripts (run this gate on Windows): %w", err)
		}
		return fmt.Errorf("validate Windows PowerShell scripts: %w", err)
	}
	return nil
}

func runPowerShellSyntaxCheck(ctx context.Context, powerShell string, scripts []string, stdout, stderr io.Writer) error {
	if len(scripts) == 0 {
		return nil
	}
	parser := `$scriptPaths = @(([Console]::In.ReadToEnd()).Split([char]10))
$failed = $false
foreach ($scriptPath in $scriptPaths) {
    $tokens = $null
    $errors = $null
    [System.Management.Automation.Language.Parser]::ParseFile($scriptPath, [ref]$tokens, [ref]$errors) | Out-Null
    foreach ($parseError in @($errors)) {
        [Console]::Error.WriteLine(('{0}: {1}' -f $scriptPath, $parseError.Message))
        $failed = $true
    }
}
if ($failed) {
    exit 1
}`
	command := hiddenCommandContext(ctx, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", parser)
	command.Stdin = strings.NewReader(strings.Join(scripts, "\n"))
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func build(ctx context.Context, stdout, stderr io.Writer) error {
	identity := buildIdentity{Version: "devel", Revision: "unknown", Freshness: productidentity.FormatBuildFreshness(time.Now())}
	if revision, err := sourceRevision(ctx); err == nil {
		identity.Revision = revision
	}
	return buildWithIdentity(ctx, identity, stdout, stderr)
}

type buildIdentity struct {
	Version   string
	Revision  string
	Freshness string
}

func sourceRevision(ctx context.Context) (string, error) {
	output, err := hiddenCommandContext(ctx, "git", "rev-parse", "--verify", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run git rev-parse --verify HEAD: %w", err)
	}
	return normalizeSourceRevision(string(output))
}

func normalizeSourceRevision(value string) (string, error) {
	revision := strings.TrimSpace(value)
	decoded, err := hex.DecodeString(revision)
	if err != nil || len(decoded) != 20 {
		return "", fmt.Errorf("source revision is not one full SHA-1 Git commit ID: %q", revision)
	}
	return strings.ToLower(revision), nil
}

func buildWithIdentity(ctx context.Context, identity buildIdentity, stdout, stderr io.Writer) error {
	if !productidentity.ValidBuildFreshness(identity.Freshness) {
		return fmt.Errorf("build freshness must use %s UTC format: %q", productidentity.BuildFreshnessLayout, identity.Freshness)
	}
	output := filepath.Join("build", "bin", productidentity.ExecutableName)
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create build output directory: %w", err)
	}
	if err := runCommand(ctx, stdout, stderr, "go", goBuildArgs(output, identity)...); err != nil {
		return err
	}
	for _, asset := range []struct {
		Source string
		Name   string
	}{
		{Source: filepath.Join("provisioning", productidentity.BaseScriptName), Name: productidentity.BaseScriptName},
		{Source: filepath.Join("provisioning", productidentity.StackScriptName), Name: productidentity.StackScriptName},
		{Source: productidentity.LicenseSourceName, Name: productidentity.LicenseName},
	} {
		data, err := os.ReadFile(asset.Source)
		if err != nil {
			return fmt.Errorf("read build asset %s: %w", asset.Name, err)
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(output), asset.Name), data, 0o644); err != nil {
			return fmt.Errorf("write build asset %s: %w", asset.Name, err)
		}
	}
	if _, err := fmt.Fprintf(stdout, "Intermediate CLI build: %s\n", filepath.Clean(output)); err != nil {
		return fmt.Errorf("write intermediate build result: %w", err)
	}
	return nil
}

func goBuildArgs(output string, identity buildIdentity) []string {
	linkerFlags := fmt.Sprintf("-s -w -X herdr-sandbox/internal/productidentity.Version=%s -X herdr-sandbox/internal/productidentity.Revision=%s -X herdr-sandbox/internal/productidentity.BuildFreshness=%s", identity.Version, identity.Revision, identity.Freshness)
	return []string{
		"build",
		"-trimpath",
		"-buildvcs=false",
		"-ldflags", linkerFlags,
		"-o", output,
		"./cmd/sandbox",
	}
}

func runCommand(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
	command := hiddenCommandContext(ctx, name, args...)
	command.Stdout = stdout
	command.Stderr = stderr
	command.Stdin = os.Stdin
	if err := command.Run(); err != nil {
		return fmt.Errorf("run %s: %w", commandText(name, args), err)
	}
	return nil
}

func commandText(name string, args []string) string {
	parts := append([]string{name}, args...)
	for i, part := range parts {
		if strings.ContainsAny(part, " \t\"") {
			parts[i] = fmt.Sprintf("%q", part)
		}
	}
	return strings.Join(parts, " ")
}
