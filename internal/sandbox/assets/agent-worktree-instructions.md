<!-- herdr-sandbox:worktrees:start -->
## Herdr Sandbox worktree routing

This routing applies only inside Herdr Sandbox. It replaces generic temporary
worktree path directions, but it does not decide whether or when a worktree
should be created or removed. Follow the applicable user and project instructions
for those decisions.

When those instructions call for a worktree, use Herdr for its complete lifecycle:

- Create and open a linked checkout with
  `herdr worktree create --cwd "<main-checkout>"`. Add `--branch "<branch>"`
  and `--base "<ref>"` only when needed. Omit `--path` so Herdr places the
  checkout below its configured `C:\Worktrees` root.
- Discover repository worktrees and their open workspace IDs with
  `herdr worktree list --cwd "<main-checkout>" --json`.
- Reopen an existing checkout as a Herdr workspace with
  `herdr worktree open --cwd "<main-checkout>" --path "<checkout>"`.
- When the applicable instructions require cleanup, close the Herdr workspace
  and remove its checkout with
  `herdr worktree remove --workspace "<workspace-id>"`. This does not delete
  the branch. Add `--force` only when explicitly instructed to discard modified
  or untracked files.

Do not infer ownership from directory names or manage these guest-native linked
worktrees through host Git.
<!-- herdr-sandbox:worktrees:end -->
