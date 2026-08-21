package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	hostHerdrCompatibilityAction    = "ensure the `herdr-win` Windows `herdr.exe` with working unattended remote provisioning is on PATH, then retry"
	hostHerdrInspectionTimeout      = 30 * time.Second
	hostHerdrProvisionTimeout       = 5 * time.Minute
	maximumRemoteProvisionOutput    = 64 * 1024
	maximumHostHerdrRuntimeFileSize = 256 * 1024 * 1024
	maximumHostHerdrRuntimeSize     = 512 * 1024 * 1024
	hostHerdrChangedAction          = "retry `sandbox up` with the stable host Herdr command and runtime"
	hostHerdrVersionMarker          = "herdr-win"
	remoteProvisionBinaryInstalled  = "installed"
	remoteProvisionBinaryMatching   = "already_matching"
	remoteProvisionServerStarted    = "started"
	remoteProvisionServerReloaded   = "reloaded"
	remoteProvisionServerRestarted  = "restarted"
)

var hostHerdrRuntimeLayout = []string{
	"herdr.exe",
	"conpty/conpty.dll",
	"conpty/herdr-conpty.json",
	"conpty/x64/OpenConsole.exe",
	"conpty/arm64/OpenConsole.exe",
}

// HostHerdr is the read-only result of resolving one compatible host command
// and its active physical runtime. Its fields remain private so callers cannot
// construct a partially verified identity.
type HostHerdr struct {
	commandPath       string
	commandSHA256     string
	commandSize       int64
	runtimeExecutable string
	version           string
	runtimeVersion    string
	protocol          int
	files             []hostHerdrRuntimeFile
}

type hostHerdrRuntimeFile struct {
	RelativePath string
	SourcePath   string
	SHA256       string
	Size         int64
}

type hostHerdrClientStatus struct {
	Version      string  `json:"version"`
	HerdrVersion string  `json:"herdr_version"`
	BuildID      *string `json:"build_id"`
	Protocol     int     `json:"protocol"`
	Binary       string  `json:"binary"`
	Session      *string `json:"session"`
}

type remoteProvisionResult struct {
	Target        string `json:"target"`
	Platform      string `json:"platform"`
	Binary        string `json:"binary"`
	BinaryOutcome string `json:"binary_outcome"`
	ServerOutcome string `json:"server_outcome"`
	Version       string `json:"version"`
	Protocol      int    `json:"protocol"`
}

// ResolveHostHerdr verifies the installed command without changing it. Host
// installation and updates remain outside Sandbox ownership.
func ResolveHostHerdr(ctx context.Context) (HostHerdr, error) {
	commandPath, err := resolveInstalledHostHerdrCommand()
	if err != nil {
		return HostHerdr{}, hostHerdrCompatibilityError("a compatible host Herdr command is required: %v", err)
	}
	return inspectCompatibleHostHerdr(ctx, commandPath)
}

