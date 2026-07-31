# WinGet submission preparation

`packaging/winget/manifests/` mirrors the exact directory that can be copied into
the Microsoft community repository. The intended independent package ID is
`hdosys.herdr-sandbox`; Herdr/Herdr-Win is never bundled or declared as a package
dependency.

## Prepared release

- Version: `0.0.7`
- Release: <https://github.com/hdosys/herdr-sandbox/releases/tag/v0.0.7>
- Installer:
  <https://github.com/hdosys/herdr-sandbox/releases/download/v0.0.7/herdr-sandbox_v0.0.7_windows_amd64_setup.exe>
- Published installer SHA-256:
  `d405b8204b20e5174df66b81520b140ccf49eb5167ca1fdb34e1a5d33f98389c`
- Manifest schema: `1.12.0`

The public installer passed a real isolated silent install, `0.0.6` to `0.0.7`
upgrade, default uninstall, and explicit configuration-removal gate. The default
uninstall removed operational state/cache/integration and preserved user settings;
the explicit option removed settings too. The installer is currently not
Authenticode-signed.

## Validate locally

From the repository root:

```powershell
$manifest = '.\packaging\winget\manifests\h\hdosys\herdr-sandbox\0.0.7'
winget validate --manifest $manifest --disable-interactivity
winget install --manifest $manifest --silent --accept-package-agreements `
  --accept-source-agreements --disable-interactivity
```

Run install/upgrade/uninstall gates only in a disposable Windows environment.
Never use `--ignore-security-hash` for acceptance.

`winget validate` needs no host setting change. Local `download --manifest` and
`install --manifest` require an administrator to enable WinGet's
`LocalManifestFiles` developer setting; do not enable it on a normal host solely
for this preparation. The official community submission pipeline provides the
authoritative manifest-install gate when that local boundary is unavailable.

## Community submission

The target path in `microsoft/winget-pkgs` is:

```text
manifests/h/hdosys/herdr-sandbox/0.0.7/
```

At submission time, re-download the public installer, recompute its SHA-256, run
`winget validate`, and then submit only that directory through WinGetCreate or a
sparse `microsoft/winget-pkgs` fork. Do not store or pass a GitHub token in this
repository, command logs, or manifest files.

Submit this Sandbox directory independently. Herdr-Win remains a separate package
and will be prepared later; it is not bundled or declared as a dependency here.
The future combined community-source install remains unavailable until that
separate package is published.
