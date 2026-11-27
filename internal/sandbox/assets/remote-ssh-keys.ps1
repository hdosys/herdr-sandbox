$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

# Load the existing owner only after checking its exact application-owned bytes.
$controlScript = 'C:\HerdrSandbox\remote-ssh\remote-ssh.ps1'
$current = 'C:\HerdrSandbox\remote-ssh'
while ($true) {
    $item = Get-Item -LiteralPath $current -Force -ErrorAction Stop
    if (-not $item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw 'Remote SSH path is not a physical directory.'
    }
    if ($current -ceq 'C:\HerdrSandbox') { break }
    $current = [IO.Directory]::GetParent($current).FullName
}
$item = Get-Item -LiteralPath $controlScript -Force -ErrorAction Stop
if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
    $item.Length -le 0 -or $item.Length -gt 65536 -or
    (Get-FileHash -LiteralPath $controlScript -Algorithm SHA256).Hash.ToLowerInvariant() -cne $expectedScriptDigest) {
    throw 'Remote SSH control script does not match this application; no keys were changed.'
}
. $controlScript -Mode Verify -ExpectedScriptSHA256 $expectedScriptDigest | Out-Null
$prepared = Read-PreparedState
$before = Assert-RunningEndpoint -Prepared $prepared
$authorizedText = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($authorizedKeysBase64))
$keys = @($authorizedText.TrimEnd("`n").Split("`n"))
if ($authorizedText.Length -gt 32768 -or -not $authorizedText.EndsWith("`n") -or
    $keys.Count -lt 1 -or $keys.Count -gt 8) { throw 'Remote SSH key update size is invalid.' }
$previous = ''
foreach ($key in $keys) {
    if ($key -notmatch '^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)) [A-Za-z0-9+/]+={0,2}$' -or
        ($previous -ne '' -and [string]::CompareOrdinal($previous, $key) -ge 0)) {
        throw 'Remote SSH key update is not sorted canonical public keys.'
    }
    $previous = $key
}

# Validate with OpenSSH before publishing. Atomic replacement preserves the
# existing file ACL. The running daemon reads this file for each authentication.
$candidate = Join-Path $script:Root ('.remote-keys-' + [Guid]::NewGuid().ToString('N') + '.tmp')
try {
    [IO.File]::WriteAllText($candidate, $authorizedText, $script:Utf8NoBom)
    Protect-KeyFile -Path $candidate
    $sshKeygen = Get-OpenSSHExecutable -Name 'ssh-keygen.exe'
    $fingerprints = @(& $sshKeygen -l -f $candidate 2>&1)
    if ($LASTEXITCODE -ne 0 -or $fingerprints.Count -ne $keys.Count) {
        throw 'OpenSSH rejected the proposed remote public keys; existing access is unchanged.'
    }
    [void](Assert-PreparedFiles -State $prepared)
    [void](Assert-RunningEndpoint -Prepared $prepared)
    $newDigest = (Get-FileHash -LiteralPath $candidate -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($newDigest -cne [string]$prepared.authorizedKeysSHA256) {
        [IO.File]::Replace($candidate, $script:AuthorizedKeysPath, [System.Management.Automation.Language.NullString]::Value)
        $prepared.authorizedKeysSHA256 = $newDigest
        Set-AtomicText -Path $script:PreparedPath -Text (($prepared | ConvertTo-Json -Compress) + "`n")
    }
    [void](Assert-PreparedFiles -State $prepared)
    $after = Assert-RunningEndpoint -Prepared $prepared
    if ([int]$before.pid -ne [int]$after.pid -or [string]$before.startedAtUTC -cne [string]$after.startedAtUTC) {
        throw 'Remote SSH process changed during public-key update.'
    }
    if ([IO.File]::ReadAllText($script:AuthorizedKeysPath) -cne $authorizedText) {
        throw 'Remote SSH public-key read-back differs from the approved set.'
    }
    [ordered]@{ schemaVersion = 1; state = 'running'; pid = [int]$after.pid } | ConvertTo-Json -Compress
} finally {
    if (Test-Path -LiteralPath $candidate) { [IO.File]::Delete($candidate) }
}
