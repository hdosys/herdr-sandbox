# herdr-sandbox

**Run coding agents in a disposable, native Windows development environment—without RDP, broad home-directory mounts, or host toolchain drift.**

[![Nightly checks](https://github.com/hdosys/herdr-sandbox/actions/workflows/nightly.yml/badge.svg)](https://github.com/hdosys/herdr-sandbox/actions/workflows/nightly.yml) [![Release](https://github.com/hdosys/herdr-sandbox/actions/workflows/release.yml/badge.svg)](https://github.com/hdosys/herdr-sandbox/actions/workflows/release.yml) [![Go 1.26.4](https://img.shields.io/badge/Go-1.26.4-00ADD8?logo=go&logoColor=white)](go.mod) ![Windows Sandbox](https://img.shields.io/badge/platform-Windows%20Sandbox-0078D4?logo=windows11&logoColor=white) [![License: Apache 2.0](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

`herdr-sandbox` is a Windows-native counterpart to a [dev container](https://containers.dev/). It launches Windows Sandbox with only the selected projects, provisions native toolchains, transfers approved agent configuration over verified SSH, starts Herdr in the guest, and attaches the normal host terminal. Source edits persist on the host; guest tools and processes disappear with the Sandbox.

> [!NOTE]
> Automated checks and opt-in native acceptance gates cover the core path. Host policy, networking, upstream tools, and Windows platform changes can still affect operation.

[How it works](#how-it-works) · [Deployment time](#deployment-time) · [Engineering](#engineering-approach) · [Get started](#get-started) · [Commands](#commands) · [Configuration](#configuration) · [Security](#security-boundaries) · [Security policy](SECURITY.md) · [Changelog](CHANGELOG.md) · [Tailscale](#stable-tailscale-tailnet-identity-experimental) · [Troubleshooting](#troubleshooting) · [Development](#development)

## How it works

```mermaid
flowchart LR
    Host["Host terminal<br/>herdr-sandbox (Go)"]
    Projects[("Selected projects")]
    Config["Approved agent config"]

    subgraph Guest["Disposable Windows Sandbox"]
        Provision["PowerShell 5.1<br/>provisioning"]
        Agents["Agents + native<br/>toolchains"]
        Herdr["Herdr server"]
    end

    Host -->|launch + lifecycle| Provision
    Projects <-->|narrow writable mappings| Agents
    Config -->|verified SSH only| Agents
    Provision --> Agents --> Herdr
    Host <-->|console-backed attach| Herdr
```

The host owns source, identity, configuration, cache, and bounded run evidence. The guest owns compilation, agent execution, and disposable runtime state. Go owns host lifecycle decisions and strict boundary contracts; PowerShell is the narrow Windows provisioning adapter.

> [!IMPORTANT]
> This is practical isolation, not a complete security boundary. Selected projects remain writable and guest networking is enabled. The disposable profile also intentionally restricts protections including Defender cloud features, SmartScreen, and automatic Windows/driver updates. Keep backups and normal supply-chain controls.

## Key capabilities

- **Native Windows isolation:** real Windows toolchains run inside Windows Sandbox instead of a compatibility layer.
- **Terminal-first workflow:** Herdr provides native attach and reattach from the host terminal; routine work does not require RDP.
- **Multi-stack provisioning:** .NET 10, Go, Node.js with Playwright Chromium, Python, Rust/MSVC, and Zig share one idempotent project-profile model.
- **Agent-ready guests:** approved configuration for OpenCode, Claude Code, Codex, GitHub Copilot CLI, and Pi is synchronized over verified SSH.
- **Fast iteration:** an exact ready guest can be reprovisioned and reattached without replacing it.
- **Narrow persistence:** selected source trees and a verified package cache survive; the guest operating system, tools, and processes do not.
- **Stable private reachability with Tailscale (experimental):** opt in to preserve one tagged guest identity, Tailscale IP, and MagicDNS name across fresh Sandboxes without exposing services publicly.

## Deployment time

There is no separate VM to set up or keep updated. With downloads cached, a fresh Sandbox is usually ready in **2–4 minutes** for projects without Rust/MSVC and **4–6 minutes** when Rust/MSVC is included.

Our full compatibility test installs all supported stacks in one Sandbox: **.NET, Go, Node.js with Playwright Chromium, Python, Rust/MSVC, and Zig**. A first run can take longer because browser and Visual Studio payloads must be downloaded. Times vary by machine and network; attaching to an already ready Sandbox skips provisioning.

## Engineering approach

- **Native Windows architecture:** a standard-library-first Go control plane owns the product while PowerShell 5.1 remains a narrow Windows provisioning adapter; no CGO, helper runtime, daemon, provider framework, or alternate provisioner is required.
- **Explicit responsibility boundaries:** CLI, configuration, lifecycle, SSH, cleanup, packaging, and user/project extensions each have one owner instead of parallel implementations.
- **Defensive process and state handling:** subprocesses propagate cancellation, use focused timeouts, hide noninteractive console trees, publish state atomically, and return bounded diagnostics.
- **Strict external contracts:** JSON, XML, status, process identity, paths, downloads, release artifacts, and installer state are validated before use; uncertain destructive operations fail closed.
- **Reproducible provisioning:** exact versions, hashes, signatures, and realized state are verified where applicable; repeat runs avoid duplicate work and read back every change.
- **Release engineering:** the installer and portable ZIP share one four-file payload, checksums, deterministic ZIP output, rollback-aware upgrades, and explicit uninstall ownership.
- **Production-path verification:** focused tests, PowerShell parse checks, `go vet`, stable builds, package checks, and opt-in real Windows Sandbox all-stack acceptance exercise the same implementation shipped to users.

## Get started

### Prerequisites

- Windows 10 or Windows 11 with hardware virtualization and Windows Sandbox support.
- Windows Terminal and internet access for cache misses.
- An existing Windows `herdr.exe` with working `--remote` support on host `PATH`.
- Go 1.26.4 or newer only when building this repository from source.

If Windows Sandbox is not enabled, run the following from elevated Windows PowerShell and restart Windows:

```powershell
Enable-WindowsOptionalFeature -Online -FeatureName Containers-DisposableClientVM -All
```

[`herdr-win`](https://github.com/hdosys/herdr-win) currently provides a Windows build with remote attach, but `herdr-sandbox` does not require that fork, package, installer, or managed layout. It capability-checks the `herdr.exe` already on `PATH`, copies the status-reported physical executable and optional complete ConPTY bundle into each fresh guest, and never installs, updates, or replaces host Herdr.

### Install herdr-sandbox

Every [GitHub release](https://github.com/hdosys/herdr-sandbox/releases/latest) provides a per-user installer, a portable ZIP, and matching `.sha256` sidecars. Both formats contain exactly `herdr-sandbox.exe`, `base.ps1`, `stacks.ps1`, and `LICENSE.txt`.

#### Installer (recommended)

Download `herdr-sandbox_<version>_windows_amd64_setup.exe` and its `.sha256` from the latest release, verify the checksum, and run setup. It needs no administrator access, installs for the current user, adds the application to Windows Installed Apps and user `PATH`, and never launches a program or browser automatically.

> [!WARNING]
> The installer path is currently unsigned, so Windows may display a SmartScreen warning. Use it only after its SHA-256 matches the sidecar from the same release.

<details>
<summary><strong>Installer ownership and uninstall behavior</strong></summary>

- Installs to `%LOCALAPPDATA%\Programs\Herdr Sandbox` and upgrades the four packaged files as one rollback-aware set. A later seed, uninstaller, registration, or PATH failure restores the prior files, uninstaller, version/PATH ownership, and any PATH entry added by that attempt.
- Creates `config.json` and `user.ps1` only when absent; setup and upgrades never replace existing user settings.
- Adds only its own user `PATH` entry. A matching entry that existed before setup remains user-owned.
- Never bundles Herdr/Herdr-Win, agents, an updater, runtime bundles, or Windows prerequisites.
- Uninstall from **Settings → Apps → Installed apps**, or run `%LOCALAPPDATA%\Programs\Herdr Sandbox\uninstall.exe`.
- Uninstall never stops a running Sandbox. It removes app-owned runtime state, SSH integration, cache, application files, registration, and installer-owned `PATH`; a running Sandbox remains open but becomes unmanaged and must be closed manually.
- **Also delete config.json and user.ps1** is off by default, so settings survive reinstall unless you explicitly select deletion.
- Project profiles and unrelated SSH/install-directory content remain untouched. Uncertain ownership aborts before application removal.

</details>

#### WinGet

The Sandbox package ID is `hdosys.herdr-sandbox`. Its first community manifest is tracked in [microsoft/winget-pkgs#410501](https://github.com/microsoft/winget-pkgs/pull/410501). Once the public source resolves the package, install it with:

```powershell
winget install --id hdosys.herdr-sandbox --exact
```

Herdr-Win remains a separate package and is never bundled or declared as a dependency.

#### Portable ZIP

Download `herdr-sandbox_<version>_windows_amd64.zip` and its `.sha256`, verify the checksum, and extract all four files into one directory. Keep the three support files beside `herdr-sandbox.exe`, then run `.\herdr-sandbox.exe` or add that directory to user `PATH`.

#### Build from source

From the repository root:

```powershell
go run ./cmd/task check
```

The checked build writes the same four files to `build\bin`. Use that executable directly or add the directory to user `PATH`.

### Launch your first project

Commands below assume `herdr-sandbox.exe` is on `PATH`; otherwise use its full path.

#### 1. Initialize a project profile

From the project root, select one or more stacks explicitly:

```powershell
herdr-sandbox init --stack go
```

For an official Herdr upstream checkout without a project profile, select its maintained virtual stack directly:

```powershell
herdr-sandbox init --stack herdr
```

Repeat `--stack` to combine `dotnet`, `go`, `herdr`, `node`, `python`, `rust`, and `zig`, or omit the flag for a guided prompt. The virtual `herdr` choice already includes Python with the repository-required `python3` command, Rust/MSVC, Zig, Bun, Cargo Nextest, Just, and Base Git for Windows `sh`, so it cannot be combined with its `python`, `rust`, or `zig` constituents. `init` validates every selection, writes one direct-call `.herdr-sandbox\provision.ps1`, and never replaces an existing or ancestor-owned profile. The nearest ancestor containing that file becomes the active project.

#### Optional: Inspect the effective plan

```powershell
herdr-sandbox plan
```

`plan` prints the validated configuration, workspaces, stacks, packages, agent-sync choices, fixed Sandbox settings, and differences from a ready guest. It does not create state, download packages, update tools, consume a Tailscale key, or execute project scripts.

#### 2. Start from the project

```powershell
herdr-sandbox up
```

The visible PowerShell bootstrap console inside Windows Sandbox is intentional and requires no interaction. A successful run creates a usable guest workspace and attaches the host Herdr client—not merely SSH or an installed toolchain.

> [!NOTE]
> Automatic attach requires real console-backed stdin, stdout, and stderr. A redirected or headless caller is rejected before cleanup or provisioning instead of sending a TUI into logs. Use the intentional headless path:

```powershell
herdr-sandbox up --no-attach
```

#### 3. Reattach

After a normal detach, the guest Herdr server remains running:

```powershell
herdr-sandbox attach
```

The verified `herdr --remote sandbox` alias remains available for direct Herdr use.

Plain SSH is available for noninteractive diagnostics:

```powershell
ssh sandbox
```

## Commands

Command output is plain and redirect-safe: summaries use descriptive headings, indented fields, deterministic ordering, and one item per line instead of packed comma-separated lists. Results go to stdout, errors go to stderr, and no color or terminal UI framework is required.

| Command | Behavior |
| --- | --- |
| `herdr-sandbox config` | Creates `config.json` when absent and opens it with the application registered for `.json` files. Existing configuration is never replaced. |
| `herdr-sandbox version` | Prints the embedded application version and abbreviated source revision, or explicitly reports an unknown development revision. |
| `herdr-sandbox plan` | Prints the validated effective plan and differences from a ready guest without changing state. |
| `herdr-sandbox init [--stack NAME]...` | Creates one direct-call project profile. With no flag, prompts for stacks; existing or ancestor-owned profiles are never replaced. |
| `herdr-sandbox up [--memory-mb MB] [--timeout DURATION] [--no-attach]` | Launches and provisions a guest, or reprovisions an exact matching ready guest. It attaches unless `--no-attach` stops at terminal ready; no overall timeout applies unless requested. |
| `herdr-sandbox attach` | Verifies and attaches to the ready guest without reprovisioning. |
| `herdr-sandbox status` | Reports guest health, operation progress, workspaces, versions, timings, diagnostics, warnings, and the next action. |
| `herdr-sandbox down` | Stops only the revalidated app-owned Sandbox. If opted-in Tailscale state cannot be preserved, the guest remains running. |
| `herdr-sandbox clean` | Removes only validated inactive run workspaces while preserving active or uncertain state, configuration, projects, and cache. |

<details>
<summary><strong>Lifecycle and automatic cleanup</strong></summary>

- After valid command syntax, `up`, `status`, and `down` run the same bounded cleanup; `clean` invokes that owner directly. Help, `plan`, and invalid command lines remain nonmutating.
- Cleanup clears stale run and SSH state only when process evidence proves no Sandbox launcher or client remains. Changed, unmanaged, reparse-bearing, or uncertain state is preserved and reported.
- A freely acquired lifecycle lock records an abandoned retained operation as interrupted before another mutation can begin.
- `up` reuses only an exact ready app-owned instance. Inspect refused state with `status`, then use `down` only when the CLI identifies the app-owned guest.
- Changing Tailscale, audio, memory, cache, folder mounts, or workspace mappings requires `down` before the next `up`.

</details>

## Configuration

### Project profiles

Profiles call built-in stacks directly so the host can inspect requirements without executing project code:

| Development need | Direct profile call |
| --- | --- |
| Herdr/Herdr-Win development | `Install-HerdrStack -ProjectDirectory $ProjectDirectory` |
| Modern .NET 10 LTS SDK | `Install-DotNetStack` |
| Go | `Install-GoStack -ProjectDirectory $ProjectDirectory` |
| Node.js LTS with Playwright Chromium | `Install-NodeStack` |
| Bun | `Install-BunStack` |
| Python (latest stable) | `Install-PythonStack` |
| Zig | `Install-ZigStack` |
| Rust with MSVC Build Tools | `Install-RustMSVCStack -ProjectDirectory $ProjectDirectory` |
| Cargo Nextest | `Install-CargoNextest` |
| Just | `Install-Just` |

- Keep stack calls direct—not behind aliases, dynamic invocation, or another dot-sourced file. Exact parameters and optional version selectors live in [`provisioning\stacks.ps1`](provisioning/stacks.ps1).
- `Install-HerdrStack` is one virtual composition, not another package provider. It reuses the standard Python, Zig, Rust/MSVC, Bun, Cargo Nextest, and Just stacks; Herdr constrains Python to 3.13 and Zig to 0.15.2, while the standard Rust/MSVC stack honors the checkout's `rust-toolchain.toml`. The Python stack supplies the verified `python3` command, and conditional Base Git exposes and verifies its own `sh.exe`; a Herdr plan fails early if `Git.Git` was removed. Bun remains latest stable unless explicitly versioned. Herdr itself owns only its composition and libghostty's guest-local Zig output state. `plan` expands the composition back into those concrete owners without executing the profile.
- An omitted version always resolves the latest stable release once for installation and verification. Playwright resolves npm's current `latest` dist-tag on every provisioning run; only `Install-NodeStack -PlaywrightVersion <x.y.z>` requests an exact version. Exact versions never fall back silently.
- Built-in stacks own toolchains rather than selecting application dependencies. The Node stack installs only guest-local Playwright tooling and Chromium, exposes its browser path to later shells, and proves a headless launch; it never runs `npm install`/`npm ci` in the mapped project. Project `playwright`/`@playwright/test`, TypeScript, and other npm dependencies remain owned by `package.json` and its lockfile. `Install-DotNetStack` installs the modern .NET 10 LTS SDK family, not .NET Framework, previews, Visual Studio, or project target frameworks.

For a project-specific tool, add idempotent Windows PowerShell 5.1 to its profile. For a package needed in every guest, use [`wingetPackages.add`](#global-configuration). There is no plugin registry or second profile format.

**Rust/MSVC note:** the first Rust-stack run may show Microsoft Visual Studio Installer on the host. The signed bootstrapper creates a verified Build Tools layout in the app-owned cache; it does not install Visual Studio or Rust on the host. The guest copies and installs from that layout.

### Global configuration

Open the global configuration with the Windows application registered for `.json` files:

```powershell
herdr-sandbox config
```

The command creates `config.json` only when absent and never replaces existing settings. Setup or the first mutating `up` also creates the user extension when absent:

```text
%APPDATA%\herdr-sandbox\config.json
%APPDATA%\herdr-sandbox\user.ps1
```

`config.json` is strict JSON, so comments are not allowed. Replace the example paths with existing folders. Object keys such as `external` and `shared` are user-chosen names; they become the final guest-folder name rather than a fixed keyword. Optional sections can stay empty.

```json
{
  "cacheDirectory": "",
  "memoryMB": 32768,
  "audio": false,
  "audioInput": false,
  "tailscale": false,
  "codingAgentSync": {
    "opencode": true,
    "claudeCode": true,
    "codex": true,
    "githubCopilot": true,
    "pi": true
  },
  "workspaces": {
    "external": "E:\\Clients\\external"
  },
  "mounts": {
    "shared": {
      "path": "E:\\Shared",
      "readOnly": true
    }
  },
  "workspaceDiscovery": {
    "root": "D:\\Projects",
    "exclude": [
      "^(archive|scratch)$",
      "(?i)^temp-"
    ]
  },
  "wingetPackages": {
    "remove": [],
    "add": [
      "SST.opencode"
    ],
    "versions": {}
  }
}
```

| Field | Meaning |
| --- | --- |
| `cacheDirectory` | Absolute dedicated Herdr Sandbox package/tool cache. Empty uses `<system-temp>\herdr-sandbox\cache`. It must not overlap a workspace or app run state; every uninstall recursively removes the entire selected cache, so never point it at a shared directory. |
| `memoryMB` | Default Sandbox memory; minimum 2048. `--memory-mb` overrides one run. |
| `audio` | Exact boolean audio-output opt-in. Omitted or `false` suppresses playback; only `true` leaves playback enabled. |
| `audioInput` | Exact boolean microphone-input opt-in. Omitted or `false` blocks host microphone sharing; only `true` enables Windows Sandbox audio input. |
| `tailscale` | Exact boolean opt-in for the stable tagged identity. Omitted or `false` leaves Tailscale install-only. |
| `codingAgentSync` | Five exact booleans; all default to `true`. Set one to `false` to skip that agent. |
| `workspaces` | User-named project roots mapped to `C:\Workspaces\<name>`. Names are arbitrary but unique; values are absolute existing host folders. |
| `mounts` | Optional user-named non-workspace folders mapped to `C:\Mounts\<name>`. Every entry requires an absolute existing `path` and explicit `readOnly`; at most 16 are allowed. |
| `workspaceDiscovery` | Optional direct-child project discovery with an absolute `root` and multiple `exclude` regular expressions. Empty or omitted `root` disables it. |
| `wingetPackages.remove` | Known optional Base packages to omit. Core packages cannot be removed. |
| `wingetPackages.add` | Exact additional WinGet package IDs installed in every guest. Fresh configs show `SST.opencode` as a replaceable example; remove or replace that entry to choose another coding agent. |
| `wingetPackages.versions` | Exact versions for retained or added packages. Omitted versions resolve latest; unavailable exact versions fail. After a successful install, an inconclusive WinGet read-back warns and continues. |

#### Experimental Vulkan

Vulkan remains disabled by default. To install only the LunarG runtime and require a real vGPU-backed device, retain any other desired additions and add `KhronosGroup.VulkanRT`:

```json
"wingetPackages": {
  "remove": [],
  "add": [
    "SST.opencode",
    "KhronosGroup.VulkanRT"
  ],
  "versions": {}
}
```

Provisioning runs `vulkaninfo --summary` and fails when no physical device is exposed. This experimental path does not install the Vulkan SDK, Microsoft's D3D mapping package, a host GPU driver, or enable vendor extensions.

#### Audio policy

Both audio toggles default off. With both off, provisioning selects Windows **No Sounds**, mutes the default render endpoint, and disables the guest audio services with read-back verification. Ordinary applications therefore cannot restore playback by changing only their own volume.

Set `"audioInput": true` to share the host microphone and retain the shared audio services. Because capture and playback use those services together, guest applications can then unmute output even while `audio` remains false; these controls are not a security boundary against guest administrator code. Set `"audio": true` independently for deliberate playback. Changing either toggle requires `down` before the next `up`.

No CPU-priority option is exposed because Windows Sandbox provides no supported per-instance control; changing the launcher priority would not reliably control guest vCPU scheduling.

#### Coding-agent sync

Configuration sync is default-on when these host surfaces exist:

| Agent | Configuration/authentication behavior |
| --- | --- |
| OpenCode | Copies approved config/data files and portable `auth.json`. |
| Claude Code | Copies approved `.claude` configuration and `.credentials.json`. |
| Codex | Copies approved `CODEX_HOME` content and file-mode credentials. Keyring credentials stay host-bound. |
| GitHub Copilot CLI | Copies approved config and reuses successfully imported GitHub CLI accounts. Native Credential Manager tokens stay host-bound. |
| Pi | Copies approved agent configuration and portable `auth.json`. |

When an enabled agent root—or the shared skills root—is a standard physical Git worktree, sync also transfers its tracked files and bounded `.git` repository so the guest retains the current branch, remote, upstream, index, refs, objects, tracked edits, and tracked deletions. Git hooks, reflogs, linked-worktree pointers, active-operation state, external object stores, non-files ref storage, and known tracked credential/runtime paths are not accepted. Because Git objects and local repository configuration can contain historical or embedded secrets, disable that agent's sync unless its complete configuration-repository history is safe for the guest.

The shared `%USERPROFILE%\.agents\skills` tree is copied once when Codex, Copilot, or Pi is enabled. Conversations, runtime history, logs, caches, generated plugin/package state, project trust, private SSH/GPG keys, and unrelated home content are excluded. Missing host configuration is a clean no-op; this includes an absent global Git config, host `gh.exe`, or authenticated GitHub CLI account. Guest Git still receives only the required mapped-workspace trust entries. This feature copies setup only; coding-agent installation remains an explicit `wingetPackages.add` or project-profile choice.

#### Workspace discovery

- Discovery checks only direct child directories and never maps the root itself. Each Go/RE2 `exclude` expression is case-sensitive unless it uses `(?i)`; any match excludes that child.
- Every selected child becomes a workspace even without `.herdr-sandbox\provision.ps1`. When that optional script exists it is validated and run; the folder name becomes the workspace name. Use `workspaces` for external projects or explicit names, which win when both select the same path.
- The active project is added and deduplicated automatically. At most 16 physical, existing, nonoverlapping, non-reparse workspaces are allowed; a changed set requires `down` before the next `up`.

#### Folder mounts

Use optional `mounts` for host folders that should be available without becoming project workspaces. The key is arbitrary: a mount named `worktrees` appears at `C:\Mounts\worktrees`, while `shared` appears at `C:\Mounts\shared`. No `reference` key is required. Generic mounts do not become active, run `.herdr-sandbox\provision.ps1`, or create Herdr workspaces. Set `readOnly` to `true` for reference/shared material and to `false` only when guest tools should persist changes to the host folder, such as creating or updating worktrees.

Mapped folders expose host data across the isolation boundary. Ordinary explicitly selected descendants of the user profile remain valid, but Herdr Sandbox rejects volume roots, reparse-bearing paths, whole protected roots, and known credential locations such as `.ssh`, `.gnupg`, cloud/container config, coding-agent authentication roots, GitHub CLI state, and Windows credential stores. A parent containing one of those locations and a descendant inside one are both rejected. Mounts also may not overlap another mount, workspace, cache, private run state, or app-owned root selected for recursive uninstall removal. Every guest destination remains fixed below `C:\Mounts`; arbitrary guest system paths cannot be selected. Changing a mount path or access mode requires `herdr-sandbox down` before the next `up`.

#### Agent packages

OpenCode is not a mandatory Base package. Fresh configs list `SST.opencode` under `wingetPackages.add` as a visible example: replace that single ID with the preferred coding-agent package, or remove it to install no coding agent globally. No separate disable entry is required.

To install every coding agent that currently has a verified WinGet package, use:

```json
{
  "remove": [],
  "add": [
    "SST.opencode",
    "Anthropic.ClaudeCode",
    "OpenAI.Codex",
    "GitHub.Copilot"
  ],
  "versions": {}
}
```

Pi does not currently have a verified WinGet package; install it explicitly in the project profile that needs it. `codingAgentSync` controls configuration/authentication transfer only and does not install an agent.

#### Global extension ownership

- `base.ps1` and `stacks.ps1` are release-owned and update with the application.
- `user.ps1` is seeded once for idempotent global PowerShell additions and runs before project profiles.
- Keep package selection in `config.json` and project-specific behavior in each project profile.
- Do not store credentials or print secrets from `user.ps1`; its immutable snapshot may remain with active or uncertain run diagnostics until safe cleanup.

Older releases seeded a user-owned `%APPDATA%\herdr-sandbox\base.ps1`. The new ownership model never overwrites or executes that file: `up` stops with migration instructions. Review it, move only deliberate global extension commands into `user.ps1`, move package choices into `config.json`, keep project tools in project profiles, archive the complete legacy Base under a non-reserved name, and retry.

#### Persistent host state

Persistent host state is split intentionally:

| Path | Contents |
| --- | --- |
| `%APPDATA%\herdr-sandbox` | User-owned global config and `user.ps1` extension. |
| `%LOCALAPPDATA%\herdr-sandbox\identity` | Host SSH identity and optional DPAPI-protected Tailscale identity. |
| `%LOCALAPPDATA%\herdr-sandbox\runs` | Per-run status, diagnostics, host-owned retained-operation state, SSH material, and `.wsb` files. Do not edit an active run. |
| `%LOCALAPPDATA%\herdr-sandbox\ssh\config` | App-owned `Host sandbox` target for the current guest; removed automatically only when no Sandbox is proven to remain. |
| `<system-temp>\herdr-sandbox\cache` | Default persistent package/tool cache. |

## Security boundaries

See [`SECURITY.md`](SECURITY.md) for vulnerability reporting, the complete threat
model, and practical guidance for credential-free or externally network-restricted
use.

- Writable host mappings are limited to selected project roots, named mounts with `readOnly: false`, the explicit package/tool cache, and bounded per-run status; networking remains enabled.
- The host home root, general AppData, unselected repositories, and private SSH/GPG keys are never mapped; only the app-owned public SSH key enters the guest.
- Approved GitHub CLI and coding-agent credentials travel only over verified SSH and never enter persistent run input or logs. Machine-bound credentials require a guest login.
- Tailscale auth-key and state bytes never enter mappings, status, diagnostics, command lines, or package cache.
- Guest OpenCode managed policy resolves every permission to `allow`; host OpenCode policy is unchanged. Treat guest agents as fully authorized inside the Sandbox and mapped projects.
- The reviewed disposable-guest privacy profile intentionally restricts Defender cloud/security features, SmartScreen, automatic updates, telemetry, and related services. It is not a hardened production workstation profile.
- Downloads and cache hits are validated against strict versions, metadata, hashes, signatures, or package identity as applicable.
- Host Rust tooling is forbidden. Rust installation, builds, and tests belong only in the verified guest or GitHub Actions.
- Lifecycle commands revalidate exact app-owned process/path identity and refuse unrelated, changed, or reparse-bearing state.

## Stable Tailscale tailnet identity (experimental)

> [!CAUTION]
> The required two-fresh-Sandbox identity and peer-connectivity acceptance gate remains open. Use this opt-in only with a tailnet prepared for a dedicated tagged device.

`herdr-sandbox` joins an existing user-owned tailnet; it does not create the tailnet or manage policy. The stable address lets an approved phone, tablet, or computer reach services in the running Sandbox without publishing them to the internet. Leave `"tailscale": false` for a manually managed disposable login whose identity is not preserved.

Tailscale supplies only the private network path. Another device still needs a compatible client and an explicitly authorized credential; the default OpenSSH endpoint accepts only the app-owned host identity. Never copy that private key to another device.

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

The CLI removes its inherited environment copy before launching children, enrolls fixed hostname `herdr-sandbox`, verifies the tagged identity, and stores node state only as current-user DPAPI ciphertext. Confirm exactly one tagged device in the admin console and verify the route from an intended peer with `tailscale ping herdr-sandbox`; service login still requires its own authorized credential.

### 4. Later Sandboxes

Do not supply the auth key again. `down` captures and verifies current state before closing; the next fresh `up` restores it over verified SSH. Device ID, node key, IPv4, MagicDNS, hostname, tags, and Windows Sandbox user SID must remain exact or the workflow fails closed.

Keep node-key expiry disabled. Do not delete the tailnet device or `%LOCALAPPDATA%\herdr-sandbox\identity\tailscale-identity.json` while expecting restoration. The protected identity is bound to the current Windows host user and is not a portable backup.

</details>

## Troubleshooting

Start with `herdr-sandbox status`; it preserves a running guest, removes only proven stale state, and reports the next action.

| Symptom | Action |
| --- | --- |
| Windows Sandbox is unavailable | Enable `Containers-DisposableClientVM` from elevated Windows PowerShell, restart Windows, and confirm hardware virtualization is enabled. |
| `up` refuses an existing Sandbox | A ready exact guest is reused automatically. A normally closed window is cleaned on the next valid command. For failed, changed-plan, unmanaged, or ownership-uncertain state, inspect the reported evidence and use `herdr-sandbox down` only when it identifies the app-owned instance. |
| Automatic attach is unavailable in a headless process | Provision intentionally with `herdr-sandbox up --no-attach`, then open a real terminal and run `herdr-sandbox attach`; a verified ready guest remains reusable. |
| The guest has no playback audio | Audio output is off by default. Set `"audio": true` in `config.json`, run `herdr-sandbox down`, then start a fresh guest with `up`. This does not enable microphone input. |
| The guest cannot use the microphone | Microphone input is off by default. Set `"audioInput": true` in `config.json`, run `herdr-sandbox down`, then start a fresh guest with `up`. Host microphone permissions or policy can still block sharing. |
| `ssh sandbox` no longer connects | Run `herdr-sandbox status`. If no Sandbox remains, startup cleanup removes the stale target and reports `stopped`; run `up` to create the next verified target. Ownership uncertainty is preserved and reported instead of guessed. |
| Legacy global Base is refused | Preserve `%APPDATA%\herdr-sandbox\base.ps1`, move only deliberate additions to `user.ps1`/config/project ownership, archive the legacy file under a non-reserved name, and retry. |
| Initial provisioning is slow | The first run may download WinGet, OpenSSH, selected SDKs such as modern .NET or Rust, and the Visual Studio layout required only by Rust/MSVC. Herdr is copied from the host and does not use the download cache. Confirm that the cache is writable and does not overlap a workspace or run state. |
| Stable Tailscale enrollment is refused | Confirm exact `true`, the retained Tailscale package, and a current one-time non-ephemeral pre-approved tagged key. Restoration refuses missing, corrupt, differently DPAPI-bound, untagged, or identity-mismatched state. |
| Old diagnostics consume space | `up`, `status`, and `down` remove validated inactive run workspaces automatically; `clean` invokes the same cleanup explicitly once. Active/uncertain evidence and the persistent cache remain preserved. |

## Development

Repository tasks:

```powershell
go run ./cmd/task fmt
go run ./cmd/task test
go run ./cmd/task build
go run ./cmd/task check
go run ./cmd/task native-all-stacks
go run ./cmd/task package v0.0.0
```

- `check` covers Go formatting, Windows PowerShell 5.1 parsing, all Go tests, `go vet`, and the stable `build\bin` artifact.
- `native-all-stacks` provisions one fresh real Sandbox with .NET, Go, Node.js plus Playwright Chromium, and the Herdr virtual stack (Python, Rust/MSVC, Zig, Bun, Nextest, Just, and `sh`); it launches Chromium headlessly over managed SSH, runs representative version/build/test commands, verifies the libghostty guest-local output contract plus Terminal and Starship transfer, and closes only its exact app-owned guest. It intentionally selects every supported stack as a breadth and compatibility gate, not as a startup-time benchmark for normal project plans. It requires Windows Sandbox, network/package access, host Herdr, and GitHub CLI.
- `package` uses pinned NSIS 3.12 and writes the installer, ZIP, and both checksum files under `build\dist` without installing them.
- Repository provisioning and installer helpers run exclusively under Windows PowerShell 5.1; installed PowerShell 7 remains interactive guest tooling.

## License

`herdr-sandbox` is licensed under the [Apache License, Version 2.0](LICENSE) and is provided on an **"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND**. See the license for the governing terms, including its warranty disclaimer and limitation of liability.

## Documentation

- Stable user-visible behavior: [`PRODUCT.md`](PRODUCT.md)
- Technical boundaries: [`ARCHITECTURE.md`](ARCHITECTURE.md)
- Open work: [`BACKLOG.md`](BACKLOG.md)
- Project-specific agent/repository rules: [`AGENTS.md`](AGENTS.md)
