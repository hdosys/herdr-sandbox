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
	differences := make([]string, 0, 8)
	if actual.MemoryInMB != expected.MemoryInMB {
		differences = append(differences, "memory")
	}
	actualAudioOutput, actualAudioOutputFound := wsbAudioSelection(actual.LogonCommand.Command, "AudioPlayback")
	expectedAudioOutput, expectedAudioOutputFound := wsbAudioSelection(expected.LogonCommand.Command, "AudioPlayback")
	if expectedAudioOutputFound && (!actualAudioOutputFound || actualAudioOutput != expectedAudioOutput) {
		differences = append(differences, "audio output")
	}
	actualAudioInput, actualAudioInputFound := wsbAudioSelection(actual.LogonCommand.Command, "AudioInput")
	expectedAudioInput, expectedAudioInputFound := wsbAudioSelection(expected.LogonCommand.Command, "AudioInput")
	if actual.AudioInput != expected.AudioInput ||
		expectedAudioInputFound && (!actualAudioInputFound || actualAudioInput != expectedAudioInput) {
		differences = append(differences, "audio input")
	}
	actualMappings := indexWSBMappings(actual.MappedFolders.Folders)
	expectedMappings := indexWSBMappings(expected.MappedFolders.Folders)
	if !sameWSBMapping(actualMappings[strings.ToLower(guestCacheDirectory)], expectedMappings[strings.ToLower(guestCacheDirectory)]) {
		differences = append(differences, "cache")
	}
	if !sameWSBMapping(actualMappings[strings.ToLower(guestVisualStudioCache)], expectedMappings[strings.ToLower(guestVisualStudioCache)]) {
		differences = append(differences, "Visual Studio cache")
	}
	if !sameWSBMapping(actualMappings[strings.ToLower(guestWorktreeDirectory)], expectedMappings[strings.ToLower(guestWorktreeDirectory)]) {
		differences = append(differences, "worktree directory")
	}
	if !sameWSBMapping(actualMappings[strings.ToLower(guestModelsDirectory)], expectedMappings[strings.ToLower(guestModelsDirectory)]) {
		differences = append(differences, "models directory")
	}
	if !sameWSBMountMappings(actualMappings, expectedMappings) {
		differences = append(differences, "folder mounts")
	}
	if !sameWSBWorkspaceMappings(actualMappings, expectedMappings) {
		differences = append(differences, "workspaces")
	}
	if !sameWSBStaticContract(actual, expected) ||
		!sameWSBMapping(actualMappings[strings.ToLower(guestInputDirectory)], expectedMappings[strings.ToLower(guestInputDirectory)]) ||
		!sameWSBMapping(actualMappings[strings.ToLower(guestStatusDirectory)], expectedMappings[strings.ToLower(guestStatusDirectory)]) ||
		normalizeWSBAudioSelections(actual.LogonCommand.Command) != normalizeWSBAudioSelections(expected.LogonCommand.Command) ||
		hasUnexpectedWSBMappings(actualMappings, expectedMappings) {
		differences = append(differences, "launch contract")
	}
	if len(differences) == 0 {
		canonicalActual, encodeErr := encodeWSBConfiguration(actual)
		if encodeErr != nil {
			return nil, encodeErr
		}
		if !bytes.Equal(actualData, canonicalActual) {
			differences = append(differences, "launch contract")
		}
	}
	sort.Strings(differences)
	return differences, nil
}

func normalizeWSBAudioSelections(command string) string {
	for _, parameter := range []string{"AudioPlayback", "AudioInput"} {
		for _, selection := range []string{"Enabled", "Disabled"} {
			command = strings.ReplaceAll(command,
				"'-"+parameter+"','"+selection+"'", "'-"+parameter+"','<selection>'")
		}
	}
	return command
}

func decodeWSBConfiguration(data []byte) (wsbConfiguration, error) {
	var configuration wsbConfiguration
	if len(bytes.TrimSpace(data)) == 0 {
		return configuration, errors.New("launch contract for Sandbox is empty")
	}
	if err := xml.Unmarshal(data, &configuration); err != nil {
		return configuration, err
	}
	if configuration.XMLName.Local != "Configuration" {
		return configuration, fmt.Errorf("launch root for Sandbox = %q", configuration.XMLName.Local)
	}
	return configuration, nil
}

func wsbAudioSelection(command, parameter string) (bool, bool) {
	if strings.Contains(command, "'-"+parameter+"','Enabled'") {
		return true, true
	}
	if strings.Contains(command, "'-"+parameter+"','Disabled'") {
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

func sameWSBMountMappings(left, right map[string]wsbMappedFolder) bool {
	leftMounts := wsbMountMappings(left)
	rightMounts := wsbMountMappings(right)
	if len(leftMounts) != len(rightMounts) {
		return false
	}
	for identity, mapping := range leftMounts {
		if !sameWSBMapping(mapping, rightMounts[identity]) {
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

func wsbMountMappings(mappings map[string]wsbMappedFolder) map[string]wsbMappedFolder {
	prefix := strings.ToLower(guestMountsDirectory + `\`)
	result := map[string]wsbMappedFolder{}
	for identity, mapping := range mappings {
		if strings.HasPrefix(identity, prefix) {
			result[identity] = mapping
		}
	}
	return result
}

func hasUnexpectedWSBMappings(actual, expected map[string]wsbMappedFolder) bool {
	for _, mappings := range []map[string]wsbMappedFolder{actual, expected} {
		for identity := range mappings {
			if !isKnownWSBMapping(identity) {
				return true
			}
		}
	}
	return false
}

func isKnownWSBMapping(identity string) bool {
	for _, exact := range []string{guestInputDirectory, guestStatusDirectory, guestCacheDirectory, guestVisualStudioCache, guestModelsDirectory, guestWorktreeDirectory} {
		if identity == strings.ToLower(exact) {
			return true
		}
	}
	for _, root := range []string{guestMountsDirectory, guestWorkspacesDirectory} {
		if strings.HasPrefix(identity, strings.ToLower(root+`\`)) {
			return true
		}
	}
	return false
}

func sameWSBStaticContract(left, right wsbConfiguration) bool {
	return left.VGPU == right.VGPU && left.Networking == right.Networking &&
		left.VideoInput == right.VideoInput &&
		left.ProtectedClient == right.ProtectedClient && left.PrinterRedirection == right.PrinterRedirection &&
		left.ClipboardRedirection == right.ClipboardRedirection
}
