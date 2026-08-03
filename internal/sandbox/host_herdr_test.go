package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	hostHerdrFixtureEnvironment        = "HERDR_SANDBOX_TEST_HOST_HERDR"
	hostHerdrRemoteExitCodeEnvironment = "HERDR_SANDBOX_TEST_HOST_HERDR_REMOTE_EXIT_CODE"
)

func TestMain(m *testing.M) {
	if os.Getenv(hostHerdrFixtureEnvironment) == "1" {
		runHostHerdrFixtureProcess()
		return
	}
	os.Exit(m.Run())
}

func runHostHerdrFixtureProcess() {
	arguments := os.Args[1:]
	switch {
	case len(arguments) == 1 && arguments[0] == "--version":
		fmt.Fprintln(os.Stdout, "herdr "+os.Getenv("HERDR_SANDBOX_TEST_HOST_HERDR_VERSION"))
		os.Exit(0)
	case len(arguments) == 3 && arguments[0] == "status" && arguments[1] == "client" && arguments[2] == "--json":
		protocol := 0
		_, _ = fmt.Sscanf(os.Getenv("HERDR_SANDBOX_TEST_HOST_HERDR_PROTOCOL"), "%d", &protocol)
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"version":  os.Getenv("HERDR_SANDBOX_TEST_HOST_HERDR_VERSION"),
			"channel":  "preview",
			"protocol": protocol,
			"binary":   os.Getenv("HERDR_SANDBOX_TEST_HOST_HERDR_RUNTIME"),
			"session":  nil,
		})
		os.Exit(0)
	case len(arguments) == 1 && arguments[0] == "--remote":
		fmt.Fprintln(os.Stderr, "error: missing value for --remote")
		os.Exit(2)
	case len(arguments) == 2 && arguments[0] == "--remote":
		if diagnostic := os.Getenv("HERDR_SANDBOX_TEST_HOST_HERDR_REMOTE"); diagnostic != "" {
			fmt.Fprintln(os.Stderr, diagnostic)
		}
		exitCode := 1
		if configured := os.Getenv(hostHerdrRemoteExitCodeEnvironment); configured != "" {
			_, _ = fmt.Sscanf(configured, "%d", &exitCode)
		}
		os.Exit(exitCode)
	default:
		fmt.Fprintf(os.Stderr, "unexpected fixture arguments: %q\n", arguments)
		os.Exit(2)
	}
}

func TestResolveHostHerdrUsesReportedPhysicalRuntimeAndSnapshotsIt(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows executable fixture")
	}
	commandPath, runtimePath := prepareHostHerdrFixture(t)
	host, err := ResolveHostHerdr(context.Background())
	if err != nil {
		t.Fatalf("ResolveHostHerdr: %v", err)
	}
	for _, expected := range []struct {
		role string
		want string
		got  string
	}{
		{role: "command", want: commandPath, got: host.commandPath},
		{role: "runtime", want: runtimePath, got: host.runtimeExecutable},
	} {
		wantInfo, statErr := os.Stat(expected.want)
		if statErr != nil {
			t.Fatalf("stat expected host Herdr %s %q: %v", expected.role, expected.want, statErr)
		}
		gotInfo, statErr := os.Stat(expected.got)
		if statErr != nil {
			t.Fatalf("stat resolved host Herdr %s %q: %v", expected.role, expected.got, statErr)
		}
		if !os.SameFile(wantInfo, gotInfo) {
			t.Fatalf("host Herdr %s paths identify different files: expected %q, got %q", expected.role, expected.want, expected.got)
		}
	}
	if host.version != "herdr 1.2.3-test" || host.protocol != 42 || len(host.commandSHA256) != 64 || host.commandSize <= 0 || len(host.files) != len(hostHerdrRuntimeLayout) {
		t.Fatalf("host identity = %#v", host)
	}

	inputDirectory := t.TempDir()
	if err := writeHostHerdrRunInput(context.Background(), host, inputDirectory); err != nil {
		t.Fatalf("writeHostHerdrRunInput: %v", err)
	}
	manifestData, err := os.ReadFile(filepath.Join(inputDirectory, "host-herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest hostHerdrManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 3 || manifest.Version != host.version || manifest.Protocol != host.protocol || len(manifest.Files) != len(hostHerdrRuntimeLayout) {
		t.Fatalf("manifest = %#v", manifest)
	}
	for _, file := range manifest.Files {
		if _, err := os.Stat(filepath.Join(inputDirectory, "herdr-runtime", filepath.FromSlash(file.Path))); err != nil {
			t.Fatalf("snapshotted %s: %v", file.Path, err)
		}
	}
}

