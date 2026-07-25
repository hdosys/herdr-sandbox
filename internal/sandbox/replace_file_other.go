//go:build !windows

package sandbox

import (
	"fmt"
	"os"
)

func replaceFileAtomically(target, replacement, backup string) error {
	if backup != "" {
		if err := os.Link(target, backup); err != nil {
			return fmt.Errorf("preserve replaced file: %w", err)
		}
	}
	if err := os.Rename(replacement, target); err != nil {
		if backup != "" {
			_ = os.Remove(backup)
		}
		return fmt.Errorf("replace file: %w", err)
	}
	return nil
}
