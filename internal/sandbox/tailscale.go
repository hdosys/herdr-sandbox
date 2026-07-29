package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

const (
	tailscaleApplySchemaVersion             = 1
	tailscaleApplyModeEnroll                = "enroll"
	tailscaleApplyModeRestore               = "restore"
	tailscaleIdentityNotEstablishedExitCode = 42
	tailscaleIdentityTimeout                = 10 * time.Minute
	maximumTailscaleCaptureResultBytes      = maximumTailscaleIdentityBytes
)

var errTailscaleIdentityNotEstablished = errors.New("Tailscale enrollment did not establish a tagged running identity")

type tailscaleBootstrap struct {
	Enabled  bool
	Existing *tailscaleIdentity
	AuthKey  []byte
}

type tailscaleApplyRequest struct {
	SchemaVersion  int    `json:"schemaVersion"`
	Mode           string `json:"mode"`
	AuthKey        []byte `json:"authKey"`
	State          []byte `json:"state"`
	WindowsUserSID string `json:"windowsUserSID"`
}

func prepareTailscaleBootstrap(dataDirectory string, enabled bool, authKey []byte, authKeyFound bool) (tailscaleBootstrap, error) {
	bootstrap := tailscaleBootstrap{Enabled: enabled}
	if !enabled {
		return bootstrap, nil
	}
	identity, found, err := loadTailscaleIdentity(dataDirectory)
	if err != nil {
		return tailscaleBootstrap{}, err
	}
	if found {
		bootstrap.Existing = &identity
		return bootstrap, nil
	}
	if !authKeyFound || len(authKey) == 0 {
		return tailscaleBootstrap{}, errors.New("tailscale is enabled but no protected identity exists; set HERDR_SANDBOX_TAILSCALE_AUTH_KEY to one one-off preapproved tagged auth key for the first up")
	}
	bootstrap.AuthKey = append([]byte(nil), authKey...)
	return bootstrap, nil
}

func (bootstrap *tailscaleBootstrap) clear() {
	clear(bootstrap.AuthKey)
	bootstrap.AuthKey = nil
	if bootstrap.Existing != nil {
		clear(bootstrap.Existing.State)
		bootstrap.Existing.State = nil
	}
}

func configureFreshTailscale(ctx context.Context, connection Connection, dataDirectory string, bootstrap tailscaleBootstrap) error {
	if !bootstrap.Enabled {
		return nil
	}
	request := tailscaleApplyRequest{SchemaVersion: tailscaleApplySchemaVersion}
	if bootstrap.Existing == nil {
		request.Mode = tailscaleApplyModeEnroll
		request.AuthKey = append([]byte(nil), bootstrap.AuthKey...)
	} else {
		request.Mode = tailscaleApplyModeRestore
		request.State = append([]byte(nil), bootstrap.Existing.State...)
		request.WindowsUserSID = bootstrap.Existing.WindowsUserSID
	}
	defer clear(request.AuthKey)
	defer clear(request.State)
	identity, err := applyAndCaptureTailscale(ctx, connection, request)
	if err != nil {
		return err
	}
	defer clear(identity.State)
	if bootstrap.Existing != nil {
		if err := verifyStableTailscaleIdentity(*bootstrap.Existing, identity); err != nil {
			return err
		}
	}
	return storeTailscaleIdentity(dataDirectory, identity)
}

func captureAndStoreTailscale(ctx context.Context, connection Connection, dataDirectory string) error {
	expected, found, err := loadTailscaleIdentity(dataDirectory)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("protected Tailscale identity is missing")
	}
	defer clear(expected.State)
	return captureAndStoreTailscaleAgainst(ctx, connection, dataDirectory, &expected)
}

func recoverAndStoreTailscale(ctx context.Context, connection Connection, dataDirectory string) error {
	expected, found, err := loadTailscaleIdentity(dataDirectory)
	if err != nil {
		return err
	}
	if found {
		defer clear(expected.State)
		return captureAndStoreTailscaleAgainst(ctx, connection, dataDirectory, &expected)
	}
	return captureAndStoreTailscaleAgainst(ctx, connection, dataDirectory, nil)
}