func TestResolveHostHerdrRejectsUnsupportedCaseInsensitivelyWithCapabilityAction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows executable fixture")
	}
	prepareHostHerdrFixture(t)
	t.Setenv("HERDR_SANDBOX_TEST_HOST_HERDR_REMOTE", "Remote mode is UnSuPpOrTeD on this Windows build")
	_, err := ResolveHostHerdr(context.Background())
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unsupported") || !strings.Contains(err.Error(), hostHerdrCompatibilityAction) {
		t.Fatalf("unsupported remote error = %v", err)
	}
}

func TestResolveHostHerdrRejectsUnexpectedRemoteCapabilityFailures(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows executable fixture")
	}
	tests := []struct {
		name       string
		diagnostic string
		exitCode   string
	}{
		{name: "empty output", exitCode: "1"},
		{name: "panic", diagnostic: "thread 'main' panicked: ssh executable file not found", exitCode: "101"},
		{name: "unrelated failure", diagnostic: "error: configuration is corrupt", exitCode: "7"},
		{name: "unrelated SSH failure", diagnostic: "error: configuration failed before ssh program not found", exitCode: "7"},
		{name: "arbitrary missing program", diagnostic: "error: internal helper program not found", exitCode: "23"},
		{name: "unexpected success", diagnostic: "error: program not found", exitCode: "0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prepareHostHerdrFixture(t)
			t.Setenv("HERDR_SANDBOX_TEST_HOST_HERDR_REMOTE", test.diagnostic)
			t.Setenv(hostHerdrRemoteExitCodeEnvironment, test.exitCode)
			_, err := ResolveHostHerdr(context.Background())
			if err == nil || !strings.Contains(err.Error(), hostHerdrCompatibilityAction) {
				t.Fatalf("unexpected remote capability error = %v", err)
			}
		})
	}
}

func TestResolveHostHerdrMissingCommandNamesCapabilityAction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PATH semantics")
	}
	t.Setenv("PATH", t.TempDir())
	_, err := ResolveHostHerdr(context.Background())
	if err == nil || !strings.Contains(err.Error(), hostHerdrCompatibilityAction) {
		t.Fatalf("missing host Herdr error = %v", err)
	}
}

func TestHostHerdrVerifyUnchangedRejectsHostUpdateRace(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows executable fixture")
	}
	prepareHostHerdrFixture(t)
	host, err := ResolveHostHerdr(context.Background())
	if err != nil {
		t.Fatalf("ResolveHostHerdr: %v", err)
	}
	if err := host.verifyUnchanged(context.Background()); err != nil {
		t.Fatalf("unchanged host Herdr: %v", err)
	}
	t.Setenv("HERDR_SANDBOX_TEST_HOST_HERDR_VERSION", "1.2.4-test")
	if err := host.verifyUnchanged(context.Background()); err == nil || !strings.Contains(err.Error(), "changed during provisioning") {
		t.Fatalf("host update race error = %v", err)
	}
}

func TestHostHerdrIdentityIncludesCommandFingerprint(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows executable fixture")
	}
	prepareHostHerdrFixture(t)
	host, err := ResolveHostHerdr(context.Background())
	if err != nil {
		t.Fatalf("ResolveHostHerdr: %v", err)
	}
	changed := host
	changed.commandSHA256 = strings.Repeat("0", 64)
	if host.sameIdentity(changed) {
		t.Fatal("command digest change preserved host identity")
	}
	changed = host
	changed.commandSize++
	if host.sameIdentity(changed) {
		t.Fatal("command size change preserved host identity")
	}
}

