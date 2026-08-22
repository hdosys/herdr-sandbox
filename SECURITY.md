# Security policy

## Supported version

Security fixes target the latest published GitHub release. Before reporting a
problem, run `sandbox --version` and include its version and abbreviated
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
- Mobile SSH private keys remain on their originating phones, tablets, or
  computers. Configuration and immutable run input contain only canonical
  Ed25519 public keys; the displayed QR contains only a connection URI.
- The separate mobile SSH server key is current-user DPAPI encrypted on the host.
  Its stable public fingerprint is displayed before connection and survives a
  fresh Sandbox without making the private key portable to another host user.
- Approved portable agent and GitHub credentials are streamed only over the
  verified SSH channel and are not placed in host run mappings or logs.
- When the TradingView stack is selected, only the `sessionid` and
  `sessionid_sign` cookie pair for `tradingview.com` subdomains is read from the
  exact installed MSIX package profile into bounded host memory and streamed over
  that verified SSH channel. The complete cookie database, broker cookies, and
  unrelated site state never enter run mappings or the transfer archive. Missing
  host login state is a no-op.
- Whole home/AppData roots, app-owned private state, reparse-bearing paths, and
  known credential roots such as `.ssh`, `.gnupg`, cloud/Kubernetes/Docker auth
  directories, supported coding-agent auth roots, GitHub CLI state, and Windows
  credential stores are rejected as mappings, including parents or descendants.
- An explicitly selected shared model root is exposed only through one writable
  mapping at `C:\Models`. It is checked against every other mapped or recursively
  removable product root before launch.
- External diagnostics are memory-bounded and terminal-control characters are
  replaced before display.
- Destructive lifecycle and uninstall paths require exact app ownership and fail
  closed when identity changes.
- Repair, upgrade, and uninstall may force-terminate only peer processes whose
  queried full image path exactly equals the installed `sandbox.exe`. They retain
  each validated process handle through bounded exit confirmation, do not kill a
  process tree, and never target `WindowsSandbox.exe`.
- Mobile SSH listens only on the verified Tailscale IPv4 at TCP 2222, accepts
  public-key authentication only, and disables forwarding. Windows Firewall
  allows that port only from the Tailscale IPv4 range and blocks tailnet access
  to the management endpoint on TCP 22; tailnet policy must independently grant
  only the intended principals access to TCP 2222.

These are deliberate non-guarantees:

- A guest process can read every selected read-only mount and can modify every
  selected read/write workspace, mount, cache, worktree, and models directory. Do
  not map data the agent must not access or modify. The optional worktree root is
  trusted only at `C:/Worktrees/*` in guest Git, but that trust does not restrict
  guest file access.
- Enabling the shared model root lets every guest administrator process read,
  replace, and add model files and reference audio. Use a dedicated AI-model
  folder containing no unrelated private data.
- Networking is enabled. A credential deliberately copied into the guest can be
  used or exfiltrated by a compromised agent or project. Herdr Sandbox currently
  has no offline mode.
- The optional `audio` project stack runs AudioGridder Server and the selected
  VSTs inside Windows Sandbox. It opens exact guest firewall rules for server TCP
  55056 and workers 55088 through 56088, scoped to the current default host
  gateway and the pinned server executable. The separately installed host DAW
  client can therefore send plugin state, MIDI, audio, and control traffic into
  the guest. Project or user provisioning owns the VST binaries that process that
  data. Herdr Sandbox adds no authentication to AudioGridder's external protocol,
  so do not broaden the firewall source and do not select untrusted VST payloads.
- Imported GitHub CLI tokens are intentionally stored in the disposable guest's
  `hosts.yml`, not Windows Credential Manager, so noninteractive provisioning and
  Git HTTPS never open a credential dialog. Any guest administrator can read that
  file; closing the Sandbox is its cleanup boundary.
- An enabled coding-agent configuration root that is a physical Git repository
  contributes its tracked files, local repository config, refs, index, and object
  history to the guest. Hooks, reflogs, worktree pointers, active-operation state,
  and known credential/runtime paths are excluded or rejected, but remote URLs or
  historical objects may still contain secrets. Disable that agent's sync unless
  its complete configuration-repository history is safe for guest processes.