func inspectCompatibleHostHerdr(ctx context.Context, commandPath string) (HostHerdr, error) {
	operationContext, cancel := context.WithTimeout(ctx, hostHerdrInspectionTimeout)
	defer cancel()

	commandPath, err := canonicalHostHerdrExecutable(commandPath, "host Herdr command")
	if err != nil {
		return HostHerdr{}, hostHerdrCompatibilityError("the installed host Herdr command is unsafe: %v", err)
	}
	cliVersion, err := inspectHostHerdrVersion(operationContext, commandPath)
	if err != nil {
		return HostHerdr{}, hostHerdrCompatibilityError("the installed host Herdr version could not be verified: %v", err)
	}
	if err := verifyHostHerdrRemoteCapability(operationContext, commandPath); err != nil {
		return HostHerdr{}, err
	}
	statusCommand := hiddenCommandContext(operationContext, commandPath, "status", "client", "--json")
	statusCommand.Env = attachEnvironment(childProcessEnvironment(os.Environ()))
	statusOutput, err := statusCommand.CombinedOutput()
	if err != nil {
		if remoteUnsupportedDiagnostic(statusOutput) {
			return HostHerdr{}, hostHerdrCompatibilityError("the installed host Herdr build reports that remote use is unsupported: %s", boundedText(statusOutput))
		}
		return HostHerdr{}, hostHerdrCompatibilityError("inspect the active host Herdr runtime: %v: %s", err, boundedText(statusOutput))
	}
	status, err := parseHostHerdrClientStatus(statusOutput)
	if err != nil {
		return HostHerdr{}, hostHerdrCompatibilityError("inspect the active host Herdr runtime: %v", err)
	}
	runtimeExecutable, err := canonicalHostHerdrExecutable(status.Binary, "active host Herdr runtime")
	if err != nil {
		return HostHerdr{}, hostHerdrCompatibilityError("resolve the active host Herdr runtime: %v", err)
	}
	runtimeFiles, err := inspectHostHerdrRuntimeFiles(runtimeExecutable)
	if err != nil {
		return HostHerdr{}, hostHerdrCompatibilityError("inspect the active host Herdr Windows runtime: %v", err)
	}
	commandSHA256, commandSize, err := sha256HostHerdrFile(commandPath)
	if err != nil {
		return HostHerdr{}, hostHerdrCompatibilityError("fingerprint the host Herdr command: %v", err)
	}
	if commandSize <= 0 {
		return HostHerdr{}, hostHerdrCompatibilityError("the host Herdr command has invalid size %d", commandSize)
	}
	host := HostHerdr{
		commandPath:       commandPath,
		commandSHA256:     commandSHA256,
		commandSize:       commandSize,
		runtimeExecutable: runtimeExecutable,
		version:           cliVersion,
		runtimeVersion:    status.Version,
		protocol:          status.Protocol,
		files:             runtimeFiles,
	}
	if err := host.validate(); err != nil {
		return HostHerdr{}, hostHerdrCompatibilityError("validate the active host Herdr runtime: %v", err)
	}
	return host, nil
}

func resolveInstalledHostHerdrCommand() (string, error) {
	path, err := exec.LookPath("herdr.exe")
	if err != nil {
		return "", fmt.Errorf("locate herdr.exe on PATH: %w", err)
	}
	return canonicalHostHerdrExecutable(path, "host Herdr command")
}

func canonicalHostHerdrExecutable(path, role string) (string, error) {
	if !filepath.IsAbs(path) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve %s path: %w", role, err)
		}
		path = absolute
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", role, err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("make %s path absolute: %w", role, err)
	}
	directory, err := canonicalMappedDirectory(filepath.Dir(resolved))
	if err != nil {
		return "", fmt.Errorf("validate %s directory: %w", role, err)
	}
	resolved = filepath.Join(directory, filepath.Base(resolved))
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", role, err)
	}
	if err := validateHostHerdrRegularFile(info, resolved, role); err != nil {
		return "", err
	}
	if !strings.EqualFold(filepath.Base(resolved), "herdr.exe") {
		return "", fmt.Errorf("%s is not named herdr.exe: %s", role, resolved)
	}
	return resolved, nil
}

func inspectHostHerdrVersion(ctx context.Context, path string) (string, error) {
	command := hiddenCommandContext(ctx, path, "--version")
	command.Env = attachEnvironment(childProcessEnvironment(os.Environ()))
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run --version: %w: %s", err, boundedText(output))
	}
	version := strings.TrimSpace(string(output))
	if !strings.Contains(version, hostHerdrVersionMarker) || strings.ContainsAny(version, "\r\n") || len(version) > 256 {
		return "", fmt.Errorf("unexpected --version output %q", version)
	}
	return version, nil
}

