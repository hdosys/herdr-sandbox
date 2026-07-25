# BACKLOG.md

Open, planned, blocked, or deferred product work only. User-visible rules belong in `PRODUCT.md`; technical decisions belong in `ARCHITECTURE.md`; workflow/tooling proposals belong in `AGENT_IMPROVEMENTS.md`.

## Rules

- Keep items actionable and current.
- Remove completed or obsolete items instead of preserving history.
- Include expected verification when known.
- Do not use this file for task logs.

## Items

- Bring the current three-workspace fresh cache-hit Development phase below the five-minute product target without adding a second provisioner or weakening verification. Native Contract 29 evidence measured 339.5 seconds; profile the concrete Visual Studio (136.3s), Go (55.6s), Wails/govulncheck (32.1s), and Python (23.0s) owners, then verify the result in a new fresh Sandbox with the same populated cache and workspace plan.
- Consider checksum-validated Cargo `.crate` archive seeding only after the package/Rust mirror path passes natively. Keep Cargo index/Git databases, extracted sources, installed binaries, and target outputs guest-local because Cargo exposes no supported split-cache paths and trusts existing archives without rehashing them.
- Evaluate the Windows 11 24H2+ `wsb.exe` management API as a second implementation only if it removes lifecycle complexity without dropping supported Windows 10 behavior.
- Pin and verify managed external tool versions/hashes when moving beyond the fast MVP's stable package-manager path.
