# herdr-sandbox-stacks-contract: 3

function Get-StackWebResponseText {
    param(
        [Parameter(Mandatory = $true)]
        [object]$Response
    )

    if ($null -eq $Response.Content) {
        throw 'Unexpected empty web response content.'
    }
    if ($Response.Content -is [byte[]]) {
        return [Text.Encoding]::UTF8.GetString([byte[]]$Response.Content)
    }
    if ($Response.Content -is [string]) {
        return [string]$Response.Content
    }
    throw "Unexpected web response content type: $($Response.Content.GetType().FullName)"
}

function Get-StackVisualStudioTargetFromChannel {
    param(
        [Parameter(Mandatory = $true)]
        [object]$Channel,
        [Parameter(Mandatory = $true)]
        [string]$SourceDescription
    )

    $channel = $Channel
    if ([string]$channel.manifestVersion -cne '1.1' -or
        [string]$channel.info.manifestName -cne 'VisualStudio.17.Release' -or
        [string]$channel.info.manifestType -cne 'channel' -or
        [string]$channel.info.productLine -cne 'Dev17' -or
        [string]$channel.info.productLineVersion -cne '2022' -or
        [string]$channel.info.productMilestone -cne 'RTW' -or
        [string]$channel.info.productMilestoneIsPreRelease -cne 'False') {
        throw "Visual Studio channel metadata is unexpected: $SourceDescription"
    }
    $products = @($channel.channelItems | Where-Object {
        [string]$_.type -ceq 'ChannelProduct' -and
        [string]$_.id -ceq 'Microsoft.VisualStudio.Product.BuildTools'
    })
    $manifests = @($channel.channelItems | Where-Object {
        [string]$_.type -ceq 'Manifest' -and
        [string]$_.id -ceq 'Microsoft.VisualStudio.Manifests.VisualStudio'
    })
    $setups = @($channel.channelItems | Where-Object {
        [string]$_.type -ceq 'Bootstrapper' -and
        [string]$_.id -ceq 'VisualStudio.17.Release.Bootstrappers.Setup'
    })
    if ($products.Count -ne 1 -or $manifests.Count -ne 1 -or $setups.Count -ne 1) {
        throw "Visual Studio channel did not resolve one Build Tools product, manifest, and setup bootstrapper: $SourceDescription"
    }
    $catalogPayloads = @($manifests[0].payloads | Where-Object { [string]$_.fileName -ceq 'VisualStudio.vsman' })
    $setupPayloads = @($setups[0].payloads | Where-Object { [string]$_.fileName -ceq 'vs_Setup.exe' })
    if ($catalogPayloads.Count -ne 1 -or $setupPayloads.Count -ne 1) {
        throw "Visual Studio channel payload selection is ambiguous: $SourceDescription"
    }
    $buildVersion = [string]$channel.info.buildVersion
    $semanticVersion = [string]$channel.info.productSemanticVersion
    if ([string]::IsNullOrWhiteSpace($buildVersion) -or
        [string]::IsNullOrWhiteSpace($semanticVersion) -or
        [string]$products[0].version -cne $buildVersion -or
        [string]$manifests[0].version -cne $buildVersion) {
        throw "Visual Studio channel version fields disagree: $SourceDescription"
    }
    foreach ($payload in @($catalogPayloads[0], $setupPayloads[0])) {
        $uri = [Uri][string]$payload.url
        if ($uri.Scheme -cne 'https' -or $uri.Host -cne 'download.visualstudio.microsoft.com' -or
            [string]$payload.sha256 -notmatch '^[A-Fa-f0-9]{64}$') {
            throw "Visual Studio channel payload is unsafe in $SourceDescription`: $($payload.fileName)"
        }
    }
    return [pscustomobject]@{
        ChannelID = [string]$channel.info.id
        BuildVersion = $buildVersion
        SemanticVersion = $semanticVersion
        ProductVersion = [string]$products[0].version
        CatalogSHA256 = ([string]$catalogPayloads[0].sha256).ToUpperInvariant()
        SetupVersion = [string]$setups[0].version
        SetupSHA256 = ([string]$setupPayloads[0].sha256).ToUpperInvariant()
    }
}

function Get-StackVisualStudioCurrentTarget {
    $channelURI = 'https://aka.ms/vs/17/release/channel'
    $response = Invoke-WebRequest -Uri $channelURI -UseBasicParsing -ErrorAction Stop
    $channelText = Get-StackWebResponseText -Response $response
    $channel = $channelText | ConvertFrom-Json
    return Get-StackVisualStudioTargetFromChannel -Channel $channel -SourceDescription $channelURI
}

function Test-StackVisualStudioTargetEqual {
    param(
        [Parameter(Mandatory = $true)]
        [object]$Left,
        [Parameter(Mandatory = $true)]
        [object]$Right
    )

    return [string]$Left.ChannelID -ceq [string]$Right.ChannelID -and
        [string]$Left.BuildVersion -ceq [string]$Right.BuildVersion -and
        [string]$Left.SemanticVersion -ceq [string]$Right.SemanticVersion -and
        [string]$Left.ProductVersion -ceq [string]$Right.ProductVersion -and
        [string]$Left.CatalogSHA256 -ceq [string]$Right.CatalogSHA256 -and
        [string]$Left.SetupVersion -ceq [string]$Right.SetupVersion -and
        [string]$Left.SetupSHA256 -ceq [string]$Right.SetupSHA256
}

function Assert-StackVisualStudioBootstrapper {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        [Parameter(Mandatory = $true)]
        [string]$ExpectedSHA256
    )

    $actualHash = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToUpperInvariant()
    if ($actualHash -cne $ExpectedSHA256) {
        throw "Visual Studio bootstrapper hash mismatch: $actualHash"
    }
    $signature = Get-AuthenticodeSignature -LiteralPath $Path
    if ($signature.Status -ne [System.Management.Automation.SignatureStatus]::Valid -or
        $null -eq $signature.SignerCertificate) {
        throw "Visual Studio bootstrapper signature is invalid: $($signature.Status)"
    }
    $publisher = $signature.SignerCertificate.GetNameInfo(
        [Security.Cryptography.X509Certificates.X509NameType]::SimpleName, $false)
    if ($publisher -cne 'Microsoft Corporation' -or
        $signature.SignerCertificate.Subject -notmatch '(^|,\s*)O=Microsoft Corporation(,|$)') {
        throw "Unexpected Visual Studio bootstrapper publisher: $publisher"
    }
    $eku = @($signature.SignerCertificate.Extensions |
        Where-Object { $_ -is [Security.Cryptography.X509Certificates.X509EnhancedKeyUsageExtension] } |
        ForEach-Object { $_.EnhancedKeyUsages } | ForEach-Object { $_.Value })
    if ('1.3.6.1.5.5.7.3.3' -notin $eku) {
        throw 'Visual Studio bootstrapper certificate lacks the Code Signing EKU.'
    }
}

function Get-StackVisualStudioRequiredArtifacts {
    return @(
        'vs_BuildTools.exe',
        'layout.json',
        'response.json',
        'Catalog.json',
        'ChannelManifest.json',
        'vs_installer.opc',
        'vs_installer.version.json',
        'Certificates\manifestRootCertificate.cer',
        'Certificates\manifestCounterSignRootCertificate.cer',
        'Certificates\vs_installer_opc.RootCertificate.cer'
    )
}

function Get-StackVisualStudioComponentIDs {
    return @(
        'Microsoft.VisualStudio.Component.VC.Tools.x86.x64',
        'Microsoft.VisualStudio.Component.Windows11SDK.26100'
    )
}

