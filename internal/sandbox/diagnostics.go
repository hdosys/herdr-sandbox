package sandbox

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maximumWorkspaceManifestBytes = 64 * 1024
	maximumTimingFileBytes        = 256 * 1024
	maximumTimingRecords          = 128
	maximumDisplayedTimings       = 8
	maximumTimingRecordBytes      = 2 * 1024
	maximumTimingElapsedMS        = int64((24 * time.Hour) / time.Millisecond)
)

// SessionWorkspace is the safe guest-side workspace identity shown by status.
// Host paths and provisioning source paths are deliberately excluded.
type SessionWorkspace struct {
	Name      string
	Directory string
	Active    bool
}

// SessionTiming is one bounded provisioning timing record.
type SessionTiming struct {
	Role                string
	ElapsedMilliseconds int64
	RecordedAtUTC       string
}

type provisioningTimingRecord struct {
	SchemaVersion       int    `json:"schemaVersion"`
	Role                string `json:"role"`
	ElapsedMilliseconds int64  `json:"elapsedMilliseconds"`
	RecordedAtUTC       string `json:"recordedAtUTC"`
}

func readSessionWorkspaces(runDirectory string) ([]SessionWorkspace, error) {
	path := filepath.Join(runDirectory, "input", "provisioning", workspaceManifestName)
	data, found, err := readBoundedRegularFile(path, maximumWorkspaceManifestBytes)
	if err != nil {
		return nil, fmt.Errorf("read workspace diagnostics: %w", err)
	}
	if !found {
		return nil, errors.New("workspace diagnostics are missing")
	}
	manifest, err := decodeGuestWorkspaceManifest(data)
	if err != nil {
		return nil, err
	}
	result := make([]SessionWorkspace, len(manifest.Workspaces))
	for index, workspace := range manifest.Workspaces {
		result[index] = SessionWorkspace{
			Name:      workspace.Name,
			Directory: workspace.Directory,
			Active:    strings.EqualFold(filepath.Clean(workspace.Directory), filepath.Clean(manifest.ActiveWorkspace)),
		}
	}
	return result, nil
}

func decodeGuestWorkspaceManifest(data []byte) (guestWorkspaceManifest, error) {
	fields := []string{"schemaVersion", "activeWorkspace", "workspaces"}
	if err := validateExactJSONObjectShape(data, "workspace manifest", fields); err != nil {
		return guestWorkspaceManifest{}, fmt.Errorf("decode workspace manifest: %w", err)
	}
	var manifest guestWorkspaceManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return guestWorkspaceManifest{}, fmt.Errorf("decode workspace manifest: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return guestWorkspaceManifest{}, fmt.Errorf("decode workspace manifest: %w", err)
	}
	if manifest.SchemaVersion != workspaceManifestSchema || len(manifest.Workspaces) == 0 || len(manifest.Workspaces) > 16 {
		return guestWorkspaceManifest{}, errors.New("workspace manifest schema or count is invalid")
	}
	active := filepath.Clean(manifest.ActiveWorkspace)
	seen := make(map[string]bool, len(manifest.Workspaces))
	activeFound := false
	for _, workspace := range manifest.Workspaces {
		if !workspaceNamePattern.MatchString(workspace.Name) ||
			!strings.EqualFold(filepath.Clean(workspace.Directory), guestWorkspaceDirectory(workspace.Name)) {
			return guestWorkspaceManifest{}, fmt.Errorf("workspace manifest entry is invalid: %q", workspace.Name)
		}
		identity := strings.ToLower(workspace.Name)
		if seen[identity] {
			return guestWorkspaceManifest{}, fmt.Errorf("workspace manifest contains duplicate name %q", workspace.Name)
		}
		seen[identity] = true
		if strings.EqualFold(filepath.Clean(workspace.Directory), active) {
			activeFound = true
		}
	}
	if !activeFound {
		return guestWorkspaceManifest{}, fmt.Errorf("workspace manifest active workspace is not selected: %s", manifest.ActiveWorkspace)
	}
	return manifest, nil
}

func readSessionTimings(statusDirectory string) ([]SessionTiming, error) {
	path := filepath.Join(statusDirectory, "timings.jsonl")
	data, found, err := readBoundedRegularFile(path, maximumTimingFileBytes)
	if err != nil {
		return nil, fmt.Errorf("read provisioning timings: %w", err)
	}
	if !found || len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), maximumTimingRecordBytes)
	records := make([]SessionTiming, 0)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			return nil, errors.New("provisioning timings contain an empty record")
		}
		if len(records) >= maximumTimingRecords {
			return nil, fmt.Errorf("provisioning timing count exceeds %d", maximumTimingRecords)
		}
		record, err := decodeProvisioningTiming(line)
		if err != nil {
			return nil, fmt.Errorf("decode provisioning timing %d: %w", len(records), err)
		}
		records = append(records, SessionTiming{
			Role:                record.Role,
			ElapsedMilliseconds: record.ElapsedMilliseconds,
			RecordedAtUTC:       record.RecordedAtUTC,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan provisioning timings: %w", err)
	}
	if len(records) > maximumDisplayedTimings {
		records = records[len(records)-maximumDisplayedTimings:]
	}
	return records, nil
}

func decodeProvisioningTiming(data []byte) (provisioningTimingRecord, error) {
	fields := []string{"schemaVersion", "role", "elapsedMilliseconds", "recordedAtUTC"}
	if err := validateExactJSONObjectShape(data, "provisioning timing", fields); err != nil {
		return provisioningTimingRecord{}, err
	}
	var record provisioningTimingRecord
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return provisioningTimingRecord{}, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return provisioningTimingRecord{}, err
	}
	if record.SchemaVersion != 1 {
		return provisioningTimingRecord{}, fmt.Errorf("schemaVersion = %d, want 1", record.SchemaVersion)
	}
	if err := validateTerminalText("timing role", record.Role, 128); err != nil {
		return provisioningTimingRecord{}, err
	}
	if record.ElapsedMilliseconds < 0 || record.ElapsedMilliseconds > maximumTimingElapsedMS {
		return provisioningTimingRecord{}, fmt.Errorf("elapsedMilliseconds = %d", record.ElapsedMilliseconds)
	}
	if _, err := time.Parse(time.RFC3339Nano, record.RecordedAtUTC); err != nil {
		return provisioningTimingRecord{}, fmt.Errorf("parse recordedAtUTC: %w", err)
	}
	return record, nil
}

func readBoundedRegularFile(path string, maximum int64) ([]byte, bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	if err := validateOpenedBoundedRegularFile(path, file, maximum); err != nil {
		return nil, false, err
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > maximum {
		return nil, false, fmt.Errorf("%s exceeds %d bytes", path, maximum)
	}
	if err := validateOpenedBoundedRegularFile(path, file, maximum); err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func validateOpenedBoundedRegularFile(path string, file *os.File, maximum int64) error {
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	current, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect opened file path %s: %w", path, err)
	}
	for _, info := range []os.FileInfo{opened, current} {
		reparse, err := fileInfoIsReparsePoint(info)
		if err != nil {
			return err
		}
		if reparse || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maximum {
			return fmt.Errorf("%s is not one bounded regular non-reparse file", path)
		}
	}
	if !os.SameFile(opened, current) {
		return fmt.Errorf("%s changed while opening the bounded regular file", path)
	}
	return nil
}