func verifyHostHerdrRemoteCapability(ctx context.Context, path string) error {
	remoteCommand := hiddenCommandContext(ctx, path, "--remote")
	remoteCommand.Env = attachEnvironment(childProcessEnvironment(os.Environ()))
	output, err := remoteCommand.CombinedOutput()
	if remoteUnsupportedDiagnostic(output) {
		return hostHerdrCompatibilityError("the installed host Herdr build reports that remote use is unsupported: %s", boundedText(output))
	}
	if err == nil {
		return hostHerdrCompatibilityError("the installed host Herdr command accepted --remote without its required target")
	}
	lower := strings.ToLower(string(output))
	mentionsMissingTarget := strings.Contains(lower, "--remote") &&
		(strings.Contains(lower, "missing") || strings.Contains(lower, "required")) &&
		(strings.Contains(lower, "value") || strings.Contains(lower, "target") || strings.Contains(lower, "argument"))
	if !mentionsMissingTarget {
		return hostHerdrCompatibilityError("the installed host Herdr command did not expose the expected remote interface: %s", boundedText(output))
	}

	// A target is required to reach the platform capability owner. Remove PATH so
	// a compatible build can cross that boundary only as far as its first local
	// ssh.exe lookup; no SSH process or network connection can be started.
	command := hiddenCommandContext(ctx, path, "--remote", "herdr-sandbox-capability-probe.invalid", "--provision", "--yes", "--json")
	command.Env = hostHerdrCapabilityEnvironment(os.Environ())
	output, err = command.CombinedOutput()
	if remoteUnsupportedDiagnostic(output) {
		return hostHerdrCompatibilityError("the installed host Herdr build reports that remote use is unsupported: %s", boundedText(output))
	}
	if err == nil {
		return hostHerdrCompatibilityError("the installed host Herdr unattended remote provisioning probe unexpectedly succeeded")
	}
	if ctx.Err() != nil {
		return hostHerdrCompatibilityError("the installed host Herdr unattended remote provisioning probe did not terminate: %v", ctx.Err())
	}
	if !expectedSSHLookupFailure(output) {
		return hostHerdrCompatibilityError("the installed host Herdr command did not expose unattended remote provisioning before the expected ssh.exe lookup: %v: %s", err, boundedText(output))
	}
	return nil
}

func hostHerdrCapabilityEnvironment(parent []string) []string {
	environment := attachEnvironment(childProcessEnvironment(parent))
	filtered := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		name, _, found := strings.Cut(entry, "=")
		if found && strings.EqualFold(name, "PATH") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered, "PATH=")
}

func remoteUnsupportedDiagnostic(output []byte) bool {
	lower := strings.ToLower(string(output))
	return strings.Contains(lower, "unsupported") ||
		(strings.Contains(lower, "remote") && strings.Contains(lower, "not supported"))
}

func expectedSSHLookupFailure(output []byte) bool {
	return strings.TrimSpace(string(output)) == "error: program not found"
}

func parseHostHerdrClientStatus(output []byte) (hostHerdrClientStatus, error) {
	trimmed := bytes.TrimSpace(output)
	if err := validateExactJSONObjectShape(trimmed, "host Herdr client status", []string{
		"version", "herdr_version", "build_id", "protocol", "binary", "session",
	}); err != nil {
		return hostHerdrClientStatus{}, fmt.Errorf("decode `herdr status client --json`: %w", err)
	}
	var status hostHerdrClientStatus
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&status); err != nil {
		return hostHerdrClientStatus{}, fmt.Errorf("decode `herdr status client --json`: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return hostHerdrClientStatus{}, errors.New("`herdr status client --json` contains trailing data")
	}
	if err := validateTerminalText("host Herdr client version", status.Version, 250); err != nil || strings.HasPrefix(status.Version, "herdr ") {
		return hostHerdrClientStatus{}, fmt.Errorf("invalid host Herdr client version %q", status.Version)
	}
	if err := validateTerminalText("host Herdr compatibility version", status.HerdrVersion, 64); err != nil {
		return hostHerdrClientStatus{}, fmt.Errorf("invalid host Herdr compatibility version %q", status.HerdrVersion)
	}
	if status.BuildID != nil {
		if !validHostHerdrBuildID(*status.BuildID) ||
			(!strings.HasSuffix(status.Version, "+"+*status.BuildID) && !strings.HasSuffix(status.Version, "."+*status.BuildID)) {
			return hostHerdrClientStatus{}, fmt.Errorf("invalid host Herdr build identity %q for runtime %q", *status.BuildID, status.Version)
		}
	}
	if status.Protocol < 1 {
		return hostHerdrClientStatus{}, fmt.Errorf("invalid host Herdr client protocol %d", status.Protocol)
	}
	if !filepath.IsAbs(status.Binary) {
		return hostHerdrClientStatus{}, fmt.Errorf("host Herdr client binary is not absolute: %q", status.Binary)
	}
	if status.Session != nil {
		if err := validateTerminalText("host Herdr session", *status.Session, 128); err != nil {
			return hostHerdrClientStatus{}, fmt.Errorf("invalid host Herdr session %q", *status.Session)
		}
	}
	return status, nil
}

