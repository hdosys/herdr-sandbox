package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadSessionWorkspacesExposesOnlyGuestIdentity(t *testing.T) {
	runDirectory := t.TempDir()
	provisioning := filepath.Join(runDirectory, "input", "provisioning")
	if err := os.MkdirAll(provisioning, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := encodeGuestWorkspaceManifest([]workspacePlan{
		{Name: "alpha", GuestDirectory: guestWorkspaceDirectory("alpha")},
		{Name: "beta", GuestDirectory: guestWorkspaceDirectory("beta")},
	}, guestWorkspaceDirectory("beta"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(provisioning, workspaceManifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	workspaces, err := readSessionWorkspaces(runDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 2 || workspaces[0].Active || !workspaces[1].Active ||
		workspaces[1].Directory != `C:\Workspaces\beta` {
		t.Fatalf("workspaces = %#v", workspaces)
	}
}

func TestDecodeGuestWorkspaceManifestIsStrict(t *testing.T) {
	valid := []byte(`{"schemaVersion":1,"activeWorkspace":"C:\\Workspaces\\alpha","workspaces":[{"name":"alpha","directory":"C:\\Workspaces\\alpha"}]}`)
	if _, err := decodeGuestWorkspaceManifest(valid); err != nil {
		t.Fatalf("valid manifest: %v", err)
	}
	for _, data := range [][]byte{
		[]byte(`{"schemaVersion":1,"activeWorkspace":"C:\\Workspaces\\other","workspaces":[{"name":"alpha","directory":"C:\\Workspaces\\alpha"}]}`),
		[]byte(`{"schemaVersion":1,"activeWorkspace":"C:\\Workspaces\\alpha","workspaces":[{"name":"alpha","directory":"C:\\Workspaces\\other"}]}`),
		[]byte(`{"schemaVersion":1,"activeWorkspace":"C:\\Workspaces\\alpha","workspaces":[{"name":"alpha","directory":"C:\\Workspaces\\alpha"}],"extra":true}`),
		append(append([]byte{}, valid...), []byte(` {}`)...),
	} {
		if _, err := decodeGuestWorkspaceManifest(data); err == nil {
			t.Fatalf("invalid manifest decoded: %s", data)
		}
	}
}

func TestReadSessionTimingsReturnsLatestBoundedRecords(t *testing.T) {
	statusDirectory := t.TempDir()
	lines := make([]string, 10)
	for index := range lines {
		lines[index] = `{"schemaVersion":1,"role":"role-` + string(rune('a'+index)) + `","elapsedMilliseconds":1250,"recordedAtUTC":"2026-07-29T12:00:00Z"}`
	}
	if err := os.WriteFile(filepath.Join(statusDirectory, "timings.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	timings, err := readSessionTimings(statusDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(timings) != maximumDisplayedTimings || timings[0].Role != "role-c" || timings[7].Role != "role-j" {
		t.Fatalf("timings = %#v", timings)
	}
}

func TestReadSessionTimingsRejectsMalformedOrUnboundedRecords(t *testing.T) {
	for name, contents := range map[string]string{
		"unknown":   `{"schemaVersion":1,"role":"role","elapsedMilliseconds":1,"recordedAtUTC":"2026-07-29T12:00:00Z","extra":true}`,
		"negative":  `{"schemaVersion":1,"role":"role","elapsedMilliseconds":-1,"recordedAtUTC":"2026-07-29T12:00:00Z"}`,
		"multiline": `{"schemaVersion":1,"role":"first\nsecond","elapsedMilliseconds":1,"recordedAtUTC":"2026-07-29T12:00:00Z"}`,
		"terminal":  `{"schemaVersion":1,"role":"role\u001b[2J","elapsedMilliseconds":1,"recordedAtUTC":"2026-07-29T12:00:00Z"}`,
	} {
		t.Run(name, func(t *testing.T) {
			statusDirectory := t.TempDir()
			if err := os.WriteFile(filepath.Join(statusDirectory, "timings.jsonl"), []byte(contents+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readSessionTimings(statusDirectory); err == nil {
				t.Fatal("invalid timing unexpectedly loaded")
			}
		})
	}
}
