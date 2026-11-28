package sandbox

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteDeviceNamesCannotAliasOtherTargets(t *testing.T) {
	first, second := testEd25519PublicKey(1), testEd25519PublicKey(2)
	firstID, secondID := remoteKeyFingerprint(first), remoteKeyFingerprint(second)
	for _, scenario := range []struct{ name, firstName, secondName, target string }{
		{"fingerprint namespace", strings.ToLower(secondID), "Laptop", secondID},
		{"unicode case folding", "S", "\u017f", "\u017f"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			original, err := json.Marshal(map[string]any{
				"tailscale":               true,
				"remoteSSHAuthorizedKeys": []string{first, second},
				"remoteSSHDeviceNames":    map[string]string{firstID: scenario.firstName, secondID: scenario.secondName},
			})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, globalConfigurationName)
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			approved := false
			_, saved, err := changeRemoteAccessAt(t.Context(), root, RemoteChange{Action: "remove", Device: scenario.target}, func(RemoteSettings) bool { approved = true; return true }, "")
			if err == nil || saved || approved {
				t.Fatalf("ambiguous device metadata reached authorization: saved=%t approved=%t error=%v", saved, approved, err)
			}
			if data, _ := os.ReadFile(path); !bytes.Equal(data, original) {
				t.Fatal("ambiguous device selection altered authorization")
			}
		})
	}
}
