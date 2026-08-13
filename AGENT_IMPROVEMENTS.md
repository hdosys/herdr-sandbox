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

- **Status: proposed. Centralize extracted PowerShell function test setup.**
  Evidence: adding the shared tool-version helper left two focused Windows
  PowerShell 5.1 harnesses with incomplete manually listed dependencies; only the
  first full repository gate exposed both omissions. Add one test helper that
  extracts an explicit production function set and initializes shared provisioning
  context consistently. Expected benefit: dependency drift fails in the focused
  test that owns the changed function and avoids repeated harness repair.
- **Status: done. Compare canonical Windows paths with Windows semantics.**
  Evidence: the complete repository gate is blocked by
  `TestResolveProvisioningIncludesDedicatedWorktreeDirectory` comparing a lowercase
  drive letter with its canonical uppercase form case-sensitively. Use one shared
  test assertion that cleans and compares Windows paths case-insensitively.
  Expected benefit: valid drive-letter casing no longer masks behavioral gate
  results.
