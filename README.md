# herdr-sandbox

`herdr-sandbox` creates a disposable Windows development environment in Windows Sandbox, provisions the tools required by selected projects, starts a persistent Herdr server in the guest, and connects the host Herdr client over SSH.

The host repositories remain on the host and are mapped into the guest. Private SSH keys, unrelated repositories, the host home directory, and general AppData are not mapped.

## What it does

`herdr-sandbox up` performs one bounded workflow:

1. Validates the host, global configuration, project profiles, package plan, cache paths, and Windows Sandbox state.
2. Creates an immutable per-run input snapshot and a narrow writable status directory.
3. Starts a fresh Windows Sandbox with a visible bootstrap console.
4. Applies the selected Windows privacy/development baseline and project stacks.
5. Installs and verifies WinGet, OpenSSH Server, PowerShell 7, Herdr, and selected development tools inside the guest.
6. Verifies SSH and the guest Herdr server from the host.
7. Transfers selected Git, GitHub CLI, OpenCode, Herdr, and Windows Terminal configuration without staging credentials in run files.
8. Creates one Herdr workspace for each mapped project and attaches the host Herdr client.

The visible bootstrap console inside Windows Sandbox is intentional. It shows guest progress and does not require interaction.

When that exact app-owned Sandbox is already ready, running `up` again does not recreate it. The tool verifies that memory, cache, and workspace mappings still match, snapshots the current Base/stack/project scripts, re-runs Development provisioning inside the existing guest, reapplies host configuration, and reattaches.

## Requirements

Host requirements:

- Windows 10 or Windows 11 edition that supports Windows Sandbox.
- Hardware virtualization enabled.
- The Windows Sandbox optional feature enabled.
- Windows PowerShell 5.1.
- OpenSSH Client (`ssh.exe` and `ssh-keygen.exe`).
- Windows Terminal Stable or Preview.
- Internet access for initial cache misses.
- Go 1.26.4 or newer to build this repository.

Enable Windows Sandbox from an elevated Windows PowerShell session if necessary:

```powershell
Enable-WindowsOptionalFeature -Online -FeatureName Containers-DisposableClientVM -All
```

Windows may require a restart after enabling the feature.

The application installs or updates its pinned host `herdr.exe` automatically. Do not install Rust tooling on the host for this project.

## Build

Clone the repository and run the checked build task from its root:

```powershell
go run ./cmd/task check
```

The stable application and its editable provisioning assets are written to:

```text
build\bin\herdr-sandbox.exe
build\bin\base.ps1
build\bin\stacks.ps1
```

For a build without the full verification gate:

```powershell
go run ./cmd/task build
```

## Quick start

### 1. Add a project profile

Create this file in the project you want to open in the Sandbox:

```text
<project>\.herdr-sandbox\provision.ps1
```

Minimal Go example:

```powershell
param(
    [Parameter(Mandatory = $true)]
    [string]$ProjectDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

Install-GoStack -ProjectDirectory $ProjectDirectory
```

The nearest ancestor containing this file becomes the active project when `up` is run. Project profiles call shared stack functions and may add project-specific Windows PowerShell 5.1 commands. Do not put project scripts in the repository's `provisioning` directory; run-local copies are generated automatically.

### 2. Start from the project

Run the built application with the project as the current directory:

```powershell
& "D:\path\to\herdr-sandbox\build\bin\herdr-sandbox.exe" up
```

The first run can take longer because package and Visual Studio layout caches are empty. Later cache-hit runs reuse verified payloads.

When provisioning completes, the application prints:

```text
herdr --remote sandbox
```

and attaches automatically when stdin, stdout, and stderr are connected to a real interactive terminal.

### 3. Reattach later

The guest Herdr server remains running after a normal detach. Reattach from any normal host terminal:

```powershell
herdr --remote sandbox
```

Plain SSH is also available for noninteractive diagnostics:

```powershell
ssh sandbox
```

The managed `sandbox` alias uses the verified guest IP, public host key, host-only private key, and strict host-key checking.
Reuse here means reconnecting to the same ready guest and persistent Herdr server. The managed target disables OpenSSH `ControlMaster` multiplexing because native Win32 OpenSSH does not support its required Unix socket/file-descriptor path.

## Commands

```text
herdr-sandbox up [--memory-mb MB] [--timeout 20m]
herdr-sandbox status
herdr-sandbox down
herdr-sandbox clean
```

### `up`

Creates a fresh Sandbox when none exists. If the exact app-owned Sandbox is already ready and its fixed launch plan still matches, `up` re-runs the current provisioning scripts in that guest and then reattaches. This is the normal edit/test loop for `.herdr-sandbox\provision.ps1` changes.

It refuses unmanaged, starting, failed, or stale instances. A changed workspace, cache, or memory plan requires `down` followed by `up`, because Windows Sandbox mappings and memory cannot change after launch.

`--memory-mb` overrides the configured memory for one run. The minimum is 2048 MB.

### `status`

Reads app-owned state without changing it. States are:

- `starting`
- `ready`
- `failed`
- `stale`
- `stopped`
- `unmanaged`

### `down`

Stops only the exact app-owned Sandbox after revalidating its process identity. It does not force-kill changed, unrelated, or unmanaged processes.

### `clean`

Explicitly removes inactive app-owned run workspaces. A valid active or stale recorded run is preserved. Cleanup refuses to proceed while a running Sandbox is unmanaged or its ownership changed, and it never follows or removes unknown or reparse-bearing paths. The SSH identity, stable alias, and package/tool cache are unaffected.

## Global configuration

The first run creates:

```text
%APPDATA%\herdr-sandbox\config.json
%APPDATA%\herdr-sandbox\base.ps1
```