func captureAndStoreTailscaleAgainst(ctx context.Context, connection Connection, dataDirectory string, expected *tailscaleIdentity) error {
	identity, err := captureTailscaleIdentity(ctx, connection)
	if err != nil {
		return err
	}
	defer clear(identity.State)
	if expected != nil {
		if err := verifyStableTailscaleIdentity(*expected, identity); err != nil {
			return err
		}
	}
	return storeTailscaleIdentity(dataDirectory, identity)
}

func applyAndCaptureTailscale(ctx context.Context, connection Connection, request tailscaleApplyRequest) (tailscaleIdentity, error) {
	if request.SchemaVersion != tailscaleApplySchemaVersion || (request.Mode != tailscaleApplyModeEnroll && request.Mode != tailscaleApplyModeRestore) {
		return tailscaleIdentity{}, errors.New("Tailscale apply request is invalid")
	}
	if request.Mode == tailscaleApplyModeEnroll {
		if len(request.AuthKey) == 0 || len(request.AuthKey) > maximumTailscaleAuthKeyBytes || len(request.State) != 0 || request.WindowsUserSID != "" {
			return tailscaleIdentity{}, errors.New("Tailscale enrollment request is invalid")
		}
	} else if len(request.State) == 0 || len(request.State) > maximumTailscaleStateBytes || len(request.AuthKey) != 0 || !windowsSIDPattern.MatchString(request.WindowsUserSID) {
		return tailscaleIdentity{}, errors.New("Tailscale restoration request is invalid")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return tailscaleIdentity{}, fmt.Errorf("encode Tailscale apply request: %w", err)
	}
	defer clear(payload)
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	launcher := buildTailscaleApplyLauncher(digest, len(payload))
	output, err := runSecretSSHPowerShell(ctx, connection, bytes.NewReader(payload), launcher, "apply stable Tailscale identity", maximumTailscaleCaptureResultBytes)
	if err != nil {
		if commandExitedWith(err, tailscaleIdentityNotEstablishedExitCode) {
			return tailscaleIdentity{}, errTailscaleIdentityNotEstablished
		}
		return tailscaleIdentity{}, err
	}
	defer clear(output)
	return decodeTailscaleIdentityResult(output)
}

func commandExitedWith(err error, exitCode int) bool {
	var exitError interface{ ExitCode() int }
	return errors.As(err, &exitError) && exitError.ExitCode() == exitCode
}

func captureTailscaleIdentity(ctx context.Context, connection Connection) (tailscaleIdentity, error) {
	output, err := runSecretSSHPowerShell(ctx, connection, nil, buildTailscaleCaptureLauncher(), "capture stable Tailscale identity", maximumTailscaleCaptureResultBytes)
	if err != nil {
		return tailscaleIdentity{}, err
	}
	defer clear(output)
	return decodeTailscaleIdentityResult(output)
}

func decodeTailscaleIdentityResult(data []byte) (tailscaleIdentity, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || len(trimmed) > maximumTailscaleCaptureResultBytes {
		return tailscaleIdentity{}, errors.New("Tailscale identity result size is invalid")
	}
	if err := validateExactJSONObjectShape(trimmed, "Tailscale identity result", []string{
		"schemaVersion", "windowsUserSID", "nodeID", "nodeKey", "ipv4", "dnsName", "hostName", "tailscaleVersion", "tags", "state",
	}); err != nil {
		return tailscaleIdentity{}, fmt.Errorf("decode Tailscale identity result: %w", err)
	}
	var identity tailscaleIdentity
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&identity); err != nil {
		clear(identity.State)
		return tailscaleIdentity{}, fmt.Errorf("decode Tailscale identity result: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		clear(identity.State)
		return tailscaleIdentity{}, errors.New("decode Tailscale identity result: trailing JSON data")
	}
	if err := identity.validate(); err != nil {
		clear(identity.State)
		return tailscaleIdentity{}, fmt.Errorf("validate Tailscale identity result: %w", err)
	}
	return identity, nil
}

