package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func TestHerdrConfigurationUsesSelectedSourceOnlyForGuestOverrides(t *testing.T) {
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	selected := filepath.Join(root, "selected.toml")
	t.Setenv("HERDR_CONFIG_PATH", selected)
	writeTestFile(t, selected, "[\"terminal\"]\n\"default_shell\" = 'nu'\n[theme]\nname = 'host-theme'\n[agent]\nargs = ['host-only']\n")
	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(wingetPackageConfiguration{Add: []string{}}, terminal)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := defaultHostConfigurationSources(terminal, packages, codingAgentSyncConfiguration{}, credentialSyncConfiguration{}, false)
	if err != nil || sources.HerdrConfig != selected {
		t.Fatalf("selected source = %q, error = %v", sources.HerdrConfig, err)
	}
	guest := []byte("[terminal]\nnew_cwd = 'C:/Guest'\n[theme]\nname = 'guest-theme'\n[agent]\nargs = ['guest-only']\n")
	patched, err := buildGuestHerdrConfig(sources.HerdrConfig, guest, guestWorktreeDirectory)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`default_shell = "nu.exe"`, `directory = "C:/Worktrees"`, `new_cwd = "C:/Guest"`, `name = "guest-theme"`, `args = ["guest-only"]`} {
		if !bytes.Contains(patched, []byte(want)) {
			t.Fatalf("guest override lost %q: %s", want, patched)
		}
	}
	if bytes.Contains(patched, []byte("host-theme")) || bytes.Contains(patched, []byte("host-only")) {
		t.Fatal("Sandbox copied host settings owned by Herdr or the target machine")
	}
	for _, snapshot := range []struct {
		input  string
		absent bool
	}{{"missing", true}, {"present:", false}, {"present:" + base64.StdEncoding.EncodeToString(guest), false}} {
		got, err := decodeGuestHerdrConfiguration([]byte(snapshot.input))
		if err != nil || (got == nil) != snapshot.absent {
			t.Fatalf("snapshot presence: %v, %v", got == nil, err)
		}
	}
}

func TestGuestHerdrOverridesPreserveTOMLValues(t *testing.T) {
	for name, input := range map[string]string{
		"quoted keys":     "[terminal]\n\"default_shell\" = 'nu.exe'\nnew_cwd = 'C:/Guest'\n[worktrees]\n'directory' = 'D:/old'\n",
		"quoted tables":   "[\"terminal\"]\ndefault_shell = 'nu.exe'\n['worktrees']\ndirectory = 'D:/old'\n",
		"dotted keys":     "terminal.default_shell = 'nu.exe'\nworktrees.directory = 'D:/old'\n",
		"inline tables":   "terminal = { default_shell = 'nu.exe', new_cwd = 'C:/Guest' }\nworktrees = { directory = 'D:/old', include_repo_name = true }\n",
		"multiline value": "[agent]\nargs = ['''\n[terminal]\ndefault_shell = 'not-a-table'\n''']\n",
	} {
		t.Run(name, func(t *testing.T) {
			input += "\n[theme]\nname = 'dracula'\n"
			var want map[string]any
			if _, err := toml.Decode(input, &want); err != nil {
				t.Fatal(err)
			}
			patched, err := patchGuestHerdrConfig([]byte(input), guestWorktreeDirectory, "pwsh.exe")
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if _, err := toml.Decode(string(patched), &got); err != nil {
				t.Fatalf("published TOML would be invalid: %v", err)
			}
			for _, target := range []struct{ section, key, value string }{{"terminal", "default_shell", "pwsh.exe"}, {"worktrees", "directory", "C:/Worktrees"}} {
				section, err := configurationObject(want, target.section)
				if err != nil {
					t.Fatal(err)
				}
				section[target.key] = target.value
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("guest override changed unrelated values: got %#v, want %#v", got, want)
			}
		})
	}
	for _, invalid := range []string{"[terminal]\ndefault_shell = 'nu'\n\"default_shell\" = 'pwsh'\n", "terminal = 'not a table'", "[terminal\n"} {
		if output, err := patchGuestHerdrConfig([]byte(invalid), guestWorktreeDirectory, "pwsh.exe"); err == nil || output != nil {
			t.Fatalf("invalid guest config produced publishable bytes: %q, %v", output, err)
		}
	}
}