func TestResolveHostHerdrAgainstInstalledRuntime(t *testing.T) {
	if os.Getenv("HERDR_SANDBOX_TEST_REAL_HOST_HERDR") != "1" {
		t.Skip("set HERDR_SANDBOX_TEST_REAL_HOST_HERDR=1 for the installed native boundary")
	}
	host, err := ResolveHostHerdr(context.Background())
	if err != nil {
		t.Fatalf("ResolveHostHerdr installed boundary: %v", err)
	}
	inputDirectory := t.TempDir()
	if err := writeHostHerdrRunInput(context.Background(), host, inputDirectory); err != nil {
		t.Fatalf("snapshot installed host Herdr: %v", err)
	}
	snapshotCommand := filepath.Join(inputDirectory, "herdr-runtime", "herdr.exe")
	output, err := hiddenCommand(snapshotCommand, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("run snapshotted host Herdr: %v: %s", err, output)
	}
	if actual := strings.TrimSpace(string(output)); actual != host.version {
		t.Fatalf("snapshotted host Herdr version = %q, want %q", actual, host.version)
	}
	t.Logf("resolved %s protocol %d from %s", host.version, host.protocol, host.runtimeExecutable)
}

func TestRemoteUnsupportedDiagnosticAcceptsCaseAndNotSupportedPhrase(t *testing.T) {
	for _, diagnostic := range []string{
		"UNSUPPORTED",
		"prefix UnSuPpOrTeD suffix",
		"remote mode is not supported on Windows yet",
	} {
		if !remoteUnsupportedDiagnostic([]byte(diagnostic)) {
			t.Fatalf("diagnostic was not classified as unsupported: %q", diagnostic)
		}
	}
	if remoteUnsupportedDiagnostic([]byte("missing value for --remote")) {
		t.Fatal("missing-target diagnostic was classified as unsupported")
	}
}

func TestExpectedSSHLookupFailureAcceptsOnlyTheLookupBoundary(t *testing.T) {
	for _, diagnostic := range []string{
		"error: program not found",
		"\r\nerror: program not found\r\n",
	} {
		if !expectedSSHLookupFailure([]byte(diagnostic)) {
			t.Fatalf("expected SSH lookup diagnostic was rejected: %q", diagnostic)
		}
	}
	for _, diagnostic := range []string{
		"",
		"thread 'main' panicked",
		"thread 'main' panicked: ssh executable file not found",
		"error: configuration is corrupt",
		"error: configuration failed before ssh program not found",
		"error: internal helper program not found",
		"error: failed to start ssh.exe: program not found",
		"ssh executable file not found in PATH",
		"ssh.exe: The system cannot find the file specified",
		"error: missing value for --remote",
	} {
		if expectedSSHLookupFailure([]byte(diagnostic)) {
			t.Fatalf("unexpected diagnostic was accepted as an SSH lookup failure: %q", diagnostic)
		}
	}
}

func TestParseHostHerdrClientStatusRejectsInvalidIdentityAndTrailingData(t *testing.T) {
	valid := []byte(`{"version":"1.2.3","channel":"preview","protocol":42,"binary":"C:\\Herdr\\herdr.exe","session":null}`)
	status, err := parseHostHerdrClientStatus(valid)
	if err != nil || status.Version != "1.2.3" || status.Protocol != 42 {
		t.Fatalf("valid status = %#v, %v", status, err)
	}
	for _, invalid := range [][]byte{
		append(append([]byte{}, valid...), []byte(` {}`)...),
		[]byte(`{"version":"herdr 1.2.3","protocol":42,"binary":"C:\\Herdr\\herdr.exe"}`),
		[]byte(`{"version":"1.2.3","protocol":0,"binary":"C:\\Herdr\\herdr.exe"}`),
		[]byte(`{"version":"1.2.3","protocol":42,"binary":"relative\\herdr.exe"}`),
	} {
		if _, err := parseHostHerdrClientStatus(invalid); err == nil {
			t.Fatalf("invalid status was accepted: %s", invalid)
		}
	}
}

