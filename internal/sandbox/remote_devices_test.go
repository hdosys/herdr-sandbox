package sandbox

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoteDevicesPersistApprovedEditsWithoutLosingExistingKeys(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, globalConfigurationName)
	oldKey, newKey, replacement := testEd25519PublicKey(1), testEd25519PublicKey(2), testEd25519PublicKey(3)
	original := []byte(`{"memoryMB":8192,"audio":true,"tailscale":true,"remoteSSHAuthorizedKeys":["` + oldKey + `"],"workspaces":{}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	add := RemoteChange{Action: "add", Name: "Laptop", PublicKey: newKey + " device comment"}
	if _, saved, err := changeRemoteAccessAt(t.Context(), root, add, func(RemoteSettings) bool { return false }, ""); err != nil || saved {
		t.Fatalf("declined edit: %t %v", saved, err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, original) {
		t.Fatal("declined edit changed configuration")
	}
	apply := func(change RemoteChange) RemoteSettings {
		t.Helper()
		result, saved, err := changeRemoteAccessAt(t.Context(), root, change, func(RemoteSettings) bool { return true }, "")
		if err != nil || !saved {
			t.Fatalf("%s: saved=%t error=%v", change.Action, saved, err)
		}
		config, err := loadGlobalConfiguration(path)
		if err != nil {
			t.Fatal(err)
		}
		reloaded, err := remoteSettingsFromConfiguration(config)
		if err != nil || !sameRemoteSSHAuthorizedKeys(reloaded.AuthorizedKeys, result.AuthorizedKeys) {
			t.Fatalf("durable read-back differs: %v", err)
		}
		if config.MemoryMB == nil || *config.MemoryMB != 8192 || !config.AudioOutput {
			t.Fatal("unrelated settings changed")
		}
		if !strings.Contains(strings.Join(result.AuthorizedKeys, "\n"), oldKey) {
			t.Fatal("pre-existing key was lost")
		}
		return result
	}
	if got := apply(add); len(got.Devices) != 2 {
		t.Fatal("new device missing")
	}
	if got := apply(add); len(got.Devices) != 2 {
		t.Fatal("repeated addition duplicated access")
	}
	apply(RemoteChange{Action: "rename", Device: "Laptop", Name: "Work laptop"})
	got := apply(RemoteChange{Action: "replace", Device: "Work laptop", Name: "Work laptop", PublicKey: replacement})
	if strings.Contains(strings.Join(got.AuthorizedKeys, "\n"), newKey) {
		t.Fatal("old device key remained authorized after replacement")
	}
	got = apply(RemoteChange{Action: "remove", Device: "Work laptop"})
	if len(got.Devices) != 1 || got.Devices[0].PublicKey != oldKey {
		t.Fatal("removal did not preserve only the original device")
	}
}

func TestRemoteDevicesRejectUnsafeInputsAndConcurrentEdits(t *testing.T) {
	for _, key := range []string{"", "-----BEGIN OPENSSH PRIVATE KEY-----", "restrict " + testEd25519PublicKey(1), testEd25519PublicKey(1) + "\n" + testEd25519PublicKey(2)} {
		root := t.TempDir()
		_, _, err := changeRemoteAccessAt(t.Context(), root, RemoteChange{Action: "add", Name: "Device", PublicKey: key}, func(RemoteSettings) bool { t.Fatal("invalid key reached approval"); return true }, "")
		if err == nil {
			t.Fatal("invalid key accepted")
		}
		if _, err := os.Stat(filepath.Join(root, globalConfigurationName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid key wrote configuration")
		}
	}
	root := t.TempDir()
	path := filepath.Join(root, globalConfigurationName)
	changed := []byte(`{"memoryMB":12288}`)
	_, _, err := changeRemoteAccessAt(t.Context(), root, RemoteChange{Action: "enable"}, func(RemoteSettings) bool {
		if err := os.WriteFile(path, changed, 0o600); err != nil {
			t.Fatal(err)
		}
		return true
	}, "")
	if !errors.Is(err, errAtomicWriteTargetChanged) {
		t.Fatalf("concurrent edit not rejected: %v", err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, changed) {
		t.Fatal("concurrent edit overwritten")
	}
}

func TestRemoteHostIdentityIsStableAndReserved(t *testing.T) {
	requireExternalBoundaryTest(t, "native automatic remote SSH identity")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	root := t.TempDir()
	plan := provisioningPlan{Tailscale: true, RemoteSSHAuthorizedKeys: []string{testEd25519PublicKey(1)}}
	if err := includeHostRemoteKey(ctx, root, &plan); err != nil {
		t.Fatal(err)
	}
	first := strings.Join(plan.RemoteSSHAuthorizedKeys, "\n")
	if len(plan.RemoteSSHAuthorizedKeys) != 2 {
		t.Fatal("host access missing")
	}
	if err := includeHostRemoteKey(ctx, root, &plan); err != nil {
		t.Fatal(err)
	}
	if strings.Join(plan.RemoteSSHAuthorizedKeys, "\n") != first {
		t.Fatal("host identity changed or duplicated")
	}
	readOnly := provisioningPlan{Tailscale: true, RemoteSSHAuthorizedKeys: []string{testEd25519PublicKey(1)}}
	if err := includeRemotePublicKey(filepath.Join(root, "identity", "id_ed25519.pub"), &readOnly); err != nil {
		t.Fatal(err)
	}
	if !sameRemoteSSHAuthorizedKeys(readOnly.RemoteSSHAuthorizedKeys, plan.RemoteSSHAuthorizedKeys) {
		t.Fatal("future launch differs from saved authorization")
	}
}

func TestRemoteDevicesCannotRevokeOrRelabelManagingHost(t *testing.T) {
	key := testEd25519PublicKey(1)
	for _, action := range []string{"add", "remove", "rename", "replace"} {
		root := t.TempDir()
		original := []byte(`{"tailscale":true,"remoteSSHAuthorizedKeys":["` + key + `"]}`)
		path := filepath.Join(root, globalConfigurationName)
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, err := changeRemoteAccessAt(t.Context(), root, RemoteChange{Action: action, Device: remoteKeyFingerprint(key), Name: "Host", PublicKey: key}, func(RemoteSettings) bool { t.Fatal("reserved identity edit reached approval"); return true }, key)
		if err == nil {
			t.Fatalf("reserved identity accepted %s", action)
		}
		if got, _ := os.ReadFile(path); !bytes.Equal(got, original) {
			t.Fatal("reserved identity edit changed saved access")
		}
	}
}

func TestRemoteDeviceReplacementKeepsOptionalNameOptional(t *testing.T) {
	root := t.TempDir()
	key := testEd25519PublicKey(1)
	if err := os.WriteFile(filepath.Join(root, globalConfigurationName), []byte(`{"tailscale":true,"remoteSSHAuthorizedKeys":["`+key+`"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, saved, err := changeRemoteAccessAt(t.Context(), root, RemoteChange{Action: "replace", Device: remoteKeyFingerprint(key), PublicKey: testEd25519PublicKey(2)}, func(RemoteSettings) bool { return true }, "")
	if err != nil || !saved || len(result.Devices) != 1 || result.Devices[0].Name != "" || result.Devices[0].PublicKey != testEd25519PublicKey(2) {
		t.Fatalf("unnamed replacement: saved=%t result=%+v err=%v", saved, result, err)
	}
}
