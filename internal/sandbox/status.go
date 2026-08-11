package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const (
	statusSchemaVersion          = 1
	readyStatusSchemaVersion     = 2
	progressFileName             = "progress.json"
	connectableFileName          = "connectable.json"
	configurationHandoffFileName = "configuration-handoff.json"
	readyFileName                = "ready.json"
	failureFileName              = "failed.json"
	configurationHandoffVerified = "verified"
	configurationHandoffFailed   = "failed"
	maxStatusFileBytes           = 64 * 1024
	progressReadGrace            = 2 * time.Second
)

type progressStatus struct {
	SchemaVersion int    `json:"schemaVersion"`
	Phase         string `json:"phase"`
	Message       string `json:"message"`
}

type connectionStatus struct {
	SchemaVersion int    `json:"schemaVersion"`
	IP            string `json:"ip"`
	SSHUser       string `json:"sshUser"`
	SSHHostKey    string `json:"sshHostKey"`
	WinGetVersion string `json:"wingetVersion"`
	HerdrVersion  string `json:"herdrVersion"`
	HerdrProtocol int    `json:"herdrProtocol"`
}

type connectableStatus connectionStatus

type readyStatus connectionStatus

type configurationHandoffStatus struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Outcome       string               `json:"outcome"`
	Phase         string               `json:"phase,omitempty"`
	Message       string               `json:"message,omitempty"`
	MobileAccess  *mobileAccessHandoff `json:"mobileAccess,omitempty"`
}

type failureStatus struct {
	SchemaVersion int    `json:"schemaVersion"`
	Phase         string `json:"phase"`
	Message       string `json:"message"`
}

func waitForReady(ctx context.Context, statusDirectory string, output io.Writer) (readyStatus, error) {
	return waitForGuestStatus(ctx, statusDirectory, readyFileName, "readiness", output,
		func(status readyStatus) error { return status.validate() })
}

func waitForConnectable(ctx context.Context, statusDirectory string, output io.Writer) (connectableStatus, error) {
	return waitForGuestStatus(ctx, statusDirectory, connectableFileName, "connectability", output,
		func(status connectableStatus) error { return status.validate() })
}

func waitForGuestStatus[T any](
	ctx context.Context,
	statusDirectory string,
	targetFileName string,
	targetDescription string,
	output io.Writer,
	validateTarget func(T) error,
) (T, error) {
	var zero T
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	startedAt := time.Now()

	lastProgress := ""
	var progressReadErrorSince time.Time
	for {
		if failure, ok, err := readOptionalStatus[failureStatus](filepath.Join(statusDirectory, failureFileName)); err != nil {
			return zero, fmt.Errorf("read Sandbox failure status: %w", err)
		} else if ok {
			if err := failure.validate(); err != nil {
				return zero, fmt.Errorf("validate Sandbox failure status: %w", err)
			}
			return zero, fmt.Errorf("Sandbox phase %q failed: %s", failure.Phase, failure.Message)
		}

		if target, ok, err := readOptionalStatus[T](filepath.Join(statusDirectory, targetFileName)); err != nil {
			return zero, fmt.Errorf("read Sandbox %s status: %w", targetDescription, err)
		} else if ok {
			if err := validateTarget(target); err != nil {
				return zero, fmt.Errorf("validate Sandbox %s status: %w", targetDescription, err)
			}
			return target, nil
		}

		if progress, ok, err := readOptionalStatus[progressStatus](filepath.Join(statusDirectory, progressFileName)); err != nil {
			now := time.Now()
			if progressReadErrorSince.IsZero() {
				progressReadErrorSince = now
			}
			if now.Sub(progressReadErrorSince) >= progressReadGrace {
				return zero, fmt.Errorf("read Sandbox progress status after %s grace: %w", progressReadGrace, err)
			}
		} else if ok {
			progressReadErrorSince = time.Time{}
			if err := progress.validate(); err != nil {
				return zero, fmt.Errorf("validate Sandbox progress status: %w", err)
			}
			current := progress.Phase + "\x00" + progress.Message
			if current != lastProgress {
				elapsed := time.Since(startedAt).Round(100 * time.Millisecond)
				fmt.Fprintf(output, "[+%s] [%s] %s\n", elapsed, progress.Phase, progress.Message)
				lastProgress = current
			}
		} else {
			progressReadErrorSince = time.Time{}
		}

		select {
		case <-ctx.Done():
			return zero, fmt.Errorf("wait for Sandbox %s in %s: %w", targetDescription, statusDirectory, context.Cause(ctx))
		case <-ticker.C:
		}
	}
}

