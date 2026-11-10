param(
    [Parameter(Mandatory=$true)][string]$BootstrapRoot,
    [Parameter(Mandatory=$true)][string]$InstallerPath,
    [Parameter(Mandatory=$true)][string]$InstallerDigest,
    [Parameter(Mandatory=$true)][string]$StateRoot,
    [Parameter(Mandatory=$true)][string]$TaskName,
    [Parameter(Mandatory=$true)][int]$OwnerProcessId,
    [Parameter(Mandatory=$true)][long]$OwnerStartTicks,
    [Parameter(Mandatory=$true)][int]$ExpectedSession,
    [Parameter(Mandatory=$true)][string]$ExpectedUser
)
$ErrorActionPreference = 'Stop'
$env:PSModulePath = $PSHOME + '\Modules'
Import-Module Microsoft.PowerShell.Utility -ErrorAction Stop
$statusPath = Join-Path (Split-Path -Parent $InstallerPath) 'interactive.json'
function Publish-InteractiveResult {
    param($Value)
    $temporary = $statusPath + '.tmp'
    [IO.File]::WriteAllText($temporary, ($Value | ConvertTo-Json -Compress), (New-Object Text.UTF8Encoding($false)))
    if ([IO.File]::Exists($statusPath)) { [IO.File]::Replace($temporary, $statusPath, [NullString]::Value) }
    else { [IO.File]::Move($temporary, $statusPath) }
}
try {
    # The running instance survives deletion; no persistent task or restart trigger
    # remains even if SSH terminates its receiver without executing finally.
    $scheduler = New-Object -ComObject Schedule.Service
    $scheduler.Connect()
    $scheduler.GetFolder('\').DeleteTask($TaskName, 0)
    if ([Diagnostics.Process]::GetCurrentProcess().SessionId -ne $ExpectedSession -or
        [Security.Principal.WindowsIdentity]::GetCurrent().User.Value -cne $ExpectedUser) {
        throw 'Interactive installer session or user does not match its receiver.'
    }
    if ((Get-FileHash -LiteralPath $InstallerPath -Algorithm SHA256).Hash -ine $InstallerDigest) {
        throw 'Interactive installer snapshot hash mismatch.'
    }
    Add-Type -Path (Join-Path $BootstrapRoot 'provisioning\provisioning-process.cs')
    $spec = New-Object HerdrSandbox.ProvisioningProcessSpec
    $spec.Role = 'Interactive workstation installer'
    $spec.FilePath = $InstallerPath
    $spec.WorkingDirectory = Split-Path -Parent $InstallerPath
    $spec.Arguments = @('/PASSIVE', ('/ROOT=' + $StateRoot))
    $spec.ShowWindow = $true
    $spec.TimeoutMilliseconds = 172800000
    $spec.SuccessExitCodes = [int[]]@(0,1,2)
    Publish-InteractiveResult ([ordered]@{state='running';session=$ExpectedSession})
    $result = [HerdrSandbox.ProvisioningProcess]::RunWithProcessLease($spec, $OwnerProcessId, $OwnerStartTicks)
    Publish-InteractiveResult ([ordered]@{state='finished';exitCode=$result.ExitCode;stopped=$result.Stopped;timedOut=$result.TimedOut;session=$ExpectedSession})
} catch {
    Publish-InteractiveResult ([ordered]@{state='failed';message=$_.Exception.Message})
    exit 2
}
