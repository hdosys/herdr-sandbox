# Security policy

## Supported version

Security fixes target the latest published GitHub release. Before reporting a
problem, run `herdr-sandbox version` and include its version and abbreviated
source revision without attaching configuration, credentials, logs, or run-state
archives.

## Reporting a vulnerability

GitHub private vulnerability reporting is not currently enabled for this
repository. Until it is enabled, open a minimal
[security-contact issue](https://github.com/hdosys/herdr-sandbox/issues/new)
containing no vulnerability or exploit details and ask the maintainer to arrange
a private channel. Repository maintainers should then create a private draft
security advisory. Do not put credentials, private keys, exploit details, or
sensitive host paths in a public issue. A detailed public issue is appropriate
only after sensitive details have been removed or coordinated disclosure is
complete.

Include the affected version, Windows version, expected boundary, smallest safe
reproduction, and whether the problem crosses from guest to host. Never attach a
real `%APPDATA%`, `%USERPROFILE%\.ssh`, agent-authentication directory, cache,
Tailscale state, or `.agent` session directory.

## Trust model

Herdr Sandbox treats coding agents, project scripts, package installers, and all
other guest processes as untrusted workloads with guest-administrator power. The
security boundary is Windows Sandbox plus the exact host resources deliberately
made available to it; it is not an application sandbox inside the guest.

The product protects these boundaries:

- The app-owned SSH private key remains on the host; only its public key enters
  the guest.
- Approved portable agent and GitHub credentials are streamed only over the
  verified SSH channel and are not placed in host run mappings or logs.
- Whole home/AppData roots, app-owned private state, reparse-bearing paths, and
  known credential roots such as `.ssh`, `.gnupg`, cloud/Kubernetes/Docker auth
  directories, supported coding-agent auth roots, GitHub CLI state, and Windows
  credential stores are rejected as mappings, including parents or descendants.
- External diagnostics are memory-bounded and terminal-control characters are
  replaced before display.
- Destructive lifecycle and uninstall paths require exact app ownership and fail
  closed when identity changes.

These are deliberate non-guarantees:

- A guest process can read every selected read-only mount and can modify every
  selected read/write workspace, mount, and cache. Do not map data the agent must
  not access.
- Networking is enabled. A credential deliberately copied into the guest can be
  used or exfiltrated by a compromised agent or project. Herdr Sandbox currently
  has no offline mode.
- OpenCode's guest-managed policy grants all permissions. Defender cloud/security
  features and SmartScreen are intentionally restricted in the disposable guest.
  This favors an unrestricted development environment, not hostile-code
  containment within that guest.
- Clipboard sharing crosses the host/guest boundary by explicit user action.
- Tailscale identity restoration remains experimental until the documented native
  two-fresh-Sandbox and peer-connectivity gate passes.

If the threat model forbids credential exfiltration, set every `codingAgentSync`
choice to `false`, remove `GitHub.cli` from the Base package plan (or use a host
profile with no authenticated `gh.exe` account), use read-only mounts wherever
possible, do not authenticate inside the guest, and enforce outbound-network
restrictions outside this product. Provisioning still needs network access for
uncached packages.

## Packages and releases

Omitted package versions intentionally resolve the latest available stable
version. Pin every required `wingetPackages.versions` entry and project toolchain
version when reproducibility is part of the threat model.

The current installer is unsigned and may trigger SmartScreen. Verify its SHA-256
sidecar from the release page, understand that the sidecar is delivered through
the same GitHub release channel, and confirm `herdr-sandbox version` after
installation. Do not treat stripping, checksums, or Windows Sandbox disposability
as a substitute for publisher signing or trusted release provenance.
