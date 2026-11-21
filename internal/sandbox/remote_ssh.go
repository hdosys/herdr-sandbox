package sandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	remoteSSHContractSchemaVersion = 1
	remoteSSHPreparationTimeout    = 2 * time.Minute
	remoteSSHVerificationTimeout   = time.Minute
	maximumRemoteSSHResultBytes    = 64 * 1024
	guestRemoteSSHRoot             = guestRootDirectory + `\remote-ssh`
	guestRemoteSSHScriptPath       = guestRemoteSSHRoot + `\remote-ssh.ps1`
)

//go:embed assets/remote-ssh.ps1
var remoteSSHScript []byte

type remoteSSHPrepareRequest struct {
	SchemaVersion  int      `json:"schemaVersion"`
	TailscaleIPv4  string   `json:"tailscaleIPv4"`
	AuthorizedKeys []string `json:"authorizedKeys"`
	PrivateKey     []byte   `json:"privateKey"`
	PublicKey      string   `json:"publicKey"`
	ScriptSHA256   string   `json:"scriptSHA256"`
}

type remoteSSHPrepareResult struct {
	SchemaVersion int    `json:"schemaVersion"`
	PrivateKey    []byte `json:"privateKey"`
	PublicKey     string `json:"publicKey"`
}

type remoteSSHRuntimeResult struct {
	SchemaVersion int    `json:"schemaVersion"`
	State         string `json:"state"`
	PID           int    `json:"pid"`
}

func activateRemoteSSH(ctx context.Context, connection Connection) error {
	operationContext, cancel := context.WithTimeout(ctx, remoteSSHVerificationTimeout)
	defer cancel()
	output, err := runSSHPowerShell(operationContext, connection, nil,
		"$ErrorActionPreference = 'Stop'\n& '"+guestRemoteSSHScriptPath+"' -Mode Activate",
		"activate remote SSH endpoint", maximumRemoteSSHResultBytes)
	if err != nil {
		return err
	}
	_, err = decodeRemoteSSHRuntimeResult(output)
	return err
}

func prepareRemoteSSH(ctx context.Context, connection Connection, dataDirectory string, authorizedKeys []string) (RemoteAccess, error) {
	keys, err := canonicalizeRemoteSSHAuthorizedKeys(authorizedKeys)
	if err != nil {
		return RemoteAccess{}, err
	}
	if len(keys) == 0 {
		return RemoteAccess{}, errors.New("remote SSH preparation requires at least one authorized key")
	}
	tailscale, found, err := loadTailscaleIdentity(dataDirectory)
	if err != nil {
		return RemoteAccess{}, err
	}
	if !found {
		return RemoteAccess{}, errors.New("protected Tailscale identity is missing before remote SSH preparation")
	}
	defer clear(tailscale.State)
	existing, identityFound, err := loadRemoteSSHIdentity(dataDirectory)
	if err != nil {
		return RemoteAccess{}, err
	}
	if identityFound {
		defer existing.clear()
	}
	scriptDigest := fmt.Sprintf("%x", sha256.Sum256(remoteSSHScript))
	request := remoteSSHPrepareRequest{
		SchemaVersion:  remoteSSHContractSchemaVersion,
		TailscaleIPv4:  tailscale.IPv4,
		AuthorizedKeys: keys,
		PrivateKey:     []byte{},
		ScriptSHA256:   scriptDigest,
	}
	if identityFound {
		request.PrivateKey = append([]byte(nil), existing.PrivateKey...)
		request.PublicKey = existing.PublicKey
	}
	defer clear(request.PrivateKey)
	archive, err := buildRemoteSSHPrepareArchive(request)
	if err != nil {
		return RemoteAccess{}, err
	}
	defer clear(archive)
	digest := fmt.Sprintf("%x", sha256.Sum256(archive))
	launcher := buildRemoteSSHPrepareLauncher(digest, len(archive))
	operationContext, cancel := context.WithTimeout(ctx, remoteSSHPreparationTimeout)
	defer cancel()
	output, err := runSecretSSHArchivePowerShell(operationContext, connection, archive, launcher, "prepare remote SSH endpoint")
	if err != nil {
		return RemoteAccess{}, err
	}
	defer clear(output)
	result, err := decodeRemoteSSHPrepareResult(output)
	if err != nil {
		return RemoteAccess{}, err
	}
	defer result.clear()
	if identityFound {
		if result.PublicKey != existing.PublicKey || !bytes.Equal(result.PrivateKey, existing.PrivateKey) {
			return RemoteAccess{}, errors.New("restored remote SSH host identity changed inside the Sandbox")
		}
	} else if err := storeRemoteSSHIdentity(dataDirectory, remoteSSHIdentity{
		SchemaVersion: remoteSSHIdentitySchemaVersion,
		PrivateKey:    result.PrivateKey,
		PublicKey:     result.PublicKey,
	}); err != nil {
		return RemoteAccess{}, err
	}
	return buildRemoteAccess(tailscale, result.PublicKey, len(keys))
}

