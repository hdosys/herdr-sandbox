# Changelog

Notable user-visible changes are recorded here. Published artifacts and exact
release notes remain available on the
[GitHub Releases](https://github.com/hdosys/herdr-sandbox/releases) page.

## Unreleased

### Added

- QR-assisted mobile Herdr access can use device-owned Ed25519 keys over the
  stable private Tailscale identity. A dedicated key-only endpoint keeps its
  host fingerprint across fresh Sandboxes, while `herdr-sandbox mobile` prints
  the secret-free connection profile and manual fallback.

### Fixed

- Installer upgrades and removal now bind registry, directory, file-manifest, and
  PATH ownership to stable product and per-installation identities. Durable
  rollback/recovery, exact silent options, cross-session command exclusion, and
  truthful quiet-uninstall results preserve the previous complete installation or
  resume the intended terminal cleanup after interruption.
- Setup now repairs verified v0.0.9 installations while preserving unrelated or
  missing legacy files, and uninstall no longer fails when an active agent or tool
  temporarily locks the disposable package cache.

## v0.0.9 - 2026-08-05

### Added

- A `tradingview` project stack provisions the latest stable TVControl commands
  and verified official TradingView Desktop payload for Windows Sandbox. Select
  it with `herdr-sandbox init --stack tradingview`; native acceptance passed
  visible launch, CDP, health, API/datafeed, and compatibility.

## v0.0.8 - 2026-08-04

### Added

- `herdr-sandbox version`, reporting the release version and abbreviated source
  revision embedded by the canonical build task.
- A public security policy describing the host/guest trust boundary, credential
  and network trade-offs, supported reporting path, unsigned installer, and
  latest-versus-pinned package behavior.
- A disabled-by-default experimental `KhronosGroup.VulkanRT` package opt-in with
  strict physical-device verification through `vulkaninfo`.
- An explicit virtual `herdr` project stack so official Herdr and Herdr-Win
  checkouts can generate their maintained Windows toolchain profile—including
  Bun, Git for Windows `sh`, and the repository-required `python3` command—with
  `herdr-sandbox init --stack herdr`.
- Git-backed OpenCode, Claude Code, Codex, GitHub Copilot, Pi, and shared-skills
  configuration now retains tracked workflow files plus branch, remote, upstream,
  index, refs, objects, tracked edits, and tracked deletions inside the guest.
- A separate `playwright-cli` project stack installs the approved Playwright CLI
  without browser binaries or update-check state and prepares the official
  extension for manual attachment to the guest user's existing headed Edge profile.
### Changed

- Host Herdr selection now accepts any Windows `herdr.exe` that proves the
  required `--remote` capability. Guest provisioning copies the reported
  physical executable and optional complete ConPTY bundle without depending on
  a specific fork, package, installer, launcher, or managed-runtime layout.
- Missing host Git configuration, GitHub CLI, and GitHub authentication are now
  clean no-ops during guest configuration sync.
- Windows Terminal's supported `system` theme now uses the deterministic dark
  guest prompt baseline instead of blocking startup.
- Ordinary selected folders beneath the user profile remain mountable, while
  known SSH, GPG, cloud, container, agent-auth, GitHub, and Windows credential
  roots are rejected together with their parents and descendants.

### Fixed

- GitHub CLI authentication now imports into disposable guest-only file storage
  and requires Git before configuring and exactly reading back `gh` as the
  host-specific Git credential helper, avoiding Windows Credential Manager/GCM
  account dialogs while preserving HTTPS Git access.
- OpenCode configuration sync now reapplies the guest-wide `allow` policy after
  every host configuration copy, even when OpenCode is installed outside the
  selected Base package plan, so host top-level and agent permission rules cannot
  govern the Sandbox.
- Ready guests now accept WinGet package-plan changes through retained
  reprovisioning when the same mappings differ only by Windows-insignificant
  letter casing or ordering, while unknown launch-contract drift still fails closed.
- Configuration archives larger than Win32-OpenSSH's redirected-stdin pipe
  window now transfer through bounded guest staging before Windows PowerShell
  verification, instead of timing out indefinitely at `receive-archive`.
- Git-backed coding-agent sync now accepts its deterministic manifest property
  order and empty tracked-deletion sets instead of rejecting enabled agent
  configuration during guest apply.
- Git-backed agent roots and configured cache overlap checks now compare physical
  Windows identities, so DOS 8.3 aliases neither reject valid repositories nor
  bypass overlap checks.
- Python 3 command compatibility and Git-for-Windows `sh` exposure now remain
  with their runtime/package owners, including retained reprovisioning after
  `Git\bin` becomes the active Git command directory.
- Bounded subprocess capture now terminates an output-flooding process tree after
  one MiB instead of growing host memory without limit.
- External diagnostics replace terminal-control characters before display.
- User SSH config updates reread and retry when a concurrent edit is observed
  before atomic replacement, substantially narrowing the prior lost-update window.
- Installer failures after payload replacement now restore the prior application
  files, uninstaller, registration version/PATH ownership, and newly added PATH
  entry through one rollback path.
