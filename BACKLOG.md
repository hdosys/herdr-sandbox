# BACKLOG.md

Open, planned, blocked, or deferred product work only. User-visible rules belong in `PRODUCT.md`; technical decisions belong in `ARCHITECTURE.md`; workflow/tooling proposals belong in `AGENT_IMPROVEMENTS.md`.

## Rules

- Keep items actionable and current.
- Remove completed or obsolete items instead of preserving history.
- Include expected verification when known.
- Do not use this file for task logs.

## Items

- Audit every remaining built-in project stack and project shortcut for
  deterministic mapped-project prerequisites that guest provisioning currently
  checks only after work has started. Move each host-observable file, manifest,
  and project-layout check into the shared nonexecuting `plan`/`up` preflight
  before fresh or retained guest mutation, while retaining its guest-side boundary
  recheck. Errors must name the workspace, host path, owning profile, and corrective
  action. Do not infer stacks from repository contents or preflight network,
  installation, or runtime-only facts. For every affected stack, verify that a
  missing prerequisite fails without starting guest provisioning and that a valid
  mapped project continues through inspection.
- Move the verified SSH connection to the earliest practical guest bootstrap
  boundary, then run the existing idempotent Base and development provisioning
  through that channel for both fresh and retained runs. A failure after SSH must
  be retryable in the same live Sandbox without `down`, GUI input, a watcher or
  daemon, a second provisioner, or a fresh launch; keep the unavoidable pre-SSH
  failure window minimal and explicit.
- Run one fresh native `python-ai` acceptance gate when `Containers-DisposableClientVM` is available: initialize the profile through the stable CLI, require `plan` to expand it to only the Python 3.13 and `astral-sh.uv` owners, provision with `up --no-attach`, and verify `python`, `python3`, uv version/cache readback, `UV_NO_MANAGED_PYTHON=1`, an offline `uv sync`, a locked `uv run`, and retained reprovisioning against the same ready guest. Keep framework packages project-owned and leave CUDA outside this CPU/API gate.
- Automate the official Playwright Extension token handoff after the manual-first stack passes natively. Keep the unmodified Chrome Web Store extension, existing headed main-user Edge profile, and `playwright-cli.cmd -s=edge-main attach --extension=msedge` contract; investigate a bounded way to extract the extension-generated profile-local token and publish it only to disposable guest environment state without UI automation, logging, project persistence, remote debugging, another profile, or a custom extension. Native verification must prove fresh-guest install/enable, token extraction, prompt-free attach, tab control, `detach` without closing Edge, and no token in host mappings, run evidence, cache, or source.
- Run one fresh native acceptance gate for the new current-path UX and modern .NET owner when `Containers-DisposableClientVM` is available: use the stable built CLI to confirm `plan` remains nonmutating, initialize a disposable `dotnet` profile, provision with `up --no-attach`, observe retained progress and prompt `status` snapshots, verify a bounded terminal operation outcome independently of ready health, attach from a real console, and build/run a minimal `net10.0` project after exact `dotnet --version`/`--list-sdks` readback. Require the resolved package plan to contain only `Microsoft.DotNet.SDK.10` for .NET, with no prior/preview SDK package, Framework, Visual Studio/MSBuild, or alternate installer path.
- Run the fresh native Herdr remote-provision gate against both a direct PATH
  executable and a launcher-backed installation when those distributions are
  available: require pre-cleanup unattended remote capability, exact fresh
  `started` and retained `reloaded|restarted` results, matching versioned sidecar,
  distribution/runtime/protocol/server identity, machine
  `HERDR_SANDBOX_HERDR_EXE` publication, initial workspaces, SSH remote attach,
  detach, persistent reuse, final host identity revalidation, and retry after a
  post-provision failure. Prove no bootstrap runtime copy, guest HTTP/WinGet
  download, direct server start, duplicate reload, or alternate executable route
  remains. Also prove that a Windows build reporting `unsupported` fails before
  cleanup with package-neutral capability guidance.
- Run one fresh native Sandbox configuration-sync gate with real, disposable test accounts/configuration for OpenCode, Claude Code, Codex, GitHub Copilot CLI, and Pi. Verify each standard guest destination, portable login behavior, GitHub CLI fallback for Copilot, retained-run additive updates, excluded runtime state, no credential content in output/run files, and the documented one-time reauthentication for machine-bound Codex/Copilot stores; then remove the README preview-validation note.
- Complete the sacrificial native acceptance gate before declaring automatic Tailscale identity restoration complete: enroll once, then launch two fresh Sandboxes sequentially from the DPAPI-protected state and require the same node key, control-plane device ID, IP, DNS name, fixed hostname, tags, and Windows user SID; verify local CLI access, independent peer connectivity, no concurrent clone, and no credential in mappings, logs, status, cache, or command lines. If exact state portability fails, remove the cloning path and separately scope ephemeral narrow-tag OAuth enrollment plus a Tailscale Service for stable TCP addressing rather than shipping two runtime paths.
- Publish `hdosys.herdr-win` as the next independent package, then verify
  `winget install hdosys.herdr-sandbox hdosys.herdr-win` against the community
  source with no package dependency or shared payload.
- Design intelligent lifecycle, refresh, and explicit reset management for durable Windows development VMs as a future product mode after the disposable Sandbox path is stable. Start with one concrete VM owner; define reset so it removes only that VM's proven mutable state while preserving user configuration, selected workspaces, and deliberately persistent caches; and preserve strict process, credential, filesystem, and terminal boundaries rather than introducing a provider framework first.
- Give the remaining native process owners in guest Base/bootstrap and unpublished visible Sandbox launch rollback explicit per-role deadlines and exact process-tree cleanup. Preserve the product-required visible Sandbox client/bootstrap and hidden noninteractive descendant consoles, avoid a second invocation framework, and verify hung children, console-allocating grandchildren, and failed-launch rollback in a real Sandbox before replacing those current direct boundaries.
- Consider checksum-validated Cargo `.crate` archive seeding only after the package/Rust mirror path passes natively. Keep Cargo index/Git databases, extracted sources, installed binaries, and target outputs guest-local because Cargo exposes no supported split-cache paths and trusts existing archives without rehashing them.
- Evaluate the Windows 11 24H2+ `wsb.exe` management API as a second implementation only if it removes lifecycle complexity without dropping supported Windows 10 behavior.
- Pin and verify managed external tool versions/hashes when moving beyond the fast MVP's stable package-manager path.
- Run a native persistent-worktree acceptance in a fresh Sandbox: configure one
  dedicated `worktreeDirectory`, create and use linked checkouts for multiple
  selected repositories through guest Herdr, restart with the same workspace and
  worktree mappings, then remove them through native Herdr/Git. Prove unrelated host
  content is not mapped and normal clean plus uninstall preserve the selected root.