func writeConfigurationHandoff(statusDirectory string, status configurationHandoffStatus) error {
	if err := status.validate(); err != nil {
		return fmt.Errorf("validate configuration handoff: %w", err)
	}
	if !filepath.IsAbs(statusDirectory) {
		return fmt.Errorf("configuration handoff status directory is not absolute: %q", statusDirectory)
	}
	if info, err := os.Stat(statusDirectory); err != nil {
		return fmt.Errorf("inspect configuration handoff status directory: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("configuration handoff status path is not a directory: %s", statusDirectory)
	}
	path := filepath.Join(statusDirectory, configurationHandoffFileName)
	if _, err := os.Stat(path); err == nil {
		return errors.New("configuration handoff is already published")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect existing configuration handoff: %w", err)
	}
	data, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("encode configuration handoff: %w", err)
	}
	if err := writeFileAtomically(path, data, 0o600); err != nil {
		return fmt.Errorf("publish configuration handoff: %w", err)
	}
	verified, ok, err := readOptionalStatus[configurationHandoffStatus](path)
	if err != nil {
		return fmt.Errorf("read back configuration handoff: %w", err)
	}
	verifiedData, marshalErr := json.Marshal(verified)
	if marshalErr != nil {
		return fmt.Errorf("encode read-back configuration handoff: %w", marshalErr)
	}
	if !ok || !bytes.Equal(verifiedData, data) {
		return errors.New("configuration handoff read-back mismatch")
	}
	return nil
}

func readOptionalStatus[T any](path string) (T, bool, error) {
	var value T
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return value, false, nil
	}
	if err != nil {
		return value, false, err
	}
	reparse, err := fileInfoIsReparsePoint(info)
	if err != nil {
		return value, false, err
	}
	if reparse || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxStatusFileBytes {
		return value, false, errors.New("status file is not one bounded regular non-reparse file")
	}
	file, err := os.Open(path)
	if err != nil {
		return value, false, err
	}
	defer file.Close()

	limited := io.LimitReader(file, maxStatusFileBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return value, false, err
	}
	if len(data) > maxStatusFileBytes {
		return value, false, fmt.Errorf("status file exceeds %d bytes", maxStatusFileBytes)
	}
	allowedFields, err := statusFields(value)
	if err != nil {
		return value, false, err
	}
	shapeValidator := validateJSONObjectShape
	if _, exact := any(value).(explorerRestartStatus); exact {
		shapeValidator = validateExactJSONObjectShape
	}
	if err := shapeValidator(data, "status file", allowedFields); err != nil {
		return value, false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, false, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return value, false, errors.New("status file contains multiple JSON values")
		}
		return value, false, err
	}
	return value, true, nil
}

func statusFields(value any) ([]string, error) {
	switch value.(type) {
	case progressStatus:
		return []string{"schemaVersion", "phase", "message"}, nil
	case connectableStatus, readyStatus:
		return []string{"schemaVersion", "ip", "sshUser", "sshHostKey", "wingetVersion", "herdrVersion", "herdrProtocol"}, nil
	case configurationHandoffStatus:
		return []string{"schemaVersion", "outcome", "phase", "message", "mobileAccess"}, nil
	case failureStatus:
		return []string{"schemaVersion", "phase", "message"}, nil
	case explorerRestartStatus:
		return []string{"schemaVersion", "restartId", "taskName", "state", "sessionId", "stoppedPids", "startedPids", "message"}, nil
	default:
		return nil, fmt.Errorf("unsupported status type %T", value)
	}
}

func validateJSONObjectShape(data []byte, objectName string, allowedFields []string) error {
	_, err := decodeJSONObjectShape(data, objectName, allowedFields)
	return err
}

func validateExactJSONObjectShape(data []byte, objectName string, expectedFields []string) error {
	seen, err := decodeJSONObjectShape(data, objectName, expectedFields)
	if err != nil {
		return err
	}
	for _, field := range expectedFields {
		if !seen[field] {
			return fmt.Errorf("%s is missing field %q", objectName, field)
		}
	}
	return nil
}

func decodeJSONObjectShape(data []byte, objectName string, allowedFields []string) (map[string]bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if opening != json.Delim('{') {
		return nil, fmt.Errorf("%s must be a JSON object", objectName)
	}
	allowed := make(map[string]bool, len(allowedFields))
	for _, field := range allowedFields {
		allowed[field] = true
	}
	seen := make(map[string]bool, len(allowedFields))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok || !allowed[key] {
			return nil, fmt.Errorf("unknown field %q", key)
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate field %q", key)
		}
		seen[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			clear(value)
			return nil, err
		}
		clear(value)
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if closing != json.Delim('}') {
		return nil, fmt.Errorf("%s object is not closed", objectName)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s contains trailing JSON data", objectName)
	}
	return seen, nil
}

