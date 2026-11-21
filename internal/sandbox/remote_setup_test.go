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

func TestRemoteSetupPreservesSettingsAndRequiresAuthorization(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, globalConfigurationName)
	original := []byte(`{"memoryMB":8192,"audio":true,"workspaces":{}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "authorized_keys")
	key := testEd25519PublicKey(1)
	if err := os.WriteFile(source, []byte("# incoming devices\n"+key+" device name\n"+key+" duplicate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, approve := range []bool{false, true, true} {
		saved, err := setupRemoteAt(t.Context(), root, []string{source}, func(plan RemoteSetup) bool {
			if plan.AdditionalKeys != 1 || len(plan.Sources) != 1 {
				t.Fatalf("incorrect permission preview: %#v", plan)
			}
			return approve
		})
		if err != nil || saved != approve {
			t.Fatalf("save=%t approval=%t error=%v", saved, approve, err)
		}
		if !approve {
			got, _ := os.ReadFile(path)
			if !bytes.Equal(got, original) {
				t.Fatal("declined setup changed configuration")
			}
			continue
		}
		config, err := loadGlobalConfiguration(path)
		if err != nil || !config.Tailscale || config.MemoryMB == nil || *config.MemoryMB != 8192 || !config.Audio || len(config.RemoteSSHAuthorizedKeys) != 1 || config.RemoteSSHAuthorizedKeys[0] != key {
			t.Fatalf("configuration not preserved: %#v %v", config, err)
		}
	}
}

func TestRemoteSetupRejectsRestrictionsAndConcurrentConfigurationEdits(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, globalConfigurationName)
	source := filepath.Join(t.TempDir(), "authorized_keys")
	if err := os.WriteFile(source, []byte("restrict "+testEd25519PublicKey(1)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := setupRemoteAt(t.Context(), root, []string{source}, func(RemoteSetup) bool { t.Fatal("restricted key reached approval"); return true }); err == nil {
		t.Fatal("restricted key imported without its restrictions")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed import created configuration")
	}
	changed := []byte(`{"memoryMB":12288}`)
	_, err := setupRemoteAt(t.Context(), root, nil, func(RemoteSetup) bool {
		if err := os.WriteFile(path, changed, 0o600); err != nil {
			t.Fatal(err)
		}
		return true
	})
	if !errors.Is(err, errAtomicWriteTargetChanged) {
		t.Fatalf("concurrent edit not rejected: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, changed) {
		t.Fatal("concurrent user edit overwritten")
	}
}

func TestRemoteSetupNativeHostIdentity(t *testing.T) {
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
		t.Fatal("read-only plan differs from launch authorization")
	}
}
