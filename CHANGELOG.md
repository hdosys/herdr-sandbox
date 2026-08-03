# Changelog

Notable user-visible changes are recorded here. Published artifacts and exact
release notes remain available on the
[GitHub Releases](https://github.com/hdosys/herdr-sandbox/releases) page.

## Unreleased

### Added

- `herdr-sandbox version`, reporting the release version and abbreviated source
  revision embedded by the canonical build task.
- A public security policy describing the host/guest trust boundary, credential
  and network trade-offs, supported reporting path, unsigned installer, and
  latest-versus-pinned package behavior.
- A disabled-by-default experimental `KhronosGroup.VulkanRT` package opt-in with
  strict physical-device verification through `vulkaninfo`.

### Changed

- Missing host Git configuration, GitHub CLI, and GitHub authentication are now
  clean no-ops during guest configuration sync.
- Windows Terminal's supported `system` theme now uses the deterministic dark
  guest prompt baseline instead of blocking startup.
- Ordinary selected folders beneath the user profile remain mountable, while
  known SSH, GPG, cloud, container, agent-auth, GitHub, and Windows credential
  roots are rejected together with their parents and descendants.

### Fixed

- Bounded subprocess capture now terminates an output-flooding process tree after
  one MiB instead of growing host memory without limit.
- External diagnostics replace terminal-control characters before display.
- User SSH config updates reread and retry when a concurrent edit is observed
  before atomic replacement, substantially narrowing the prior lost-update window.
- Installer failures after payload replacement now restore the prior application
  files, uninstaller, registration version/PATH ownership, and newly added PATH
  entry through one rollback path.
