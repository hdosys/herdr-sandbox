# herdr-sandbox

**Move your agents into a disposable Windows environment—without rebuilding it by hand or living in RDP.**

[![Nightly checks](https://github.com/hdosys/herdr-sandbox/actions/workflows/nightly.yml/badge.svg)](https://github.com/hdosys/herdr-sandbox/actions/workflows/nightly.yml)
[![Release](https://github.com/hdosys/herdr-sandbox/actions/workflows/release.yml/badge.svg)](https://github.com/hdosys/herdr-sandbox/actions/workflows/release.yml)

`herdr-sandbox` is a Windows-native counterpart to a [dev container](https://containers.dev/). It maps only selected projects into a disposable Windows Sandbox, provisions their native toolchains, transfers approved configuration over verified SSH, starts Herdr in the guest, and attaches the normal host terminal. Source edits persist on the host; guest tools and processes disappear with the Sandbox.

Highlights:

- Project-owned, repeatable Windows PowerShell 5.1 provisioning.
- Persistent Herdr server with native terminal attach and reattach.
- Default-on configuration sync for OpenCode, Claude Code, Codex, GitHub Copilot CLI, and Pi.
- Optional [stable Tailscale tailnet identity](#stable-tailscale-tailnet-identity-experimental), giving approved phones, tablets, and laptops a stable private route to deliberately exposed guest services across fresh Sandboxes.
- Narrow project mappings, bounded status, verified downloads, and fail-closed lifecycle handling.

> **Safety:** this is practical isolation, not a complete security boundary. Selected projects remain writable and guest networking is enabled. The disposable guest profile also intentionally restricts protections including Defender cloud features, SmartScreen, and automatic Windows/driver updates. Keep backups and normal supply-chain controls.

## Quick start

Commands below assume `herdr-sandbox.exe` is on `PATH`; otherwise use its full `build\bin` path.

### Requirements

- Windows 10 or Windows 11 with hardware virtualization and Windows Sandbox support.
- Windows PowerShell 5.1, OpenSSH Client, Windows Terminal, and internet access for cache misses.
- Go 1.26.4 or newer to build this repository.
- An existing `herdr.exe` from the maintainer's [`herdr-win`](https://github.com/hdosys/herdr-win) fork on `PATH`.

Enable Windows Sandbox from an elevated Windows PowerShell session when necessary, then restart Windows:

```powershell
Enable-WindowsOptionalFeature -Online -FeatureName Containers-DisposableClientVM -All
```

**Herdr setup:** Install `herdr.exe` from [`herdr-win`](https://github.com/hdosys/herdr-win) once and make it available on `PATH`; `herdr-sandbox` does not perform that first installation. Later runs verify that the host and guest use the same pinned Herdr release and update the existing host executable when required.

### 1. Build

From the repository root:

```powershell
go run ./cmd/task check
```

The checked build writes `build\bin\herdr-sandbox.exe` beside its required app-owned `base.ps1` and `stacks.ps1` assets. Installing a newer release replaces those providers together.

### 2. Add a project profile

Create `<project>\.herdr-sandbox\provision.ps1`. Minimal Go example:

```powershell
param(
    [Parameter(Mandatory = $true)]
    [string]$ProjectDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0

Install-GoStack -ProjectDirectory $ProjectDirectory
```

The nearest ancestor containing this file becomes the active project. Profiles must be idempotent and fail fast.

### 3. Start from the project

```powershell
herdr-sandbox up
```

The visible PowerShell bootstrap console inside Windows Sandbox is intentional and requires no interaction. A successful run creates a usable guest workspace and attaches the host Herdr client—not merely SSH or an installed toolchain.

Automatic attach requires real console-backed stdin, stdout, and stderr. A redirected or headless caller fails explicitly, leaves the verified guest ready, and prints the reusable command instead of sending a TUI into logs:

```powershell
herdr --remote sandbox
```

### 4. Reattach

After a normal detach, the guest Herdr server remains running:

```powershell
herdr --remote sandbox
```

Plain SSH is available for noninteractive diagnostics:

```powershell
ssh sandbox
```

## Commands

| Command | Behavior |
| --- | --- |
| `herdr-sandbox up [--memory-mb MB] [--timeout DURATION]` | Launches a fresh guest, or re-runs current provisioning and reattaches when the exact ready app-owned launch plan still matches. There is no overall timeout by default; `--timeout` adds one. Changed workspace/cache mappings or memory require `down` first. |
| `herdr-sandbox status` | Reports `starting`, `ready`, `failed`, `stale`, `stopped`, or `unmanaged` without changing state. |
| `herdr-sandbox down` | Idempotently requests orderly close only for the exact revalidated app-owned Sandbox. It never force-kills any Sandbox; failed Tailscale preservation leaves an opted-in guest open. |
| `herdr-sandbox clean` | Removes only strictly validated inactive run workspaces. A valid active or stale recorded run, identity, SSH configuration, and package caches are preserved. |

`up` refuses starting, failed, stale, changed-plan, and unmanaged instances rather than guessing how to reuse them. Inspect with `status`, then use `down` when the recorded app-owned state can be safely closed.

## Customize the guest

### Project profiles

Profiles call built-in stacks directly so the host can inspect requirements without executing project code:

| Development need | Direct profile call |
| --- | --- |
| Go | `Install-GoStack -ProjectDirectory $ProjectDirectory` |
| Node.js LTS | `Install-NodeStack` |
| Python (latest stable) | `Install-PythonStack` |
| Zig | `Install-ZigStack` |
| Rust with MSVC Build Tools | `Install-RustMSVCStack -ProjectDirectory $ProjectDirectory` |
| Cargo Nextest | `Install-CargoNextest` |
| Just | `Install-Just` |

Keep these calls direct—not behind aliases, dynamic invocation, or another dot-sourced file. Exact parameters and optional version selectors live in [`provisioning\stacks.ps1`](provisioning/stacks.ps1). With no version, a development stack resolves the latest stable release once and carries that concrete identity through cache, installation, and verification. An explicit version remains exact and fails instead of silently falling back. Release/bootstrap artifacts such as WinGet, Herdr, OpenSSH, VC prerequisites, and the GeistMono payload remain application-release pins. TypeScript and other application libraries remain project dependencies owned by their manifests and lockfiles.

For a project-specific tool, add idempotent Windows PowerShell 5.1 to its profile. For a package needed in every guest, use [`wingetPackages.add`](#global-configuration). There is intentionally no plugin registry.

**Rust/MSVC note:** the first Rust-stack run may show Microsoft Visual Studio Installer on the host. The signed bootstrapper creates a verified Build Tools layout in the app-owned cache; it does not install Visual Studio or Rust on the host. The guest copies and installs from that layout.

### Global configuration

The first run creates:

```text
%APPDATA%\herdr-sandbox\config.json
%APPDATA%\herdr-sandbox\user.ps1
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

| Field | Meaning |
| --- | --- |
| `cacheDirectory` | Absolute persistent package/tool cache. Empty uses `<system-temp>\herdr-sandbox\cache`. It must not overlap a workspace or app run state. |
| `memoryMB` | Default Sandbox memory; minimum 2048. `--memory-mb` overrides one run. |
| `tailscale` | Exact boolean opt-in for the stable tagged identity. Omitted or `false` leaves Tailscale install-only. |
| `codingAgentSync` | Five exact booleans; all default to `true`. Set one to `false` to skip that agent. |
| `workspaces` | Additional unique workspace names mapped to absolute host project roots. |
| `wingetPackages.remove` | Known optional Base packages to omit. Core packages cannot be removed. |
| `wingetPackages.add` | Exact additional WinGet package IDs installed in every guest. |
| `wingetPackages.versions` | Exact versions for retained or added packages. Omitted versions resolve latest; unavailable exact versions fail. |

`config.json` is strict JSON, so comments are not allowed. To install every coding agent that currently has a verified WinGet package, set `wingetPackages` to this copy-paste object:

```json
{
  "remove": [],
  "add": [
    "Anthropic.ClaudeCode",
    "OpenAI.Codex",
    "GitHub.Copilot"
  ],
  "versions": {}
}
```

OpenCode (`SST.opencode`) is already a Base default and must not be added again. Pi does not currently have a verified WinGet package; install it explicitly in the project profile that needs it. `codingAgentSync` controls configuration/authentication transfer only and does not install an agent.

The active project is added automatically and deduplicated against global workspaces. Workspace paths must exist, must not overlap, and must not contain reparse aliases.

`base.ps1` and `stacks.ps1` are release-owned provider/adapter code and update with the application. Put idempotent global PowerShell additions in the seeded-once `user.ps1`; it runs after app-owned helpers are ready and before project profiles. Keep package selection in `config.json` and project-specific behavior in the project profile. Do not store credentials or print secrets from `user.ps1`, because its immutable run snapshot remains with diagnostics until `clean`.

Older releases seeded a user-owned `%APPDATA%\herdr-sandbox\base.ps1`. The new ownership model never overwrites or executes that file: `up` stops with migration instructions. Review it, move only deliberate global extension commands into `user.ps1`, move package choices into `config.json`, keep project tools in project profiles, archive the complete legacy Base under a non-reserved name, and retry.

Persistent host state is split intentionally:

| Path | Contents |
| --- | --- |
| `%APPDATA%\herdr-sandbox` | User-owned global config and `user.ps1` extension. |
| `%LOCALAPPDATA%\herdr-sandbox\identity` | Host SSH identity and optional DPAPI-protected Tailscale identity. |
| `%LOCALAPPDATA%\herdr-sandbox\runs` | Per-run status, diagnostics, SSH material, and `.wsb` files. Do not edit an active run. |
| `<system-temp>\herdr-sandbox\cache` | Default persistent package/tool cache. |

### Coding-agent sync

Configuration sync is default-on when these host surfaces exist:

| Agent | Configuration/authentication behavior |
| --- | --- |
| OpenCode | Copies approved config/data files and portable `auth.json`. |
| Claude Code | Copies approved `.claude` configuration and `.credentials.json`. |
| Codex | Copies approved `CODEX_HOME` content and file-mode credentials. Keyring credentials stay host-bound. |
| GitHub Copilot CLI | Copies approved config and reuses successfully imported GitHub CLI accounts. Native Credential Manager tokens stay host-bound. |
| Pi | Copies approved agent configuration and portable `auth.json`. |

The shared `%USERPROFILE%\.agents\skills` tree is copied once when Codex, Copilot, or Pi is enabled. Conversations, history, logs, caches, generated plugin/package state, project trust, private SSH/GPG keys, and unrelated home content are excluded. Missing host configuration is a clean no-op. This feature copies setup only; it does not install Claude Code, Codex, Copilot, or Pi.

## Stable Tailscale tailnet identity (experimental)

> The required two-fresh-Sandbox identity and peer-connectivity acceptance gate remains open. Use this opt-in only with a tailnet prepared for a dedicated tagged device.

`herdr-sandbox` joins an existing user-owned tailnet; it does not create the tailnet or manage its policy. The stable address lets an approved phone, tablet, or another computer reach services in the running Sandbox without publishing them to the internet. The web admin console remains available. Installation only suppresses automatically opening the guest tray app; noninteractive DPAPI protection does not disable the Tailscale UI. Leave `"tailscale": false` if you prefer a manually managed disposable login; that identity will not be preserved.

Tailscale supplies the private network path, not a terminal UI or automatic service authorization. To control the guest from a phone, install Tailscale plus a compatible SSH/terminal client on the phone and deliberately authorize that client's public key in guest provisioning—or expose another authenticated guest service. The default OpenSSH endpoint authorizes only the app-owned host-side client identity; never copy that private key to the phone.

<details>
<summary><strong>First-time tailnet and enrollment setup</strong></summary>

### 1. Prepare the tailnet

In the [Tailscale admin console](https://login.tailscale.com/admin):

1. Create or open the target tailnet and enable MagicDNS.
2. Add a dedicated tag with least-privilege access.
3. Merge policy entries such as the following; do not replace unrelated policy:

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

Replace the source and ports with the users, groups, and services actually required. See Tailscale's [tag](https://tailscale.com/docs/features/tags) and [access-control](https://tailscale.com/docs/features/access-control) documentation.

### 2. Create the one-time key

Create one auth key on the admin console's Keys page:

- **Reusable:** off
- **Ephemeral:** off
- **Pre-approved/pre-authorized:** on when device approval is enabled
- **Tags:** `tag:herdr-sandbox`

If Tailnet Lock is enabled, sign the key from an existing trusted node first. Never put it in `config.json`, provisioning, shell history, or an argument to `herdr-sandbox up` or `tailscale up`.

### 3. Enable and enroll

Set `"tailscale": true` in `%APPDATA%\herdr-sandbox\config.json`. A minimal `{ "tailscale": true }` file is valid; before the first-ever `up`, create that file with an editor. Keep `Tailscale.Tailscale` in the Base package plan.

From the intended project directory, pass the key once without putting it in command history:

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

The CLI removes its inherited environment copy before launching children, enrolls fixed hostname `herdr-sandbox`, verifies the tagged identity, and stores the complete node state only as current-user DPAPI ciphertext. Confirm exactly one tagged device in the admin console and verify the private route from the phone or another intended peer with `tailscale ping herdr-sandbox`; service login still requires the separately authorized credential described above.

### 4. Later Sandboxes

Do not supply the auth key again. `down` captures and verifies current state before closing; the next fresh `up` restores it over verified SSH. Device ID, node key, IPv4, MagicDNS, hostname, tags, and Windows Sandbox user SID must remain exact or the workflow fails closed.

Keep node-key expiry disabled. Do not delete the tailnet device or `%LOCALAPPDATA%\herdr-sandbox\identity\tailscale-identity.json` while expecting restoration. The protected identity is bound to the current Windows host user and is not a portable backup.

</details>

## Security boundaries

- Writable host mappings are limited to selected project roots, the explicit package/tool cache, and bounded per-run status; networking remains enabled.
- The host home directory, general AppData, unrelated repositories, and private SSH/GPG keys are never mapped; only the app-owned public SSH key enters the guest.
- Approved GitHub CLI and coding-agent credentials travel only over verified SSH and never enter persistent run input or logs. Machine-bound credentials require a guest login.
- Tailscale auth-key and state bytes never enter mappings, status, diagnostics, command lines, or package cache.
- Guest OpenCode managed policy resolves every permission to `allow`; host OpenCode policy is unchanged. Treat guest agents as fully authorized inside the Sandbox and mapped projects.
- The reviewed disposable-guest privacy profile intentionally restricts Defender cloud/security features, SmartScreen, automatic updates, telemetry, and related services. It is not a hardened production workstation profile.
- Downloads and cache hits are validated against strict versions, metadata, hashes, signatures, or package identity as applicable.
- Host Rust tooling is forbidden. Rust installation, builds, and tests belong only in the verified guest or GitHub Actions.
- Lifecycle commands revalidate exact app-owned process/path identity and refuse unrelated, changed, or reparse-bearing state.

## Troubleshooting

Start with `herdr-sandbox status`; it is read-only.

| Symptom | Action |
| --- | --- |
| `up` refuses an existing Sandbox | A ready exact guest is reused automatically. For failed, stale, changed-plan, or unmanaged state, inspect the reported owner and use `herdr-sandbox down` only when it identifies the app-owned instance. |
| Automatic attach fails in a headless process | Open a real terminal and run `herdr --remote sandbox`; the verified guest remains ready. |
| `ssh sandbox` no longer connects | A `stale` status means the recorded process ended. Use `down` to clear safe stale ownership, then start again. |
| Legacy global Base is refused | Preserve `%APPDATA%\herdr-sandbox\base.ps1`, move only deliberate additions to `user.ps1`/config/project ownership, archive the legacy file under a non-reserved name, and retry. |
| Initial provisioning is slow | The first run may download WinGet, Herdr/OpenSSH, Rust, and Visual Studio layout payloads. Confirm that the cache is writable and does not overlap a workspace or run state. |
| Stable Tailscale enrollment is refused | Confirm exact `true`, the retained Tailscale package, and a current one-time non-ephemeral pre-approved tagged key. Restoration refuses missing, corrupt, differently DPAPI-bound, untagged, or identity-mismatched state. |
| Old diagnostics consume space | Run `clean`; it preserves active ownership and the persistent cache while removing only validated inactive run workspaces. |

## Development

Repository tasks:

```powershell
go run ./cmd/task fmt
go run ./cmd/task test
go run ./cmd/task build
go run ./cmd/task check
```

`check` covers Go formatting, Windows PowerShell 5.1 parsing, all Go tests, `go vet`, and the stable `build\bin` artifact. Repository-owned provisioning runs exclusively under Windows PowerShell 5.1; installed PowerShell 7 is interactive guest tooling.

## Documentation

- Stable user-visible behavior: [`PRODUCT.md`](PRODUCT.md)
- Technical boundaries: [`ARCHITECTURE.md`](ARCHITECTURE.md)
- Open work: [`BACKLOG.md`](BACKLOG.md)
- Project-specific agent/repository rules: [`AGENTS.md`](AGENTS.md)
