package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

const (
	maximumSSHResultBytes                       = 1024 * 1024
	maximumSSHErrorBytes                        = 64 * 1024
	maximumSSHArchiveTransportCommandCharacters = 30000
)

func guestArchiveStagingPowerShell(directoryName, role string) string {
	return fmt.Sprintf(`$stagingRoot = '%s\staging'
$transferRoot = Join-Path $stagingRoot '%s'
$archive = Join-Path $transferRoot 'input.zip'
$expanded = Join-Path $transferRoot 'expanded'
$stagingRole = '%s'
function Assert-GuestArchivePath {
    param([Parameter(Mandatory = $true)][string]$Path)
    $current = [IO.Path]::GetFullPath($Path)
    while ($true) {
        $item = Get-Item -LiteralPath $current -Force -ErrorAction SilentlyContinue
        if ($null -ne $item) {
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "$stagingRole staging contains a reparse point: $current"
            }
        }
        $parent = [IO.Directory]::GetParent($current)
        if ($null -eq $parent) { break }
        $current = $parent.FullName
    }
}
function Assert-GuestArchiveTree {
    Assert-GuestArchivePath -Path $transferRoot
    if (Test-Path -LiteralPath $transferRoot) {
        $item = Get-Item -LiteralPath $transferRoot -Force -ErrorAction Stop
        if (-not $item.PSIsContainer) { throw "$stagingRole staging root is not a directory." }
        $pending = New-Object 'System.Collections.Generic.List[string]'
        $pending.Add([IO.Path]::GetFullPath($transferRoot)) | Out-Null
        while ($pending.Count -gt 0) {
            $index = $pending.Count - 1
            $directory = $pending[$index]
            $pending.RemoveAt($index)
            foreach ($child in @(Get-ChildItem -LiteralPath $directory -Force -ErrorAction Stop)) {
                if (($child.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                    throw "$stagingRole staging tree contains a reparse point: $($child.FullName)"
                }
                if ($child.PSIsContainer) { $pending.Add($child.FullName) | Out-Null }
            }
        }
    }
}
function Remove-GuestArchiveStaging {
    Assert-GuestArchivePath -Path $stagingRoot
    $rootItem = Get-Item -LiteralPath $transferRoot -Force -ErrorAction SilentlyContinue
    if ($null -eq $rootItem) { return }
    if (($rootItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        if ($rootItem.PSIsContainer) {
            [IO.Directory]::Delete($rootItem.FullName, $false)
        } else {
            [IO.File]::Delete($rootItem.FullName)
        }
    } elseif (-not $rootItem.PSIsContainer) {
        [IO.File]::SetAttributes($rootItem.FullName, [IO.FileAttributes]::Normal)
        [IO.File]::Delete($rootItem.FullName)
    } else {
        $pending = New-Object 'System.Collections.Generic.List[string]'
        $directories = New-Object 'System.Collections.Generic.List[string]'
        $pending.Add($rootItem.FullName) | Out-Null
        while ($pending.Count -gt 0) {
            $index = $pending.Count - 1
            $directory = $pending[$index]
            $pending.RemoveAt($index)
            $item = Get-Item -LiteralPath $directory -Force -ErrorAction Stop
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                [IO.Directory]::Delete($item.FullName, $false)
                continue
            }
            if (-not $item.PSIsContainer) { throw "$stagingRole staging directory changed type: $directory" }
            $directories.Add($item.FullName) | Out-Null
            foreach ($child in @(Get-ChildItem -LiteralPath $item.FullName -Force -ErrorAction Stop)) {
                if (($child.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                    if ($child.PSIsContainer) {
                        [IO.Directory]::Delete($child.FullName, $false)
                    } else {
                        [IO.File]::Delete($child.FullName)
                    }
                } elseif ($child.PSIsContainer) {
                    $pending.Add($child.FullName) | Out-Null
                } else {
                    [IO.File]::SetAttributes($child.FullName, [IO.FileAttributes]::Normal)
                    [IO.File]::Delete($child.FullName)
                }
            }
        }
        for ($index = $directories.Count - 1; $index -ge 0; $index--) {
            [IO.Directory]::Delete($directories[$index], $false)
        }
    }
    if ($null -ne (Get-Item -LiteralPath $transferRoot -Force -ErrorAction SilentlyContinue)) {
        throw "$stagingRole staging cleanup did not remove all input."
    }
}
Assert-GuestArchivePath -Path $stagingRoot
if (-not (Test-Path -LiteralPath $stagingRoot -PathType Container)) {
    New-Item -ItemType Directory -Path $stagingRoot -ErrorAction Stop | Out-Null
}
Assert-GuestArchivePath -Path $stagingRoot
Remove-GuestArchiveStaging
New-Item -ItemType Directory -Path $transferRoot -ErrorAction Stop | Out-Null
Assert-GuestArchiveTree`, guestRootDirectory, directoryName, role)
}

func runSSHArchivePowerShell(ctx context.Context, connection Connection, archive []byte, launcherScript, role string) ([]byte, error) {
	if len(archive) == 0 {
		return nil, fmt.Errorf("%s archive is empty", role)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(archive))
	transportCommand := buildSSHArchiveTransportCommand(digest, len(archive), launcherScript)
	if len(transportCommand) > maximumSSHArchiveTransportCommandCharacters {
		return nil, fmt.Errorf("%s SSH transport command exceeds %d characters", role, maximumSSHArchiveTransportCommandCharacters)
	}
	return runSSHRemoteCommandWithDiagnostics(ctx, connection, bytes.NewReader(archive), []string{transportCommand}, role, maximumSSHResultBytes, true)
}

