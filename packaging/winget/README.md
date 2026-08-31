# WinGet package maintenance

`packaging/winget/manifests/` mirrors the exact directory that can be copied into
the Microsoft community repository. The intended independent package ID is
`hdosys.herdr-sandbox`; Herdr/Herdr-Win is never bundled or declared as a package
dependency.

## Current WinGet mirror

The mirrored community package version may intentionally lag the newest GitHub
release. These facts describe the currently mirrored WinGet package, version
`0.0.22`, not the current Herdr Sandbox product release.

- Mirrored package version: `0.0.22`
- Source tag: <https://github.com/hdosys/herdr-sandbox/releases/tag/v0.0.22>
- Community package ID: `hdosys.herdr-sandbox`
- Installer asset:
  <https://github.com/hdosys/herdr-sandbox/releases/download/v0.0.22/herdr-sandbox_v0.0.22_windows_amd64_setup.exe>
- Installer SHA-256:
  `d141148e397e1d09bc06adf337bbd2831bdde3499acdeb2e4bf75dc66383e2bb`
- Manifest schema: `1.12.0`

The currently mirrored v0.0.22 manifest passed the repository release gate,
public GitHub asset digest verification, WinGetCreate generation, and `winget
validate`. Community validation, installation, and publication remain downstream.
The installer is currently not Authenticode-signed.

## Validate locally

From the repository root:

```powershell
$manifest = '.\packaging\winget\manifests\h\hdosys\herdr-sandbox\0.0.22'
winget validate --manifest $manifest --disable-interactivity
winget install --manifest $manifest --silent --accept-package-agreements `
  --accept-source-agreements --disable-interactivity
```

Run install/upgrade/uninstall gates only in a disposable Windows environment.
Never use `--ignore-security-hash` for acceptance.

Local `validate --manifest`, `download --manifest`, and `install --manifest`
require an administrator to enable WinGet's `LocalManifestFiles` developer
setting. Use that setting only in a disposable Windows environment; the official
community validation pipeline remains the authoritative manifest-install gate.

## Publish an update

The target path in `microsoft/winget-pkgs` is:

```text
manifests/h/hdosys/herdr-sandbox/<version>/
```

For each release, re-download the public installer, recompute its SHA-256, run
`winget validate`, review the generated manifest diff, and submit only the
version directory through WinGetCreate. Do not store or pass a GitHub token in
this repository, command logs, or manifest files.

The package contains only Herdr Sandbox. Herdr Win remains a separate distribution
and is not bundled or declared as a dependency; install its community package
`hdosys.herdr-win` separately.
