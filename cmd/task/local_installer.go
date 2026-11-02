package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"herdr-sandbox/internal/productidentity"
)

// localInstallerEvidence binds only the current candidate, not a history or cache.
type localInstallerEvidence struct {
	Identity        buildIdentity     `json:"identity"`
	InstallerSHA256 string            `json:"installerSHA256"`
	PayloadSHA256   map[string]string `json:"payloadSHA256"`
}

func cleanPackageSource(ctx context.Context) (string, error) {
	output, err := hiddenCommandContext(ctx, "git", "status", "--porcelain=v1", "--untracked-files=all").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("inspect package source: %w", err)
	}
	if len(strings.TrimSpace(string(output))) != 0 {
		return "", errors.New("packaging requires clean committed source")
	}
	return sourceRevision(ctx)
}

func validateLocalInstallerSource(ctx context.Context, revision string) error {
	current, err := cleanPackageSource(ctx)
	if err != nil || current == revision {
		return err
	}
	// Documentation-only commits do not invalidate an otherwise exact candidate.
	args := []string{"diff", "--quiet", revision, current, "--", "."}
	for _, name := range []string{"AGENTS.md", "AGENT_IMPROVEMENTS.md", "ARCHITECTURE.md", "BACKLOG.md", "CHANGELOG.md", "PRODUCT.md", "README.md", "SECURITY.md"} {
		args = append(args, ":(top,exclude)"+name)
	}
	if err := hiddenCommandContext(ctx, "git", args...).Run(); err != nil {
		return fmt.Errorf("local installer source differs; run package for the current source: %w", err)
	}
	return nil
}

func packagePayloadHashes(stage string) (map[string]string, error) {
	hashes := make(map[string]string, len(releasePackageFiles))
	for _, file := range releasePackageFiles {
		hash, err := fileSHA256(filepath.Join(stage, file.Name))
		if err != nil {
			return nil, err
		}
		hashes[file.Name] = hex.EncodeToString(hash)
	}
	return hashes, nil
}

func captureLocalInstallerEvidence(identity buildIdentity, paths releasePackagePaths) (localInstallerEvidence, error) {
	payload, err := packagePayloadHashes(paths.Stage)
	if err != nil {
		return localInstallerEvidence{}, err
	}
	hash, err := fileSHA256(paths.Installer)
	return localInstallerEvidence{Identity: identity, InstallerSHA256: hex.EncodeToString(hash), PayloadSHA256: payload}, err
}

func readLocalInstallerEvidence(root string, version releaseVersion) (localInstallerEvidence, error) {
	var evidence localInstallerEvidence
	path := filepath.Join(root, "build", "dist", canonicalLocalEvidenceName)
	info, err := os.Lstat(path)
	if err != nil {
		return evidence, fmt.Errorf("local installer evidence is required; run package first: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 16*1024 {
		return evidence, errors.New("local installer evidence must be a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return evidence, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 16*1024+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return evidence, fmt.Errorf("decode local installer evidence: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return evidence, errors.New("local installer evidence contains trailing data")
	}
	revision, err := normalizeSourceRevision(evidence.Identity.Revision)
	if err != nil || revision != evidence.Identity.Revision || evidence.Identity.Version != version.Display ||
		!productidentity.ValidBuildFreshness(evidence.Identity.Freshness) {
		return evidence, errors.New("local installer evidence has a mismatched or invalid build identity")
	}
	if len(evidence.PayloadSHA256) != len(releasePackageFiles) {
		return evidence, errors.New("local installer evidence has an unexpected payload set")
	}
	hashes := []string{evidence.InstallerSHA256}
	for _, file := range releasePackageFiles {
		hashes = append(hashes, evidence.PayloadSHA256[file.Name])
	}
	for _, hash := range hashes {
		decoded, err := hex.DecodeString(hash)
		if err != nil || len(decoded) != sha256.Size || hash != strings.ToLower(hash) {
			return evidence, errors.New("local installer evidence has an invalid SHA-256")
		}
	}
	return evidence, nil
}

// Copy once into the acceptance workspace. Later canonical replacements cannot
// change the installer exercised by any of the existing lifecycle transitions.
func snapshotLocalInstaller(root, destination string, evidence localInstallerEvidence) error {
	path := canonicalLocalInstallerPath(root)
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("local installer must be a nonempty regular file")
	}
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(target, hash), source)
	if err := errors.Join(copyErr, target.Close()); err != nil {
		return fmt.Errorf("snapshot local installer: %w", err)
	}
	if hex.EncodeToString(hash.Sum(nil)) != evidence.InstallerSHA256 {
		return errors.New("local installer does not match its evidence; run package again")
	}
	return nil
}
