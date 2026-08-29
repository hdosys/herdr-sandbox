package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
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
	tailscaleDownCaptureTimeout             = 75 * time.Second
	tailscaleDownRollbackTimeout            = 45 * time.Second
	maximumTailscaleCaptureResultBytes      = maximumTailscaleIdentityBytes
)

var errTailscaleIdentityNotEstablished = errors.New("enrollment through Tailscale did not establish a tagged running identity")

//go:embed assets/tailscale-lifecycle.ps1
var tailscaleLifecyclePowerShell string

//go:embed assets/tailscale-apply.ps1
var tailscaleApplyPowerShell string

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

func recoverAndStoreTailscaleForDown(ctx context.Context, connection Connection, dataDirectory string) (bool, error) {
	expected, found, err := loadTailscaleIdentity(dataDirectory)
	if err != nil {
		return false, err
	}
	if !found {
		return false, recoverAndStoreTailscale(ctx, connection, dataDirectory)
	}
	defer func() { clear(expected.State) }()
	captured, err := captureTailscaleIdentityForDown(ctx, connection)
	if err != nil {
		return false, restartTailscaleAfterDownCaptureFailure(connection, err)
	}
	defer clear(captured.State)
	if err := verifyStableTailscaleIdentity(expected, captured); err != nil {
		return false, restartTailscaleAfterDownCaptureFailure(connection, err)
	}
	if err := storeTailscaleIdentity(dataDirectory, captured); err != nil {
		return false, restartTailscaleAfterDownCaptureFailure(connection, err)
	}
	return true, nil
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
		return tailscaleIdentity{}, errors.New("apply request for Tailscale is invalid")
	}
	if request.Mode == tailscaleApplyModeEnroll {
		if len(request.AuthKey) == 0 || len(request.AuthKey) > maximumTailscaleAuthKeyBytes || len(request.State) != 0 || request.WindowsUserSID != "" {
			return tailscaleIdentity{}, errors.New("enrollment request for Tailscale is invalid")
		}
	} else if len(request.State) == 0 || len(request.State) > maximumTailscaleStateBytes || len(request.AuthKey) != 0 || !windowsSIDPattern.MatchString(request.WindowsUserSID) {
		return tailscaleIdentity{}, errors.New("restoration request for Tailscale is invalid")
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

func captureTailscaleIdentityForDown(ctx context.Context, connection Connection) (tailscaleIdentity, error) {
	output, err := runSecretSSHPowerShell(ctx, connection, nil, buildTailscaleDownCaptureLauncher(), "capture stable Tailscale identity before down", maximumTailscaleCaptureResultBytes)
	if err != nil {
		return tailscaleIdentity{}, err
	}
	defer clear(output)
	return decodeTailscaleIdentityResult(output)
}

func restartTailscaleAfterDownCaptureFailure(connection Connection, cause error) error {
	if err := restartTailscaleAfterFailedDown(connection); err != nil {
		return errors.Join(cause, fmt.Errorf("restart Tailscale after failed down capture: %w", err))
	}
	return cause
}

func restartTailscaleAfterFailedDown(connection Connection) error {
	restartContext, cancel := context.WithTimeout(context.Background(), tailscaleDownRollbackTimeout)
	defer cancel()
	output, err := runSecretSSHPowerShell(restartContext, connection, nil, buildTailscaleRestartLauncher(), "restart Tailscale after failed down", 4096)
	clear(output)
	return err
}

func decodeTailscaleIdentityResult(data []byte) (tailscaleIdentity, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || len(trimmed) > maximumTailscaleCaptureResultBytes {
		return tailscaleIdentity{}, errors.New("identity result from Tailscale has invalid size")
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
%s

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$result = Invoke-TailscaleApply -ExpectedLength %d -ExpectedDigest '%s' -ApplySchemaVersion %d -MaximumAuthKeyBytes %d -IdentityNotEstablishedExitCode %d
[Console]::Out.Write(($result | ConvertTo-Json -Compress))
exit 0`, tailscalePowerShellFunctions(), tailscaleApplyPowerShell, payloadLength, expectedDigest, tailscaleApplySchemaVersion, maximumTailscaleAuthKeyBytes, tailscaleIdentityNotEstablishedExitCode)
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

func buildTailscaleDownCaptureLauncher() string {
	return fmt.Sprintf(`%s

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$currentSID = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
Set-TailscalePortablePolicy
$identity = Read-TailscaleIdentity -ExpectedSID $currentSID
if ($null -eq $identity) { throw 'Tailscale running identity is unavailable before state capture.' }
$result = Capture-TailscaleState -Identity $identity -LeaveServiceStopped -IdentityTimeoutSeconds 60
[Console]::Out.Write(($result | ConvertTo-Json -Compress))
exit 0`, tailscalePowerShellFunctions())
}

func buildTailscaleRestartLauncher() string {
	return fmt.Sprintf(`%s

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
Start-TailscaleService
exit 0`, tailscalePowerShellFunctions())
}

func tailscalePowerShellFunctions() string {
	return fmt.Sprintf(`$HerdrMaximumTailscaleStateBytes = %d
$HerdrTailscaleHostName = '%s'
$HerdrTailscaleIdentitySchemaVersion = %d
%s`, maximumTailscaleStateBytes, tailscaleHostName, tailscaleIdentitySchemaVersion, tailscaleLifecyclePowerShell)
}
