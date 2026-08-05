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
	mobileSSHContractSchemaVersion = 1
	mobileSSHPreparationTimeout    = 2 * time.Minute
	mobileSSHVerificationTimeout   = time.Minute
	maximumMobileSSHResultBytes    = 64 * 1024
	guestMobileSSHRoot             = guestRootDirectory + `\mobile-ssh`
	guestMobileSSHScriptPath       = guestMobileSSHRoot + `\mobile-ssh.ps1`
)

//go:embed assets/mobile-ssh.ps1
var mobileSSHScript []byte

type mobileSSHPrepareRequest struct {
	SchemaVersion  int      `json:"schemaVersion"`
	TailscaleIPv4  string   `json:"tailscaleIPv4"`
	AuthorizedKeys []string `json:"authorizedKeys"`
	PrivateKey     []byte   `json:"privateKey"`
	PublicKey      string   `json:"publicKey"`
	ScriptSHA256   string   `json:"scriptSHA256"`
}

type mobileSSHPrepareResult struct {
	SchemaVersion int    `json:"schemaVersion"`
	PrivateKey    []byte `json:"privateKey"`
	PublicKey     string `json:"publicKey"`
}

type mobileSSHRuntimeResult struct {
	SchemaVersion int    `json:"schemaVersion"`
	State         string `json:"state"`
	PID           int    `json:"pid"`
}

func prepareMobileSSH(ctx context.Context, connection Connection, dataDirectory string, authorizedKeys []string) (MobileAccess, error) {
	keys, err := canonicalizeMobileSSHAuthorizedKeys(authorizedKeys)
	if err != nil {
		return MobileAccess{}, err
	}
	if len(keys) == 0 {
		return MobileAccess{}, errors.New("mobile SSH preparation requires at least one authorized key")
	}
	tailscale, found, err := loadTailscaleIdentity(dataDirectory)
	if err != nil {
		return MobileAccess{}, err
	}
	if !found {
		return MobileAccess{}, errors.New("protected Tailscale identity is missing before mobile SSH preparation")
	}
	defer clear(tailscale.State)
	existing, identityFound, err := loadMobileSSHIdentity(dataDirectory)
	if err != nil {
		return MobileAccess{}, err
	}
	if identityFound {
		defer existing.clear()
	}
	scriptDigest := fmt.Sprintf("%x", sha256.Sum256(mobileSSHScript))
	request := mobileSSHPrepareRequest{
		SchemaVersion:  mobileSSHContractSchemaVersion,
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
	archive, err := buildMobileSSHPrepareArchive(request)
	if err != nil {
		return MobileAccess{}, err
	}
	defer clear(archive)
	digest := fmt.Sprintf("%x", sha256.Sum256(archive))
	launcher := buildMobileSSHPrepareLauncher(digest, len(archive))
	operationContext, cancel := context.WithTimeout(ctx, mobileSSHPreparationTimeout)
	defer cancel()
	output, err := runSecretSSHArchivePowerShell(operationContext, connection, archive, launcher, "prepare mobile SSH endpoint")
	if err != nil {
		return MobileAccess{}, err
	}
	defer clear(output)
	result, err := decodeMobileSSHPrepareResult(output)
	if err != nil {
		return MobileAccess{}, err
	}
	defer result.clear()
	if identityFound {
		if result.PublicKey != existing.PublicKey || !bytes.Equal(result.PrivateKey, existing.PrivateKey) {
			return MobileAccess{}, errors.New("restored mobile SSH host identity changed inside the Sandbox")
		}
	} else if err := storeMobileSSHIdentity(dataDirectory, mobileSSHIdentity{
		SchemaVersion: mobileSSHIdentitySchemaVersion,
		PrivateKey:    result.PrivateKey,
		PublicKey:     result.PublicKey,
	}); err != nil {
		return MobileAccess{}, err
	}
	return buildMobileAccess(tailscale, result.PublicKey, len(keys))
}

func verifyMobileSSH(ctx context.Context, connection Connection, dataDirectory string, authorizedKeys []string) (MobileAccess, error) {
	keys, err := canonicalizeMobileSSHAuthorizedKeys(authorizedKeys)
	if err != nil {
		return MobileAccess{}, err
	}
	if len(keys) == 0 {
		return MobileAccess{}, errors.New("mobile SSH verification requires at least one authorized key")
	}
	scriptDigest := fmt.Sprintf("%x", sha256.Sum256(mobileSSHScript))
	launcher := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$scriptPath = '%s'
if (-not (Test-Path -LiteralPath $scriptPath -PathType Leaf)) { throw 'Mobile SSH control script is missing.' }
& $scriptPath -Mode Verify -ExpectedScriptSHA256 '%s'
exit 0`, guestMobileSSHScriptPath, scriptDigest)
	operationContext, cancel := context.WithTimeout(ctx, mobileSSHVerificationTimeout)
	defer cancel()
	output, err := runSSHPowerShell(operationContext, connection, nil, launcher, "verify mobile SSH endpoint", maximumMobileSSHResultBytes)
	if err != nil {
		return MobileAccess{}, err
	}
	if _, err := decodeMobileSSHRuntimeResult(output); err != nil {
		return MobileAccess{}, err
	}
	return loadMobileAccess(dataDirectory, len(keys))
}

func loadMobileAccess(dataDirectory string, authorizedKeyCount int) (MobileAccess, error) {
	tailscale, found, err := loadTailscaleIdentity(dataDirectory)
	if err != nil {
		return MobileAccess{}, err
	}
	if !found {
		return MobileAccess{}, errors.New("protected Tailscale identity is missing")
	}
	defer clear(tailscale.State)
	mobile, found, err := loadMobileSSHIdentity(dataDirectory)
	if err != nil {
		return MobileAccess{}, err
	}
	if !found {
		return MobileAccess{}, errors.New("protected mobile SSH host identity is missing")
	}
	defer mobile.clear()
	return buildMobileAccess(tailscale, mobile.PublicKey, authorizedKeyCount)
}

func buildMobileSSHPrepareArchive(request mobileSSHPrepareRequest) ([]byte, error) {
	if len(mobileSSHScript) == 0 || len(mobileSSHScript) > 64*1024 {
		return nil, errors.New("embedded mobile SSH control script size is invalid")
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode mobile SSH prepare request: %w", err)
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
	if err := add("mobile-ssh.ps1", mobileSSHScript); err != nil {
		_ = archive.Close()
		return nil, fmt.Errorf("archive mobile SSH control script: %w", err)
	}
	if err := add("request.json", requestJSON); err != nil {
		_ = archive.Close()
		return nil, fmt.Errorf("archive mobile SSH prepare request: %w", err)
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("close mobile SSH prepare archive: %w", err)
	}
	if buffer.Len() == 0 || buffer.Len() > 128*1024 {
		return nil, errors.New("mobile SSH prepare archive size is invalid")
	}
	return buffer.Bytes(), nil
}

func buildMobileSSHPrepareLauncher(expectedDigest string, expectedArchiveLength int) string {
	staging := guestArchiveStagingPowerShell("mobile-ssh-"+expectedDigest[:16], "Mobile SSH preparation")
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
            if ($read -le 0) { throw "Mobile SSH archive ended with $remaining bytes missing." }
            $outputStream.Write($buffer, 0, $read)
            $remaining -= $read
        }
        $outputStream.Flush($true)
    } finally {
        $outputStream.Dispose()
    }
    $digest = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($digest -cne '%s') { throw 'Mobile SSH archive SHA-256 mismatch.' }
    Expand-Archive -LiteralPath $archive -DestinationPath $expanded
    Assert-GuestArchiveTree
    $files = @(Get-ChildItem -LiteralPath $expanded -Force)
    if ($files.Count -ne 2 -or @($files | Where-Object { $_.PSIsContainer }).Count -ne 0) {
        throw 'Mobile SSH archive must contain exactly two regular files.'
    }
    $controlScript = Join-Path $expanded 'mobile-ssh.ps1'
    $requestPath = Join-Path $expanded 'request.json'
    if (-not (Test-Path -LiteralPath $controlScript -PathType Leaf) -or
        -not (Test-Path -LiteralPath $requestPath -PathType Leaf)) {
        throw 'Mobile SSH archive contents are incomplete.'
    }
    & $controlScript -Mode Prepare -RequestPath $requestPath
} finally {
    Remove-GuestArchiveStaging
}
exit 0`, staging, expectedArchiveLength, expectedDigest)
}