func validHostHerdrBuildID(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 2 || len(parts[0]) != 12 || len(parts[1]) != 12 {
		return false
	}
	for _, part := range parts {
		for _, character := range part {
			if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
				return false
			}
		}
	}
	return true
}

func inspectHostHerdrRuntimeFiles(runtimeExecutable string) ([]hostHerdrRuntimeFile, error) {
	root := filepath.Dir(runtimeExecutable)
	files := make([]hostHerdrRuntimeFile, 0, len(hostHerdrRuntimeLayout))
	var total int64
	for index, relative := range hostHerdrRuntimeLayout {
		if index == 1 {
			bundle := filepath.Join(root, "conpty")
			info, err := os.Lstat(bundle)
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("inspect optional host Herdr ConPTY bundle: %w", err)
			}
			reparse, err := fileInfoIsReparsePoint(info)
			if err != nil {
				return nil, fmt.Errorf("inspect optional host Herdr ConPTY bundle reparse state: %w", err)
			}
			if reparse || !info.IsDir() {
				return nil, fmt.Errorf("optional host Herdr ConPTY bundle is not a regular non-reparse directory: %s", bundle)
			}
		}
		source := filepath.Join(root, filepath.FromSlash(relative))
		file, err := inspectHostHerdrRuntimeFile(source, relative)
		if err != nil {
			return nil, err
		}
		total += file.Size
		if total > maximumHostHerdrRuntimeSize {
			return nil, fmt.Errorf("host Herdr runtime exceeds %d bytes", maximumHostHerdrRuntimeSize)
		}
		files = append(files, file)
	}
	return files, nil
}

func inspectHostHerdrRuntimeFile(path, relative string) (hostHerdrRuntimeFile, error) {
	if err := rejectMappedPathReparsePoints(filepath.Dir(path)); err != nil {
		return hostHerdrRuntimeFile{}, fmt.Errorf("validate runtime file %s directory: %w", relative, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return hostHerdrRuntimeFile{}, fmt.Errorf("inspect runtime file %s: %w", relative, err)
	}
	if err := validateHostHerdrRegularFile(info, path, "host Herdr runtime file"); err != nil {
		return hostHerdrRuntimeFile{}, err
	}
	if info.Size() <= 0 || info.Size() > maximumHostHerdrRuntimeFileSize {
		return hostHerdrRuntimeFile{}, fmt.Errorf("host Herdr runtime file %s has invalid size %d", relative, info.Size())
	}
	digest, size, err := sha256HostHerdrFile(path)
	if err != nil {
		return hostHerdrRuntimeFile{}, fmt.Errorf("hash host Herdr runtime file %s: %w", relative, err)
	}
	if size != info.Size() {
		return hostHerdrRuntimeFile{}, fmt.Errorf("host Herdr runtime file %s changed size during inspection", relative)
	}
	return hostHerdrRuntimeFile{RelativePath: relative, SourcePath: path, SHA256: digest, Size: size}, nil
}

func sha256HostHerdrFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, maximumHostHerdrRuntimeFileSize+1))
	if err != nil {
		return "", 0, err
	}
	if written > maximumHostHerdrRuntimeFileSize {
		return "", 0, fmt.Errorf("file exceeds %d bytes", maximumHostHerdrRuntimeFileSize)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), written, nil
}

