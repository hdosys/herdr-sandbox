# Changelog

Notable user-visible changes are recorded here. Exact publication times and
artifacts remain available on the
[GitHub Releases](https://github.com/hdosys/herdr-sandbox/releases) page.

## Unreleased

### Added

- Selected coding agents now receive Herdr awareness automatically. Existing host
  integrations are synchronized without being replaced, while Herdr installs an
  offered integration only when that agent is available and no integration exists.
- One optional shared host model directory now maps read/write at `C:\Models` for
  guest AI tools. Herdr Sandbox uses the same root for verified local VoxCPM2
  models and provisions only the CPU runtime with GPU layers disabled in the
  existing HyperFrames stack.

### Changed

- Local VoxCPM2 narration now uses the selected German Herdr narrator reference as
  one stable default voice across segments. Projects can explicitly select Voice
  Design or another reference, and the concise `tts.ps1` command exposes both paths
  directly in the guest.
- The audio stack now installs REAPER 7.79 and disables its in-app update checks
  before first launch while preserving existing REAPER settings.
- Fresh TradingView guests now reject optional analytics and advertising cookies
  before first launch, so the privacy consent prompt does not block the chart.

### Fixed

- HyperFrames provisioning no longer rejects the verified VoxCPM2 provider only
  because its build metadata names another HyperFrames version. The current
  HyperFrames release is provisioned and real runtime behavior now determines
  compatibility.
- Guest OpenCode now keeps automatic clipboard copy when text is selected in
  Herdr, matching the established mouse-selection workflow.
- Microsoft OpenJDK 25 provisioning now accepts Microsoft's runtime patch suffix
  while still requiring the exact WinGet package, Microsoft runtime identity, and
  matching `java` and `javac` versions, so reprovisioning no longer rejects
  package `25.0.4.101` when its tools report `25.0.4.1`.

## v0.0.18

### Added

- A new optional `audio` stack provisions REAPER plus AudioGridder
  Server and clients, making the Sandbox the VST execution machine. Project
  provisioning owns the guest VST set, while the production host DAW client is a
  manual prerequisite; native acceptance inserts the guest client in REAPER and
  requires a real local AudioGridder worker connection.
- A new optional `hyperframes` stack provisions the latest stable
  HyperFrames CLI, Node.js 22+, full FFmpeg/FFprobe, managed Chrome Headless
  Shell, and global skills for every supported coding agent. Provisioning checks
  the core doctor boundary and a software H.264 encode without claiming hardware
  encoder availability.

### Fixed

- Current TVControl releases no longer fail provisioning on an obsolete source
  digest and launch TradingView Desktop directly from the interactive agent
  session.
- Fresh and retained `sandbox up` now validate Herdr-Win provisioning against
  its matching runtime identity and use the verified guest executable location,
  allowing provisioning to continue into initial workspace creation after the
  server starts.
- GitHub CLI account transfer now resolves each token's current login before guest
  import, preserving selected accounts while avoiding failures after account renames
  or username case normalization.
- `sandbox init --stack all` now includes every generic stack, including Audio,
  HyperFrames, and Python AI; only checkout-specific Handy and Herdr remain
  explicit selections.
- Retained Android provisioning now reuses fully verified Platform Tools instead
  of rerunning the Android package operation that can fast-fail on an already
  provisioned guest.

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
