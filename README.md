# herdr-sandbox

**Move your coding-agent setup into a disposable Windows environment—without rebuilding it by hand or living in RDP.**

[![Nightly checks](https://github.com/hdosys/herdr-sandbox/actions/workflows/nightly.yml/badge.svg)](https://github.com/hdosys/herdr-sandbox/actions/workflows/nightly.yml)
[![Release](https://github.com/hdosys/herdr-sandbox/actions/workflows/release.yml/badge.svg)](https://github.com/hdosys/herdr-sandbox/actions/workflows/release.yml)

Think of `herdr-sandbox` as a Windows-native cousin of a [dev container](https://containers.dev/): a repeatable, project-defined development environment, but backed by a real disposable Windows guest rather than a Linux container.

Container-first tools make isolated Linux development straightforward. Native Windows work often leaves a worse choice: give agents, installers, and package scripts access to the host (or answer constant permission prompts), or maintain a full VM and drive it over RDP.

`herdr-sandbox up` closes that gap. It maps only the projects you select, provisions their Windows toolchains, transfers selected configuration and authentication over verified SSH, starts Herdr in the guest, and connects your normal terminal. Source edits persist on the host; guest tools and processes can be discarded without an uninstall ritual. Private SSH keys, unrelated repositories, the host home directory, and general AppData stay out of the mapping.

This is a contract-driven product rather than a loose bootstrap script: versioned provisioning contracts, validated mappings and downloads, bounded phases, explicit status, and fail-closed handoffs make runs repeatable and failures diagnosable.

**Agent support:** OpenCode, Claude Code, Codex, GitHub Copilot CLI, and Pi configuration are copied automatically when present. Portable saved credentials come with them; Copilot reuses the transferred GitHub CLI login. Machine-bound Windows credentials still require one guest login. This copies setup only—the four additional agent CLIs are installed separately or by a project profile.

**Stable Tailscale identity (experimental):** Opt in once with a tagged auth key and later fresh Sandboxes restore the same tailnet device, node key, IPv4 address, and MagicDNS name without another login. The tailnet and its access policy remain user-owned in Tailscale's web admin console. See [Stable Tailscale tailnet identity](#stable-tailscale-tailnet-identity-experimental) before the first opted-in `up`.

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
8. Transfers selected Git, GitHub CLI, coding-agent, Herdr, and Windows Terminal configuration without staging credentials in run files.
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
  "codingAgentSync": {
    "opencode": true,
    "claudeCode": true,
    "codex": true,
    "githubCopilot": true,
    "pi": true
  },
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
- `codingAgentSync`: per-agent configuration/authentication copying. All five fields default to `true`; set one to `false` to opt out.
- `workspaces`: additional workspace names mapped to absolute host project roots.
- `wingetPackages.remove`: known optional Base packages to omit.
- `wingetPackages.add`: exact extra WinGet package IDs.
- `wingetPackages.versions`: exact versions for retained or added packages.

Agent sync uses fixed portable surfaces rather than copying whole home directories:

| Agent | Host source | Authentication copied |
| --- | --- | --- |
| OpenCode | `XDG_CONFIG_HOME\opencode` and `XDG_DATA_HOME\opencode` | `auth.json` |
| Claude Code | `CLAUDE_CONFIG_DIR` or `%USERPROFILE%\.claude` | `.credentials.json` |
| Codex | `CODEX_HOME` or `%USERPROFILE%\.codex` | File-mode `auth.json` and `.credentials.json` |
| GitHub Copilot CLI | `COPILOT_HOME` or `%USERPROFILE%\.copilot` | Reuses imported GitHub CLI accounts |
| Pi | `PI_CODING_AGENT_DIR` or `%USERPROFILE%\.pi\agent` | `auth.json` |

The standard `%USERPROFILE%\.agents\skills` tree is shared by Codex, Copilot, and Pi. Copilot authentication reuse requires the default `GitHub.cli` Base package and at least one successfully imported GitHub CLI account. Sessions, history, logs, caches, generated package/plugin state, and project trust are excluded. Codex encrypted/keyring credentials and Copilot-native Windows Credential Manager tokens are machine-bound and must be authenticated once inside the guest. Sandbox copies these agents' setup but does not install their executables.

The four new integrations are implemented from current official source/documentation and covered by local archive, security, and Windows PowerShell 5.1 contract tests. A fresh-Sandbox live copy/login/read-back pass is still pending manual verification.

The nearest active project profile is added automatically and deduplicated against global workspaces.

`base.ps1` is user-owned after it is first seeded and is not silently overwritten. If a newer binary reports an unsupported Base contract, merge the current repository `provisioning\base.ps1` changes into the global file before retrying.

## Stable Tailscale tailnet identity (experimental)

> **Experimental:** the implementation is opt-in while the required two-fresh-Sandbox identity and peer-connectivity acceptance gate remains open.

`herdr-sandbox` joins an existing user-owned tailnet; it does not create a tailnet, change its DNS settings, or invent an access policy. Initial administration happens in the [Tailscale admin console](https://login.tailscale.com/admin), not in the disposable guest.

The Tailscale UI is not removed or globally disabled. The MSI's `TS_NOLAUNCH` setting only suppresses automatically opening the guest tray application after installation, and unattended enrollment keeps the service usable without a GUI session. `CRYPTPROTECT_UI_FORBIDDEN` applies only to background DPAPI calls and prevents Windows credential prompts; it does not affect Tailscale. The web admin console remains available. If you prefer an interactive, disposable Tailscale login, leave `"tailscale": false` and launch the installed guest application yourself; `herdr-sandbox` will not preserve that manually managed identity.

### 1. Prepare the tailnet

In the Tailscale admin console:

1. Sign in to create or open the tailnet that should own the Sandbox device.
2. Enable MagicDNS under DNS settings.
3. Define a dedicated tag and only the access that the Sandbox requires.

For example, merge the following entries into the existing tailnet policy; do not replace unrelated policy rules. This example lets only tailnet administrators reach SSH on the Sandbox:

```json
{
  "tagOwners": {
    "tag:herdr-sandbox": ["autogroup:admin"]
  },
  "acls": [
    {
      "action": "accept",
      "src": ["autogroup:admin"],
      "dst": ["tag:herdr-sandbox:22"]
    }
  ]
}
```

Replace the source with the intended user or group and add only required ports. See Tailscale's [tag](https://tailscale.com/docs/features/tags) and [access-control](https://tailscale.com/docs/features/access-control) documentation for an existing production policy.

### 2. Create the one-time auth key

Create one auth key from the admin console's Keys page with:

- **Reusable:** off;
- **Ephemeral:** off;
- **Pre-approved/pre-authorized:** on when device approval is enabled;
- **Tags:** `tag:herdr-sandbox`.

Use the key for only this enrollment. If Tailnet Lock is enabled, sign the pre-approved key from an existing trusted node as described in the [Tailnet Lock documentation](https://tailscale.com/docs/features/tailnet-lock) before continuing. Do not put the key in `config.json`, a provisioning script, shell history, or an argument to `herdr-sandbox up` or `tailscale up`.

### 3. Enable the feature

In `%APPDATA%\herdr-sandbox\config.json`, set:

```json
{
  "tailscale": true
}
```

The minimal object above is valid because omitted settings retain their defaults. If this is the very first `up`, create the directory and file with an editor before starting; otherwise edit the file already seeded by the CLI. Leave `Tailscale.Tailscale` in the effective Base package plan.

### 4. Run the first enrollment

From the intended project directory, use this Windows PowerShell-compatible form so the key does not enter command history. Replace the executable path with the checked build or installed command you use:

```powershell
$herdrSandbox = 'D:\path\to\herdr-sandbox\build\bin\herdr-sandbox.exe'
$keyPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR(
    (Read-Host 'One-off tagged Tailscale auth key' -AsSecureString)
)
try {
    $env:HERDR_SANDBOX_TAILSCALE_AUTH_KEY = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($keyPointer)
    & $herdrSandbox up
} finally {
    Remove-Item Env:HERDR_SANDBOX_TAILSCALE_AUTH_KEY -ErrorAction SilentlyContinue
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($keyPointer)
}
```

The CLI removes its inherited environment copy before launching child processes, enrolls the fixed `herdr-sandbox` hostname without opening the guest UI, verifies the tagged running identity, and stores the complete state only as current-user DPAPI ciphertext under `%LOCALAPPDATA%\herdr-sandbox\identity`.

After readiness, confirm in the admin console that exactly one tagged `herdr-sandbox` device exists with the expected IP and MagicDNS name. From an allowed peer, verify reachability with:

```powershell
tailscale ping herdr-sandbox
```

### 5. Use later Sandboxes

Do not set `HERDR_SANDBOX_TAILSCALE_AUTH_KEY` again. Normal lifecycle commands are enough:

```powershell
& 'D:\path\to\herdr-sandbox\build\bin\herdr-sandbox.exe' down
& 'D:\path\to\herdr-sandbox\build\bin\herdr-sandbox.exe' up
```

`down` captures and verifies the current state before closing a ready opted-in guest. A later fresh `up` decrypts the host copy and restores it over verified SSH before the guest becomes ready. It fails closed if the device ID, node key, IPv4 address, MagicDNS name, fixed hostname, tags, or Windows Sandbox user SID changes.

Keep node-key expiry disabled for this tagged device. Do not delete the tailnet device or `%LOCALAPPDATA%\herdr-sandbox\identity\tailscale-identity.json` while expecting restoration. The DPAPI-protected identity is bound to the current Windows host user and is not a portable backup for another account or machine.

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
- Approved coding-agent and GitHub CLI credentials are streamed only through the verified SSH channel and are not written to host run state. Codex/Copilot machine-bound keyring entries are never scraped.
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