func (host HostHerdr) validate() error {
	if !filepath.IsAbs(host.commandPath) || !filepath.IsAbs(host.runtimeExecutable) {
		return errors.New("host Herdr command and runtime paths must be absolute")
	}
	if host.commandSize <= 0 || len(host.commandSHA256) != 64 {
		return errors.New("host Herdr command fingerprint is invalid")
	}
	if !strings.Contains(host.version, hostHerdrVersionMarker) || strings.ContainsAny(host.version, "\r\n") ||
		host.runtimeVersion == "" || strings.ContainsAny(host.runtimeVersion, "\r\n") ||
		len(host.runtimeVersion) > 250 || host.protocol < 1 {
		return errors.New("host Herdr version or protocol is invalid")
	}
	if len(host.files) != 1 && len(host.files) != len(hostHerdrRuntimeLayout) {
		return fmt.Errorf("host Herdr runtime file count = %d, want 1 or %d", len(host.files), len(hostHerdrRuntimeLayout))
	}
	expectedLayout := hostHerdrRuntimeLayout
	if len(host.files) == 1 {
		expectedLayout = hostHerdrRuntimeLayout[:1]
	}
	foundRuntime := false
	for index, file := range host.files {
		if file.RelativePath != expectedLayout[index] {
			return fmt.Errorf("host Herdr runtime file %d path = %q, want %q", index, file.RelativePath, expectedLayout[index])
		}
		if file.RelativePath == "herdr.exe" {
			foundRuntime = strings.EqualFold(file.SourcePath, host.runtimeExecutable)
			if strings.EqualFold(host.commandPath, host.runtimeExecutable) &&
				(file.SHA256 != host.commandSHA256 || file.Size != host.commandSize) {
				return errors.New("host Herdr command changed while its physical runtime was inspected")
			}
		}
		if !filepath.IsAbs(file.SourcePath) || file.Size <= 0 || len(file.SHA256) != 64 {
			return fmt.Errorf("host Herdr runtime file %q is invalid", file.RelativePath)
		}
	}
	if !foundRuntime {
		return errors.New("host Herdr runtime file set does not own its reported executable")
	}
	return nil
}

func (host HostHerdr) verifyUnchanged(ctx context.Context) error {
	current, err := inspectCompatibleHostHerdr(ctx, host.commandPath)
	if err != nil {
		return fmt.Errorf("revalidate host Herdr before reporting the Sandbox ready: %w; %s", err, hostHerdrChangedAction)
	}
	if host.sameIdentity(current) {
		return nil
	}
	return fmt.Errorf("host Herdr runtime changed during provisioning; %s", hostHerdrChangedAction)
}

func (host HostHerdr) sameIdentity(other HostHerdr) bool {
	if !strings.EqualFold(host.commandPath, other.commandPath) ||
		host.commandSHA256 != other.commandSHA256 || host.commandSize != other.commandSize ||
		!strings.EqualFold(host.runtimeExecutable, other.runtimeExecutable) ||
		host.version != other.version || host.runtimeVersion != other.runtimeVersion ||
		host.protocol != other.protocol || len(host.files) != len(other.files) {
		return false
	}
	for index, file := range host.files {
		otherFile := other.files[index]
		if file.RelativePath != otherFile.RelativePath ||
			!strings.EqualFold(file.SourcePath, otherFile.SourcePath) ||
			file.SHA256 != otherFile.SHA256 || file.Size != otherFile.Size {
			return false
		}
	}
	return true
}

func (host HostHerdr) provisionRemote(ctx context.Context, connection Connection) (remoteProvisionResult, error) {
	if err := host.validate(); err != nil {
		return remoteProvisionResult{}, err
	}
	if connection.SSHTarget != sshTargetName {
		return remoteProvisionResult{}, fmt.Errorf("remote provision target = %q, want %q", connection.SSHTarget, sshTargetName)
	}
	provisionContext, cancel := context.WithTimeout(ctx, hostHerdrProvisionTimeout)
	defer cancel()
	command := hiddenCommandContext(provisionContext, host.commandPath,
		"--remote", connection.SSHTarget, "--provision", "--yes", "--json")
	command.Env = attachEnvironment(childProcessEnvironment(os.Environ()))
	stdout := boundedCommandOutput{maximum: maximumRemoteProvisionOutput}
	stderr := boundedCommandOutput{maximum: maximumRemoteProvisionOutput}
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	defer stdout.clear()
	defer stderr.clear()
	diagnostic := stderr.text()
	if strings.TrimSpace(diagnostic) == "" {
		diagnostic = stdout.text()
	}
	if err != nil {
		if provisionContext.Err() != nil {
			return remoteProvisionResult{}, fmt.Errorf("provision guest Herdr through %s: %w: %s", connection.SSHTarget, provisionContext.Err(), diagnostic)
		}
		return remoteProvisionResult{}, fmt.Errorf("provision guest Herdr through %s: %w: %s", connection.SSHTarget, err, diagnostic)
	}
	if stdout.overflow || stderr.overflow || stdout.buffer.Len()+stderr.buffer.Len() > maximumRemoteProvisionOutput {
		return remoteProvisionResult{}, fmt.Errorf("provision guest Herdr through %s exceeded the %d-byte output limit", connection.SSHTarget, maximumRemoteProvisionOutput)
	}
	result, err := decodeRemoteProvisionResult(stdout.buffer.Bytes())
	if err != nil {
		return remoteProvisionResult{}, err
	}
	if err := result.validate(host, connection.SSHTarget); err != nil {
		return remoteProvisionResult{}, fmt.Errorf("validate guest Herdr provision result: %w", err)
	}
	return result, nil
}

