param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('Prepare', 'Activate', 'Verify')]
    [string]$Mode,

    [string]$RequestPath = '',

    [string]$ExpectedScriptSHA256 = ''
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$script:SchemaVersion = 1
$script:MobilePort = 2222
$script:MobileUser = 'WDAGUtilityAccount'
$script:MobileUserRule = 'wdagutilityaccount'
$script:Root = 'C:\HerdrSandbox\mobile-ssh'
$script:PreparedPath = Join-Path $script:Root 'prepared.json'
$script:ProcessPath = Join-Path $script:Root 'process.json'
$script:ConfigPath = Join-Path $script:Root 'sshd_config'
$script:PrivateKeyPath = Join-Path $script:Root 'ssh_host_ed25519_key'
$script:PublicKeyPath = $script:PrivateKeyPath + '.pub'
$script:AuthorizedKeysPath = Join-Path $script:Root 'authorized_keys'
$script:StableScriptPath = Join-Path $script:Root 'mobile-ssh.ps1'
$script:LogPath = Join-Path $script:Root 'sshd.log'
$script:StdoutPath = Join-Path $script:Root 'sshd.stdout.log'
$script:StderrPath = Join-Path $script:Root 'sshd.stderr.log'
$script:AllowRuleName = 'HerdrSandbox-MobileSSH-In-TCP'
$script:BlockRuleName = 'HerdrSandbox-ManagementSSH-Block-Tailnet'
$script:Utf8NoBom = New-Object Text.UTF8Encoding($false)

function Assert-ExactProperties {
    param(
        [Parameter(Mandatory = $true)][object]$Value,
        [Parameter(Mandatory = $true)][string[]]$Expected,
        [Parameter(Mandatory = $true)][string]$Role
    )
    $actual = @($Value.PSObject.Properties.Name)
    if ($actual.Count -ne $Expected.Count) { throw "$Role property count is invalid." }
    for ($index = 0; $index -lt $Expected.Count; $index += 1) {
        if ([string]$actual[$index] -cne $Expected[$index]) { throw "$Role properties are invalid." }
    }
}

function Assert-SafeRoot {
    $appRoot = [IO.Path]::GetFullPath('C:\HerdrSandbox')
    $root = [IO.Path]::GetFullPath($script:Root)
    if (-not $root.StartsWith($appRoot.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Mobile SSH root is outside the app-owned guest root.'
    }
    $current = $root
    while ($true) {
        $item = Get-Item -LiteralPath $current -Force -ErrorAction SilentlyContinue
        if ($null -ne $item -and ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Mobile SSH path contains a reparse point: $current"
        }
        if ([string]::Equals($current, $appRoot, [StringComparison]::OrdinalIgnoreCase)) { break }
        $parent = [IO.Directory]::GetParent($current)
        if ($null -eq $parent) { throw 'Mobile SSH root escaped its app-owned parent.' }
        $current = $parent.FullName
    }
}

function Assert-RegularFile {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][long]$Maximum,
        [Parameter(Mandatory = $true)][string]$Role
    )
    $item = Get-Item -LiteralPath $Path -Force -ErrorAction Stop
    if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
        $item.Length -le 0 -or $item.Length -gt $Maximum) {
        throw "$Role is not one bounded regular non-reparse file."
    }
    return $item
}

function Set-AtomicBytes {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][byte[]]$Bytes
    )
    Assert-SafeRoot
    $temporary = Join-Path $script:Root ('.mobile-ssh-' + [Guid]::NewGuid().ToString('N') + '.tmp')
    try {
        [IO.File]::WriteAllBytes($temporary, $Bytes)
        [void](Assert-RegularFile -Path $temporary -Maximum 65536 -Role 'Mobile SSH staging file')
        if (Test-Path -LiteralPath $Path) {
            [void](Assert-RegularFile -Path $Path -Maximum 65536 -Role 'Mobile SSH replacement target')
            [IO.File]::Replace($temporary, $Path, $null)
        } else {
            [IO.File]::Move($temporary, $Path)
        }
    } finally {
        if (Test-Path -LiteralPath $temporary) { [IO.File]::Delete($temporary) }
    }
}

