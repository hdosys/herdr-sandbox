# herdr-sandbox-tailscale-lifecycle-contract: 1

function Assert-ExactProperties {
    param([object]$Value, [string[]]$Expected)
    $actual = @($Value.PSObject.Properties.Name)
    if ($actual.Count -ne $Expected.Count) { throw 'Tailscale JSON property count is invalid.' }
    foreach ($name in $Expected) { if (-not ($actual -ccontains $name)) { throw 'Tailscale JSON properties are invalid.' } }
}

function Get-TailscaleExecutable {
    $command = Get-Command 'tailscale.exe' -CommandType Application -ErrorAction Stop
    return $command.Source
}

function Get-TailscaleService {
    return Get-Service -Name 'Tailscale' -ErrorAction Stop
}

function Stop-TailscaleService {
    $service = Get-TailscaleService
    if ($service.Status -ne [ServiceProcess.ServiceControllerStatus]::Stopped) {
        Stop-Service -Name 'Tailscale' -ErrorAction Stop
        $service.WaitForStatus([ServiceProcess.ServiceControllerStatus]::Stopped, [TimeSpan]::FromSeconds(30))
    }
}

function Start-TailscaleService {
    $service = Get-TailscaleService
    if ($service.Status -ne [ServiceProcess.ServiceControllerStatus]::Running) {
        Start-Service -Name 'Tailscale' -ErrorAction Stop
        $service.WaitForStatus([ServiceProcess.ServiceControllerStatus]::Running, [TimeSpan]::FromSeconds(30))
    }
}

function Set-TailscalePortablePolicy {
    $policy = 'HKLM:\SOFTWARE\Policies\Tailscale'
    New-Item -Path $policy -Force | Out-Null
    New-ItemProperty -Path $policy -Name 'EncryptState' -PropertyType DWord -Value 0 -Force | Out-Null
    New-ItemProperty -Path $policy -Name 'HardwareAttestation' -PropertyType DWord -Value 0 -Force | Out-Null
}

function Assert-RegularNonReparseFile {
    param([string]$Path, [long]$Maximum)
    $item = Get-Item -LiteralPath $Path -Force -ErrorAction Stop
    if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or $item.Length -le 0 -or $item.Length -gt $Maximum) {
        throw 'Tailscale state path is unsafe or too large.'
    }
}

function Assert-TailscalePlaintextState {
    param([byte[]]$StateBytes)
    if ($StateBytes.Length -le 0 -or $StateBytes.Length -gt $HerdrMaximumTailscaleStateBytes) { throw 'Tailscale state size is invalid.' }
    $stateObject = [Text.Encoding]::UTF8.GetString($StateBytes) | ConvertFrom-Json
    if ($null -eq $stateObject.PSObject.Properties['_machinekey'] -or [string]::IsNullOrWhiteSpace([string]$stateObject._machinekey)) {
        throw 'Tailscale state is not portable plaintext state.'
    }
}