func buildSSHArchiveTransportCommand(expectedDigest string, expectedArchiveLength int, launcherScript string) string {
	staging := guestArchiveStagingPowerShell("transport-"+expectedDigest[:16], "SSH archive transport")
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
%s
$expectedTransportLength = [long]%d
try {
    [Console]::Error.WriteLine('[ssh-transport] receive-archive')
    $inputStream = [Console]::OpenStandardInput()
    $outputStream = [IO.File]::Open($archive, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
    try {
        $remaining = $expectedTransportLength
        $buffer = New-Object byte[] 8192
        while ($remaining -gt 0) {
            $requested = [int][Math]::Min([long]$buffer.Length, $remaining)
            $read = $inputStream.Read($buffer, 0, $requested)
            if ($read -le 0) { throw "SSH archive transport ended with $remaining bytes missing." }
            $outputStream.Write($buffer, 0, $read)
            $remaining -= $read
        }
        $outputStream.Flush($true)
    } finally {
        $outputStream.Dispose()
    }
    Remove-Item Env:PSModulePath -ErrorAction SilentlyContinue
    $process = Start-Process -FilePath 'powershell.exe' -ArgumentList @('-NoLogo','-NoProfile','-NonInteractive','-WindowStyle','Hidden','-ExecutionPolicy','Bypass','-EncodedCommand','%s') -RedirectStandardInput $archive -NoNewWindow -Wait -PassThru
    if ($process.ExitCode -ne 0) { exit $process.ExitCode }
} finally {
    Remove-GuestArchiveStaging
}
exit 0`, staging, expectedArchiveLength, encodePowerShell(launcherScript))
}

func runSSHPowerShell(ctx context.Context, connection Connection, input io.Reader, launcherScript, role string, maximumOutput int) ([]byte, error) {
	return runSSHPowerShellWithDiagnostics(ctx, connection, input, launcherScript, role, maximumOutput, true)
}

func runSecretSSHPowerShell(ctx context.Context, connection Connection, input io.Reader, launcherScript, role string, maximumOutput int) ([]byte, error) {
	return runSSHPowerShellWithDiagnostics(ctx, connection, input, launcherScript, role, maximumOutput, false)
}

func runSSHPowerShellWithDiagnostics(ctx context.Context, connection Connection, input io.Reader, launcherScript, role string, maximumOutput int, includeRemoteDiagnostics bool) ([]byte, error) {
	return runSSHRemoteCommandWithDiagnostics(ctx, connection, input, []string{
		"powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", encodePowerShell(launcherScript),
	}, role, maximumOutput, includeRemoteDiagnostics)
}

func runSSHRemoteCommandWithDiagnostics(ctx context.Context, connection Connection, input io.Reader, remoteArguments []string, role string, maximumOutput int, includeRemoteDiagnostics bool) ([]byte, error) {
	if maximumOutput <= 0 {
		return nil, fmt.Errorf("%s output limit is invalid", role)
	}
	sshExecutable, err := exec.LookPath("ssh.exe")
	if err != nil {
		return nil, errors.New("OpenSSH ssh.exe is not on PATH")
	}
	arguments := []string{"-T", "-F", connection.SSHConfigPath, connection.SSHTarget}
	arguments = append(arguments, remoteArguments...)
	command := hiddenCommandContext(ctx, sshExecutable, arguments...)
	command.Stdin = input
	stdout := boundedCommandOutput{maximum: maximumOutput}
	stderr := boundedCommandOutput{maximum: maximumSSHErrorBytes}
	defer stderr.clear()
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		stdout.clear()
		remoteDiagnostics := ""
		if includeRemoteDiagnostics {
			remoteDiagnostics = stderr.text()
		}
		if contextError := ctx.Err(); contextError != nil {
			return nil, sshPowerShellError(role, err, contextError, remoteDiagnostics, includeRemoteDiagnostics)
		}
		return nil, sshPowerShellError(role, err, nil, remoteDiagnostics, includeRemoteDiagnostics)
	}
	if stdout.overflow {
		stdout.clear()
		return nil, fmt.Errorf("%s over SSH exceeded the %d-byte output limit", role, maximumOutput)
	}
	return stdout.buffer.Bytes(), nil
}

func sshPowerShellError(role string, commandError, contextError error, remoteDiagnostics string, includeRemoteDiagnostics bool) error {
	if contextError != nil {
		if includeRemoteDiagnostics {
			return fmt.Errorf("%s over SSH: %w (%v): %s", role, commandError, contextError, remoteDiagnostics)
		}
		return fmt.Errorf("%s over SSH: %w (%v); remote diagnostics redacted", role, commandError, contextError)
	}
	if includeRemoteDiagnostics {
		return fmt.Errorf("%s over SSH: %w: %s", role, commandError, remoteDiagnostics)
	}
	return fmt.Errorf("%s over SSH: %w; remote diagnostics redacted", role, commandError)
}

type boundedCommandOutput struct {
	buffer   bytes.Buffer
	maximum  int
	overflow bool
}

func (output *boundedCommandOutput) Write(data []byte) (int, error) {
	written := len(data)
	remaining := output.maximum - output.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			_, _ = output.buffer.Write(data[:remaining])
		} else {
			_, _ = output.buffer.Write(data)
		}
	}
	if len(data) > remaining {
		output.overflow = true
	}
	return written, nil
}

func (output *boundedCommandOutput) text() string {
	text := boundedText(output.buffer.Bytes())
	if output.overflow {
		return text + " [truncated]"
	}
	return text
}

func (output *boundedCommandOutput) clear() {
	clear(output.buffer.Bytes())
	output.buffer.Reset()
	output.overflow = false
}
