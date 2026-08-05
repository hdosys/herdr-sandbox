# WinGet submission preparation

`packaging/winget/manifests/` mirrors the exact directory that can be copied into
the Microsoft community repository. The intended independent package ID is
`hdosys.herdr-sandbox`; Herdr/Herdr-Win is never bundled or declared as a package
dependency.

## Prepared release

- Version: `0.0.9`
- Release: <https://github.com/hdosys/herdr-sandbox/releases/tag/v0.0.9>
- Installer:
  <https://github.com/hdosys/herdr-sandbox/releases/download/v0.0.9/herdr-sandbox_v0.0.9_windows_amd64_setup.exe>
- Published installer SHA-256:
  `ae92c0f1b6d8ea26f0fa7555ca59b1effea386241898876cda99265f343a9dc6`
- Manifest schema: `1.12.0`

The v0.0.9 installer path passed a real isolated silent install, upgrade, default
uninstall, and explicit configuration-removal gate. The published asset was then
downloaded and verified against its sidecar, four-file payload, version, and source
revision. The installer is currently not Authenticode-signed.

## Validate locally

From the repository root:

```powershell
$manifest = '.\packaging\winget\manifests\h\hdosys\herdr-sandbox\0.0.9'
winget validate --manifest $manifest --disable-interactivity
winget install --manifest $manifest --silent --accept-package-agreements `
  --accept-source-agreements --disable-interactivity
```

Run install/upgrade/uninstall gates only in a disposable Windows environment.
Never use `--ignore-security-hash` for acceptance.

Local `validate --manifest`, `download --manifest`, and `install --manifest`
require an administrator to enable WinGet's `LocalManifestFiles` developer
setting. Use that setting only in a disposable Windows environment; the official
community submission pipeline remains the authoritative manifest-install gate.

## Community submission

The target path in `microsoft/winget-pkgs` is:

```text
manifests/h/hdosys/herdr-sandbox/0.0.9/
```

At submission time, re-download the public installer, recompute its SHA-256, run
`winget validate`, and then submit only that directory through WinGetCreate or a
sparse `microsoft/winget-pkgs` fork. Do not store or pass a GitHub token in this
repository, command logs, or manifest files.

Submit this Sandbox directory independently. Herdr-Win remains a separate package
and will be prepared later; it is not bundled or declared as a dependency here.
The future combined community-source install remains unavailable until that
separate package is published.