function Set-TailscaleState {
    param([byte[]]$StateBytes)
    Assert-TailscalePlaintextState -StateBytes $StateBytes
    $directory = Join-Path $env:ProgramData 'Tailscale'
    $directoryItem = Get-Item -LiteralPath $directory -Force -ErrorAction Stop
    if (-not $directoryItem.PSIsContainer -or ($directoryItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Tailscale state directory is unsafe.' }
    $target = Join-Path $directory 'server-state.conf'
    if (Test-Path -LiteralPath $target) { Assert-RegularNonReparseFile -Path $target -Maximum $HerdrMaximumTailscaleStateBytes }
    $temporary = Join-Path $directory ('server-state.herdr-sandbox-' + [Guid]::NewGuid().ToString('N') + '.tmp')
    try {
        [IO.File]::WriteAllBytes($temporary, $StateBytes)
        Assert-RegularNonReparseFile -Path $temporary -Maximum $HerdrMaximumTailscaleStateBytes
        if (Test-Path -LiteralPath $target) { [IO.File]::Replace($temporary, $target, $null) } else { [IO.File]::Move($temporary, $target) }
        Assert-RegularNonReparseFile -Path $target -Maximum $HerdrMaximumTailscaleStateBytes
    } finally {
        if (Test-Path -LiteralPath $temporary) {
            Remove-Item -LiteralPath $temporary -Force -ErrorAction Stop
        }
        if (Test-Path -LiteralPath $temporary) {
            throw 'Tailscale plaintext state staging cleanup did not remove the credential file.'
        }
    }
}

function Read-TailscaleIdentity {
    param([string]$ExpectedSID)
    $tailscale = Get-TailscaleExecutable
    $raw = & $tailscale status --json --peers=false 2>$null | Out-String
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($raw)) { return $null }
    $status = $raw | ConvertFrom-Json
    if ([string]$status.BackendState -cne 'Running' -or $null -eq $status.Self) { return $null }
    $ipv4 = @($status.TailscaleIPs | Where-Object {
        $parsed = $null
        [Net.IPAddress]::TryParse([string]$_, [ref]$parsed) -and $parsed.AddressFamily -eq [Net.Sockets.AddressFamily]::InterNetwork
    })
    $tags = @($status.Self.Tags)
    if ($ipv4.Count -ne 1 -or $tags.Count -eq 0 -or [string]$status.Self.HostName -cne $HerdrTailscaleHostName -or
        [string]::IsNullOrWhiteSpace([string]$status.Self.ID) -or [string]::IsNullOrWhiteSpace([string]$status.Self.PublicKey) -or
        [string]::IsNullOrWhiteSpace([string]$status.Self.DNSName) -or [string]::IsNullOrWhiteSpace([string]$status.Version)) { return $null }
    return [ordered]@{
        schemaVersion = $HerdrTailscaleIdentitySchemaVersion
        windowsUserSID = $ExpectedSID
        nodeID = [string]$status.Self.ID
        nodeKey = [string]$status.Self.PublicKey
        ipv4 = [string]$ipv4[0]
        dnsName = [string]$status.Self.DNSName
        hostName = [string]$status.Self.HostName
        tailscaleVersion = [string]$status.Version
        tags = @($tags | ForEach-Object { [string]$_ })
    }
}

function Wait-TailscaleIdentity {
    param([string]$ExpectedSID, [int]$TimeoutSeconds = 120)
    if ($TimeoutSeconds -lt 1 -or $TimeoutSeconds -gt 600) { throw 'Tailscale identity timeout is invalid.' }
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        $identity = Read-TailscaleIdentity -ExpectedSID $ExpectedSID
        if ($null -ne $identity) { return $identity }
        Start-Sleep -Milliseconds 500
    } while ([DateTime]::UtcNow -lt $deadline)
    throw 'Tailscale did not reach the required tagged running identity.'
}

function Capture-TailscaleStateBytes {
    param([string]$ExpectedSID, [switch]$LeaveServiceStopped)
    $currentSID = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    if ($currentSID -cne $ExpectedSID) { throw 'Tailscale state Windows user SID changed.' }
    Stop-TailscaleService
    try {
        $path = Join-Path $env:ProgramData 'Tailscale\server-state.conf'
        Assert-RegularNonReparseFile -Path $path -Maximum $HerdrMaximumTailscaleStateBytes
        $stateBytes = [IO.File]::ReadAllBytes($path)
        Assert-TailscalePlaintextState -StateBytes $stateBytes
    } finally {
        if (-not $LeaveServiceStopped) { Start-TailscaleService }
    }
    $encodedState = [Convert]::ToBase64String($stateBytes)
    $stateBytes = $null
    return [ordered]@{
        windowsUserSID = $currentSID
        state = $encodedState
    }
}

function Capture-TailscaleState {
    param([Collections.IDictionary]$Identity, [switch]$LeaveServiceStopped, [int]$IdentityTimeoutSeconds = 120)
    $captured = Capture-TailscaleStateBytes -ExpectedSID ([string]$Identity.windowsUserSID)
    $verified = Wait-TailscaleIdentity -ExpectedSID ([string]$Identity.windowsUserSID) -TimeoutSeconds $IdentityTimeoutSeconds
    foreach ($name in @('nodeID', 'nodeKey', 'ipv4', 'dnsName', 'hostName')) {
        if ([string]$verified[$name] -cne [string]$Identity[$name]) { throw 'Tailscale identity changed while capturing state.' }
    }
    $verified['state'] = [string]$captured.state
    if ($LeaveServiceStopped) { Stop-TailscaleService }
    return $verified
}