function Set-AtomicText {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Text
    )
    Set-AtomicBytes -Path $Path -Bytes $script:Utf8NoBom.GetBytes($Text)
}

function Protect-KeyFile {
    param([Parameter(Mandatory = $true)][string]$Path)
    & icacls.exe $Path '/inheritance:r' '/grant' '*S-1-5-32-544:F' 'SYSTEM:F' | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Could not secure mobile SSH key file: $Path" }
}

function Get-OpenSSHExecutable {
    param([Parameter(Mandatory = $true)][string]$Name)
    $path = Join-Path $env:ProgramFiles ('OpenSSH\' + $Name)
    [void](Assert-RegularFile -Path $path -Maximum 67108864 -Role "OpenSSH $Name")
    return $path
}

function Get-CurrentTailscaleIPv4 {
    $tailscale = (Get-Command 'tailscale.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
    $raw = & $tailscale status --json --peers=false 2>$null | Out-String
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($raw)) { throw 'Tailscale status is unavailable.' }
    $status = $raw | ConvertFrom-Json
    if ([string]$status.BackendState -cne 'Running' -or $null -eq $status.Self -or
        [string]$status.Self.HostName -cne 'herdr-sandbox') {
        throw 'Tailscale is not the expected running herdr-sandbox node.'
    }
    $addresses = @($status.TailscaleIPs | Where-Object {
        $parsed = $null
        [Net.IPAddress]::TryParse([string]$_, [ref]$parsed) -and
            $parsed.AddressFamily -eq [Net.Sockets.AddressFamily]::InterNetwork
    })
    if ($addresses.Count -ne 1) { throw 'Tailscale did not report exactly one IPv4 address.' }
    return [string]$addresses[0]
}

function Assert-Ed25519PublicKey {
    param([Parameter(Mandatory = $true)][string]$Value)
    if ($Value.Length -le 0 -or $Value.Length -gt 1024 -or
        $Value -notmatch '^ssh-ed25519 [A-Za-z0-9+/]+={0,2}$') {
        throw 'Mobile SSH public key is not canonical Ed25519 text.'
    }
    $fields = $Value.Split(' ')
    try { $blob = [Convert]::FromBase64String($fields[1]) } catch { throw 'Mobile SSH public key base64 is invalid.' }
    $algorithmLength = ([int]$blob[0] * 16777216) + ([int]$blob[1] * 65536) + ([int]$blob[2] * 256) + [int]$blob[3]
    $keyLength = ([int]$blob[15] * 16777216) + ([int]$blob[16] * 65536) + ([int]$blob[17] * 256) + [int]$blob[18]
    if ($blob.Length -ne 51 -or $algorithmLength -ne 11 -or
        [Text.Encoding]::ASCII.GetString($blob, 4, 11) -cne 'ssh-ed25519' -or
        $keyLength -ne 32) {
        throw 'Mobile SSH public key payload is invalid.'
    }
}

function Read-PreparedState {
    [void](Assert-RegularFile -Path $script:PreparedPath -Maximum 8192 -Role 'Mobile SSH prepared state')
    $text = [IO.File]::ReadAllText($script:PreparedPath)
    try { $state = $text | ConvertFrom-Json } catch { throw 'Mobile SSH prepared state is invalid JSON.' }
    Assert-ExactProperties -Value $state -Expected @('schemaVersion', 'tailscaleIPv4', 'hostPublicKey', 'authorizedKeysSHA256', 'scriptSHA256') -Role 'Mobile SSH prepared state'
    if ($state.schemaVersion -isnot [int] -or [int]$state.schemaVersion -ne $script:SchemaVersion -or
        $state.tailscaleIPv4 -isnot [string] -or $state.hostPublicKey -isnot [string] -or
        $state.authorizedKeysSHA256 -isnot [string] -or [string]$state.authorizedKeysSHA256 -notmatch '^[0-9a-f]{64}$' -or
        $state.scriptSHA256 -isnot [string] -or [string]$state.scriptSHA256 -notmatch '^[0-9a-f]{64}$') {
        throw 'Mobile SSH prepared state values are invalid.'
    }
    Assert-Ed25519PublicKey -Value ([string]$state.hostPublicKey)
    return $state
}

function Assert-PreparedFiles {
    param([Parameter(Mandatory = $true)][object]$State)
    Assert-SafeRoot
    if ((Get-CurrentTailscaleIPv4) -cne [string]$State.tailscaleIPv4) {
        throw 'Mobile SSH prepared Tailscale IPv4 address changed.'
    }
    foreach ($entry in @(
        @($script:PrivateKeyPath, 16384, 'Mobile SSH host private key'),
        @($script:PublicKeyPath, 1024, 'Mobile SSH host public key'),
        @($script:AuthorizedKeysPath, 8192, 'Mobile SSH authorized keys'),
        @($script:ConfigPath, 8192, 'Mobile SSH server configuration'),
        @($script:StableScriptPath, 65536, 'Mobile SSH control script')
    )) {
        [void](Assert-RegularFile -Path ([string]$entry[0]) -Maximum ([long]$entry[1]) -Role ([string]$entry[2]))
    }
    $publicKey = ([IO.File]::ReadAllText($script:PublicKeyPath)).Trim()
    if ($publicKey -cne [string]$State.hostPublicKey) { throw 'Mobile SSH host public key changed.' }
    $sshKeygen = Get-OpenSSHExecutable -Name 'ssh-keygen.exe'
    $derivedOutput = @(& $sshKeygen -y -f $script:PrivateKeyPath 2>&1)
    if ($LASTEXITCODE -ne 0) { throw "Mobile SSH host private key validation failed: $($derivedOutput -join ' ')" }
    $derivedPublicKey = ([string]($derivedOutput | Select-Object -First 1)).Trim()
    Assert-Ed25519PublicKey -Value $derivedPublicKey
    if ($derivedPublicKey -cne [string]$State.hostPublicKey) { throw 'Mobile SSH host private key no longer matches its stable public identity.' }
    $authorizedDigest = (Get-FileHash -LiteralPath $script:AuthorizedKeysPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($authorizedDigest -cne [string]$State.authorizedKeysSHA256) { throw 'Mobile SSH authorized keys changed.' }
    $scriptDigest = (Get-FileHash -LiteralPath $script:StableScriptPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($scriptDigest -cne [string]$State.scriptSHA256) { throw 'Mobile SSH control script changed.' }
    $sshd = Get-OpenSSHExecutable -Name 'sshd.exe'
    $output = @(& $sshd -t -f $script:ConfigPath 2>&1)
    if ($LASTEXITCODE -ne 0) { throw "Mobile SSH server configuration validation failed: $($output -join ' ')" }
    return $sshd
}

function Invoke-Prepare {
    if ([string]::IsNullOrWhiteSpace($RequestPath)) { throw 'Mobile SSH prepare request path is required.' }
    [void](Assert-RegularFile -Path $RequestPath -Maximum 65536 -Role 'Mobile SSH prepare request')
    $requestText = [IO.File]::ReadAllText($RequestPath)
    try { $request = $requestText | ConvertFrom-Json } catch { throw 'Mobile SSH prepare request is invalid JSON.' }
    Assert-ExactProperties -Value $request -Expected @('schemaVersion', 'tailscaleIPv4', 'authorizedKeys', 'privateKey', 'publicKey', 'scriptSHA256') -Role 'Mobile SSH prepare request'
    if ($request.schemaVersion -isnot [int] -or [int]$request.schemaVersion -ne $script:SchemaVersion -or
        $request.tailscaleIPv4 -isnot [string] -or $null -eq $request.authorizedKeys -or
        $request.privateKey -isnot [string] -or $request.publicKey -isnot [string] -or
        $request.scriptSHA256 -isnot [string] -or [string]$request.scriptSHA256 -notmatch '^[0-9a-f]{64}$') {
        throw 'Mobile SSH prepare request values are invalid.'
    }
    if ((Get-CurrentTailscaleIPv4) -cne [string]$request.tailscaleIPv4) {
        throw 'Mobile SSH prepare request does not match the running Tailscale IPv4 address.'
    }
    $authorizedKeys = @($request.authorizedKeys)
    if ($authorizedKeys.Count -lt 1 -or $authorizedKeys.Count -gt 8) { throw 'Mobile SSH prepare request key count is invalid.' }
    $previousKey = ''
    foreach ($key in $authorizedKeys) {
        if ($key -isnot [string]) { throw 'Mobile SSH authorized key must be text.' }
        Assert-Ed25519PublicKey -Value ([string]$key)
        if (-not [string]::IsNullOrEmpty($previousKey) -and [string]::CompareOrdinal($previousKey, [string]$key) -ge 0) {
            throw 'Mobile SSH authorized keys are not sorted and unique.'
        }
        $previousKey = [string]$key
    }
    Assert-SafeRoot
    if (-not (Test-Path -LiteralPath $script:Root -PathType Container)) {
        New-Item -ItemType Directory -Path $script:Root -ErrorAction Stop | Out-Null
    }
    Assert-SafeRoot
    if (Test-Path -LiteralPath $script:ProcessPath) { throw 'Mobile SSH process state already exists before preparation.' }
    if (Get-NetTCPConnection -State Listen -LocalPort $script:MobilePort -ErrorAction SilentlyContinue) {
        throw 'Mobile SSH port is already owned before preparation.'
    }
    $sourceScriptDigest = (Get-FileHash -LiteralPath $PSCommandPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($sourceScriptDigest -cne [string]$request.scriptSHA256) { throw 'Mobile SSH control script SHA-256 mismatch.' }
    Set-AtomicBytes -Path $script:StableScriptPath -Bytes ([IO.File]::ReadAllBytes($PSCommandPath))

    $sshKeygen = Get-OpenSSHExecutable -Name 'ssh-keygen.exe'
    if ([string]::IsNullOrEmpty([string]$request.privateKey)) {
        if (-not [string]::IsNullOrEmpty([string]$request.publicKey)) { throw 'Mobile SSH generated identity request contains only a public key.' }
        $output = @(& $sshKeygen -q -t ed25519 -N '' -C 'herdr-sandbox-mobile-host' -f $script:PrivateKeyPath 2>&1)
        if ($LASTEXITCODE -ne 0) { throw "Mobile SSH host-key generation failed: $($output -join ' ')" }
    } else {
        if ([string]::IsNullOrEmpty([string]$request.publicKey)) { throw 'Mobile SSH restored identity request is missing its public key.' }
        try { $privateKey = [Convert]::FromBase64String([string]$request.privateKey) } catch { throw 'Mobile SSH restored private key base64 is invalid.' }
        if ($privateKey.Length -le 0 -or $privateKey.Length -gt 16384) { throw 'Mobile SSH restored private key size is invalid.' }
        Assert-Ed25519PublicKey -Value ([string]$request.publicKey)
        Set-AtomicBytes -Path $script:PrivateKeyPath -Bytes $privateKey
        $privateKey = $null
        Set-AtomicText -Path $script:PublicKeyPath -Text ([string]$request.publicKey + "`n")
    }
    Protect-KeyFile -Path $script:PrivateKeyPath
    Protect-KeyFile -Path $script:PublicKeyPath
    $derivedOutput = @(& $sshKeygen -y -f $script:PrivateKeyPath 2>&1)
    if ($LASTEXITCODE -ne 0) { throw "Mobile SSH host private key validation failed: $($derivedOutput -join ' ')" }
    $derivedPublicKey = ([string]($derivedOutput | Select-Object -First 1)).Trim()
    Assert-Ed25519PublicKey -Value $derivedPublicKey
    $storedPublicKey = ([IO.File]::ReadAllText($script:PublicKeyPath)).Trim()
    $storedFields = $storedPublicKey.Split(' ')
    $storedCanonical = $storedFields[0] + ' ' + $storedFields[1]
    if ($derivedPublicKey -cne $storedCanonical) { throw 'Mobile SSH host private and public keys do not match.' }
    Set-AtomicText -Path $script:PublicKeyPath -Text ($derivedPublicKey + "`n")
    Protect-KeyFile -Path $script:PublicKeyPath

    $authorizedText = ($authorizedKeys -join "`n") + "`n"
    Set-AtomicText -Path $script:AuthorizedKeysPath -Text $authorizedText
    Protect-KeyFile -Path $script:AuthorizedKeysPath
    $sshdConfig = @"
Port $($script:MobilePort)
AddressFamily inet
ListenAddress $([string]$request.tailscaleIPv4)
HostKey C:/HerdrSandbox/mobile-ssh/ssh_host_ed25519_key
AuthorizedKeysFile C:/HerdrSandbox/mobile-ssh/authorized_keys
PubkeyAuthentication yes
AuthenticationMethods publickey
PasswordAuthentication no
PermitEmptyPasswords no
PermitTTY yes
AllowUsers $($script:MobileUserRule)
DisableForwarding yes
AllowAgentForwarding no
AllowTcpForwarding no
GatewayPorts no
MaxAuthTries 3
MaxSessions 4
MaxStartups 3:30:6
LogLevel VERBOSE
SyslogFacility LOCAL0
"@
    Set-AtomicText -Path $script:ConfigPath -Text $sshdConfig
    $sshd = Get-OpenSSHExecutable -Name 'sshd.exe'
    $validation = @(& $sshd -t -f $script:ConfigPath 2>&1)
    if ($LASTEXITCODE -ne 0) { throw "Mobile SSH server configuration validation failed: $($validation -join ' ')" }
    $prepared = [ordered]@{
        schemaVersion = $script:SchemaVersion
        tailscaleIPv4 = [string]$request.tailscaleIPv4
        hostPublicKey = $derivedPublicKey
        authorizedKeysSHA256 = (Get-FileHash -LiteralPath $script:AuthorizedKeysPath -Algorithm SHA256).Hash.ToLowerInvariant()
        scriptSHA256 = $sourceScriptDigest
    }
    Set-AtomicText -Path $script:PreparedPath -Text (($prepared | ConvertTo-Json -Compress) + "`n")
    $privateBytes = [IO.File]::ReadAllBytes($script:PrivateKeyPath)
    try {
        [ordered]@{
            schemaVersion = $script:SchemaVersion
            privateKey = [Convert]::ToBase64String($privateBytes)
            publicKey = $derivedPublicKey
        } | ConvertTo-Json -Compress
    } finally {
        [Array]::Clear($privateBytes, 0, $privateBytes.Length)
    }
}

function Remove-OwnedFirewallRules {
    foreach ($name in @($script:AllowRuleName, $script:BlockRuleName)) {
        $rule = Get-NetFirewallRule -Name $name -ErrorAction SilentlyContinue
        if ($null -ne $rule) { Remove-NetFirewallRule -Name $name -ErrorAction Stop }
    }
}

function Assert-FirewallRules {
    param([Parameter(Mandatory = $true)][string]$TailscaleIPv4)
    $allow = Get-NetFirewallRule -Name $script:AllowRuleName -ErrorAction Stop
    $block = Get-NetFirewallRule -Name $script:BlockRuleName -ErrorAction Stop
    if ([string]$allow.Direction -cne 'Inbound' -or [string]$allow.Action -cne 'Allow' -or [string]$allow.Enabled -cne 'True' -or
        [string]$block.Direction -cne 'Inbound' -or [string]$block.Action -cne 'Block' -or [string]$block.Enabled -cne 'True') {
        throw 'Mobile SSH firewall rule actions are invalid.'
    }
    $allowPort = $allow | Get-NetFirewallPortFilter
    $allowAddress = $allow | Get-NetFirewallAddressFilter
    $blockPort = $block | Get-NetFirewallPortFilter
    $blockAddress = $block | Get-NetFirewallAddressFilter
    $allowRemote = [string]$allowAddress.RemoteAddress
    $blockRemote = [string]$blockAddress.RemoteAddress
    if ([string]$allowPort.Protocol -cne 'TCP' -or [string]$allowPort.LocalPort -cne [string]$script:MobilePort -or
        [string]$allowAddress.LocalAddress -cne $TailscaleIPv4 -or $allowRemote -notin @('100.64.0.0/10', '100.64.0.0/255.192.0.0') -or
        [string]$blockPort.Protocol -cne 'TCP' -or [string]$blockPort.LocalPort -cne '22' -or
        $blockRemote -notin @('100.64.0.0/10', '100.64.0.0/255.192.0.0')) {
        throw 'Mobile SSH firewall rule filters are invalid.'
    }
}

function Read-ProcessState {
    [void](Assert-RegularFile -Path $script:ProcessPath -Maximum 8192 -Role 'Mobile SSH process state')
    try { $state = [IO.File]::ReadAllText($script:ProcessPath) | ConvertFrom-Json } catch { throw 'Mobile SSH process state is invalid JSON.' }
    Assert-ExactProperties -Value $state -Expected @('schemaVersion', 'pid', 'executablePath', 'startedAtUTC', 'commandLine', 'tailscaleIPv4') -Role 'Mobile SSH process state'
    if ($state.schemaVersion -isnot [int] -or [int]$state.schemaVersion -ne $script:SchemaVersion -or
        $state.pid -isnot [int] -or [int]$state.pid -lt 1 -or $state.executablePath -isnot [string] -or
        $state.startedAtUTC -isnot [string] -or $state.commandLine -isnot [string] -or
        $state.tailscaleIPv4 -isnot [string]) {
        throw 'Mobile SSH process state values are invalid.'
    }
    return $state
}

function Assert-RunningEndpoint {
    param([Parameter(Mandatory = $true)][object]$Prepared)
    $state = Read-ProcessState
    if ([string]$state.tailscaleIPv4 -cne [string]$Prepared.tailscaleIPv4) { throw 'Mobile SSH process Tailscale identity changed.' }
    $item = Get-CimInstance Win32_Process -Filter ("ProcessId = " + [int]$state.pid) -ErrorAction Stop
    if ($null -eq $item -or [string]$item.Name -ine 'sshd.exe' -or
        -not [string]::Equals([IO.Path]::GetFullPath([string]$item.ExecutablePath), [IO.Path]::GetFullPath([string]$state.executablePath), [StringComparison]::OrdinalIgnoreCase) -or
        [string]$item.CommandLine -cne [string]$state.commandLine) {
        throw 'Mobile SSH process identity changed.'
    }
    $process = Get-Process -Id ([int]$state.pid) -ErrorAction Stop
    if ($process.StartTime.ToUniversalTime().ToString('O') -cne [string]$state.startedAtUTC) { throw 'Mobile SSH process start identity changed.' }
    $listeners = @(Get-NetTCPConnection -State Listen -LocalAddress ([string]$Prepared.tailscaleIPv4) -LocalPort $script:MobilePort -ErrorAction Stop |
        Where-Object { [int]$_.OwningProcess -eq [int]$state.pid })
    if ($listeners.Count -ne 1) { throw 'Mobile SSH listener identity is unavailable.' }
    Assert-FirewallRules -TailscaleIPv4 ([string]$Prepared.tailscaleIPv4)
    return $state
}

function Invoke-Activate {
    $prepared = Read-PreparedState
    $sshd = Assert-PreparedFiles -State $prepared
    if (Test-Path -LiteralPath $script:ProcessPath) { throw 'Mobile SSH process state already exists before activation.' }
    if (Get-NetTCPConnection -State Listen -LocalPort $script:MobilePort -ErrorAction SilentlyContinue) {
        throw 'Mobile SSH port is already owned before activation.'
    }
    foreach ($name in @($script:AllowRuleName, $script:BlockRuleName)) {
        if (Get-NetFirewallRule -Name $name -ErrorAction SilentlyContinue) { throw "Mobile SSH firewall rule already exists before activation: $name" }
    }
    $process = $null
    $activated = $false
    try {
        New-NetFirewallRule -Name $script:AllowRuleName -DisplayName 'Herdr Sandbox mobile SSH over Tailscale' `
            -Enabled True -Direction Inbound -Protocol TCP -Action Allow -LocalAddress ([string]$prepared.tailscaleIPv4) `
            -RemoteAddress '100.64.0.0/10' -LocalPort $script:MobilePort | Out-Null
        New-NetFirewallRule -Name $script:BlockRuleName -DisplayName 'Block Herdr Sandbox management SSH from Tailscale' `
            -Enabled True -Direction Inbound -Protocol TCP -Action Block -RemoteAddress '100.64.0.0/10' -LocalPort 22 | Out-Null
        Assert-FirewallRules -TailscaleIPv4 ([string]$prepared.tailscaleIPv4)
        foreach ($path in @($script:LogPath, $script:StdoutPath, $script:StderrPath)) {
            if (Test-Path -LiteralPath $path) { [void](Assert-RegularFile -Path $path -Maximum 1048576 -Role 'Mobile SSH log') }
        }
        $arguments = @('-D', '-f', $script:ConfigPath, '-E', $script:LogPath)
        $process = Start-Process -FilePath $sshd -ArgumentList $arguments -WindowStyle Hidden `
            -RedirectStandardOutput $script:StdoutPath -RedirectStandardError $script:StderrPath -PassThru
        $deadline = [DateTime]::UtcNow.AddSeconds(30)
        do {
            if ($process.HasExited) { throw "Mobile SSH server exited with code $($process.ExitCode)." }
            $listener = @(Get-NetTCPConnection -State Listen -LocalAddress ([string]$prepared.tailscaleIPv4) `
                -LocalPort $script:MobilePort -ErrorAction SilentlyContinue | Where-Object { [int]$_.OwningProcess -eq $process.Id })
            if ($listener.Count -eq 1) { break }
            Start-Sleep -Milliseconds 200
        } while ([DateTime]::UtcNow -lt $deadline)
        if ($listener.Count -ne 1) { throw 'Mobile SSH server did not begin listening within 30 seconds.' }
        $item = Get-CimInstance Win32_Process -Filter ("ProcessId = " + $process.Id) -ErrorAction Stop
        if ($null -eq $item -or [string]$item.Name -ine 'sshd.exe') { throw 'Mobile SSH process identity was unavailable after launch.' }
        $state = [ordered]@{
            schemaVersion = $script:SchemaVersion
            pid = [int]$process.Id
            executablePath = [string]$item.ExecutablePath
            startedAtUTC = $process.StartTime.ToUniversalTime().ToString('O')
            commandLine = [string]$item.CommandLine
            tailscaleIPv4 = [string]$prepared.tailscaleIPv4
        }
        Set-AtomicText -Path $script:ProcessPath -Text (($state | ConvertTo-Json -Compress) + "`n")
        $verified = Assert-RunningEndpoint -Prepared $prepared
        $activated = $true
        [ordered]@{ schemaVersion = $script:SchemaVersion; state = 'running'; pid = [int]$verified.pid } | ConvertTo-Json -Compress
    } finally {
        if (-not $activated) {
            if ($null -ne $process -and -not $process.HasExited) {
                $process.Kill()
                if (-not $process.WaitForExit(15000)) { throw 'Mobile SSH failed-process cleanup timed out.' }
            }
            Remove-OwnedFirewallRules
            if (Test-Path -LiteralPath $script:ProcessPath) { [IO.File]::Delete($script:ProcessPath) }
        }
    }
}

function Invoke-Verify {
    $prepared = Read-PreparedState
    if ($ExpectedScriptSHA256 -notmatch '^[0-9a-f]{64}$' -or [string]$prepared.scriptSHA256 -cne $ExpectedScriptSHA256) {
        throw 'Mobile SSH control script differs from the current application; launch a fresh Sandbox.'
    }
    [void](Assert-PreparedFiles -State $prepared)
    $state = Assert-RunningEndpoint -Prepared $prepared
    [ordered]@{ schemaVersion = $script:SchemaVersion; state = 'running'; pid = [int]$state.pid } | ConvertTo-Json -Compress
}

Assert-SafeRoot
switch ($Mode) {
    'Prepare' { Invoke-Prepare }
    'Activate' { Invoke-Activate }
    'Verify' { Invoke-Verify }
}
