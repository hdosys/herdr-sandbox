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

- **Status: proposed. Emit commit-keyed native acceptance evidence.**
  Evidence: the v0.0.13 release audit could not bind prior native coverage to
  the frozen candidate commit and had to treat `native-all-stacks` as missing.
  Have the native task emit one bounded structured summary containing the exact
  commit, platform, command, terminal result, and key acceptance boundaries.
  Expected benefit: release audits can distinguish current evidence from prose
  history without adding another persistent state owner.
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
