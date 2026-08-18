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
- Status values: proposed, accepted, declined, done.

## Proposals

- **Status: done. Add a public-audience pass to README review.**
  Evidence: `AGENTS.md` now requires every README change to retain only reader
  action, user value, or concise engineering evidence and routes workflow status
  to its canonical owner. Expected benefit: prevent process leakage without
  stripping useful portfolio-level engineering evidence.
- **Status: proposed. Validate GitHub README video embeds with the target renderer.**
  Evidence: a repository-relative MP4 passed media checks but rendered only as a
  linked preview, while the native player required a bare GitHub user-attachment
  URL. A WebP fallback added conversion time and produced an unusable 130 MB
  artifact. Add a focused `gh api markdown` check for one controlled video element
  before committing README video changes. Expected benefit: catch non-playing
  embeds before push and avoid unnecessary conversion work.
- **Status: done. Bound WinGet validation watching below the session watchdog.**
  Evidence: `AGENTS.md` now requires one post-submission status read and reporting
  of remaining Microsoft-owned stages instead of an interactive watch. Expected
  benefit: preserve accurate status without a long process wait.
- **Status: done. Mark `build` as intermediate and `package` as the candidate artifact.**
  Evidence: task help and successful output identify `build/bin` as intermediate,
  while package evidence identifies the validated ZIP and installer as candidate
  artifacts. Expected benefit: command output reinforces the installer-first
  workflow without another build path.
- **Status: done. Emit commit-keyed native acceptance evidence.**
  Evidence: `native-all-stacks` now requires a clean committed tree and emits one
  JSON summary with commit, platform, command, terminal result, and covered native
  boundaries. A focused test owns the schema. Expected benefit: release audits can
  bind native evidence to one immutable source snapshot without another file owner.
- **Status: done. Emit structured package artifact evidence.**
  Evidence: the package task emits one JSON object per published artifact with its
  clean path, byte count, and lowercase SHA-256. A focused test validates the exact
  fields and artifact order. Expected benefit: closeout no longer needs ad hoc
  evidence scripts or manual hash transcription.
- **Status: done. Centralize extracted PowerShell function test setup.**
  Evidence: the NSIS, online WinGet, and TradingView metadata regressions share one
  helper that extracts each explicit production function set and initializes the
  tool-version context consistently. Expected benefit: dependency drift fails in
  the focused test that owns the changed function.
- **Status: done. Normalize Windows CI evidence before comparison.**
  Evidence: Nightly exposed both PowerShell console-width line wrapping in three
  diagnostic assertions and short-versus-long path aliases in
  `TestResolveProvisioningIncludesDedicatedWorktreeDirectory`. Collapse diagnostic
  whitespace and canonicalize the expected mapped directory with the production
  path owner before comparison. Expected benefit: presentation-only output and
  valid Windows path aliases no longer mask behavioral gate results.
- **Status: done. Publish process-tree fixture PIDs atomically.**
  Evidence: the process-tree fixture writes its PID to a same-directory temporary
  file and renames it into place before the reader can observe it. Repeated focused
  execution passes. Expected benefit: deterministic process-tree tests without
  masking product verification.
