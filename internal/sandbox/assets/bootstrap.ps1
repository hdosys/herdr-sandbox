param(
    [Parameter(Mandatory = $true)]
    [string]$InputDirectory,

    [Parameter(Mandatory = $true)]
    [string]$StatusDirectory,

    [Parameter(Mandatory = $true)]
    [ValidateSet('Disabled', 'Enabled')]
    [string]$AudioPlayback,

    [Parameter(Mandatory = $true)]
    [ValidateSet('Disabled', 'Enabled')]
    [string]$AudioInput,

    [Parameter(Mandatory = $true)]
    [ValidateRange(1, 60)]
    [int]$ConfigurationHandoffTimeoutMinutes
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$script:Phase = 'startup'
$script:Utf8NoBom = New-Object System.Text.UTF8Encoding($false)
try { $Host.UI.RawUI.WindowTitle = 'Sandbox bootstrap' } catch {}

function Write-AtomicJson {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,

        [Parameter(Mandatory = $true)]
        [object]$Value
    )

    $temporaryPath = $Path + '.' + [Guid]::NewGuid().ToString('N') + '.tmp'
    $json = $Value | ConvertTo-Json -Compress -Depth 4
    [IO.File]::WriteAllText($temporaryPath, $json, $script:Utf8NoBom)
    $publicationError = $null
    for ($attempt = 1; $attempt -le 30; $attempt += 1) {
        $backupPath = $null
        try {
            if (Test-Path -LiteralPath $Path -PathType Leaf) {
                $backupPath = $Path + '.' + [Guid]::NewGuid().ToString('N') + '.bak'
                [IO.File]::Replace($temporaryPath, $Path, $backupPath, $true)
            } else {
                [IO.File]::Move($temporaryPath, $Path)
            }
            return
        } catch [IO.IOException] {
            $publicationError = $_.Exception
        } catch [UnauthorizedAccessException] {
            $publicationError = $_.Exception
        } finally {
            if (-not [string]::IsNullOrWhiteSpace($backupPath)) {
                try { [IO.File]::Delete($backupPath) } catch { }
            }
        }
        Start-Sleep -Milliseconds 100
    }
    throw "Atomic JSON publication failed after 30 attempts: $($publicationError.Message)"
}

function Write-ProgressStatus {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Phase,

        [Parameter(Mandatory = $true)]
        [string]$Message
    )

    $script:Phase = $Phase
    Write-Host "[$Phase] $Message"
    Write-AtomicJson -Path (Join-Path $StatusDirectory 'progress.json') -Value ([ordered]@{
        schemaVersion = 1
        phase = $Phase
        message = $Message
    })
}

function Read-ConfigurationHandoff {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    $item = Get-Item -LiteralPath $Path -ErrorAction Stop
    if ($item.Length -le 0 -or $item.Length -gt 16384) {
        throw "Configuration handoff size is invalid: $($item.Length)"
    }
    $text = [IO.File]::ReadAllText($item.FullName)
    $verified = '{"schemaVersion":1,"outcome":"verified"}'
    if ($text -ceq $verified) {
        return [pscustomobject]@{ outcome = 'verified'; phase = ''; message = ''; mobileAccess = $null }
    }
    $failurePattern = '^\{"schemaVersion":1,"outcome":"failed","phase":"(?:[^"\\]|\\.)*","message":"(?:[^"\\]|\\.)*"\}$'
    try {
        $handoff = $text | ConvertFrom-Json
    } catch {
        throw "Configuration handoff is not valid JSON: $($_.Exception.Message)"
    }
    if ($text -match $failurePattern) {
        if ($handoff.schemaVersion -isnot [int] -or [int]$handoff.schemaVersion -ne 1 -or
            $handoff.outcome -isnot [string] -or [string]$handoff.outcome -cne 'failed' -or
            $handoff.phase -isnot [string] -or
            [string]::IsNullOrWhiteSpace([string]$handoff.phase) -or
            $handoff.message -isnot [string] -or
            [string]::IsNullOrWhiteSpace([string]$handoff.message) -or
            ([string]$handoff.message).Length -gt 4096) {
            throw 'Failed configuration handoff values are invalid.'
        }
        return [pscustomobject]@{
            outcome = [string]$handoff.outcome
            phase = [string]$handoff.phase
            message = [string]$handoff.message
            mobileAccess = $null
        }
    }
    $properties = @($handoff.PSObject.Properties.Name)
    if (($properties -join '|') -cne 'schemaVersion|outcome|mobileAccess' -or
        $handoff.schemaVersion -isnot [int] -or [int]$handoff.schemaVersion -ne 1 -or
        $handoff.outcome -isnot [string] -or [string]$handoff.outcome -cne 'verified' -or
        $null -eq $handoff.mobileAccess) {
        throw 'Configuration handoff is not canonical.'
    }
    $mobile = $handoff.mobileAccess
    $mobileProperties = @($mobile.PSObject.Properties.Name)
    if (($mobileProperties -join '|') -cne 'uri|dnsName|ipv4|sshUser|port|hostKeyFingerprint|qr' -or
        $mobile.uri -isnot [string] -or $mobile.dnsName -isnot [string] -or $mobile.ipv4 -isnot [string] -or
        $mobile.sshUser -isnot [string] -or [string]$mobile.sshUser -cne 'WDAGUtilityAccount' -or
        $mobile.port -isnot [int] -or [int]$mobile.port -ne 2222 -or
        $mobile.hostKeyFingerprint -isnot [string] -or
        [string]$mobile.hostKeyFingerprint -notmatch '^SHA256:[A-Za-z0-9+/]{43}$' -or
        [string]$mobile.dnsName -notmatch '^herdr-sandbox\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*\.ts\.net$' -or
        [string]$mobile.uri -cne ('ssh://WDAGUtilityAccount@' + [string]$mobile.dnsName + ':2222')) {
        throw 'Mobile access handoff values are invalid.'
    }
    $parsedIPv4 = $null
    if (-not [Net.IPAddress]::TryParse([string]$mobile.ipv4, [ref]$parsedIPv4) -or
        $parsedIPv4.AddressFamily -ne [Net.Sockets.AddressFamily]::InterNetwork -or
        $parsedIPv4.ToString() -cne [string]$mobile.ipv4) {
        throw 'Mobile access handoff IPv4 address is invalid.'
    }
    $qr = @($mobile.qr)
    if ($qr.Count -lt 29 -or $qr.Count -gt 65) { throw 'Mobile access QR height is invalid.' }
    foreach ($line in $qr) {
        if ($line -isnot [string] -or ([string]$line).Length -ne (2 * $qr.Count) -or
            [string]$line -notmatch '^(?:##|  )+$') {
            throw 'Mobile access QR matrix is invalid.'
        }
    }
    return [pscustomobject]@{
        outcome = 'verified'
        phase = ''
        message = ''
        mobileAccess = $mobile
    }
}

