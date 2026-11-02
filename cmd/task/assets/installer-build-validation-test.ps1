param([Parameter(Mandatory = $true)][string]$Validator)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0
$arguments = @{
    InstallerScript = 'C:\fixture\installer.nsi'
    Version = '0.0.13'; FixedVersion = '0.0.13.0'
    BuildFreshness = '2026.11.02.1200Z'; BuildRevision = '0123456789ab'
    BuildDisplay = '0.0.13 2026.11.02.1200Z (0123456789ab)'
    AppName = 'fixture'; AppApplicationName = 'fixture'; AppDisplayName = 'Fixture'
    AppPublisher = 'Fixture'; AppProductUrl = 'https://example.com'; AppCopyright = 'Fixture'
    AppConfigFile = 'config.json'; AppUserScript = 'user.ps1'; AppProjectDirectory = '.fixture'
    OutputFile = 'C:\fixture\fixture_v0.0.13_windows_amd64_setup.exe'
    OutputFileName = 'fixture_v0.0.13_windows_amd64_setup.exe'
    PackageDirectory = 'C:\fixture\payload'; AssetsDirectory = 'C:\fixture\assets'
    PathHelper = 'C:\fixture\path.ps1'; QuietUninstallHelper = 'C:\fixture\uninstall.ps1'
    AppProductGuid = '01234567-89ab-4def-8123-456789abcdef'
    AppUninstallKey = '{01234567-89AB-4DEF-8123-456789ABCDEF}'
    AppInstallDirectory = 'Fixture'; AppExecutable = 'fixture.exe'
    AppBaseScript = 'base.ps1'; AppStackScript = 'stacks.ps1'; AppLicense = 'LICENSE.txt'
    AppQuietUninstallHelper = 'uninstall.ps1'
}
foreach ($case in @(
    @{ Key = 'AppExecutable'; Value = 'CON.txt'; Message = 'reserved Windows device' },
    @{ Key = 'AppExecutable'; Value = '..\escape.exe'; Message = 'safe Windows leaf' },
    @{ Key = 'AppExecutable'; Value = ('a' * 121); Message = 'leaf-name limit' },
    @{ Key = 'AppBaseScript'; Value = 'FIXTURE.EXE'; Message = 'collides' },
    @{ Key = 'AppUninstallKey'; Value = '{FFFFFFFF-FFFF-4FFF-8FFF-FFFFFFFFFFFF}'; Message = 'brace-wrapped uppercase product GUID' },
    @{ Key = 'AppProductGuid'; Value = '01234567-89ab-0def-8123-456789abcdef'; Message = 'canonical D-format GUID' },
    @{ Key = 'FixedVersion'; Value = '0.0.14.0'; Message = 'same supported' },
    @{ Key = 'OutputFileName'; Value = 'wrong.exe'; Message = 'application and release identity' },
    @{ Key = 'AppDisplayName'; Value = 'A&B'; Message = 'unescaped ampersand' },
    @{ Key = 'AppPublisher'; Value = 'embedded$variable'; Message = 'cannot be injected safely' }
)) {
    $parameters = $arguments.Clone()
    $parameters[$case.Key] = $case.Value
    $rejected = $false
    try { & $Validator @parameters }
    catch {
        if (-not $_.Exception.Message.Contains($case.Message)) { throw }
        $rejected = $true
    }
    if (-not $rejected) { throw "Unsafe installer metadata accepted: $($case.Key)" }
}
'Installer metadata rejection checks passed.'
