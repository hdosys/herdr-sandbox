package sandbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const guestHerdrIntegrationTimeout = 2 * time.Minute

type guestHerdrIntegrationState string

const (
	guestHerdrIntegrationNotInstalled guestHerdrIntegrationState = "not installed"
	guestHerdrIntegrationCurrent      guestHerdrIntegrationState = "current"
	guestHerdrIntegrationOutdated     guestHerdrIntegrationState = "outdated"
	guestHerdrIntegrationNeedsRepair  guestHerdrIntegrationState = "needs repair"
)

type guestHerdrIntegrationSpec struct {
	target            string
	command           string
	configurationRoot string
}

func selectedGuestHerdrIntegrations(configuration codingAgentSyncConfiguration) []guestHerdrIntegrationSpec {
	selected := make([]guestHerdrIntegrationSpec, 0, 5)
	for _, candidate := range []struct {
		enabled bool
		spec    guestHerdrIntegrationSpec
	}{
		{configuration.OpenCode, guestHerdrIntegrationSpec{target: "opencode", command: "opencode", configurationRoot: `.config\opencode`}},
		{configuration.ClaudeCode, guestHerdrIntegrationSpec{target: "claude", command: "claude", configurationRoot: `.claude`}},
		{configuration.Codex, guestHerdrIntegrationSpec{target: "codex", command: "codex", configurationRoot: `.codex`}},
		{configuration.GitHubCopilot, guestHerdrIntegrationSpec{target: "copilot", command: "copilot", configurationRoot: `.copilot`}},
		{configuration.Pi, guestHerdrIntegrationSpec{target: "pi", command: "pi", configurationRoot: `.pi\agent`}},
	} {
		if candidate.enabled {
			selected = append(selected, candidate.spec)
		}
	}
	return selected
}

func installMissingGuestHerdrIntegrations(ctx context.Context, connection Connection, configuration codingAgentSyncConfiguration) ([]string, error) {
	specs := selectedGuestHerdrIntegrations(configuration)
	if len(specs) == 0 {
		return nil, nil
	}

	operationContext, cancel := context.WithTimeout(ctx, guestHerdrIntegrationTimeout)
	defer cancel()
	statuses, err := readGuestHerdrIntegrationStatuses(operationContext, connection, specs)
	if err != nil {
		return nil, err
	}

	missing := missingGuestHerdrIntegrations(specs, statuses)
	installed := make([]guestHerdrIntegrationSpec, 0, len(missing))
	for _, spec := range missing {
		available, err := installGuestHerdrIntegration(operationContext, connection, spec)
		if err != nil {
			return nil, fmt.Errorf("install missing %s Herdr integration: %w", spec.target, err)
		}
		if available {
			installed = append(installed, spec)
		}
	}
	if len(installed) == 0 {
		return nil, nil
	}

	verified, err := readGuestHerdrIntegrationStatuses(operationContext, connection, specs)
	if err != nil {
		return nil, fmt.Errorf("verify installed Herdr integrations: %w", err)
	}
	targets := make([]string, 0, len(installed))
	for _, spec := range installed {
		if verified[spec.target] != guestHerdrIntegrationCurrent {
			return nil, fmt.Errorf("verify installed Herdr integration %s: status = %q, want %q", spec.target, verified[spec.target], guestHerdrIntegrationCurrent)
		}
		targets = append(targets, spec.target)
	}
	return targets, nil
}

func missingGuestHerdrIntegrations(specs []guestHerdrIntegrationSpec, statuses map[string]guestHerdrIntegrationState) []guestHerdrIntegrationSpec {
	missing := make([]guestHerdrIntegrationSpec, 0, len(specs))
	for _, spec := range specs {
		if statuses[spec.target] == guestHerdrIntegrationNotInstalled {
			missing = append(missing, spec)
		}
	}
	return missing
}

func readGuestHerdrIntegrationStatuses(ctx context.Context, connection Connection, specs []guestHerdrIntegrationSpec) (map[string]guestHerdrIntegrationState, error) {
	output, err := runSSHPowerShell(ctx, connection, nil, guestHerdrIntegrationStatusScript(), "inspect guest Herdr integrations", maximumRemoteProvisionOutput)
	if err != nil {
		return nil, err
	}
	statuses, err := parseGuestHerdrIntegrationStatuses(output, specs)
	if err != nil {
		return nil, fmt.Errorf("inspect guest Herdr integrations: %w", err)
	}
	return statuses, nil
}

