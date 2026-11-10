package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"herdr-sandbox/internal/installerartifact"
	"herdr-sandbox/internal/productidentity"
)

// localInstallerEvidence binds only the current candidate, not a history or cache.
type localInstallerEvidence = installerartifact.Evidence
type buildIdentity = installerartifact.Identity

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
	return localInstallerEvidence{Product: productidentity.ApplicationName, Identity: identity, InstallerSHA256: hex.EncodeToString(hash), PayloadSHA256: payload}, err
}

func readLocalInstallerEvidence(root string, version releaseVersion) (localInstallerEvidence, error) {
	path := filepath.Join(root, "build", "dist", canonicalLocalEvidenceName)
	evidence, err := installerartifact.Read(path)
	if err != nil {
		return evidence, err
	}
	if evidence.Product != productidentity.ApplicationName || evidence.Identity.Version != version.Display {
		return evidence, errors.New("local installer evidence has a mismatched version")
	}
	return evidence, nil
}

// Copy once into the acceptance workspace. Later canonical replacements cannot
// change the installer exercised by any of the existing lifecycle transitions.
func snapshotLocalInstaller(root, destination string, evidence localInstallerEvidence) error {
	return installerartifact.Snapshot(canonicalLocalInstallerPath(root), destination, evidence)
}
