# WinGet package maintenance

`packaging/winget/manifests/` mirrors the exact directory that can be copied into
the Microsoft community repository. The intended independent package ID is
`hdosys.herdr-sandbox`; Herdr/Herdr-Win is never bundled or declared as a package
dependency.

## Current WinGet mirror

The mirrored community package version may intentionally lag the newest GitHub
release. These facts describe the currently mirrored WinGet package, version
`0.0.25`, not the current Herdr Sandbox product release.

- Mirrored package version: `0.0.25`
- Source tag: <https://github.com/hdosys/herdr-sandbox/releases/tag/v0.0.25>
- Community package ID: `hdosys.herdr-sandbox`
- Installer asset:
  <https://github.com/hdosys/herdr-sandbox/releases/download/v0.0.25/herdr-sandbox_v0.0.25_windows_amd64_setup.exe>
- Installer SHA-256:
  `2f34526a557d7251ce228415a9f53f30e2adb1b38f410759d9cbcbd38fd659d5`
- Manifest schema: `1.12.0`

The currently mirrored v0.0.25 manifest passed the repository release gate,
public GitHub asset digest verification, WinGetCreate generation, and `winget
validate`. Community validation, installation, and publication remain downstream.
The installer is currently not Authenticode-signed.

## Validate locally

From the repository root:

```powershell
$manifest = '.\packaging\winget\manifests\h\hdosys\herdr-sandbox\0.0.25'
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

The package contains only Herdr Sandbox. Herdr Extended remains a separate distribution
and is not bundled or declared as a dependency; install its community package
`hdosys.herdr-win` separately. The WinGet ID is unchanged by the distribution rename.
