# AGENT_IMPROVEMENTS.md

Evidence-backed proposals for making future agent work faster, cheaper, safer, or more reliable.

This is not product backlog or task history. Product work belongs in `BACKLOG.md`;
user-visible behavior in `PRODUCT.md`; repository-specific design in
`ARCHITECTURE.md`; accepted herdr-sandbox-specific agent rules in `AGENTS.md`;
cross-project workflow in the global OpenCode configuration repository.

## Rules

- Add only concrete proposals likely to help future work.
- Keep entries short and evidence-based.
- Merge duplicates instead of appending repeats.
- Do not include secrets, private data, logs, transcripts, or product feature requests.
- Keep only open entries with **Status: proposed**; remove resolved entries
  instead of preserving history.

## Proposals

- **Status: proposed. Preflight native UI input and capture diagnostics inside the checker.**
  Evidence: the workstation wizard rendered correctly, but Session 1/Default
  returned no foreground window and refused activation. A Task Scheduler attempt
  returned exit 1 without child diagnostics; in-child transcription exposed the
  actual input-route failure. Reuse the bounded native interactive adapter with
  explicit child logging and an input-desktop/owned-focus check before keyboard
  assertions. Distinguish message-level dialog checks from physical SendInput.
  Expected benefit: avoid repeated blocked GUI attempts and unsupported input
  acceptance claims without adding another launcher or background service.

- **Status: proposed. Establish a bounded real SSH smoke before lifecycle rewrites.**
  Evidence: local PowerShell receiver tests passed while the real SSH check
  stalled; inherited Win32-OpenSSH descriptor metadata was isolated only after
  repeated long attempts. A temporary runner also waited on unbounded output
  reads after killing its child. Start transport work with one small framed-input
  smoke in the actual calling environment; give startup, execution, output drain,
  and owned-tree cleanup a single finite total budget. Exercise the exact output
  API used by the application: a promoted `exec.Cmd.Output` bypassed the owned
  suspended-start/resume and cancellation path, despite lower-level runner checks.
  A deadline is not evidence that the guest installer or its descendants stopped.
  Expected benefit: expose environment-specific failures before cross-layer edits
  and avoid lengthy waits
  without a usable installer.

- **Status: proposed. Gate repeated long acceptance runs on a focused causal check.**
  Evidence: repeated installer runs lasted 18 to 45 minutes while shared startup,
  configuration and process-completion defects remained unresolved. After a failed
  long run, require the last verified phase, the failing responsibility owner, one
  discriminating check, and confirmation that the previous owned execution ended.
  Run the complete acceptance path again only after the demonstrated correction
  passes its focused check. Timebox startup, diagnosis and acceptance separately,
  with evidence capture and owned cleanup defined before execution. Reaching a
  deadline changes the next action; it does not trigger another identical run.
  Use a retained installation only for the smallest causal check or an explicit
  recovery scenario, not repeated full acceptance rounds. Repeated installations
  grew the Sandbox disk and accumulated gigabyte-sized input copies. Prefer a
  fresh Sandbox for full installer acceptance when replacement is authorized;
  promptly remove terminal task-owned input copies on both sides.
  Expected benefit: avoid hour-scale diagnostic loops and overlapping installers.
  Routing: retained here at the user's request for later adoption by the global
  OpenCode workflow owner; no global files changed.

- **Status: proposed. Keep prepared inputs immutable during a correction round.**
  Evidence: rebuilding installer code re-resolved package versions, introducing a
  changed Nushell manifest scope and additional downloads during unrelated fixes.
  Bind a correction round to the existing verified dependency snapshot and reuse
  it while changing code. Refresh dependencies only as an explicit new input
  milestone, not as a side effect of every rebuild. Retain integrity validation;
  do not substitute stale-cache fallbacks or permanently pin global defaults.
  Expected benefit: reproducible failures, fewer downloads and faster rechecks.
  Routing: proposed global workflow refinement, stored locally until the authorized
  global owner can reconcile it with provisioning-version rules.

- **Status: proposed. Prove shared-path parity before adding a delivery variant.**
  Evidence: the private installer initially used direct package installation and
  raw configuration copies instead of the existing cache adapters, target
  transformations and credential exporters. Installation/version checks passed
  while shell starts and authentication remained unusable. Identify the existing
  input-to-effect owner and the exact allowed differences first; implement transport
  differences around it. Include one real user-facing configuration/authentication
  journey in initial acceptance, not just source hashes or producer success reports.
  Expected benefit: prevent a second implementation and repeated per-tool repairs.
  Routing: proposed global outcome/reuse rule; this file is temporary proposal
  storage, not a second canonical workflow policy.

- **Status: proposed. Verify external distribution identities before rename edits.**
  Evidence: the Herdr rename changed the version marker and repository name but
  retained WinGet ID `hdosys.herdr-win`; assuming all identities changed required
  a documentation correction after the candidate build. Check the distribution's
  canonical rename guidance once before editing package IDs or display names,
  while keeping the demonstrated parser fix direct. Expected benefit: avoid
  incorrect installation instructions and an extra review/commit loop.

- **Status: proposed. Validate GitHub README video embeds with the target renderer.**
  Evidence: a repository-relative MP4 passed media checks but rendered only as a
  linked preview, while the native player required a bare GitHub user-attachment
  URL. A WebP fallback added conversion time and produced an unusable 130 MB
  artifact. Add a focused `gh api markdown` check for one controlled video element
  before committing README video changes. Expected benefit: catch non-playing
  embeds before push and avoid unnecessary conversion work.
- **Status: proposed. Preflight WinGetCreate authentication during the release build.**
  Evidence: an otherwise ready WinGet update waited five minutes in an unseen
  device-login prompt before timing out, while the existing authenticated GitHub
  CLI account completed submission immediately after secure local credential
  bridging. Add a bounded preflight that either confirms cached authorization or
  selects the supported `WINGET_CREATE_GITHUB_TOKEN` child-process environment
  handoff from existing GitHub CLI authorization, never a token command argument.
  In the v0.0.25 release, generation then completed in 8.9 seconds without login
  interaction, but tool discovery ran after a 183-second workflow watch. Prepare
  the tool and authorization while GitHub packages the release. Expected benefit:
  remove serial setup and prevent another hidden device-login wait.