func (status progressStatus) validate() error {
	if status.SchemaVersion != statusSchemaVersion {
		return fmt.Errorf("schemaVersion = %d, want %d", status.SchemaVersion, statusSchemaVersion)
	}
	if err := validateTerminalText("phase", status.Phase, 128); err != nil {
		return err
	}
	if err := validateTerminalText("message", status.Message, 4096); err != nil {
		return err
	}
	return nil
}

func (status failureStatus) validate() error {
	if status.SchemaVersion != statusSchemaVersion {
		return fmt.Errorf("schemaVersion = %d, want %d", status.SchemaVersion, statusSchemaVersion)
	}
	if err := validateTerminalText("phase", status.Phase, 128); err != nil {
		return err
	}
	if err := validateTerminalText("message", status.Message, 4096); err != nil {
		return err
	}
	return nil
}

func (status connectableStatus) validate() error {
	return validateConnectionStatus(connectionStatus(status), statusSchemaVersion)
}

func (status readyStatus) validate() error {
	return validateConnectionStatus(connectionStatus(status), readyStatusSchemaVersion)
}

func validateConnectionStatus(status connectionStatus, expectedSchemaVersion int) error {
	if status.SchemaVersion != expectedSchemaVersion {
		return fmt.Errorf("schemaVersion = %d, want %d", status.SchemaVersion, expectedSchemaVersion)
	}
	ip := net.ParseIP(status.IP)
	if ip == nil || ip.To4() == nil || strings.Contains(status.IP, ":") {
		return fmt.Errorf("ip %q is not an IPv4 address", status.IP)
	}
	if status.SSHUser != "WDAGUtilityAccount" {
		return fmt.Errorf("sshUser = %q, want WDAGUtilityAccount", status.SSHUser)
	}
	fields := strings.Fields(status.SSHHostKey)
	if len(fields) != 2 || fields[0] != "ssh-ed25519" {
		return errors.New("sshHostKey must contain exactly one Ed25519 public key")
	}
	if _, err := base64.StdEncoding.DecodeString(fields[1]); err != nil {
		return fmt.Errorf("decode sshHostKey: %w", err)
	}
	if err := validateTerminalText("wingetVersion", status.WinGetVersion, 256); err != nil {
		return err
	}
	if err := validateTerminalText("herdrVersion", status.HerdrVersion, 256); err != nil {
		return err
	}
	if status.HerdrProtocol < 1 {
		return fmt.Errorf("herdrProtocol = %d, want a positive value", status.HerdrProtocol)
	}
	return nil
}

func (status configurationHandoffStatus) validate() error {
	if status.SchemaVersion != statusSchemaVersion {
		return fmt.Errorf("schemaVersion = %d, want %d", status.SchemaVersion, statusSchemaVersion)
	}
	switch status.Outcome {
	case configurationHandoffVerified:
		if status.Phase != "" || status.Message != "" {
			return errors.New("verified configuration handoff must not contain failure details")
		}
		if status.MobileAccess != nil {
			if err := status.MobileAccess.validate(); err != nil {
				return fmt.Errorf("validate verified mobile access handoff: %w", err)
			}
		}
	case configurationHandoffFailed:
		if status.MobileAccess != nil {
			return errors.New("failed configuration handoff must not publish mobile access")
		}
		if err := validateTerminalText("failed configuration handoff phase", status.Phase, 128); err != nil {
			return err
		}
		if err := validateTerminalText("failed configuration handoff message", status.Message, 4096); err != nil {
			return err
		}
	default:
		return fmt.Errorf("configuration handoff outcome = %q", status.Outcome)
	}
	return nil
}

func validateTerminalText(role, value string, maximumBytes int) error {
	if value == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s is empty or has surrounding whitespace", role)
	}
	if len([]byte(value)) > maximumBytes {
		return fmt.Errorf("%s exceeds %d UTF-8 bytes", role, maximumBytes)
	}
	for _, character := range value {
		if isUnsafeTerminalRune(character) {
			return fmt.Errorf("%s contains a non-printing terminal control", role)
		}
	}
	return nil
}

func isUnsafeTerminalRune(value rune) bool {
	return !unicode.IsPrint(value)
}

func sameConnectionIdentity(connectable connectableStatus, ready readyStatus) bool {
	left := connectionStatus(connectable)
	right := connectionStatus(ready)
	return left.IP == right.IP &&
		left.SSHUser == right.SSHUser &&
		left.SSHHostKey == right.SSHHostKey &&
		left.WinGetVersion == right.WinGetVersion &&
		left.HerdrVersion == right.HerdrVersion &&
		left.HerdrProtocol == right.HerdrProtocol
}
