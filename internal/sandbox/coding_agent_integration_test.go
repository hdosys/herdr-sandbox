package sandbox

import (
	"reflect"
	"testing"
)

func TestSelectedGuestHerdrIntegrationsFollowCodingAgentSync(t *testing.T) {
	selected := selectedGuestHerdrIntegrations(codingAgentSyncConfiguration{
		OpenCode:      true,
		Codex:         true,
		GitHubCopilot: true,
	})
	want := []guestHerdrIntegrationSpec{
		{target: "opencode", command: "opencode", configurationRoot: `.config\opencode`},
		{target: "codex", command: "codex", configurationRoot: `.codex`},
		{target: "copilot", command: "copilot", configurationRoot: `.copilot`},
	}
	if !reflect.DeepEqual(selected, want) {
		t.Fatalf("selected integrations = %#v, want %#v", selected, want)
	}
}

func TestMissingGuestHerdrIntegrationsPreserveEveryExistingState(t *testing.T) {
	specs := selectedGuestHerdrIntegrations(codingAgentSyncConfiguration{
		OpenCode: true, ClaudeCode: true, Codex: true, GitHubCopilot: true, Pi: true,
	})
	statuses, err := parseGuestHerdrIntegrationStatuses([]byte(
		"pi: not installed (C:\\Users\\fixture\\.pi\\agent\\extensions\\herdr-agent-state.ts)\r\n"+
			"claude: current (v8) (C:\\Users\\fixture\\.claude\\hooks\\herdr-agent-state.ps1)\r\n"+
			"codex: outdated (v7 < v8) (C:\\Users\\fixture\\.codex\\herdr-agent-state.ps1)\r\n"+
			"copilot: needs repair (v3) (C:\\Users\\fixture\\.copilot\\hooks\\herdr-agent-state.ps1)\r\n"+
			"opencode: not installed (C:\\Users\\fixture\\.config\\opencode\\plugins\\herdr-agent-state.js)\r\n"), specs)
	if err != nil {
		t.Fatal(err)
	}
	missing := missingGuestHerdrIntegrations(specs, statuses)
	want := []guestHerdrIntegrationSpec{specs[0], specs[4]}
	if !reflect.DeepEqual(missing, want) {
		t.Fatalf("missing integrations = %#v, want %#v", missing, want)
	}
}

func TestParseGuestHerdrIntegrationStatusesRejectsAmbiguousSelectedStatus(t *testing.T) {
	specs := []guestHerdrIntegrationSpec{{target: "codex"}}
	for _, output := range []string{
		"codex: unknown (C:\\Users\\fixture\\.codex\\herdr-agent-state.ps1)\n",
		"codex: not installed (C:\\one)\ncodex: current (v8) (C:\\two)\n",
		"codex: current (v8)\n",
	} {
		if _, err := parseGuestHerdrIntegrationStatuses([]byte(output), specs); err == nil {
			t.Fatalf("ambiguous integration status unexpectedly passed: %q", output)
		}
	}
}
