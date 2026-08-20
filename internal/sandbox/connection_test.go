package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderSSHConfigPinsIdentityAndHostKey(t *testing.T) {
	status := connectionStatus{IP: "172.24.16.3", SSHUser: "WDAGUtilityAccount"}
	config := renderSSHConfig(status, `C:\Users\A Person\key`, `C:\Runs\one\known_hosts`, "windows-sandbox-one")
	checks := []string{
		"Host sandbox",
		"HostName 172.24.16.3",
		"User WDAGUtilityAccount",
		`IdentityFile "C:/Users/A Person/key"`,
		`UserKnownHostsFile "C:/Runs/one/known_hosts"`,
		"HostKeyAlias windows-sandbox-one",
		"ForwardAgent no",
		"ControlMaster no",
		"ControlPath none",
		"ControlPersist no",
		"StrictHostKeyChecking yes",
		"PasswordAuthentication no",
	}
	for _, check := range checks {
		if !strings.Contains(config, check) {
			t.Fatalf("SSH config missing %q:\n%s", check, config)
		}
	}
}

func TestWriteRunConnectionDoesNotPublishStableAlias(t *testing.T) {
	root := t.TempDir()
	runDirectory := filepath.Join(root, "runs", "20260724-123456-abcdef12")
	if err := os.MkdirAll(runDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	plan := runPlan{
		ID:              "20260724-123456-abcdef12",
		DataDirectory:   filepath.Join(root, "data"),
		RunDirectory:    runDirectory,
		StatusDirectory: filepath.Join(runDirectory, "status"),
		PrivateKeyPath:  filepath.Join(root, "id_ed25519"),
	}
	connectable := connectableStatus{
		SchemaVersion: statusSchemaVersion,
		IP:            "172.24.16.3",
		SSHUser:       "WDAGUtilityAccount",
		SSHHostKey:    testHostKey,
		WinGetVersion: "v1",
	}
	connection, err := writeRunConnection(plan, connectable, `C:\Herdr\herdr.exe`)
	if err != nil {
		t.Fatalf("writeRunConnection: %v", err)
	}
	if _, err := os.Stat(connection.SSHConfigPath); err != nil {
		t.Fatalf("run SSH config: %v", err)
	}
	if _, err := os.Stat(filepath.Join(plan.DataDirectory, "ssh", "config")); !os.IsNotExist(err) {
		t.Fatalf("stable alias was published before verification: %v", err)
	}
}

func TestGuestHerdrStatusScriptUsesPublishedProvisionedBinary(t *testing.T) {
	script := guestHerdrStatusScript()
	for _, required := range []string{
		"HERDR_SANDBOX_HERDR_EXE",
		"GetEnvironmentVariable",
		"& $herdr status server --json",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("guest Herdr status script is missing %q: %s", required, script)
		}
	}
	if strings.Contains(script, "Get-Command -Name 'herdr.exe'") || strings.Contains(script, `C:\HerdrSandbox\bin`) {
		t.Fatalf("guest Herdr status script retains the replaced Sandbox install path: %s", script)
	}
}

func TestGuestHerdrPublicationAddsExactSidecarDirectoryToMachinePath(t *testing.T) {
	executable := `C:\Users\WDAGUtilityAccount\.herdr\remote\build-id\herdr.exe`
	script := guestHerdrPublicationScript(executable)
	for _, required := range []string{
		"SetEnvironmentVariable('HERDR_SANDBOX_HERDR_EXE', $path, 'Machine')",
		"SetEnvironmentVariable('Path'",
		"$publishedEntries[0]",
		"Count -ne 1",
		"Get-Command herdr.exe",
		"Guest Herdr PATH resolution failed.",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("guest Herdr publication script is missing %q: %s", required, script)
		}
	}
	if !strings.Contains(script, executable) || strings.Contains(script, `C:\HerdrSandbox\bin`) {
		t.Fatalf("guest Herdr publication script uses the wrong binary owner: %s", script)
	}
}

func TestDecodeGuestHerdrStatusRequiresExactShape(t *testing.T) {
	valid := []byte(`{"status":"running","running":true,"version":"0.8.0+build","protocol":42,"binary":"C:\\Users\\WDAGUtilityAccount\\.herdr\\remote\\build\\herdr.exe","capabilities":{"live_handoff":false,"detached_server_daemon":true},"compatible":true,"socket":"C:\\Users\\WDAGUtilityAccount\\.herdr\\herdr.sock","session":null,"restart_needed":false}`)
	status, err := decodeGuestHerdrStatus(valid)
	if err != nil || !status.Running || status.Capabilities == nil || !status.Capabilities.DetachedServerDaemon {
		t.Fatalf("guest status = %#v, err = %v", status, err)
	}
	for _, invalid := range [][]byte{
		append(append([]byte{}, valid...), []byte(` {}`)...),
		[]byte(strings.Replace(string(valid), `"protocol":42`, `"protocol":"42"`, 1)),
		[]byte(strings.Replace(string(valid), `"restart_needed":false`, `"extra":true,"restart_needed":false`, 1)),
	} {
		if _, err := decodeGuestHerdrStatus(invalid); err == nil {
			t.Fatalf("invalid guest status passed: %s", invalid)
		}
	}
}

