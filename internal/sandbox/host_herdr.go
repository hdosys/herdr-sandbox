package sandbox

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	maximumHostHerdrArchiveSize = 256 * 1024 * 1024
	maximumHostHerdrBinarySize  = 128 * 1024 * 1024
)

func ensurePinnedHostHerdr(ctx context.Context, release herdrRelease, dataDirectory string, output io.Writer) (string, string, error) {
	target, err := resolveInstalledHostHerdrPath()
	if err != nil {
		return "", "", err
	}
	_, version, inspectErr := inspectHostHerdrAt(ctx, target)
	if inspectErr == nil && version == release.Version {
		_ = cleanupReplacedHostHerdrBackup(target + ".herdr-sandbox-previous")
		_ = cleanupReplacedHostHerdrBackup(target + ".herdr-sandbox-atomic-previous")
		return target, version, nil
	}
	if output != nil {
		fmt.Fprintf(output, "Updating the standard host Herdr executable to %s...\n", strings.TrimPrefix(release.Version, "herdr "))
	}
	archivePath, err := ensurePinnedHostHerdrArchive(ctx, release, dataDirectory, output)
	if err != nil {
		return "", "", err
	}
	temporaryExecutable, err := extractPinnedHostHerdr(archivePath, filepath.Dir(target))
	if err != nil {
		return "", "", err
	}
	defer os.Remove(temporaryExecutable)
	if _, temporaryVersion, err := inspectHostHerdrAt(ctx, temporaryExecutable); err != nil {
		return "", "", fmt.Errorf("verify extracted host Herdr executable: %w", err)
	} else if temporaryVersion != release.Version {
		return "", "", fmt.Errorf("extracted host Herdr version = %q, required %q", temporaryVersion, release.Version)
	}
	backup, err := replacePinnedHostHerdr(ctx, temporaryExecutable, target, release.Version)
	if err != nil {
		return "", "", err
	}
	if cleanupErr := cleanupReplacedHostHerdrBackup(backup); cleanupErr != nil && output != nil {
		fmt.Fprintf(output, "Warning: deferred old host Herdr executable cleanup: %v\n", cleanupErr)
	}
	return inspectHostHerdrAt(ctx, target)
}

func resolveInstalledHostHerdrPath() (string, error) {
	target, err := exec.LookPath("herdr.exe")
	if err != nil {
		recovered, recoveryErr := recoverStandardHostHerdrBackup()
		if recoveryErr != nil {
			return "", fmt.Errorf("locate installed host Herdr executable: %w (atomic-backup recovery failed: %v)", err, recoveryErr)
		}
		if !recovered {
			return "", fmt.Errorf("locate installed host Herdr executable: %w", err)
		}
		target, err = exec.LookPath("herdr.exe")
		if err != nil {
			return "", fmt.Errorf("locate recovered host Herdr executable: %w", err)
		}
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve installed host Herdr executable: %w", err)
	}
	targetDirectory, err := resolveHostHerdrTargetDirectory(filepath.Dir(target))
	if err != nil {
		return "", fmt.Errorf("validate installed Herdr directory: %w", err)
	}
	return filepath.Join(targetDirectory, filepath.Base(target)), nil
}

func recoverStandardHostHerdrBackup() (bool, error) {
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if !filepath.IsAbs(localAppData) {
		return false, fmt.Errorf("LOCALAPPDATA is not absolute: %q", localAppData)
	}
	standardDirectory := filepath.Join(localAppData, "Programs", "Herdr", "bin")
	targetDirectory, err := resolveHostHerdrTargetDirectory(standardDirectory)
	if err != nil {
		return false, fmt.Errorf("validate standard host Herdr recovery directory: %w", err)
	}
	target := filepath.Join(targetDirectory, "herdr.exe")
	if _, err := os.Lstat(target); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("inspect host Herdr recovery target: %w", err)
	}
	backup := target + ".herdr-sandbox-atomic-previous"
	info, err := os.Lstat(backup)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect host Herdr recovery backup: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, fmt.Errorf("host Herdr recovery backup is not a regular file: %s", backup)
	}
	if err := os.Rename(backup, target); err != nil {
		return false, fmt.Errorf("restore host Herdr atomic backup: %w", err)
	}
	return true, nil
}

func resolveHostHerdrTargetDirectory(directory string) (string, error) {
	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home for host Herdr executable: %w", err)
	}
	standardJunction := filepath.Join(localAppData, "Programs", "Herdr", "bin")
	if strings.EqualFold(filepath.Clean(directory), filepath.Clean(standardJunction)) {
		resolved, err := resolvedDirectoryPath(directory)
		if err != nil {
			return "", fmt.Errorf("resolve standard host Herdr junction: %w", err)
		}
		releasesRoot := filepath.Join(userHome, ".herdr", "packages", "standalone", "releases")
		relative, err := filepath.Rel(releasesRoot, resolved)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || strings.Contains(relative, string(os.PathSeparator)) {
			return "", fmt.Errorf("standard host Herdr junction has unexpected target: %s", resolved)
		}
		validated, err := canonicalMappedDirectory(resolved)
		if err != nil {
			return "", fmt.Errorf("validate standard host Herdr release directory: %w", err)
		}
		return validated, nil
	}
	validated, err := canonicalMappedDirectory(directory)
	if err != nil {
		return "", fmt.Errorf("validate installed Herdr directory: %w", err)
	}
	return validated, nil
}