function Assert-StackVisualStudioLayoutIdentity {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Layout,
        [Parameter(Mandatory = $true)]
        [object]$Target,
        [switch]$GuestLocal
    )

    if (-not (Test-Path -LiteralPath $Layout -PathType Container)) {
        throw "Visual Studio layout directory is missing: $Layout"
    }
    if (-not $GuestLocal) {
        Assert-ProvisioningCachePath -Path $Layout
    }
    $catalogPath = Join-Path $Layout 'Catalog.json'
    $channelManifestPath = Join-Path $Layout 'ChannelManifest.json'
    $layoutPath = Join-Path $Layout 'layout.json'
    foreach ($path in @($catalogPath, $channelManifestPath, $layoutPath)) {
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            throw "Visual Studio layout identity file is missing: $path"
        }
        if (-not $GuestLocal) {
            Assert-ProvisioningCachePath -Path $path
        }
    }

    $localChannel = [IO.File]::ReadAllText($channelManifestPath) | ConvertFrom-Json
    $localTarget = Get-StackVisualStudioTargetFromChannel -Channel $localChannel `
        -SourceDescription $channelManifestPath
    if (-not (Test-StackVisualStudioTargetEqual -Left $Target -Right $localTarget)) {
        throw 'Visual Studio layout channel identity does not match the resolved Current target.'
    }
    $catalog = [IO.File]::ReadAllText($catalogPath) | ConvertFrom-Json
    if ([string]$catalog.info.manifestName -cne 'VisualStudio' -or
        [string]$catalog.info.manifestType -cne 'installer' -or
        [string]::IsNullOrWhiteSpace([string]$catalog.engineVersion) -or
        [string]$catalog.info.buildVersion -cne $Target.BuildVersion -or
        [string]$catalog.info.productSemanticVersion -cne $Target.SemanticVersion -or
        [string]$catalog.info.productLine -cne 'Dev17' -or
        [string]$catalog.info.productLineVersion -cne '2022' -or
        [string]$catalog.info.productMilestone -cne 'RTW' -or
        [string]$catalog.info.productMilestoneIsPreRelease -cne 'False') {
        throw 'Visual Studio layout catalog identity is unexpected.'
    }
    $layoutText = [IO.File]::ReadAllText($layoutPath)
    $layoutConfig = $layoutText | ConvertFrom-Json
    $archProperty = $layoutConfig.PSObject.Properties['arch']
    $expectedComponents = @(Get-StackVisualStudioComponentIDs | Sort-Object)
    $actualComponents = @(@($layoutConfig.add) | ForEach-Object { [string]$_ } | Sort-Object)
    if ([string]$layoutConfig.channelId -cne 'VisualStudio.17.Release' -or
        [string]$layoutConfig.productId -cne 'Microsoft.VisualStudio.Product.BuildTools' -or
        ($null -ne $archProperty -and [string]$archProperty.Value -cne 'x64') -or
        ($actualComponents -join '|') -cne ($expectedComponents -join '|') -or
        $layoutText -match 'Microsoft\.VisualStudio\.Workload\.' -or
        $layoutText -match 'includeRecommended|includeOptional') {
        throw 'Visual Studio layout configuration identity is unexpected.'
    }
}

function Copy-StackVisualStudioLayoutToGuest {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Source,
        [Parameter(Mandatory = $true)]
        [string]$Destination
    )

    $copyStopwatch = [Diagnostics.Stopwatch]::StartNew()
    try {
        Assert-ProvisioningCachePath -Path $Source
        foreach ($item in @(Get-ChildItem -LiteralPath $Source -Recurse -Force)) {
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "Visual Studio layout contains a reparse point: $($item.FullName)"
            }
        }
        if (Test-Path -LiteralPath $Destination) {
            Remove-Item -LiteralPath $Destination -Recurse -Force
        }
        New-Item -ItemType Directory -Path $Destination | Out-Null
        foreach ($item in @(Get-ChildItem -LiteralPath $Source -Force)) {
            Copy-Item -LiteralPath $item.FullName -Destination $Destination -Recurse -Force
        }
        foreach ($relativePath in @(Get-StackVisualStudioRequiredArtifacts)) {
            $sourcePath = Join-Path $Source $relativePath
            $destinationPath = Join-Path $Destination $relativePath
            if (-not (Test-Path -LiteralPath $destinationPath -PathType Leaf)) {
                throw "Guest-local Visual Studio layout artifact is missing: $relativePath"
            }
            $sourceHash = (Get-FileHash -LiteralPath $sourcePath -Algorithm SHA256).Hash.ToUpperInvariant()
            $destinationHash = (Get-FileHash -LiteralPath $destinationPath -Algorithm SHA256).Hash.ToUpperInvariant()
            if ($sourceHash -cne $destinationHash) {
                throw "Guest-local Visual Studio layout artifact hash mismatch: $relativePath"
            }
        }
    } finally {
        $copyStopwatch.Stop()
        Write-ProvisioningTiming -Role 'Visual Studio layout guest materialization' `
            -Seconds $copyStopwatch.Elapsed.TotalSeconds
    }
}