func verifyRemoteSSH(ctx context.Context, connection Connection, dataDirectory string, authorizedKeys []string) (RemoteAccess, error) {
	keys, err := canonicalizeRemoteSSHAuthorizedKeys(authorizedKeys)
	if err != nil {
		return RemoteAccess{}, err
	}
	if len(keys) == 0 {
		return RemoteAccess{}, errors.New("remote SSH verification requires at least one authorized key")
	}
	scriptDigest := fmt.Sprintf("%x", sha256.Sum256(remoteSSHScript))
	launcher := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$scriptPath = '%s'
if (-not (Test-Path -LiteralPath $scriptPath -PathType Leaf)) { throw 'Remote SSH control script is missing.' }
& $scriptPath -Mode Verify -ExpectedScriptSHA256 '%s'
exit 0`, guestRemoteSSHScriptPath, scriptDigest)
	operationContext, cancel := context.WithTimeout(ctx, remoteSSHVerificationTimeout)
	defer cancel()
	output, err := runSSHPowerShell(operationContext, connection, nil, launcher, "verify remote SSH endpoint", maximumRemoteSSHResultBytes)
	if err != nil {
		return RemoteAccess{}, err
	}
	if _, err := decodeRemoteSSHRuntimeResult(output); err != nil {
		return RemoteAccess{}, err
	}
	return loadRemoteAccess(dataDirectory, len(keys))
}

func loadRemoteAccess(dataDirectory string, authorizedKeyCount int) (RemoteAccess, error) {
	tailscale, found, err := loadTailscaleIdentity(dataDirectory)
	if err != nil {
		return RemoteAccess{}, err
	}
	if !found {
		return RemoteAccess{}, errors.New("protected Tailscale identity is missing")
	}
	defer clear(tailscale.State)
	remote, found, err := loadRemoteSSHIdentity(dataDirectory)
	if err != nil {
		return RemoteAccess{}, err
	}
	if !found {
		return RemoteAccess{}, errors.New("protected remote SSH host identity is missing")
	}
	defer remote.clear()
	return buildRemoteAccess(tailscale, remote.PublicKey, authorizedKeyCount)
}

func buildRemoteSSHPrepareArchive(request remoteSSHPrepareRequest) ([]byte, error) {
	if len(remoteSSHScript) == 0 || len(remoteSSHScript) > 64*1024 {
		return nil, errors.New("embedded remote SSH control script size is invalid")
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode remote SSH prepare request: %w", err)
	}
	defer clear(requestJSON)
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	add := func(name string, data []byte) error {
		writer, err := archive.Create(name)
		if err != nil {
			return err
		}
		_, err = writer.Write(data)
		return err
	}
	if err := add("remote-ssh.ps1", remoteSSHScript); err != nil {
		_ = archive.Close()
		return nil, fmt.Errorf("archive remote SSH control script: %w", err)
	}
	if err := add("request.json", requestJSON); err != nil {
		_ = archive.Close()
		return nil, fmt.Errorf("archive remote SSH prepare request: %w", err)
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("close remote SSH prepare archive: %w", err)
	}
	if buffer.Len() == 0 || buffer.Len() > 128*1024 {
		return nil, errors.New("remote SSH prepare archive size is invalid")
	}
	return buffer.Bytes(), nil
}

func buildRemoteSSHPrepareLauncher(expectedDigest string, expectedArchiveLength int) string {
	staging := guestArchiveStagingPowerShell("remote-ssh-"+expectedDigest[:16], "Remote SSH preparation", true)
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
%s
$expectedArchiveLength = [long]%d
try {
    $inputStream = [Console]::OpenStandardInput()
    $outputStream = [IO.File]::Open($archive, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try {
        $remaining = $expectedArchiveLength
        $buffer = New-Object byte[] 8192
        while ($remaining -gt 0) {
            $requested = [int][Math]::Min([long]$buffer.Length, $remaining)
            $read = $inputStream.Read($buffer, 0, $requested)
            if ($read -le 0) { throw "Remote SSH archive ended with $remaining bytes missing." }
            $outputStream.Write($buffer, 0, $read)
            $remaining -= $read
        }
        $outputStream.Flush($true)
    } finally {
        $outputStream.Dispose()
    }
    $digest = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($digest -cne '%s') { throw 'Remote SSH archive SHA-256 mismatch.' }
    Expand-Archive -LiteralPath $archive -DestinationPath $expanded
    Assert-GuestArchiveTree
    $files = @(Get-ChildItem -LiteralPath $expanded -Force)
    if ($files.Count -ne 2 -or @($files | Where-Object { $_.PSIsContainer }).Count -ne 0) {
        throw 'Remote SSH archive must contain exactly two regular files.'
    }
    $controlScript = Join-Path $expanded 'remote-ssh.ps1'
    $requestPath = Join-Path $expanded 'request.json'
    if (-not (Test-Path -LiteralPath $controlScript -PathType Leaf) -or
        -not (Test-Path -LiteralPath $requestPath -PathType Leaf)) {
        throw 'Remote SSH archive contents are incomplete.'
    }
    & $controlScript -Mode Prepare -RequestPath $requestPath
} finally {
    Remove-GuestArchiveStaging
}
exit 0`, staging, expectedArchiveLength, expectedDigest)
}

