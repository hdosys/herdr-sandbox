package sandbox

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	guestInputDirectory  = `C:\SandboxBootstrap`
	guestStatusDirectory = `C:\SandboxStatus`
	guestRootDirectory   = `C:\HerdrSandbox`
	guestCacheDirectory  = guestRootDirectory + `\cache`
	guestBootstrapScript = guestInputDirectory + `\bootstrap.ps1`
)

type wsbConfiguration struct {
	XMLName              xml.Name         `xml:"Configuration"`
	VGPU                 string           `xml:"vGPU"`
	Networking           string           `xml:"Networking"`
	AudioInput           string           `xml:"AudioInput"`
	VideoInput           string           `xml:"VideoInput"`
	ProtectedClient      string           `xml:"ProtectedClient"`
	PrinterRedirection   string           `xml:"PrinterRedirection"`
	ClipboardRedirection string           `xml:"ClipboardRedirection"`
	MemoryInMB           int              `xml:"MemoryInMB"`
	MappedFolders        wsbMappedFolders `xml:"MappedFolders"`
	LogonCommand         wsbLogonCommand  `xml:"LogonCommand"`
}

type wsbMappedFolders struct {
	Folders []wsbMappedFolder `xml:"MappedFolder"`
}

type wsbMappedFolder struct {
	HostFolder    string `xml:"HostFolder"`
	SandboxFolder string `xml:"SandboxFolder"`
	ReadOnly      bool   `xml:"ReadOnly"`
}

type wsbLogonCommand struct {
	Command string `xml:"Command"`
}

func guestBootstrapLaunch(audioOutputEnabled, audioInputEnabled bool) string {
	audioOutputSelection := "'Disabled'"
	if audioOutputEnabled {
		audioOutputSelection = "'Enabled'"
	}
	audioInputSelection := "'Disabled'"
	if audioInputEnabled {
		audioInputSelection = "'Enabled'"
	}
	arguments := []string{
		"'-NoLogo'", "'-NoProfile'", "'-NoExit'", "'-ExecutionPolicy'", "'Bypass'",
		"'-File'", "'C:\\SandboxBootstrap\\bootstrap.ps1'",
		"'-InputDirectory'", "'C:\\SandboxBootstrap'",
		"'-StatusDirectory'", "'C:\\SandboxStatus'",
		"'-AudioPlayback'", audioOutputSelection,
		"'-AudioInput'", audioInputSelection,
		"'-ConfigurationHandoffTimeoutMinutes'", fmt.Sprintf("'%d'", configurationHandoffTimeout/time.Minute),
	}
	return "Start-Process -FilePath 'powershell.exe' -WindowStyle Normal -Wait -ArgumentList @(" +
		strings.Join(arguments, ",") + ")"
}

