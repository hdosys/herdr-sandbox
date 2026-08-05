# WinGet submission preparation

`packaging/winget/manifests/` mirrors the exact directory that can be copied into
the Microsoft community repository. The intended independent package ID is
`hdosys.herdr-sandbox`; Herdr/Herdr-Win is never bundled or declared as a package
dependency.

## Prepared release

- Version: `0.0.10`
- Release: <https://github.com/hdosys/herdr-sandbox/releases/tag/v0.0.10>
- Installer:
  <https://github.com/hdosys/herdr-sandbox/releases/download/v0.0.10/herdr-sandbox_v0.0.10_windows_amd64_setup.exe>
- Published installer SHA-256:
  `e2aa290cfa1d842f33d13d359bc0692d6b9979ecf9bdb1c73cab63b7f9b0f65a`
- Manifest schema: `1.12.0`

The v0.0.10 installer passed a 24-case real lifecycle matrix. The published asset
was then downloaded, verified against its sidecar and GitHub digest, installed
silently, checked for the exact version, uninstalled quietly, and confirmed to
preserve user configuration. The installer is currently not Authenticode-signed.

## Validate locally

From the repository root:

```powershell
$manifest = '.\packaging\winget\manifests\h\hdosys\herdr-sandbox\0.0.10'
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
manifests/h/hdosys/herdr-sandbox/0.0.10/
```

At submission time, re-download the public installer, recompute its SHA-256, run
`winget validate`, and then submit only that directory through WinGetCreate or a
sparse `microsoft/winget-pkgs` fork. Do not store or pass a GitHub token in this
repository, command logs, or manifest files.

Submit this Sandbox directory independently. Herdr-Win remains a separate package
and will be prepared later; it is not bundled or declared as a dependency here.
The future combined community-source install remains unavailable until that
separate package is published.