func parseGuestHerdrIntegrationStatuses(output []byte, specs []guestHerdrIntegrationSpec) (map[string]guestHerdrIntegrationState, error) {
	statuses := make(map[string]guestHerdrIntegrationState, len(specs))
	lines := strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n")
	for _, spec := range specs {
		prefix := spec.target + ": "
		for _, line := range lines {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			if _, duplicate := statuses[spec.target]; duplicate {
				return nil, fmt.Errorf("duplicate status for %s", spec.target)
			}
			detail := strings.TrimPrefix(line, prefix)
			state, err := classifyGuestHerdrIntegrationStatus(detail)
			if err != nil {
				return nil, fmt.Errorf("status for %s: %w", spec.target, err)
			}
			statuses[spec.target] = state
		}
	}
	return statuses, nil
}

func classifyGuestHerdrIntegrationStatus(detail string) (guestHerdrIntegrationState, error) {
	if !strings.HasSuffix(detail, ")") {
		return "", errors.New("missing integration path")
	}
	missingPrefix := string(guestHerdrIntegrationNotInstalled) + " ("
	if strings.HasPrefix(detail, missingPrefix) {
		if len(strings.TrimSpace(detail[len(missingPrefix):len(detail)-1])) == 0 {
			return "", errors.New("empty integration path")
		}
		return guestHerdrIntegrationNotInstalled, nil
	}
	for _, candidate := range []guestHerdrIntegrationState{
		guestHerdrIntegrationCurrent,
		guestHerdrIntegrationOutdated,
		guestHerdrIntegrationNeedsRepair,
	} {
		prefix := string(candidate) + " ("
		if !strings.HasPrefix(detail, prefix) {
			continue
		}
		remainder := detail[len(prefix) : len(detail)-1]
		separator := strings.Index(remainder, ") (")
		if separator > 0 && len(strings.TrimSpace(remainder[separator+3:])) > 0 {
			return candidate, nil
		}
		return "", errors.New("missing integration path")
	}
	return "", fmt.Errorf("unsupported status %q", detail)
}

func installGuestHerdrIntegration(ctx context.Context, connection Connection, spec guestHerdrIntegrationSpec) (bool, error) {
	script := guestHerdrIntegrationInstallScript(spec)
	output, err := runSSHPowerShell(ctx, connection, nil, script, "install guest "+spec.target+" Herdr integration", maximumRemoteProvisionOutput)
	if err != nil {
		return false, err
	}
	lines := strings.Fields(strings.ReplaceAll(string(output), "\r\n", "\n"))
	if len(lines) == 0 {
		return false, errors.New("missing installation outcome")
	}
	switch lines[len(lines)-1] {
	case "herdr-sandbox:installed":
		return true, nil
	case "herdr-sandbox:unavailable":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected installation outcome %q", boundedText(output))
	}
}

func guestHerdrIntegrationStatusScript() string {
	return "$ErrorActionPreference = 'Stop'; " + guestHerdrCommandPowerShell() +
		"& $herdr integration status; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }"
}

func guestHerdrIntegrationInstallScript(spec guestHerdrIntegrationSpec) string {
	quote := func(value string) string { return strings.ReplaceAll(value, "'", "''") }
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
%s
$env:Path = @([Environment]::GetEnvironmentVariable('Path', 'Machine'), [Environment]::GetEnvironmentVariable('Path', 'User')) -join ';'
$agentCommand = Get-Command '%s' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if ($null -eq $agentCommand) {
    [Console]::Out.WriteLine('herdr-sandbox:unavailable')
    exit 0
}
$home = [IO.Path]::GetFullPath([string]$env:USERPROFILE).TrimEnd([char]92)
$root = [IO.Path]::GetFullPath((Join-Path $home '%s')).TrimEnd([char]92)
if (-not $root.StartsWith($home + '\', [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Coding-agent integration root escapes the guest user profile.'
}
$current = $root
while ($true) {
    if (Test-Path -LiteralPath $current) {
        $item = Get-Item -LiteralPath $current -Force
        if (-not $item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Coding-agent integration root is unsafe: $current"
        }
    }
    if ($current -ieq $home) { break }
    $parent = Split-Path -Parent $current
    if ([string]::IsNullOrWhiteSpace($parent) -or $parent -ieq $current) {
        throw 'Coding-agent integration root parent resolution failed.'
    }
    $current = $parent.TrimEnd([char]92)
}
New-Item -ItemType Directory -Path $root -Force | Out-Null
& $herdr integration install '%s'
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
[Console]::Out.WriteLine('herdr-sandbox:installed')`, guestHerdrCommandPowerShell(), quote(spec.command), quote(spec.configurationRoot), quote(spec.target))
}
