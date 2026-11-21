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
	"net/url"
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

var errTailscaleIdentityNotEstablished = errors.New("enrollment through Tailscale did not establish a running identity")

//go:embed assets/tailscale-lifecycle.ps1
var tailscaleLifecyclePowerShell string

//go:embed assets/tailscale-apply.ps1
var tailscaleApplyPowerShell string

//go:embed assets/tailscale-enroll.ps1
var tailscaleEnrollPowerShell string

type tailscaleBootstrap struct {
	Enabled  bool
	Existing *tailscaleIdentity
}

func (bootstrap tailscaleBootstrap) confirmedNotEnrolled(wasReady bool, err error) bool {
	return bootstrap.Enabled && !wasReady && bootstrap.Existing == nil && errors.Is(err, errTailscaleIdentityNotEstablished)
}

type tailscaleApplyRequest struct {
	SchemaVersion  int    `json:"schemaVersion"`
	Mode           string `json:"mode"`
	State          []byte `json:"state"`
	WindowsUserSID string `json:"windowsUserSID"`
}

type tailscaleStateCapture struct {
	WindowsUserSID string `json:"windowsUserSID"`
	State          []byte `json:"state"`
}

func prepareTailscaleBootstrap(dataDirectory string, enabled bool) (tailscaleBootstrap, error) {
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
	return bootstrap, nil
}

func (bootstrap *tailscaleBootstrap) clear() {
	if bootstrap.Existing != nil {
		clear(bootstrap.Existing.State)
		bootstrap.Existing.State = nil
	}
}

func configureFreshTailscale(ctx context.Context, connection Connection, dataDirectory string, bootstrap tailscaleBootstrap, output io.Writer) error {
	if !bootstrap.Enabled {
		return nil
	}
	request := tailscaleApplyRequest{SchemaVersion: tailscaleApplySchemaVersion}
	if bootstrap.Existing == nil {
		if err := beginTailscaleEnrollment(ctx, connection, output); err != nil {
			return err
		}
		request.Mode = tailscaleApplyModeEnroll
	} else {
		request.Mode = tailscaleApplyModeRestore
		request.State = append([]byte(nil), bootstrap.Existing.State...)
		request.WindowsUserSID = bootstrap.Existing.WindowsUserSID
	}
	defer clear(request.State)
	identity, err := applyAndCaptureTailscale(ctx, connection, request)
	if err != nil {
		return fmt.Errorf("identity for Tailscale is not ready; complete browser/device approval or check device expiry and revocation in your tailnet; the guest is preserved: %w", err)
	}
	defer clear(identity.State)
	if bootstrap.Existing != nil {
		if err := verifyStableTailscaleIdentity(*bootstrap.Existing, identity); err != nil {
			return err
		}
	}
	return storeTailscaleIdentity(dataDirectory, identity)
}

type tailscaleEnrollment struct {
	BackendState string `json:"backendState"`
	AuthURL      string `json:"authURL"`
}

func decodeTailscaleEnrollment(data []byte) (tailscaleEnrollment, error) {
	var result tailscaleEnrollment
	if len(data) > 8192 {
		return result, errors.New("tailscale enrollment response is too large")
	}
	if err := validateExactJSONObjectShape(data, "tailscale enrollment", []string{"backendState", "authURL"}); err != nil {
		return result, errors.New("tailscale enrollment response shape is invalid")
	}
	if err := decodeStrictJSON(data, &result); err != nil {
		return result, errors.New("tailscale enrollment response is invalid")
	}
	switch result.BackendState {
	case "Running", "NeedsMachineAuth":
		if result.AuthURL != "" {
			return result, errors.New("tailscale returned an unexpected enrollment URL")
		}
	case "NeedsLogin":
		link, err := url.Parse(result.AuthURL)
		if err != nil || len(result.AuthURL) > 2048 || link.Scheme != "https" || link.Host != "login.tailscale.com" ||
			link.User != nil || !strings.HasPrefix(link.Path, "/a/") || len(strings.TrimPrefix(link.Path, "/a/")) == 0 ||
			link.RawQuery != "" || link.Fragment != "" || strings.ContainsAny(result.AuthURL, "\x00\r\n\t ") {
			return result, errors.New("tailscale returned an invalid browser enrollment URL")
		}
	default:
		return result, errors.New("tailscale did not start browser enrollment")
	}
	return result, nil
}

