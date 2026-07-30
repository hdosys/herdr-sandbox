package sandbox

import (
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
		HerdrVersion:  "herdr 1.0.0",
		HerdrProtocol: 18,
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

func TestGuestHerdrStatusScriptResolvesVerifiedApplicationFromPath(t *testing.T) {
	script := guestHerdrStatusScript()
	for _, required := range []string{
		"Get-Command -Name 'herdr.exe' -CommandType Application",
		guestHerdrPath,
		"& $resolvedPath status server",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("guest Herdr status script is missing %q: %s", required, script)
		}
	}
	if strings.Contains(script, "& '"+guestHerdrPath+"'") {
		t.Fatalf("guest Herdr status script bypasses PATH: %s", script)
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

func TestDefaultDataDirectoryRequiresLocalAppData(t *testing.T) {
	t.Setenv("LOCALAPPDATA", "")
	if _, err := defaultDataDirectory(); err == nil {
		t.Fatal("defaultDataDirectory unexpectedly succeeded")
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