func verifyStableTailscaleIdentity(expected, actual tailscaleIdentity) error {
	if expected.WindowsUserSID != actual.WindowsUserSID {
		return errors.New("restored Tailscale identity belongs to a different Windows Sandbox user SID")
	}
	if expected.NodeID != actual.NodeID {
		return errors.New("restored Tailscale control-plane device identity changed")
	}
	if expected.NodeKey != actual.NodeKey {
		return errors.New("restored Tailscale node key changed")
	}
	if expected.IPv4 != actual.IPv4 {
		return errors.New("restored Tailscale IPv4 address changed")
	}
	if expected.DNSName != actual.DNSName || expected.HostName != actual.HostName {
		return errors.New("restored Tailscale DNS or host name changed")
	}
	expectedTags := append([]string(nil), expected.Tags...)
	actualTags := append([]string(nil), actual.Tags...)
	sort.Strings(expectedTags)
	sort.Strings(actualTags)
	if strings.Join(expectedTags, "\x00") != strings.Join(actualTags, "\x00") {
		return errors.New("restored Tailscale tags changed")
	}
	return nil
}

func buildTailscaleApplyLauncher(expectedDigest string, payloadLength int) string {
	return fmt.Sprintf(`%s

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$expectedLength = [long]%d
$inputStream = [Console]::OpenStandardInput()
$payload = New-Object byte[] $expectedLength
$offset = 0
while ($offset -lt $payload.Length) {
    $read = $inputStream.Read($payload, $offset, $payload.Length - $offset)
    if ($read -le 0) { throw 'Tailscale identity input ended before its declared length.' }
    $offset += $read
}
$sha256 = [Security.Cryptography.SHA256]::Create()
try { $actualDigest = ([BitConverter]::ToString($sha256.ComputeHash($payload))).Replace('-', '').ToLowerInvariant() } finally { $sha256.Dispose() }
if ($actualDigest -cne '%s') { throw 'Tailscale identity input SHA-256 mismatch.' }
$request = [Text.Encoding]::UTF8.GetString($payload) | ConvertFrom-Json
$payload = $null
Assert-ExactProperties $request @('schemaVersion', 'mode', 'authKey', 'state', 'windowsUserSID')
if ([int]$request.schemaVersion -ne %d) { throw 'Tailscale identity input schema is unsupported.' }
$currentSID = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
$identity = $null
$identityNotEstablished = $false
if ([string]$request.mode -ceq 'restore') {
    if ([string]$request.windowsUserSID -cne $currentSID) { throw 'Tailscale identity Windows user SID changed.' }
    $stateBytes = [Convert]::FromBase64String([string]$request.state)
    if ($stateBytes.Length -le 0 -or $stateBytes.Length -gt %d) { throw 'Tailscale state size is invalid.' }
    Set-TailscalePortablePolicy
    Stop-TailscaleService
    try {
        Set-TailscaleState -StateBytes $stateBytes
    } finally {
        $stateBytes = $null
        Start-TailscaleService
    }
} elseif ([string]$request.mode -ceq 'enroll') {
    $authPath = Join-Path 'C:\HerdrSandbox\staging' ('tailscale-auth-' + [Guid]::NewGuid().ToString('N'))
    $enrollmentAttempted = $false
    try {
        $authBytes = [Convert]::FromBase64String([string]$request.authKey)
        if ($authBytes.Length -le 0 -or $authBytes.Length -gt %d) { throw 'Tailscale auth key size is invalid.' }
        Set-TailscalePortablePolicy
        Stop-TailscaleService
        Start-TailscaleService
        New-Item -ItemType Directory -Path (Split-Path -Parent $authPath) -Force | Out-Null
        [IO.File]::WriteAllBytes($authPath, $authBytes)
        $authBytes = $null
        $tailscale = Get-TailscaleExecutable
        $enrollmentAttempted = $true
        & $tailscale up ('--auth-key=file:' + $authPath) '--hostname=%s' '--unattended=true' '--timeout=2m' *> $null
        if ($LASTEXITCODE -ne 0) {
            try { $identity = Wait-TailscaleIdentity -ExpectedSID $currentSID } catch { $identityNotEstablished = $true }
        }
    } catch {
        if (-not $enrollmentAttempted) { $identityNotEstablished = $true } else { throw }
    } finally {
        $authBytes = $null
        if (Test-Path -LiteralPath $authPath) {
            Remove-Item -LiteralPath $authPath -Force -ErrorAction Stop
        }
        if (Test-Path -LiteralPath $authPath) {
            throw 'Tailscale auth-key staging cleanup did not remove the credential file.'
        }
    }
} else {
    throw 'Tailscale identity input mode is invalid.'
}
if ($identityNotEstablished) { exit %d }
if ($null -eq $identity) { $identity = Wait-TailscaleIdentity -ExpectedSID $currentSID }
$result = Capture-TailscaleState -Identity $identity
[Console]::Out.Write(($result | ConvertTo-Json -Compress))
exit 0`, tailscalePowerShellFunctions(), payloadLength, expectedDigest, tailscaleApplySchemaVersion, maximumTailscaleStateBytes, maximumTailscaleAuthKeyBytes, tailscaleHostName, tailscaleIdentityNotEstablishedExitCode)
}

