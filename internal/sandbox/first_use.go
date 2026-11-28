package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
)

type FirstUse struct {
	Directory     string
	NeedsProject  bool
	RemoteEnabled bool
	MemoryMB      int
}

// InspectFirstUse uses the same read-only resolver as planning. Invalid
// configuration is an error, never interpreted as permission to replace it.
func InspectFirstUse(directory string) (FirstUse, error) {
	explicitDirectory := directory != ""
	if directory == "" {
		var err error
		directory, err = os.Getwd()
		if err != nil {
			return FirstUse{}, err
		}
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return FirstUse{}, err
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return FirstUse{}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return FirstUse{}, err
	}
	plan, err := resolveProvisioningReadOnlyAt(directory, filepath.Join(root, applicationName), filepath.Dir(executable))
	if err != nil {
		return FirstUse{}, err
	}
	hasProfile := false
	for _, workspace := range plan.Workspaces {
		hasProfile = hasProfile || workspace.Active
		if explicitDirectory && workspace.Active {
			equal, err := workspaceDirectoriesEqual(directory, workspace.HostDirectory)
			if err != nil {
				return FirstUse{}, err
			}
			if !equal {
				return FirstUse{}, fmt.Errorf("selected folder %s inherits a broader project profile at %s; select that root explicitly only if you intend to expose it", directory, workspace.HostDirectory)
			}
		}
	}
	if explicitDirectory {
		if _, err := validateProjectFolder(directory); err != nil {
			return FirstUse{}, err
		}
	}
	return FirstUse{Directory: directory, NeedsProject: len(plan.Workspaces) == 0 || explicitDirectory && !hasProfile, RemoteEnabled: plan.Tailscale, MemoryMB: plan.MemoryMB}, nil
}