func renderConfig(inputDirectory, statusDirectory, cacheDirectory string, mounts []mountPlan, workspaces []workspacePlan, memoryMB int, audioOutputEnabled, audioInputEnabled bool) ([]byte, error) {
	if len(mounts) > maximumMounts {
		return nil, fmt.Errorf("folder mount count %d exceeds limit %d", len(mounts), maximumMounts)
	}
	if !filepath.IsAbs(inputDirectory) {
		return nil, errors.New("Sandbox input directory must be absolute")
	}
	if !filepath.IsAbs(statusDirectory) {
		return nil, errors.New("Sandbox status directory must be absolute")
	}
	if !filepath.IsAbs(cacheDirectory) {
		return nil, errors.New("herdr-sandbox cache directory must be absolute")
	}
	cleanInput := filepath.Clean(inputDirectory)
	cleanStatus := filepath.Clean(statusDirectory)
	cleanCache := filepath.Clean(cacheDirectory)
	hostMappings := []string{cleanInput, cleanStatus, cleanCache}
	for left := range hostMappings {
		for right := left + 1; right < len(hostMappings); right++ {
			if hostPathsOverlap(hostMappings[left], hostMappings[right]) {
				return nil, errors.New("Sandbox input, status, and cache directories must not overlap")
			}
		}
	}
	if memoryMB < 2048 {
		return nil, fmt.Errorf("Sandbox memory must be at least 2048 MB, got %d", memoryMB)
	}

	mappings := []wsbMappedFolder{{HostFolder: cleanInput, SandboxFolder: guestInputDirectory, ReadOnly: true}}
	seenGuests := map[string]struct{}{strings.ToLower(guestInputDirectory): {}, strings.ToLower(guestStatusDirectory): {}, strings.ToLower(guestCacheDirectory): {}}
	for _, mount := range mounts {
		if !workspaceNamePattern.MatchString(mount.Name) {
			return nil, fmt.Errorf("folder mount name is invalid: %q", mount.Name)
		}
		if !filepath.IsAbs(mount.HostDirectory) || !filepath.IsAbs(mount.GuestDirectory) {
			return nil, fmt.Errorf("folder mount %q paths must be absolute", mount.Name)
		}
		host := filepath.Clean(mount.HostDirectory)
		guest := filepath.Clean(mount.GuestDirectory)
		if !strings.EqualFold(guest, guestMountDirectory(mount.Name)) {
			return nil, fmt.Errorf("folder mount %q has unsupported guest directory %s", mount.Name, guest)
		}
		for _, mappedHost := range hostMappings {
			if hostPathsOverlap(host, mappedHost) {
				return nil, fmt.Errorf("folder mount %q overlaps host mapping %s", mount.Name, mappedHost)
			}
		}
		if _, exists := seenGuests[strings.ToLower(guest)]; exists {
			return nil, fmt.Errorf("folder mount %q duplicates a guest mapping", mount.Name)
		}
		hostMappings = append(hostMappings, host)
		seenGuests[strings.ToLower(guest)] = struct{}{}
		mappings = append(mappings, wsbMappedFolder{HostFolder: host, SandboxFolder: guest, ReadOnly: mount.ReadOnly})
	}
	for _, workspace := range workspaces {
		if !filepath.IsAbs(workspace.HostDirectory) || !filepath.IsAbs(workspace.GuestDirectory) {
			return nil, fmt.Errorf("workspace %q paths must be absolute", workspace.Name)
		}
		host := filepath.Clean(workspace.HostDirectory)
		guest := filepath.Clean(workspace.GuestDirectory)
		for _, mappedHost := range hostMappings {
			if hostPathsOverlap(host, mappedHost) {
				return nil, fmt.Errorf("workspace %q overlaps host mapping %s", workspace.Name, mappedHost)
			}
		}
		if _, exists := seenGuests[strings.ToLower(guest)]; exists {
			return nil, fmt.Errorf("workspace %q duplicates a guest mapping", workspace.Name)
		}
		hostMappings = append(hostMappings, host)
		seenGuests[strings.ToLower(guest)] = struct{}{}
		mappings = append(mappings, wsbMappedFolder{HostFolder: host, SandboxFolder: guest, ReadOnly: false})
	}
	mappings = append(mappings, wsbMappedFolder{HostFolder: cleanStatus, SandboxFolder: guestStatusDirectory, ReadOnly: false})
	mappings = append(mappings, wsbMappedFolder{HostFolder: cleanCache, SandboxFolder: guestCacheDirectory, ReadOnly: false})

	audioInput := "Disable"
	if audioInputEnabled {
		audioInput = "Enable"
	}
	config := wsbConfiguration{
		VGPU:                 "Enable",
		Networking:           "Enable",
		AudioInput:           audioInput,
		VideoInput:           "Disable",
		ProtectedClient:      "Disable",
		PrinterRedirection:   "Disable",
		ClipboardRedirection: "Enable",
		MemoryInMB:           memoryMB,
		MappedFolders:        wsbMappedFolders{Folders: mappings},
		LogonCommand: wsbLogonCommand{Command: strings.Join([]string{
			"powershell.exe",
			"-NoLogo",
			"-NoProfile",
			"-NonInteractive",
			"-ExecutionPolicy Bypass",
			`-Command "` + guestBootstrapLaunch(audioOutputEnabled, audioInputEnabled) + `"`,
		}, " ")},
	}

	return encodeWSBConfiguration(config)
}

func encodeWSBConfiguration(config wsbConfiguration) ([]byte, error) {
	encoded, err := xml.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Windows Sandbox configuration: %w", err)
	}
	return append([]byte(xml.Header), append(encoded, '\n')...), nil
}

func canonicalMappedDirectory(path string) (string, error) {
	path = filepath.Clean(path)
	if err := rejectMappedPathReparsePoints(path); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect mapped directory %s: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("mapped path is not a directory: %s", path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve mapped directory %s: %w", path, err)
	}
	return filepath.Clean(resolved), nil
}

func ensurePhysicalDirectory(path, role string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%s directory is not absolute: %q", role, path)
	}
	path = filepath.Clean(path)
	existing := path
	for {
		info, err := os.Lstat(existing)
		if err == nil {
			if err := rejectMappedPathReparsePoints(existing); err != nil {
				return "", fmt.Errorf("refusing unsafe %s directory: %w", role, err)
			}
			if !info.IsDir() {
				return "", fmt.Errorf("%s path component is not a directory: %s", role, existing)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect %s path component %s: %w", role, existing, err)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("find existing parent for %s directory: %s", role, path)
		}
		existing = parent
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("create %s directory: %w", role, err)
	}
	physical, err := canonicalMappedDirectory(path)
	if err != nil {
		return "", fmt.Errorf("validate %s directory: %w", role, err)
	}
	return physical, nil
}

func rejectMappedPathReparsePoints(path string) error {
	current := filepath.Clean(path)
	for {
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect mapped path component %s: %w", current, err)
		}
		reparse, err := fileInfoIsReparsePoint(info)
		if err != nil {
			return fmt.Errorf("inspect mapped path component %s: %w", current, err)
		}
		if reparse {
			return fmt.Errorf("mapped directory must not contain a reparse point: %s", current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return nil
}

func physicalMappedDirectory(path string) (string, error) {
	identity, err := mappedDirectoryPhysicalIdentity(path)
	if err != nil {
		return "", fmt.Errorf("resolve mapped directory physical identity %s: %w", path, err)
	}
	if strings.TrimSpace(identity) == "" {
		return "", fmt.Errorf("mapped directory physical identity is empty: %s", path)
	}
	return identity, nil
}
