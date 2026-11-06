package sandbox

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

type downloadedInstaller struct {
	Architecture  string `yaml:"Architecture"`
	InstallerType string `yaml:"InstallerType"`
	Scope         string `yaml:"Scope"`
	InstallerURL  string `yaml:"InstallerUrl"`
	SHA256        string `yaml:"InstallerSha256"`
}

type downloadedManifest struct {
	PackageIdentifier string                `yaml:"PackageIdentifier"`
	PackageVersion    string                `yaml:"PackageVersion"`
	InstallerType     string                `yaml:"InstallerType"`
	Scope             string                `yaml:"Scope"`
	Installers        []downloadedInstaller `yaml:"Installers"`
}

type DownloadedPackageSelection struct {
	Directory        string
	ID               string
	Version          string
	Architecture     string
	InstallerType    string
	Scope            string
	PayloadExtension string
}

type downloadedPackageMetadata struct {
	ID            string `json:"Id"`
	Version       string `json:"Version"`
	Architecture  string `json:"Architecture"`
	InstallerType string `json:"InstallerType"`
	Scope         string `json:"Scope"`
	URL           string `json:"Url"`
	SHA256        string `json:"Sha256"`
	PayloadName   string `json:"PayloadName"`
	PayloadPath   string `json:"PayloadPath"`
}

// InspectDownloadedPackage consumes WinGet's invariant, selected-installer YAML.
// It never parses translated UI labels or executes an installer. WinGet's own
// verified download is bound again to the selected identity and SHA-256 here.
func InspectDownloadedPackage(selection DownloadedPackageSelection, output io.Writer) error {
	metadata, err := inspectDownloadedPackage(selection)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(metadata)
}

func inspectDownloadedPackage(selection DownloadedPackageSelection) (downloadedPackageMetadata, error) {
	var metadata downloadedPackageMetadata
	if !validWingetPackageID(selection.ID) || !slices.Contains([]string{"x64", "x86"}, selection.Architecture) {
		return metadata, errors.New("invalid selected package identity or architecture")
	}
	if err := validateWorkstationLocalPath(selection.Directory); err != nil {
		return metadata, err
	}
	entries, err := os.ReadDir(selection.Directory)
	if err != nil {
		return metadata, err
	}
	if len(entries) != 2 {
		return metadata, errors.New("WinGet download must contain exactly one selected manifest and one payload")
	}
	var manifestPath, payload string
	for _, entry := range entries {
		file := filepath.Join(selection.Directory, entry.Name())
		if err := validateWorkstationLocalPath(file); err != nil {
			return metadata, err
		}
		info, err := os.Lstat(file)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return metadata, errors.New("WinGet download contains an empty or nonregular entry")
		}
		if strings.EqualFold(filepath.Ext(file), ".yaml") {
			if manifestPath != "" {
				return metadata, errors.New("WinGet download contains multiple manifests")
			}
			manifestPath = file
		} else {
			payload = file
		}
	}
	if manifestPath == "" || payload == "" {
		return metadata, errors.New("WinGet download is missing its manifest or payload")
	}
	data, found, err := readBoundedRegularFile(manifestPath, 1024*1024)
	if err != nil || !found {
		return metadata, errors.New("selected installer manifest is unavailable")
	}
	manifest, err := decodeDownloadedManifest(data)
	if err != nil {
		return metadata, err
	}
	if !strings.EqualFold(manifest.PackageIdentifier, selection.ID) || !validWingetPackageVersion(manifest.PackageVersion) || (selection.Version != "" && manifest.PackageVersion != selection.Version) || len(manifest.Installers) != 1 {
		return metadata, errors.New("downloaded manifest does not match the selected package/version or unique installer")
	}
	installer := manifest.Installers[0]
	if installer.InstallerType == "" {
		installer.InstallerType = manifest.InstallerType
	}
	if installer.Scope == "" {
		installer.Scope = manifest.Scope
	}
	if installer.Architecture != selection.Architecture && installer.Architecture != "neutral" {
		return metadata, errors.New("downloaded installer architecture does not match selection")
	}
	if selection.InstallerType != "" && installer.InstallerType != selection.InstallerType {
		return metadata, errors.New("downloaded installer type does not match selection")
	}
	if selection.Scope != "" && installer.Scope != "" && installer.Scope != selection.Scope {
		return metadata, errors.New("downloaded installer scope does not match selection")
	}
	uri, err := url.Parse(installer.InstallerURL)
	if err != nil || uri.Scheme != "https" || uri.Host == "" || uri.User != nil {
		return metadata, errors.New("downloaded installer URL is not an authenticated HTTPS origin")
	}
	digest, err := hex.DecodeString(installer.SHA256)
	if err != nil || len(digest) != sha256.Size {
		return metadata, errors.New("downloaded manifest has no valid SHA-256")
	}
	extension := strings.ToLower(path.Ext(uri.Path))
	if extension == "" {
		extension = selection.PayloadExtension
	}
	if !slices.Contains([]string{".exe", ".msi", ".zip", ".msix", ".msixbundle", ".appx", ".appxbundle"}, extension) || (selection.PayloadExtension != "" && extension != selection.PayloadExtension) {
		return metadata, errors.New("downloaded installer extension is unsupported or does not match selection")
	}
	file, err := os.Open(payload)
	if err != nil {
		return metadata, err
	}
	hash := sha256.New()
	_, readErr := io.Copy(hash, file)
	if err := errors.Join(readErr, file.Close()); err != nil {
		return metadata, err
	}
	if !bytes.Equal(hash.Sum(nil), digest) {
		return metadata, errors.New("downloaded payload SHA-256 differs from selected manifest; installation refused")
	}
	return downloadedPackageMetadata{ID: manifest.PackageIdentifier, Version: manifest.PackageVersion, Architecture: selection.Architecture,
		InstallerType: installer.InstallerType, Scope: selection.Scope, URL: installer.InstallerURL,
		SHA256: strings.ToUpper(installer.SHA256), PayloadName: "payload" + extension, PayloadPath: payload}, nil
}

func decodeDownloadedManifest(data []byte) (downloadedManifest, error) {
	var manifest downloadedManifest
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("decode selected installer YAML: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return manifest, errors.New("selected installer manifest contains trailing YAML documents")
	}
	return manifest, nil
}