func decodeRemoteProvisionResult(data []byte) (remoteProvisionResult, error) {
	trimmed := bytes.TrimSpace(data)
	if err := validateExactJSONObjectShape(trimmed, "remote provision result", []string{
		"target", "platform", "binary", "binary_outcome", "server_outcome", "version", "protocol",
	}); err != nil {
		return remoteProvisionResult{}, err
	}
	var result remoteProvisionResult
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return remoteProvisionResult{}, fmt.Errorf("decode remote provision result: %w: %s", err, boundedText(trimmed))
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return remoteProvisionResult{}, errors.New("decode remote provision result: trailing JSON data")
	}
	return result, nil
}

func (result remoteProvisionResult) validate(host HostHerdr, target string) error {
	if result.Target != target {
		return fmt.Errorf("target = %q, want %q", result.Target, target)
	}
	if result.Platform != "windows-x86_64" && result.Platform != "windows-aarch64" {
		return fmt.Errorf("platform = %q, want a supported Windows target", result.Platform)
	}
	if err := validateGuestHerdrBinary(result.Binary); err != nil {
		return fmt.Errorf("binary = %q: %w", result.Binary, err)
	}
	if result.BinaryOutcome != remoteProvisionBinaryInstalled && result.BinaryOutcome != remoteProvisionBinaryMatching {
		return fmt.Errorf("binary_outcome = %q", result.BinaryOutcome)
	}
	if result.ServerOutcome != remoteProvisionServerStarted && result.ServerOutcome != remoteProvisionServerReloaded &&
		result.ServerOutcome != remoteProvisionServerRestarted {
		return fmt.Errorf("server_outcome = %q", result.ServerOutcome)
	}
	if result.BinaryOutcome == remoteProvisionBinaryInstalled && result.ServerOutcome == remoteProvisionServerReloaded {
		return errors.New("an installed binary cannot report an unchanged-server reload")
	}
	if result.Version != host.version || result.Protocol != host.protocol {
		return fmt.Errorf("version/protocol = %q/%d, want %q/%d", result.Version, result.Protocol, host.version, host.protocol)
	}
	return nil
}

func validateGuestHerdrBinary(path string) error {
	volume := filepath.VolumeName(path)
	localDrive := len(volume) == 2 && volume[1] == ':' &&
		((volume[0] >= 'A' && volume[0] <= 'Z') || (volume[0] >= 'a' && volume[0] <= 'z'))
	if !localDrive || !filepath.IsAbs(path) || filepath.Clean(path) != path ||
		!strings.EqualFold(filepath.Base(path), "herdr.exe") || strings.TrimSpace(path) != path ||
		strings.ContainsAny(path, "\x00\r\n") || len(path) > 32767 {
		return errors.New("want one canonical absolute local Windows herdr.exe path")
	}
	return nil
}

func validateHostHerdrRegularFile(info os.FileInfo, path, role string) error {
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return fmt.Errorf("inspect %s reparse state: %w", role, err)
	}
	if reparse || !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular non-reparse file: %s", role, path)
	}
	return nil
}

func hostHerdrCompatibilityError(format string, arguments ...any) error {
	reason := fmt.Sprintf(format, arguments...)
	return fmt.Errorf("%s; %s", reason, hostHerdrCompatibilityAction)
}