func TestUpdateManagedSSHIncludeIsFirstAndIdempotent(t *testing.T) {
	existing := "Host work\n    HostName work.example\n"
	managedPath := `C:\Users\Test User\AppData\Local\herdr-sandbox\ssh\config`

	updated, err := updateManagedSSHInclude(existing, managedPath)
	if err != nil {
		t.Fatalf("updateManagedSSHInclude: %v", err)
	}
	expectedPrefix := "# BEGIN herdr-sandbox managed SSH include\n" +
		`Include "C:/Users/Test User/AppData/Local/herdr-sandbox/ssh/config"` + "\n" +
		"# END herdr-sandbox managed SSH include\n\n"
	if !strings.HasPrefix(updated, expectedPrefix) {
		t.Fatalf("managed include is not first:\n%s", updated)
	}
	if !strings.HasSuffix(updated, existing) {
		t.Fatalf("existing SSH config changed:\n%s", updated)
	}

	again, err := updateManagedSSHInclude(updated, managedPath)
	if err != nil {
		t.Fatalf("second updateManagedSSHInclude: %v", err)
	}
	if again != updated {
		t.Fatalf("managed include update is not idempotent:\n%s", again)
	}
}

func TestUpdateManagedSSHIncludeRejectsMalformedMarkers(t *testing.T) {
	_, err := updateManagedSSHInclude(managedSSHIncludeStart+"\n", `C:\sandbox\config`)
	if err == nil {
		t.Fatal("updateManagedSSHInclude accepted an incomplete managed block")
	}
}

func TestInstallSSHHostAliasPreservesUserConfig(t *testing.T) {
	dataDirectory := filepath.Join(t.TempDir(), "data")
	userHome := filepath.Join(t.TempDir(), "home")
	userSSHDirectory := filepath.Join(userHome, ".ssh")
	if err := os.MkdirAll(userSSHDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	existing := "Host work\n    HostName work.example\n"
	userConfigPath := filepath.Join(userSSHDirectory, "config")
	if err := os.WriteFile(userConfigPath, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := installSSHHostAliasAt(dataDirectory, userHome, "Host sandbox\n    HostName 172.24.1.2\n"); err != nil {
		t.Fatalf("installSSHHostAliasAt: %v", err)
	}
	managedPath := filepath.Join(dataDirectory, "ssh", "config")
	managed, err := os.ReadFile(managedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(managed), "Host sandbox\n") {
		t.Fatalf("managed config is missing sandbox target:\n%s", managed)
	}
	userConfig, err := os.ReadFile(userConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(userConfig), existing) {
		t.Fatalf("user SSH config was not preserved:\n%s", userConfig)
	}
}

func TestAtomicSSHConfigWriteRejectsConcurrentContentChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("concurrent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := writeFileAtomicallyIfUnchanged(path, []byte("initial\n"), []byte("replacement\n"), 0o600)
	if !errors.Is(err, errAtomicWriteTargetChanged) {
		t.Fatalf("conditional atomic write error = %v, want changed target", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "concurrent\n" {
		t.Fatalf("conditional atomic write replaced concurrent contents: %q", contents)
	}
}

func TestDefaultDataDirectoryRequiresLocalAppData(t *testing.T) {
	t.Setenv("LOCALAPPDATA", "")
	if _, err := defaultDataDirectory(); err == nil {
		t.Fatal("defaultDataDirectory unexpectedly succeeded")
	}
}

func TestDefaultDataDirectoryUsesApplicationName(t *testing.T) {
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	directory, err := defaultDataDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(localAppData, applicationName); directory != want {
		t.Fatalf("defaultDataDirectory = %q, want %q", directory, want)
	}
}

func TestValidateEd25519PublicKey(t *testing.T) {
	if err := validateEd25519PublicKey(testHostKey + " comment"); err != nil {
		t.Fatalf("validateEd25519PublicKey: %v", err)
	}
	if err := validateEd25519PublicKey("ssh-rsa AAAA"); err == nil {
		t.Fatal("validateEd25519PublicKey accepted RSA")
	}
}
