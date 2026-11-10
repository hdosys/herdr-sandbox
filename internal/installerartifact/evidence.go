// Package installerartifact owns the current local installer provenance contract.
package installerartifact

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"herdr-sandbox/internal/productidentity"
)

type Identity struct {
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	Freshness string `json:"freshness"`
}

type Evidence struct {
	Product             string            `json:"product"`
	ProfileSHA256       string            `json:"profileSHA256,omitempty"`
	ConfigurationFiles  int               `json:"configurationFiles,omitempty"`
	PackageIDs          []string          `json:"packageIDs,omitempty"`
	ConfigurationSource string            `json:"configurationSource,omitempty"`
	Identity            Identity          `json:"identity"`
	InstallerSHA256     string            `json:"installerSHA256"`
	PayloadSHA256       map[string]string `json:"payloadSHA256"`
}

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func Read(path string) (Evidence, error) {
	var value Evidence
	info, err := os.Lstat(path)
	if err != nil {
		return value, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 16*1024 {
		return value, errors.New("installer evidence must be a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return value, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 16*1024+1))
	if err != nil {
		return value, err
	}
	return Decode(data)
}

func Decode(data []byte) (Evidence, error) {
	var value Evidence
	if len(data) > 16*1024 {
		return value, errors.New("installer evidence exceeds its size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("decode installer evidence: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return value, errors.New("installer evidence has trailing data")
	}
	if value.Product != productidentity.ApplicationName && value.Product != productidentity.WorkstationApplicationName {
		return value, errors.New("installer product identity is missing or unsupported; rebuild the matching candidate")
	}
	if !revisionPattern.MatchString(value.Identity.Revision) || !versionPattern.MatchString(value.Identity.Version) || !productidentity.ValidBuildFreshness(value.Identity.Freshness) {
		return value, errors.New("invalid installer build identity")
	}
	if len(value.PayloadSHA256) != 4 {
		return value, errors.New("installer evidence has an unexpected payload set")
	}
	hashes := []string{value.InstallerSHA256}
	if value.Product == productidentity.WorkstationApplicationName {
		hashes = append(hashes, value.ProfileSHA256)
		if value.ConfigurationFiles < 1 || value.ConfigurationFiles > 2048 || len(value.PackageIDs) > 128 || (value.ConfigurationSource != "current-windows-user" && value.ConfigurationSource != "current-sandbox-user") {
			return value, errors.New("workstation candidate must identify its embedded personal configuration")
		}
	}
	for _, name := range []string{productidentity.ExecutableName, productidentity.BaseScriptName, productidentity.StackScriptName, productidentity.LicenseName} {
		hashes = append(hashes, value.PayloadSHA256[name])
	}
	for _, hash := range hashes {
		decoded, err := hex.DecodeString(hash)
		if err != nil || len(decoded) != sha256.Size || hash != strings.ToLower(hash) {
			return value, errors.New("installer evidence has an invalid SHA-256")
		}
	}
	return value, nil
}

// Snapshot binds the single copy that will execute, even if the canonical file changes.
func Snapshot(path, destination string, evidence Evidence) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 512*1024*1024 {
		return errors.New("installer must be a nonempty regular file")
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
	n, copyErr := io.Copy(io.MultiWriter(target, hash), io.LimitReader(source, 512*1024*1024+1))
	if err := errors.Join(copyErr, target.Close()); err != nil {
		return fmt.Errorf("snapshot installer: %w", err)
	}
	if n != info.Size() || hex.EncodeToString(hash.Sum(nil)) != evidence.InstallerSHA256 {
		return errors.New("installer does not match its provenance evidence")
	}
	return nil
}
