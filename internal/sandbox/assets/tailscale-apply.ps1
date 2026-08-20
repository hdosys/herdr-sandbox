# herdr-sandbox-tailscale-apply-contract: 1

function Invoke-TailscaleApply {
    param([long]$ExpectedLength, [string]$ExpectedDigest, [int]$ApplySchemaVersion, [int]$MaximumAuthKeyBytes, [int]$IdentityNotEstablishedExitCode)
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
    Assert-ExactProperties $request @('schemaVersion', 'mode', 'authKey', 'state', 'windowsUserSID')
    if ([int]$request.schemaVersion -ne $ApplySchemaVersion) { throw 'Tailscale identity input schema is unsupported.' }
    $currentSID = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    $identity = $null
    $identityNotEstablished = $false
    if ([string]$request.mode -ceq 'restore') {
        if ([string]$request.windowsUserSID -cne $currentSID) { throw 'Tailscale identity Windows user SID changed.' }
        $stateBytes = [Convert]::FromBase64String([string]$request.state)
        if ($stateBytes.Length -le 0 -or $stateBytes.Length -gt $HerdrMaximumTailscaleStateBytes) { throw 'Tailscale state size is invalid.' }
        Set-TailscalePortablePolicy
        Stop-TailscaleService
        try {
            Set-TailscaleState -StateBytes $stateBytes
        } finally {
            $stateBytes = $null
            Start-TailscaleService
        }
    } elseif ([string]$request.mode -ceq 'enroll') {
        $authPath = Join-Path 'C:\HerdrSandbox\staging' ('tailscale-auth-' + [Guid]::NewGuid().ToString('N'))
        $enrollmentAttempted = $false
        try {
            $authBytes = [Convert]::FromBase64String([string]$request.authKey)
            if ($authBytes.Length -le 0 -or $authBytes.Length -gt $MaximumAuthKeyBytes) { throw 'Tailscale auth key size is invalid.' }
            Set-TailscalePortablePolicy
            Stop-TailscaleService
            Start-TailscaleService
            New-Item -ItemType Directory -Path (Split-Path -Parent $authPath) -Force | Out-Null
            [IO.File]::WriteAllBytes($authPath, $authBytes)
            $authBytes = $null
            $tailscale = Get-TailscaleExecutable
            $enrollmentAttempted = $true
            & $tailscale up ('--auth-key=file:' + $authPath) ('--hostname=' + $HerdrTailscaleHostName) '--unattended=true' '--timeout=2m' *> $null
            if ($LASTEXITCODE -ne 0) {
                try { $identity = Wait-TailscaleIdentity -ExpectedSID $currentSID } catch { $identityNotEstablished = $true }
            }
        } catch {
            if (-not $enrollmentAttempted) { $identityNotEstablished = $true } else { throw }
        } finally {
            $authBytes = $null
            if (Test-Path -LiteralPath $authPath) {
                Remove-Item -LiteralPath $authPath -Force -ErrorAction Stop
            }
            if (Test-Path -LiteralPath $authPath) {
                throw 'Tailscale auth-key staging cleanup did not remove the credential file.'
            }
        }
    } else {
        throw 'Tailscale identity input mode is invalid.'
    }
    if ($identityNotEstablished) { exit $IdentityNotEstablishedExitCode }
    if ($null -eq $identity) { $identity = Wait-TailscaleIdentity -ExpectedSID $currentSID }
    return Capture-TailscaleState -Identity $identity
}
