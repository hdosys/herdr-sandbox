# Start the ordinary browser flow. A CLI wait timeout leaves daemon enrollment active.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$stateExistedBefore = Test-TailscaleStateExists
$enrollmentAttempted = $false
try {
Set-TailscalePortablePolicy
Stop-TailscaleService
Start-TailscaleService
$tailscale = Get-TailscaleExecutable
$enrollmentAttempted = $true
try {
    # Windows PowerShell wraps native stderr in ErrorRecord, including the expected CLI timeout.
    $ErrorActionPreference = 'Continue'
    $messages = @(& $tailscale up ('--hostname=' + $HerdrTailscaleHostName) '--unattended=true' '--timeout=15s' 2>&1)
    $upExitCode = $LASTEXITCODE
} finally { $ErrorActionPreference = 'Stop' }
$messages = ($messages | ForEach-Object { [string]$_ }) -join "`n"
$raw = & $tailscale status --json --peers=false 2>$null | Out-String
if ($LASTEXITCODE -ne 0) { throw 'Tailscale enrollment status is unavailable.' }
$status = $raw | ConvertFrom-Json
$state = [string]$status.BackendState
if ($upExitCode -ne 0 -and $messages -notmatch '(?m)^timeout waiting for Tailscale service to enter a Running state; check health with "tailscale status"\s*$') {
    throw 'Tailscale could not start browser enrollment. Inspect tailscale status inside the Sandbox.'
}
$authURL = ''
if ($state -ceq 'NeedsLogin') { $authURL = [string]$status.AuthURL }
if ($state -notin @('Running', 'NeedsLogin', 'NeedsMachineAuth')) {
    throw 'Tailscale is not running or awaiting browser/device authorization.'
}
[ordered]@{ backendState = $state; authURL = $authURL } | ConvertTo-Json -Compress
} catch {
    if (Confirm-TailscaleNonEnrollment -EnrollmentAttempted $enrollmentAttempted -StateExistedBefore $stateExistedBefore) { exit 42 }
    throw
}
exit 0
