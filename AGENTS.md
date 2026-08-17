# herdr-sandbox repository overlay

The global OpenCode working agreement owns reusable personal workflow. This file
owns only herdr-sandbox product, architecture, native-boundary, provisioning, and
verification rules; it overrides global defaults where they differ. Do not copy
the global workflow or recreate project `.opencode/` configuration.

## Canonical owners and precedence

- Code and tests own detailed implementation behavior.
- `README.md` owns practical public onboarding plus a portfolio-facing overview
  of product value, engineering decisions, and verification; detailed behavior
  and design remain canonical in the owners below.
- Before committing README changes, run one public-audience pass. Keep only copy
  that helps a reader act, understand user value, or assess the engineering;
  verification assignments, backlog status, and agent-process history stay in
  their canonical owners.
- `PRODUCT.md` owns stable user-visible CLI behavior, terminology, and acceptance
  outcomes.
- `ARCHITECTURE.md` owns technical boundaries, state, external integrations, and
  verification architecture.
- `SECURITY.md` owns the public threat model, supported security-reporting path,
  accepted security trade-offs, and release-verification guidance.
- `CHANGELOG.md` owns concise user-visible changes not yet or already grouped by
  release; GitHub Releases retain exact published artifact notes.
- `BACKLOG.md` owns open product work.
- `AGENT_IMPROVEMENTS.md` owns evidence-backed repository-specific agent, tooling,
  and test improvements.
- `.agent/sessions/<session-id>/` owns ignored task-local `TASK.md`, `STATE.md`, and
  `LOG.md` for long work.
- `.agent/deferred-verification/` owns ignored, workspace-persistent pending,
  running, completed, and blocked verifier assignments.

Do not invent another active memory owner or carry product facts/task history from
another repository into these files. After system/current-user instructions,
local precedence is this file, `PRODUCT.md`, `ARCHITECTURE.md`, active session
state, then `BACKLOG.md`.

Route durable decisions to exactly one owner above. Cross-project workflow belongs
in the global OpenCode configuration, not this repository.

## Repository and session boundaries

- Treat this directory as the repository root and work on `main`; stop for
  direction on another branch.
- Do not read or modify `reference/` or `references/` unless the user names the
  exact path.
- Never commit `.agent/`, generated run workspaces, logs, binaries, credentials,
  private keys/tokens, or local runtime state.
- Long/resumable work uses one `.agent/sessions/<session-id>/` unless
  `OPENCODE_SESSION_DIR` or `AGENT_SESSION_DIR` selects it. Never create root
  `TASK.md`, `STATE.md`, or `LOG.md`; never write another session's directory.

## Product and implementation invariants

- This project currently has no backward-compatibility contract. Do not add or
  retain legacy formats, layouts, versions, migrations, shims, aliases, fallback
  paths, cleanup bridges, or dual behavior. A future exception requires an
  explicit current-user decision and a matching canonical product or architecture
  contract in the same milestone; historical releases alone never authorize it.
- Agent-invoked host console subprocesses must not create visible console windows.
  Capture output/exit status through a hidden process-tree console inherited by
  descendants; a consoleless parent is insufficient for toolchains that spawn
  console grandchildren.
- Agent-invoked noninteractive guest console processes use hidden-window options,
  including Windows PowerShell 5.1 `-WindowStyle Hidden`. Only a real interactive
  terminal check and the product-required visible Sandbox bootstrap console may be
  visible.
- Hidden-window behavior belongs to the launched process path, not a user-managed
  setting or alternate mode.
- Keep a ready app-owned Sandbox alive while iterating on provisioning or shell
  integration. Exercise changed deployment paths idempotently in that guest;
  replace it only for final reproducibility or when the isolation boundary changes.
  Preserve a ready guest for user-observed UI acceptance before replacement.
- Treat every ready Sandbox as active production state. Never close, restart, or
  replace it without explicit approval from the current user; report or defer a
  fresh-native gate instead of consuming that instance.
- Executable wiring belongs under `cmd/`, testable behavior under `internal/`, and
  recurring repository automation under `cmd/task`.
- Start standard-library-only; prefer pure Go and avoid CGO/helper runtimes unless
  the required capability demands them.
- Use `context.Context` for subprocesses/long operations, propagate cancellation,
  and wrap errors with `%w` at responsibility boundaries.