function Test-StackVisualStudioLayoutSlot {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Slot,
        [Parameter(Mandatory = $true)]
        [object]$Target
    )

    try {
        if (-not (Test-Path -LiteralPath $Slot -PathType Container)) { return $false }
        Assert-ProvisioningCachePath -Path $Slot
        $layout = Join-Path $Slot 'layout'
        $descriptorPath = Join-Path $Slot 'complete.json'
        if (-not (Test-Path -LiteralPath $layout -PathType Container) -or
            -not (Test-Path -LiteralPath $descriptorPath -PathType Leaf)) { return $false }
        Assert-ProvisioningCachePath -Path $layout
        Assert-ProvisioningCachePath -Path $descriptorPath
        $descriptor = [IO.File]::ReadAllText($descriptorPath) | ConvertFrom-Json
        $expectedProperties = @('artifacts', 'bootstrapperSHA256', 'bootstrapperURL', 'buildVersion',
            'catalogSHA256', 'channelID', 'componentIDs', 'productID', 'productVersion',
            'schemaVersion', 'semanticVersion', 'setupSHA256', 'setupVersion')
        $actualProperties = @($descriptor.PSObject.Properties.Name | Sort-Object)
        $expectedComponents = @(Get-StackVisualStudioComponentIDs | Sort-Object)
        $actualComponents = @(@($descriptor.componentIDs) | ForEach-Object { [string]$_ } | Sort-Object)
        if (($actualProperties -join '|') -cne (($expectedProperties | Sort-Object) -join '|') -or
            [int]$descriptor.schemaVersion -ne 2 -or
            [string]$descriptor.channelID -cne $Target.ChannelID -or
            [string]$descriptor.buildVersion -cne $Target.BuildVersion -or
            [string]$descriptor.semanticVersion -cne $Target.SemanticVersion -or
            [string]$descriptor.productVersion -cne $Target.ProductVersion -or
            [string]$descriptor.catalogSHA256 -cne $Target.CatalogSHA256 -or
            [string]$descriptor.setupVersion -cne $Target.SetupVersion -or
            [string]$descriptor.setupSHA256 -cne $Target.SetupSHA256 -or
            [string]$descriptor.productID -cne 'Microsoft.VisualStudio.Product.BuildTools' -or
            ($actualComponents -join '|') -cne ($expectedComponents -join '|')) {
            return $false
        }
        $required = @(Get-StackVisualStudioRequiredArtifacts)
        $artifactProperties = @($descriptor.artifacts.PSObject.Properties)
        if ($artifactProperties.Count -ne $required.Count) { return $false }
        foreach ($relativePath in $required) {
            $path = Join-Path $layout $relativePath
            if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { return $false }
            Assert-ProvisioningCachePath -Path $path
            $expectedHashProperty = $descriptor.artifacts.PSObject.Properties[$relativePath]
            if ($null -eq $expectedHashProperty) { return $false }
            $actualHash = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToUpperInvariant()
            if ($actualHash -cne [string]$expectedHashProperty.Value) { return $false }
        }
        $bootstrapper = Join-Path $layout 'vs_BuildTools.exe'
        Assert-StackVisualStudioBootstrapper -Path $bootstrapper `
            -ExpectedSHA256 ([string]$descriptor.bootstrapperSHA256)
        Assert-StackVisualStudioLayoutIdentity -Layout $layout -Target $Target
        return $true
    } catch {
        return $false
    }
}

function Invoke-StackVisualStudioInstaller {
    param(
        [Parameter(Mandatory = $true)][string]$FilePath,
        [Parameter(Mandatory = $true)][string[]]$ArgumentList,
        [int]$TimeoutSeconds = 900
    )

    Write-ProvisioningProgress -Message 'Visual Studio Build Tools offline installation'
    $stopwatch = [Diagnostics.Stopwatch]::StartNew()
    $process = $null
    try {
        $process = Start-Process -FilePath $FilePath -ArgumentList $ArgumentList `
            -WindowStyle Hidden -PassThru
        if (-not $process.WaitForExit($TimeoutSeconds * 1000)) {
            Stop-Process -InputObject $process -Force -ErrorAction SilentlyContinue
            throw "Visual Studio Build Tools installer exceeded $TimeoutSeconds seconds."
        }
        $process.WaitForExit()
        if ($process.ExitCode -ne 0) {
            throw "Visual Studio Build Tools installer failed with exit code $($process.ExitCode)."
        }
    } finally {
        if ($null -ne $process) { $process.Dispose() }
        $stopwatch.Stop()
        Write-ProvisioningTiming -Role 'Visual Studio Build Tools offline installation' `
            -Seconds $stopwatch.Elapsed.TotalSeconds
    }
}

function Wait-StackVisualStudioInstalled {
    param([int]$TimeoutSeconds = 120)

    $vswhere = [string](Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\vswhere.exe')
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        if (Test-Path -LiteralPath $vswhere -PathType Leaf) {
            $previousErrorActionPreference = $ErrorActionPreference
            try {
                $ErrorActionPreference = 'Continue'
                $installationPath = @(& $vswhere '-latest' '-products' '*' '-requires' `
                    'Microsoft.VisualStudio.Component.VC.Tools.x86.x64' `
                    'Microsoft.VisualStudio.Component.Windows11SDK.26100' '-property' 'installationPath' 2>&1)
                $exitCode = $LASTEXITCODE
            } finally {
                $ErrorActionPreference = $previousErrorActionPreference
            }
            if ($exitCode -eq 0 -and ($installationPath -join ' ').Trim() -ieq 'C:\HerdrSandbox\toolchains\visual-studio') {
                return
            }
        }
        Start-Sleep -Seconds 2
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "Visual Studio C++ workload did not become ready within $TimeoutSeconds seconds."
}

function Test-StackFirewallValue {
    param(
        [AllowNull()]
        [object]$Value,
        [Parameter(Mandatory = $true)]
        [AllowEmptyString()]
        [string]$Expected
    )

    $values = @($Value)
    return $values.Count -eq 1 -and [string]$values[0] -ceq $Expected
}

function Test-StackVisualStudioFirewallRule {
    param(
        [Parameter(Mandatory = $true)]
        [AllowEmptyCollection()]
        [object[]]$Rules,
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [ValidateSet('Inbound', 'Outbound')]
        [string]$Direction,
        [Parameter(Mandatory = $true)]
        [string]$Program
    )

    if ($Rules.Count -ne 1) { return $false }
    $candidate = $Rules[0]
    try {
        $applicationFilters = @($candidate | Get-NetFirewallApplicationFilter -ErrorAction Stop)
        $addressFilters = @($candidate | Get-NetFirewallAddressFilter -ErrorAction Stop)
        $portFilters = @($candidate | Get-NetFirewallPortFilter -ErrorAction Stop)
        $serviceFilters = @($candidate | Get-NetFirewallServiceFilter -ErrorAction Stop)
        $interfaceFilters = @($candidate | Get-NetFirewallInterfaceFilter -ErrorAction Stop)
        $interfaceTypeFilters = @($candidate | Get-NetFirewallInterfaceTypeFilter -ErrorAction Stop)
        $securityFilters = @($candidate | Get-NetFirewallSecurityFilter -ErrorAction Stop)
        if ($applicationFilters.Count -ne 1 -or $addressFilters.Count -ne 1 -or
            $portFilters.Count -ne 1 -or $serviceFilters.Count -ne 1 -or
            $interfaceFilters.Count -ne 1 -or $interfaceTypeFilters.Count -ne 1 -or
            $securityFilters.Count -ne 1) {
            return $false
        }
        $actualProgram = [IO.Path]::GetFullPath([string]$applicationFilters[0].Program)
        $expectedProgram = [IO.Path]::GetFullPath($Program)
    } catch {
        return $false
    }
    return [string]$candidate.Name -ceq $Name -and
        [string]$candidate.DisplayName -ceq $Name -and
        [string]$candidate.Enabled -ceq 'True' -and
        [string]$candidate.Profile -ceq 'Any' -and
        [string]$candidate.Direction -ceq $Direction -and
        [string]$candidate.Action -ceq 'Block' -and
        ($Direction -cne 'Inbound' -or [string]$candidate.EdgeTraversalPolicy -ceq 'Block') -and
        [string]$candidate.LooseSourceMapping -ceq 'False' -and
        [string]$candidate.LocalOnlyMapping -ceq 'False' -and
        [string]::IsNullOrEmpty([string]$candidate.Owner) -and
        [string]::Equals($actualProgram, $expectedProgram, [StringComparison]::OrdinalIgnoreCase) -and
        ([string]::IsNullOrEmpty([string]$applicationFilters[0].Package) -or
            [string]$applicationFilters[0].Package -ceq 'Any') -and
        (Test-StackFirewallValue -Value $addressFilters[0].LocalAddress -Expected 'Any') -and
        (Test-StackFirewallValue -Value $addressFilters[0].RemoteAddress -Expected 'Any') -and
        [string]$portFilters[0].Protocol -ceq 'Any' -and
        (Test-StackFirewallValue -Value $portFilters[0].LocalPort -Expected 'Any') -and
        (Test-StackFirewallValue -Value $portFilters[0].RemotePort -Expected 'Any') -and
        (Test-StackFirewallValue -Value $portFilters[0].IcmpType -Expected 'Any') -and
        ([string]::IsNullOrEmpty([string]$portFilters[0].DynamicTarget) -or
            [string]$portFilters[0].DynamicTarget -ceq 'Any') -and
        [string]$serviceFilters[0].Service -ceq 'Any' -and
        (Test-StackFirewallValue -Value $interfaceFilters[0].InterfaceAlias -Expected 'Any') -and
        [string]$interfaceTypeFilters[0].InterfaceType -ceq 'Any' -and
        [string]$securityFilters[0].Authentication -ceq 'NotRequired' -and
        [string]$securityFilters[0].Encryption -ceq 'NotRequired' -and
        [string]$securityFilters[0].OverrideBlockRules -ceq 'False' -and
        [string]$securityFilters[0].LocalUser -ceq 'Any' -and
        [string]$securityFilters[0].RemoteUser -ceq 'Any' -and
        [string]$securityFilters[0].RemoteMachine -ceq 'Any'
}

function Set-StackVisualStudioFirewallRule {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [ValidateSet('Inbound', 'Outbound')]
        [string]$Direction,
        [Parameter(Mandatory = $true)]
        [string]$Program
    )

    $existing = @(Get-NetFirewallRule -Name $Name -ErrorAction SilentlyContinue)
    if (Test-StackVisualStudioFirewallRule -Rules $existing -Name $Name `
            -Direction $Direction -Program $Program) {
        return
    }
    if ($existing.Count -gt 0) {
        $existing | Remove-NetFirewallRule -ErrorAction Stop
    }
    $parameters = @{
        Name = $Name; DisplayName = $Name; Enabled = 'True'; Profile = 'Any'
        Direction = $Direction; Action = 'Block'; LooseSourceMapping = $false; LocalOnlyMapping = $false
        Program = $Program; LocalAddress = 'Any'; RemoteAddress = 'Any'; Protocol = 'Any'
        LocalPort = 'Any'; RemotePort = 'Any'; Service = 'Any'; InterfaceType = 'Any'
        Authentication = 'NotRequired'; Encryption = 'NotRequired'
    }
    if ($Direction -ceq 'Inbound') { $parameters.EdgeTraversalPolicy = 'Block' }
    New-NetFirewallRule @parameters | Out-Null
    $verified = @(Get-NetFirewallRule -Name $Name -ErrorAction Stop)
    if (-not (Test-StackVisualStudioFirewallRule -Rules $verified -Name $Name `
                -Direction $Direction -Program $Program)) {
        throw "Visual Studio firewall rule verification failed: $Name"
    }
}

function Install-StackVisualStudioBuildTools {
    $visualStudioStopwatch = [Diagnostics.Stopwatch]::StartNew()
    $cacheRoot = 'C:\HerdrSandbox\cache\vsbt'
    $guestLayout = 'C:\HerdrSandbox\visual-studio\layout'
    if (-not (Test-Path -LiteralPath $cacheRoot -PathType Container)) {
        throw 'The host-prepared Visual Studio Build Tools cache is missing.'
    }
    Assert-ProvisioningCachePath -Path $cacheRoot
    $lockPath = Join-Path $cacheRoot '.lock'
    Assert-ProvisioningCachePath -Path $lockPath
    $lock = $null
    $primaryFailure = $null
    $cleanupFailure = $null
    try {
        $lock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate,
            [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
        $target = Get-StackVisualStudioCurrentTarget
        $slotA = Join-Path $cacheRoot 'a'
        $slotB = Join-Path $cacheRoot 'b'
        $matchingSlots = @(@($slotA, $slotB) | Where-Object {
            Test-StackVisualStudioLayoutSlot -Slot $_ -Target $target
        })
        if ($matchingSlots.Count -ne 1) {
            throw "Expected one host-prepared Visual Studio Current layout, found $($matchingSlots.Count)."
        }
        $selectedSlot = $matchingSlots[0]
        Write-Output "Visual Studio Build Tools host layout cache hit: $($target.BuildVersion)"
        $layout = Join-Path $selectedSlot 'layout'
        $descriptor = [IO.File]::ReadAllText((Join-Path $selectedSlot 'complete.json')) | ConvertFrom-Json
        $expectedBootstrapperHash = [string]$descriptor.bootstrapperSHA256
        Write-ProvisioningProgress -Message 'Visual Studio Build Tools guest-local layout materialization'
        Copy-StackVisualStudioLayoutToGuest -Source $layout -Destination $guestLayout
        $guestLayoutBootstrapper = Join-Path $guestLayout 'vs_BuildTools.exe'
        Assert-StackVisualStudioBootstrapper -Path $guestLayoutBootstrapper `
            -ExpectedSHA256 $expectedBootstrapperHash
        Assert-StackVisualStudioLayoutIdentity -Layout $guestLayout -Target $target -GuestLocal
        $channelManifest = Join-Path $guestLayout 'ChannelManifest.json'
        $catalog = Join-Path $guestLayout 'Catalog.json'
        $installationArguments = @('--noWeb', '--noUpdateInstaller', '--wait', '--quiet', '--norestart',
            '--installPath', 'C:\HerdrSandbox\toolchains\visual-studio', '--channelId', 'VisualStudio.17.Release',
            '--productId', 'Microsoft.VisualStudio.Product.BuildTools', '--channelUri', $channelManifest,
            '--installChannelUri', $channelManifest, '--installCatalogUri', $catalog)
        foreach ($componentID in @(Get-StackVisualStudioComponentIDs)) {
            $installationArguments += @('--add', $componentID)
        }
        $installationArguments += @('--addProductLang', 'en-US')
        $installerEngine = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\setup.exe'
        foreach ($rule in @(
            @{ Name = 'HerdrSandbox-VSBootstrapper-In'; Direction = 'Inbound'; Program = $guestLayoutBootstrapper },
            @{ Name = 'HerdrSandbox-VSBootstrapper-Out'; Direction = 'Outbound'; Program = $guestLayoutBootstrapper },
            @{ Name = 'HerdrSandbox-VSInstaller-In'; Direction = 'Inbound'; Program = $installerEngine },
            @{ Name = 'HerdrSandbox-VSInstaller-Out'; Direction = 'Outbound'; Program = $installerEngine }
        )) {
            Set-StackVisualStudioFirewallRule -Name $rule.Name -Direction $rule.Direction `
                -Program $rule.Program
        }
        Invoke-StackVisualStudioInstaller -FilePath $guestLayoutBootstrapper -ArgumentList $installationArguments
        Wait-StackVisualStudioInstalled
        foreach ($slot in @($slotA, $slotB)) {
            if ($slot -ine $selectedSlot -and (Test-Path -LiteralPath $slot)) {
                Assert-ProvisioningCacheTree -Path $slot
                Remove-Item -LiteralPath $slot -Recurse -Force
            }
        }
    } catch {
        $primaryFailure = $_
    } finally {
        if ($null -ne $lock) {
            try { $lock.Dispose() } catch { $cleanupFailure = $_ }
        }
        $visualStudioStopwatch.Stop()
        Write-ProvisioningTiming -Role 'Visual Studio Build Tools total' `
            -Seconds $visualStudioStopwatch.Elapsed.TotalSeconds
    }
    if ($null -ne $primaryFailure) {
        if ($null -ne $cleanupFailure) {
            Write-Warning "Visual Studio cache cleanup also failed: $($cleanupFailure.Exception.Message)"
        }
        throw $primaryFailure
    }
    if ($null -ne $cleanupFailure) { throw $cleanupFailure }
}

function Get-StackRustSHA256 {
    param([Parameter(Mandatory = $true)][byte[]]$Bytes)

    $sha256 = [Security.Cryptography.SHA256]::Create()
    try {
        return [BitConverter]::ToString($sha256.ComputeHash($Bytes)).Replace('-', '').ToUpperInvariant()
    } finally {
        $sha256.Dispose()
    }
}

function Invoke-StackRustMetadataDownload {
    param(
        [Parameter(Mandatory = $true)][string]$Uri,
        [Parameter(Mandatory = $true)][ValidateRange(1, 4194304)][int]$MaximumBytes
    )

    $parsed = [Uri]$Uri
    if (-not $parsed.IsAbsoluteUri -or $parsed.Scheme -cne 'https' -or
        $parsed.Host -cne 'static.rust-lang.org' -or $parsed.Port -ne 443 -or
        -not [string]::IsNullOrEmpty($parsed.UserInfo) -or
        -not [string]::IsNullOrEmpty($parsed.Query) -or
        -not [string]::IsNullOrEmpty($parsed.Fragment) -or $parsed.AbsoluteUri -cne $Uri) {
        throw "Rust metadata URL is not canonical: $Uri"
    }
    $response = Invoke-WebRequest -Uri $Uri -UseBasicParsing -ErrorAction Stop
    if ([int]$response.StatusCode -ne 200 -or $response.BaseResponse.ResponseUri.AbsoluteUri -cne $Uri -or
        $response.Content -isnot [byte[]]) {
        throw "Rust metadata response is unexpected: $Uri"
    }
    $bytes = [byte[]]$response.Content
    if ($bytes.Length -le 0 -or $bytes.Length -gt $MaximumBytes) {
        throw "Rust metadata response size is invalid: $Uri"
    }
    return ,$bytes
}

function ConvertFrom-StackRustManifest {
    param(
        [Parameter(Mandatory = $true)][byte[]]$ManifestBytes,
        [Parameter(Mandatory = $true)][string]$ExpectedChannel,
        [Parameter(Mandatory = $true)][string]$Target
    )

    $versionPattern = '(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)'
    if (($ExpectedChannel -cne 'stable' -and $ExpectedChannel -notmatch ('^' + $versionPattern + '$')) -or
        $Target -cne 'x86_64-pc-windows-msvc') {
        throw "Rust channel or target is unsupported: $ExpectedChannel $Target"
    }
    $utf8 = New-Object Text.UTF8Encoding($false, $true)
    try { $text = $utf8.GetString($ManifestBytes) } catch { throw 'Rust manifest is not strict UTF-8.' }
    if ($text.IndexOf([char]0) -ge 0 -or $text.Contains("'''") -or $text.Contains('"""')) {
        throw 'Rust manifest uses an unsupported text shape.'
    }
    $header = [regex]::Match($text, '\Amanifest-version = "2"\r?\ndate = "(?<date>[0-9]{4}-[0-9]{2}-[0-9]{2})"\r?\n')
    if (-not $header.Success) { throw 'Rust manifest header is unexpected.' }
    $date = $header.Groups['date'].Value
    $parsedDate = [DateTime]::MinValue
    if (-not [DateTime]::TryParseExact($date, 'yyyy-MM-dd', [Globalization.CultureInfo]::InvariantCulture,
            [Globalization.DateTimeStyles]::None, [ref]$parsedDate)) {
        throw "Rust manifest date is invalid: $date"
    }
    $rustTable = [regex]::Match($text, '(?ms)^\[pkg\.rust\]\r?\n(?<body>.*?)(?=^\[|\z)')
    if (-not $rustTable.Success) { throw 'Rust manifest is missing [pkg.rust].' }
    $versionMatches = @([regex]::Matches($rustTable.Groups['body'].Value,
        ('(?m)^version = "(?<version>' + $versionPattern + ') \([0-9a-f]{9,40} [0-9]{4}-[0-9]{2}-[0-9]{2}\)"\r?$')))
    if ($versionMatches.Count -ne 1) { throw 'Rust manifest version identity is unexpected.' }
    $version = $versionMatches[0].Groups['version'].Value
    if ($ExpectedChannel -cne 'stable' -and $version -cne $ExpectedChannel) {
        throw "Rust manifest resolved $version instead of $ExpectedChannel."
    }

    $payloads = @()
    foreach ($package in @('cargo', 'clippy-preview', 'rust-std', 'rustc', 'rustfmt-preview')) {
        $tableName = "pkg.$package.target.$Target"
        $table = [regex]::Match($text, ('(?ms)^\[' + [regex]::Escape($tableName) + '\]\r?\n(?<body>.*?)(?=^\[|\z)'))
        if (-not $table.Success) { throw "Rust manifest is missing [$tableName]." }
        $fields = @{}
        foreach ($line in @([regex]::Split($table.Groups['body'].Value.Trim(), '\r?\n'))) {
            $match = [regex]::Match($line, '^(?<name>available|url|hash|xz_url|xz_hash|zst_url|zst_hash) = (?:(?<bool>true|false)|"(?<text>[^"\r\n]+)")$')
            if (-not $match.Success -or $fields.ContainsKey($match.Groups['name'].Value)) {
                throw "Rust manifest field is unsupported in [$tableName]: $line"
            }
            $fields[$match.Groups['name'].Value] = if ($match.Groups['bool'].Success) { $match.Groups['bool'].Value } else { $match.Groups['text'].Value }
        }
        if (-not $fields.ContainsKey('available') -or $fields['available'] -cne 'true') {
            throw "Rust package $package is unavailable for $Target."
        }
        $stem = if ($package -ceq 'clippy-preview') { 'clippy' } elseif ($package -ceq 'rustfmt-preview') { 'rustfmt' } else { $package }
        $selected = $null
        foreach ($format in @(
            [pscustomobject]@{ Url = 'zst_url'; Hash = 'zst_hash'; Suffix = 'tar.zst' },
            [pscustomobject]@{ Url = 'xz_url'; Hash = 'xz_hash'; Suffix = 'tar.xz' },
            [pscustomobject]@{ Url = 'url'; Hash = 'hash'; Suffix = 'tar.gz' }
        )) {
            $hasURL = $fields.ContainsKey($format.Url)
            if ($hasURL -ne $fields.ContainsKey($format.Hash)) { throw "Rust $package has incomplete $($format.Suffix) metadata." }
            if ($null -eq $selected -and $hasURL) {
                $fileName = "$stem-$version-$Target.$($format.Suffix)"
                $expectedURL = "https://static.rust-lang.org/dist/$date/$fileName"
                $hash = [string]$fields[$format.Hash]
                if ([string]$fields[$format.Url] -cne $expectedURL -or $hash -notmatch '^[0-9a-f]{64}$') {
                    throw "Rust $package has unsafe $($format.Suffix) metadata."
                }
                $selected = [pscustomobject]@{ RelativePath = "dist\$date\$fileName"; Url = $expectedURL; Sha256 = $hash.ToUpperInvariant() }
            }
        }
        if ($null -eq $selected) { throw "Rust package $package has no supported payload." }
        $payloads += $selected
    }
    return [pscustomobject]@{ Version = $version; Date = $date; Target = $Target; Payloads = @($payloads) }
}

function Get-StackRustManifestSnapshot {
    param(
        [Parameter(Mandatory = $true)][string]$Channel,
        [Parameter(Mandatory = $true)][string]$Target
    )

    $manifestName = "channel-rust-$Channel.toml"
    $manifestURL = "https://static.rust-lang.org/dist/$manifestName"
    $sidecarURL = "$manifestURL.sha256"
    $sidecarBytes = Invoke-StackRustMetadataDownload -Uri $sidecarURL -MaximumBytes 256
    $utf8 = New-Object Text.UTF8Encoding($false, $true)
    try { $sidecar = $utf8.GetString($sidecarBytes) } catch { throw 'Rust manifest sidecar is not strict UTF-8.' }
    $sidecarMatch = [regex]::Match($sidecar, ('\A(?<hash>[0-9a-f]{64})  ' + [regex]::Escape($manifestName) + '\r?\n\z'))
    if (-not $sidecarMatch.Success) { throw "Rust manifest sidecar is invalid: $sidecarURL" }
    $manifestBytes = Invoke-StackRustMetadataDownload -Uri $manifestURL -MaximumBytes 4194304
    $manifestHash = Get-StackRustSHA256 -Bytes $manifestBytes
    if ($manifestHash -cne $sidecarMatch.Groups['hash'].Value.ToUpperInvariant()) {
        throw "Rust manifest hash mismatch: $manifestURL"
    }
    $selection = ConvertFrom-StackRustManifest -ManifestBytes $manifestBytes -ExpectedChannel $Channel -Target $Target
    return [pscustomobject]@{
        Version = $selection.Version; Date = $selection.Date; Target = $selection.Target
        ManifestURL = $manifestURL; ManifestBytes = [byte[]]$manifestBytes; ManifestSha256 = $manifestHash
        SidecarURL = $sidecarURL; SidecarBytes = [byte[]]$sidecarBytes; SidecarSha256 = Get-StackRustSHA256 -Bytes $sidecarBytes
        ComponentPayloads = @($selection.Payloads)
    }
}

function Resolve-StackRustDistribution {
    param(
        [Parameter(Mandatory = $true)][string]$RequestedChannel,
        [string]$Target = 'x86_64-pc-windows-msvc'
    )

    if ($RequestedChannel -ceq 'stable') {
        $stable = Get-StackRustManifestSnapshot -Channel 'stable' -Target $Target
        $concrete = Get-StackRustManifestSnapshot -Channel $stable.Version -Target $Target
        $stablePayloads = @($stable.ComponentPayloads | ForEach-Object { "$($_.RelativePath)|$($_.Sha256)" }) -join "`n"
        $concretePayloads = @($concrete.ComponentPayloads | ForEach-Object { "$($_.RelativePath)|$($_.Sha256)" }) -join "`n"
        if ($stable.Version -cne $concrete.Version -or $stable.Date -cne $concrete.Date -or $stablePayloads -cne $concretePayloads) {
            throw "Rust stable and concrete manifests disagree for $($stable.Version)."
        }
    } else {
        $concrete = Get-StackRustManifestSnapshot -Channel $RequestedChannel -Target $Target
    }
    $manifestPath = "dist\channel-rust-$($concrete.Version).toml"
    $sidecarPath = "$manifestPath.sha256"
    $payloads = @(
        [pscustomobject]@{ RelativePath = $manifestPath; Url = $concrete.ManifestURL; Sha256 = $concrete.ManifestSha256 },
        [pscustomobject]@{ RelativePath = $sidecarPath; Url = $concrete.SidecarURL; Sha256 = $concrete.SidecarSha256 }
    ) + @($concrete.ComponentPayloads)
    return [pscustomobject]@{
        Toolchain = $concrete.Version; Target = $Target; Payloads = @($payloads)
        ManifestRelativePath = $manifestPath; ManifestBytes = [byte[]]$concrete.ManifestBytes
        SidecarRelativePath = $sidecarPath; SidecarBytes = [byte[]]$concrete.SidecarBytes
        Metadata = [pscustomobject][ordered]@{ schemaVersion = 1; toolchain = $concrete.Version; target = $Target; manifestSha256 = $concrete.ManifestSha256 }
        CacheEntryName = "$($concrete.Version)-$Target-$($concrete.ManifestSha256.ToLowerInvariant())"
    }
}

function Assert-StackRustMirrorPayloads {
    param(
        [Parameter(Mandatory = $true)]
        [string]$MirrorRoot,
        [Parameter(Mandatory = $true)]
        [object[]]$Payloads,
        [Parameter(Mandatory = $true)]
        [object]$Metadata
    )

    $metadataProperties = @($Metadata.PSObject.Properties.Name | Sort-Object)
    if (($metadataProperties -join '|') -cne 'manifestSha256|schemaVersion|target|toolchain' -or
        [int]$Metadata.schemaVersion -ne 1 -or
        [string]$Metadata.toolchain -notmatch '^\d+\.\d+\.\d+$' -or
        [string]$Metadata.target -cne 'x86_64-pc-windows-msvc' -or
        [string]$Metadata.manifestSha256 -cnotmatch '^[A-F0-9]{64}$' -or $Payloads.Count -ne 7) {
        throw 'Rust mirror metadata is unexpected.'
    }
    if (-not (Test-Path -LiteralPath $MirrorRoot -PathType Container)) {
        throw "Rust mirror directory is missing: $MirrorRoot"
    }
    $mirrorInfo = Get-Item -LiteralPath $MirrorRoot -Force
    if (($mirrorInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Rust mirror root is a reparse point: $MirrorRoot"
    }
    foreach ($directory in @(Get-ChildItem -LiteralPath $MirrorRoot -Directory -Recurse -Force)) {
        if (($directory.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Rust mirror directory is a reparse point: $($directory.FullName)"
        }
    }
    $files = @(Get-ChildItem -LiteralPath $MirrorRoot -File -Recurse -Force)
    if ($files.Count -ne $Payloads.Count) {
        throw "Rust mirror contains $($files.Count) files; expected $($Payloads.Count)."
    }
    $seenPaths = @{}
    foreach ($payload in $Payloads) {
        $relativePath = [string]$payload.RelativePath
        if ([string]::IsNullOrWhiteSpace($relativePath) -or [IO.Path]::IsPathRooted($relativePath) -or
            $relativePath.Contains('/') -or -not $relativePath.StartsWith('dist\', [StringComparison]::Ordinal) -or
            @($relativePath -split '\\' | Where-Object { $_ -ceq '.' -or $_ -ceq '..' }).Count -ne 0 -or
            [string]$payload.Sha256 -cnotmatch '^[A-F0-9]{64}$' -or $seenPaths.ContainsKey($relativePath)) {
            throw "Rust mirror payload metadata is unsafe: $relativePath"
        }
        $seenPaths[$relativePath] = $true
        $path = Join-Path $MirrorRoot $payload.RelativePath
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            throw "Rust mirror payload is missing: $($payload.RelativePath)"
        }
        $info = Get-Item -LiteralPath $path -Force
        if (($info.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Rust mirror payload is a reparse point: $($payload.RelativePath)"
        }
        $actualHash = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToUpperInvariant()
        if ($actualHash -cne $payload.Sha256) {
            throw "Rust mirror payload hash mismatch: $($payload.RelativePath)"
        }
    }
    $manifestName = "channel-rust-$($Metadata.toolchain).toml"
    $manifestPath = "dist\$manifestName"
    $sidecarPath = "$manifestPath.sha256"
    $manifestPayload = @($Payloads | Where-Object { [string]$_.RelativePath -ceq $manifestPath })
    $sidecarPayload = @($Payloads | Where-Object { [string]$_.RelativePath -ceq $sidecarPath })
    if ($manifestPayload.Count -ne 1 -or $sidecarPayload.Count -ne 1 -or
        [string]$manifestPayload[0].Sha256 -cne [string]$Metadata.manifestSha256) {
        throw 'Rust mirror manifest payload metadata is unexpected.'
    }
    $sidecar = [IO.File]::ReadAllText((Join-Path $MirrorRoot $sidecarPath))
    $expectedSidecar = ([string]$Metadata.manifestSha256).ToLowerInvariant() + "  $manifestName"
    if ($sidecar -cne ($expectedSidecar + "`n") -and $sidecar -cne ($expectedSidecar + "`r`n")) {
        throw 'Rust channel manifest sidecar content is unexpected.'
    }
}

function Test-StackRustMirrorCacheEntry {
    param(
        [Parameter(Mandatory = $true)]
        [string]$EntryDirectory,
        [Parameter(Mandatory = $true)]
        [object[]]$Payloads,
        [Parameter(Mandatory = $true)]
        [object]$Metadata
    )

    try {
        if (-not (Test-Path -LiteralPath $EntryDirectory -PathType Container)) {
            return $false
        }
        $entry = Get-Item -LiteralPath $EntryDirectory -Force
        if (($entry.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            return $false
        }
        $entryItems = @(Get-ChildItem -LiteralPath $EntryDirectory -Force)
        if ($entryItems.Count -ne 2 -or
            -not (Test-Path -LiteralPath (Join-Path $EntryDirectory 'mirror') -PathType Container)) {
            return $false
        }
        $descriptorPath = Join-Path $EntryDirectory 'complete.json'
        if (-not (Test-Path -LiteralPath $descriptorPath -PathType Leaf)) {
            return $false
        }
        $descriptorInfo = Get-Item -LiteralPath $descriptorPath -Force
        if (($descriptorInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            return $false
        }
        $descriptor = [IO.File]::ReadAllText($descriptorPath) | ConvertFrom-Json
        $expectedProperties = @('manifestSha256', 'schemaVersion', 'target', 'toolchain')
        $actualProperties = @($descriptor.PSObject.Properties.Name | Sort-Object)
        if (($actualProperties -join '|') -cne ($expectedProperties -join '|') -or
            [int]$descriptor.schemaVersion -ne [int]$Metadata.schemaVersion -or
            [string]$descriptor.toolchain -cne [string]$Metadata.toolchain -or
            [string]$descriptor.target -cne [string]$Metadata.target -or
            [string]$descriptor.manifestSha256 -cne [string]$Metadata.manifestSha256) {
            return $false
        }
        Assert-StackRustMirrorPayloads -MirrorRoot (Join-Path $EntryDirectory 'mirror') -Payloads $Payloads -Metadata $Metadata
        return $true
    } catch {
        return $false
    }
}

function Publish-StackRustMirrorCacheEntry {
    param(
        [Parameter(Mandatory = $true)]
        [string]$PackageRoot,
        [Parameter(Mandatory = $true)]
        [string]$EntryDirectory,
        [Parameter(Mandatory = $true)]
        [string]$GuestMirrorRoot,
        [Parameter(Mandatory = $true)]
        [object[]]$Payloads,
        [Parameter(Mandatory = $true)]
        [object]$Metadata
    )

    Assert-ProvisioningCachePath -Path $PackageRoot
    $staging = Join-Path $PackageRoot ('.stage-' + [Guid]::NewGuid().ToString('N'))
    $stagedMirror = Join-Path $staging 'mirror'
    New-Item -ItemType Directory -Path $stagedMirror -Force | Out-Null
    Assert-ProvisioningCachePath -Path $staging
    $displaced = ''
    $promotionSucceeded = $false
    $primaryFailure = $null
    $cleanupFailure = $null
    try {
        foreach ($payload in $Payloads) {
            $source = Join-Path $GuestMirrorRoot $payload.RelativePath
            $destination = Join-Path $stagedMirror $payload.RelativePath
            New-Item -ItemType Directory -Path (Split-Path -Parent $destination) -Force | Out-Null
            Copy-Item -LiteralPath $source -Destination $destination -Force
        }
        Assert-StackRustMirrorPayloads -MirrorRoot $stagedMirror -Payloads $Payloads -Metadata $Metadata
        $descriptor = [ordered]@{
            schemaVersion = [int]$Metadata.schemaVersion
            toolchain = [string]$Metadata.toolchain
            target = [string]$Metadata.target
            manifestSha256 = [string]$Metadata.manifestSha256
        } | ConvertTo-Json -Compress
        [IO.File]::WriteAllText((Join-Path $staging 'complete.json'), $descriptor, (New-Object Text.UTF8Encoding($false)))
        if (-not (Test-StackRustMirrorCacheEntry -EntryDirectory $staging -Payloads $Payloads -Metadata $Metadata)) {
            throw 'Staged Rust mirror validation failed.'
        }
        if (Test-Path -LiteralPath $EntryDirectory) {
            Assert-ProvisioningCachePath -Path $EntryDirectory
            $displaced = Join-Path $PackageRoot ('.invalid-' + [Guid]::NewGuid().ToString('N'))
            Move-Item -LiteralPath $EntryDirectory -Destination $displaced
        }
        try {
            Move-Item -LiteralPath $staging -Destination $EntryDirectory
        } catch {
            $promotionFailure = $_
            $rollbackFailure = $null
            try {
                if (-not [string]::IsNullOrWhiteSpace($displaced) -and
                    (Test-Path -LiteralPath $displaced) -and
                    -not (Test-Path -LiteralPath $EntryDirectory)) {
                    Move-Item -LiteralPath $displaced -Destination $EntryDirectory
                    $displaced = ''
                }
            } catch {
                $rollbackFailure = $_
            }
            if ($null -ne $rollbackFailure) {
                Write-Warning "Rust mirror cache rollback also failed: $($rollbackFailure.Exception.Message)"
            }
            throw $promotionFailure
        }
        if (-not (Test-StackRustMirrorCacheEntry -EntryDirectory $EntryDirectory -Payloads $Payloads -Metadata $Metadata)) {
            throw 'Published Rust mirror validation failed.'
        }
        $promotionSucceeded = $true
    } catch {
        $primaryFailure = $_
    } finally {
        try {
            if (Test-Path -LiteralPath $staging) {
                Assert-ProvisioningCacheTree -Path $staging
                Remove-Item -LiteralPath $staging -Recurse -Force
            }
        } catch {
            $cleanupFailure = $_
        }
        try {
            if ($promotionSucceeded -and -not [string]::IsNullOrWhiteSpace($displaced) -and
                (Test-Path -LiteralPath $displaced)) {
                Assert-ProvisioningCacheTree -Path $displaced
                Remove-Item -LiteralPath $displaced -Recurse -Force
            }
        } catch {
            if ($null -eq $cleanupFailure) {
                $cleanupFailure = $_
            }
        }
    }
    if ($null -ne $primaryFailure) {
        if ($null -ne $cleanupFailure) {
            Write-Warning "Rust mirror cache cleanup also failed: $($cleanupFailure.Exception.Message)"
        }
        throw $primaryFailure
    }
    if ($null -ne $cleanupFailure) {
        throw $cleanupFailure
    }
}

function Install-GoStack {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [string]$ProjectDirectory,
        [ValidatePattern('^$|^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$')]
        [string]$Version = ''
    )

    if (-not (Test-Path -LiteralPath (Join-Path $ProjectDirectory 'go.mod') -PathType Leaf)) {
        throw "Go project go.mod is missing from mapped project: $ProjectDirectory"
    }
    Write-Output 'Installing Go...'
    Install-ProvisioningWinGetPackage -Role 'Go' -Id 'GoLang.Go' -Version $Version -InstallerType 'wix' `
        -Scope 'machine' -Adapter 'MSI' -ExecutableName 'go.exe' -RequireAuthenticodeSignature
    $goPattern = if ([string]::IsNullOrWhiteSpace($Version)) {
        '^go version go\d+\.\d+\.\d+ windows/amd64$'
    } else {
        '^go version go' + [regex]::Escape($Version) + ' windows/amd64$'
    }
    $goVersion = Assert-ProvisioningCommand -Role 'Go' -Name 'go.exe' `
        -VersionArguments @('version') -ExpectedPattern $goPattern
    Write-Output "Go ready: $goVersion"
}

function Install-NodeStack {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^\d+\.\d+\.\d+$')]
        [string]$Version = ''
    )

    Write-Output 'Installing Node.js LTS...'
    Install-ProvisioningWinGetPackage -Role 'Node.js LTS' -Id 'OpenJS.NodeJS.LTS' -Version $Version `
        -InstallerType 'wix' -Scope 'machine' -Adapter 'MSI' -ExecutableName 'node.exe' `
        -RequireAuthenticodeSignature
    $nodePattern = if ([string]::IsNullOrWhiteSpace($Version)) {
        '^v\d+\.\d+\.\d+$'
    } else {
        '^v' + [regex]::Escape($Version) + '$'
    }
    $nodeVersion = Assert-ProvisioningCommand -Role 'Node.js' -Name 'node.exe' `
        -VersionArguments @('--version') -ExpectedPattern $nodePattern
    Write-Output "Node.js ready: $nodeVersion"
}

function Resolve-StackPythonPackage {
    param(
        [AllowEmptyString()]
        [string]$Series,
        [AllowEmptyString()]
        [string]$Version
    )

    $seriesPattern = '^(?<major>[1-9][0-9]*)\.(?<minor>0|[1-9][0-9]*)$'
    $versionPattern = '^(?<major>[1-9][0-9]*)\.(?<minor>0|[1-9][0-9]*)\.(?<patch>0|[1-9][0-9]*)(?:\.(?<revision>0|[1-9][0-9]*))?$'
    if (-not [string]::IsNullOrWhiteSpace($Series) -and $Series -notmatch $seriesPattern) {
        throw "Python series is invalid: $Series"
    }
    if (-not [string]::IsNullOrWhiteSpace($Version)) {
        $versionMatch = [regex]::Match($Version, $versionPattern)
        if (-not $versionMatch.Success) {
            throw "Python version is invalid: $Version"
        }
        $derivedSeries = $versionMatch.Groups['major'].Value + '.' + $versionMatch.Groups['minor'].Value
        if (-not [string]::IsNullOrWhiteSpace($Series) -and $Series -cne $derivedSeries) {
            throw "Python version $Version conflicts with series $Series."
        }
        return [pscustomobject]@{ Series = $derivedSeries; Version = $Version }
    }

    $query = if ([string]::IsNullOrWhiteSpace($Series)) { 'Python.Python.' } else { "Python.Python.$Series" }
    $rows = @(Search-ProvisioningWinGetPackages -Role 'Python' -IdQuery $query -Exact:(-not [string]::IsNullOrWhiteSpace($Series)))
    $candidates = @()
    foreach ($row in $rows) {
        $idMatch = [regex]::Match([string]$row.Id, '^Python\.Python\.(?<major>[1-9][0-9]*)\.(?<minor>0|[1-9][0-9]*)$')
        $versionMatch = [regex]::Match([string]$row.Version, $versionPattern)
        if (-not $idMatch.Success) {
            $majorOnlyID = [regex]::Match([string]$row.Id, '^Python\.Python\.(?<major>[1-9][0-9]*)$')
            if ($majorOnlyID.Success -and $versionMatch.Success -and
                $majorOnlyID.Groups['major'].Value -ceq $versionMatch.Groups['major'].Value) {
                continue
            }
            throw "Python WinGet search returned an unsupported package identity: $($row.Id) $($row.Version)"
        }
        if (-not $versionMatch.Success -or
            $idMatch.Groups['major'].Value -cne $versionMatch.Groups['major'].Value -or
            $idMatch.Groups['minor'].Value -cne $versionMatch.Groups['minor'].Value) {
            throw "Python WinGet search returned an unsupported package identity: $($row.Id) $($row.Version)"
        }
        $candidateSeries = $idMatch.Groups['major'].Value + '.' + $idMatch.Groups['minor'].Value
        if (-not [string]::IsNullOrWhiteSpace($Series) -and $candidateSeries -cne $Series) {
            throw "Python WinGet exact search returned $($row.Id) instead of Python.Python.$Series."
        }
        $candidates += [pscustomobject]@{
            Series = $candidateSeries
            Version = [string]$row.Version
            Major = [int64]$idMatch.Groups['major'].Value
            Minor = [int64]$idMatch.Groups['minor'].Value
        }
    }
    if (-not [string]::IsNullOrWhiteSpace($Series) -and $candidates.Count -ne 1) {
        throw "Python series $Series resolved $($candidates.Count) WinGet packages; expected one."
    }
    $selected = @($candidates | Sort-Object -Property @{ Expression = 'Major'; Descending = $true }, @{ Expression = 'Minor'; Descending = $true } | Select-Object -First 1)
    if ($selected.Count -ne 1) {
        throw 'Python latest stable package could not be resolved.'
    }
    return [pscustomobject]@{ Series = $selected[0].Series; Version = $selected[0].Version }
}

function Install-PythonStack {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^[1-9][0-9]*\.(?:0|[1-9][0-9]*)$')]
        [string]$Series = '',
        [ValidatePattern('^$|^[1-9][0-9]*\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*))?$')]
        [string]$Version = ''
    )

    $pythonSelection = Resolve-StackPythonPackage -Series $Series -Version $Version
    $Series = [string]$pythonSelection.Series
    $Version = [string]$pythonSelection.Version
    Write-Output "Installing Python $Version..."
    Install-ProvisioningWinGetPackage -Role 'Python' -Id "Python.Python.$Series" -Version $Version `
        -InstallerType 'burn' -Scope 'machine' -Adapter 'Burn' -ExecutableName 'python.exe' `
        -CommandSourceExclusion '*\Microsoft\WindowsApps\python.exe' -DeferCommandReadiness `
        -RequireAuthenticodeSignature
    Wait-ProvisioningCommandAvailable -Role 'Python' -Name 'python.exe' `
        -CommandSourceExclusion '*\Microsoft\WindowsApps\python.exe' | Out-Null
    $runtimeVersion = ($Version -split '\.')[0..2] -join '\.'
    $pythonPattern = '^Python ' + [regex]::Escape($runtimeVersion) + '$'
    $pythonVersion = Assert-ProvisioningCommand -Role 'Python' -Name 'python.exe' `
        -VersionArguments @('--version') -ExpectedPattern $pythonPattern
    Write-Output "Python ready: $pythonVersion"
}

function Install-ZigStack {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^\d+\.\d+\.\d+$')]
        [string]$Version = ''
    )

    Write-Output 'Installing Zig...'
    Install-ProvisioningWinGetPackage -Role 'Zig' -Id 'zig.zig' -Version $Version `
        -InstallerType 'zip' -Adapter 'Portable' -ExecutableName 'zig.exe'
    $zigPattern = if ([string]::IsNullOrWhiteSpace($Version)) {
        '^\d+\.\d+\.\d+$'
    } else {
        '^' + [regex]::Escape($Version) + '$'
    }
    $zigVersion = Assert-ProvisioningCommand -Role 'Zig' -Name 'zig.exe' `
        -VersionArguments @('version') -ExpectedPattern $zigPattern
    $zigBuildRoot = 'C:\HerdrSandbox\build\zig'
    $env:ZIG_LOCAL_CACHE_DIR = Join-Path $zigBuildRoot 'local-cache'
    $env:ZIG_GLOBAL_CACHE_DIR = Join-Path $zigBuildRoot 'global-cache'
    foreach ($directory in @($env:ZIG_LOCAL_CACHE_DIR, $env:ZIG_GLOBAL_CACHE_DIR)) {
        New-Item -ItemType Directory -Path $directory -Force | Out-Null
    }
    [Environment]::SetEnvironmentVariable('ZIG_LOCAL_CACHE_DIR', $env:ZIG_LOCAL_CACHE_DIR, 'Machine')
    [Environment]::SetEnvironmentVariable('ZIG_GLOBAL_CACHE_DIR', $env:ZIG_GLOBAL_CACHE_DIR, 'Machine')
    Write-Output "Zig ready: $zigVersion"
}

function Install-RustMSVCStack {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [string]$ProjectDirectory,
        [ValidatePattern('^$|^\d+\.\d+\.\d+$')]
        [string]$Toolchain = ''
    )

    if (-not (Test-Path -LiteralPath $ProjectDirectory -PathType Container)) {
        throw "Rust project directory is missing: $ProjectDirectory"
    }
    $toolchainFile = Join-Path $ProjectDirectory 'rust-toolchain.toml'
    $projectToolchain = ''
    if (Test-Path -LiteralPath $toolchainFile -PathType Leaf) {
        $toolchainText = [IO.File]::ReadAllText($toolchainFile)
        $channelMatches = [regex]::Matches($toolchainText, '(?m)^\s*channel\s*=\s*"([^"]+)"\s*$')
        if ($channelMatches.Count -ne 1) {
            throw "Rust toolchain file must declare exactly one literal channel: $toolchainFile"
        }
        $projectToolchain = [string]$channelMatches[0].Groups[1].Value
    }
    $exactToolchainPattern = '^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$'
    if (-not [string]::IsNullOrWhiteSpace($projectToolchain) -and $projectToolchain -notmatch $exactToolchainPattern) {
        throw "Rust project toolchain must pin one exact x.y.z channel: $projectToolchain"
    }
    if ([string]::IsNullOrWhiteSpace($Toolchain)) {
        $requestedChannel = if ([string]::IsNullOrWhiteSpace($projectToolchain)) { 'stable' } else { $projectToolchain }
    } elseif (-not [string]::IsNullOrWhiteSpace($projectToolchain) -and $Toolchain -cne $projectToolchain) {
        throw "Requested Rust toolchain $Toolchain conflicts with project toolchain $projectToolchain."
    } else {
        $requestedChannel = $Toolchain
    }

$rustTriple = 'x86_64-pc-windows-msvc'
$rustDistribution = Resolve-StackRustDistribution -RequestedChannel $requestedChannel -Target $rustTriple
$Toolchain = [string]$rustDistribution.Toolchain
$rustToolchain = "$Toolchain-$rustTriple"
Write-Output "Installing Rust $Toolchain for MSVC..."
$env:RUSTUP_HOME = 'C:\HerdrSandbox\toolchains\rustup'
$env:CARGO_HOME = 'C:\HerdrSandbox\toolchains\cargo'
$env:CARGO_TARGET_DIR = 'C:\HerdrSandbox\build\cargo-target'
$env:RUSTUP_AUTO_INSTALL = '0'
foreach ($directory in @($env:RUSTUP_HOME, $env:CARGO_HOME, $env:CARGO_TARGET_DIR)) {
    New-Item -ItemType Directory -Path $directory -Force | Out-Null
}
$machineEnvironment = [ordered]@{
    RUSTUP_HOME = $env:RUSTUP_HOME
    CARGO_HOME = $env:CARGO_HOME
    CARGO_TARGET_DIR = $env:CARGO_TARGET_DIR
    RUSTUP_AUTO_INSTALL = $env:RUSTUP_AUTO_INSTALL
}
foreach ($entry in $machineEnvironment.GetEnumerator()) {
    [Environment]::SetEnvironmentVariable([string]$entry.Key, [string]$entry.Value, 'Machine')
}
Install-ProvisioningWinGetPackage -Role 'Rustup' -Id 'Rustlang.Rustup' -InstallerType 'exe' `
    -Adapter 'Rustup' -InstallerArguments @('-y', '-q', '--no-modify-path', '--default-host', $rustTriple,
        '--default-toolchain', 'none', '--profile', 'minimal')
$cargoDirectory = Join-Path $env:CARGO_HOME 'bin'
Add-ProvisioningMachinePath -Directory $cargoDirectory
$rustupVersion = Assert-ProvisioningCommand -Role 'Rustup' -Name 'rustup.exe' `
    -VersionArguments @('--version') -ExpectedPattern '^rustup \d+\.\d+\.\d+ '
$rustPayloads = @($rustDistribution.Payloads)
$rustCacheMetadata = $rustDistribution.Metadata
$rustCacheRoot = 'C:\HerdrSandbox\cache\rust'
$rustEntryName = [string]$rustDistribution.CacheEntryName
$rustEntryDirectory = Join-Path $rustCacheRoot $rustEntryName
$rustGuestStage = Join-Path 'C:\HerdrSandbox\staging\rust-mirror' ([Guid]::NewGuid().ToString('N'))
$rustGuestMirror = Join-Path $rustGuestStage 'mirror'
$rustLock = $null
$rustServer = $null
$rustSetupSucceeded = $false
$rustPrimaryFailure = $null
$rustCleanupFailure = $null
$rustStopwatch = [Diagnostics.Stopwatch]::StartNew()
New-Item -ItemType Directory -Path $rustCacheRoot -Force | Out-Null
Assert-ProvisioningCachePath -Path $rustCacheRoot
New-Item -ItemType Directory -Path $rustGuestMirror -Force | Out-Null
try {
    $rustLockPath = Join-Path $rustCacheRoot '.lock'
    Assert-ProvisioningCachePath -Path $rustLockPath
    $rustLock = [IO.File]::Open($rustLockPath, [IO.FileMode]::OpenOrCreate,
        [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
    if (Test-Path -LiteralPath $rustEntryDirectory) {
        Assert-ProvisioningCachePath -Path $rustEntryDirectory
    }
    $rustCacheHit = Test-StackRustMirrorCacheEntry -EntryDirectory $rustEntryDirectory `
        -Payloads $rustPayloads -Metadata $rustCacheMetadata
    if ($rustCacheHit) {
        Write-Output "Rust distribution mirror cache hit: $Toolchain"
        $cachedMirrorRoot = Join-Path $rustEntryDirectory 'mirror'
        foreach ($payload in $rustPayloads) {
            $source = Join-Path $cachedMirrorRoot $payload.RelativePath
            $destination = Join-Path $rustGuestMirror $payload.RelativePath
            New-Item -ItemType Directory -Path (Split-Path -Parent $destination) -Force | Out-Null
            Copy-Item -LiteralPath $source -Destination $destination -Force
        }
        Assert-StackRustMirrorPayloads -MirrorRoot $rustGuestMirror -Payloads $rustPayloads `
            -Metadata $rustCacheMetadata
        $rustMirrorRoot = $rustGuestMirror
    } else {
        Write-Output "Rust distribution mirror cache miss: $Toolchain"
        foreach ($payload in $rustPayloads) {
            $destination = Join-Path $rustGuestMirror $payload.RelativePath
            New-Item -ItemType Directory -Path (Split-Path -Parent $destination) -Force | Out-Null
            if ([string]$payload.RelativePath -ceq [string]$rustDistribution.ManifestRelativePath) {
                [IO.File]::WriteAllBytes($destination, [byte[]]$rustDistribution.ManifestBytes)
            } elseif ([string]$payload.RelativePath -ceq [string]$rustDistribution.SidecarRelativePath) {
                [IO.File]::WriteAllBytes($destination, [byte[]]$rustDistribution.SidecarBytes)
            } else {
                Invoke-WebRequest -Uri ([string]$payload.Url) -OutFile $destination `
                    -UseBasicParsing -ErrorAction Stop
            }
            $actualHash = (Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash.ToUpperInvariant()
            if ($actualHash -cne $payload.Sha256) {
                throw "Downloaded Rust mirror payload hash mismatch: $($payload.RelativePath)"
            }
        }
        Assert-StackRustMirrorPayloads -MirrorRoot $rustGuestMirror -Payloads $rustPayloads `
            -Metadata $rustCacheMetadata
        $rustMirrorRoot = $rustGuestMirror
    }

    $rustPort = 49601
    $rustDistServer = "http://127.0.0.1:$rustPort"
    $env:RUSTUP_DIST_SERVER = $rustDistServer
    $env:RUSTUP_UPDATE_ROOT = "$rustDistServer/__self_update_disabled__"
    $env:RUSTUP_AUTO_INSTALL = '0'
    $env:NO_PROXY = '127.0.0.1,localhost'
    $env:no_proxy = $env:NO_PROXY
    Wait-ProvisioningCommandAvailable -Role 'Python' -Name 'python.exe' `
        -CommandSourceExclusion '*\Microsoft\WindowsApps\python.exe' | Out-Null
    $pythonVersion = Assert-ProvisioningCommand -Role 'Python' -Name 'python.exe' `
        -VersionArguments @('--version') -ExpectedPattern '^Python 3\.\d+\.\d+$'
    $pythonCommand = Get-Command 'python.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1
    $rustServerOutput = Join-Path $rustGuestStage 'server.stdout.log'
    $rustServerError = Join-Path $rustGuestStage 'server.stderr.log'
    $rustServer = Start-Process -FilePath $pythonCommand.Source -ArgumentList @(
        '-I', '-u', '-m', 'http.server', '--bind', '127.0.0.1', '--directory', $rustMirrorRoot, [string]$rustPort
    ) -WindowStyle Hidden -RedirectStandardOutput $rustServerOutput -RedirectStandardError $rustServerError -PassThru
    $probeURI = "$rustDistServer/$($rustDistribution.SidecarRelativePath.Replace('\', '/'))"
    $probeUTF8 = New-Object Text.UTF8Encoding($false, $true)
    $expectedProbeBody = $probeUTF8.GetString([byte[]]$rustDistribution.SidecarBytes)
    $probeDeadline = [DateTime]::UtcNow.AddSeconds(10)
    $serverReady = $false
    $lastProbeError = ''
    do {
        if ($rustServer.HasExited) {
            $serverFailure = if (Test-Path -LiteralPath $rustServerError) { [IO.File]::ReadAllText($rustServerError) } else { '' }
            throw "Rust mirror server exited early. $serverFailure"
        }
        try {
            $response = Invoke-WebRequest -Uri $probeURI -UseBasicParsing -TimeoutSec 1
            $body = Get-StackWebResponseText -Response $response
            if ($response.StatusCode -eq 200 -and $body -ceq $expectedProbeBody) {
                $serverReady = $true
            } else {
                $lastProbeError = "unexpected HTTP response status=$($response.StatusCode) characters=$($body.Length)"
            }
        } catch {
            $lastProbeError = $_.Exception.Message
        }
        if (-not $serverReady) {
            Start-Sleep -Milliseconds 100
        }
    } while (-not $serverReady -and [DateTime]::UtcNow -lt $probeDeadline)
    if (-not $serverReady) {
        throw "Rust mirror readiness timed out: $lastProbeError"
    }

    Invoke-ProvisioningNative -Role 'Rust toolchain installation' -FilePath 'rustup.exe' -ArgumentList @(
        'toolchain', 'install', $rustToolchain, '--profile', 'minimal', '--component', 'rustfmt',
        '--component', 'clippy', '--target', $rustTriple, '--no-self-update'
    ) | Out-Null
    Invoke-ProvisioningNative -Role 'Rust default toolchain selection' -FilePath 'rustup.exe' `
        -ArgumentList @('default', $rustToolchain) | Out-Null
    Invoke-ProvisioningNative -Role 'Rustup automatic self-update disable' -FilePath 'rustup.exe' `
        -ArgumentList @('set', 'auto-self-update', 'disable') | Out-Null
    Invoke-ProvisioningNative -Role 'Rustup automatic toolchain install disable' -FilePath 'rustup.exe' `
        -ArgumentList @('set', 'auto-install', 'disable') | Out-Null

    if (-not $rustCacheHit) {
        Publish-StackRustMirrorCacheEntry -PackageRoot $rustCacheRoot -EntryDirectory $rustEntryDirectory `
            -GuestMirrorRoot $rustGuestMirror -Payloads $rustPayloads -Metadata $rustCacheMetadata
    }
    foreach ($directory in @(Get-ChildItem -LiteralPath $rustCacheRoot -Directory -Force)) {
        if ($directory.Name -ine $rustEntryName) {
            Assert-ProvisioningCacheTree -Path $directory.FullName
            Remove-Item -LiteralPath $directory.FullName -Recurse -Force
        }
    }
    $rustSetupSucceeded = $true
} catch {
    $rustPrimaryFailure = $_
} finally {
    if ($null -ne $rustServer) {
        try {
            if (-not $rustServer.HasExited) {
                Stop-Process -InputObject $rustServer -Force -ErrorAction Stop
            }
            if (-not $rustServer.WaitForExit(5000)) {
                throw "Rust mirror server did not stop: PID $($rustServer.Id)"
            }
        } catch {
            $rustCleanupFailure = $_
        } finally {
            try {
                $rustServer.Dispose()
            } catch {
                if ($null -eq $rustCleanupFailure) {
                    $rustCleanupFailure = $_
                }
            }
        }
    }
    if ($null -ne $rustLock) {
        try {
            $rustLock.Dispose()
        } catch {
            if ($null -eq $rustCleanupFailure) {
                $rustCleanupFailure = $_
            }
        }
    }
    if ($rustSetupSucceeded) {
        try {
            if (Test-Path -LiteralPath $rustGuestStage) {
                Remove-Item -LiteralPath $rustGuestStage -Recurse -Force
            }
        } catch {
            if ($null -eq $rustCleanupFailure) {
                $rustCleanupFailure = $_
            }
        }
    }
    $rustStopwatch.Stop()
    Write-ProvisioningTiming -Role 'Rust toolchain total' -Seconds $rustStopwatch.Elapsed.TotalSeconds
}
if ($null -ne $rustPrimaryFailure) {
    if ($null -ne $rustCleanupFailure) {
        Write-Warning "Rust cleanup also failed: $($rustCleanupFailure.Exception.Message)"
    }
    throw $rustPrimaryFailure
}
if ($null -ne $rustCleanupFailure) {
    throw $rustCleanupFailure
}
$toolchainPattern = [regex]::Escape($Toolchain)
$rustVersion = Assert-ProvisioningCommand -Role 'Rust' -Name 'rustc.exe' `
    -VersionArguments @('--version') -ExpectedPattern ("^rustc $toolchainPattern ")
$cargoVersion = Assert-ProvisioningCommand -Role 'Cargo' -Name 'cargo.exe' `
    -VersionArguments @('--version') -ExpectedPattern ("^cargo $toolchainPattern ")

Write-Output 'Installing Visual Studio C++ Build Tools...'
Install-StackVisualStudioBuildTools
Write-Output "Rustup ready: $rustupVersion"
Write-Output "Rust ready: $rustVersion"
Write-Output "Cargo ready: $cargoVersion"
}

function Install-CargoNextest {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^\d+\.\d+\.\d+$')]
        [string]$Version = ''
    )

    Write-Output 'Installing Cargo Nextest...'
    Install-ProvisioningWinGetPackage -Role 'Cargo Nextest' -Id 'nextest.cargo-nextest' -Version $Version `
        -InstallerType 'zip' -Adapter 'Portable' -ExecutableName 'cargo-nextest.exe'
    $nextestPattern = if ([string]::IsNullOrWhiteSpace($Version)) {
        '^cargo-nextest \d+\.\d+\.\d+ '
    } else {
        '^cargo-nextest ' + [regex]::Escape($Version) + ' '
    }
    $nextestVersion = Assert-ProvisioningCommand -Role 'Cargo Nextest' -Name 'cargo-nextest.exe' `
        -VersionArguments @('--version') -ExpectedPattern $nextestPattern
    Write-Output "Cargo Nextest ready: $nextestVersion"
}

function Install-Just {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^\d+\.\d+\.\d+$')]
        [string]$Version = ''
    )

    Write-Output 'Installing Just...'
    Install-ProvisioningWinGetPackage -Role 'Just' -Id 'Casey.Just' -Version $Version `
        -InstallerType 'zip' -Adapter 'Portable' -ExecutableName 'just.exe'
    $justPattern = if ([string]::IsNullOrWhiteSpace($Version)) {
        '^just \d+\.\d+\.\d+$'
    } else {
        '^just ' + [regex]::Escape($Version) + '$'
    }
    $justVersion = Assert-ProvisioningCommand -Role 'Just' -Name 'just.exe' `
        -VersionArguments @('--version') -ExpectedPattern $justPattern
    Write-Output "Just ready: $justVersion"
}