func TestInspectHostHerdrRuntimeFilesAcceptsStandaloneOrCompleteConPTYBundle(t *testing.T) {
	root := t.TempDir()
	writeHostHerdrFixtureFile(t, filepath.Join(root, "herdr.exe"), "herdr")
	files, err := inspectHostHerdrRuntimeFiles(filepath.Join(root, "herdr.exe"))
	if err != nil || len(files) != 1 {
		t.Fatalf("standalone runtime files = %#v, %v", files, err)
	}
	for _, relative := range hostHerdrRuntimeLayout[1:] {
		writeHostHerdrFixtureFile(t, filepath.Join(root, filepath.FromSlash(relative)), relative)
	}
	files, err = inspectHostHerdrRuntimeFiles(filepath.Join(root, "herdr.exe"))
	if err != nil || len(files) != len(hostHerdrRuntimeLayout) {
		t.Fatalf("bundled runtime files = %#v, %v", files, err)
	}
	if err := os.Remove(filepath.Join(root, "conpty", "arm64", "OpenConsole.exe")); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectHostHerdrRuntimeFiles(filepath.Join(root, "herdr.exe")); err == nil {
		t.Fatal("partial ConPTY runtime unexpectedly passed")
	}
}

func TestInspectHostHerdrRuntimeFilesRejectsReparseBundleDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	for _, relative := range hostHerdrRuntimeLayout {
		if strings.HasPrefix(relative, "conpty/") {
			writeHostHerdrFixtureFile(t, filepath.Join(outside, filepath.FromSlash(relative)), relative)
			continue
		}
		writeHostHerdrFixtureFile(t, filepath.Join(root, filepath.FromSlash(relative)), relative)
	}
	createTestDirectoryLink(t, filepath.Join(root, "conpty"), filepath.Join(outside, "conpty"))
	if _, err := inspectHostHerdrRuntimeFiles(filepath.Join(root, "herdr.exe")); err == nil || !strings.Contains(strings.ToLower(err.Error()), "reparse") {
		t.Fatalf("reparse runtime error = %v", err)
	}
}

func TestReplaceFileAtomicallyPreservesTargetAndBackup(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	replacement := filepath.Join(directory, "replacement")
	backup := filepath.Join(directory, "backup")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceFileAtomically(target, replacement, backup); err != nil {
		t.Fatalf("replaceFileAtomically: %v", err)
	}
	assertTestFileContents(t, target, "new")
	assertTestFileContents(t, backup, "old")
}

func prepareHostHerdrFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	commandPath := filepath.Join(root, "command", "herdr.exe")
	runtimePath := filepath.Join(root, "physical-runtime", "herdr.exe")
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	copyHostHerdrFixtureExecutable(t, testExecutable, commandPath)
	copyHostHerdrFixtureExecutable(t, testExecutable, runtimePath)
	for _, relative := range hostHerdrRuntimeLayout[1:] {
		writeHostHerdrFixtureFile(t, filepath.Join(filepath.Dir(runtimePath), filepath.FromSlash(relative)), relative)
	}
	t.Setenv(hostHerdrFixtureEnvironment, "1")
	t.Setenv("HERDR_SANDBOX_TEST_HOST_HERDR_VERSION", "1.2.3-test")
	t.Setenv("HERDR_SANDBOX_TEST_HOST_HERDR_PROTOCOL", "42")
	t.Setenv("HERDR_SANDBOX_TEST_HOST_HERDR_RUNTIME", runtimePath)
	t.Setenv("HERDR_SANDBOX_TEST_HOST_HERDR_REMOTE", "error: program not found")
	t.Setenv(hostHerdrRemoteExitCodeEnvironment, "1")
	t.Setenv("PATH", filepath.Dir(commandPath))
	return commandPath, runtimePath
}

func copyHostHerdrFixtureExecutable(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeHostHerdrFixtureFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertTestFileContents(t *testing.T, path, expected string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != expected {
		t.Fatalf("%s contents = %q, expected %q", filepath.Base(path), contents, expected)
	}
}