function Get-BoundedDiagnosticText {
    param(
        [AllowEmptyString()]
        [string]$Text,

        [Parameter(Mandatory = $true)]
        [int]$MaximumBytes
    )

    if ([string]::IsNullOrEmpty($Text)) {
        return ''
    }
    if ($MaximumBytes -lt 128) {
        throw "Diagnostic byte limit is too small: $MaximumBytes"
    }
    $encoding = [Text.Encoding]::UTF8
    $bytes = $encoding.GetBytes($Text)
    if ($bytes.Length -le $MaximumBytes) {
        return $Text
    }
    $marker = "`n... diagnostic truncated; original UTF-8 bytes: $($bytes.Length) ...`n"
    $markerBytes = $encoding.GetByteCount($marker)
    $contentBudget = $MaximumBytes - $markerBytes - 16
    $headLength = [int][Math]::Floor($contentBudget / 2)
    $tailLength = $contentBudget - $headLength
    $head = $encoding.GetString($bytes, 0, $headLength)
    $tail = $encoding.GetString($bytes, $bytes.Length - $tailLength, $tailLength)
    return $head + $marker + $tail
}

function Invoke-NativeCapture {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,

        [Parameter(Mandatory = $true)]
        [object]$FilePath,

        [string[]]$ArgumentList = @(),

        [Parameter(Mandatory = $true)]
        [ref]$ExitCode
    )

    $pathValues = @($FilePath)
    if ($pathValues.Count -ne 1) {
        throw "$Role executable resolved to $($pathValues.Count) values; expected exactly one."
    }
    $resolvedFilePath = [string]$pathValues[0]
    if ([string]::IsNullOrWhiteSpace($resolvedFilePath)) {
        throw "$Role executable path is empty."
    }
    $command = Get-Command $resolvedFilePath -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $command) {
        throw "$Role executable is not available: $resolvedFilePath"
    }
    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $output = @(& $command.Source @ArgumentList 2>&1 | ForEach-Object { [string]$_ })
        $capturedExitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    $ExitCode.Value = $capturedExitCode
    return $output
}

function Invoke-Native {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,

        [Parameter(Mandatory = $true)]
        [object]$FilePath,

        [string[]]$ArgumentList = @()
    )

    $exitCode = 0
    $output = @(Invoke-NativeCapture -Role $Role -FilePath $FilePath -ArgumentList $ArgumentList -ExitCode ([ref]$exitCode))
    if ($exitCode -ne 0) {
        $detail = Get-BoundedDiagnosticText -Text (($output -join [Environment]::NewLine).Trim()) -MaximumBytes 1600
        throw "$Role exited with code $exitCode. $detail"
    }
    return $output
}

function Invoke-HerdrBoundary {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,

        [Parameter(Mandatory = $true)]
        [string]$FilePath,

        [Parameter(Mandatory = $true)]
        [string[]]$ArgumentList,

        [ValidateRange(1, 60)]
        [int]$TimeoutSeconds = 30
    )

    if ($null -eq ('HerdrSandbox.ProvisioningProcess' -as [type])) {
        throw 'The bounded provisioning process owner is unavailable.'
    }
    $spec = New-Object HerdrSandbox.ProvisioningProcessSpec
    $spec.Role = $Role
    $spec.FilePath = $FilePath
    $spec.Arguments = [string[]]@($ArgumentList)
    $spec.WorkingDirectory = [IO.Path]::GetFullPath($env:USERPROFILE)
    $spec.TimeoutMilliseconds = $TimeoutSeconds * 1000
    $spec.SuccessExitCodes = [int[]]@(0)
    $spec.TerminateDescendantsAfterRootExit = $true
    $result = [HerdrSandbox.ProvisioningProcess]::Run($spec)
    if (-not $result.Succeeded -or $result.OutputTruncated -or $result.OutputBytes -gt 65536) {
        $detail = Get-BoundedDiagnosticText -Text ([string]$result.Output) -MaximumBytes 1600
        if ($result.TimedOut) { throw "$Role exceeded $TimeoutSeconds seconds. $detail" }
        if ($result.Stopped) { throw "$Role was stopped before completion. $detail" }
        if ($result.OutputTruncated -or $result.OutputBytes -gt 65536) { throw "$Role exceeded the 65536-byte output limit. $detail" }
        throw "$Role exited with code $($result.ExitCode). $detail"
    }
    return [string]$result.Output
}

function Assert-BootstrapCachePath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,

        [Parameter(Mandatory = $true)]
        [string]$TrustRoot
    )

    $root = [IO.Path]::GetFullPath($TrustRoot).TrimEnd('\')
    $candidate = [IO.Path]::GetFullPath($Path).TrimEnd('\')
    if ($candidate -ine $root -and
        -not $candidate.StartsWith($root + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw "Bootstrap cache path escapes $root`: $candidate"
    }
    $current = $candidate
    while ($current.Length -ge $root.Length) {
        if (Test-Path -LiteralPath $current) {
            $item = Get-Item -LiteralPath $current -Force
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "Bootstrap cache path contains a reparse point: $current"
            }
        }
        if ($current -ieq $root) {
            return
        }
        $parent = Split-Path -Parent $current
        if ([string]::IsNullOrWhiteSpace($parent) -or $parent -ieq $current) {
            throw "Bootstrap cache parent resolution failed: $candidate"
        }
        $current = $parent.TrimEnd('\')
    }
    throw "Bootstrap cache path does not reach trust root $root`: $candidate"
}

function Assert-BootstrapCacheTree {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,

        [Parameter(Mandatory = $true)]
        [string]$TrustRoot
    )

    Assert-BootstrapCachePath -Path $Path -TrustRoot $TrustRoot
    if (-not (Test-Path -LiteralPath $Path -PathType Container)) {
        return
    }
    $pending = New-Object 'System.Collections.Generic.List[string]'
    $pending.Add([IO.Path]::GetFullPath($Path)) | Out-Null
    while ($pending.Count -gt 0) {
        $index = $pending.Count - 1
        $directory = $pending[$index]
        $pending.RemoveAt($index)
        foreach ($item in @(Get-ChildItem -LiteralPath $directory -Force)) {
            Assert-BootstrapCachePath -Path $item.FullName -TrustRoot $TrustRoot
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "Bootstrap cache tree contains a reparse point: $($item.FullName)"
            }
            if ($item.PSIsContainer) {
                $pending.Add($item.FullName) | Out-Null
            }
        }
    }
}

