package installerartifact

import (
	"debug/pe"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FindNSIS resolves the existing compiler. It never installs tools on the host.
func FindNSIS() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("MAKENSIS")); configured != "" {
		return RequireRegularFile(configured, "MAKENSIS")
	}
	if path, err := exec.LookPath("makensis.exe"); err == nil {
		return path, nil
	}
	for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if strings.TrimSpace(base) == "" {
			continue
		}
		if path, err := RequireRegularFile(filepath.Join(base, "NSIS", "makensis.exe"), "NSIS compiler"); err == nil {
			return path, nil
		}
	}
	return "", errors.New("NSIS compiler is missing; select the nsis provisioning stack in the build Sandbox, or install NSIS.NSIS on this computer")
}

func RequireRegularFile(path, role string) (string, error) {
	if !filepath.IsAbs(path) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve %s path: %w", role, err)
		}
		path = absolute
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", role, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file: %s", role, path)
	}
	return path, nil
}

func VerifyPE(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect installer artifact: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("installer artifact must be a nonempty regular file: %s", path)
	}
	file, err := pe.Open(path)
	if err != nil {
		return fmt.Errorf("inspect installer PE: %w", err)
	}
	defer file.Close()
	if file.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_I386 {
		return fmt.Errorf("unexpected NSIS installer PE machine: %#x", file.FileHeader.Machine)
	}
	return nil
}
