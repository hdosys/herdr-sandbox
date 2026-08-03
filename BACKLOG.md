# BACKLOG.md

Open, planned, blocked, or deferred product work only. User-visible rules belong in `PRODUCT.md`; technical decisions belong in `ARCHITECTURE.md`; workflow/tooling proposals belong in `AGENT_IMPROVEMENTS.md`.

## Rules

- Keep items actionable and current.
- Remove completed or obsolete items instead of preserving history.
- Include expected verification when known.
- Do not use this file for task logs.

## Items

- Run one fresh native acceptance gate for the new current-path UX and modern .NET owner when `Containers-DisposableClientVM` is available: use the stable built CLI to confirm `plan` remains nonmutating, initialize a disposable `dotnet` profile, provision with `up --no-attach`, observe retained progress and prompt `status` snapshots, verify a bounded terminal operation outcome independently of ready health, attach from a real console, and build/run a minimal `net10.0` project after exact `dotnet --version`/`--list-sdks` readback. Require the resolved package plan to contain only `Microsoft.DotNet.SDK.10` for .NET—no prior/preview SDK package, Framework, Visual Studio/MSBuild, or alternate installer path.
- Run the fresh native Herdr dependency gate against both a direct PATH executable and a launcher-backed installation when those distributions are available: require pre-cleanup remote capability, a digest-identical host-to-guest physical executable plus optional complete ConPTY bundle with no Herdr network/cache request, Application lookup of the copied `herdr.exe` through the real SSH user's guest `PATH`, final host identity revalidation, matching version/protocol, SSH remote attach, detach, and persistent server reuse. Also prove that a Windows build reporting `unsupported` fails before cleanup with package-neutral capability guidance.
- Run one fresh native Sandbox configuration-sync gate with real, disposable test accounts/configuration for OpenCode, Claude Code, Codex, GitHub Copilot CLI, and Pi. Verify each standard guest destination, portable login behavior, GitHub CLI fallback for Copilot, retained-run additive updates, excluded runtime state, no credential content in output/run files, and the documented one-time reauthentication for machine-bound Codex/Copilot stores; then remove the README preview-validation note.
- Complete the sacrificial native acceptance gate before declaring automatic Tailscale identity restoration complete: enroll once, then launch two fresh Sandboxes sequentially from the DPAPI-protected state and require the same node key, control-plane device ID, IP, DNS name, fixed hostname, tags, and Windows user SID; verify local CLI access, independent peer connectivity, no concurrent clone, and no credential in mappings, logs, status, cache, or command lines. If exact state portability fails, remove the cloning path and separately scope ephemeral narrow-tag OAuth enrollment plus a Tailscale Service for stable TCP addressing rather than shipping two runtime paths.
- Submit the prepared schema-1.12 `hdosys.herdr-sandbox` manifests from `packaging/winget`; release `v0.0.7` supplies the public URL/hash and passed the exact published-installer gate. Prepare `hdosys.herdr-win` as the next independent package, then verify `winget install hdosys.herdr-sandbox hdosys.herdr-win` against the community source with no package dependency or shared payload.
- Design intelligent lifecycle, refresh, and explicit reset management for durable Windows development VMs as a future product mode after the disposable Sandbox path is stable. Start with one concrete VM owner; define reset so it removes only that VM's proven mutable state while preserving user configuration, selected workspaces, and deliberately persistent caches; and preserve strict process, credential, filesystem, and terminal boundaries rather than introducing a provider framework first.
- Give the remaining native process owners in guest Base/bootstrap and unpublished visible Sandbox launch rollback explicit per-role deadlines and exact process-tree cleanup. Preserve the product-required visible Sandbox client/bootstrap and hidden noninteractive descendant consoles, avoid a second invocation framework, and verify hung children, console-allocating grandchildren, and failed-launch rollback in a real Sandbox before replacing those current direct boundaries.
- Consider checksum-validated Cargo `.crate` archive seeding only after the package/Rust mirror path passes natively. Keep Cargo index/Git databases, extracted sources, installed binaries, and target outputs guest-local because Cargo exposes no supported split-cache paths and trusts existing archives without rehashing them.
- Evaluate the Windows 11 24H2+ `wsb.exe` management API as a second implementation only if it removes lifecycle complexity without dropping supported Windows 10 behavior.
- Pin and verify managed external tool versions/hashes when moving beyond the fast MVP's stable package-manager path.
- Design an optional persistent-worktree mode for public-project users whose
  workflow needs long-lived linked checkouts instead of the default shared checkout
  plus disposable integration fallback. Add one explicit absolute
  `worktreeDirectory` setting, map that dedicated root once at Sandbox launch to a
  fixed writable guest root so repository/session children can be created without
  another Sandbox restart, and never map a broader parent, home, AppData, cache, or
  unrelated directory implicitly. Define strict physical-path/non-reparse and
  non-overlap validation, per-repository/session identity, active-owner leases,
  crash recovery, startup/terminal garbage collection through `git worktree`, and
  preservation across `clean` and uninstall. Native verification must create and
  use worktrees for multiple selected repositories after launch, retain active and
  recover abandoned work, remove completed trees/branches plus stale metadata, and
  prove unrelated host content is neither mapped nor deleted.