func ensurePinnedHostHerdrArchive(ctx context.Context, release herdrRelease, dataDirectory string, output io.Writer) (string, error) {
	directory := filepath.Join(dataDirectory, "downloads")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create host download cache: %w", err)
	}
	archivePath := filepath.Join(directory, "herdr-"+release.ArchiveSHA256+".zip")
	if info, err := os.Lstat(archivePath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("host Herdr archive cache is not a regular file: %s", archivePath)
		}
		if digest, err := sha256File(archivePath); err == nil && digest == release.ArchiveSHA256 {
			return archivePath, nil
		}
		if err := os.Remove(archivePath); err != nil {
			return "", fmt.Errorf("remove invalid host Herdr archive cache: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect host Herdr archive cache: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".herdr-download-*.zip")
	if err != nil {
		return "", fmt.Errorf("create host Herdr download stage: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, release.ArchiveURL, nil)
	if err != nil {
		temporary.Close()
		return "", fmt.Errorf("create host Herdr release request: %w", err)
	}
	client := &http.Client{CheckRedirect: func(request *http.Request, previous []*http.Request) error {
		if len(previous) >= 10 || request.URL.Scheme != "https" {
			return fmt.Errorf("unsafe host Herdr release redirect to %s", request.URL)
		}
		return nil
	}}
	response, err := client.Do(request)
	if err != nil {
		temporary.Close()
		if removeErr := os.Remove(temporaryPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return "", fmt.Errorf("download host Herdr release: %w (remove direct-download stage: %v)", err, removeErr)
		}
		if output != nil {
			fmt.Fprintln(output, "Direct HTTPS was unavailable; retrying the same pinned Herdr Windows asset with BITS...")
		}
		if bitsErr := downloadPinnedHostHerdrWithBITS(ctx, release.ArchiveURL, temporaryPath); bitsErr != nil {
			return "", fmt.Errorf("download host Herdr release: direct HTTPS: %v; BITS: %w", err, bitsErr)
		}
		info, statErr := os.Stat(temporaryPath)
		if statErr != nil {
			return "", fmt.Errorf("inspect BITS host Herdr release: %w", statErr)
		}
		if info.Size() <= 0 || info.Size() > maximumHostHerdrArchiveSize {
			return "", fmt.Errorf("BITS host Herdr release has invalid size %d", info.Size())
		}
		digest, hashErr := sha256File(temporaryPath)
		if hashErr != nil {
			return "", fmt.Errorf("hash BITS host Herdr release: %w", hashErr)
		}
		if digest != release.ArchiveSHA256 {
			return "", fmt.Errorf("BITS host Herdr release SHA-256 = %s, required %s", digest, release.ArchiveSHA256)
		}
		if renameErr := os.Rename(temporaryPath, archivePath); renameErr != nil {
			return "", fmt.Errorf("publish BITS host Herdr release cache: %w", renameErr)
		}
		return archivePath, nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		temporary.Close()
		return "", fmt.Errorf("download host Herdr release: HTTP %s", response.Status)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(response.Body, maximumHostHerdrArchiveSize+1))
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if copyErr != nil {
		return "", fmt.Errorf("write host Herdr release: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close host Herdr release: %w", closeErr)
	}
	if syncErr != nil {
		return "", fmt.Errorf("flush host Herdr release: %w", syncErr)
	}
	if written > maximumHostHerdrArchiveSize {
		return "", fmt.Errorf("host Herdr release exceeds %d bytes", maximumHostHerdrArchiveSize)
	}
	if digest := fmt.Sprintf("%x", hash.Sum(nil)); digest != release.ArchiveSHA256 {
		return "", fmt.Errorf("host Herdr release SHA-256 = %s, required %s", digest, release.ArchiveSHA256)
	}
	if err := os.Rename(temporaryPath, archivePath); err != nil {
		return "", fmt.Errorf("publish host Herdr release cache: %w", err)
	}
	return archivePath, nil
}

func downloadPinnedHostHerdrWithBITS(ctx context.Context, source, destination string) error {
	powerShell, err := windowsPowerShellExecutable()
	if err != nil {
		return err
	}
	script := `$ErrorActionPreference = 'Stop'
Import-Module BitsTransfer -ErrorAction Stop
Start-BitsTransfer -Source $env:HERDR_SANDBOX_BITS_SOURCE -Destination $env:HERDR_SANDBOX_BITS_DESTINATION -ErrorAction Stop`
	command := hiddenCommandContext(ctx, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script))
	command.Env = append(os.Environ(),
		"HERDR_SANDBOX_BITS_SOURCE="+source,
		"HERDR_SANDBOX_BITS_DESTINATION="+destination,
	)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("BITS transfer failed: %w: %s", err, boundedText(output))
	}
	return nil
}

func extractPinnedHostHerdr(archivePath, targetDirectory string) (string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("open host Herdr release: %w", err)
	}
	defer reader.Close()
	var executable *zip.File
	for _, file := range reader.File {
		if strings.EqualFold(filepath.Base(filepath.FromSlash(file.Name)), "herdr.exe") && file.Mode().IsRegular() {
			if executable != nil {
				return "", fmt.Errorf("host Herdr release contains multiple herdr.exe files")
			}
			executable = file
		}
	}
	if executable == nil || executable.UncompressedSize64 > maximumHostHerdrBinarySize {
		return "", fmt.Errorf("host Herdr release does not contain one bounded herdr.exe")
	}
	source, err := executable.Open()
	if err != nil {
		return "", fmt.Errorf("open host Herdr executable from release: %w", err)
	}
	defer source.Close()
	temporary, err := os.CreateTemp(targetDirectory, ".herdr-sandbox-client-*.exe")
	if err != nil {
		return "", fmt.Errorf("create host Herdr executable stage: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		temporary.Close()
		if err != nil {
			os.Remove(temporaryPath)
		}
	}()
	written, copyErr := io.Copy(temporary, io.LimitReader(source, maximumHostHerdrBinarySize+1))
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if copyErr != nil {
		os.Remove(temporaryPath)
		return "", fmt.Errorf("extract host Herdr executable: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(temporaryPath)
		return "", fmt.Errorf("close host Herdr executable stage: %w", closeErr)
	}
	if syncErr != nil {
		os.Remove(temporaryPath)
		return "", fmt.Errorf("flush host Herdr executable stage: %w", syncErr)
	}
	if written == 0 || written > maximumHostHerdrBinarySize {
		os.Remove(temporaryPath)
		return "", fmt.Errorf("host Herdr executable has invalid size %d", written)
	}
	return temporaryPath, nil
}

func replacePinnedHostHerdr(ctx context.Context, source, target, requiredVersion string) (string, error) {
	info, err := os.Lstat(target)
	if err != nil {
		return "", fmt.Errorf("inspect installed host Herdr executable: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("installed host Herdr executable is not a regular file: %s", target)
	}
	backup := target + ".herdr-sandbox-atomic-previous"
	if err := cleanupReplacedHostHerdrBackup(backup); err != nil {
		return "", fmt.Errorf("clear prior host Herdr atomic backup: %w", err)
	}
	if err := replaceFileAtomically(target, source, backup); err != nil {
		recoveryErr := recoverFailedAtomicReplacement(target, source, backup)
		if recoveryErr != nil {
			return "", fmt.Errorf("atomically install pinned host Herdr executable: %w (recovery failed: %v)", err, recoveryErr)
		}
		return "", fmt.Errorf("atomically install pinned host Herdr executable: %w", err)
	}
	rollback := func() error {
		failedReplacement := target + ".herdr-sandbox-failed-replacement"
		if err := cleanupReplacedHostHerdrBackup(failedReplacement); err != nil {
			return fmt.Errorf("clear failed host Herdr replacement backup: %w", err)
		}
		if err := replaceFileAtomically(target, backup, failedReplacement); err != nil {
			recoveryErr := recoverFailedAtomicReplacement(target, backup, failedReplacement)
			if recoveryErr != nil {
				return fmt.Errorf("atomically restore host Herdr executable: %w (recovery failed: %v)", err, recoveryErr)
			}
			return fmt.Errorf("atomically restore host Herdr executable: %w", err)
		}
		_ = cleanupReplacedHostHerdrBackup(failedReplacement)
		return nil
	}
	_, version, err := inspectHostHerdrAt(ctx, target)
	if err != nil || version != requiredVersion {
		rollbackErr := rollback()
		if err != nil {
			if rollbackErr != nil {
				return "", fmt.Errorf("verify installed host Herdr executable: %w (rollback failed: %v)", err, rollbackErr)
			}
			return "", fmt.Errorf("verify installed host Herdr executable: %w", err)
		}
		if rollbackErr != nil {
			return "", fmt.Errorf("installed host Herdr version = %q, required %q (rollback failed: %v)", version, requiredVersion, rollbackErr)
		}
		return "", fmt.Errorf("installed host Herdr version = %q, required %q", version, requiredVersion)
	}
	return backup, nil
}

func recoverFailedAtomicReplacement(target, replacement, backup string) error {
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("host Herdr target became unsafe during atomic replacement: %s", target)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect host Herdr target after atomic replacement: %w", err)
	}
	for _, candidate := range []string{backup, replacement} {
		info, err := os.Lstat(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("inspect host Herdr recovery candidate: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		if err := os.Rename(candidate, target); err != nil {
			return fmt.Errorf("restore host Herdr target after atomic replacement: %w", err)
		}
		return nil
	}
	return errors.New("host Herdr atomic replacement left no recoverable target")
}

func cleanupReplacedHostHerdrBackup(backup string) error {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if err := os.Remove(backup); err == nil || os.IsNotExist(err) {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return lastErr
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
