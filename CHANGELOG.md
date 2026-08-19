# Changelog

Notable user-visible changes are recorded here. Exact publication times and
artifacts remain available on the
[GitHub Releases](https://github.com/hdosys/herdr-sandbox/releases) page.

## Unreleased

## v0.0.16

### Fixed

- Host GitHub CLI credential transfer no longer performs a redundant `/user` API
  lookup before the existing strict guest import and verification.
- TradingView stack provisioning now uses release-pinned installer metadata,
  avoiding runtime catalog and manifest lookups before the verified download.

## v0.0.15

### Added

- Base provisioning now includes the native `opensrc` CLI for package-source
  inspection and keeps fetched sources in the persistent tool cache.

### Fixed

- Generic stacks, including Go, now work in otherwise empty projects. `sandbox plan`
  reports where each resolved tool version came from.
- Project-profile errors identify the affected workspace, host directory, and
  source profile.
- Fresh authenticated TradingView guests open a controllable chart instead of
  the first-run Welcome dialog, without replacing retained user tabs.
- Android Platform Tools verification avoids a crash-prone package listing and
  tolerates the harmless OpenJDK processor-group warning.
- `sandbox down` preserves verified Tailscale state and bounds lifecycle and
  process waits while retaining rollback on a failed stop.

## v0.0.13

### Added

- The CLI accepts both `sandbox --version` and `sandbox version`, and setup keeps
  an up-to-date `config.sample.json` without replacing user configuration.
- Optional persistent Herdr worktrees survive fresh Sandboxes under one dedicated
  host root and use Herdr's normal create, reopen, and remove lifecycle.
- New Android, C/C++, Java, NSIS, and Nushell stacks add verified native Windows
  toolchains. `sandbox init --stack all` selects every standalone stack.
- The TradingView stack can transfer the signed session cookie pair from an
  available host login, launch Desktop visibly, and expose TVControl to OpenCode
  as an explicit session-only MCP opt-in.

### Changed

- Built-in tool versions are resolved and checked before guest mutation; conflicts
  name every owner, while `sandbox plan` shows the selected value and source.
- Fresh and retained `sandbox up` use Herdr-Win's unattended remote provisioning
  path as the single guest runtime and server lifecycle owner.
- Git-backed configuration roots can fast-forward before `up`, after `down`, or
  explicitly through `sandbox pull-host-config`, while unsafe local state is left
  for the user to resolve.
- Fresh configuration selects every coding agent with a verified WinGet package;
  remove unwanted entries from `wingetPackages.add`.
- Setup, repair, upgrade, and uninstall now stop exact installed commands, preserve
  a running Windows Sandbox, recover retryable failures, and keep user
  configuration unless deletion is explicitly selected.
- The current installer uses one new product identity and does not migrate older
  installer formats. Remove an older-format installation with its matching
  uninstaller before installing this release.

## v0.0.12

### Changed

- The installed and portable command is now `sandbox.exe`, invoked as `sandbox`.
  Setup removes the former executable only for a proven owned upgrade and does not
  retain a compatibility alias.
- GitHub Releases now display SHA-256 digests for the installer and portable ZIP,
  which form the complete downloadable artifact set.

## v0.0.11

### Added

- A `handy` project shortcut provisions its Windows toolchain with Bun,
  Rust/MSVC, CMake, WebView2, and the project-pinned Vulkan SDK.

### Changed

- The Windows installer improves interrupted-upgrade, PATH registration, and
  quiet-uninstall recovery.

## v0.0.10

### Added

- A `python-ai` shortcut provisions Python 3.13 and uv with a persistent dependency
  cache while projects retain their own frameworks and lockfiles.
- QR-assisted mobile Herdr access can use device-owned Ed25519 keys over the
  stable private Tailscale identity. A dedicated key-only endpoint keeps its
  host fingerprint across fresh Sandboxes, while the mobile command prints the
  secret-free connection profile and manual fallback.

### Fixed

- Setup and uninstall recover interrupted or drifted installer state and return
  actionable terminal results instead of stranding the progress page.
- Uninstall no longer fails when an active agent or tool temporarily locks
  disposable package-cache or machine-local state.

## v0.0.9

### Added

- A `tradingview` stack provisions verified TradingView Desktop and TVControl for
  visible chart automation inside Windows Sandbox.

## v0.0.8

### Added

- A version command reports the release and abbreviated source revision, and the
  new security policy documents the real host/guest trust model.
- The `herdr` shortcut provisions the Windows toolchain used by Herdr checkouts;
  the `playwright-cli` stack prepares the official Edge extension integration.
- Git-backed coding-agent configuration retains usable repository state in the
  guest, while an experimental Vulkan runtime remains an explicit opt-in.

### Changed

- Configuration sync treats missing optional host Git, GitHub CLI, or login state
  as a clean no-op and protects known credential roots from folder mappings.
- The supported Windows Terminal `system` theme uses a deterministic guest prompt
  baseline.

### Fixed

- GitHub CLI and OpenCode authentication/configuration now import without host
  credential dialogs and reapply the guest-wide OpenCode permission policy.
- Ready guests can apply package-plan changes through retained reprovisioning, and
  larger configuration archives transfer without the former SSH pipe timeout.
- Process output and diagnostics are bounded and terminal-safe, SSH configuration
  updates retry concurrent edits, and failed installer replacement restores the
  prior application state.