func TestHerdrGuestConfigurationSnapshotAndPublicationInWindowsPowerShell51(t *testing.T) {
	requireExternalBoundaryTest(t, "guest Herdr configuration publication")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	config := filepath.Join(root, "herdr", "config.toml")
	readScript := filepath.Join(root, "read.ps1")
	writeTestFile(t, readScript, guestHerdrConfigurationReadScript)
	readSnapshot := func() []byte {
		t.Helper()
		command := hiddenCommandContext(ctx, mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", readScript)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("read snapshot: %v: %s", err, output)
		}
		data, err := decodeGuestHerdrConfiguration(output)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if readSnapshot() != nil {
		t.Fatal("missing configuration was not represented as absent")
	}
	original := "[agent]\nargs = ['guest-local']\n"
	if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, config, original)
	if string(readSnapshot()) != original {
		t.Fatal("guest configuration did not round trip")
	}
	patched, err := patchGuestHerdrConfig([]byte(original), guestWorktreeDirectory, "nu.exe")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "patched.toml")
	writeTestFile(t, source, string(patched))
	start := bytes.Index(configurationSyncScript, []byte("$script:CopiedConfigurationFiles = 0"))
	end := bytes.Index(configurationSyncScript, []byte("function Copy-VerifiedConfigurationTree"))
	if start < 0 || end <= start {
		t.Fatal("configuration writer helper not found")
	}
	t.Setenv("TEST_HERDR_SOURCE", source)
	t.Setenv("TEST_HERDR_DESTINATION", config)
	t.Setenv("TEST_HERDR_EXPECTED", fmt.Sprintf("%x", sha256.Sum256([]byte(original))))
	applyScript := filepath.Join(root, "apply.ps1")
	writeTestFile(t, applyScript, "$ErrorActionPreference = 'Stop'\n"+string(configurationSyncScript[start:end])+`
Set-AtomicConfigurationFile -Source $env:TEST_HERDR_SOURCE -Destination $env:TEST_HERDR_DESTINATION -ExpectedSHA256 $env:TEST_HERDR_EXPECTED
$rejected = $false
try {
    Set-AtomicConfigurationFile -Source $env:TEST_HERDR_SOURCE -Destination $env:TEST_HERDR_DESTINATION -ExpectedSHA256 $env:TEST_HERDR_EXPECTED
} catch {
    if ($_.Exception.Message -notlike '*changed during sync*') { throw }
    $rejected = $true
}
if (-not $rejected) { throw 'A stale guest snapshot overwrote a newer configuration.' }
`)
	command := hiddenCommandContext(ctx, mustWindowsPowerShellPath(t), "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", applyScript)
	// The production SSH launcher removes the interactive shell's PSModulePath
	// before starting Windows PowerShell 5.1. Exercise that same environment.
	command.Env = slices.DeleteFunc(os.Environ(), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		return strings.EqualFold(name, "PSModulePath")
	})
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("publish guest overrides: %v: %s", err, output)
	}
	if !bytes.Equal(readSnapshot(), patched) {
		t.Fatal("published configuration changed after stale-snapshot rejection")
	}
}

func TestGuestHerdrOverridesSurviveNativeProvisionImport(t *testing.T) {
	executable := os.Getenv("HERDR_SANDBOX_TEST_CONFIG_IMPORT_HERDR")
	if executable == "" {
		t.Skip("set HERDR_SANDBOX_TEST_CONFIG_IMPORT_HERDR to the matching packaged runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	guest, err := patchGuestHerdrConfig([]byte("[agent]\nargs = ['guest-local']\n"), guestWorktreeDirectory, "nu.exe")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, string(guest))
	t.Setenv("HERDR_CONFIG_PATH", path)
	// Native import receives the same encoding as Herdr's export owner. It must
	// preserve our overlay even when incoming machine-local values are present.
	incoming := "[terminal]\ndefault_shell = 'host-shell'\n[worktrees]\ndirectory = 'D:/Host'\n[agent]\nargs = ['host-local']\n[theme]\nname = 'dracula'\n"
	command := hiddenCommandContext(ctx, executable, "config", "provision-import", "--overwrite")
	command.Env = attachEnvironment(childProcessEnvironment(os.Environ()))
	command.Env = slices.DeleteFunc(command.Env, func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		return strings.EqualFold(name, "HERDR_REMOTE_SIDECAR_V1")
	})
	command.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString([]byte(incoming)))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("native Herdr import: %v: %s", err, output)
	} else if strings.TrimSpace(string(output)) != `"applied"` {
		t.Fatalf("native Herdr import outcome = %q, want applied", output)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"nu.exe", "C:/Worktrees", "guest-local", "dracula"} {
		if !bytes.Contains(saved, []byte(want)) {
			t.Fatalf("native imported config lost %q: %s", want, saved)
		}
	}
	if bytes.Contains(saved, []byte("host-local")) || bytes.Contains(saved, []byte("D:/Host")) {
		t.Fatal("native import overwrote Sandbox-owned or guest-local settings")
	}
}

