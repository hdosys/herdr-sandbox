# Changelog

Notable user-visible changes are recorded here. Published artifacts and exact
release notes remain available on the
[GitHub Releases](https://github.com/hdosys/herdr-sandbox/releases) page.

## Unreleased

### Added

- Optional `worktreeDirectory` support now maps one dedicated persistent host root
  to `C:\Worktrees`, configures guest Herdr to create native Git worktrees there,
  and teaches selected guest agents to use Herdr's create, discover, reopen, and
  remove lifecycle while retaining the user's own cleanup policy.
- Selecting the TradingView stack now carries an available host TradingView
  Desktop login into the disposable guest through the verified configuration
  transfer, without copying the host profile or unrelated cookies.
- A new `android` stack provisions verified Android command-line build tooling,
  latest stable Platform Tools, an isolated OpenJDK 17, and wireless ADB
  `pair`/`connect` support without adding a host USB bridge.
- A new `nushell` stack installs and verifies the latest stable x64 Nushell MSI
  and exposes `nu.exe` without adding shell configuration or scripts.
- A separate `nsis` stack installs and verifies the NSIS compiler, including a
  real installer compile, so installer-only projects can select it with
  `sandbox init --stack nsis`.
- New `cpp` and `java` project stacks provide verified x64 C/C++ Build Tools and
  Microsoft OpenJDK 25 LTS compile/run environments. Select them with
  `sandbox init --stack cpp` and `sandbox init --stack java`.
- `sandbox init --stack all` creates one direct-call profile for every standalone
  technology and tool stack while keeping project-specific shortcuts separate.

### Changed

- TradingView login transfer now reads the installed Desktop MSIX package profile
  and carries its complete signed session cookie pair into the portable guest app.
- Selecting TradingView now also registers the verified TVControl server as an
  enabled guest-managed OpenCode MCP integration with fixed loopback CDP settings.
- The portable TradingView Desktop now receives a Start-menu shortcut and a
  conditional taskbar pin that launch with its fixed local CDP port enabled.
- Built-in stack versions are now preflighted across `user.ps1` and every selected
  project profile before fresh or retained guest mutation. Exact requests converge,
  conflicts name all owners and stop, all-omitted tools resolve latest stable once,
  the Jobs Playwright version is independently resolved from its matching npm
  lockfile entries, Rust toolchain channels remain separate from the rustup package,
  and `sandbox plan` shows the resulting selection and owners.
- Fresh and retained `sandbox up` now use Herdr's exact unattended remote
  provisioning command as the single guest runtime, configuration validation, and
  server lifecycle owner. Sandbox no longer snapshots or copies a Herdr runtime,
  starts a duplicate server, or reloads configuration separately, and readiness
  now verifies the exact versioned sidecar, runtime version, protocol, and detached
  server reported by Herdr. Successful Herdr diagnostics remain separate from its
  strict JSON result instead of corrupting the final readiness handoff.
- Guest Herdr status verification now likewise keeps PowerShell and SSH diagnostics
  outside the strict JSON response.
- Guest project terminals and new PowerShell 7 shells now resolve the exact
  provisioned Herdr sidecar through refreshed `PATH` state, without a copied
  binary or wrapper.
- Every explicitly registered transferred configuration root that is itself a Git
  repository now fast-forwards from its configured upstream on the host before
  `up` and after a terminal `down` by default. Both lifecycle hooks can be disabled
  independently, while `sandbox pull-host-config` performs the host-only update on
  demand. Local edits remain when Git can update safely; unsafe states stop the
  pull for explicit user resolution without copying configuration from the guest.
- Fresh configurations now select every coding agent with a verified WinGet
  package. Remove unwanted entries from `wingetPackages.add` before provisioning.
- Fresh interactive setup now offers a checked option on the Finish page to open
  the Sandbox configuration with its registered application.
- `sandbox up` now stops promptly when its launched Windows Sandbox process exits,
  releases installer coordination, and safely clears the exact stale run on retry
  even when process exit races cleanup inspection. Setup now uses actual Windows
  mutex ownership, so a process or host crash is acquired as abandoned immediately
  while only a currently live owner blocks installation.
- Host Herdr inspection now requires bounded `herdr --version` output to contain
  `herdr-win` in addition to proving the Windows remote interface. It no longer
  compares that distribution identity with the independently formatted
  `status client` version.
- Visual Studio Build Tools host layout preparation now refreshes an existing
  cached bootstrapper correctly on cache misses.
- Guest provisioning now installs the pinned VC++ runtime before stack packages,
  preventing the Vulkan SDK prerequisite from opening an interactive installer.
  Handy now validates its CMake packages separately and uses the verified direct
  compiler path, avoiding reusable MSBuild or Debug PDB workers.
- Python 3 stacks now expose adjacent verified `python` and `python3` commands so
  uv-created Windows virtual environments retain a valid base executable.
- Installer ownership now keeps one stable unversioned lifecycle mutex across
  releases, accepts rooted literal PATH entries containing `%`, and rejects a
  replaced-executable name that collides with any current payload filename.
- Setup and uninstall now use a dedicated installer-only gate, so ordinary
  Sandbox commands no longer trigger a false installer-busy message. The simpler
  direct model removes the application-wide installer gate and transaction
  machinery, verifies every ownership-marker write, bounds quiet uninstall to 30
  seconds, and keeps destructive cleanup race-free under the existing app lock.
- Installer builds now validate identity, payload, helper syntax, version/output
  agreement, and artwork before NSIS compilation. PATH cleanup removes every
  normalized literal spelling of the fixed install directory while preserving
  environment-expression and unrelated entries.

## v0.0.12 - 2026-08-07

### Changed

- The installed and portable command is now `sandbox.exe`, invoked as `sandbox`.
  Setup removes the former executable only for a proven owned upgrade and does not
  retain a compatibility alias.
- GitHub Releases now display SHA-256 digests for the installer and portable ZIP,
  which form the complete downloadable artifact set.

## v0.0.11 - 2026-08-06

### Added

- A `handy` virtual project stack provisions the current Handy checkout's Windows
  development toolchain with Bun, Rust/MSVC, latest-stable CMake and WebView2,
  Vulkan SDK 1.4.309.0, and a verified SPIRV-Headers CMake target. Select it with
  `herdr-sandbox init --stack handy`.

### Changed

- The Windows installer now repairs interrupted upgrades and PATH registration,
  preserves unknown files in its install directory, retains quiet-uninstall
  recovery after late failures, and reports cleanup retry outcomes accurately.

## v0.0.10 - 2026-08-05

### Added

- A `python-ai` project stack provisions Python 3.13 and latest-stable uv with a
  persistent dependency cache for CPU and API-based AI projects. Select it with
  `herdr-sandbox init --stack python-ai`; each project keeps its frameworks and
  reproducible environment in `pyproject.toml` and `uv.lock`.
- QR-assisted mobile Herdr access can use device-owned Ed25519 keys over the
  stable private Tailscale identity. A dedicated key-only endpoint keeps its
  host fingerprint across fresh Sandboxes, while `herdr-sandbox mobile` prints
  the secret-free connection profile and manual fallback.

### Fixed

- Setup and uninstall now prioritize the requested terminal result: durable
  transactions recover normally, while stale journals, registration drift, and
  leftover files in the dedicated install directory automatically converge to a
  complete current installation or complete removal instead of stranding the
  installer on an aborted progress page. Interactive blockers remain actionable;
  silent runs terminate without a dialog and return a stable failure status.
- Uninstall no longer fails when an active agent or tool temporarily locks
  disposable package-cache or machine-local state.

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
  checkouts can generate their maintained Windows toolchain profile, including
  Bun, Git for Windows `sh`, and the repository-required `python3` command, with
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
