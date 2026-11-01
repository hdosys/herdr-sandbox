package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// addZedConfiguration transfers authored configuration, never Zed's runtime state.
func addZedConfiguration(root string, add func(string, string) error) error {
	if err := rejectMappedPathReparsePoints(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, name := range []string{"settings.json", "keymap.json", "tasks.json"} {
		source := filepath.Join(root, name)
		if err := rejectMappedPathReparsePoints(source); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if err := add(source, filepath.Join("zed", name)); err != nil {
			return err
		}
	}
	for _, name := range []string{"snippets", "themes"} {
		source := filepath.Join(root, name)
		if err := rejectMappedPathReparsePoints(source); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if err := addConfigurationTree(source, filepath.Join("zed", name), map[string]bool{".git": true}, func(path, destination string) error {
			if !strings.EqualFold(filepath.Ext(path), ".json") {
				return nil
			}
			if err := rejectMappedPathReparsePoints(path); err != nil {
				return err
			}
			return add(path, destination)
		}); err != nil {
			return err
		}
	}
	return nil
}