// This executes the production archive, launcher and apply script together.
// Optional tools are absent from the child PATH, and every destination is a
// synthetic profile. It covers metadata/count drift missed by helper tests.
func TestHerdrConfigurationArchiveAppliesInIsolatedProfile(t *testing.T) {
	requireExternalBoundaryTest(t, "isolated configuration archive application")
	root := t.TempDir()
	guestRoot := filepath.Join(root, "guest")
	profile := filepath.Join(root, "profile")
	appData := filepath.Join(profile, "AppData", "Roaming")
	config := filepath.Join(appData, "herdr", "config.toml")
	if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USERPROFILE", profile)
	t.Setenv("APPDATA", appData)
	t.Setenv("LOCALAPPDATA", filepath.Join(profile, "AppData", "Local"))
	t.Setenv("PATH", filepath.Join(os.Getenv("SystemRoot"), "System32"))
	terminal := testStableWindowsTerminalConfiguration()
	packages, err := resolveWingetPackagePlan(wingetPackageConfiguration{
		Remove: []string{packageGit, packageGitHubCLI, packageStarship, packageTerminalStable},
		Add:    []string{}, Versions: map[string]string{},
	}, terminal)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := encodeWingetPackagePlan(packages, terminal)
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(root, "packages.json")
	writeTestFile(t, planPath, string(plan))
	hostConfig := filepath.Join(root, "host.toml")
	writeTestFile(t, hostConfig, "[terminal]\n'default_shell' = 'nu'\n[theme]\nname = 'host-only'\n")
	writeTestFile(t, config, "['terminal']\n'default_shell' = 'old'\n[agent]\nargs = ['guest-only']\n")
	for attempt := range 2 {
		snapshot, err := os.ReadFile(config)
		if err != nil {
			t.Fatal(err)
		}
		archive, err := buildDevelopmentConfigurationArchive(t.Context(), hostConfigurationSources{
			HerdrConfig: hostConfig, GuestHerdrConfig: snapshot, PackagePlan: planPath,
		}, configurationSyncScript)
		if err != nil {
			t.Fatal(err)
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(archive))
		launcherScript := strings.ReplaceAll(buildDevelopmentConfigurationLauncher(digest, len(archive)), guestRootDirectory, guestRoot)
		launcher := sshArchiveLauncherBytes(launcherScript)
		command := buildSSHArchiveTransportCommand(digest, len(archive), launcher, 30*time.Second, fmt.Sprintf("config-%d", attempt))
		command = strings.ReplaceAll(command, guestRootDirectory, guestRoot)
		output, err := runLocalSSHTransport(t, command, launcher, archive, nil)
		if err != nil {
			t.Fatalf("apply real configuration archive: %v: %s", err, output)
		}
		// The local transport helper combines diagnostic stderr with stdout.
		var resultJSON []byte
		for line := range bytes.Lines(output) {
			if bytes.HasPrefix(line, []byte("{")) {
				if resultJSON != nil {
					t.Fatal("configuration script returned multiple result objects")
				}
				resultJSON = line
			}
		}
		result, err := decodeDevelopmentConfigurationSyncResult(resultJSON)
		count, countErr := configurationArchivePayloadFileCount(archive)
		if err != nil || countErr != nil || result.SchemaVersion != 9 || result.ArchiveSHA256 != digest || !result.HerdrConfigurationPublished || result.CopiedFiles != count {
			t.Fatalf("configuration apply result mismatch: %#v, %v, %v: %s", result, err, countErr, output)
		}
		actual, err := os.ReadFile(config)
		if err != nil || !bytes.Contains(actual, []byte("nu.exe")) || !bytes.Contains(actual, []byte("guest-only")) || bytes.Contains(actual, []byte("host-only")) {
			t.Fatalf("guest configuration was not preserved: %q, %v", actual, err)
		}
	}
}