Example `config.json`:

```json
{
  "cacheDirectory": "",
  "memoryMB": 32768,
  "wingetPackages": {
    "remove": [],
    "add": [],
    "versions": {}
  },
  "workspaces": {
    "herdr": "D:\\Projects\\herdr"
  }
}
```

Fields:

- `cacheDirectory`: absolute host directory for package/tool caches. Empty uses `<system-temp>\herdr-sandbox\cache`.
- `memoryMB`: default Sandbox memory, minimum 2048.
- `workspaces`: additional workspace names mapped to absolute host project roots.
- `wingetPackages.remove`: known optional Base packages to omit.
- `wingetPackages.add`: exact extra WinGet package IDs.
- `wingetPackages.versions`: exact versions for retained or added packages.

The nearest active project profile is added automatically and deduplicated against global workspaces.

`base.ps1` is user-owned after it is first seeded and is not silently overwritten. If a newer binary reports an unsupported Base contract, merge the current repository `provisioning\base.ps1` changes into the global file before retrying.

## Built-in project stacks

Project profiles may call:

```powershell
Install-GoStack -ProjectDirectory $ProjectDirectory
Install-NodeStack
Install-PythonStack
Install-ZigStack
Install-RustMSVCStack -ProjectDirectory $ProjectDirectory
Install-CargoNextest
Install-Just
```

Stacks install only the concrete tools they own. Optional versions are supported by the functions that expose a version parameter; unavailable versions fail instead of falling back silently.

TypeScript and other application libraries remain project dependencies owned by the project's package manifest and lockfile.

## Important paths

Host:

```text
%APPDATA%\herdr-sandbox\config.json       global settings
%APPDATA%\herdr-sandbox\base.ps1         editable global Base profile
%LOCALAPPDATA%\herdr-sandbox\identity\   app-owned SSH identity
%LOCALAPPDATA%\herdr-sandbox\runs\       per-run input, status, SSH, and .wsb files
%LOCALAPPDATA%\herdr-sandbox\ssh\        stable managed SSH alias configuration
<system-temp>\herdr-sandbox\cache\       default package/tool cache
```

Each run directory is isolated because its `.wsb`, provisioning snapshot, status records, and known host key belong to one exact Sandbox process. Do not edit an active run directory. Inactive diagnostics remain until `herdr-sandbox clean` is invoked.

Guest:

```text
C:\HerdrSandbox\cache\        mapped host cache
C:\HerdrSandbox\runtime\      guest runtime state
C:\HerdrSandbox\tools\        installed guest tools
C:\HerdrSandbox\toolchains\   Rust and other toolchains
C:\HerdrSandbox\build\        guest-local build state
C:\HerdrSandbox\staging\      bounded temporary staging
C:\Workspaces\<name>\         explicitly selected writable projects
```

## Security boundaries

- The host private SSH key is never mapped into the guest.
- Only selected project roots are writable guest mappings.
- The host home directory, general AppData, unrelated repositories, and private SSH/GPG keys are not mapped.
- OpenCode and GitHub CLI credentials are streamed only through the verified SSH channel and are not written to host run state.
- Guest OpenCode managed policy sets every resolved agent permission to `allow`; host permission settings remain unchanged.
- Downloads and cache hits are validated against strict metadata, versions, hashes, signatures, or package identity as applicable.
- An app-owned Sandbox is stopped only after exact process identity revalidation.
- Run cleanup preserves the valid active identity and rejects every exact-ID candidate containing a reparse point before deleting any candidate.

## Development

Repository tasks:

```powershell
go run ./cmd/task fmt
go run ./cmd/task test
go run ./cmd/task build
go run ./cmd/task check
```

`check` covers Go formatting, Windows PowerShell 5.1 syntax, all Go tests, `go vet`, and the stable build.

Repository-owned Windows scripting uses Windows PowerShell 5.1. PowerShell 7 is installed as interactive guest tooling, not as a provisioning interpreter.

## Troubleshooting

### `up` refuses an existing Sandbox

Inspect it first:

```powershell
herdr-sandbox status
```

Stop an exact app-owned instance with:

```powershell
herdr-sandbox down
```

An exact ready app-owned Sandbox is reused automatically. The tool intentionally refuses to close or reuse unmanaged/non-ready Windows Sandbox processes.

### Automatic attach fails in a redirected/headless process

This is expected. Provisioning remains ready. Open a real interactive terminal and run:

```powershell
herdr --remote sandbox
```

### `ssh sandbox` no longer reaches the guest

Run `herdr-sandbox status`. A stale state means the recorded Sandbox process ended. Start a fresh Sandbox after clearing the stale app-owned state with `herdr-sandbox down`.

### Global Base contract is outdated

The global Base file is deliberately not overwritten. Compare it with:

```text
provisioning\base.ps1
```

Merge deliberate local customizations into the current contract and retry.

### Initial provisioning is slow

The first run may download WinGet payloads, Herdr/OpenSSH assets, Rust distributions, and a Visual Studio Build Tools layout. Confirm that the configured cache directory is writable and does not overlap a workspace or app-owned run state.

### Old run diagnostics consume space

Inspect the current owner first with `herdr-sandbox status`, then run `herdr-sandbox clean`. The command preserves the recorded active run and removes only strictly validated inactive run workspaces; it does not remove the persistent cache.

## More detail

- Stable user-visible behavior: [`PRODUCT.md`](PRODUCT.md)
- Technical boundaries: [`ARCHITECTURE.md`](ARCHITECTURE.md)
- Open work: [`BACKLOG.md`](BACKLOG.md)
- Agent/repository rules: [`AGENTS.md`](AGENTS.md)
