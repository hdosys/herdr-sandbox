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
	taskTimeout                = 30 * time.Minute
	nativeAllStacksTaskTimeout = 2 * time.Hour
)

var usage = fmt.Sprintf(`Usage: go run ./cmd/task <task>

Tasks:
  fmt              format Go source
  test [args...]   run go test ./... with optional extra arguments
  build            build build/bin/%s
  native-all-stacks build and test all built-in stacks in one real Windows Sandbox
  release-notes VERSION  print curated notes from the matching CHANGELOG section
  package VERSION  build the canonical ZIP and NSIS installer release artifacts
  check            check format, PowerShell syntax, tests, vet, and build
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
		goArgs := append([]string{"test", "./..."}, extra...)
		return runCommand(ctx, stdout, stderr, "go", goArgs...)
	case "build":
		if len(args) != 1 {
			return errors.New("build accepts no arguments")
		}
		return build(ctx, stdout, stderr)
	case "native-all-stacks":
		if len(args) != 1 {
			return errors.New("native-all-stacks accepts no arguments")
		}
		return nativeAllStacks(ctx, stdout, stderr)
	case "package":
		if len(args) != 2 {
			return errors.New("package requires one v0.0.RELEASE_ID version")
		}
		return packageWindowsRelease(ctx, args[1], stdout, stderr)
	case "release-notes":
		if len(args) != 2 {
			return errors.New("release-notes requires one v0.0.RELEASE_ID version")
		}
		return writeReleaseNotes(args[1], stdout)
	case "check":
		if len(args) != 1 {
			return errors.New("check accepts no arguments")
		}
		return check(ctx, stdout, stderr)
	default:
		return fmt.Errorf("unknown task %q\n\n%s", args[0], usage)
	}
}

func taskTimeoutFor(args []string) time.Duration {
	if len(args) > 0 && args[0] == "native-all-stacks" {
		return nativeAllStacksTaskTimeout
	}
	return taskTimeout
}

func check(ctx context.Context, stdout, stderr io.Writer) error {
	if err := checkGoFormat(ctx, stderr); err != nil {
		return err
	}
	if err := checkPowerShell(ctx, stdout, stderr); err != nil {
		return err
	}
	for _, command := range [][]string{
		{"go", "test", "./..."},
		{"go", "vet", "./..."},
	} {
		if err := runCommand(ctx, stdout, stderr, command[0], command[1:]...); err != nil {
			return err
		}
	}
	return build(ctx, stdout, stderr)
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
		return fmt.Errorf("Go files need formatting:\n%s", unformatted)
	}
	return nil
}

func checkPowerShell(ctx context.Context, stdout, stderr io.Writer) error {
	scripts := []string{}
	for _, pattern := range []string{filepath.Join("internal", "sandbox", "assets", "*.ps1"), filepath.Join("provisioning", "*.ps1"), filepath.Join(".herdr-sandbox", "*.ps1"), filepath.Join("packaging", "windows", "*.ps1")} {
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
	parser := `$tokens = $null
$errors = $null
    [System.Management.Automation.Language.Parser]::ParseFile($env:HERDR_SANDBOX_POWERSHELL_SCRIPT, [ref]$tokens, [ref]$errors) | Out-Null
if ($errors.Count -gt 0) {
    $errors | ForEach-Object { [Console]::Error.WriteLine($_.Message) }
    exit 1
}`
	for _, relative := range scripts {
		script, err := filepath.Abs(relative)
		if err != nil {
			return fmt.Errorf("resolve PowerShell script %s: %w", relative, err)
		}
		if _, err := os.Stat(script); err != nil {
			return fmt.Errorf("find PowerShell script %s: %w", relative, err)
		}
		command := hiddenCommandContext(ctx, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", parser)
		command.Env = append(os.Environ(), "HERDR_SANDBOX_POWERSHELL_SCRIPT="+script)
		command.Stdout = stdout
		command.Stderr = stderr
		if err := command.Run(); err != nil {
			if runtime.GOOS != "windows" {
				return fmt.Errorf("validate Windows PowerShell script %s (run this gate on Windows): %w", relative, err)
			}
			return fmt.Errorf("validate Windows PowerShell script %s: %w", relative, err)
		}
	}
	return nil
}

func build(ctx context.Context, stdout, stderr io.Writer) error {
	identity := buildIdentity{Version: "devel", Revision: "unknown"}
	if revision, err := sourceRevision(ctx); err == nil {
		identity.Revision = revision
	}
	return buildWithIdentity(ctx, identity, stdout, stderr)
}

type buildIdentity struct {
	Version  string
	Revision string
}

func buildRelease(ctx context.Context, version releaseVersion, stdout, stderr io.Writer) error {
	revision, err := sourceRevision(ctx)
	if err != nil {
		return fmt.Errorf("resolve release source revision: %w", err)
	}
	return buildWithIdentity(ctx, buildIdentity{Version: version.Display, Revision: revision}, stdout, stderr)
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
		return "", fmt.Errorf("Git HEAD is not one full SHA-1 revision: %q", revision)
	}
	return strings.ToLower(revision), nil
}

func buildWithIdentity(ctx context.Context, identity buildIdentity, stdout, stderr io.Writer) error {
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
	return nil
}

func goBuildArgs(output string, identity buildIdentity) []string {
	linkerFlags := fmt.Sprintf("-s -w -X herdr-sandbox/internal/productidentity.Version=%s -X herdr-sandbox/internal/productidentity.Revision=%s", identity.Version, identity.Revision)
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
