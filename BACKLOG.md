# BACKLOG.md

Open, planned, blocked, or deferred product work only. User-visible rules belong in `PRODUCT.md`; technical decisions belong in `ARCHITECTURE.md`; workflow/tooling proposals belong in `AGENT_IMPROVEMENTS.md`.

## Rules

- Keep items actionable and current.
- Remove completed or obsolete items instead of preserving history.
- Include expected verification when known.
- Do not use this file for task logs.

## Items

- Complete the sacrificial native acceptance gate before declaring automatic Tailscale identity restoration complete: enroll once, then launch two fresh Sandboxes sequentially from the DPAPI-protected state and require the same node key, control-plane device ID, IP, DNS name, fixed hostname, tags, and Windows user SID; verify local CLI access, independent peer connectivity, no concurrent clone, and no credential in mappings, logs, status, cache, or command lines. If exact state portability fails, remove the cloning path and separately scope ephemeral narrow-tag OAuth enrollment plus a Tailscale Service for stable TCP addressing rather than shipping two runtime paths.
- Select and add the next out-of-the-box coding-agent integration after OpenCode, with an explicit configuration/authentication allowlist, verified in-memory SSH transfer, guest read-back, and focused tests. Do not broaden host mappings or introduce a generic agent/plugin framework.
- Coordinate one exact `herdr-sandbox` release into the `herdr-win` repository's combined Windows installer and publish that single assembled package through WinGet without a WinGet package dependency. Only recommend it after installation and upgrade verify the paired Herdr/Sandbox versions; this repository must continue to publish only Sandbox-owned files and never republish `herdr.exe`.
- Design intelligent lifecycle and refresh management for durable Windows development VMs as a future product mode after the disposable Sandbox path is stable. Start with one concrete VM owner and preserve strict process, credential, filesystem, and terminal boundaries rather than introducing a provider framework first.
- Bring the current three-workspace fresh cache-hit Development phase below the five-minute product target without adding a second provisioner or weakening verification. Native Contract 29 evidence measured 339.5 seconds; profile the concrete Visual Studio (136.3s), Go (55.6s), Wails/govulncheck (32.1s), and Python (23.0s) owners, then verify the result in a new fresh Sandbox with the same populated cache and workspace plan.
- Consider checksum-validated Cargo `.crate` archive seeding only after the package/Rust mirror path passes natively. Keep Cargo index/Git databases, extracted sources, installed binaries, and target outputs guest-local because Cargo exposes no supported split-cache paths and trusts existing archives without rehashing them.
- Evaluate the Windows 11 24H2+ `wsb.exe` management API as a second implementation only if it removes lifecycle complexity without dropping supported Windows 10 behavior.
- Pin and verify managed external tool versions/hashes when moving beyond the fast MVP's stable package-manager path.
