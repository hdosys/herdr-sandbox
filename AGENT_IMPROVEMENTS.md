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

- **Status: proposed. Validate GitHub README video embeds with the target renderer.**
  Evidence: a repository-relative MP4 passed media checks but rendered only as a
  linked preview, while the native player required a bare GitHub user-attachment
  URL. A WebP fallback added conversion time and produced an unusable 130 MB
  artifact. Add a focused `gh api markdown` check for one controlled video element
  before committing README video changes. Expected benefit: catch non-playing
  embeds before push and avoid unnecessary conversion work.
