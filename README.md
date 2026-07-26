# herdr-sandbox

**Move your coding-agent setup into a disposable Windows environment—without rebuilding it by hand or living in RDP.**

[![Nightly checks](https://github.com/hdosys/herdr-sandbox/actions/workflows/nightly.yml/badge.svg)](https://github.com/hdosys/herdr-sandbox/actions/workflows/nightly.yml)
[![Release](https://github.com/hdosys/herdr-sandbox/actions/workflows/release.yml/badge.svg)](https://github.com/hdosys/herdr-sandbox/actions/workflows/release.yml)

Think of `herdr-sandbox` as a Windows-native cousin of a [dev container](https://containers.dev/): a repeatable, project-defined development environment, but backed by a real disposable Windows guest rather than a Linux container.

Container-first tools make isolated Linux development straightforward. Native Windows work often leaves a worse choice: give agents, installers, and package scripts access to the host (or answer constant permission prompts), or maintain a full VM and drive it over RDP.

`herdr-sandbox up` closes that gap. It maps only the projects you select, provisions their Windows toolchains, transfers selected configuration and authentication over verified SSH, starts Herdr in the guest, and connects your normal terminal. Source edits persist on the host; guest tools and processes can be discarded without an uninstall ritual. Private SSH keys, unrelated repositories, the host home directory, and general AppData stay out of the mapping.

This is a contract-driven product rather than a loose bootstrap script: versioned provisioning contracts, validated mappings and downloads, bounded phases, explicit status, and fail-closed handoffs make runs repeatable and failures diagnosable.

**Agent support:** OpenCode is currently the only coding agent whose configuration and authentication are transferred automatically. Other agents can be installed and configured in a project profile, but out-of-the-box migration for them is future work.

**Herdr dependency:** Install the maintainer's [`herdr-win`](https://github.com/hdosys/herdr-win) fork first. Sandbox needs its Windows remote support, which official upstream builds do not provide yet, and keeps the host and guest Herdr versions aligned. A combined WinGet package is planned.

**Limits:** This is practical isolation, not a complete security boundary. Mapped projects remain writable and networking is enabled, so keep backups and normal supply-chain controls. Durable VM management is planned; today the focus is disposable Windows environments.

## Quick start

You need a supported Windows edition with Windows Sandbox enabled, an existing `herdr-win` `herdr.exe` on `PATH`, Windows PowerShell 5.1, OpenSSH Client, Windows Terminal, and Go. See [Requirements](#requirements) for the complete list.

### 1. Build the CLI

Clone the repository and run its checked build from the repository root:

```powershell
go run ./cmd/task check
```

This verifies the repository and writes the executable with its editable provisioning assets to:

```text
build\bin\herdr-sandbox.exe
build\bin\base.ps1
build\bin\stacks.ps1
```

For a build without the full verification gate:

```powershell
go run ./cmd/task build
```

### 2. Add a project profile

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

Choose and combine built-ins by calling them directly from the profile:

| Development need | Direct profile call |
| --- | --- |
| Go | `Install-GoStack -ProjectDirectory $ProjectDirectory` |
| Node.js LTS | `Install-NodeStack` |
| Python (3.13 by default) | `Install-PythonStack` |
| Zig | `Install-ZigStack` |
| Rust with MSVC Build Tools | `Install-RustMSVCStack -ProjectDirectory $ProjectDirectory` |
| Cargo Nextest | `Install-CargoNextest` |
| Just | `Install-Just` |

Keep built-in calls direct—not behind aliases or dynamic invocation—so the host can inspect requirements without executing project code. Exact parameters and optional version selectors live in [`provisioning\stacks.ps1`](provisioning/stacks.ps1); unavailable versions fail instead of silently falling back. TypeScript and other application libraries remain project dependencies owned by the project's manifest and lockfile.

**Rust/MSVC host window:** The first Rust-stack run may show Microsoft Visual Studio Installer on the host. This is expected: the signed bootstrapper runs in [layout mode](https://learn.microsoft.com/visualstudio/install/create-an-offline-installation-of-visual-studio) to download and verify only the required C++ Build Tools and Windows SDK files in the app-owned cache; it does not install Visual Studio or Build Tools on the host. Complete layout creation was not reliable inside a Windows 10 Sandbox, and keeping the layout in the persistent cache avoids downloading it for every disposable guest. The guest revalidates and copies the layout locally, then installs from it with networking disabled for that installer.

Need something not listed?

- Add an exact WinGet package needed in every guest through [`wingetPackages.add`](#global-configuration).
- Add idempotent, fail-fast Windows PowerShell 5.1 commands needed by one project directly to its profile and verify the resulting tool.
- Contribute a reusable built-in as one concrete `Install-*` function in `provisioning\stacks.ps1`, with project-plan recognition and tests. There is intentionally no plugin registry to maintain.

The nearest ancestor containing the profile becomes the active project when `up` is run. Do not put project scripts in this repository's `provisioning` directory; run-local copies are generated automatically.

### 3. Start from the project

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

### 4. Reattach later

The guest Herdr server remains running after a normal detach. Reattach from any normal host terminal:

```powershell
herdr --remote sandbox
```

Plain SSH is also available for noninteractive diagnostics:

```powershell
ssh sandbox
```

The managed `sandbox` alias uses the verified guest IP, public host key, host-only private key, and strict host-key checking. Reuse here means reconnecting to the same ready guest and persistent Herdr server. The managed target disables OpenSSH `ControlMaster` multiplexing because native Win32 OpenSSH does not support its required Unix socket/file-descriptor path.

## What it does

`herdr-sandbox up` performs one bounded workflow:

1. Validates the host, global configuration, project profiles, package plan, cache paths, and Windows Sandbox state.
2. Creates an immutable per-run input snapshot and a narrow writable status directory.
3. Starts a fresh Windows Sandbox with a visible bootstrap console.
4. Applies the selected Windows privacy/development baseline and project stacks.
5. Installs and verifies WinGet, OpenSSH Server, PowerShell 7, Herdr, and selected development tools inside the guest.
6. Verifies SSH and the guest Herdr server from the host.
7. When opted in, restores or enrolls and captures the stable tagged Tailscale identity over verified SSH.
8. Transfers selected Git, GitHub CLI, OpenCode, Herdr, and Windows Terminal configuration without staging credentials in run files.
9. Creates one Herdr workspace for each mapped project and attaches the host Herdr client.

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
- An existing standard `herdr.exe` from [`herdr-win`](https://github.com/hdosys/herdr-win) on `PATH`.
- Internet access for initial cache misses.
- Go 1.26.4 or newer to build this repository.

Enable Windows Sandbox from an elevated Windows PowerShell session if necessary:

```powershell
Enable-WindowsOptionalFeature -Online -FeatureName Containers-DisposableClientVM -All
```

Windows may require a restart after enabling the feature.

The application does not perform the initial host Herdr installation. Once `herdr.exe` exists on `PATH`, it verifies and, when necessary, atomically updates that existing command to the pinned `herdr-win` build used by the guest. Do not install Rust tooling on the host for this project.

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

Stops only the exact app-owned Sandbox after revalidating its process identity. For a ready guest with stable Tailscale identity enabled, it captures and verifies the current identity before requesting close; a capture failure leaves the guest open. It does not force-kill changed, unrelated, or unmanaged processes.

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
  "tailscale": false,
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
- `tailscale`: exact boolean opt-in for one stable tagged Tailscale device identity; omitted or `false` leaves Tailscale install-only.
- `workspaces`: additional workspace names mapped to absolute host project roots.
- `wingetPackages.remove`: known optional Base packages to omit.
- `wingetPackages.add`: exact extra WinGet package IDs.
- `wingetPackages.versions`: exact versions for retained or added packages.

The nearest active project profile is added automatically and deduplicated against global workspaces.

`base.ps1` is user-owned after it is first seeded and is not silently overwritten. If a newer binary reports an unsupported Base contract, merge the current repository `provisioning\base.ps1` changes into the global file before retrying.

### Optional stable Tailscale identity

> **Experimental:** the implementation is opt-in while the required two-fresh-Sandbox identity and peer-connectivity acceptance gate remains open.

Set `"tailscale": true`, leave `Tailscale.Tailscale` in the effective Base package plan, and ensure MagicDNS is enabled for the tailnet. In the Tailscale admin console, create one auth key that is:

- one-off, not reusable;
- non-ephemeral;
- pre-approved;
- assigned at least one server tag whose access policy is intentionally narrow.

Provide that key only to the first enrollment. This Windows PowerShell-compatible form avoids putting it in command history and removes the parent-shell copy after `up` returns:

```powershell
$keyPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR(
    (Read-Host 'One-off tagged Tailscale auth key' -AsSecureString)
)
try {
    $env:HERDR_SANDBOX_TAILSCALE_AUTH_KEY = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($keyPointer)
    herdr-sandbox up
} finally {
    Remove-Item Env:HERDR_SANDBOX_TAILSCALE_AUTH_KEY -ErrorAction SilentlyContinue
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($keyPointer)
}
```

Do not put the key in `config.json`, a command argument, or a provisioning script. The CLI removes its inherited copy before launching any child process and does not need the key again after enrollment. It stores the complete identity only as current-user DPAPI ciphertext under the host identity directory, restores it before later fresh guests become ready, and verifies the same device ID, node key, IPv4 address, MagicDNS name, fixed `herdr-sandbox` hostname, tags, and Windows Sandbox user SID. The protected identity is bound to the current Windows host user and is not a portable backup for another account or machine.

Tagged devices default to node-key expiry disabled; keep expiry disabled for this exact-identity path. A node-key rotation is treated as identity drift and fails closed rather than silently replacing the protected identity.

## Important paths

Host:

```text
%APPDATA%\herdr-sandbox\config.json       global settings
%APPDATA%\herdr-sandbox\base.ps1         editable global Base profile
%LOCALAPPDATA%\herdr-sandbox\identity\   SSH identity and DPAPI-protected Tailscale identity
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
- Tailscale auth-key and node-state bytes use only bounded verified SSH payloads; they never enter mappings, run status, diagnostics, command lines, or the package cache.
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

### Nightly checks and releases

GitHub Actions runs the same `check` task nightly and on manual request. It intentionally does not run on every push; run the checked build locally before pushing.

Early releases deliberately use `v0.0.N`, beginning with `v0.0.0`. `N` is a monotonically increasing release ID, not a semantic feature version: increment it by one for each manually created release and leave the first two components at zero.

To publish, start from a clean, synchronized `main` that has passed `check`, then push the next annotated tag:

```powershell
git tag -a v0.0.0 -m "v0.0.0"
git push origin v0.0.0
```

The release workflow accepts only that version shape, reruns `check`, packages the Sandbox executable with its required editable `base.ps1` and `stacks.ps1` provisioning assets in a Windows amd64 ZIP, writes its SHA-256 checksum, and publishes both with generated release notes. It never bundles `herdr.exe`; combined distribution belongs to the `herdr-win` repository. The tag owns the release ID, and no release is created by ordinary pushes or the nightly check.

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

### Stable Tailscale enrollment is refused

For the first enrollment, confirm that `tailscale` is exactly `true`, the Tailscale Base package was not removed, and `HERDR_SANDBOX_TAILSCALE_AUTH_KEY` contains one current one-off, non-ephemeral, pre-approved tagged auth key. Later runs deliberately refuse a missing, corrupt, differently DPAPI-bound, untagged, or identity-mismatched protected state instead of silently creating another tailnet device.

### Old run diagnostics consume space

Inspect the current owner first with `herdr-sandbox status`, then run `herdr-sandbox clean`. The command preserves the recorded active run and removes only strictly validated inactive run workspaces; it does not remove the persistent cache.

## More detail

- Stable user-visible behavior: [`PRODUCT.md`](PRODUCT.md)
- Technical boundaries: [`ARCHITECTURE.md`](ARCHITECTURE.md)
- Open work: [`BACKLOG.md`](BACKLOG.md)
- Agent/repository rules: [`AGENTS.md`](AGENTS.md)
