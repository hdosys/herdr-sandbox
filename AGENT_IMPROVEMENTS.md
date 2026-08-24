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

- **Status: done. Exercise required instruction filters through linked checkout.**
  Evidence: configuration sync marked the agent-instruction filter required but
  configured only its clean direction, so Git rejected a real linked-worktree
  checkout. The existing integration test now creates and removes a worktree after
  verifying clean-filter staging. Expected benefit: keep guest routing text out of
  commits while catching checkout-direction regressions before configuration sync.
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
- **Status: done. Add an in-place current-Sandbox provisioning gate.**
  Evidence: three native checks of one Base package boundary took 158.993, 149.192,
  and 405.072 seconds because `sandbox up --no-attach` continued into unrelated
  project stacks after reaching that boundary; the last run later hit an unrelated
  Visual Studio readiness timeout. A later 580.735-second integration pass was
  green but could not close eight native assignments from inside the development
  Sandbox. `native-current-sandbox` now executes real Base, package adapters, and
  every direct and virtual stack in the active guest while proving that SSH and
  Herdr process identities remain unchanged. A passing warm-cache run took
  488.701 seconds; the normal `verify` gate remained separate at 58.506 seconds.
  Expected benefit: real provisioning evidence without a nested Sandbox or slower
  standard iteration.
- **Status: done. Make the exact installed candidate a release acceptance owner.**
  Evidence: WinGet validation and community publication accepted a v0.0.15
  manifest whose ProductCode differed from the uninstall identity registered by
  its exact public installer; only a disposable installed-artifact check exposed
  the mismatch. Install the candidate, compare its registered identity with the
  manifest, run the focused native path through installed files, and quietly
  uninstall before publication. The package gate now rejects any checked-in
  WinGet ProductCode that differs from the canonical uninstall identity, while
  `package-current-sandbox` installs the exact candidate, checks its registered
  identity and payload, provisions through those installed files, preserves SSH,
  Herdr, and user configuration, then quietly uninstalls it. The first complete
  pass took 552.332 seconds. Expected benefit: catch packaging and installed
  lifecycle defects that compilation, source checks, and manifest syntax miss.
- **Status: done. Run the expensive installed-candidate gate only on the immutable commit.**
  Evidence: a pre-commit candidate passed `package-current-sandbox` in 587.203
  seconds but embedded the prior `HEAD` revision, so the unchanged product source
  needed another 622.120-second installed gate after commit. `AGENTS.md` now makes
  `package` the early candidate owner and permits `package-current-sandbox` only
  after production source is frozen, committed, pushed, and rebuilt from that
  immutable revision. Expected benefit: preserve early user testing while
  removing one roughly ten-minute duplicate install, provisioning, native, and
  uninstall cycle.
- **Status: done. Retire low-value source-coupled tests in favor of unique behavior signal.**
  Evidence: the repository has 18,112 lines of Go tests for 20,365 lines of
  production Go, including at least 68 source-only tests and roughly 2,600 lines
  centered on copied tokens, ordering, or implementation absence. Delete tests of
  test fixtures, external-tool semantics, copied source inventories, and wiring
  already exercised by a stronger owner; consolidate repeated table cases; retain
  product decisions, unsafe-boundary checks, and unique failure contracts. The
  completed pass removed 2,333 net test lines, including copied all-stack and
  provisioning inventories, while preserving executable PowerShell, parser,
  reparse, planner, process, configuration, installer, and native-boundary checks.
  It also removed 18 net production lines of no-op or test-only process and release
  machinery. The normal `verify` gate remained below one minute.
  Expected benefit: lower maintenance and cognitive cost while shifting confidence
  toward executable product and native outcomes.
- **Status: done. Retier Herdr identity-shape drift to the focused provision contract.**
  Evidence: a real run failed after 11 minutes because remote JSON reported the
  machine runtime identity while the validator expected human-readable version
  output; the prior fixture used the display identity in both places. The existing
  focused test now keeps those identities distinct and passed in 6.017 seconds.
  Expected benefit: catch this contract drift before expensive native provisioning
  and installer generation.
- **Status: proposed. Run the exact integration gate before creating release tags.**
  Evidence: v0.0.17 passed normal verification and the installed-candidate boundary,
  but its tagged GitHub run failed in the project-plan integration test and consumed
  the release ID. After correcting two ownership expectations, the exact local
  `verify-integration` gate passed in 449.277 seconds before v0.0.18 published
  successfully. Require one frozen-commit `verify-integration` run before the
  installed-candidate gate and tag, or expose one release-precheck task that invokes
  the same owner exactly once. Expected benefit: catch GitHub checked-build blockers
  locally before spending a release ID, an installed gate, and a remote run.
- **Status: done. Make first-install configuration an explicit installed-candidate contract.**
  Evidence: `package-current-sandbox` against an isolated initially empty `APPDATA`
  completed install, version, native reprovisioning, and quiet uninstall in 271.491
  seconds, then failed because setup correctly seeded `config.json` while the
  preservation checker expected continued absence. The checker now captures exact
  canonical create-if-missing files as the post-setup preservation baseline while
  retaining every pre-existing `config.json` and `user.ps1` hash. Focused tests
  passed, and the immutable `40426e5` candidate completed provisioning, native audio
  acceptance, configuration preservation, and quiet uninstall in 666.471 seconds.
  Expected benefit: the exact installed-candidate gate distinguishes legitimate
  first-install seeding from configuration loss.
- **Status: done. Fail fast on PowerShell automatic-variable collisions in isolation smokes.**
  Evidence: a one-off HyperFrames staging smoke assigned `$home`, which PowerShell
  resolves case-insensitively to the read-only `$HOME`; because the script did not
  stop on that assignment error, the following external command used the real agent
  roots instead of its temporary home. `AGENTS.md` now requires
  `$ErrorActionPreference = 'Stop'` before setup and purpose-specific names in
  PowerShell isolation smokes. Expected benefit: keep failed test setup from
  crossing into real configuration state.
- **Status: done. Add a fast current-environment provisioning preflight.**
  Evidence: four immutable `package-current-sandbox` attempts took 160.566,
  177.336, 183.399, and 188.623 seconds while serially exposing an injected
  AudioGridder encoding dependency, the installed OpenJDK release shape, the
  Android CLI version shape, and an invalid Visual Studio A/B descriptor. Each
  condition was available from existing files or subsecond commands before the
  full installed-candidate run. `provisioning-preflight` now reuses those
  production parsers before either current-Sandbox gate, completed against the
  active guest in 11.553 seconds, and keeps expensive installed acceptance in its
  explicit release or deferred tier. Expected benefit: preserve the real installer
  gate while avoiding repeated multi-minute runs for locally diagnosable inputs.