- Use the standard `flag` package until real CLI complexity justifies a framework.
- Native/external process invocation must be explicit, bounded, and diagnosable;
  never silently switch executable, endpoint, credential, or compatibility path.

## Host/guest tool boundaries

### Rust stays off the host

- Never install or invoke host `rustup`, `rustc`, `cargo`, or `just` recipes that
  execute Rust. Host checkouts are source-inspection/patch-preparation only.
- The proven Windows Sandbox/Herdr boundary may install and run the pinned Rust
  toolchain inside the isolated guest through this repository's provisioning path.
- GitHub Actions owns release artifacts and is the fallback when guest provisioning
  is unavailable; host Rust is never a substitute.

### Provisioning is Windows PowerShell 5.1 only

- All repository-owned Windows/guest scripts use PowerShell; do not add or generate
  `.cmd`/`.bat` files. `cmd/task` is Go source, not permission for CMD scripts.
- Windows PowerShell 5.1 exclusively owns bootstrap, Base, project/stack scripts,
  configuration sync, parser adapters, and verification.
- Installed PowerShell 7 is interactive guest tooling only and provisioning must
  never invoke `pwsh.exe`.
- Prefer direct Go process execution for application behavior and PowerShell for
  Windows-specific orchestration; do not add Makefiles or alternate shell task
  systems.

## Sandbox and security boundaries

- Keep host mappings narrow. Bootstrap/provisioning input is read-only; only the
  explicit per-run status directory, app-owned package/tool cache, project roots,
  and explicitly configured named folder mounts may be guest-writable. Folder
  mounts require an exact read-only selection and remain outside workspace state.
- Never map host home/app data, credentials, private SSH keys, or a parent broader
  than the exact project or generic folder root selected by the user.
- Approved coding-agent config/authentication may be streamed only over the
  verified SSH channel; never stage it in host run state, log it, commit it, or
  scrape machine-bound keyring credentials.
- Only the host SSH public key may enter the guest. Private keys remain on host.
- After the current user explicitly requests a lifecycle action that closes or
  replaces a ready guest, the product may force-terminate only the exact
  revalidated app-owned Sandbox client/process tree after preserving terminal
  status. This avoids Windows Sandbox confirmation prompts without granting an
  agent authority to consume a ready guest on its own. Never extend this permission
  to unrelated processes, VMs, evidence/cache, or user data.
- Network, firewall, SSH, package installation, and tunnel behavior require native
  evidence. Use stable `build/bin/` executable paths; do not switch to random
  `go run` binaries after approval.
- Herdr/TUI attach success requires a real interactive terminal with console-backed
  stdin/stdout/stderr. Redirected rejection, captured ANSI bytes, blank terminals,
  or a live process chain are not success.

## Verification

Use repository-owned tasks and the smallest ladder covering the change:

1. Focused pure tests for parsing, rendering, status, and quoting.
2. `go run ./cmd/task fmt`, the fast product-focused `go run ./cmd/task test`, and
   applicable Windows PowerShell 5.1 syntax checks.
3. `go run ./cmd/task build` and the stable CLI under `build/bin` when binary
   behavior changed.
4. `go run ./cmd/task native-all-stacks` for the opt-in real Windows Sandbox +
   WinGet + all-stack + guest Herdr server + managed SSH smoke, followed by a
   real interactive remote-attach check when attach behavior changed.

`go run ./cmd/task verify` is the normal iteration gate covering formatting, a
clean Go modernization diff, pinned Staticcheck with all checks enabled, the
pinned Go nilness analyzer, one batched PowerShell parse, fast product tests,
`go vet`, and the stable build artifact. It must remain within a five-minute hard
deadline and should normally finish within three minutes. `go run ./cmd/task test-integration` and
`go run ./cmd/task verify-integration` own external PowerShell/Git execution and
the full nightly/release matrix. Do not run them while a user waits for an
ordinary build or installer. Unit tests do not replace the native gate when the
changed behavior depends on the boundary.

For long-running GitHub Actions, check status no more than once every two minutes
and fetch detailed logs only after terminal failure. GitHub release artifacts do
not replace required local/guest evidence unless the boundary is blocked and
reported.

After submitting a WinGet community update, perform one post-submission status
read and report any remaining Microsoft-owned stages. Do not keep an interactive
watch open while those external checks run.
