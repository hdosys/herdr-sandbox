package sandbox

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

//go:embed assets/workstation-prepare.ps1
var workstationPrepareScript []byte

type preparedWorkstationPackage struct {
	Metadata  downloadedPackageMetadata `json:"metadata"`
	CachePath string                    `json:"cachePath"`
}

type preparedWorkstationRust struct {
	Toolchain      string `json:"toolchain"`
	CacheEntryName string `json:"cacheEntryName"`
}

type preparedWorkstationInputs struct {
	SchemaVersion int                          `json:"schemaVersion"`
	Packages      []preparedWorkstationPackage `json:"packages"`
	Tools         []resolvedToolVersion        `json:"tools"`
	Rust          *preparedWorkstationRust     `json:"rust"`
}

func prepareWorkstationInputs(ctx context.Context, stage, destination string, profile WorkstationProfile, output io.Writer) (string, string, error) {
	configurationRoot, err := os.UserConfigDir()
	if err != nil {
		return "", "", err
	}
	configuration, err := loadGlobalConfiguration(filepath.Join(configurationRoot, applicationName, globalConfigurationName))
	if err != nil {
		return "", "", err
	}
	cache, err := effectiveCacheDirectory(configuration.CacheDirectory)
	if err != nil {
		return "", "", err
	}
	if err := validateWorkstationLocalPath(cache); err != nil {
		return "", "", err
	}
	work := filepath.Join(filepath.Dir(cache), "materialization")
	if err := os.MkdirAll(work, 0o700); err != nil {
		return "", "", err
	}
	visualStudioSlot := ""
	if profileNeedsVisualStudioLayout(profile) {
		if err := prepareVisualStudioLayout(ctx, runPlan{RequiresVisualStudioLayout: true, CacheDirectory: cache, RunDirectory: stage}, output); err != nil {
			return "", "", err
		}
		bootstrapper, err := os.Open(filepath.Join(cache, "vsbt", "bootstrapper", "vs_BuildTools.exe"))
		if err != nil {
			return "", "", err
		}
		identity := sha256.New()
		_, copyErr := io.Copy(identity, bootstrapper)
		if err := errors.Join(copyErr, bootstrapper.Close()); err != nil {
			return "", "", err
		}
		for _, name := range []string{"a", "b"} {
			slot := filepath.Join(cache, "vsbt", name)
			data, err := os.ReadFile(filepath.Join(slot, "complete.json"))
			if err != nil {
				continue
			}
			var descriptor struct {
				SchemaVersion      int    `json:"schemaVersion"`
				BootstrapperSHA256 string `json:"bootstrapperSHA256"`
			}
			if json.Unmarshal(data, &descriptor) == nil && descriptor.SchemaVersion == 3 && strings.EqualFold(descriptor.BootstrapperSHA256, fmt.Sprintf("%x", identity.Sum(nil))) {
				visualStudioSlot = slot
				break
			}
		}
		if visualStudioSlot == "" {
			return "", "", errors.New("verified current Visual Studio layout was not published")
		}
	}
	selection := profile
	selection.Files = []WorkstationFile{}
	data, err := json.Marshal(selection)
	if err != nil {
		return "", "", err
	}
	selectionPath := filepath.Join(stage, "materialization-selection.json")
	if err := os.WriteFile(selectionPath, data, 0o600); err != nil {
		return "", "", err
	}
	preparationPath := filepath.Join(stage, "prepare.ps1")
	if err := os.WriteFile(preparationPath, workstationPrepareScript, 0o600); err != nil {
		return "", "", err
	}
	processOwnerPath := filepath.Join(stage, provisioningProcessName)
	if err := os.WriteFile(processOwnerPath, provisioningProcessSource, 0o600); err != nil {
		return "", "", err
	}
	resultPath := filepath.Join(stage, "prepared-inputs.json")
	powerShell, err := windowsPowerShellExecutable()
	if err != nil {
		return "", "", err
	}
	preparationContext, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	command := hiddenCommandContext(preparationContext, powerShell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", preparationPath,
		"-SelectionPath", selectionPath, "-BasePath", filepath.Join(stage, baseProvisioningName),
		"-StackPath", filepath.Join(stage, stackProvisioningName), "-ProcessOwnerPath", processOwnerPath,
		"-CacheDirectory", cache, "-WorkDirectory", work, "-ResultPath", resultPath)
	command.Stdout, command.Stderr = output, output
	fmt.Fprintln(output, "Preparing the selected dependencies through the shared cache owners...")
	if err := command.Run(); err != nil {
		return "", "", fmt.Errorf("materialize workstation inputs: %w", err)
	}
	preparedData, err := os.ReadFile(resultPath)
	if err != nil {
		return "", "", err
	}
	var prepared preparedWorkstationInputs
	if json.Unmarshal(preparedData, &prepared) != nil || prepared.SchemaVersion != 1 || len(prepared.Packages) > 128 {
		return "", "", errors.New("materialized input result is invalid")
	}
	bundlePath := filepath.Join(destination, "herdr-workstation_setup.data.zip")
	bundle, err := os.OpenFile(bundlePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", "", err
	}
	hash := sha256.New()
	archive := zip.NewWriter(io.MultiWriter(bundle, hash))
	seen := map[string]string{}
	expectedPayloads := map[string]string{}
	for _, item := range prepared.Packages {
		relative := filepath.FromSlash(strings.ReplaceAll(item.CachePath, `\`, "/"))
		if !filepath.IsLocal(relative) || filepath.Clean(relative) != relative || strings.ContainsAny(item.Metadata.PayloadName, `/\:`) {
			_ = archive.Close()
			_ = bundle.Close()
			return "", "", errors.New("materialized payload location is unsafe")
		}
		expectedPayloads[filepath.Join(cache, relative, item.Metadata.PayloadName)] = item.Metadata.SHA256
	}
	addFile := func(source, name string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := validateWorkstationLocalPath(source); err != nil {
			return err
		}
		if previous, exists := seen[strings.ToLower(name)]; exists {
			if previous != source {
				return errors.New("materialized archive targets collide")
			}
			return nil
		}
		seen[strings.ToLower(name)] = source
		info, err := os.Stat(source)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("materialized source is not a regular file")
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name, header.Method = name, zip.Store
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		reader, err := os.Open(source)
		if err != nil {
			return err
		}
		payloadHash := sha256.New()
		_, copyErr := io.Copy(io.MultiWriter(writer, payloadHash), workstationContextReader{ctx, reader})
		if err := errors.Join(copyErr, reader.Close()); err != nil {
			return err
		}
		if expected, exists := expectedPayloads[source]; exists && !strings.EqualFold(expected, fmt.Sprintf("%x", payloadHash.Sum(nil))) {
			return errors.New("verified package input changed while materializing the installer")
		}
		return nil
	}
	addTree := func(source, prefix string) error {
		return filepath.WalkDir(source, func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("materialized tree contains a link")
			}
			if entry.IsDir() {
				if prefix == "tools/playwright-browsers" && entry.Name() == ".links" {
					return filepath.SkipDir
				}
				return validateWorkstationLocalPath(name)
			}
			relative, err := filepath.Rel(source, name)
			if err != nil {
				return err
			}
			return addFile(name, path.Join(prefix, filepath.ToSlash(relative)))
		})
	}
	buildErr := addFile(resultPath, "prepared-inputs.json")
	for _, item := range prepared.Packages {
		if buildErr != nil {
			break
		}
		metadata := item.Metadata
		uri, parseErr := url.Parse(metadata.URL)
		_, digestErr := hex.DecodeString(metadata.SHA256)
		relative := filepath.FromSlash(strings.ReplaceAll(item.CachePath, `\`, "/"))
		if parseErr != nil || uri.Scheme != "https" || uri.Host == "" || digestErr != nil || len(metadata.SHA256) != 64 || !validWingetPackageID(metadata.ID) || !filepath.IsLocal(relative) || filepath.Clean(relative) != relative || !strings.HasPrefix(filepath.ToSlash(relative), "packages/") {
			buildErr = errors.New("materialized package metadata is unsafe")
			break
		}
		buildErr = addTree(filepath.Join(cache, relative), path.Join("cache", filepath.ToSlash(relative)))
	}
	if buildErr == nil && prepared.Rust != nil {
		relative := filepath.Join("rust", prepared.Rust.CacheEntryName)
		if !filepath.IsLocal(relative) || strings.ContainsAny(prepared.Rust.CacheEntryName, `/\:`) {
			buildErr = errors.New("materialized Rust cache identity is unsafe")
		} else {
			buildErr = addTree(filepath.Join(cache, relative), path.Join("cache", filepath.ToSlash(relative)))
		}
	}
	for _, name := range []string{"npm-cache", "playwright-browsers"} {
		source := filepath.Join(work, "tools", name)
		prefix := path.Join("tools", name)
		if name == "npm-cache" {
			source = filepath.Join(source, "_cacache")
			prefix = path.Join(prefix, "_cacache")
		}
		if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if buildErr == nil {
			buildErr = addTree(source, prefix)
		}
	}
	if profileNeedsVisualStudioLayout(profile) && buildErr == nil {
		buildErr = addTree(visualStudioSlot, "cache/vsbt/a")
	}
	buildErr = errors.Join(buildErr, archive.Close(), bundle.Sync(), bundle.Close())
	if buildErr != nil {
		return "", "", buildErr
	}
	return bundlePath, fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func profileNeedsVisualStudioLayout(profile WorkstationProfile) bool {
	for _, stack := range profile.Stacks {
		if stack == "rust-msvc" || stack == "cpp" || stack == "handy" {
			return true
		}
	}
	return false
}

type workstationContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader workstationContextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

func applyWorkstationInputs(ctx context.Context, root, sourcePath, expectedDigest string, output io.Writer) error {
	if len(expectedDigest) != 64 {
		return errors.New("materialized data requires its installer-bound SHA-256")
	}
	if _, err := hex.DecodeString(expectedDigest); err != nil {
		return errors.New("materialized data SHA-256 is invalid")
	}
	if err := validateWorkstationLocalPath(sourcePath); err != nil {
		return err
	}
	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64<<30 {
		return errors.New("materialized data is not a bounded regular file")
	}
	fmt.Fprintln(output, "Verifying materialized installer data...")
	hash := sha256.New()
	if _, err := io.Copy(hash, workstationContextReader{ctx, file}); err != nil {
		return err
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != expectedDigest {
		return errors.New("materialized installer data SHA-256 mismatch")
	}
	archive, err := zip.NewReader(file, info.Size())
	if err != nil || len(archive.File) > 200000 {
		return errors.New("materialized installer data has an invalid archive inventory")
	}
	programData := os.Getenv("ProgramData")
	if !filepath.IsAbs(programData) {
		return errors.New("ProgramData is not absolute")
	}
	cache := filepath.Join(programData, "herdr-workstation", "cache")
	if err := validateWorkstationLocalPath(cache); err != nil {
		return err
	}
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return err
	}
	release, err := acquireNamedLifecycleLock(ctx, fmt.Sprintf(`Local\HerdrWorkstationInputs-%x`, sha256.Sum256([]byte(strings.ToLower(cache)))))
	if err != nil {
		return err
	}
	defer release()
	seen := map[string]bool{}
	var total uint64
	fmt.Fprintf(output, "Applying %d verified data files without decompression...\n", len(archive.File))
	for _, entry := range archive.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name
		if path.Clean(name) != name || strings.ContainsAny(name, `\:`) || !filepath.IsLocal(filepath.FromSlash(name)) || seen[strings.ToLower(name)] || !entry.Mode().IsRegular() || entry.Method != zip.Store {
			return errors.New("materialized data contains an unsafe or duplicated entry")
		}
		seen[strings.ToLower(name)] = true
		total += entry.UncompressedSize64
		if total > 64<<30 {
			return errors.New("materialized data exceeds its size boundary")
		}
		var destination string
		switch {
		case name == "prepared-inputs.json":
			destination = filepath.Join(root, "prepared-inputs.json")
		case strings.HasPrefix(name, "cache/"):
			destination = filepath.Join(cache, filepath.FromSlash(strings.TrimPrefix(name, "cache/")))
		case strings.HasPrefix(name, "tools/npm-cache/_cacache/"), strings.HasPrefix(name, "tools/playwright-browsers/"):
			destination = filepath.Join(root, filepath.FromSlash(name))
		default:
			return errors.New("materialized data contains an unexpected target")
		}
		if err := validateWorkstationLocalPath(destination); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		temporary, err := os.CreateTemp(filepath.Dir(destination), ".herdr-input-")
		if err != nil {
			return err
		}
		reader, openErr := entry.Open()
		var copyErr error
		if openErr == nil {
			_, copyErr = io.Copy(temporary, workstationContextReader{ctx, reader})
			copyErr = errors.Join(copyErr, reader.Close())
		}
		writeErr := errors.Join(openErr, copyErr, temporary.Sync(), temporary.Close())
		if writeErr == nil {
			writeErr = os.Rename(temporary.Name(), destination)
		}
		if writeErr != nil {
			_ = os.Remove(temporary.Name())
			return writeErr
		}
	}
	if !seen["prepared-inputs.json"] {
		return errors.New("materialized input metadata is missing")
	}
	fmt.Fprintln(output, "Materialized input integrity and local cache publication completed.")
	return nil
}
