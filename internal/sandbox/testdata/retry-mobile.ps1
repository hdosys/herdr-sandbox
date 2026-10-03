$script:SchemaVersion = 1
$script:ProcessPath = 'fixture-process-state'
$script:checks = 0
$script:hasProcess = $false
function Test-Path { param($LiteralPath) if ($LiteralPath -cne $script:ProcessPath) { throw 'Unexpected path' }; return $script:hasProcess }
function Assert-PreparedFiles { param($State) $script:checks += 1; return 'fixture-sshd.exe' }
function Assert-RunningEndpoint { param($Prepared) $script:checks += 1; return [pscustomobject]@{ pid = 42 } }
function Read-PreparedState { return $script:prepared }
function New-NetFirewallRule { throw 'Retry attempted to recreate firewall rules.' }
$request = [pscustomobject]@{
    tailscaleIPv4 = '100.64.0.10'; publicKey = 'fixture-public-key'
    authorizedKeys = @('fixture-device-key'); scriptSHA256 = ('a' * 64)
}
$sha256 = [Security.Cryptography.SHA256]::Create()
try {
    $digest = ([BitConverter]::ToString($sha256.ComputeHash([Text.Encoding]::UTF8.GetBytes("fixture-device-key`n")))).Replace('-', '').ToLowerInvariant()
} finally { $sha256.Dispose() }
$script:prepared = [pscustomobject]@{
    tailscaleIPv4 = $request.tailscaleIPv4; hostPublicKey = $request.publicKey
    authorizedKeysSHA256 = $digest; scriptSHA256 = $request.scriptSHA256
}
Assert-RepeatedPreparation -Request $request -Prepared $script:prepared
if ($script:checks -ne 1) { throw 'Prepared-only retry did not verify its existing files.' }
$script:hasProcess = $true
Assert-RepeatedPreparation -Request $request -Prepared $script:prepared
$result = Invoke-Activate | ConvertFrom-Json
if ($script:checks -ne 5 -or $result.state -cne 'running' -or $result.pid -ne 42) {
    throw 'Activated endpoint was not revalidated and reused.'
}
foreach ($field in @('tailscaleIPv4', 'publicKey', 'scriptSHA256', 'authorizedKeys')) {
    $original = $request.$field
    $request.$field = 'changed'
    $rejected = $false
    try { Assert-RepeatedPreparation -Request $request -Prepared $script:prepared } catch {
        if ($_.Exception.Message -notlike 'Repeated mobile preparation*') { throw }
        $rejected = $true
    }
    $request.$field = $original
    if (-not $rejected) { throw "Changed endpoint identity was reused: $field" }
}
Write-Output 'verified'