func buildTailscaleCaptureLauncher() string {
	return fmt.Sprintf(`%s

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$currentSID = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
Set-TailscalePortablePolicy
$identity = Wait-TailscaleIdentity -ExpectedSID $currentSID
$result = Capture-TailscaleState -Identity $identity
[Console]::Out.Write(($result | ConvertTo-Json -Compress))
exit 0`, tailscalePowerShellFunctions())
}

func tailscalePowerShellFunctions() string {
	return fmt.Sprintf(`function Assert-ExactProperties {
    param([object]$Value, [string[]]$Expected)
    $actual = @($Value.PSObject.Properties.Name)
    if ($actual.Count -ne $Expected.Count) { throw 'Tailscale JSON property count is invalid.' }
    foreach ($name in $Expected) { if (-not ($actual -ccontains $name)) { throw 'Tailscale JSON properties are invalid.' } }
}
function Get-TailscaleExecutable {
    $command = Get-Command 'tailscale.exe' -CommandType Application -ErrorAction Stop
    return $command.Source
}
function Get-TailscaleService {
    return Get-Service -Name 'Tailscale' -ErrorAction Stop
}
function Stop-TailscaleService {
    $service = Get-TailscaleService
    if ($service.Status -ne [ServiceProcess.ServiceControllerStatus]::Stopped) {
        Stop-Service -Name 'Tailscale' -ErrorAction Stop
        $service.WaitForStatus([ServiceProcess.ServiceControllerStatus]::Stopped, [TimeSpan]::FromSeconds(30))
    }
}
function Start-TailscaleService {
    $service = Get-TailscaleService
    if ($service.Status -ne [ServiceProcess.ServiceControllerStatus]::Running) {
        Start-Service -Name 'Tailscale' -ErrorAction Stop
        $service.WaitForStatus([ServiceProcess.ServiceControllerStatus]::Running, [TimeSpan]::FromSeconds(30))
    }
}
function Set-TailscalePortablePolicy {
    $policy = 'HKLM:\SOFTWARE\Policies\Tailscale'
    New-Item -Path $policy -Force | Out-Null
    New-ItemProperty -Path $policy -Name 'EncryptState' -PropertyType DWord -Value 0 -Force | Out-Null
    New-ItemProperty -Path $policy -Name 'HardwareAttestation' -PropertyType DWord -Value 0 -Force | Out-Null
}
function Assert-RegularNonReparseFile {
    param([string]$Path, [long]$Maximum)
    $item = Get-Item -LiteralPath $Path -Force -ErrorAction Stop
    if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or $item.Length -le 0 -or $item.Length -gt $Maximum) {
        throw 'Tailscale state path is unsafe or too large.'
    }
}
function Assert-TailscalePlaintextState {
    param([byte[]]$StateBytes)
    if ($StateBytes.Length -le 0 -or $StateBytes.Length -gt %d) { throw 'Tailscale state size is invalid.' }
    $stateObject = [Text.Encoding]::UTF8.GetString($StateBytes) | ConvertFrom-Json
    if ($null -eq $stateObject.PSObject.Properties['_machinekey'] -or [string]::IsNullOrWhiteSpace([string]$stateObject._machinekey)) {
        throw 'Tailscale state is not portable plaintext state.'
    }
}
function Set-TailscaleState {
    param([byte[]]$StateBytes)
    Assert-TailscalePlaintextState -StateBytes $StateBytes
    $directory = Join-Path $env:ProgramData 'Tailscale'
    $directoryItem = Get-Item -LiteralPath $directory -Force -ErrorAction Stop
    if (-not $directoryItem.PSIsContainer -or ($directoryItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Tailscale state directory is unsafe.' }
    $target = Join-Path $directory 'server-state.conf'
    if (Test-Path -LiteralPath $target) { Assert-RegularNonReparseFile -Path $target -Maximum %d }
    $temporary = Join-Path $directory ('server-state.herdr-sandbox-' + [Guid]::NewGuid().ToString('N') + '.tmp')
    try {
        [IO.File]::WriteAllBytes($temporary, $StateBytes)
        Assert-RegularNonReparseFile -Path $temporary -Maximum %d
        if (Test-Path -LiteralPath $target) { [IO.File]::Replace($temporary, $target, $null) } else { [IO.File]::Move($temporary, $target) }
        Assert-RegularNonReparseFile -Path $target -Maximum %d
    } finally {
        if (Test-Path -LiteralPath $temporary) {
            Remove-Item -LiteralPath $temporary -Force -ErrorAction Stop
        }
        if (Test-Path -LiteralPath $temporary) {
            throw 'Tailscale plaintext state staging cleanup did not remove the credential file.'
        }
    }
}
function Read-TailscaleIdentity {
    param([string]$ExpectedSID)
    $tailscale = Get-TailscaleExecutable
    $raw = & $tailscale status --json --peers=false 2>$null | Out-String
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($raw)) { return $null }
    $status = $raw | ConvertFrom-Json
    if ([string]$status.BackendState -cne 'Running' -or $null -eq $status.Self) { return $null }
    $ipv4 = @($status.TailscaleIPs | Where-Object {
        $parsed = $null
        [Net.IPAddress]::TryParse([string]$_, [ref]$parsed) -and $parsed.AddressFamily -eq [Net.Sockets.AddressFamily]::InterNetwork
    })
    $tags = @($status.Self.Tags)
    if ($ipv4.Count -ne 1 -or $tags.Count -eq 0 -or [string]$status.Self.HostName -cne '%s' -or
        [string]::IsNullOrWhiteSpace([string]$status.Self.ID) -or [string]::IsNullOrWhiteSpace([string]$status.Self.PublicKey) -or
        [string]::IsNullOrWhiteSpace([string]$status.Self.DNSName) -or [string]::IsNullOrWhiteSpace([string]$status.Version)) { return $null }
    return [ordered]@{
        schemaVersion = %d
        windowsUserSID = $ExpectedSID
        nodeID = [string]$status.Self.ID
        nodeKey = [string]$status.Self.PublicKey
        ipv4 = [string]$ipv4[0]
        dnsName = [string]$status.Self.DNSName
        hostName = [string]$status.Self.HostName
        tailscaleVersion = [string]$status.Version
        tags = @($tags | ForEach-Object { [string]$_ })
    }
}
function Wait-TailscaleIdentity {
    param([string]$ExpectedSID)
    $deadline = [DateTime]::UtcNow.AddMinutes(2)
    do {
        $identity = Read-TailscaleIdentity -ExpectedSID $ExpectedSID
        if ($null -ne $identity) { return $identity }
        Start-Sleep -Milliseconds 500
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'Tailscale did not reach the required tagged running identity.'
}
function Capture-TailscaleState {
    param([Collections.IDictionary]$Identity)
    Stop-TailscaleService
    try {
        $path = Join-Path $env:ProgramData 'Tailscale\server-state.conf'
        Assert-RegularNonReparseFile -Path $path -Maximum %d
        $stateBytes = [IO.File]::ReadAllBytes($path)
        Assert-TailscalePlaintextState -StateBytes $stateBytes
    } finally {
        Start-TailscaleService
    }
    $verified = Wait-TailscaleIdentity -ExpectedSID ([string]$Identity.windowsUserSID)
    foreach ($name in @('nodeID', 'nodeKey', 'ipv4', 'dnsName', 'hostName')) {
        if ([string]$verified[$name] -cne [string]$Identity[$name]) { throw 'Tailscale identity changed while capturing state.' }
    }
    $verified['state'] = [Convert]::ToBase64String($stateBytes)
    $stateBytes = $null
    return $verified
}`, maximumTailscaleStateBytes, maximumTailscaleStateBytes, maximumTailscaleStateBytes, maximumTailscaleStateBytes, tailscaleHostName, tailscaleIdentitySchemaVersion, maximumTailscaleStateBytes)
}