func decodeMobileSSHPrepareResult(data []byte) (mobileSSHPrepareResult, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || len(trimmed) > maximumMobileSSHResultBytes {
		return mobileSSHPrepareResult{}, errors.New("mobile SSH prepare result size is invalid")
	}
	if err := validateExactJSONObjectShape(trimmed, "mobile SSH prepare result", []string{"schemaVersion", "privateKey", "publicKey"}); err != nil {
		return mobileSSHPrepareResult{}, err
	}
	var result mobileSSHPrepareResult
	if err := decodeStrictJSON(trimmed, &result); err != nil {
		clear(result.PrivateKey)
		return mobileSSHPrepareResult{}, fmt.Errorf("decode mobile SSH prepare result: %w", err)
	}
	identity := mobileSSHIdentity{SchemaVersion: mobileSSHIdentitySchemaVersion, PrivateKey: result.PrivateKey, PublicKey: result.PublicKey}
	if err := identity.validate(); err != nil {
		clear(result.PrivateKey)
		return mobileSSHPrepareResult{}, fmt.Errorf("validate mobile SSH prepare result: %w", err)
	}
	if result.SchemaVersion != mobileSSHContractSchemaVersion {
		clear(result.PrivateKey)
		return mobileSSHPrepareResult{}, errors.New("mobile SSH prepare result schema is unsupported")
	}
	return result, nil
}

func decodeMobileSSHRuntimeResult(data []byte) (mobileSSHRuntimeResult, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || len(trimmed) > maximumMobileSSHResultBytes {
		return mobileSSHRuntimeResult{}, errors.New("mobile SSH runtime result size is invalid")
	}
	if err := validateExactJSONObjectShape(trimmed, "mobile SSH runtime result", []string{"schemaVersion", "state", "pid"}); err != nil {
		return mobileSSHRuntimeResult{}, err
	}
	var result mobileSSHRuntimeResult
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return mobileSSHRuntimeResult{}, fmt.Errorf("decode mobile SSH runtime result: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return mobileSSHRuntimeResult{}, errors.New("mobile SSH runtime result contains trailing JSON data")
	}
	if result.SchemaVersion != mobileSSHContractSchemaVersion || result.State != "running" || result.PID < 1 {
		return mobileSSHRuntimeResult{}, errors.New("mobile SSH runtime result values are invalid")
	}
	return result, nil
}

func (result *mobileSSHPrepareResult) clear() {
	if result == nil {
		return
	}
	clear(result.PrivateKey)
	result.PrivateKey = nil
}