function Get-BootstrapFileSHA256 {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    $stream = [IO.File]::OpenRead($Path)
    $hasher = [Security.Cryptography.SHA256]::Create()
    try {
        $digest = $hasher.ComputeHash($stream)
    } finally {
        $hasher.Dispose()
        $stream.Dispose()
    }
    return ([BitConverter]::ToString($digest)).Replace('-', '').ToLowerInvariant()
}

function Get-PinnedBootstrapAsset {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,

        [Parameter(Mandatory = $true)]
        [ValidatePattern('^[a-z0-9][a-z0-9-]{0,63}$')]
        [string]$CacheKey,

        [Parameter(Mandatory = $true)]
        [string]$Uri,

        [Parameter(Mandatory = $true)]
        [ValidatePattern('^[0-9a-f]{64}$')]
        [string]$ExpectedSHA256,

        [Parameter(Mandatory = $true)]
        [ValidatePattern('^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$')]
        [string]$FileName,

        [Parameter(Mandatory = $true)]
        [string]$DestinationPath,

        [Parameter(Mandatory = $true)]
        [string]$CacheRoot,

        [Parameter(Mandatory = $true)]
        [string]$CacheTrustRoot
    )

    if (-not (Test-Path -LiteralPath $CacheTrustRoot -PathType Container)) {
        throw "Bootstrap cache mapping is missing: $CacheTrustRoot"
    }
    Assert-BootstrapCachePath -Path $CacheTrustRoot -TrustRoot $CacheTrustRoot
    Assert-BootstrapCachePath -Path $CacheRoot -TrustRoot $CacheTrustRoot
    New-Item -ItemType Directory -Path $CacheRoot -Force | Out-Null
    Assert-BootstrapCachePath -Path $CacheRoot -TrustRoot $CacheTrustRoot

    $packageRoot = Join-Path $CacheRoot $CacheKey
    New-Item -ItemType Directory -Path $packageRoot -Force | Out-Null
    Assert-BootstrapCachePath -Path $packageRoot -TrustRoot $CacheTrustRoot
    $entryDirectory = Join-Path $packageRoot $ExpectedSHA256
    $cachePath = Join-Path $entryDirectory $FileName
    $cacheHit = $false
    if (Test-Path -LiteralPath $entryDirectory) {
        Assert-BootstrapCachePath -Path $entryDirectory -TrustRoot $CacheTrustRoot
        if (Test-Path -LiteralPath $cachePath -PathType Leaf) {
            Assert-BootstrapCachePath -Path $cachePath -TrustRoot $CacheTrustRoot
            $cachedDigest = Get-BootstrapFileSHA256 -Path $cachePath
            $cacheHit = $cachedDigest -ceq $ExpectedSHA256
        }
        if (-not $cacheHit) {
            Assert-BootstrapCacheTree -Path $entryDirectory -TrustRoot $CacheTrustRoot
            Remove-Item -LiteralPath $entryDirectory -Recurse -Force
        }
    }

    if (-not $cacheHit) {
        Write-Host "$Role bootstrap cache miss: $ExpectedSHA256"
        $staging = Join-Path $packageRoot ('.staging-' + [Guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Path $staging | Out-Null
        Assert-BootstrapCachePath -Path $staging -TrustRoot $CacheTrustRoot
        try {
            $downloadPath = Join-Path $staging $FileName
            Invoke-WebRequest -Uri $Uri -UseBasicParsing -OutFile $downloadPath -ErrorAction Stop
            $downloadDigest = Get-BootstrapFileSHA256 -Path $downloadPath
            if ($downloadDigest -cne $ExpectedSHA256) {
                throw "$Role checksum mismatch. Expected $ExpectedSHA256 but got $downloadDigest."
            }
            Assert-BootstrapCacheTree -Path $staging -TrustRoot $CacheTrustRoot
            Move-Item -LiteralPath $staging -Destination $entryDirectory
            $staging = ''
        } finally {
            if (-not [string]::IsNullOrWhiteSpace($staging) -and (Test-Path -LiteralPath $staging)) {
                Assert-BootstrapCacheTree -Path $staging -TrustRoot $CacheTrustRoot
                Remove-Item -LiteralPath $staging -Recurse -Force
            }
        }
    } else {
        Write-Host "$Role bootstrap cache hit: $ExpectedSHA256"
    }

    foreach ($directory in @(Get-ChildItem -LiteralPath $packageRoot -Directory -Force)) {
        if ($directory.Name -ine $ExpectedSHA256) {
            Assert-BootstrapCacheTree -Path $directory.FullName -TrustRoot $CacheTrustRoot
            Remove-Item -LiteralPath $directory.FullName -Recurse -Force
        }
    }
    if (-not (Test-Path -LiteralPath $cachePath -PathType Leaf)) {
        throw "$Role cache publication is missing: $cachePath"
    }
    $destination = [IO.Path]::GetFullPath($DestinationPath)
    $trustRoot = [IO.Path]::GetFullPath($CacheTrustRoot).TrimEnd('\')
    if ($destination.StartsWith($trustRoot + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw "$Role guest-local destination must be outside the mapped cache: $destination"
    }
    $destinationParent = Split-Path -Parent $destination
    if (-not (Test-Path -LiteralPath $destinationParent -PathType Container)) {
        throw "$Role destination parent is missing: $destinationParent"
    }
    Copy-Item -LiteralPath $cachePath -Destination $destination -Force
    $destinationDigest = Get-BootstrapFileSHA256 -Path $destination
    if ($destinationDigest -cne $ExpectedSHA256) {
        Remove-Item -LiteralPath $destination -Force -ErrorAction SilentlyContinue
        throw "$Role guest-local copy checksum mismatch. Expected $ExpectedSHA256 but got $destinationDigest."
    }
    return $destination
}

function Get-PowerShell7Installation {
    $packages = @(Get-AppxPackage -Name 'Microsoft.PowerShell' -ErrorAction SilentlyContinue)
    if ($packages.Count -ne 1) {
        throw "Expected one registered stable PowerShell 7 package, found $($packages.Count)."
    }
    $package = $packages[0]
    $packageVersion = [string]$package.Version
    if ([string]$package.Name -cne 'Microsoft.PowerShell' -or
        [string]$package.Architecture -cne 'X64' -or
        [string]$package.Publisher -cne 'CN=Microsoft Corporation, O=Microsoft Corporation, L=Redmond, S=Washington, C=US' -or
        $packageVersion -notmatch '^\d+\.\d+\.\d+\.\d+$') {
        throw "PowerShell 7 package identity is unexpected: name=$($package.Name) version=$packageVersion architecture=$($package.Architecture)"
    }
    $version = [Version]$packageVersion
    $displayVersion = "$($version.Major).$($version.Minor).$($version.Build)"
    $executable = Join-Path ([string]$package.InstallLocation) 'pwsh.exe'
    if (-not (Test-Path -LiteralPath $executable -PathType Leaf)) {
        throw "PowerShell 7 package is missing pwsh.exe: $executable"
    }
    $file = Get-Item -LiteralPath $executable -Force
    if (($file.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
        -not ([string]$file.VersionInfo.FileVersion).StartsWith($displayVersion + '.', [StringComparison]::Ordinal) -or
        -not ([string]$file.VersionInfo.ProductVersion).StartsWith($displayVersion + ' ', [StringComparison]::Ordinal)) {
        throw "PowerShell 7 executable metadata does not match package version $packageVersion."
    }
    $signature = Get-AuthenticodeSignature -LiteralPath $executable
    if ($signature.Status -ne [System.Management.Automation.SignatureStatus]::Valid -or
        $null -eq $signature.SignerCertificate -or
        $signature.SignerCertificate.Subject -notmatch '(^|,\s*)O=Microsoft Corporation(,|$)') {
        throw "PowerShell 7 executable signature is invalid: $($signature.Status)"
    }
    return [pscustomobject]@{
        DisplayVersion = $displayVersion
        Executable = $executable
    }
}

try {
    if (-not (Test-Path -LiteralPath $InputDirectory -PathType Container)) {
        throw "Sandbox input directory does not exist: $InputDirectory"
    }
    if (-not (Test-Path -LiteralPath $StatusDirectory -PathType Container)) {
        throw "Sandbox status directory does not exist: $StatusDirectory"
    }
    $env:HERDR_SANDBOX_STATUS_DIRECTORY = [IO.Path]::GetFullPath($StatusDirectory)

    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

    $releaseMetadataPath = Join-Path $InputDirectory 'bootstrap-release.json'
    $releaseMetadata = Get-Content -LiteralPath $releaseMetadataPath -Raw | ConvertFrom-Json
    $releaseMetadataProperties = @($releaseMetadata.PSObject.Properties.Name | Sort-Object)
    if (($releaseMetadataProperties -join '|') -cne 'openSSHMSISha256|openSSHMSIUrl|openSSHVersion|schemaVersion|vcRuntimeSha256|vcRuntimeUrl|wingetBundleSha256|wingetBundleUrl|wingetDependenciesSha256|wingetDependenciesUrl|wingetVersion' -or
        [int]$releaseMetadata.schemaVersion -ne 1) {
        throw 'Unsupported bootstrap release metadata schema.'
    }
    $VCRuntimeUrl = [string]$releaseMetadata.vcRuntimeUrl
    $VCRuntimeSha256 = [string]$releaseMetadata.vcRuntimeSha256
    $ExpectedWinGetVersion = [string]$releaseMetadata.wingetVersion
    $WinGetBundleUrl = [string]$releaseMetadata.wingetBundleUrl
    $WinGetBundleSha256 = [string]$releaseMetadata.wingetBundleSha256
    $WinGetDependenciesUrl = [string]$releaseMetadata.wingetDependenciesUrl
    $WinGetDependenciesSha256 = [string]$releaseMetadata.wingetDependenciesSha256
    $OpenSSHVersion = [string]$releaseMetadata.openSSHVersion
    $OpenSSHMSIUrl = [string]$releaseMetadata.openSSHMSIUrl
    $OpenSSHMSISha256 = [string]$releaseMetadata.openSSHMSISha256
    if (-not $VCRuntimeUrl.StartsWith('https://download.visualstudio.microsoft.com/download/pr/', [StringComparison]::Ordinal) -or
        -not $VCRuntimeUrl.EndsWith('/VC_redist.x64.exe', [StringComparison]::Ordinal)) {
        throw 'VC++ runtime URL is not an immutable Microsoft x64 redistributable URL.'
    }
    if ($VCRuntimeSha256 -notmatch '^[0-9a-f]{64}$') {
        throw 'VC++ runtime SHA-256 is malformed.'
    }
    $expectedWinGetUrlPrefix = 'https://github.com/microsoft/winget-cli/releases/download/' + $ExpectedWinGetVersion + '/'
    if (-not $WinGetBundleUrl.StartsWith($expectedWinGetUrlPrefix, [StringComparison]::Ordinal) -or
        -not $WinGetBundleUrl.EndsWith('/Microsoft.DesktopAppInstaller_8wekyb3d8bbwe.msixbundle', [StringComparison]::Ordinal)) {
        throw 'WinGet bundle URL does not match the pinned Microsoft release.'
    }
    if (-not $WinGetDependenciesUrl.StartsWith($expectedWinGetUrlPrefix, [StringComparison]::Ordinal) -or
        -not $WinGetDependenciesUrl.EndsWith('/DesktopAppInstaller_Dependencies.zip', [StringComparison]::Ordinal)) {
        throw 'WinGet dependency URL does not match the pinned Microsoft release.'
    }
    if ($WinGetBundleSha256 -notmatch '^[0-9a-f]{64}$' -or $WinGetDependenciesSha256 -notmatch '^[0-9a-f]{64}$') {
        throw 'WinGet release SHA-256 is malformed.'
    }
    $expectedOpenSSHUrlPrefix = 'https://github.com/PowerShell/Win32-OpenSSH/releases/download/' + $OpenSSHVersion + '/'
    if (-not $OpenSSHMSIUrl.StartsWith($expectedOpenSSHUrlPrefix, [StringComparison]::Ordinal) -or
        -not $OpenSSHMSIUrl.EndsWith('/OpenSSH-Win64-v10.0.0.0.msi', [StringComparison]::Ordinal)) {
        throw 'OpenSSH MSI URL does not match the pinned Microsoft release.'
    }
    if ($OpenSSHMSISha256 -notmatch '^[0-9a-f]{64}$') {
        throw 'OpenSSH MSI SHA-256 is malformed.'
    }

    $provisioningDirectory = Join-Path $InputDirectory 'provisioning'
    $baseProvisioning = Join-Path $provisioningDirectory 'base.ps1'
    $userProvisioning = Join-Path $provisioningDirectory 'user.ps1'
    $processOwner = Join-Path $provisioningDirectory 'provisioning-process.cs'
    $activeSessionLauncher = Join-Path $provisioningDirectory 'active-session-launch.ps1'
    $projectProvisioningDirectory = Join-Path $provisioningDirectory 'projects'
    $workspaceManifestPath = Join-Path $provisioningDirectory 'workspaces.json'
    $packagePlanPath = Join-Path $provisioningDirectory 'winget-packages.json'
    $toolVersionPlanPath = Join-Path $provisioningDirectory 'tool-versions.json'
    foreach ($requiredPath in @($baseProvisioning, $userProvisioning, $processOwner, $activeSessionLauncher, $projectProvisioningDirectory, $workspaceManifestPath, $packagePlanPath, $toolVersionPlanPath)) {
        if (-not (Test-Path -LiteralPath $requiredPath)) {
            throw "Development provisioning input is missing: $requiredPath"
        }
    }
    if ($null -eq ('HerdrSandbox.ProvisioningProcess' -as [type])) {
        Add-Type -Path $processOwner
    }
    if ([HerdrSandbox.ProvisioningProcess]::ContractVersion -ne 3) {
        throw 'Provisioning process owner contract is invalid.'
    }
    $workspaceManifest = [IO.File]::ReadAllText($workspaceManifestPath) | ConvertFrom-Json
    $manifestProperties = @($workspaceManifest.PSObject.Properties.Name | Sort-Object)
    if (($manifestProperties -join '|') -cne 'activeWorkspace|schemaVersion|workspaces' -or
        [int]$workspaceManifest.schemaVersion -ne 1) {
        throw 'Workspace manifest has an unsupported contract.'
    }
    $workspaceEntries = @($workspaceManifest.workspaces)
    if ($workspaceEntries.Count -eq 0 -or $workspaceEntries.Count -gt 16) {
        throw "Workspace manifest count is invalid: $($workspaceEntries.Count)"
    }
    $workspaceNames = @{}
    $activeWorkspace = [string]$workspaceManifest.activeWorkspace
    $activeWorkspaceMatches = 0
    foreach ($workspace in $workspaceEntries) {
        $entryProperties = @($workspace.PSObject.Properties.Name | Sort-Object)
        $workspaceName = [string]$workspace.name
        $workspaceDirectory = [string]$workspace.directory
        $expectedDirectory = Join-Path 'C:\Workspaces' $workspaceName
        if (($entryProperties -join '|') -cne 'directory|name' -or
            $workspaceName -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$' -or
            $workspaceDirectory -cne $expectedDirectory -or
            -not (Test-Path -LiteralPath $workspaceDirectory -PathType Container) -or
            $workspaceNames.ContainsKey($workspaceName.ToLowerInvariant())) {
            throw "Workspace manifest entry is invalid: $workspaceName"
        }
        $workspaceNames[$workspaceName.ToLowerInvariant()] = $true
        if ($workspaceDirectory -ceq $activeWorkspace) { $activeWorkspaceMatches += 1 }
    }
    $projectScriptNames = @(Get-ChildItem -LiteralPath $projectProvisioningDirectory -File -Filter '*.ps1' |
        ForEach-Object { $_.BaseName.ToLowerInvariant() } | Sort-Object)
    $unknownProjectScriptNames = @($projectScriptNames | Where-Object { -not $workspaceNames.ContainsKey($_) })
    if ($unknownProjectScriptNames.Count -ne 0 -or $activeWorkspaceMatches -ne 1) {
        throw 'Workspace manifest does not match the selected project scripts and active workspace.'
    }

    Write-ProgressStatus -Phase 'registry-customization' -Message 'Applying registry settings before package installation'
    & $baseProvisioning -Phase 'Registry' -ProjectProvisioningDirectory $projectProvisioningDirectory `
        -WorkspacesDirectory 'C:\Workspaces' -PackagePlanPath $packagePlanPath `
        -UserProvisioningPath $userProvisioning -ProcessOwnerPath $processOwner `
        -AudioOutputEnabled:($AudioPlayback -ceq 'Enabled') `
        -AudioInputEnabled:($AudioInput -ceq 'Enabled')

    $bootstrapCacheTrustRoot = 'C:\HerdrSandbox\cache'
    $bootstrapCacheRoot = Join-Path $bootstrapCacheTrustRoot 'bootstrap'
    Write-ProgressStatus -Phase 'winget-download' -Message 'Restoring the pinned Microsoft WinGet package'
    $wingetBundle = Join-Path $env:TEMP 'Microsoft.DesktopAppInstaller_8wekyb3d8bbwe.msixbundle'
    $wingetDependenciesArchive = Join-Path $env:TEMP 'DesktopAppInstaller_Dependencies.zip'
    $wingetBundle = Get-PinnedBootstrapAsset -Role 'WinGet bundle' -CacheKey 'winget-bundle' `
        -Uri $WinGetBundleUrl -ExpectedSHA256 $WinGetBundleSha256 `
        -FileName 'Microsoft.DesktopAppInstaller_8wekyb3d8bbwe.msixbundle' `
        -DestinationPath $wingetBundle -CacheRoot $bootstrapCacheRoot -CacheTrustRoot $bootstrapCacheTrustRoot
    $wingetDependenciesArchive = Get-PinnedBootstrapAsset -Role 'WinGet dependencies' `
        -CacheKey 'winget-dependencies' -Uri $WinGetDependenciesUrl `
        -ExpectedSHA256 $WinGetDependenciesSha256 -FileName 'DesktopAppInstaller_Dependencies.zip' `
        -DestinationPath $wingetDependenciesArchive -CacheRoot $bootstrapCacheRoot `
        -CacheTrustRoot $bootstrapCacheTrustRoot

    Write-ProgressStatus -Phase 'winget-install' -Message 'Installing the pinned WinGet package and dependencies'
    $wingetDependenciesDirectory = Join-Path $env:TEMP 'winget-dependencies'
    Expand-Archive -LiteralPath $wingetDependenciesArchive -DestinationPath $wingetDependenciesDirectory
    $wingetDependencyPaths = @(Get-ChildItem -LiteralPath (Join-Path $wingetDependenciesDirectory 'x64') -File -Filter '*.appx' |
        ForEach-Object { $_.FullName })
    if ($wingetDependencyPaths.Count -ne 3) {
        throw "Expected 3 x64 WinGet dependencies but found $($wingetDependencyPaths.Count)."
    }
    for ($attempt = 1; $attempt -le 4; $attempt += 1) {
        try {
            Add-AppxPackage -Path $wingetBundle -DependencyPath $wingetDependencyPaths -ErrorAction Stop
            break
        } catch {
            $diagnostic = [string]$_.Exception.Message
            $registrationNotReady =
                $diagnostic.IndexOf('0x80073CF3', [StringComparison]::OrdinalIgnoreCase) -ge 0 -and
                $diagnostic.IndexOf('0x80070003', [StringComparison]::OrdinalIgnoreCase) -ge 0 -and
                $diagnostic.IndexOf('AppxManifest.xml', [StringComparison]::OrdinalIgnoreCase) -ge 0
            if (-not $registrationNotReady -or $attempt -eq 4) {
                throw
            }
            $nextAttempt = $attempt + 1
            Write-ProgressStatus -Phase 'winget-install' -Message "WinGet package registration retry $nextAttempt of 4"
            Start-Sleep -Seconds (5 * $attempt)
        }
    }

    $wingetPath = $null
    for ($attempt = 0; $attempt -lt 60; $attempt += 1) {
        $command = Get-Command winget.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($null -ne $command -and (Test-Path -LiteralPath $command.Source -PathType Leaf)) {
            $wingetPath = $command.Source
            break
        }
        $appAlias = Join-Path $env:LOCALAPPDATA 'Microsoft\WindowsApps\winget.exe'
        if (Test-Path -LiteralPath $appAlias -PathType Leaf) {
            $wingetPath = $appAlias
            break
        }
        Start-Sleep -Seconds 1
    }
    if ([string]::IsNullOrWhiteSpace($wingetPath)) {
        throw 'WinGet was not found after Repair-WinGetPackageManager completed.'
    }
    $wingetVersionOutput = Invoke-Native -Role 'winget version check' -FilePath $wingetPath -ArgumentList @('--version')
    $wingetVersion = ($wingetVersionOutput -join ' ').Trim()
    if ($wingetVersion -ne $ExpectedWinGetVersion) {
        throw "WinGet version mismatch. Expected $ExpectedWinGetVersion but got $wingetVersion."
    }

    Write-ProgressStatus -Phase 'development-provisioning' `
        -Message 'Installing the pinned Microsoft VC++ runtime required by development tools and Herdr'
    $vcRuntimeInstaller = Join-Path $env:TEMP 'VC_redist.x64.exe'
    $vcRuntimeInstaller = Get-PinnedBootstrapAsset -Role 'VC++ runtime' -CacheKey 'vc-runtime' `
        -Uri $VCRuntimeUrl -ExpectedSHA256 $VCRuntimeSha256 -FileName 'VC_redist.x64.exe' `
        -DestinationPath $vcRuntimeInstaller -CacheRoot $bootstrapCacheRoot `
        -CacheTrustRoot $bootstrapCacheTrustRoot
    $vcRuntimeProcess = Start-Process -FilePath $vcRuntimeInstaller `
        -ArgumentList @('/install', '/quiet', '/norestart') -WindowStyle Hidden -Wait -PassThru
    if ($vcRuntimeProcess.ExitCode -notin @(0, 1638)) {
        throw "VC++ runtime installer exited with code $($vcRuntimeProcess.ExitCode)."
    }

    Write-ProgressStatus -Phase 'development-provisioning' -Message 'Applying global and project development provisioning'
    & $baseProvisioning -Phase 'Development' -ProjectProvisioningDirectory $projectProvisioningDirectory `
        -WorkspacesDirectory 'C:\Workspaces' -PackagePlanPath $packagePlanPath `
        -UserProvisioningPath $userProvisioning -ProcessOwnerPath $processOwner
    $powerShell7 = Get-PowerShell7Installation
    $powerShell7Executable = $powerShell7.Executable

    Write-ProgressStatus -Phase 'openssh-install' -Message 'Installing the pinned Microsoft OpenSSH Server'
    $openSSHInstaller = Join-Path $env:TEMP 'OpenSSH-Win64-v10.0.0.0.msi'
    $openSSHInstaller = Get-PinnedBootstrapAsset -Role 'OpenSSH MSI' -CacheKey 'openssh-msi' `
        -Uri $OpenSSHMSIUrl -ExpectedSHA256 $OpenSSHMSISha256 -FileName 'OpenSSH-Win64-v10.0.0.0.msi' `
        -DestinationPath $openSSHInstaller -CacheRoot $bootstrapCacheRoot `
        -CacheTrustRoot $bootstrapCacheTrustRoot
    $openSSHInstallProcess = Start-Process -FilePath 'msiexec.exe' `
        -ArgumentList @('/i', $openSSHInstaller, '/qn', '/norestart', 'ADDLOCAL=Server') `
        -WindowStyle Hidden -Wait -PassThru
    if ($openSSHInstallProcess.ExitCode -notin @(0, 1638)) {
        throw "OpenSSH MSI installer exited with code $($openSSHInstallProcess.ExitCode)."
    }
    $openSSHDirectory = Join-Path $env:ProgramFiles 'OpenSSH'
    foreach ($requiredFile in @((Join-Path $openSSHDirectory 'ssh-keygen.exe'), (Join-Path $openSSHDirectory 'sshd.exe'))) {
        if (-not (Test-Path -LiteralPath $requiredFile -PathType Leaf)) {
            throw "OpenSSH package is missing required file: $requiredFile"
        }
    }

    Write-ProgressStatus -Phase 'openssh-config' -Message 'Configuring OpenSSH keys, authentication, and PowerShell shell'
    $sshDirectory = Join-Path $env:ProgramData 'ssh'
    New-Item -ItemType Directory -Path $sshDirectory -Force | Out-Null
    $publicKeyPath = Join-Path $InputDirectory 'authorized_key.pub'
    $publicKey = (Get-Content -LiteralPath $publicKeyPath -Raw).Trim()
    if ($publicKey -notmatch '^ssh-ed25519\s+[A-Za-z0-9+/]+={0,2}(\s+[^\r\n]+)?$') {
        throw 'Expected exactly one Ed25519 public key in authorized_key.pub.'
    }

    $authorizedKeysPath = Join-Path $sshDirectory 'administrators_authorized_keys'
    [IO.File]::WriteAllText($authorizedKeysPath, $publicKey + "`r`n", $script:Utf8NoBom)
    & icacls.exe $authorizedKeysPath '/inheritance:r' '/grant' '*S-1-5-32-544:F' 'SYSTEM:F' | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw 'Could not secure administrators_authorized_keys.'
    }

    $sshdConfig = @'
Port 22
AddressFamily inet
ListenAddress 0.0.0.0
PubkeyAuthentication yes
PasswordAuthentication no
PermitEmptyPasswords no
AuthorizedKeysFile __PROGRAMDATA__/ssh/administrators_authorized_keys
'@
    $sshdConfigPath = Join-Path $sshDirectory 'sshd_config'
    [IO.File]::WriteAllText($sshdConfigPath, $sshdConfig, $script:Utf8NoBom)

    $sshKeygen = Join-Path $openSSHDirectory 'ssh-keygen.exe'
    $sshd = Join-Path $openSSHDirectory 'sshd.exe'
    Invoke-Native -Role 'SSH host-key generation' -FilePath $sshKeygen -ArgumentList @('-A') | Out-Null
    Invoke-Native -Role 'sshd configuration validation' -FilePath $sshd -ArgumentList @('-t', '-f', $sshdConfigPath) | Out-Null

    New-Item -Path 'HKLM:\SOFTWARE\OpenSSH' -Force | Out-Null
    New-ItemProperty -Path 'HKLM:\SOFTWARE\OpenSSH' -Name DefaultShell `
        -Value $powerShell7Executable `
        -PropertyType String -Force | Out-Null
    $configuredDefaultShell = [string](Get-ItemProperty -LiteralPath 'HKLM:\SOFTWARE\OpenSSH').DefaultShell
    if (-not [string]::Equals($configuredDefaultShell, $powerShell7Executable, [StringComparison]::OrdinalIgnoreCase)) {
        throw "OpenSSH default shell verification failed: $configuredDefaultShell"
    }

    Stop-Service -Name sshd -Force -ErrorAction SilentlyContinue
    Set-Service -Name sshd -StartupType Automatic
    Write-ProgressStatus -Phase 'openssh-start' -Message 'Starting OpenSSH Server and opening its firewall rule'
    Start-Service -Name sshd
    if (-not (Get-NetFirewallRule -Name 'OpenSSH-Server-In-TCP' -ErrorAction SilentlyContinue)) {
        New-NetFirewallRule -Name 'OpenSSH-Server-In-TCP' -DisplayName 'OpenSSH Server (sshd)' `
            -Enabled True -Direction Inbound -Protocol TCP -Action Allow -LocalPort 22 | Out-Null
    } else {
        Enable-NetFirewallRule -Name 'OpenSSH-Server-In-TCP'
    }
    if (-not (Get-NetTCPConnection -State Listen -LocalPort 22 -ErrorAction SilentlyContinue)) {
        throw 'sshd did not begin listening on TCP port 22.'
    }

    $hostKeyFields = ((Get-Content -LiteralPath (Join-Path $sshDirectory 'ssh_host_ed25519_key.pub') -Raw).Trim() -split '\s+')
    if ($hostKeyFields.Count -lt 2 -or $hostKeyFields[0] -ne 'ssh-ed25519') {
        throw 'OpenSSH did not produce an Ed25519 host key.'
    }
    $sshHostKey = $hostKeyFields[0] + ' ' + $hostKeyFields[1]

    $network = Get-NetIPConfiguration |
        Where-Object { $null -ne $_.IPv4DefaultGateway -and $null -ne $_.IPv4Address } |
        Select-Object -First 1
    if ($null -eq $network) {
        throw 'No Sandbox network adapter with an IPv4 default gateway was found.'
    }
    $ipAddress = ($network.IPv4Address | Select-Object -First 1).IPAddress
    if ([string]::IsNullOrWhiteSpace($ipAddress)) {
        throw 'Sandbox IPv4 address is empty.'
    }

    Write-AtomicJson -Path (Join-Path $StatusDirectory 'connectable.json') -Value ([ordered]@{
        schemaVersion = 1
        ip = $ipAddress
        sshUser = 'WDAGUtilityAccount'
        sshHostKey = $sshHostKey
        wingetVersion = $wingetVersion
    })
    Write-ProgressStatus -Phase 'configuration-handoff' `
        -Message 'Waiting for verified host configuration before workspace creation'
    $configurationHandoffPath = Join-Path $StatusDirectory 'configuration-handoff.json'
    $configurationDeadline = [DateTime]::UtcNow.AddMinutes($ConfigurationHandoffTimeoutMinutes)
    while (-not (Test-Path -LiteralPath $configurationHandoffPath -PathType Leaf)) {
        if ([DateTime]::UtcNow -ge $configurationDeadline) {
            throw "Verified host configuration did not arrive within $ConfigurationHandoffTimeoutMinutes minutes."
        }
        Start-Sleep -Milliseconds 250
    }
    $configurationHandoff = Read-ConfigurationHandoff -Path $configurationHandoffPath
    if ([string]$configurationHandoff.outcome -ceq 'failed') {
        throw "Host configuration phase '$($configurationHandoff.phase)' failed: $($configurationHandoff.message)"
    }

    $herdrExecutable = [Environment]::GetEnvironmentVariable('HERDR_SANDBOX_HERDR_EXE', 'Machine')
    if ([string]::IsNullOrWhiteSpace($herdrExecutable) -or -not [IO.Path]::IsPathRooted($herdrExecutable) -or
        -not (Test-Path -LiteralPath $herdrExecutable -PathType Leaf)) {
        throw 'Host provisioning did not publish the guest Herdr executable identity.'
    }
    $herdrClientStatusText = (Invoke-HerdrBoundary -Role 'Provisioned Herdr client status' -FilePath $herdrExecutable `
        -ArgumentList @('status', 'client', '--json')).Trim()
    if ([Text.Encoding]::UTF8.GetByteCount($herdrClientStatusText) -gt 65536 -or
        $herdrClientStatusText -notmatch '^\{"version":"(?:[^"\\]|\\.)+","herdr_version":"(?:[^"\\]|\\.)+","build_id":(?:null|"[0-9a-f]{12}\.[0-9a-f]{12}"),"protocol":[1-9][0-9]*,"binary":"(?:[^"\\]|\\.)+","session":(?:null|"(?:[^"\\]|\\.)+")\}$') {
        throw 'Provisioned guest Herdr client status is not canonical.'
    }
    $herdrClientStatus = $herdrClientStatusText | ConvertFrom-Json
    $herdrClientStatusProperties = @($herdrClientStatus.PSObject.Properties.Name)
    if (($herdrClientStatusProperties -join '|') -cne 'version|herdr_version|build_id|protocol|binary|session' -or
        $herdrClientStatus.version -isnot [string] -or [string]::IsNullOrWhiteSpace([string]$herdrClientStatus.version) -or
        $herdrClientStatus.herdr_version -isnot [string] -or [string]::IsNullOrWhiteSpace([string]$herdrClientStatus.herdr_version) -or
        ($null -ne $herdrClientStatus.build_id -and $herdrClientStatus.build_id -isnot [string]) -or
        $herdrClientStatus.protocol -isnot [int] -or [int]$herdrClientStatus.protocol -lt 1 -or
        $herdrClientStatus.binary -isnot [string] -or
        -not [string]::Equals([string]$herdrClientStatus.binary, $herdrExecutable, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Provisioned guest Herdr client identity is invalid.'
    }
    $herdrRuntimeVersion = [string]$herdrClientStatus.version
    $herdrProtocol = [int]$herdrClientStatus.protocol
    $herdrVersion = (Invoke-HerdrBoundary -Role 'Provisioned Herdr version' -FilePath $herdrExecutable `
        -ArgumentList @('--version')).Trim()
    if ([string]::IsNullOrWhiteSpace($herdrVersion) -or $herdrVersion.Length -gt 256 -or
        $herdrVersion.IndexOf("`r") -ge 0 -or $herdrVersion.IndexOf("`n") -ge 0) {
        throw 'Provisioned guest Herdr version identity is invalid.'
    }
    $herdrDirectory = Split-Path -Parent $herdrExecutable
    $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    $machinePathEntries = @($machinePath -split ';' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    if ($machinePathEntries.Count -eq 0 -or
        $machinePathEntries[0].TrimEnd('\') -ine $herdrDirectory.TrimEnd('\') -or
        @($machinePathEntries | Where-Object { $_.TrimEnd('\') -ieq $herdrDirectory.TrimEnd('\') }).Count -ne 1) {
        throw 'Provisioned guest Herdr directory is not the unique first machine PATH entry.'
    }
    $workspacePath = @($machinePath, [Environment]::GetEnvironmentVariable('Path', 'User')) -join ';'
    if ([string]::IsNullOrWhiteSpace($workspacePath) -or $workspacePath.Length -gt 32767) {
        throw 'Provisioned guest workspace PATH is invalid.'
    }

    Write-ProgressStatus -Phase 'herdr-workspace' -Message "Creating $($workspaceEntries.Count) mounted-project workspaces and terminal panes"
    $orderedWorkspaceEntries = @($workspaceEntries | Sort-Object `
        @{ Expression = { if ([string]$_.directory -ceq $activeWorkspace) { 1 } else { 0 } } }, `
        @{ Expression = { [string]$_.name } })
    $createdWorkspaceIds = @{}
    $createdRootPaneIds = @{}
    foreach ($workspace in $orderedWorkspaceEntries) {
        $workspaceName = [string]$workspace.name
        $workspaceDirectory = [string]$workspace.directory
        $workspaceArguments = @('workspace', 'create', '--cwd', $workspaceDirectory, '--label', $workspaceName,
            '--env', "PATH=$workspacePath", '--env', "HERDR_SANDBOX_HERDR_EXE=$herdrExecutable")
        if ($workspaceDirectory -ceq $activeWorkspace) { $workspaceArguments += '--focus' }
        $workspaceOutput = Invoke-HerdrBoundary -Role "Herdr workspace creation for $workspaceName" `
            -FilePath $herdrExecutable -ArgumentList $workspaceArguments
        $workspaceResponse = $workspaceOutput | ConvertFrom-Json
        $workspaceId = [string]$workspaceResponse.result.workspace.workspace_id
        $rootPaneId = [string]$workspaceResponse.result.root_pane.pane_id
        if ([string]::IsNullOrWhiteSpace($workspaceId) -or [string]::IsNullOrWhiteSpace($rootPaneId) -or
            $createdWorkspaceIds.ContainsKey($workspaceId) -or $createdRootPaneIds.ContainsKey($rootPaneId)) {
            throw "Herdr did not create a unique workspace and root pane for: $workspaceName"
        }
        $createdWorkspaceIds[$workspaceId] = $true
        $createdRootPaneIds[$rootPaneId] = $true
    }

    if ($null -ne $configurationHandoff.mobileAccess) {
        Write-ProgressStatus -Phase 'mobile-access' -Message 'Starting the private Tailscale mobile Herdr endpoint'
        $mobileScript = 'C:\HerdrSandbox\mobile-ssh\mobile-ssh.ps1'
        $mobileScriptItem = Get-Item -LiteralPath $mobileScript -Force -ErrorAction Stop
        if ($mobileScriptItem.PSIsContainer -or ($mobileScriptItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
            $mobileScriptItem.Length -le 0 -or $mobileScriptItem.Length -gt 65536) {
            throw 'Mobile SSH control script is unsafe or invalid.'
        }
        $activationOutput = @(& $mobileScript -Mode Activate)
        $activationText = ($activationOutput -join [Environment]::NewLine).Trim()
        try { $activation = $activationText | ConvertFrom-Json } catch { throw 'Mobile SSH activation returned invalid JSON.' }
        $activationProperties = @($activation.PSObject.Properties.Name)
        if (($activationProperties -join '|') -cne 'schemaVersion|state|pid' -or
            $activation.schemaVersion -isnot [int] -or [int]$activation.schemaVersion -ne 1 -or
            $activation.state -isnot [string] -or [string]$activation.state -cne 'running' -or
            $activation.pid -isnot [int] -or [int]$activation.pid -lt 1) {
            throw 'Mobile SSH activation result is invalid.'
        }
        Write-Host ''
        Write-Host 'Mobile Herdr access is ready over Tailscale.' -ForegroundColor Green
        Write-Host 'Scan this secret-free QR code with a mobile SSH app or camera:'
        foreach ($line in @($configurationHandoff.mobileAccess.qr)) {
            $rendered = ([string]$line).Replace('#', [string][char]0x2588)
            Write-Host $rendered -ForegroundColor Black -BackgroundColor White
        }
        Write-Host ("Connect: " + [string]$configurationHandoff.mobileAccess.uri)
        Write-Host ("Fallback: " + [string]$configurationHandoff.mobileAccess.sshUser + '@' + [string]$configurationHandoff.mobileAccess.ipv4 + ':2222')
        Write-Host ("Verify host key: " + [string]$configurationHandoff.mobileAccess.hostKeyFingerprint)
        Write-Host 'The device private key never leaves that device.'
        Write-Host ''
    }

    Write-AtomicJson -Path (Join-Path $StatusDirectory 'ready.json') -Value ([ordered]@{
        schemaVersion = 3
        ip = $ipAddress
        sshUser = 'WDAGUtilityAccount'
        sshHostKey = $sshHostKey
        wingetVersion = $wingetVersion
        herdrVersion = $herdrVersion
        herdrRuntimeVersion = $herdrRuntimeVersion
        herdrProtocol = $herdrProtocol
        herdrBinary = $herdrExecutable
    })
    Write-Host '[ready] Sandbox provisioning completed; this window may remain open.' -ForegroundColor Green
} catch {
    $message = Get-BoundedDiagnosticText -Text ([string]$_.Exception.Message) -MaximumBytes 4000
    $message = (($message -replace '[\x00-\x1F\x7F-\x9F]', ' ') -replace '\s+', ' ').Trim()
    if ([string]::IsNullOrWhiteSpace($message)) {
        $message = 'Provisioning failed without printable diagnostics.'
    }
    try {
        Write-AtomicJson -Path (Join-Path $StatusDirectory 'failed.json') -Value ([ordered]@{
            schemaVersion = 1
            phase = $script:Phase
            message = $message
        })
    } catch {
        # The host timeout remains the fallback if the status mapping itself failed.
    }
    exit 1
}
