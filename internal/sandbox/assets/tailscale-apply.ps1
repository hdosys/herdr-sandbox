# herdr-sandbox-tailscale-apply-contract: 1

function Invoke-TailscaleApply {
    param([long]$ExpectedLength, [string]$ExpectedDigest, [int]$ApplySchemaVersion)
    $inputStream = [Console]::OpenStandardInput()
    $payload = New-Object byte[] $ExpectedLength
    $offset = 0
    while ($offset -lt $payload.Length) {
        $read = $inputStream.Read($payload, $offset, $payload.Length - $offset)
        if ($read -le 0) { throw 'Tailscale identity input ended before its declared length.' }
        $offset += $read
    }
    $sha256 = [Security.Cryptography.SHA256]::Create()
    try { $actualDigest = ([BitConverter]::ToString($sha256.ComputeHash($payload))).Replace('-', '').ToLowerInvariant() } finally { $sha256.Dispose() }
    if ($actualDigest -cne $ExpectedDigest) { throw 'Tailscale identity input SHA-256 mismatch.' }
    $request = [Text.Encoding]::UTF8.GetString($payload) | ConvertFrom-Json
    $payload = $null
    Assert-ExactProperties $request @('schemaVersion', 'mode', 'state', 'windowsUserSID')
    if ([int]$request.schemaVersion -ne $ApplySchemaVersion) { throw 'Tailscale identity input schema is unsupported.' }
    $currentSID = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    $waitMinutes = 2
    if ([string]$request.mode -ceq 'restore') {
        if ([string]$request.windowsUserSID -cne $currentSID) { throw 'Tailscale identity Windows user SID changed.' }
        $stateBytes = [Convert]::FromBase64String([string]$request.state)
        if ($stateBytes.Length -le 0 -or $stateBytes.Length -gt $HerdrMaximumTailscaleStateBytes) { throw 'Tailscale state size is invalid.' }
        Set-TailscalePortablePolicy
        Stop-TailscaleService
        try { Set-TailscaleState -StateBytes $stateBytes } finally {
            $stateBytes = $null
            Start-TailscaleService
        }
    } elseif ([string]$request.mode -ceq 'enroll') {
        $waitMinutes = 8
    } else { throw 'Tailscale identity input mode is invalid.' }
    $identity = Wait-TailscaleIdentity -ExpectedSID $currentSID -Minutes $waitMinutes
    return Capture-TailscaleState -Identity $identity
}
