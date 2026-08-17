# BACKLOG.md

Open, planned, blocked, or deferred product features and fixes only. User-visible rules belong in `PRODUCT.md`; technical decisions belong in `ARCHITECTURE.md`; workflow/tooling proposals belong in `AGENT_IMPROVEMENTS.md`.

## Rules

- Keep only current user-selected product features and fixes.
- Remove completed or obsolete items instead of preserving history.
- Do not use this file for testing, verification, acceptance gates, or task logs.

## Items

- Audit remaining project-file checks by ownership rather than moving every guest
  check earlier. Generic stacks must accept an otherwise empty mapped project and
  must not require manifests, dependency state, builds, or tests. Preflight only a
  project file that is directly consumed by an explicitly selected project
  shortcut or as an optional version source. Missing optional version files select
  the stack default; present selected files remain bounded, safe, and strict, with
  guest-side recheck where the mapped file affects the immutable plan. Errors must
  name the workspace, host path, owner, and correction.
- Make verified management SSH the first functional guest capability. Before
  implementation, explicitly challenge this target against unavoidable bootstrap
  prerequisites, partial-guest attach semantics, security, connection recovery,
  and whether a simpler reconnectable phase model preserves the same outcome.
  Record the better design rather than assuming a long-lived transport is
  inherently correct. Limit the pre-SSH bootstrap to the minimum required to
  establish and publish that secure channel; do not run Base, development
  provisioning, or configuration sync there. Immediately provision and start
  Herdr over SSH, establish one host-owned running Herdr/SSH control session, and
  make the guest attachable before continuing setup through that same session.
  Run every remaining script and external package/API operation as a
  host-invokable idempotent phase for fresh and retained guests. Keep the control
  channel available or re-establish it against the same live guest so `up` can
  retry or resume unfinished work from the last verified boundary. Every post-SSH
  failure must preserve the connectable Sandbox, exact failed phase, and bounded
  diagnostic. Treat transient provider availability as retryable without silently
  skipping credentials, integrity checks, or functional verification. Require no
  `down`, GUI input, watcher, daemon, second provisioner, or fresh launch; keep the
  unavoidable pre-SSH failure window minimal and explicit.
- Automate the official Playwright Extension token handoff. Keep the unmodified
  Chrome Web Store extension, existing headed main-user Edge profile, and
  `playwright-cli.cmd -s=edge-main attach --extension=msedge` contract; extract
  the extension-generated profile-local token and publish it only to disposable
  guest environment state without UI automation, logging, project persistence,
  remote debugging, another profile, or a custom extension.
- Publish `hdosys.herdr-win` as an independent package so users can install both
  `hdosys.herdr-sandbox` and `hdosys.herdr-win` from the WinGet community source
  without a package dependency or shared payload.
- Design intelligent lifecycle, refresh, and explicit reset management for
  durable Windows development VMs as a future product mode. Start with one
  concrete VM owner; define reset so it removes only that VM's proven mutable
  state while preserving user configuration, selected workspaces, and deliberately
  persistent caches; and preserve strict process, credential, filesystem, and
  terminal boundaries rather than introducing a provider framework first.
- Give the remaining native process owners in guest Base/bootstrap and visible
  Sandbox launch rollback explicit per-role deadlines and exact process-tree
  cleanup. Preserve the product-required visible Sandbox client/bootstrap and
  hidden noninteractive descendant consoles without adding a second invocation
  framework.
- Evaluate checksum-validated Cargo `.crate` archive seeding. Keep Cargo index/Git
  databases, extracted sources, installed binaries, and target outputs guest-local
  because Cargo exposes no supported split-cache paths and trusts existing
  archives without rehashing them.
- Evaluate the Windows 11 24H2+ `wsb.exe` management API as a second implementation only if it removes lifecycle complexity without dropping supported Windows 10 behavior.