- Default-on host configuration Git updates contact each explicitly registered
  configuration root's upstream before transfer and after a terminal `down`. They
  use existing host Git credential configuration without opening an interactive
  prompt. Treat each configured remote as trusted host configuration input, or
  independently disable `configurationSync.pullHostGitRepositoriesOnUp` and
  `configurationSync.pullHostGitRepositoriesOnDown`. The explicit
  `pull-host-config` command remains available regardless of those flags.
- OpenCode's guest-managed policy grants all permissions. Defender cloud/security
  features and SmartScreen are intentionally restricted in the disposable guest.
  This favors an unrestricted development environment, not hostile-code
  containment within that guest.
- Enabling the official Playwright Extension grants the selected guest agent
  debugger access to the existing guest Edge profile, including controlled tabs,
  cookies, and signed-in sessions. Its connection token belongs only in the
  disposable guest environment; anyone with that token and guest process access
  can bypass the extension's approval dialog until the token changes or the guest
  is discarded.
- Starting TradingView Desktop from its managed shortcut or through TVControl's
  direct upstream `launch` command passes `--remote-debugging-port=9222` and opens
  a local Chrome DevTools Protocol endpoint with powerful chart/UI access,
  exposing signed-in TradingView content to guest processes until Desktop exits.
  Selecting the stack registers TVControl in guest OpenCode as disabled by
  default; toggling it through `/mcps` exposes its tools, including launch, to that
  OpenCode session. TVControl and the guest agent run in the same interactive
  Session 1 context, so that upstream command directly spawns the visible Desktop
  from the already verified `TradingView.exe` on `PATH`. The stack leaves the
  upstream TVControl package unmodified and installs and verifies the official
  signed MSIX payload, but provisioning and configuration sync never launch
  Desktop, open the endpoint themselves, or perform an interactive authentication
  flow.
  Desktop is already guest-local, so TVControl does not need its protected-MSIX
  copy fallback; launch is non-destructive unless the user explicitly supplies
  `--kill-existing`. TradingView terms and market-data licenses
  may prohibit automation, scraping, non-display use, or redistribution regardless
  of local execution; the stack does not grant rights or bypass access controls.
- A transferred TradingView session is deliberately readable and usable by any
  guest administrator process. Close the Sandbox to discard that guest copy, and
  do not select the TradingView stack when its account session must not be exposed
  to the guest workload. Configuration sync refuses to replace those cookies while
  guest TradingView Desktop is running rather than terminating the application.
- Clipboard sharing crosses the host/guest boundary by explicit user action.
- Tailscale supplies private routing, not login authorization. Anyone who gains an
  authorized mobile private key and tailnet reachability can open Herdr as the
  guest administrator user and access every resource available to that guest.
  Remove a compromised public key from `mobileSSHAuthorizedKeys`, stop the
  app-owned Sandbox, and launch a fresh one; changing a live listener in place is
  intentionally unsupported.

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

Opt-in model persistence also enables HyperFrames VoxCPM2 preparation from the latest stable
`hdosys/hyperframes-voxcpm2` GitHub release instead of a Sandbox-owned version
pin. The host accepts only HTTPS with normal certificate validation, refuses
non-HTTPS redirects, user curl configuration, inherited custom CA files, and TLS
key logging, and requires GitHub's asset
digest plus the exact release `.sha256` sidecar. It binds each downloaded model
and reference-audio file to the immutable revision, size, and SHA-256 before
publishing the writable guest input. The HyperFrames stack rehashes the selected
model files before activation. The admitted archive may contain only
the CPU runtime, and provisioning removes prior VoxCPM2 Vulkan selection state
before publishing the CPU server. This protects integrity for
the selected release channel, but GitHub and the named upstream repositories
remain trusted distribution authorities. Disable the setting when that dynamic
release policy or outbound download is outside the accepted threat model.

The upstream Windows `opensrc` executable provisioned by Base is unsigned. Its
release-owned URL and SHA-256 are pinned, and both cache hits and guest-local
copies are revalidated before execution. That digest comes from the same upstream
GitHub release channel and supplies integrity checking, not publisher signing.

The current installer is unsigned and may trigger SmartScreen. Compare its local
SHA-256 with the digest GitHub displays for that Release asset, understand that
this digest is delivered through the same GitHub release channel, and confirm
`sandbox --version` after installation. Do not treat stripping, digests, or Windows
Sandbox disposability as a substitute for publisher signing or trusted release
provenance.
