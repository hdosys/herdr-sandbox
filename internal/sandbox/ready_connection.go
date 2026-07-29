package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
)

// OpenReadyConnection reconstructs and verifies the exact app-owned ready
// session without re-running provisioning. The lifecycle lock is released before
// the caller starts the interactive client.
func OpenReadyConnection(ctx context.Context, output io.Writer) (connection Connection, resultErr error) {
	if output == nil {
		output = io.Discard
	}
	dataDirectory, err := defaultDataDirectory()
	if err != nil {
		return Connection{}, err
	}
	lockContext, cancelLock := context.WithTimeout(ctx, statusLifecycleLockTimeout)
	release, err := acquireLifecycleLock(lockContext)
	cancelLock()
	if err != nil {
		if ctx.Err() != nil {
			return Connection{}, err
		}
		status, inspectErr := inspectSessionDuringOperation(ctx, dataDirectory, err)
		if inspectErr == nil && status.Operation != nil && status.Operation.State == operationStateRunning {
			return Connection{}, errors.New("retained reprovisioning is active; wait for it to finish, then run `herdr-sandbox attach` again")
		}
		return Connection{}, fmt.Errorf("open ready Sandbox lifecycle: %w", err)
	}
	defer func() {
		if releaseErr := release(); resultErr == nil && releaseErr != nil {
			connection = Connection{}
			resultErr = releaseErr
		}
	}()
	if _, err := cleanupStaleStateAt(ctx, dataDirectory); err != nil {
		return Connection{}, fmt.Errorf("clean stale state before attach: %w", err)
	}

	releaseIdentity, err := loadHerdrRelease()
	if err != nil {
		return Connection{}, err
	}
	sandboxExecutable, err := windowsSandboxExecutable()
	if err != nil {
		return Connection{}, err
	}
	active, found, err := loadActiveSession(dataDirectory, sandboxExecutable)
	if err != nil {
		return Connection{}, err
	}
	if !found {
		return Connection{}, errors.New("no app-owned ready Sandbox exists; run `herdr-sandbox up`")
	}
	status, err := inspectSessionAt(ctx, dataDirectory)
	if err != nil {
		return Connection{}, err
	}
	if status.State != SessionReady {
		return Connection{}, fmt.Errorf("Sandbox state is %s, not ready; run `herdr-sandbox status`", status.State)
	}
	runDirectory := filepath.Join(dataDirectory, "runs", active.RunID)
	readyPath := filepath.Join(runDirectory, "status", readyFileName)
	ready, found, err := readOptionalStatus[readyStatus](readyPath)
	if err != nil {
		return Connection{}, fmt.Errorf("read ready Sandbox identity: %w", err)
	}
	if !found {
		return Connection{}, errors.New("ready Sandbox identity disappeared before attach")
	}
	if err := ready.validate(); err != nil {
		return Connection{}, fmt.Errorf("validate ready Sandbox identity: %w", err)
	}
	if ready.HerdrVersion != releaseIdentity.Version || ready.HerdrProtocol != releaseIdentity.Protocol {
		return Connection{}, fmt.Errorf("ready guest Herdr identity = %q protocol %d, required %q protocol %d",
			ready.HerdrVersion, ready.HerdrProtocol, releaseIdentity.Version, releaseIdentity.Protocol)
	}
	herdrExecutable, hostVersion, err := ensurePinnedHostHerdr(ctx, releaseIdentity, dataDirectory, output)
	if err != nil {
		return Connection{}, err
	}
	if hostVersion != releaseIdentity.Version {
		return Connection{}, fmt.Errorf("host Herdr version = %q, required %q", hostVersion, releaseIdentity.Version)
	}
	plan := runPlan{
		ID:              active.RunID,
		DataDirectory:   dataDirectory,
		RunDirectory:    runDirectory,
		StatusDirectory: filepath.Join(runDirectory, "status"),
		PrivateKeyPath:  filepath.Join(dataDirectory, "identity", "id_ed25519"),
	}
	connection, err = writeRunConnection(plan, connectableStatus(connectionStatus(ready)), herdrExecutable)
	if err != nil {
		return Connection{}, err
	}
	if err := connection.validate(); err != nil {
		return Connection{}, err
	}
	if err := verifySSH(ctx, connection); err != nil {
		return Connection{}, err
	}
	if err := verifyGuestHerdr(ctx, connection); err != nil {
		return Connection{}, err
	}
	after, found, err := loadActiveSession(dataDirectory, sandboxExecutable)
	if err != nil {
		return Connection{}, err
	}
	if !found || after != active {
		return Connection{}, errors.New("active Sandbox identity changed before attach")
	}
	readyAfter, found, err := readOptionalStatus[readyStatus](readyPath)
	if err != nil {
		return Connection{}, err
	}
	if !found || readyAfter != ready {
		return Connection{}, errors.New("ready Sandbox identity changed before attach")
	}
	if err := installRunConnectionAlias(dataDirectory, connection); err != nil {
		return Connection{}, err
	}
	fmt.Fprintf(output, "Ready Sandbox run %s verified.\n", active.RunID)
	fmt.Fprintf(output, "Remote attach: herdr --remote %s\n", connection.SSHTarget)
	return connection, nil
}

func (connection Connection) validate() error {
	for role, path := range map[string]string{
		"run directory":    connection.RunDirectory,
		"status directory": connection.StatusDirectory,
		"SSH config":       connection.SSHConfigPath,
		"private key":      connection.privateKeyPath,
		"Herdr executable": connection.herdrExecutable,
	} {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("Herdr connection %s is not absolute: %q", role, path)
		}
	}
	if connection.SSHTarget != sshTargetName || connection.GuestIP == "" || connection.WinGetVersion == "" ||
		connection.HerdrVersion == "" || connection.HerdrProtocol < 1 {
		return errors.New("Herdr connection identity is incomplete")
	}
	return nil
}