func beginTailscaleEnrollment(ctx context.Context, connection Connection, output io.Writer) error {
	launcher := tailscalePowerShellFunctions() + "\n" + tailscaleEnrollPowerShell
	resultJSON, err := runSecretSSHPowerShell(ctx, connection, nil, launcher, "start Tailscale browser enrollment", 8192)
	if err != nil {
		if commandExitedWith(err, tailscaleIdentityNotEstablishedExitCode) {
			return errTailscaleIdentityNotEstablished
		}
		return err
	}
	defer clear(resultJSON)
	result, err := decodeTailscaleEnrollment(resultJSON)
	if err != nil {
		return err
	}
	if result.BackendState == "NeedsLogin" {
		fmt.Fprintf(output, "Sign in to your existing tailnet in a browser:\n%s\nWaiting up to eight minutes for this Sandbox to be authorized...\n", result.AuthURL)
		if err := openShellTarget(result.AuthURL); err != nil {
			fmt.Fprintf(output, "Could not open the browser: %v. Open the link above to continue.\n", err)
		}
	} else if result.BackendState == "NeedsMachineAuth" {
		fmt.Fprintln(output, "Approve herdr-sandbox in your Tailscale admin console. Waiting up to eight minutes...")
	}
	return nil
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
	captured, err := captureTailscaleStateForDown(ctx, connection)
	if err != nil {
		return false, restartTailscaleAfterDownCaptureFailure(connection, err)
	}
	defer clear(captured.State)
	if captured.WindowsUserSID != expected.WindowsUserSID {
		return false, restartTailscaleAfterDownCaptureFailure(connection, errors.New("captured Tailscale state belongs to a different Windows Sandbox user SID"))
	}
	clear(expected.State)
	expected.State = append([]byte(nil), captured.State...)
	if err := storeTailscaleIdentity(dataDirectory, expected); err != nil {
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
		if len(request.State) != 0 || request.WindowsUserSID != "" {
			return tailscaleIdentity{}, errors.New("enrollment request for Tailscale is invalid")
		}
	} else if len(request.State) == 0 || len(request.State) > maximumTailscaleStateBytes || !windowsSIDPattern.MatchString(request.WindowsUserSID) {
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

func captureTailscaleStateForDown(ctx context.Context, connection Connection) (tailscaleStateCapture, error) {
	output, err := runSecretSSHPowerShell(ctx, connection, nil, buildTailscaleDownCaptureLauncher(), "capture stable Tailscale identity before down", maximumTailscaleCaptureResultBytes)
	if err != nil {
		return tailscaleStateCapture{}, err
	}
	defer clear(output)
	return decodeTailscaleStateCaptureResult(output)
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

func decodeTailscaleStateCaptureResult(data []byte) (tailscaleStateCapture, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || len(trimmed) > maximumTailscaleCaptureResultBytes {
		return tailscaleStateCapture{}, errors.New("state capture result from Tailscale has invalid size")
	}
	if err := validateExactJSONObjectShape(trimmed, "Tailscale state capture result", []string{"windowsUserSID", "state"}); err != nil {
		return tailscaleStateCapture{}, fmt.Errorf("decode Tailscale state capture result: %w", err)
	}
	var captured tailscaleStateCapture
	if err := decodeStrictJSON(trimmed, &captured); err != nil {
		clear(captured.State)
		return tailscaleStateCapture{}, fmt.Errorf("decode Tailscale state capture result: %w", err)
	}
	if len(captured.WindowsUserSID) > 184 || !windowsSIDPattern.MatchString(captured.WindowsUserSID) || len(captured.State) == 0 || len(captured.State) > maximumTailscaleStateBytes {
		clear(captured.State)
		return tailscaleStateCapture{}, errors.New("validate Tailscale state capture result: SID or state size is invalid")
	}
	return captured, nil
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
$result = Invoke-TailscaleApply -ExpectedLength %d -ExpectedDigest '%s' -ApplySchemaVersion %d
[Console]::Out.Write(($result | ConvertTo-Json -Compress))
exit 0`, tailscalePowerShellFunctions(), tailscaleApplyPowerShell, payloadLength, expectedDigest, tailscaleApplySchemaVersion)
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
$result = Capture-TailscaleStateBytes -ExpectedSID $currentSID -LeaveServiceStopped
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
