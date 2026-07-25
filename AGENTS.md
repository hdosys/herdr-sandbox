# AGENTS.md

## Purpose

Repository contract for agents working on this Go CLI. The default loop is: find the current owner, implement the smallest real path, keep hard contracts at unsafe boundaries, delete replaced paths, verify the shared path, and report net complexity.

This repository intentionally starts lean. Do not import generic template machinery, compatibility layers, or infrastructure from the predecessor application unless a current requirement makes it useful and it removes more complexity than it adds.

## Sources Of Truth

- Code and tests own detailed implementation behavior.
- `PRODUCT.md` owns stable user-visible CLI behavior, terminology, and acceptance outcomes.
- `ARCHITECTURE.md` owns stable technical boundaries, state, external integrations, and verification architecture.
- `BACKLOG.md` owns open product work only.
- `AGENT_IMPROVEMENTS.md` owns evidence-backed agent/tooling/process proposals.
- `.agent/sessions/<session-id>/` owns ignored task-local `TASK.md`, `STATE.md`, and `LOG.md` for long work.

Do not invent additional active memory files without explicit approval. Do not carry product facts or task history from another repository into these owners.

## Precedence And Safety

1. Platform/system safety and the current user.
2. This file.
3. `PRODUCT.md` for user-visible behavior.
4. `ARCHITECTURE.md` for technical decisions.
5. Active session state.
6. `BACKLOG.md`.

- Treat this directory as the repository root.
- Preserve unrelated and shared-worktree changes. Never reset, restore, clean, rebase, delete, or overwrite user work without explicit approval.
- Never read or modify `reference/` or `references/` unless the user names the exact path.
- Never commit secrets, private keys, tokens, local runtime state, `.agent/`, generated run workspaces, logs, or binaries.
- Work on `main` unless the user explicitly requests another branch. Stop if an existing repository is unexpectedly on another branch.
- Never force-push, bypass hooks, create an upstream, or resolve a non-fast-forward implicitly.
- Launch agent-invoked host console subprocesses without creating visible console windows; capture their output and exit status through a hidden process-tree console that descendants inherit. Do not use a consoleless parent for toolchains that can spawn console grandchildren, because those grandchildren may allocate visible windows. Agent-invoked noninteractive guest console subprocesses must likewise use their hidden-window option, including Windows PowerShell 5.1 `-WindowStyle Hidden`; only a real interactive terminal check may be visible. This does not hide the product-required visible bootstrap console inside Windows Sandbox.

## Default Workflow

`direct-builder` owns normal work from discovery through implementation, verification, durable-decision routing, commit, and push. `worker` is for bounded discovery or isolated preparation. `verifier` is for independent read-only checks, slow test suites, and native evidence. Delegated work replaces duplicate inline work.

Background-agent completion notifications are not a scheduler or wake-up mechanism. Never delegate a critical-path native run, terminal gate, status-file transition, or final delivery blocker to the background and then wait for notification. Keep direct ownership with foreground execution or a bounded OS/file-event wait, inspect terminal state immediately when it changes, and continue without requiring a user prompt. Background agents are allowed only for independent, non-gating work that cannot stall the next action.

Keep a ready app-owned Sandbox alive while iterating on provisioning or shell integration. Exercise changed deployment paths idempotently inside that existing guest until they pass; create a fresh Sandbox only for the final reproducibility gate or when the isolation boundary itself must change. When a UI acceptance outcome requires human observation, preserve the ready guest, state exactly what is ready to inspect, and let the user verify it before replacement.

Parallelize independent discovery, preparation, and review with multiple subagents when it reduces wall time or protects the main context. Keep one cross-layer responsibility, shared-worktree edits, final diff/status review, and every critical-path decision under the direct builder; delegated evidence replaces rather than duplicates an equivalent inline pass.

Within an approved product scope, work autonomously through routine implementation, CI repair, commit, and delivery choices. Ask the user only for a genuine product decision, an explicitly destructive action, or ambiguity that cannot be resolved from current owners and evidence. Do not interrupt delivery for routine commit-message approval or mechanical verification fixes.

Before nontrivial implementation, record a compact Simplicity Check:

1. Current owner/path.
2. Files, contracts, and state touched.
3. Reuse, consolidation, or deletion plan.
4. Why a smaller change is insufficient.

Inspect existing owners before creating packages, services, status formats, configuration, helper layers, or alternate execution paths. Prefer one concrete verified happy path. Add abstractions only after two real implementations make duplication costly and the abstraction removes net code or decisions.

For every nontrivial project continuation, create a visible task list before implementation and keep it current as work changes. Mark each completed item immediately after its implementation and verification; never report the project slice complete while a task remains pending or in progress.

## Long Work And Resume

For work likely to cross compaction, span multiple phases, or require handoff, use one unique `.agent/sessions/<session-id>/` directory unless `OPENCODE_SESSION_DIR` or `AGENT_SESSION_DIR` selects it.

After compaction or a long interruption:

1. Re-read this file from disk.
2. Read the active session's `TASK.md`, `STATE.md`, and `LOG.md`.
3. Re-read relevant product and architecture sections.
4. Inspect current branch, status, and relevant diff.
5. Continue only from the exact recorded next action.

Never store credentials, private keys, private user data, raw dumps, full logs, or transcripts in session files.

