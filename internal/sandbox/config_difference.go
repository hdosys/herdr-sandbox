package sandbox

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func describeWSBLaunchDifferences(actualData, expectedData []byte) ([]string, error) {
	if bytes.Equal(actualData, expectedData) {
		return nil, nil
	}
	actual, err := decodeWSBConfiguration(actualData)
	if err != nil {
		return []string{"launch contract"}, nil
	}
	expected, err := decodeWSBConfiguration(expectedData)
	if err != nil {
		return nil, fmt.Errorf("decode expected Sandbox launch contract: %w", err)
	}
	differences := make([]string, 0, 5)
	if actual.MemoryInMB != expected.MemoryInMB {
		differences = append(differences, "memory")
	}
	actualAudio, actualAudioFound := wsbAudioSelection(actual.LogonCommand.Command)
	expectedAudio, expectedAudioFound := wsbAudioSelection(expected.LogonCommand.Command)
	audioDifference := expectedAudioFound && (!actualAudioFound || actualAudio != expectedAudio)
	if audioDifference {
		differences = append(differences, "audio")
	}
	actualMappings := indexWSBMappings(actual.MappedFolders.Folders)
	expectedMappings := indexWSBMappings(expected.MappedFolders.Folders)
	if !sameWSBMapping(actualMappings[strings.ToLower(guestCacheDirectory)], expectedMappings[strings.ToLower(guestCacheDirectory)]) {
		differences = append(differences, "cache")
	}
	if !sameWSBWorkspaceMappings(actualMappings, expectedMappings) {
		differences = append(differences, "workspaces")
	}
	if !sameWSBStaticContract(actual, expected) ||
		!sameWSBMapping(actualMappings[strings.ToLower(guestInputDirectory)], expectedMappings[strings.ToLower(guestInputDirectory)]) ||
		!sameWSBMapping(actualMappings[strings.ToLower(guestStatusDirectory)], expectedMappings[strings.ToLower(guestStatusDirectory)]) ||
		normalizeWSBAudioSelection(actual.LogonCommand.Command) != normalizeWSBAudioSelection(expected.LogonCommand.Command) ||
		hasUnexpectedWSBMappings(actualMappings, expectedMappings) {
		differences = append(differences, "launch contract")
	}
	if len(differences) == 0 {
		differences = append(differences, "launch contract")
	}
	sort.Strings(differences)
	return differences, nil
}

func normalizeWSBAudioSelection(command string) string {
	for _, selection := range []string{"Enabled", "Disabled"} {
		command = strings.ReplaceAll(command,
			"'-AudioPlayback','"+selection+"'", "'-AudioPlayback','<selection>'")
	}
	return command
}

func decodeWSBConfiguration(data []byte) (wsbConfiguration, error) {
	var configuration wsbConfiguration
	if len(bytes.TrimSpace(data)) == 0 {
		return configuration, errors.New("Sandbox launch contract is empty")
	}
	if err := xml.Unmarshal(data, &configuration); err != nil {
		return configuration, err
	}
	if configuration.XMLName.Local != "Configuration" {
		return configuration, fmt.Errorf("Sandbox launch root = %q", configuration.XMLName.Local)
	}
	return configuration, nil
}

func wsbAudioSelection(command string) (bool, bool) {
	if strings.Contains(command, "'-AudioPlayback','Enabled'") {
		return true, true
	}
	if strings.Contains(command, "'-AudioPlayback','Disabled'") {
		return false, true
	}
	return false, false
}

func indexWSBMappings(mappings []wsbMappedFolder) map[string]wsbMappedFolder {
	result := make(map[string]wsbMappedFolder, len(mappings))
	for _, mapping := range mappings {
		identity := strings.ToLower(strings.TrimSpace(mapping.SandboxFolder))
		if identity == "" {
			identity = fmt.Sprintf("<empty-%d>", len(result))
		}
		if _, found := result[identity]; found {
			identity = fmt.Sprintf("<duplicate-%d>", len(result))
		}
		result[identity] = mapping
	}
	return result
}

func sameWSBMapping(left, right wsbMappedFolder) bool {
	return strings.EqualFold(left.HostFolder, right.HostFolder) &&
		strings.EqualFold(left.SandboxFolder, right.SandboxFolder) && left.ReadOnly == right.ReadOnly
}

func sameWSBWorkspaceMappings(left, right map[string]wsbMappedFolder) bool {
	leftWorkspaces := wsbWorkspaceMappings(left)
	rightWorkspaces := wsbWorkspaceMappings(right)
	if len(leftWorkspaces) != len(rightWorkspaces) {
		return false
	}
	for identity, mapping := range leftWorkspaces {
		if !sameWSBMapping(mapping, rightWorkspaces[identity]) {
			return false
		}
	}
	return true
}

func wsbWorkspaceMappings(mappings map[string]wsbMappedFolder) map[string]wsbMappedFolder {
	prefix := strings.ToLower(guestWorkspacesDirectory + `\`)
	result := map[string]wsbMappedFolder{}
	for identity, mapping := range mappings {
		if strings.HasPrefix(identity, prefix) {
			result[identity] = mapping
		}
	}
	return result
}

func hasUnexpectedWSBMappings(actual, expected map[string]wsbMappedFolder) bool {
	if len(actual) != len(expected) {
		return true
	}
	for identity := range actual {
		if _, found := expected[identity]; !found {
			return true
		}
	}
	return false
}

func sameWSBStaticContract(left, right wsbConfiguration) bool {
	return left.VGPU == right.VGPU && left.Networking == right.Networking &&
		left.AudioInput == right.AudioInput && left.VideoInput == right.VideoInput &&
		left.ProtectedClient == right.ProtectedClient && left.PrinterRedirection == right.PrinterRedirection &&
		left.ClipboardRedirection == right.ClipboardRedirection
}