func decodeRemoteSSHPrepareResult(data []byte) (remoteSSHPrepareResult, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || len(trimmed) > maximumRemoteSSHResultBytes {
		return remoteSSHPrepareResult{}, errors.New("remote SSH prepare result size is invalid")
	}
	if err := validateExactJSONObjectShape(trimmed, "remote SSH prepare result", []string{"schemaVersion", "privateKey", "publicKey"}); err != nil {
		return remoteSSHPrepareResult{}, err
	}
	var result remoteSSHPrepareResult
	if err := decodeStrictJSON(trimmed, &result); err != nil {
		clear(result.PrivateKey)
		return remoteSSHPrepareResult{}, fmt.Errorf("decode remote SSH prepare result: %w", err)
	}
	identity := remoteSSHIdentity{SchemaVersion: remoteSSHIdentitySchemaVersion, PrivateKey: result.PrivateKey, PublicKey: result.PublicKey}
	if err := identity.validate(); err != nil {
		clear(result.PrivateKey)
		return remoteSSHPrepareResult{}, fmt.Errorf("validate remote SSH prepare result: %w", err)
	}
	if result.SchemaVersion != remoteSSHContractSchemaVersion {
		clear(result.PrivateKey)
		return remoteSSHPrepareResult{}, errors.New("remote SSH prepare result schema is unsupported")
	}
	return result, nil
}

func decodeRemoteSSHRuntimeResult(data []byte) (remoteSSHRuntimeResult, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || len(trimmed) > maximumRemoteSSHResultBytes {
		return remoteSSHRuntimeResult{}, errors.New("remote SSH runtime result size is invalid")
	}
	if err := validateExactJSONObjectShape(trimmed, "remote SSH runtime result", []string{"schemaVersion", "state", "pid"}); err != nil {
		return remoteSSHRuntimeResult{}, err
	}
	var result remoteSSHRuntimeResult
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return remoteSSHRuntimeResult{}, fmt.Errorf("decode remote SSH runtime result: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return remoteSSHRuntimeResult{}, errors.New("remote SSH runtime result contains trailing JSON data")
	}
	if result.SchemaVersion != remoteSSHContractSchemaVersion || result.State != "running" || result.PID < 1 {
		return remoteSSHRuntimeResult{}, errors.New("remote SSH runtime result values are invalid")
	}
	return result, nil
}

func (result *remoteSSHPrepareResult) clear() {
	if result == nil {
		return
	}
	clear(result.PrivateKey)
	result.PrivateKey = nil
}