## Durable Decisions

Persist durable user guidance automatically:

- Agent workflow/safety/quality rule -> `AGENTS.md`.
- User-visible behavior/terminology/acceptance outcome -> `PRODUCT.md`.
- Technical owner/boundary/state/integration decision -> `ARCHITECTURE.md`.
- Deferred product work -> `BACKLOG.md`.
- Agent/tool/test improvement proposal -> `AGENT_IMPROVEMENTS.md`.

Keep each decision in one canonical owner. Keep task progress out of permanent product documentation.

## Go And Automation Defaults

- Keep executable wiring under `cmd/`, testable behavior under `internal/`, and recurring repository automation under `cmd/task`.
- Start standard-library-only. Prefer pure Go and avoid CGO or helper runtimes unless the required capability demands them.
- Use `context.Context` for subprocesses and long operations. Propagate cancellation and wrap errors with `%w` at responsibility boundaries.
- Use the standard `flag` package until real CLI complexity justifies a framework.
- Keep native/external process invocation explicit, bounded, and diagnosable. Do not silently fall back to unrelated executables, endpoints, credentials, or compatibility paths.
- Inspect current official documentation or exact installed/versioned source before relying on Windows Sandbox, WinGet, Herdr, OpenSSH, OpenCode, or another external CLI contract.

### Rust tooling stays off the host

- Never install or invoke Rust tooling on the host system, including `rustup`, `rustc`, `cargo`, or `just` recipes that execute Rust tools. A host checkout may be used for source inspection and patch preparation only.
- The proven Windows Sandbox/Herdr boundary may install and invoke the pinned Rust toolchain inside the isolated guest. Guest builds and tests are allowed only through the explicit `herdr-sandbox` project provisioning path and must preserve native evidence.
- GitHub Actions remains the owner of release artifacts and the fallback when guest provisioning is unavailable. Do not treat a host Rust execution as a substitute for either path.

### PowerShell-only scripting

- All repository-owned Windows and guest scripting uses PowerShell. Do not add `.cmd` or `.bat` files or generate CMD/batch scripts.
- `cmd/task` is Go source for repository tasks; its name does not authorize Windows CMD scripting.
- Windows PowerShell 5.1 is the exclusive interpreter for the entire host/guest provisioning lifecycle, including bootstrap, Base, project/stack scripts, configuration sync, parser adapters, and verification. Installed PowerShell 7 is only the interactive shell for agents through Herdr, OpenSSH, and Windows Terminal; provisioning code must never invoke `pwsh.exe`.
- Prefer direct Go process execution for application behavior and PowerShell for Windows-specific orchestration. Do not add Makefiles or alternate shell task systems.

## Sandbox And Security Boundaries

- Keep host mappings narrow. Bootstrap/provisioning input is read-only; only the explicit per-run status directory, the app-owned package/tool cache, and project roots selected by `%APPDATA%\herdr-sandbox\config.json` or the nearest `.herdr-sandbox\provision.ps1` may be guest-writable.
- Never map the host home directory, app data, credentials, private SSH key, unrelated repositories, or a parent containing more than an explicitly selected project root. The required OpenCode configuration/auth archive may be streamed only over the verified SSH channel into the guest; it is never staged, logged, or committed.
- Never place the host private SSH key in a Sandbox mapping. Only its public key may enter the guest.
- Repository-owned Windows Sandbox instances may be closed and replaced autonomously during implementation and verification after preserving terminal run status. Never ask for routine close/replacement approval; if an orderly close fails, the agent may force-terminate only the exact revalidated app-owned Sandbox client and its process tree. Do not delete run evidence or cache, and do not apply this permission to unrelated processes, VMs, or user data.
- Network, firewall, SSH, package installation, and tunnel behavior require realistic native evidence before the core path is called complete. Unit tests alone are not enough.
- Use stable `build/bin/` executable paths for network/firewall-sensitive manual checks; do not switch to random `go run` binaries after approval.
- Native Herdr/TUI attach checks must run in a real interactive terminal with console-backed stdin, stdout, and stderr. Redirected streams may test explicit rejection only; ANSI bytes in a file, a blank terminal, or a live process chain is not attach success.

## Verification And Delivery

Use the smallest non-duplicative ladder covering the changed behavior:

1. Focused pure tests for parsing, rendering, status contracts, and quoting.
2. `go test ./...`, `go vet ./...`, formatting, and PowerShell syntax validation as applicable.
3. Stable CLI build under `build/bin/`.
4. Opt-in real Windows Sandbox + WinGet + guest Herdr server + SSH remote-attach smoke for the core path.

Tests should exercise the shared implementation path rather than a second fake product. Report exact commands and blockers when native evidence cannot run.

For long-running remote jobs such as GitHub Actions, do not use high-frequency watch loops. Check status no more than once every two minutes, and fetch detailed logs only after a job reaches a terminal failure state.

In a Git repository, inspect status, intended diff, and recent log before committing. Stage only task-owned files, commit coherent verified milestones with the repository's style, and push immediately to the configured upstream. If there is no upstream or push is blocked, stop and report the evidence; never create one implicitly.

Final responses include changed files/docs, verification, net complexity, artifact path when applicable, commit/push result, unresolved risk, next action, and session/checkpoint path when used.
