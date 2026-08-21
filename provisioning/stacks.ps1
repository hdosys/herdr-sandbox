# herdr-sandbox-stacks-contract: 23

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

function Get-StackVisualStudioInstallation {
    param(
        [Parameter(Mandatory = $true)]
        [object]$Target
    )

    $vswhere = [string](Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\vswhere.exe')
    if (-not (Test-Path -LiteralPath $vswhere -PathType Leaf)) {
        return ''
    }
    $arguments = @(
        '-latest', '-products', '*', '-requires',
        'Microsoft.VisualStudio.Component.VC.Tools.x86.x64',
        'Microsoft.VisualStudio.Component.Windows11SDK.26100'
    )
    $pathResult = Invoke-ProvisioningNativeResult -Role 'Visual Studio installation path inspection' `
        -FilePath $vswhere -ArgumentList ($arguments + @('-property', 'installationPath')) -TimeoutSeconds 30
    $versionResult = Invoke-ProvisioningNativeResult -Role 'Visual Studio installation version inspection' `
        -FilePath $vswhere -ArgumentList ($arguments + @('-property', 'installationVersion')) -TimeoutSeconds 30
    if (-not $pathResult.Succeeded -or -not $versionResult.Succeeded) {
        return ''
    }
    $installationPath = (@(ConvertFrom-ProvisioningNativeOutput -Text ([string]$pathResult.Output)) -join ' ').Trim()
    $installationVersion = (@(ConvertFrom-ProvisioningNativeOutput -Text ([string]$versionResult.Output)) -join ' ').Trim()
    if ($installationPath -ine 'C:\HerdrSandbox\toolchains\visual-studio' -or
        $installationVersion -cne [string]$Target.BuildVersion) {
        return ''
    }
    return $installationPath
}

function Wait-StackVisualStudioInstalled {
    param(
        [Parameter(Mandatory = $true)]
        [object]$Target,
        [int]$TimeoutSeconds = 120
    )

    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        $installationPath = Get-StackVisualStudioInstallation -Target $Target
        if (-not [string]::IsNullOrWhiteSpace($installationPath)) {
            return
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
        [string]$candidate.EdgeTraversalPolicy -ceq 'Block' -and
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
    New-NetFirewallRule -Name $Name -DisplayName $Name -Enabled True -Profile Any `
        -Direction $Direction -Action Block -LooseSourceMapping $false -LocalOnlyMapping $false -Program $Program `
        -LocalAddress Any -RemoteAddress Any -Protocol Any -LocalPort Any -RemotePort Any `
        -Service Any -InterfaceType Any -Authentication NotRequired `
        -Encryption NotRequired | Out-Null
    $verified = @(Get-NetFirewallRule -Name $Name -ErrorAction Stop)
    if (-not (Test-StackVisualStudioFirewallRule -Rules $verified -Name $Name `
                -Direction $Direction -Program $Program)) {
        throw "Visual Studio firewall rule verification failed: $Name"
    }
}

function Install-StackVisualStudioBuildTools {
    param(
        [AllowNull()]
        [Collections.IDictionary]$RustToolchainTask = $null
    )

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
        $installedPath = Get-StackVisualStudioInstallation -Target $target
        if (-not [string]::IsNullOrWhiteSpace($installedPath)) {
            Write-Output "Visual Studio Build Tools already matches Current: $($target.BuildVersion)"
            if ($null -ne $RustToolchainTask) {
                Invoke-ProvisioningNative -Role ([string]$RustToolchainTask['Role']) `
                    -FilePath $RustToolchainTask['FilePath'] `
                    -ArgumentList ([string[]]@($RustToolchainTask['ArgumentList'])) `
                    -WorkingDirectory ([string]$RustToolchainTask['WorkingDirectory']) `
                    -TimeoutSeconds ([int]$RustToolchainTask['TimeoutSeconds']) | Out-Null
            }
        } else {
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
            if ($null -eq $RustToolchainTask) {
                Invoke-ProvisioningNative -Role 'Visual Studio Build Tools offline installation' `
                    -FilePath $guestLayoutBootstrapper -ArgumentList $installationArguments `
                    -WorkingDirectory $guestLayout -TimeoutSeconds 900 | Out-Null
            } else {
                $installationGroup = Start-ProvisioningNativeGroup -Tasks @(
                    $RustToolchainTask,
                    [ordered]@{
                        Role = 'Visual Studio Build Tools offline installation'
                        FilePath = $guestLayoutBootstrapper
                        ArgumentList = $installationArguments
                        WorkingDirectory = $guestLayout
                        TimeoutSeconds = 900
                    }
                )
                $installationCompleted = $false
                try {
                    Complete-ProvisioningNativeGroup -Group $installationGroup | Out-Null
                    $installationCompleted = $true
                } finally {
                    if (-not $installationCompleted) {
                        Stop-ProvisioningNativeGroup -Group $installationGroup
                    }
                }
            }
            Wait-StackVisualStudioInstalled -Target $target
        }
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

function Enable-StackVisualStudioDeveloperEnvironment {
    $installationRoot = 'C:\HerdrSandbox\toolchains\visual-studio'
    $developerShell = Join-Path $installationRoot 'Common7\Tools\Launch-VsDevShell.ps1'
    foreach ($path in @($installationRoot, $developerShell)) {
        if (-not (Test-Path -LiteralPath $path)) {
            throw "Visual Studio developer environment input is missing: $path"
        }
        $item = Get-Item -LiteralPath $path -Force
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
            ($path -ceq $installationRoot -and -not $item.PSIsContainer) -or
            ($path -ceq $developerShell -and $item.PSIsContainer)) {
            throw "Visual Studio developer environment input is unsafe: $path"
        }
    }

    $originalLocation = Get-Location
    try {
        & $developerShell -Arch 'amd64' -HostArch 'amd64' -SkipAutomaticLocation | Out-Null
    } finally {
        Set-Location -LiteralPath $originalLocation.ProviderPath
    }

    $requiredEnvironment = @(
        'INCLUDE', 'LIB', 'LIBPATH', 'VCINSTALLDIR', 'VCToolsInstallDir',
        'VSINSTALLDIR', 'WindowsSdkDir', 'WindowsSDKVersion'
    )
    foreach ($name in $requiredEnvironment) {
        $value = [string][Environment]::GetEnvironmentVariable($name, 'Process')
        if ([string]::IsNullOrWhiteSpace($value)) {
            throw "Visual Studio developer environment did not define $name."
        }
        [Environment]::SetEnvironmentVariable($name, $value, 'Machine')
        if ([Environment]::GetEnvironmentVariable($name, 'Machine') -cne $value) {
            throw "Visual Studio developer environment did not persist $name."
        }
    }
    if ([IO.Path]::GetFullPath($env:VSINSTALLDIR).TrimEnd('\') -ine $installationRoot -or
        -not [IO.Path]::GetFullPath($env:VCToolsInstallDir).StartsWith(
            $installationRoot + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Visual Studio developer environment resolved outside the app-owned installation.'
    }

    $windowsKitsRoot = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10'
    $expectedRoots = [ordered]@{
        'cl.exe' = $installationRoot
        'link.exe' = $installationRoot
        'lib.exe' = $installationRoot
        'nmake.exe' = $installationRoot
        'msbuild.exe' = $installationRoot
        'rc.exe' = $windowsKitsRoot
    }
    $resolvedCommands = [ordered]@{}
    $commandDirectories = @()
    foreach ($entry in $expectedRoots.GetEnumerator()) {
        $command = Get-Command $entry.Key -CommandType Application -ErrorAction Stop | Select-Object -First 1
        $source = [IO.Path]::GetFullPath([string]$command.Source)
        $expectedRoot = [IO.Path]::GetFullPath([string]$entry.Value).TrimEnd('\')
        $item = Get-Item -LiteralPath $source -Force
        if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
            -not $source.StartsWith($expectedRoot + '\', [StringComparison]::OrdinalIgnoreCase)) {
            throw "Visual Studio developer command is outside its expected owner: $($entry.Key) at $source"
        }
        $resolvedCommands[$entry.Key] = $source
        $directory = Split-Path -Parent $source
        if (-not ($commandDirectories | Where-Object { $_ -ieq $directory })) {
            $commandDirectories += $directory
        }
    }
    for ($index = $commandDirectories.Count - 1; $index -ge 0; $index--) {
        Add-ProvisioningMachinePath -Directory $commandDirectories[$index]
    }
    foreach ($entry in $resolvedCommands.GetEnumerator()) {
        $resolved = Get-Command $entry.Key -CommandType Application -ErrorAction Stop | Select-Object -First 1
        if ([IO.Path]::GetFullPath([string]$resolved.Source) -ine [string]$entry.Value) {
            throw "Visual Studio developer command PATH read-back failed: $($entry.Key)"
        }
    }
    return [pscustomobject]@{
        Compiler = [string]$resolvedCommands['cl.exe']
        Linker = [string]$resolvedCommands['link.exe']
        MSBuild = [string]$resolvedCommands['msbuild.exe']
    }
}

function Assert-StackCppToolchain {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Compiler,
        [Parameter(Mandatory = $true)]
        [string]$Linker
    )

    $stage = Join-Path 'C:\HerdrSandbox\staging' ('cpp-stack-probe-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $stage -Force | Out-Null
    try {
        $cSource = Join-Path $stage 'probe.c'
        $cppSource = Join-Path $stage 'probe.cpp'
        $cObject = Join-Path $stage 'c-probe.obj'
        $cppObject = Join-Path $stage 'cpp-probe.obj'
        $cExecutable = Join-Path $stage 'c-probe.exe'
        $cppExecutable = Join-Path $stage 'cpp-probe.exe'
        $cProgram = @'
#include <stdio.h>
int main(void) { puts("c-stack-ok"); return 0; }
'@
        $cppProgram = @'
#include <iostream>
int main() { std::cout << "cpp-stack-ok\n"; return 0; }
'@
        [IO.File]::WriteAllText($cSource, $cProgram + "`n", (New-Object Text.UTF8Encoding($false)))
        [IO.File]::WriteAllText($cppSource, $cppProgram + "`n", (New-Object Text.UTF8Encoding($false)))
        Invoke-ProvisioningNative -Role 'MSVC C compiler probe' -FilePath $Compiler `
            -ArgumentList @('/nologo', '/W4', '/WX', '/Z7', '/TC', '/c', $cSource, "/Fo:$cObject") `
            -WorkingDirectory $stage -TimeoutSeconds 60 -TerminateDescendantsAfterRootExit | Out-Null
        Invoke-ProvisioningNative -Role 'MSVC C linker probe' -FilePath $Linker `
            -ArgumentList @('/NOLOGO', '/DEBUG:NONE', "/OUT:$cExecutable", $cObject) `
            -WorkingDirectory $stage -TimeoutSeconds 30 -TerminateDescendantsAfterRootExit | Out-Null
        Invoke-ProvisioningNative -Role 'MSVC C++ compiler probe' -FilePath $Compiler `
            -ArgumentList @('/nologo', '/W4', '/WX', '/Z7', '/EHsc', '/std:c++20', '/TP', '/c',
                $cppSource, "/Fo:$cppObject") -WorkingDirectory $stage -TimeoutSeconds 60 `
            -TerminateDescendantsAfterRootExit | Out-Null
        Invoke-ProvisioningNative -Role 'MSVC C++ linker probe' -FilePath $Linker `
            -ArgumentList @('/NOLOGO', '/DEBUG:NONE', "/OUT:$cppExecutable", $cppObject) `
            -WorkingDirectory $stage -TimeoutSeconds 30 -TerminateDescendantsAfterRootExit | Out-Null
        $cOutput = ((Invoke-ProvisioningNative -Role 'MSVC C executable probe' -FilePath $cExecutable `
                -ArgumentList @() -WorkingDirectory $stage -TimeoutSeconds 30) -join [Environment]::NewLine).Trim()
        $cppOutput = ((Invoke-ProvisioningNative -Role 'MSVC C++ executable probe' -FilePath $cppExecutable `
                -ArgumentList @() -WorkingDirectory $stage -TimeoutSeconds 30) -join [Environment]::NewLine).Trim()
        if ($cOutput -cne 'c-stack-ok' -or $cppOutput -cne 'cpp-stack-ok') {
            throw "MSVC C/C++ compile and run verification failed: C=$cOutput C++=$cppOutput"
        }
    } finally {
        if (Test-Path -LiteralPath $stage) {
            Remove-Item -LiteralPath $stage -Recurse -Force
        }
    }
}

function Install-CppStack {
    [CmdletBinding()]
    param()

    Write-Output 'Installing Visual Studio C/C++ Build Tools...'
    Install-StackVisualStudioBuildTools
    $environment = Enable-StackVisualStudioDeveloperEnvironment
    Assert-StackCppToolchain -Compiler $environment.Compiler -Linker $environment.Linker
    Write-Output "C/C++ ready: $($environment.Compiler)"
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

function Test-StackAndroidArchiveEntry {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Entry,
        [Parameter(Mandatory = $true)]
        [string]$Root
    )

    if ([string]::IsNullOrWhiteSpace($Entry) -or $Entry.Contains('\') -or
        -not $Entry.StartsWith($Root + '/', [StringComparison]::Ordinal) -or
        $Entry.StartsWith('/', [StringComparison]::Ordinal) -or $Entry -match '^[A-Za-z]:' -or
        @($Entry.TrimEnd('/') -split '/' | Where-Object { $_ -ceq '.' -or $_ -ceq '..' }).Count -ne 0) {
        throw "Android archive entry is unsafe: $Entry"
    }
}

function Assert-StackAndroidTree {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Root,
        [Parameter(Mandatory = $true)]
        [string[]]$RequiredRelativePaths
    )

    if (-not (Test-Path -LiteralPath $Root -PathType Container)) {
        throw "Android tool root is missing: $Root"
    }
    $rootInfo = Get-Item -LiteralPath $Root -Force
    if (($rootInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Android tool root is a reparse point: $Root"
    }
    foreach ($item in @(Get-ChildItem -LiteralPath $Root -Recurse -Force)) {
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Android tool tree contains a reparse point: $($item.FullName)"
        }
    }
    foreach ($relativePath in $RequiredRelativePaths) {
        $path = Join-Path $Root $relativePath
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            throw "Android tool file is missing: $path"
        }
        $item = Get-Item -LiteralPath $path -Force
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Android tool file is a reparse point: $path"
        }
    }
}

function Assert-StackAndroidGoogleSignature {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    $signature = Get-AuthenticodeSignature -LiteralPath $Path
    if ($signature.Status -ne [System.Management.Automation.SignatureStatus]::Valid -or
        $null -eq $signature.SignerCertificate -or
        $signature.SignerCertificate.GetNameInfo(
            [Security.Cryptography.X509Certificates.X509NameType]::SimpleName, $false) -cne 'Google LLC' -or
        $signature.SignerCertificate.Subject -notmatch '(^|,\s*)O=Google LLC(,|$)') {
        throw "Android CLI Authenticode signature is invalid: $($signature.Status)"
    }
}

function Assert-StackAndroidMicrosoftSignature {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )

    $signature = Get-AuthenticodeSignature -LiteralPath $Path
    if ($signature.Status -ne [System.Management.Automation.SignatureStatus]::Valid -or
        $null -eq $signature.SignerCertificate -or
        $signature.SignerCertificate.GetNameInfo(
            [Security.Cryptography.X509Certificates.X509NameType]::SimpleName, $false) -cne 'Microsoft Corporation' -or
        $signature.SignerCertificate.Subject -notmatch '(^|,\s*)O=Microsoft Corporation(,|$)') {
        throw "Android JDK Authenticode signature is invalid: $($signature.Status)"
    }
}

function Install-StackAndroidDirectArchive {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Role,
        [Parameter(Mandatory = $true)]
        [object]$Metadata,
        [Parameter(Mandatory = $true)]
        [string]$Destination,
        [Parameter(Mandatory = $true)]
        [string]$ArchiveRoot,
        [Parameter(Mandatory = $true)]
        [int]$ExpectedEntryCount,
        [Parameter(Mandatory = $true)]
        [string[]]$RequiredRelativePaths,
        [string]$SignedRelativePath = ''
    )

    $cacheRoot = 'C:\HerdrSandbox\cache\packages'
    if (-not (Test-Path -LiteralPath 'C:\HerdrSandbox\cache' -PathType Container)) {
        throw 'The writable guest package cache mapping is missing: C:\HerdrSandbox\cache'
    }
    Assert-ProvisioningCachePath -Path 'C:\HerdrSandbox\cache'
    New-Item -ItemType Directory -Path $cacheRoot -Force | Out-Null
    Assert-ProvisioningCachePath -Path $cacheRoot
    $packageRoot = Join-Path $cacheRoot (Get-ProvisioningSafeCacheName -Value ([string]$Metadata.Id))
    New-Item -ItemType Directory -Path $packageRoot -Force | Out-Null
    Assert-ProvisioningCachePath -Path $packageRoot
    $entryName = (Get-ProvisioningSafeCacheName -Value ([string]$Metadata.Version)) + '-' +
        ([string]$Metadata.Sha256).Substring(0, 16).ToLowerInvariant()
    $entryDirectory = Join-Path $packageRoot $entryName
    $lockPath = Join-Path $packageRoot '.lock'
    Assert-ProvisioningCachePath -Path $lockPath
    $lock = $null
    $guestStage = Join-Path 'C:\HerdrSandbox\staging\packages' ([Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $guestStage -Force | Out-Null
    $guestPayload = Join-Path $guestStage $Metadata.PayloadName
    $extracted = Join-Path $guestStage 'extracted'
    $primaryFailure = $null
    $cleanupFailure = $null
    try {
        $lock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate,
            [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
        if (Test-Path -LiteralPath $entryDirectory) {
            Assert-ProvisioningCachePath -Path $entryDirectory
        }
        $cacheHit = Test-ProvisioningPackageCacheEntry -Directory $entryDirectory -Metadata $Metadata
        if ($cacheHit) {
            Write-Output "$Role package cache hit: $($Metadata.Version)"
            Copy-ProvisioningPackageToGuest -Source (Join-Path $entryDirectory $Metadata.PayloadName) `
                -Destination $guestPayload -ExpectedSHA256 $Metadata.Sha256
        } else {
            Write-Output "$Role package cache miss: $($Metadata.Version)"
            Get-ProvisioningDirectPackage -Role $Role -Metadata $Metadata -GuestPayloadPath $guestPayload
        }

        $tar = Join-Path $env:SystemRoot 'System32\tar.exe'
        $archiveEntries = @(Invoke-ProvisioningNative -Role "$Role archive inspection" -FilePath $tar `
            -ArgumentList @('-tf', $guestPayload) -TimeoutSeconds 120 | ForEach-Object { [string]$_ })
        if ($archiveEntries.Count -ne $ExpectedEntryCount) {
            throw "$Role archive contains $($archiveEntries.Count) entries; expected $ExpectedEntryCount."
        }
        foreach ($archiveEntry in $archiveEntries) {
            Test-StackAndroidArchiveEntry -Entry $archiveEntry -Root $ArchiveRoot
        }
        New-Item -ItemType Directory -Path $extracted | Out-Null
        Invoke-ProvisioningNative -Role "$Role cached extraction" -FilePath $tar `
            -ArgumentList @('-xf', $guestPayload, '-C', $extracted) -TimeoutSeconds 180 | Out-Null
        $sourceRoot = Join-Path $extracted $ArchiveRoot
        Assert-StackAndroidTree -Root $sourceRoot -RequiredRelativePaths $RequiredRelativePaths
        if (-not [string]::IsNullOrWhiteSpace($SignedRelativePath)) {
            Assert-StackAndroidGoogleSignature -Path (Join-Path $sourceRoot $SignedRelativePath)
        }
        if (Test-Path -LiteralPath $Destination) {
            Remove-Item -LiteralPath $Destination -Recurse -Force
        }
        New-Item -ItemType Directory -Path (Split-Path -Parent $Destination) -Force | Out-Null
        Move-Item -LiteralPath $sourceRoot -Destination $Destination
        Assert-StackAndroidTree -Root $Destination -RequiredRelativePaths $RequiredRelativePaths
        if (-not [string]::IsNullOrWhiteSpace($SignedRelativePath)) {
            Assert-StackAndroidGoogleSignature -Path (Join-Path $Destination $SignedRelativePath)
        }
        if (-not $cacheHit) {
            Publish-ProvisioningPackageCacheEntry -PackageRoot $packageRoot -EntryDirectory $entryDirectory `
                -GuestPayloadPath $guestPayload -Metadata $Metadata
        }
        foreach ($directory in @(Get-ChildItem -LiteralPath $packageRoot -Directory -Force)) {
            if ($directory.Name -ine $entryName) {
                Assert-ProvisioningCacheTree -Path $directory.FullName
                Remove-Item -LiteralPath $directory.FullName -Recurse -Force
            }
        }
    } catch {
        $primaryFailure = $_
    } finally {
        if ($null -ne $lock) {
            try { $lock.Dispose() } catch { $cleanupFailure = $_ }
        }
        try {
            if (Test-Path -LiteralPath $guestStage) {
                Remove-ProvisioningGuestPackageStage -Path $guestStage -Attempts 1 `
                    -DelayMilliseconds 0 -BestEffort | Out-Null
            }
        } catch {
            if ($null -eq $cleanupFailure) { $cleanupFailure = $_ }
        }
    }
    if ($null -ne $primaryFailure) {
        if ($null -ne $cleanupFailure) {
            Write-Warning "$Role package cleanup also failed: $($cleanupFailure.Exception.Message)"
        }
        throw $primaryFailure
    }
    if ($null -ne $cleanupFailure) { throw $cleanupFailure }
}

function Assert-StackAndroidPlatformTools {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Root
    )

    Assert-StackAndroidTree -Root $Root `
        -RequiredRelativePaths @('adb.exe', 'AdbWinApi.dll', 'AdbWinUsbApi.dll', 'source.properties')
    $properties = [IO.File]::ReadAllText((Join-Path $Root 'source.properties'))
    if ($properties -notmatch '(?m)^Pkg\.Revision=(?<version>\d+\.\d+\.\d+)\r?$') {
        throw 'Android Platform Tools source identity is unexpected.'
    }
    $version = [string]$Matches['version']
    $adb = Join-Path $Root 'adb.exe'
    $reportedVersion = ((Invoke-ProvisioningNative -Role 'Android ADB version verification' -FilePath $adb `
            -ArgumentList @('version') -TimeoutSeconds 30) -join [Environment]::NewLine).Trim()
    $help = ((Invoke-ProvisioningNative -Role 'Android wireless ADB command verification' -FilePath $adb `
            -ArgumentList @('help') -TimeoutSeconds 30) -join [Environment]::NewLine)
    if ($reportedVersion -notmatch ('(?m)^Version ' + [regex]::Escape($version) + '-') -or
        $help -notmatch '(?m)^\s*pair HOST\[:PORT\]' -or
        $help -notmatch '(?m)^\s*connect HOST\[:PORT\]') {
        throw 'Android wireless ADB verification failed.'
    }
    return $version
}

function Install-AndroidStack {
    [CmdletBinding()]
    param()

    $androidCLIRevision = '22.0'
    $androidCLIVersion = '1.0.15985488'
    $androidCLIMetadata = [pscustomobject]@{
        Id = 'Google.AndroidCommandLineTools'
        Version = $androidCLIRevision
        Architecture = 'x64'
        InstallerType = 'zip'
        Scope = ''
        Url = 'https://dl.google.com/android/repository/commandlinetools-win-15859902_latest.zip'
        Sha256 = '90AE805D20434428BFFCB699C290860F19BB5F66A67E6B330067E3DE801FB04A'
        PayloadName = 'payload.zip'
    }
    $jdkVersion = '17.0.20'
    $jdkBuild = '8'
    $jdkMetadata = [pscustomobject]@{
        Id = 'Microsoft.OpenJDK.17.Android'
        Version = "$jdkVersion.$jdkBuild"
        Architecture = 'x64'
        InstallerType = 'zip'
        Scope = ''
        Url = 'https://aka.ms/download-jdk/microsoft-jdk-17.0.20-windows-x64.zip'
        Sha256 = 'E46FD292317C6BB0A8FE9DC63115021329F3A63CAEBA791C185F89F3666A68E5'
        PayloadName = 'payload.zip'
    }
    foreach ($metadata in @($androidCLIMetadata, $jdkMetadata)) {
        $uri = [Uri][string]$metadata.Url
        if ($uri.Scheme -cne 'https' -or [string]$metadata.Sha256 -cnotmatch '^[A-F0-9]{64}$' -or
            [string]$metadata.Architecture -cne 'x64' -or [string]$metadata.InstallerType -cne 'zip') {
            throw "Android stack package metadata is invalid: $($metadata.Id)"
        }
    }
    $androidCLIURI = [Uri][string]$androidCLIMetadata.Url
    $jdkURI = [Uri][string]$jdkMetadata.Url
    if ($androidCLIURI.Host -cne 'dl.google.com' -or $jdkURI.Host -cne 'aka.ms') {
        throw 'Android stack package host is invalid.'
    }

    $androidSDK = 'C:\HerdrSandbox\tools\android-sdk'
    $androidCLIRoot = Join-Path $androidSDK 'cmdline-tools\latest'
    $androidCLIBin = Join-Path $androidCLIRoot 'bin'
    $androidCLI = Join-Path $androidCLIBin 'android.exe'
    $platformTools = Join-Path $androidSDK 'platform-tools'
    $adb = Join-Path $platformTools 'adb.exe'
    $jdkRoot = 'C:\HerdrSandbox\toolchains\android-jdk-17'
    $androidUserHome = 'C:\HerdrSandbox\build\android-user'
    Write-Output "Installing Android SDK Command-line Tools $androidCLIRevision and Microsoft OpenJDK $jdkVersion..."
    Install-StackAndroidDirectArchive -Role 'Android SDK Command-line Tools' -Metadata $androidCLIMetadata `
        -Destination $androidCLIRoot -ArchiveRoot 'cmdline-tools' -ExpectedEntryCount 330 `
        -RequiredRelativePaths @('source.properties', 'bin\android.exe', 'bin\sdkmanager.bat',
            'lib\sdk-common\tools.sdk-common.jar') -SignedRelativePath 'bin\android.exe'
    Install-StackAndroidDirectArchive -Role 'Android Microsoft OpenJDK 17' -Metadata $jdkMetadata `
        -Destination $jdkRoot -ArchiveRoot "jdk-$jdkVersion+$jdkBuild" -ExpectedEntryCount 576 `
        -RequiredRelativePaths @('release', 'bin\java.exe', 'bin\javac.exe', 'lib\modules')

    $sourceProperties = [IO.File]::ReadAllText((Join-Path $androidCLIRoot 'source.properties'))
    if ($sourceProperties -notmatch ('(?m)^Pkg\.Revision=' + [regex]::Escape($androidCLIRevision) + '\r?$') -or
        $sourceProperties -notmatch ('(?m)^Pkg\.Path=cmdline-tools;' + [regex]::Escape($androidCLIRevision) + '\r?$')) {
        throw 'Android SDK Command-line Tools source identity is unexpected.'
    }
    $jdkRelease = [IO.File]::ReadAllText((Join-Path $jdkRoot 'release'))
    if ($jdkRelease -notmatch ('(?m)^JAVA_VERSION="' + [regex]::Escape($jdkVersion) + '"\r?$') -or
        $jdkRelease -notmatch '(?m)^IMPLEMENTOR="Microsoft"\r?$' -or
        $jdkRelease -notmatch '(?m)^OS_ARCH="x86_64"\r?$') {
        throw 'Android Microsoft OpenJDK release identity is unexpected.'
    }
    Assert-StackAndroidMicrosoftSignature -Path (Join-Path $jdkRoot 'bin\java.exe')
    Assert-StackAndroidMicrosoftSignature -Path (Join-Path $jdkRoot 'bin\javac.exe')

    $androidJava = Join-Path $jdkRoot 'bin\java.exe'
    $androidJavac = Join-Path $jdkRoot 'bin\javac.exe'
    $androidJavaVersion = ((Invoke-ProvisioningNative -Role 'Android JDK runtime version verification' `
            -FilePath $androidJava -ArgumentList @('-version') -TimeoutSeconds 30) `
        -join [Environment]::NewLine).Trim()
    $androidJavacVersion = ((Invoke-ProvisioningNative -Role 'Android JDK compiler version verification' `
            -FilePath $androidJavac -ArgumentList @('-version') -TimeoutSeconds 30) `
        -join [Environment]::NewLine).Trim()
    if ($androidJavaVersion -notmatch ('(?m)^openjdk version "' + [regex]::Escape($jdkVersion) + '"(?:\s|$)') -or
        $androidJavaVersion -notmatch '(?i)Microsoft' -or $androidJavacVersion -cne "javac $jdkVersion") {
        throw "Android Microsoft OpenJDK version verification failed: java=$androidJavaVersion javac=$androidJavacVersion"
    }

    $env:ANDROID_HOME = $androidSDK
    $env:ANDROID_USER_HOME = $androidUserHome
    $env:ANDROID_JAVA_HOME = $jdkRoot
    foreach ($directory in @($androidSDK, $androidUserHome)) {
        New-Item -ItemType Directory -Path $directory -Force | Out-Null
    }
    $androidEnvironment = [ordered]@{
        ANDROID_HOME = $androidSDK
        ANDROID_USER_HOME = $androidUserHome
        ANDROID_JAVA_HOME = $jdkRoot
    }
    foreach ($entry in $androidEnvironment.GetEnumerator()) {
        [Environment]::SetEnvironmentVariable([string]$entry.Key, [string]$entry.Value, 'Machine')
        if ([Environment]::GetEnvironmentVariable([string]$entry.Key, 'Machine') -cne [string]$entry.Value) {
            throw "Android environment read-back failed: $($entry.Key)"
        }
    }

    $machineJavaHome = [string][Environment]::GetEnvironmentVariable('JAVA_HOME', 'Machine')
    if ([string]::IsNullOrWhiteSpace($machineJavaHome)) {
        $env:JAVA_HOME = $jdkRoot
        [Environment]::SetEnvironmentVariable('JAVA_HOME', $jdkRoot, 'Machine')
        if ([Environment]::GetEnvironmentVariable('JAVA_HOME', 'Machine') -cne $jdkRoot) {
            throw 'Android JAVA_HOME activation read-back failed.'
        }
        Add-ProvisioningMachinePath -Directory (Join-Path $jdkRoot 'bin')
    }

    $previousJavaHome = [string]$env:JAVA_HOME
    $platformToolsVersion = ''
    try {
        $env:JAVA_HOME = $jdkRoot
        $androidJVMWarning = 'OpenJDK 64-Bit Server VM warning: The UseAllWindowsProcessorGroups flag is not supported on this Windows version and will be ignored.'
        if (Test-Path -LiteralPath $platformTools -PathType Container) {
            $platformToolsVersion = Assert-StackAndroidPlatformTools -Root $platformTools
            Write-Output "Android Platform Tools already verified: $platformToolsVersion"
        } else {
            Invoke-ProvisioningNative -Role 'Android Platform Tools installation' -FilePath $androidCLI `
                -ArgumentList @('--no-metrics', "--sdk=$androidSDK", 'sdk', 'install', 'platform-tools') `
                -TimeoutSeconds 600 | Out-Null
        }
        $androidVersion = (@(Invoke-ProvisioningNative -Role 'Android CLI version verification' `
                    -FilePath $androidCLI -ArgumentList @('--no-metrics', '--version') -TimeoutSeconds 30 | `
                    Where-Object { ([string]$_).Trim() -cne $androidJVMWarning }) `
            -join [Environment]::NewLine).Trim()
        $reportedSDK = (@(Invoke-ProvisioningNative -Role 'Android SDK location verification' `
                    -FilePath $androidCLI -ArgumentList @('--no-metrics', "--sdk=$androidSDK", 'info', 'sdk') `
                    -TimeoutSeconds 30 | Where-Object { ([string]$_).Trim() -cne $androidJVMWarning }) `
            -join [Environment]::NewLine).Trim()
    } finally {
        $env:JAVA_HOME = $previousJavaHome
    }
    if ($androidVersion -cne $androidCLIVersion -or
        [IO.Path]::GetFullPath($reportedSDK).TrimEnd('\') -ine [IO.Path]::GetFullPath($androidSDK).TrimEnd('\')) {
        throw "Android SDK verification failed: cli=$androidVersion sdk=$reportedSDK"
    }
    if ([string]::IsNullOrWhiteSpace($platformToolsVersion)) {
        $platformToolsVersion = Assert-StackAndroidPlatformTools -Root $platformTools
    }
    Add-ProvisioningMachinePath -Directory $androidCLIBin
    Add-ProvisioningMachinePath -Directory $platformTools
    $androidCommands = [ordered]@{'android.exe' = $androidCLI; 'adb.exe' = $adb}
    foreach ($entry in $androidCommands.GetEnumerator()) {
        $resolved = Get-Command $entry.Key -CommandType Application -ErrorAction Stop | Select-Object -First 1
        if ([IO.Path]::GetFullPath([string]$resolved.Source) -ine [IO.Path]::GetFullPath([string]$entry.Value)) {
            throw "Android command PATH read-back failed: $($entry.Key)"
        }
    }
    Write-Output "Android ready: CLI $androidVersion, Platform Tools $platformToolsVersion, Microsoft OpenJDK $jdkVersion"
}

function Get-StackAudioGridderFiles {
    return [ordered]@{
        'bin\AudioGridderPluginTray.exe' = '681626482BC2A084D439D1D29F42E874DADF41A3DB342156B8B6F35F65CCC280'
        'bin\AudioGridderServer.exe' = '6C1A125EE26977DA22C95086B710C5DBF4667AA253EEEBBFB540EE8D4EACB52E'
        'bin\crashpad_handler.exe' = '1ED7B6EB8BE0ABC034A6630199A38A925395758D3B476160BC0A01BFE037D441'
        'lib\VST\AudioGridder.dll' = 'B349EAE03268F4D521D8741B0BA4A7366102D1BF2C7B2959962706FB37AC1C1A'
        'lib\VST\AudioGridderInst.dll' = 'B45F4F0FB66C82BD8575E993AF7277185A5C98C4D23FF1B0C4462A0B7FA6F3F9'
        'lib\VST\AudioGridderMidi.dll' = '68DAF126507A60598BA94C24322F441DBCA46B4C76632EBFCDB4F5CCFD5FE3E2'
        'lib\VST3\AudioGridder.vst3' = '59D8054E7C094178DE35ADE6F92EEE5ED256D7785739DCC795EDFC0F7839EBBF'
        'lib\VST3\AudioGridderInst.vst3' = 'EA89147EB0460852033076AC1C5DD9571B879936805BDFD29139D52D8BD13782'
        'lib\VST3\AudioGridderMidi.vst3' = '2D163D7B9E7D9CAE4D7B62FDB05473AD811C418897E573903B514DC04E7E6CCB'
    }
}

function Test-StackAudioGridderPayload {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Root
    )

    try {
        if (-not (Test-Path -LiteralPath $Root -PathType Container)) { return $false }
        $rootInfo = Get-Item -LiteralPath $Root -Force
        if (($rootInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { return $false }
        $expected = Get-StackAudioGridderFiles
        $files = @(Get-ChildItem -LiteralPath $Root -File -Recurse -Force)
        if (@($files | Where-Object {
                    ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0
                }).Count -ne 0) {
            return $false
        }
        $rootPath = [IO.Path]::GetFullPath($Root).TrimEnd('\')
        $actualNames = @($files | ForEach-Object {
                $_.FullName.Substring($rootPath.Length + 1)
            } | Sort-Object)
        $expectedNames = @($expected.Keys | Sort-Object)
        if (($actualNames -join '|') -cne ($expectedNames -join '|')) { return $false }
        foreach ($entry in $expected.GetEnumerator()) {
            $path = Join-Path $rootPath ([string]$entry.Key)
            $hash = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToUpperInvariant()
            if ($hash -cne [string]$entry.Value) { return $false }
        }
        $server = Get-Item -LiteralPath (Join-Path $rootPath 'bin\AudioGridderServer.exe') -Force
        $tray = Get-Item -LiteralPath (Join-Path $rootPath 'bin\AudioGridderPluginTray.exe') -Force
        return [string]$server.VersionInfo.FileVersion -ceq '1.2.0' -and
            [string]$server.VersionInfo.ProductName -ceq 'AudioGridderServer' -and
            [string]$tray.VersionInfo.FileVersion -ceq '1.2.0' -and
            [string]$tray.VersionInfo.ProductName -ceq 'AudioGridderPluginTray'
    } catch {
        return $false
    }
}

function Remove-StackAudioGridderRoot {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Root
    )

    $expectedRoot = 'C:\HerdrSandbox\tools\AudioGridder'
    if ([IO.Path]::GetFullPath($Root).TrimEnd('\') -ine $expectedRoot) {
        throw "AudioGridder root is not app-owned: $Root"
    }
    if (-not (Test-Path -LiteralPath $Root)) { return }
    $items = @((Get-Item -LiteralPath $Root -Force)) + @(Get-ChildItem -LiteralPath $Root -Recurse -Force)
    if (@($items | Where-Object {
                ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0
            }).Count -ne 0) {
        throw "AudioGridder root contains a reparse point: $Root"
    }
    Remove-Item -LiteralPath $Root -Recurse -Force
}

function Install-StackAudioGridderClientFiles {
    param(
        [Parameter(Mandatory = $true)]
        [string]$SourceRoot
    )

    $expected = Get-StackAudioGridderFiles
    $destinations = [ordered]@{
        'bin\AudioGridderPluginTray.exe' = 'C:\Program Files\AudioGridderPluginTray\AudioGridderPluginTray.exe'
        'bin\crashpad_handler.exe' = 'C:\Program Files\AudioGridderPluginTray\crashpad_handler.exe'
        'lib\VST\AudioGridder.dll' = 'C:\Program Files\VstPlugins\AudioGridder.dll'
        'lib\VST\AudioGridderInst.dll' = 'C:\Program Files\VstPlugins\AudioGridderInst.dll'
        'lib\VST\AudioGridderMidi.dll' = 'C:\Program Files\VstPlugins\AudioGridderMidi.dll'
        'lib\VST3\AudioGridder.vst3' = 'C:\Program Files\Common Files\VST3\AudioGridder.vst3'
        'lib\VST3\AudioGridderInst.vst3' = 'C:\Program Files\Common Files\VST3\AudioGridderInst.vst3'
        'lib\VST3\AudioGridderMidi.vst3' = 'C:\Program Files\Common Files\VST3\AudioGridderMidi.vst3'
    }
    foreach ($entry in $destinations.GetEnumerator()) {
        $source = Join-Path $SourceRoot ([string]$entry.Key)
        $destination = [string]$entry.Value
        $directory = Split-Path -Parent $destination
        New-Item -ItemType Directory -Path $directory -Force | Out-Null
        $directoryInfo = Get-Item -LiteralPath $directory -Force
        if (-not $directoryInfo.PSIsContainer -or
            ($directoryInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "AudioGridder plugin directory is unsafe: $directory"
        }
        if (Test-Path -LiteralPath $destination) {
            $destinationInfo = Get-Item -LiteralPath $destination -Force
            if ($destinationInfo.PSIsContainer -or
                ($destinationInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "AudioGridder plugin destination is unsafe: $destination"
            }
        }
        $expectedHash = [string]$expected[[string]$entry.Key]
        $actualHash = if (Test-Path -LiteralPath $destination -PathType Leaf) {
            (Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash.ToUpperInvariant()
        } else { '' }
        if ($actualHash -cne $expectedHash) {
            Copy-Item -LiteralPath $source -Destination $destination -Force
        }
        $installedHash = (Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash.ToUpperInvariant()
        if ($installedHash -cne $expectedHash) {
            throw "AudioGridder plugin copy verification failed: $destination"
        }
    }
}

function Get-StackAudioGridderNetwork {
    $configurations = @(Get-NetIPConfiguration -ErrorAction Stop | Where-Object {
            $null -ne $_.IPv4DefaultGateway -and [string]$_.NetAdapter.Status -ceq 'Up'
        })
    if ($configurations.Count -ne 1) {
        throw "AudioGridder expected one Windows Sandbox default IPv4 interface; found: $($configurations.InterfaceAlias -join ', ')"
    }
    $gateways = @(@($configurations[0].IPv4DefaultGateway) | ForEach-Object { [string]$_.NextHop } |
        Where-Object { -not [string]::IsNullOrWhiteSpace($_) -and $_ -cne '0.0.0.0' } | Sort-Object -Unique)
    $guestAddresses = @(@($configurations[0].IPv4Address) | ForEach-Object { [string]$_.IPAddress } |
        Where-Object { -not [string]::IsNullOrWhiteSpace($_) -and $_ -cne '0.0.0.0' } | Sort-Object -Unique)
    if ($gateways.Count -ne 1 -or $guestAddresses.Count -ne 1) {
        throw "AudioGridder expected one host gateway and guest IPv4 address; found gateways=$($gateways -join ', ') addresses=$($guestAddresses -join ', ')"
    }
    foreach ($value in @($gateways[0], $guestAddresses[0])) {
        $address = $null
        if (-not [Net.IPAddress]::TryParse($value, [ref]$address) -or
            $address.AddressFamily -ne [Net.Sockets.AddressFamily]::InterNetwork -or
            [Net.IPAddress]::IsLoopback($address)) {
            throw "AudioGridder network address is invalid: $value"
        }
        $octets = $address.GetAddressBytes()
        if ($octets[0] -eq 0 -or $octets[0] -ge 224 -or
            ($octets[0] -eq 169 -and $octets[1] -eq 254)) {
            throw "AudioGridder network address is not routable: $value"
        }
    }
    return [pscustomobject]@{ HostGateway = $gateways[0]; GuestAddress = $guestAddresses[0] }
}

function ConvertTo-StackAudioGridderJSONValue {
    param(
        [AllowNull()]
        [object]$Value
    )

    return ConvertTo-Json -InputObject $Value -Depth 20 -Compress
}

function Set-StackAudioGridderConfiguration {
    param(
        [Parameter(Mandatory = $true)]
        [ValidateSet('audiogridderplugin.cfg', 'audiogridderserver.cfg')]
        [string]$FileName,
        [Parameter(Mandatory = $true)]
        [Collections.IDictionary]$Values,
        [Parameter(Mandatory = $true)]
        [string[]]$BlockingProcesses,
        [Parameter(Mandatory = $true)]
        [string]$BlockingMessage
    )

    $configurationRoot = Join-Path $env:APPDATA 'AudioGridder'
    $configurationPath = Join-Path $configurationRoot $FileName
    New-Item -ItemType Directory -Path $configurationRoot -Force | Out-Null
    $rootInfo = Get-Item -LiteralPath $configurationRoot -Force
    if (-not $rootInfo.PSIsContainer -or
        ($rootInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "AudioGridder configuration root is unsafe: $configurationRoot"
    }
    $configuration = [pscustomobject]@{}
    if (Test-Path -LiteralPath $configurationPath) {
        $file = Get-Item -LiteralPath $configurationPath -Force
        if ($file.PSIsContainer -or ($file.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
            $file.Length -le 0 -or $file.Length -gt 1048576) {
            throw "AudioGridder configuration is unsafe: $configurationPath"
        }
        try {
            $configuration = [IO.File]::ReadAllText($configurationPath) | ConvertFrom-Json
        } catch {
            throw "AudioGridder configuration is invalid: $($_.Exception.Message)"
        }
        if ($configuration -isnot [pscustomobject]) {
            throw 'AudioGridder configuration must be one JSON object.'
        }
    }
    $matches = $true
    foreach ($entry in $Values.GetEnumerator()) {
        $property = $configuration.PSObject.Properties[[string]$entry.Key]
        if ($null -eq $property -or
            (ConvertTo-StackAudioGridderJSONValue -Value $property.Value) -cne
            (ConvertTo-StackAudioGridderJSONValue -Value $entry.Value)) {
            $matches = $false
            break
        }
    }
    if ($matches) {
        return
    }
    if (@(Get-Process -Name $BlockingProcesses -ErrorAction SilentlyContinue).Count -ne 0) {
        throw $BlockingMessage
    }
    foreach ($entry in $Values.GetEnumerator()) {
        $configuration | Add-Member -NotePropertyName ([string]$entry.Key) -NotePropertyValue $entry.Value -Force
    }
    $contents = ($configuration | ConvertTo-Json -Depth 20 -Compress) + [Environment]::NewLine
    $temporary = Join-Path $configurationRoot ('audiogridder-' + [Guid]::NewGuid().ToString('N') + '.tmp')
    $backup = ''
    try {
        [IO.File]::WriteAllText($temporary, $contents, (New-Object Text.UTF8Encoding($false)))
        if (Test-Path -LiteralPath $configurationPath) {
            $backup = Join-Path $configurationRoot ('audiogridder-' + [Guid]::NewGuid().ToString('N') + '.bak')
            [IO.File]::Replace($temporary, $configurationPath, $backup, $true)
        } else {
            [IO.File]::Move($temporary, $configurationPath)
        }
    } finally {
        if (Test-Path -LiteralPath $temporary) {
            Remove-Item -LiteralPath $temporary -Force
        }
        if (-not [string]::IsNullOrWhiteSpace($backup) -and (Test-Path -LiteralPath $backup)) {
            Remove-Item -LiteralPath $backup -Force
        }
    }
    $verified = [IO.File]::ReadAllText($configurationPath) | ConvertFrom-Json
    foreach ($entry in $Values.GetEnumerator()) {
        $property = $verified.PSObject.Properties[[string]$entry.Key]
        if ($null -eq $property -or
            (ConvertTo-StackAudioGridderJSONValue -Value $property.Value) -cne
            (ConvertTo-StackAudioGridderJSONValue -Value $entry.Value)) {
            throw "AudioGridder configuration read-back failed for $($entry.Key)."
        }
    }
}

function Set-StackAudioGridderServerConfiguration {
    $values = [ordered]@{
        ID = 0
        NAME = 'Herdr Sandbox'
        VST = $true
        VST3Folders = @('C:\Program Files\Common Files\VST3')
        VST2 = $true
        VST2Folders = @('C:\Program Files\VstPlugins')
        VSTNoStandardFolders = $true
        ScanForPlugins = $true
        Logger = $true
        CrashReporting = $false
        SandboxMode = 1
        ScreenLocalMode = $false
    }
    Set-StackAudioGridderConfiguration -FileName 'audiogridderserver.cfg' -Values $values `
        -BlockingProcesses @('AudioGridderServer') `
        -BlockingMessage 'Close AudioGridder Server before changing its server-0 configuration.'
}

function Set-StackAudioGridderClientConfiguration {
    $values = [ordered]@{
        Servers = @('127.0.0.1:0')
        LastServer = '127.0.0.1:0:::0:0:00000000-0000-0000-0000-000000000000'
    }
    Set-StackAudioGridderConfiguration -FileName 'audiogridderplugin.cfg' -Values $values `
        -BlockingProcesses @('reaper', 'AudioGridderPluginTray') `
        -BlockingMessage 'Close REAPER and AudioGridder Plugin Tray before changing the local test endpoint.'
}

function Test-StackAudioGridderFirewallRule {
    param(
        [Parameter(Mandatory = $true)]
        [AllowEmptyCollection()]
        [object[]]$Rules,
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [string]$Program,
        [Parameter(Mandatory = $true)]
        [string]$LocalPort,
        [Parameter(Mandatory = $true)]
        [string]$RemoteAddress
    )

    if ($Rules.Count -ne 1) { return $false }
    $candidate = $Rules[0]
    try {
        $application = @($candidate | Get-NetFirewallApplicationFilter -ErrorAction Stop)
        $address = @($candidate | Get-NetFirewallAddressFilter -ErrorAction Stop)
        $port = @($candidate | Get-NetFirewallPortFilter -ErrorAction Stop)
        $service = @($candidate | Get-NetFirewallServiceFilter -ErrorAction Stop)
        $interface = @($candidate | Get-NetFirewallInterfaceFilter -ErrorAction Stop)
        $interfaceType = @($candidate | Get-NetFirewallInterfaceTypeFilter -ErrorAction Stop)
        $security = @($candidate | Get-NetFirewallSecurityFilter -ErrorAction Stop)
        if ($application.Count -ne 1 -or $address.Count -ne 1 -or $port.Count -ne 1 -or
            $service.Count -ne 1 -or $interface.Count -ne 1 -or $interfaceType.Count -ne 1 -or
            $security.Count -ne 1) {
            return $false
        }
        $actualProgram = [IO.Path]::GetFullPath([string]$application[0].Program)
        $expectedProgram = [IO.Path]::GetFullPath($Program)
    } catch {
        return $false
    }
    return [string]$candidate.Name -ceq $Name -and [string]$candidate.DisplayName -ceq $Name -and
        [string]$candidate.Enabled -ceq 'True' -and [string]$candidate.Profile -ceq 'Any' -and
        [string]$candidate.Direction -ceq 'Inbound' -and [string]$candidate.Action -ceq 'Allow' -and
        [string]$candidate.EdgeTraversalPolicy -ceq 'Block' -and
        [string]$candidate.LooseSourceMapping -ceq 'False' -and
        [string]$candidate.LocalOnlyMapping -ceq 'False' -and
        [string]::IsNullOrEmpty([string]$candidate.Owner) -and
        [string]::Equals($actualProgram, $expectedProgram, [StringComparison]::OrdinalIgnoreCase) -and
        ([string]::IsNullOrEmpty([string]$application[0].Package) -or
            [string]$application[0].Package -ceq 'Any') -and
        (Test-StackFirewallValue -Value $address[0].LocalAddress -Expected 'Any') -and
        (Test-StackFirewallValue -Value $address[0].RemoteAddress -Expected $RemoteAddress) -and
        [string]$port[0].Protocol -ceq 'TCP' -and
        (Test-StackFirewallValue -Value $port[0].LocalPort -Expected $LocalPort) -and
        (Test-StackFirewallValue -Value $port[0].RemotePort -Expected 'Any') -and
        (Test-StackFirewallValue -Value $port[0].IcmpType -Expected 'Any') -and
        ([string]::IsNullOrEmpty([string]$port[0].DynamicTarget) -or
            [string]$port[0].DynamicTarget -ceq 'Any') -and
        [string]$service[0].Service -ceq 'Any' -and
        (Test-StackFirewallValue -Value $interface[0].InterfaceAlias -Expected 'Any') -and
        [string]$interfaceType[0].InterfaceType -ceq 'Any' -and
        [string]$security[0].Authentication -ceq 'NotRequired' -and
        [string]$security[0].Encryption -ceq 'NotRequired' -and
        [string]$security[0].OverrideBlockRules -ceq 'False' -and
        [string]$security[0].LocalUser -ceq 'Any' -and
        [string]$security[0].RemoteUser -ceq 'Any' -and
        [string]$security[0].RemoteMachine -ceq 'Any'
}

function Set-StackAudioGridderFirewallRule {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [string]$Program,
        [Parameter(Mandatory = $true)]
        [string]$LocalPort,
        [Parameter(Mandatory = $true)]
        [string]$RemoteAddress
    )

    $existing = @(Get-NetFirewallRule -Name $Name -ErrorAction SilentlyContinue)
    if (Test-StackAudioGridderFirewallRule -Rules $existing -Name $Name -Program $Program `
            -LocalPort $LocalPort -RemoteAddress $RemoteAddress) {
        return
    }
    if (@(Get-Process -Name 'AudioGridderServer' -ErrorAction SilentlyContinue).Count -ne 0) {
        throw 'Close AudioGridder Server before changing its guest firewall rules.'
    }
    if ($existing.Count -gt 0) {
        $existing | Remove-NetFirewallRule -ErrorAction Stop
    }
    New-NetFirewallRule -Name $Name -DisplayName $Name -Enabled True -Profile Any -Direction Inbound `
        -Action Allow -Program $Program -LocalAddress Any -RemoteAddress $RemoteAddress -Protocol TCP `
        -LocalPort $LocalPort -RemotePort Any -Service Any -InterfaceType Any | Out-Null
    $verified = @(Get-NetFirewallRule -Name $Name -ErrorAction Stop)
    if (-not (Test-StackAudioGridderFirewallRule -Rules $verified -Name $Name -Program $Program `
                -LocalPort $LocalPort -RemoteAddress $RemoteAddress)) {
        throw "AudioGridder firewall rule verification failed: $Name"
    }
}

function Install-AudioStack {
    [CmdletBinding()]
    param()

    $reaperVersion = Get-ProvisioningToolVersion -Tool 'Cockos.REAPER' -Requested '7.78'
    $audioGridderVersion = Get-ProvisioningToolVersion -Tool 'AudioGridder' -Requested '1.2.0'
    if ($reaperVersion -cne '7.78' -or $audioGridderVersion -cne '1.2.0') {
        throw "Audio stack version plan is unsupported: REAPER=$reaperVersion AudioGridder=$audioGridderVersion"
    }

    $reaperMetadata = Get-ProvisioningWinGetMetadata -Role 'REAPER' -Id 'Cockos.REAPER' `
        -Version $reaperVersion -Architecture 'x64' -InstallerType 'exe' -Scope 'machine' -PayloadExtension '.exe'
    $reaperURI = [Uri][string]$reaperMetadata.Url
    if ([string]$reaperMetadata.Id -cne 'Cockos.REAPER' -or
        [string]$reaperMetadata.Version -cne '7.78' -or
        [string]$reaperMetadata.Architecture -cne 'x64' -or
        [string]$reaperMetadata.InstallerType -cne 'exe' -or
        [string]$reaperMetadata.Scope -cne 'machine' -or
        [string]$reaperMetadata.Sha256 -cne 'E7AD77BDD572C35D205034F871181C7B4D9A4110798131B60ACD54CD44453947' -or
        $reaperURI.Scheme -cne 'https' -or $reaperURI.Host -cne 'www.reaper.fm' -or
        $reaperURI.AbsolutePath -cne '/files/7.x/reaper778_x64-install.exe') {
        throw 'REAPER metadata does not match the approved 7.78 x64 installer.'
    }
    Write-Output 'Installing REAPER 7.78...'
    Install-ProvisioningCachedPackage -Role 'REAPER' -Metadata $reaperMetadata -DownloadSource 'WinGet' `
        -Adapter 'Exe' -InstallerArguments @('/S') -InstallerSuccessExitCodes @(0, 1223) `
        -RequireAuthenticodeSignature
    $reaper = 'C:\Program Files\REAPER (x64)\reaper.exe'
    $reaperInfo = Get-Item -LiteralPath $reaper -Force -ErrorAction Stop
    if ($reaperInfo.PSIsContainer -or ($reaperInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
        [string]$reaperInfo.VersionInfo.FileVersion -cne '7.78') {
        throw "REAPER installation identity is invalid: $reaper"
    }
    Assert-ProvisioningAuthenticodeSignature -Role 'REAPER executable' -Path $reaper
    $reaperSignature = Get-AuthenticodeSignature -LiteralPath $reaper
    if ($reaperSignature.SignerCertificate.Subject -notmatch '(^|,\s*)O=Cockos Incorporated(,|$)') {
        throw 'REAPER executable signer is not Cockos Incorporated.'
    }
    Ensure-ProvisioningStartShortcut -DisplayName 'REAPER' -Executable $reaper

    $audioGridderMetadata = [pscustomobject]@{
        Id = 'AudioGridder'
        Version = '1.2.0'
        Architecture = 'x64'
        InstallerType = 'zip'
        Scope = ''
        Url = 'https://github.com/apohl79/audiogridder/releases/download/release_1_2_0/AudioGridder_1.2.0-Windows.zip'
        Sha256 = '97E1B9484257B4A6B4A33FCFD7906DDE737513E26AE756B2E42E9466B0637D7D'
        PayloadName = 'AudioGridder_1.2.0-Windows.zip'
    }
    $audioGridderURI = [Uri][string]$audioGridderMetadata.Url
    if ($audioGridderURI.Scheme -cne 'https' -or $audioGridderURI.Host -cne 'github.com' -or
        $audioGridderURI.AbsolutePath -cne '/apohl79/audiogridder/releases/download/release_1_2_0/AudioGridder_1.2.0-Windows.zip') {
        throw 'AudioGridder metadata does not match the official 1.2.0 Windows release.'
    }
    $audioGridderRoot = 'C:\HerdrSandbox\tools\AudioGridder'
    if ((Test-Path -LiteralPath $audioGridderRoot) -and
        -not (Test-StackAudioGridderPayload -Root $audioGridderRoot)) {
        Remove-StackAudioGridderRoot -Root $audioGridderRoot
    }
    Write-Output 'Installing AudioGridder 1.2.0 server and VST2/VST3 clients...'
    Install-ProvisioningCachedPackage -Role 'AudioGridder' -Metadata $audioGridderMetadata `
        -DownloadSource 'Direct' -Adapter 'Portable' -ExecutableName 'AudioGridderServer.exe' `
        -PortableVersionSource 'File'
    $aax = Join-Path $audioGridderRoot 'lib\AAX'
    if (Test-Path -LiteralPath $aax) {
        $unwantedItems = @((Get-Item -LiteralPath $aax -Force))
        if ($unwantedItems[0].PSIsContainer) {
            $unwantedItems += @(Get-ChildItem -LiteralPath $aax -Recurse -Force)
        }
        if (@($unwantedItems | Where-Object {
                    ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0
                }).Count -ne 0) {
            throw "AudioGridder AAX payload contains a reparse point: $aax"
        }
        Remove-Item -LiteralPath $aax -Recurse -Force
    }
    if (-not (Test-StackAudioGridderPayload -Root $audioGridderRoot)) {
        throw 'AudioGridder payload does not match the approved server and VST2/VST3 release.'
    }
    Install-StackAudioGridderClientFiles -SourceRoot $audioGridderRoot
    $server = Join-Path $audioGridderRoot 'bin\AudioGridderServer.exe'
    Ensure-ProvisioningStartShortcut -DisplayName 'AudioGridder Server' -Executable $server -ShortcutArguments '-id 0'
    Set-StackAudioGridderServerConfiguration
    Set-StackAudioGridderClientConfiguration
    $network = Get-StackAudioGridderNetwork
    Set-StackAudioGridderFirewallRule -Name 'HerdrSandbox-AudioGridder-Server0' -Program $server `
        -LocalPort '55056' -RemoteAddress $network.HostGateway
    Set-StackAudioGridderFirewallRule -Name 'HerdrSandbox-AudioGridder-Workers' -Program $server `
        -LocalPort '55088-56088' -RemoteAddress $network.HostGateway

    Write-Output "REAPER ready: $reaper"
    Write-Output "AudioGridder server 0 and local REAPER clients ready; start the server from the guest Start menu."
    Write-Output "Manual host client endpoint: $($network.GuestAddress):0"
    Write-Output 'Install production VST2/VST3 payloads through project or user provisioning in the guest.'
}

function Install-DotNetStack {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^10\.0\.(?:0|[1-9][0-9]*)$')]
        [string]$Version = ''
    )

    $packageID = 'Microsoft.DotNet.SDK.10'
    $Version = Get-ProvisioningToolVersion -Tool $packageID -Requested $Version
    $metadata = Get-ProvisioningWinGetMetadata -Role '.NET 10 SDK' -Id $packageID -Version $Version `
        -InstallerType 'burn'
    if ([string]$metadata.Id -cne $packageID -or [string]$metadata.Version -notmatch '^10\.0\.(?:0|[1-9][0-9]*)$') {
        throw ".NET SDK metadata is not the current modern .NET 10 family: $($metadata.Id) $($metadata.Version)"
    }
    $Version = [string]$metadata.Version
    Write-Output "Installing modern .NET SDK $Version..."
    Install-ProvisioningCachedPackage -Role '.NET 10 SDK' -Metadata $metadata -DownloadSource 'WinGet' `
        -Adapter 'Burn' -ExecutableName 'dotnet.exe' `
        -InstallerArguments @('/install', '/quiet', '/norestart') `
        -InstallerSuccessExitCodes @(0, 3010) -RequireAuthenticodeSignature
    $dotnetExecutable = 'C:\Program Files\dotnet\dotnet.exe'
    $dotnetExecutableItem = Get-Item -LiteralPath $dotnetExecutable -Force -ErrorAction Stop
    if (($dotnetExecutableItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
        $dotnetExecutableItem.PSIsContainer) {
        throw ".NET SDK executable is not one regular non-reparse file: $dotnetExecutable"
    }
    $verificationDirectory = Split-Path -Parent $dotnetExecutable
    Push-Location -LiteralPath $verificationDirectory
    try {
        $dotnetVersion = Assert-ProvisioningCommand -Role '.NET SDK' -Name $dotnetExecutable `
            -VersionArguments @('--version') -ExpectedPattern ('^' + [regex]::Escape($Version) + '$')
        $installedSDKs = @(Invoke-ProvisioningNative -Role '.NET SDK list verification' -FilePath $dotnetExecutable `
            -ArgumentList @('--list-sdks') | ForEach-Object { [string]$_ })
    } finally {
        Pop-Location
    }
    $sdkPattern = '^' + [regex]::Escape($Version) + ' \[C:\\Program Files\\dotnet\\sdk\]$'
    if (@($installedSDKs | Where-Object { $_ -match $sdkPattern }).Count -ne 1) {
        throw ".NET SDK $Version was not present exactly once in dotnet --list-sdks."
    }
    Write-Output ".NET SDK ready: $dotnetVersion"
}

function Get-StackJavaInstalledVersions {
    param(
        [Parameter(Mandatory = $true)]
        [object]$Metadata
    )

    $result = Invoke-ProvisioningNativeResult -Role 'Microsoft OpenJDK 25 installed-version inspection' `
        -FilePath 'winget.exe' -ArgumentList @(
            'list', '--id', [string]$Metadata.Id, '--exact', '--source', 'winget',
            '--scope', 'machine', '--accept-source-agreements', '--disable-interactivity'
        ) -TimeoutSeconds 120
    if ($result.ExitCode -ne 0) {
        return @()
    }
    $pattern = '(?:^|\s)' + [regex]::Escape([string]$Metadata.Id) + '\s+(?<version>\S+)(?:\s|$)'
    $versions = @()
    foreach ($line in @(ConvertFrom-ProvisioningNativeOutput -Text ([string]$result.Output))) {
        $match = [regex]::Match([string]$line, $pattern, [Text.RegularExpressions.RegexOptions]::IgnoreCase)
        if ($match.Success) {
            $versions += $match.Groups['version'].Value
        }
    }
    return @($versions)
}

function Remove-StackJavaPreviousInstallation {
    param(
        [Parameter(Mandatory = $true)]
        [object]$Metadata
    )

    $installedVersions = @(Get-StackJavaInstalledVersions -Metadata $Metadata)
    if ($installedVersions.Count -eq 0 -or
        ($installedVersions.Count -eq 1 -and $installedVersions[0] -ceq [string]$Metadata.Version)) {
        return
    }
    Write-Output "Replacing previous Microsoft OpenJDK 25 versions: $($installedVersions -join ', ')"
    Invoke-ProvisioningNative -Role 'Microsoft OpenJDK 25 previous-version uninstall' `
        -FilePath 'winget.exe' -ArgumentList @(
            'uninstall', '--id', [string]$Metadata.Id, '--exact', '--source', 'winget',
            '--scope', 'machine', '--all-versions', '--silent', '--accept-source-agreements',
            '--disable-interactivity'
        ) -TimeoutSeconds 600 | Out-Null
    $remainingVersions = @(Get-StackJavaInstalledVersions -Metadata $Metadata)
    if ($remainingVersions.Count -ne 0) {
        throw "Microsoft OpenJDK 25 previous-version uninstall left installed versions: $($remainingVersions -join ', ')"
    }
}

function Install-JavaStack {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^25\.0\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$')]
        [string]$Version = ''
    )

    $packageID = 'Microsoft.OpenJDK.25'
    $Version = Get-ProvisioningToolVersion -Tool $packageID -Requested $Version
    $metadata = Get-ProvisioningWinGetMetadata -Role 'Microsoft OpenJDK 25' -Id $packageID `
        -Version $Version -InstallerType 'wix' -Scope 'machine'
    if ([string]$metadata.Id -cne $packageID -or
        [string]$metadata.Version -notmatch '^25\.0\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$') {
        throw "Java metadata is not the Microsoft OpenJDK 25 LTS family: $($metadata.Id) $($metadata.Version)"
    }
    $Version = [string]$metadata.Version
    $versionMatch = [regex]::Match($Version, '^(?<language>25\.0\.[0-9]+)\.(?<build>[0-9]+)$')
    if (-not $versionMatch.Success) {
        throw "Java package version cannot be mapped to its language version: $Version"
    }
    $languageVersion = $versionMatch.Groups['language'].Value

    Write-Output "Installing Microsoft OpenJDK $Version..."
    Remove-StackJavaPreviousInstallation -Metadata $metadata
    Install-ProvisioningCachedPackage -Role 'Microsoft OpenJDK 25' -Metadata $metadata `
        -DownloadSource 'WinGet' -Adapter 'MSI' -ExecutableName 'java.exe' `
        -InstallerArguments @('ADDLOCAL=FeatureMain,FeatureEnvironment,FeatureJavaHome') `
        -RequireAuthenticodeSignature
    Update-ProvisioningPath

    $javaHomeValue = [string][Environment]::GetEnvironmentVariable('JAVA_HOME', 'Machine')
    if ([string]::IsNullOrWhiteSpace($javaHomeValue) -or -not [IO.Path]::IsPathRooted($javaHomeValue)) {
        throw "Microsoft OpenJDK did not publish an absolute machine JAVA_HOME: $javaHomeValue"
    }
    $javaHome = [IO.Path]::GetFullPath($javaHomeValue).TrimEnd('\')
    $microsoftRoot = [IO.Path]::GetFullPath((Join-Path $env:ProgramFiles 'Microsoft')).TrimEnd('\')
    if (-not $javaHome.StartsWith($microsoftRoot + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw "Microsoft OpenJDK JAVA_HOME is outside the expected publisher root: $javaHome"
    }
    $javaBin = Join-Path $javaHome 'bin'
    foreach ($directory in @($javaHome, $javaBin)) {
        if (-not (Test-Path -LiteralPath $directory -PathType Container)) {
            throw "Microsoft OpenJDK directory is missing: $directory"
        }
        $item = Get-Item -LiteralPath $directory -Force
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Microsoft OpenJDK directory is a reparse point: $directory"
        }
    }
    $commands = [ordered]@{
        'java.exe' = Join-Path $javaBin 'java.exe'
        'javac.exe' = Join-Path $javaBin 'javac.exe'
    }
    foreach ($entry in $commands.GetEnumerator()) {
        if (-not (Test-Path -LiteralPath $entry.Value -PathType Leaf)) {
            throw "Microsoft OpenJDK command is missing: $($entry.Value)"
        }
        $item = Get-Item -LiteralPath $entry.Value -Force
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Microsoft OpenJDK command is a reparse point: $($entry.Value)"
        }
    }
    $env:JAVA_HOME = $javaHome
    [Environment]::SetEnvironmentVariable('JAVA_HOME', $javaHome, 'Machine')
    if ([Environment]::GetEnvironmentVariable('JAVA_HOME', 'Machine') -cne $javaHome) {
        throw 'Microsoft OpenJDK JAVA_HOME read-back failed.'
    }
    Add-ProvisioningMachinePath -Directory $javaBin
    foreach ($entry in $commands.GetEnumerator()) {
        $resolved = Get-Command $entry.Key -CommandType Application -ErrorAction Stop | Select-Object -First 1
        if ([IO.Path]::GetFullPath([string]$resolved.Source) -ine [IO.Path]::GetFullPath([string]$entry.Value)) {
            throw "Microsoft OpenJDK command PATH read-back failed: $($entry.Key)"
        }
    }

    $javaVersion = ((Invoke-ProvisioningNative -Role 'Java runtime version verification' `
            -FilePath $commands['java.exe'] -ArgumentList @('-version') -TimeoutSeconds 30) `
        -join [Environment]::NewLine).Trim()
    $javacVersion = ((Invoke-ProvisioningNative -Role 'Java compiler version verification' `
            -FilePath $commands['javac.exe'] -ArgumentList @('-version') -TimeoutSeconds 30) `
        -join [Environment]::NewLine).Trim()
    if ($javaVersion -notmatch ('(?m)^openjdk version "' + [regex]::Escape($languageVersion) + '"(?:\s|$)') -or
        $javaVersion -notmatch '(?i)Microsoft' -or $javacVersion -cne "javac $languageVersion") {
        throw "Microsoft OpenJDK version verification failed: java=$javaVersion javac=$javacVersion"
    }

    $stage = Join-Path 'C:\HerdrSandbox\staging' ('java-stack-probe-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $stage -Force | Out-Null
    try {
        $source = Join-Path $stage 'HerdrJavaStackProbe.java'
        $program = @'
public final class HerdrJavaStackProbe {
    public static void main(String[] args) {
        System.out.println("java-stack-ok");
    }
}
'@
        [IO.File]::WriteAllText($source, $program + "`n", (New-Object Text.UTF8Encoding($false)))
        Invoke-ProvisioningNative -Role 'Java compiler probe' -FilePath $commands['javac.exe'] `
            -ArgumentList @('-d', $stage, $source) -WorkingDirectory $stage -TimeoutSeconds 120 | Out-Null
        $output = ((Invoke-ProvisioningNative -Role 'Java runtime probe' -FilePath $commands['java.exe'] `
                -ArgumentList @('-cp', $stage, 'HerdrJavaStackProbe') -WorkingDirectory $stage `
                -TimeoutSeconds 30) -join [Environment]::NewLine).Trim()
        if ($output -cne 'java-stack-ok') {
            throw "Java compile and run verification failed: $output"
        }
    } finally {
        if (Test-Path -LiteralPath $stage) {
            Remove-Item -LiteralPath $stage -Recurse -Force
        }
    }
    Write-Output "Java ready: Microsoft OpenJDK $Version"
}

function Install-NSISStack {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*))?$')]
        [string]$Version = ''
    )

    $packageID = 'NSIS.NSIS'
    $Version = Get-ProvisioningToolVersion -Tool $packageID -Requested $Version
    $metadata = Get-ProvisioningWinGetMetadata -Role 'NSIS' -Id $packageID -Version $Version `
        -Architecture 'x86' -InstallerType 'nullsoft' -Scope 'machine' -PayloadExtension '.exe'
    if ([string]$metadata.Id -cne $packageID -or [string]$metadata.Architecture -cne 'x86' -or
        [string]$metadata.InstallerType -cne 'nullsoft' -or [string]$metadata.Scope -cne 'machine' -or
        [string]$metadata.Version -notmatch '^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*))?$') {
        throw "NSIS metadata is unsupported: $($metadata.Id) $($metadata.Version) $($metadata.Architecture)"
    }
    $Version = [string]$metadata.Version

    Write-Output "Installing NSIS $Version..."
    Install-ProvisioningCachedPackage -Role 'NSIS' -Metadata $metadata -DownloadSource 'WinGet' `
        -Adapter 'NSIS'

    $installRoot = Join-Path ${env:ProgramFiles(x86)} 'NSIS'
    $compiler = Join-Path $installRoot 'makensis.exe'
    foreach ($path in @($installRoot, $compiler)) {
        if (-not (Test-Path -LiteralPath $path)) {
            throw "NSIS installed path is missing: $path"
        }
        $item = Get-Item -LiteralPath $path -Force
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
            ($path -ieq $installRoot -and -not $item.PSIsContainer) -or
            ($path -ieq $compiler -and $item.PSIsContainer)) {
            throw "NSIS installed path is unsafe: $path"
        }
    }
    Add-ProvisioningMachinePath -Directory $installRoot
    $resolvedCompiler = Get-Command 'makensis.exe' -CommandType Application -ErrorAction Stop |
        Select-Object -First 1
    if ([IO.Path]::GetFullPath([string]$resolvedCompiler.Source) -ine [IO.Path]::GetFullPath($compiler)) {
        throw "NSIS compiler PATH read-back failed: $($resolvedCompiler.Source)"
    }
    $compilerVersion = ((Invoke-ProvisioningNative -Role 'NSIS compiler version verification' `
            -FilePath $compiler -ArgumentList @('/VERSION') -TimeoutSeconds 30) `
        -join [Environment]::NewLine).Trim()
    if ($compilerVersion -cne "v$Version") {
        throw "NSIS compiler version is unexpected: $compilerVersion"
    }

    $stage = Join-Path 'C:\HerdrSandbox\staging' ('nsis-stack-probe-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $stage -Force | Out-Null
    try {
        $scriptPath = Join-Path $stage 'probe.nsi'
        $outputPath = Join-Path $stage 'probe.exe'
        $script = @'
Unicode true
Name "Herdr NSIS Stack Probe"
OutFile "probe.exe"
RequestExecutionLevel user
SilentInstall silent
Section "Probe"
  SetOutPath "$TEMP"
SectionEnd
'@
        [IO.File]::WriteAllText($scriptPath, $script + "`n", (New-Object Text.UTF8Encoding($false)))
        Invoke-ProvisioningNative -Role 'NSIS compiler probe' -FilePath $compiler `
            -ArgumentList @('/WX', '/V2', '/NOCONFIG', $scriptPath) -WorkingDirectory $stage `
            -TimeoutSeconds 120 | Out-Null
        if (-not (Test-Path -LiteralPath $outputPath -PathType Leaf)) {
            throw 'NSIS compiler probe did not create an installer.'
        }
        $output = Get-Item -LiteralPath $outputPath -Force
        $bytes = [IO.File]::ReadAllBytes($outputPath)
        if (($output.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
            $output.Length -lt 1024 -or $bytes.Length -lt 2 -or $bytes[0] -ne 0x4d -or $bytes[1] -ne 0x5a) {
            throw 'NSIS compiler probe produced an invalid Windows executable.'
        }
    } finally {
        if (Test-Path -LiteralPath $stage) {
            Remove-Item -LiteralPath $stage -Recurse -Force
        }
    }
    Write-Output "NSIS ready: $compilerVersion"
}

function Install-NushellStack {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$')]
        [string]$Version = ''
    )

    $packageID = 'Nushell.Nushell'
    $Version = Get-ProvisioningToolVersion -Tool $packageID -Requested $Version
    $metadata = Get-ProvisioningWinGetMetadata -Role 'Nushell' -Id $packageID -Version $Version `
        -Architecture 'x64' -InstallerType 'wix' -Scope 'machine'
    if ([string]$metadata.Id -cne $packageID -or [string]$metadata.Architecture -cne 'x64' -or
        [string]$metadata.InstallerType -cne 'wix' -or [string]$metadata.Scope -cne 'machine' -or
        [string]$metadata.Version -notmatch '^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$') {
        throw "Nushell metadata is unsupported: $($metadata.Id) $($metadata.Version) $($metadata.Architecture)"
    }
    $Version = [string]$metadata.Version

    Write-Output "Installing Nushell $Version..."
    Install-ProvisioningCachedPackage -Role 'Nushell' -Metadata $metadata -DownloadSource 'WinGet' `
        -Adapter 'MSI' -ExecutableName 'nu.exe' -InstallerArguments @('ALLUSERS=1')

    $expectedCommand = Join-Path $env:ProgramFiles 'nu\bin\nu.exe'
    if (-not (Test-Path -LiteralPath $expectedCommand -PathType Leaf) -or
        ((Get-Item -LiteralPath $expectedCommand -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Nushell command is missing or unsafe: $expectedCommand"
    }
    $resolvedCommand = Get-Command 'nu.exe' -CommandType Application -ErrorAction Stop |
        Select-Object -First 1
    if ([IO.Path]::GetFullPath([string]$resolvedCommand.Source) -ine [IO.Path]::GetFullPath($expectedCommand)) {
        throw "Nushell command resolved from an unexpected path: $($resolvedCommand.Source)"
    }
    $nuVersion = ((Invoke-ProvisioningNative -Role 'Nushell version verification' `
            -FilePath $expectedCommand -ArgumentList @('--version') -TimeoutSeconds 30) `
        -join [Environment]::NewLine).Trim()
    if ($nuVersion -cne $Version) {
        throw "Nushell version output is unexpected: $nuVersion"
    }
    Write-Output "Nushell ready: $nuVersion"
}

function Install-GoStack {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$')]
        [string]$Version = ''
    )

    $Version = Get-ProvisioningToolVersion -Tool 'GoLang.Go' -Requested $Version
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

function Install-NodeRuntime {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^\d+\.\d+\.\d+$')]
        [string]$Version = ''
    )

    $Version = Get-ProvisioningToolVersion -Tool 'OpenJS.NodeJS.LTS' -Requested $Version
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

function Get-StackNodeTools {
    $node = Get-Command 'node.exe' -CommandType Application -ErrorAction SilentlyContinue |
        Select-Object -First 1
    if ($null -eq $node) {
        throw 'Node.js runtime is unavailable.'
    }
    $npmCLI = Join-Path (Split-Path -Parent $node.Source) 'node_modules\npm\bin\npm-cli.js'
    if (-not (Test-Path -LiteralPath $npmCLI -PathType Leaf) -or
        ((Get-Item -LiteralPath $npmCLI -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Node.js npm CLI is missing or unsafe: $npmCLI"
    }
    return [pscustomobject]@{ Node = [string]$node.Source; NpmCLI = $npmCLI }
}

function Install-PlaywrightChromium {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$')]
        [string]$Version = ''
    )

    $Version = Get-ProvisioningToolVersion -Tool 'playwright' -Requested $Version
    $nodeTools = Get-StackNodeTools
    $node = $nodeTools.Node
    $npmCLI = $nodeTools.NpmCLI

    $playwrightRoot = 'C:\HerdrSandbox\tools\playwright'
    $browserRoot = 'C:\HerdrSandbox\tools\playwright-browsers'
    $npmCache = 'C:\HerdrSandbox\tools\npm-cache'
    $stagingRoot = 'C:\HerdrSandbox\staging'
    foreach ($directory in @('C:\HerdrSandbox\tools', $playwrightRoot, $browserRoot, $npmCache, $stagingRoot)) {
        if (-not (Test-Path -LiteralPath $directory)) {
            New-Item -ItemType Directory -Path $directory -Force | Out-Null
        }
        $directoryInfo = Get-Item -LiteralPath $directory -Force
        if (-not $directoryInfo.PSIsContainer -or
            ($directoryInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Playwright guest-local directory is unsafe: $directory"
        }
    }
    $env:npm_config_cache = $npmCache
    $env:npm_config_update_notifier = 'false'

    if ([string]::IsNullOrWhiteSpace($Version)) {
        $versionJSON = ((Invoke-ProvisioningNative -Role 'Playwright latest version resolution' `
            -FilePath $node -ArgumentList @($npmCLI, 'view', 'playwright@latest', 'version', '--json')) `
            -join [Environment]::NewLine).Trim()
        try {
            $resolvedVersion = $versionJSON | ConvertFrom-Json
        } catch {
            throw "Playwright latest version resolution returned invalid JSON: $($_.Exception.Message)"
        }
        if ($resolvedVersion -isnot [string] -or
            [string]$resolvedVersion -notmatch '^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$') {
            throw "Playwright latest version resolution returned an invalid stable version: $resolvedVersion"
        }
        $Version = [string]$resolvedVersion
        $null = Get-ProvisioningToolVersion -Tool 'playwright' -Requested $Version
    }

    $toolRoot = Join-Path $playwrightRoot $Version
    if (-not (Test-Path -LiteralPath $toolRoot)) {
        New-Item -ItemType Directory -Path $toolRoot -Force | Out-Null
    }
    $toolRootInfo = Get-Item -LiteralPath $toolRoot -Force
    if (-not $toolRootInfo.PSIsContainer -or
        ($toolRootInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Playwright versioned tool directory is unsafe: $toolRoot"
    }

    Write-Output "Installing Playwright $Version and Chromium..."
    Invoke-ProvisioningNative -Role 'Playwright CLI installation' -FilePath $node -ArgumentList @(
        $npmCLI,
        'install',
        '--prefix', $toolRoot,
        '--ignore-scripts',
        '--omit=optional',
        '--no-bin-links',
        '--no-audit',
        '--no-fund',
        '--no-save',
        '--package-lock=false',
        "playwright@$Version"
    ) | Out-Null

    $playwrightDirectory = Join-Path $toolRoot 'node_modules\playwright'
    $playwrightCoreDirectory = Join-Path $toolRoot 'node_modules\playwright-core'
    foreach ($directory in @($playwrightDirectory, $playwrightCoreDirectory)) {
        if (-not (Test-Path -LiteralPath $directory -PathType Container) -or
            ((Get-Item -LiteralPath $directory -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Playwright package directory is missing or unsafe: $directory"
        }
    }
    if (Test-Path -LiteralPath (Join-Path $toolRoot 'node_modules\fsevents')) {
        throw 'Playwright installed the unsupported optional fsevents package on Windows.'
    }

    $playwrightPackagePath = Join-Path $playwrightDirectory 'package.json'
    $playwrightCorePackagePath = Join-Path $playwrightCoreDirectory 'package.json'
    $playwrightCLI = Join-Path $playwrightDirectory 'cli.js'
    foreach ($file in @($playwrightPackagePath, $playwrightCorePackagePath, $playwrightCLI)) {
        if (-not (Test-Path -LiteralPath $file -PathType Leaf) -or
            ((Get-Item -LiteralPath $file -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Playwright package file is missing or unsafe: $file"
        }
    }
    try {
        $playwrightPackage = [IO.File]::ReadAllText($playwrightPackagePath) | ConvertFrom-Json
        $playwrightCorePackage = [IO.File]::ReadAllText($playwrightCorePackagePath) | ConvertFrom-Json
    } catch {
        throw "Playwright package identity is unreadable: $($_.Exception.Message)"
    }
    if ([string]$playwrightPackage.name -cne 'playwright' -or
        [string]$playwrightPackage.version -cne $Version -or
        [string]$playwrightPackage.dependencies.'playwright-core' -cne $Version -or
        [string]$playwrightCorePackage.name -cne 'playwright-core' -or
        [string]$playwrightCorePackage.version -cne $Version) {
        throw "Playwright package identity does not match exact version $Version."
    }
    $cliVersion = ((Invoke-ProvisioningNative -Role 'Playwright CLI version check' `
        -FilePath $node -ArgumentList @($playwrightCLI, '--version')) -join [Environment]::NewLine).Trim()
    if ($cliVersion -cne "Version $Version") {
        throw "Playwright CLI version output is unexpected: $cliVersion"
    }

    $env:PLAYWRIGHT_BROWSERS_PATH = $browserRoot
    $env:PLAYWRIGHT_DOWNLOAD_CONNECTION_TIMEOUT = '120000'
    [Environment]::SetEnvironmentVariable('PLAYWRIGHT_BROWSERS_PATH', $browserRoot, 'Machine')
    if ([Environment]::GetEnvironmentVariable('PLAYWRIGHT_BROWSERS_PATH', 'Machine') -cne $browserRoot) {
        throw 'Playwright browser path machine environment verification failed.'
    }
    Invoke-ProvisioningNative -Role 'Playwright Chromium installation' -FilePath $node `
        -ArgumentList @($playwrightCLI, 'install', 'chromium') | Out-Null

    $screenshotPath = Join-Path $stagingRoot ("playwright-chromium-$([Guid]::NewGuid().ToString('N')).png")
    try {
        Invoke-ProvisioningNative -Role 'Playwright Chromium headless launch' -FilePath $node `
            -ArgumentList @($playwrightCLI, 'screenshot', '-b', 'chromium', 'about:blank', $screenshotPath) `
            -TimeoutSeconds 120 | Out-Null
        if (-not (Test-Path -LiteralPath $screenshotPath -PathType Leaf)) {
            throw 'Playwright Chromium headless launch did not create its screenshot.'
        }
        $screenshotInfo = Get-Item -LiteralPath $screenshotPath -Force
        $screenshotBytes = [IO.File]::ReadAllBytes($screenshotPath)
        if (($screenshotInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
            $screenshotBytes.Length -lt 8 -or
            (($screenshotBytes[0..7] -join ',') -cne '137,80,78,71,13,10,26,10')) {
            throw 'Playwright Chromium headless launch returned an invalid PNG screenshot.'
        }
    } finally {
        if (Test-Path -LiteralPath $screenshotPath -PathType Leaf) {
            Remove-Item -LiteralPath $screenshotPath -Force
        }
    }
    Write-Output "Playwright Chromium ready: $Version"
}

function Install-NodeStack {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^\d+\.\d+\.\d+$')]
        [string]$Version = '',
        [ValidatePattern('^$|^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$')]
        [string]$PlaywrightVersion = ''
    )

    Install-NodeRuntime -Version $Version
    Install-PlaywrightChromium -Version $PlaywrightVersion
}

function Install-PlaywrightCLIStack {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^\d+\.\d+\.\d+$')]
        [string]$NodeVersion = '',
        [ValidateSet('0.1.17')]
        [string]$Version = '0.1.17'
    )

    $Version = Get-ProvisioningToolVersion -Tool '@playwright/cli' -Requested $Version
    Install-NodeRuntime -Version $NodeVersion
    $nodeTools = Get-StackNodeTools
    $node = $nodeTools.Node
    $npmCLI = $nodeTools.NpmCLI
    $playwrightVersion = '1.62.0-alpha-1783623505000'

    $cliRoot = 'C:\HerdrSandbox\tools\playwright-cli'
    $toolRoot = Join-Path $cliRoot $Version
    $npmCache = 'C:\HerdrSandbox\tools\npm-cache'
    foreach ($directory in @('C:\HerdrSandbox\tools', $cliRoot, $toolRoot, $npmCache)) {
        if (-not (Test-Path -LiteralPath $directory)) {
            New-Item -ItemType Directory -Path $directory -Force | Out-Null
        }
        $directoryInfo = Get-Item -LiteralPath $directory -Force
        if (-not $directoryInfo.PSIsContainer -or
            ($directoryInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Playwright CLI guest-local directory is unsafe: $directory"
        }
    }
    $env:npm_config_cache = $npmCache
    $env:npm_config_update_notifier = 'false'
    $env:NO_UPDATE_NOTIFIER = '1'
    [Environment]::SetEnvironmentVariable('NO_UPDATE_NOTIFIER', '1', 'Machine')
    if ([Environment]::GetEnvironmentVariable('NO_UPDATE_NOTIFIER', 'Machine') -cne '1') {
        throw 'Playwright CLI update-notifier environment was not persisted.'
    }

    Write-Output "Installing Playwright CLI $Version without browser binaries..."
    $previousBrowserDownload = $env:PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD
    try {
        $env:PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD = '1'
        Invoke-ProvisioningNative -Role 'Playwright CLI package installation' -FilePath $node -ArgumentList @(
            $npmCLI,
            'install',
            '--global',
            '--prefix', $toolRoot,
            '--ignore-scripts',
            '--omit=optional',
            '--no-audit',
            '--no-fund',
            '--package-lock=false',
            "@playwright/cli@$Version"
        ) | Out-Null
    } finally {
        if ($null -eq $previousBrowserDownload) {
            Remove-Item Env:\PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD -ErrorAction SilentlyContinue
        } else {
            $env:PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD = $previousBrowserDownload
        }
    }

    $cliDirectory = Join-Path $toolRoot 'node_modules\@playwright\cli'
    $cliDependencies = Join-Path $cliDirectory 'node_modules'
    $playwrightDirectory = Join-Path $cliDependencies 'playwright'
    $playwrightCoreDirectory = Join-Path $cliDependencies 'playwright-core'
    $packagePaths = @(
        (Join-Path $cliDirectory 'package.json'),
        (Join-Path $playwrightDirectory 'package.json'),
        (Join-Path $playwrightCoreDirectory 'package.json')
    )
    foreach ($path in $packagePaths) {
        if (-not (Test-Path -LiteralPath $path -PathType Leaf) -or
            ((Get-Item -LiteralPath $path -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Playwright CLI package identity is missing or unsafe: $path"
        }
    }
    try {
        $cliPackage = [IO.File]::ReadAllText($packagePaths[0]) | ConvertFrom-Json
        $playwrightPackage = [IO.File]::ReadAllText($packagePaths[1]) | ConvertFrom-Json
        $playwrightCorePackage = [IO.File]::ReadAllText($packagePaths[2]) | ConvertFrom-Json
    } catch {
        throw "Playwright CLI package identity is unreadable: $($_.Exception.Message)"
    }
    if ([string]$cliPackage.name -cne '@playwright/cli' -or
        [string]$cliPackage.version -cne $Version -or
        [string]$cliPackage.dependencies.playwright -cne $playwrightVersion -or
        [string]$cliPackage.dependencies.'playwright-core' -cne $playwrightVersion -or
        [string]$playwrightPackage.name -cne 'playwright' -or
        [string]$playwrightPackage.version -cne $playwrightVersion -or
        [string]$playwrightCorePackage.name -cne 'playwright-core' -or
        [string]$playwrightCorePackage.version -cne $playwrightVersion) {
        throw "Playwright CLI dependency identity does not match $Version."
    }
    if (Test-Path -LiteralPath (Join-Path $toolRoot 'node_modules\fsevents')) {
        throw 'Playwright CLI installed the unsupported optional fsevents package on Windows.'
    }

    $cliEntry = Join-Path $cliDirectory 'playwright-cli.js'
    $cliCommand = Join-Path $toolRoot 'playwright-cli.cmd'
    foreach ($path in @($cliEntry, $cliCommand)) {
        if (-not (Test-Path -LiteralPath $path -PathType Leaf) -or
            ((Get-Item -LiteralPath $path -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Playwright CLI command is missing or unsafe: $path"
        }
    }
    $powerShellShim = Join-Path $toolRoot 'playwright-cli.ps1'
    if (Test-Path -LiteralPath $powerShellShim) {
        $powerShellShimInfo = Get-Item -LiteralPath $powerShellShim -Force
        if ($powerShellShimInfo.PSIsContainer -or
            ($powerShellShimInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Playwright CLI PowerShell shim is unsafe: $powerShellShim"
        }
        Remove-Item -LiteralPath $powerShellShim -Force
    }
    Add-ProvisioningMachinePath -Directory $toolRoot
    $resolvedCLI = Wait-ProvisioningCommandAvailable -Role 'Playwright CLI command' -Name 'playwright-cli.cmd'
    if ([IO.Path]::GetFullPath($resolvedCLI) -ine [IO.Path]::GetFullPath($cliCommand)) {
        throw "Playwright CLI command resolved from an unexpected path: $resolvedCLI"
    }
    $cliVersion = ((Invoke-ProvisioningNative -Role 'Playwright CLI version check' -FilePath $node `
        -ArgumentList @($cliEntry, '--version')) -join [Environment]::NewLine).Trim()
    if ($cliVersion -cne $Version) {
        throw "Playwright CLI version output is unexpected: $cliVersion"
    }

    $extensionID = 'mmlmfjhmonkocbjadbfplnigmagldckm'
    $extensionUpdateURL = 'https://clients2.google.com/service/update2/crx'
    $externalExtensionKey = "HKLM:\SOFTWARE\Wow6432Node\Microsoft\Edge\Extensions\$extensionID"
    if (-not (Test-Path -LiteralPath $externalExtensionKey)) {
        New-Item -Path $externalExtensionKey -Force | Out-Null
    }
    New-ItemProperty -LiteralPath $externalExtensionKey -Name 'update_url' -PropertyType String `
        -Value $extensionUpdateURL -Force | Out-Null
    $registeredUpdateURL = [string](Get-ItemPropertyValue -LiteralPath $externalExtensionKey `
        -Name 'update_url' -ErrorAction Stop)
    if ($registeredUpdateURL -cne $extensionUpdateURL) {
        throw 'Playwright Extension registration verification failed.'
    }

    Write-Output "Playwright CLI ready: $Version"
    Write-Output 'Manual first use: open Edge, enable the registered Playwright Extension, copy its PLAYWRIGHT_MCP_EXTENSION_TOKEN value into the guest environment, then run playwright-cli.cmd -s=edge-main attach --extension=msedge.'
}

function Assert-StackHyperFramesSoftwareEncode {
    param(
        [Parameter(Mandatory = $true)][string]$FFmpeg,
        [Parameter(Mandatory = $true)][string]$FFprobe
    )

    $probeRoot = Join-Path 'C:\HerdrSandbox\staging' ('hyperframes-encode-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $probeRoot -Force | Out-Null
    try {
        $outputPath = Join-Path $probeRoot 'software-h264.mp4'
        Invoke-ProvisioningNative -Role 'HyperFrames libx264 software encode' -FilePath $FFmpeg `
            -ArgumentList @('-hide_banner', '-loglevel', 'error', '-f', 'lavfi', '-i',
                'color=c=black:s=64x64:r=1:d=1', '-frames:v', '1', '-c:v', 'libx264',
                '-pix_fmt', 'yuv420p', '-y', $outputPath) -TimeoutSeconds 60 | Out-Null
        if (-not (Test-Path -LiteralPath $outputPath -PathType Leaf)) {
            throw 'HyperFrames software encode did not create an MP4 file.'
        }
        $outputInfo = Get-Item -LiteralPath $outputPath -Force
        if (($outputInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or $outputInfo.Length -le 0) {
            throw 'HyperFrames software encode created an unsafe or empty MP4 file.'
        }
        $probeJSON = ((Invoke-ProvisioningNative -Role 'HyperFrames software encode probe' `
            -FilePath $FFprobe -ArgumentList @('-v', 'error', '-select_streams', 'v:0',
                '-show_entries', 'stream=codec_name,width,height,pix_fmt', '-of', 'json', $outputPath) `
            -TimeoutSeconds 30) -join [Environment]::NewLine).Trim()
        try {
            $probe = $probeJSON | ConvertFrom-Json
        } catch {
            throw "HyperFrames software encode probe returned invalid JSON: $($_.Exception.Message)"
        }
        $streams = @($probe.streams)
        if ($streams.Count -ne 1 -or [string]$streams[0].codec_name -cne 'h264' -or
            [int]$streams[0].width -ne 64 -or [int]$streams[0].height -ne 64 -or
            [string]$streams[0].pix_fmt -cne 'yuv420p') {
            throw "HyperFrames software encode identity is unexpected: $probeJSON"
        }
    } finally {
        if (Test-Path -LiteralPath $probeRoot) {
            Remove-Item -LiteralPath $probeRoot -Recurse -Force
        }
    }
}

function Assert-StackHyperFramesAgentSkills {
    param(
        [Parameter(Mandatory = $true)][object]$Report,
        [Parameter(Mandatory = $true)][Collections.IDictionary]$SkillRoots
    )

    $skills = @($Report.skills)
    if ($Report.updateAvailable -ne $false -or $null -eq $Report.summary -or
        [int]$Report.summary.outdated -ne 0 -or [int]$Report.summary.missing -ne 0 -or
        [int]$Report.summary.removed -ne 0 -or $skills.Count -le 0) {
        throw 'HyperFrames global skills are not current and complete.'
    }
    $skillNames = @($skills | ForEach-Object {
            if ([string]$_.status -cne 'current' -or [string]$_.name -notmatch '^[a-z0-9][a-z0-9._-]*$') {
                throw "HyperFrames skill report entry is invalid: $($_ | ConvertTo-Json -Compress)"
            }
            [string]$_.name
        } | Sort-Object -Unique)
    if ($skillNames.Count -ne $skills.Count) {
        throw 'HyperFrames skill report contains duplicate names.'
    }
    foreach ($entry in $SkillRoots.GetEnumerator()) {
        $root = [IO.Path]::GetFullPath([string]$entry.Value)
        if (-not (Test-Path -LiteralPath $root -PathType Container) -or
            ((Get-Item -LiteralPath $root -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "HyperFrames skills are unavailable for $($entry.Key): $root"
        }
        foreach ($name in $skillNames) {
            $skillFile = Join-Path (Join-Path $root $name) 'SKILL.md'
            if (-not (Test-Path -LiteralPath $skillFile -PathType Leaf) -or
                ((Get-Item -LiteralPath $skillFile -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "HyperFrames skill $name is unavailable for $($entry.Key): $skillFile"
            }
        }
    }
    return $skillNames
}

function Install-HyperFramesStack {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$')]
        [string]$NodeVersion = '',
        [ValidatePattern('^$|^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*))?$')]
        [string]$FFmpegVersion = '',
        [ValidatePattern('^$|^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$')]
        [string]$Version = ''
    )

    $NodeVersion = Get-ProvisioningToolVersion -Tool 'OpenJS.NodeJS.LTS' -Requested $NodeVersion
    $FFmpegVersion = Get-ProvisioningToolVersion -Tool 'Gyan.FFmpeg' -Requested $FFmpegVersion
    $Version = Get-ProvisioningToolVersion -Tool 'hyperframes' -Requested $Version

    if (-not [string]::IsNullOrWhiteSpace($NodeVersion) -and
        ($NodeVersion -notmatch '^(?<major>\d+)\.\d+\.\d+$' -or [int]$Matches['major'] -lt 22)) {
        throw "HyperFrames requires Node.js 22 or newer; requested $NodeVersion."
    }

    Install-NodeRuntime -Version $NodeVersion
    $nodeTools = Get-StackNodeTools
    $node = $nodeTools.Node
    $npmCLI = $nodeTools.NpmCLI
    $nodeVersionText = ((Invoke-ProvisioningNative -Role 'HyperFrames Node.js readiness' -FilePath $node `
        -ArgumentList @('--version')) -join [Environment]::NewLine).Trim()
    if ($nodeVersionText -notmatch '^v(?<major>\d+)\.\d+\.\d+$' -or [int]$Matches['major'] -lt 22) {
        throw "HyperFrames requires Node.js 22 or newer; found $nodeVersionText."
    }

    $git = Get-Command 'git.exe' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $git) {
        throw 'HyperFrames global skills require Base package Git.Git.'
    }

    $ffmpegMetadata = Get-ProvisioningWinGetMetadata -Role 'HyperFrames FFmpeg full build' `
        -Id 'Gyan.FFmpeg' -Version $FFmpegVersion -Architecture 'x64' -InstallerType 'zip'
    $ffmpegURI = [Uri][string]$ffmpegMetadata.Url
    $FFmpegVersion = [string]$ffmpegMetadata.Version
    $expectedFFmpegPath = "/GyanD/codexffmpeg/releases/download/$FFmpegVersion/ffmpeg-$FFmpegVersion-full_build.zip"
    if ([string]$ffmpegMetadata.Id -cne 'Gyan.FFmpeg' -or
        $FFmpegVersion -notmatch '^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*))?$' -or
        [string]$ffmpegMetadata.Architecture -cne 'x64' -or
        [string]$ffmpegMetadata.InstallerType -cne 'zip' -or
        $ffmpegURI.Scheme -cne 'https' -or $ffmpegURI.Host -cne 'github.com' -or
        $ffmpegURI.AbsolutePath -cne $expectedFFmpegPath) {
        throw 'Gyan.FFmpeg metadata does not describe the current stable x64 full build.'
    }
    $null = Get-ProvisioningToolVersion -Tool 'Gyan.FFmpeg' -Requested $FFmpegVersion
    Install-ProvisioningCachedPackage -Role 'HyperFrames FFmpeg full build' -Metadata $ffmpegMetadata `
        -DownloadSource 'WinGet' -Adapter 'Portable' -ExecutableName 'ffmpeg.exe' `
        -PortableVersionArguments @('-version')
    $ffmpeg = Get-Command 'ffmpeg.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1
    $ffprobe = Get-Command 'ffprobe.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1
    if ((Split-Path -Parent $ffmpeg.Source) -ine (Split-Path -Parent $ffprobe.Source)) {
        throw 'HyperFrames FFmpeg and FFprobe resolved from different distributions.'
    }
    $ffmpegSuffix = '(?:[-+][^ ]*)? Copyright \(c\) \d{4}-\d{4} the FFmpeg developers'
    $ffmpegVersionText = Assert-ProvisioningCommand -Role 'HyperFrames FFmpeg' -Name 'ffmpeg.exe' `
        -VersionArguments @('-version') `
        -ExpectedPattern ('^ffmpeg version ' + [regex]::Escape($FFmpegVersion) + $ffmpegSuffix)
    $ffprobeVersionText = Assert-ProvisioningCommand -Role 'HyperFrames FFprobe' -Name 'ffprobe.exe' `
        -VersionArguments @('-version') `
        -ExpectedPattern ('^ffprobe version ' + [regex]::Escape($FFmpegVersion) + $ffmpegSuffix)

    $toolRoot = 'C:\HerdrSandbox\tools\hyperframes'
    $npmCache = 'C:\HerdrSandbox\tools\npm-cache'
    $stagingRoot = 'C:\HerdrSandbox\staging'
    foreach ($directory in @('C:\HerdrSandbox\tools', $npmCache, $stagingRoot)) {
        if (-not (Test-Path -LiteralPath $directory)) {
            New-Item -ItemType Directory -Path $directory -Force | Out-Null
        }
        $directoryInfo = Get-Item -LiteralPath $directory -Force
        if (-not $directoryInfo.PSIsContainer -or
            ($directoryInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "HyperFrames guest-local directory is unsafe: $directory"
        }
    }

    $environmentNames = @('npm_config_cache', 'npm_config_update_notifier', 'npm_config_yes',
        'npm_config_engine_strict', 'HYPERFRAMES_NO_TELEMETRY', 'HYPERFRAMES_BROWSER_PATH')
    $previousEnvironment = @{}
    foreach ($name in $environmentNames) {
        $previousEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
    }
    try {
        $env:npm_config_cache = $npmCache
        $env:npm_config_update_notifier = 'false'
        $env:npm_config_yes = 'true'
        $env:npm_config_engine_strict = 'true'
        $env:HYPERFRAMES_NO_TELEMETRY = '1'
        Remove-Item Env:\HYPERFRAMES_BROWSER_PATH -ErrorAction SilentlyContinue

        if ([string]::IsNullOrWhiteSpace($Version)) {
            $versionJSON = ((Invoke-ProvisioningNative -Role 'HyperFrames latest version resolution' `
                -FilePath $node -ArgumentList @($npmCLI, 'view', 'hyperframes@latest', 'version', '--json') `
                -WorkingDirectory $stagingRoot -TimeoutSeconds 60) -join [Environment]::NewLine).Trim()
            try {
                $resolvedVersion = $versionJSON | ConvertFrom-Json
            } catch {
                throw "HyperFrames latest version resolution returned invalid JSON: $($_.Exception.Message)"
            }
            if ($resolvedVersion -isnot [string] -or
                [string]$resolvedVersion -notmatch '^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$') {
                throw "HyperFrames latest version resolution returned an invalid stable version: $resolvedVersion"
            }
            $Version = [string]$resolvedVersion
            $null = Get-ProvisioningToolVersion -Tool 'hyperframes' -Requested $Version
        }

        if (Test-Path -LiteralPath $toolRoot) {
            $rootInfo = Get-Item -LiteralPath $toolRoot -Force
            $rootItems = @(Get-ChildItem -LiteralPath $toolRoot -Recurse -Force)
            if (-not $rootInfo.PSIsContainer -or
                ($rootInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
                @($rootItems | Where-Object {
                        ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0
                    }).Count -ne 0) {
                throw "HyperFrames tool root is unsafe: $toolRoot"
            }
        } else {
            New-Item -ItemType Directory -Path $toolRoot | Out-Null
        }

        Write-Output "Installing HyperFrames CLI $Version globally in the Sandbox..."
        Invoke-ProvisioningNative -Role 'HyperFrames CLI global installation' -FilePath $node -ArgumentList @(
            $npmCLI,
            'install',
            '--global',
            '--prefix', $toolRoot,
            '--omit=dev',
            '--no-audit',
            '--no-fund',
            '--package-lock=false',
            "hyperframes@$Version"
        ) -WorkingDirectory $stagingRoot -TimeoutSeconds 600 | Out-Null

        $packageDirectory = Join-Path $toolRoot 'node_modules\hyperframes'
        $packagePath = Join-Path $packageDirectory 'package.json'
        $cliCommand = Join-Path $toolRoot 'hyperframes.cmd'
        foreach ($path in @($packagePath, $cliCommand)) {
            if (-not (Test-Path -LiteralPath $path -PathType Leaf) -or
                ((Get-Item -LiteralPath $path -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "HyperFrames package command is missing or unsafe: $path"
            }
        }
        try {
            $package = [IO.File]::ReadAllText($packagePath) | ConvertFrom-Json
        } catch {
            throw "HyperFrames package identity is unreadable: $($_.Exception.Message)"
        }
        $engine = [string]$package.engines.node
        $binRelative = [string]$package.bin.hyperframes
        if ($binRelative.StartsWith('./', [StringComparison]::Ordinal) -or
            $binRelative.StartsWith('.\', [StringComparison]::Ordinal)) {
            $binRelative = $binRelative.Substring(2)
        }
        if ($engine -notmatch '^>=\s*(?<major>\d+)(?:\.0(?:\.0)?)?\s*$') {
            throw "HyperFrames package Node.js engine is unsupported: $engine"
        }
        $minimumNodeMajor = [int]$Matches['major']
        if ([string]$package.name -cne 'hyperframes' -or [string]$package.version -cne $Version -or
            $minimumNodeMajor -lt 22 -or [string]::IsNullOrWhiteSpace($binRelative) -or
            [IO.Path]::IsPathRooted($binRelative) -or
            @($binRelative -split '[/\\]' | Where-Object { $_ -ceq '.' -or $_ -ceq '..' }).Count -ne 0) {
            throw "HyperFrames package identity does not match version $Version and Node.js 22+ readiness."
        }
        $cliEntry = [IO.Path]::GetFullPath((Join-Path $packageDirectory ($binRelative -replace '/', '\')))
        $packageRoot = [IO.Path]::GetFullPath($packageDirectory).TrimEnd('\')
        if (-not $cliEntry.StartsWith($packageRoot + '\', [StringComparison]::OrdinalIgnoreCase) -or
            -not (Test-Path -LiteralPath $cliEntry -PathType Leaf) -or
            ((Get-Item -LiteralPath $cliEntry -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "HyperFrames CLI entry is missing or unsafe: $cliEntry"
        }
        $powerShellShim = Join-Path $toolRoot 'hyperframes.ps1'
        if (Test-Path -LiteralPath $powerShellShim) {
            $powerShellShimInfo = Get-Item -LiteralPath $powerShellShim -Force
            if ($powerShellShimInfo.PSIsContainer -or
                ($powerShellShimInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "HyperFrames PowerShell shim is unsafe: $powerShellShim"
            }
            Remove-Item -LiteralPath $powerShellShim -Force
        }
        Add-ProvisioningMachinePath -Directory $toolRoot
        $resolvedCLI = Wait-ProvisioningCommandAvailable -Role 'HyperFrames CLI command' -Name 'hyperframes.cmd'
        if ([IO.Path]::GetFullPath($resolvedCLI) -ine [IO.Path]::GetFullPath($cliCommand)) {
            throw "HyperFrames CLI command resolved from an unexpected path: $resolvedCLI"
        }
        $cliVersion = ((Invoke-ProvisioningNative -Role 'HyperFrames CLI version check' -FilePath $node `
            -ArgumentList @($cliEntry, '--version') -WorkingDirectory $stagingRoot -TimeoutSeconds 30) `
            -join [Environment]::NewLine).Trim()
        if ($cliVersion -cne $Version) {
            throw "HyperFrames CLI version output is unexpected: $cliVersion"
        }

        Invoke-ProvisioningNative -Role 'HyperFrames managed Chrome Headless Shell installation' `
            -FilePath $node -ArgumentList @($cliEntry, 'browser', 'ensure') `
            -WorkingDirectory $stagingRoot -TimeoutSeconds 600 | Out-Null
        $browserPath = ((Invoke-ProvisioningNative -Role 'HyperFrames managed browser path check' `
            -FilePath $node -ArgumentList @($cliEntry, 'browser', 'path') `
            -WorkingDirectory $stagingRoot -TimeoutSeconds 60) -join [Environment]::NewLine).Trim()
        $browserRoot = [IO.Path]::GetFullPath((Join-Path $env:USERPROFILE '.cache\hyperframes\chrome')).TrimEnd('\')
        if ([string]::IsNullOrWhiteSpace($browserPath) -or -not [IO.Path]::IsPathRooted($browserPath)) {
            throw "HyperFrames managed browser path is invalid: $browserPath"
        }
        $browserPath = [IO.Path]::GetFullPath($browserPath)
        if (-not $browserPath.StartsWith($browserRoot + '\', [StringComparison]::OrdinalIgnoreCase) -or
            [IO.Path]::GetFileName($browserPath) -ine 'chrome-headless-shell.exe' -or
            -not (Test-Path -LiteralPath $browserPath -PathType Leaf) -or
            ((Get-Item -LiteralPath $browserPath -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "HyperFrames did not prepare its managed Chrome Headless Shell: $browserPath"
        }
        $browserVersion = ((Invoke-ProvisioningNative -Role 'HyperFrames managed browser launch check' `
            -FilePath $browserPath -ArgumentList @('--version') -WorkingDirectory $stagingRoot `
            -TimeoutSeconds 30) -join [Environment]::NewLine).Trim()
        if ($browserVersion -notmatch '(?i)(?:chrome|chromium).*\d+\.\d+\.\d+\.\d+') {
            throw "HyperFrames managed Chrome Headless Shell version is unexpected: $browserVersion"
        }

        $skillRoots = [ordered]@{
            'OpenCode' = Join-Path $env:USERPROFILE '.config\opencode\skills'
            'Claude Code' = Join-Path $env:USERPROFILE '.claude\skills'
            'Codex' = Join-Path $env:USERPROFILE '.codex\skills'
            'GitHub Copilot' = Join-Path $env:USERPROFILE '.copilot\skills'
            'Pi' = Join-Path $env:USERPROFILE '.pi\agent\skills'
            'Universal agents' = Join-Path $env:USERPROFILE '.agents\skills'
        }
        foreach ($root in @($skillRoots.Values)) {
            $parent = Split-Path -Parent ([string]$root)
            New-Item -ItemType Directory -Path $parent -Force | Out-Null
            $parentInfo = Get-Item -LiteralPath $parent -Force
            if (-not $parentInfo.PSIsContainer -or
                ($parentInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "HyperFrames global agent directory is unsafe: $parent"
            }
        }
        Invoke-ProvisioningNative -Role 'HyperFrames global agent skills installation' -FilePath $node `
            -ArgumentList @($cliEntry, 'skills') -WorkingDirectory $stagingRoot -TimeoutSeconds 600 | Out-Null
        $skillsJSON = ((Invoke-ProvisioningNative -Role 'HyperFrames global agent skills check' `
            -FilePath $node -ArgumentList @($cliEntry, 'skills', 'check', '--json') `
            -WorkingDirectory $stagingRoot -TimeoutSeconds 120) -join [Environment]::NewLine).Trim()
        try {
            $skillsReport = $skillsJSON | ConvertFrom-Json
        } catch {
            throw "HyperFrames global skills check returned invalid JSON: $($_.Exception.Message)"
        }
        $skillNames = @(Assert-StackHyperFramesAgentSkills -Report $skillsReport -SkillRoots $skillRoots)

        $doctorLines = @(Invoke-ProvisioningNative -Role 'HyperFrames doctor' -FilePath $node `
            -ArgumentList @($cliEntry, 'doctor', '--json') -WorkingDirectory $stagingRoot -TimeoutSeconds 180)
        $doctorJSONStart = -1
        for ($index = 0; $index -lt $doctorLines.Count; $index += 1) {
            if ([string]$doctorLines[$index] -ceq '{') {
                $doctorJSONStart = $index
                break
            }
        }
        if ($doctorJSONStart -lt 0) {
            throw "HyperFrames doctor did not return a JSON object: $($doctorLines -join [Environment]::NewLine)"
        }
        $doctorJSON = (($doctorLines[$doctorJSONStart..($doctorLines.Count - 1)]) `
            -join [Environment]::NewLine).Trim()
        try {
            $doctor = $doctorJSON | ConvertFrom-Json
        } catch {
            throw "HyperFrames doctor returned invalid JSON: $($_.Exception.Message)"
        }
        foreach ($requiredCheck in @('Node.js', 'FFmpeg', 'FFprobe', 'Chrome')) {
            $matches = @($doctor.checks | Where-Object { [string]$_.name -ceq $requiredCheck })
            if ($matches.Count -ne 1 -or $matches[0].ok -ne $true) {
                throw "HyperFrames doctor did not confirm $requiredCheck readiness: $doctorJSON"
            }
        }

        Assert-StackHyperFramesSoftwareEncode -FFmpeg ([string]$ffmpeg.Source) `
            -FFprobe ([string]$ffprobe.Source)
        Write-Output "HyperFrames CLI ready: $cliVersion"
        Write-Output "HyperFrames managed Chrome Headless Shell ready: $browserPath"
        Write-Output "HyperFrames skills ready for all supported agents: $($skillNames.Count)"
        Write-Output "HyperFrames FFmpeg ready: $($ffmpegVersionText.Split([Environment]::NewLine)[0])"
        Write-Output "HyperFrames FFprobe ready: $($ffprobeVersionText.Split([Environment]::NewLine)[0])"
        Write-Output 'HyperFrames rendering ready with verified libx264 software encoding. Browser GPU acceleration may be available; FFmpeg hardware encoding is not claimed.'
    } finally {
        foreach ($name in $environmentNames) {
            $previous = $previousEnvironment[$name]
            if ($null -eq $previous) {
                Remove-Item "Env:\$name" -ErrorAction SilentlyContinue
            } else {
                [Environment]::SetEnvironmentVariable($name, [string]$previous, 'Process')
            }
        }
    }
}

function Get-TradingViewDesktopPortableMetadata {
    param(
        [ValidatePattern('^$|^\d+\.\d+\.\d+\.\d+$')]
        [string]$Version = ''
    )

    $packageID = 'TradingView.TradingViewDesktop'
    $pinnedVersion = '3.3.0.7992'
    $resolvedVersion = Get-ProvisioningToolVersion -Tool $packageID -Requested $Version
    if ([string]::IsNullOrWhiteSpace($resolvedVersion)) {
        $resolvedVersion = $pinnedVersion
    }
    if ($resolvedVersion -cne $pinnedVersion) {
        throw "TradingView Desktop version $resolvedVersion is unsupported; this release owns $pinnedVersion."
    }
    $null = Get-ProvisioningToolVersion -Tool $packageID -Requested $resolvedVersion
    return [pscustomobject]@{
        Id = $packageID
        Version = $resolvedVersion
        Architecture = 'x64'
        InstallerType = 'msix'
        Scope = ''
        Url = 'https://tvd-packages.tradingview.com/stable/latest/win32/TradingView.msix'
        Sha256 = '96B5EBC196A3824EF22667BA9AE1A6AB92E83B70615D0AFE96031AB11C6CE6DF'
        PayloadName = 'payload.msix'
    }
}

function Install-TradingViewStack {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^\d+\.\d+\.\d+$')]
        [string]$NodeVersion = '',
        [ValidatePattern('^$|^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$')]
        [string]$TVControlVersion = '',
        [ValidatePattern('^$|^\d+\.\d+\.\d+\.\d+$')]
        [string]$DesktopVersion = ''
    )

    $NodeVersion = Get-ProvisioningToolVersion -Tool 'OpenJS.NodeJS.LTS' -Requested $NodeVersion
    $TVControlVersion = Get-ProvisioningToolVersion -Tool '@ferroxlabs/tvcontrol' -Requested $TVControlVersion
    $DesktopVersion = Get-ProvisioningToolVersion -Tool 'TradingView.TradingViewDesktop' -Requested $DesktopVersion
    $desktopPackageID = 'TradingView.TradingViewDesktop'
    $desktopMetadata = Get-TradingViewDesktopPortableMetadata -Version $DesktopVersion
    if ([string]$desktopMetadata.Id -cne $desktopPackageID -or
        [string]$desktopMetadata.Version -notmatch '^\d+\.\d+\.\d+\.\d+$') {
        throw "TradingView Desktop metadata is unexpected: $($desktopMetadata.Id) $($desktopMetadata.Version)"
    }
    Install-ProvisioningCachedPackage -Role 'TradingView Desktop' -Metadata $desktopMetadata `
        -DownloadSource 'Direct' -Adapter 'Portable' -ExecutableName 'TradingView.exe' `
        -PortableVersionSource 'File' -RequireAuthenticodeSignature

    $desktopRoot = Join-Path 'C:\HerdrSandbox\tools' (Get-ProvisioningSafeCacheName -Value $desktopPackageID)
    $desktopExecutables = @(Get-ChildItem -LiteralPath $desktopRoot -File -Recurse -Filter 'TradingView.exe')
    $desktopManifestPath = Join-Path $desktopRoot 'AppxManifest.xml'
    if ($desktopExecutables.Count -ne 1 -or
        [string]$desktopExecutables[0].VersionInfo.FileVersion -cne [string]$desktopMetadata.Version -or
        -not (Test-Path -LiteralPath $desktopManifestPath -PathType Leaf)) {
        throw "TradingView Desktop portable payload does not match $($desktopMetadata.Version)."
    }
    try {
        [xml]$desktopManifest = [IO.File]::ReadAllText($desktopManifestPath)
    } catch {
        throw "TradingView Desktop package identity is unreadable: $($_.Exception.Message)"
    }
    if ([string]$desktopManifest.Package.Identity.Name -cne 'TradingView.Desktop' -or
        [string]$desktopManifest.Package.Identity.Version -cne [string]$desktopMetadata.Version -or
        [string]$desktopManifest.Package.Identity.ProcessorArchitecture -cne 'x64' -or
        [string]$desktopManifest.Package.Identity.Publisher -cne 'CN="TradingView, Inc.", O="TradingView, Inc.", S=Ohio, C=US') {
        throw "TradingView Desktop package identity does not match $($desktopMetadata.Version)."
    }
    $desktopExecutable = $desktopExecutables[0].FullName
    $resolvedDesktop = Wait-ProvisioningCommandAvailable -Role 'TradingView Desktop command' -Name 'TradingView.exe'
    if ([IO.Path]::GetFullPath($resolvedDesktop) -ine [IO.Path]::GetFullPath($desktopExecutable)) {
        throw "TradingView Desktop command resolved from an unexpected path: $resolvedDesktop"
    }
    Ensure-ProvisioningStartShortcut -DisplayName 'TradingView' -Executable $desktopExecutable `
        -ShortcutArguments '--remote-debugging-port=9222'

    Install-NodeRuntime -Version $NodeVersion
    $nodeTools = Get-StackNodeTools
    $node = $nodeTools.Node
    $npmCLI = $nodeTools.NpmCLI
    $tvControlRoot = 'C:\HerdrSandbox\tools\tvcontrol'
    $npmCache = 'C:\HerdrSandbox\tools\npm-cache'
    foreach ($directory in @('C:\HerdrSandbox\tools', $tvControlRoot, $npmCache)) {
        if (-not (Test-Path -LiteralPath $directory)) {
            New-Item -ItemType Directory -Path $directory -Force | Out-Null
        }
        $directoryInfo = Get-Item -LiteralPath $directory -Force
        if (-not $directoryInfo.PSIsContainer -or
            ($directoryInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "TVControl guest-local directory is unsafe: $directory"
        }
    }
    $env:npm_config_cache = $npmCache
    $env:npm_config_update_notifier = 'false'

    if ([string]::IsNullOrWhiteSpace($TVControlVersion)) {
        $versionJSON = ((Invoke-ProvisioningNative -Role 'TVControl latest version resolution' `
            -FilePath $node -ArgumentList @($npmCLI, 'view', '@ferroxlabs/tvcontrol@latest', 'version', '--json')) `
            -join [Environment]::NewLine).Trim()
        try {
            $resolvedVersion = $versionJSON | ConvertFrom-Json
        } catch {
            throw "TVControl latest version resolution returned invalid JSON: $($_.Exception.Message)"
        }
        if ($resolvedVersion -isnot [string] -or
            [string]$resolvedVersion -notmatch '^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$') {
            throw "TVControl latest version resolution returned an invalid stable version: $resolvedVersion"
        }
        $TVControlVersion = [string]$resolvedVersion
        $null = Get-ProvisioningToolVersion -Tool '@ferroxlabs/tvcontrol' -Requested $TVControlVersion
    }

    $toolRoot = $tvControlRoot

    Write-Output "Installing TVControl $TVControlVersion..."
    Invoke-ProvisioningNative -Role 'TVControl package installation' -FilePath $node -ArgumentList @(
        $npmCLI,
        'install',
        '--global',
        '--prefix', $toolRoot,
        '--ignore-scripts',
        '--omit=optional',
        '--no-audit',
        '--no-fund',
        '--package-lock=false',
        "@ferroxlabs/tvcontrol@$TVControlVersion"
    ) | Out-Null

    $packageDirectory = Join-Path $toolRoot 'node_modules\@ferroxlabs\tvcontrol'
    $packagePath = Join-Path $packageDirectory 'package.json'
    if (-not (Test-Path -LiteralPath $packagePath -PathType Leaf) -or
        ((Get-Item -LiteralPath $packagePath -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "TVControl package identity is missing or unsafe: $packagePath"
    }
    try {
        $package = [IO.File]::ReadAllText($packagePath) | ConvertFrom-Json
    } catch {
        throw "TVControl package identity is unreadable: $($_.Exception.Message)"
    }
    $tvBin = [string]$package.bin.tv
    $tvControlBin = [string]$package.bin.tvcontrol
    if ([string]$package.name -cne '@ferroxlabs/tvcontrol' -or
        [string]$package.version -cne $TVControlVersion -or
        [string]$package.engines.node -notmatch '^>=\d+\.\d+\.\d+$') {
        throw "TVControl package identity does not match exact version $TVControlVersion."
    }
    $packageRootPath = [IO.Path]::GetFullPath($packageDirectory).TrimEnd('\') + '\'
    foreach ($bin in @($tvBin, $tvControlBin)) {
        if ([string]::IsNullOrWhiteSpace($bin) -or $bin -notmatch '^[A-Za-z0-9._/-]+\.js$' -or
            $bin.StartsWith('/') -or @($bin.Split('/') | Where-Object { $_ -ceq '..' }).Count -ne 0) {
            throw "TVControl package identity does not match exact version $TVControlVersion."
        }
    }
    $tvCLIEntryPath = [IO.Path]::GetFullPath((Join-Path $packageDirectory ($tvBin.Replace('/', '\'))))
    $tvControlEntryPath = [IO.Path]::GetFullPath((Join-Path $packageDirectory ($tvControlBin.Replace('/', '\'))))
    foreach ($cliEntryPath in @($tvCLIEntryPath, $tvControlEntryPath)) {
        if (-not $cliEntryPath.StartsWith($packageRootPath, [StringComparison]::OrdinalIgnoreCase) -or
            -not (Test-Path -LiteralPath $cliEntryPath -PathType Leaf) -or
            ((Get-Item -LiteralPath $cliEntryPath -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "TVControl CLI entry is missing or unsafe: $cliEntryPath"
        }
    }

    $tvCommand = Join-Path $toolRoot 'tv.cmd'
    $tvControlCommand = Join-Path $toolRoot 'tvcontrol.cmd'
    foreach ($command in @($tvCommand, $tvControlCommand)) {
        if (-not (Test-Path -LiteralPath $command -PathType Leaf) -or
            ((Get-Item -LiteralPath $command -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "TVControl command is missing or unsafe: $command"
        }
    }
    foreach ($powerShellShim in @((Join-Path $toolRoot 'tv.ps1'), (Join-Path $toolRoot 'tvcontrol.ps1'))) {
        if (Test-Path -LiteralPath $powerShellShim) {
            $shimInfo = Get-Item -LiteralPath $powerShellShim -Force
            if ($shimInfo.PSIsContainer -or
                ($shimInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "TVControl PowerShell shim is unsafe: $powerShellShim"
            }
            Remove-Item -LiteralPath $powerShellShim -Force
        }
    }
    Add-ProvisioningMachinePath -Directory $toolRoot
    foreach ($command in @{'tv.cmd' = $tvCommand; 'tvcontrol.cmd' = $tvControlCommand}.GetEnumerator()) {
        $resolved = Wait-ProvisioningCommandAvailable -Role "TVControl $($command.Key) command" -Name $command.Key
        if ([IO.Path]::GetFullPath($resolved) -ine [IO.Path]::GetFullPath($command.Value)) {
            throw "TVControl command resolved from an unexpected path: $resolved"
        }
    }
    $helpText = ((Invoke-ProvisioningNative -Role 'TVControl CLI help verification' -FilePath $node `
        -ArgumentList @($tvCLIEntryPath, '--help')) -join [Environment]::NewLine).Trim()
    if (-not $helpText.StartsWith('Usage: tv <command> [options]', [StringComparison]::Ordinal) -or
        $helpText -notmatch '(?m)^  status\s+\S' -or
        $helpText -notmatch '(?m)^  launch\s+\S') {
        throw 'TVControl CLI help output is unexpected.'
    }

    Write-Output "TradingView Desktop ready: $($desktopMetadata.Version)"
    Write-Output "TVControl ready: $TVControlVersion"
    Write-Output 'TradingView remains stopped after provisioning. Its managed shortcut and TVControl launch command enable local CDP on port 9222 when explicitly launched.'
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

    if ([string]::IsNullOrWhiteSpace($Series)) {
        throw 'Python series was not resolved before provisioning.'
    }
    return [pscustomobject]@{ Series = $Series; Version = '' }
}

function Install-PythonStack {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^[1-9][0-9]*\.(?:0|[1-9][0-9]*)$')]
        [string]$Series = '',
        [ValidatePattern('^$|^[1-9][0-9]*\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*))?$')]
        [string]$Version = ''
    )

    $Series = Get-ProvisioningToolSeries -Tool 'Python' -Requested $Series
    $Version = Get-ProvisioningToolVersion -Tool 'Python' -Requested $Version
    $pythonSelection = Resolve-StackPythonPackage -Series $Series -Version $Version
    $Series = [string]$pythonSelection.Series
    $Version = [string]$pythonSelection.Version
    $null = Get-ProvisioningToolSeries -Tool 'Python' -Requested $Series
    $pythonAliasDirectory = 'C:\HerdrSandbox\tools\python\bin'
    $pythonAlias = Join-Path $pythonAliasDirectory 'python.exe'
    $python3 = Join-Path $pythonAliasDirectory 'python3.exe'
    $pythonSourceExclusions = @('*\Microsoft\WindowsApps\python.exe', $pythonAlias)
    $pythonTarget = if ([string]::IsNullOrWhiteSpace($Version)) { $Series } else { $Version }
    Write-Output "Installing Python $pythonTarget..."
    Install-ProvisioningWinGetPackage -Role 'Python' -Id "Python.Python.$Series" -Version $Version `
        -VersionTool 'Python' -InstallerType 'burn' -Scope 'machine' -Adapter 'Burn' -ExecutableName 'python.exe' `
        -CommandSourceExclusion '*\Microsoft\WindowsApps\python.exe' -DeferCommandReadiness `
        -RequireAuthenticodeSignature
    $Version = Get-ProvisioningToolVersion -Tool 'Python'
    $pythonSelection = Resolve-StackPythonPackage -Series $Series -Version $Version
    $Series = [string]$pythonSelection.Series
    $Version = [string]$pythonSelection.Version
    $pythonPath = Wait-ProvisioningCommandAvailable -Role 'Python' -Name 'python.exe' `
        -CommandSourceExclusion $pythonSourceExclusions
    $runtimeVersion = ($Version -split '\.')[0..2] -join '.'
    $pythonPattern = '^Python ' + [regex]::Escape($runtimeVersion) + '$'
    $pythonVersion = ((Invoke-ProvisioningNative -Role 'Python version check' -FilePath $pythonPath `
                -ArgumentList @('--version')) -join [Environment]::NewLine).Trim()
    if ($pythonVersion -notmatch $pythonPattern) {
        throw "Python version output is unexpected: $pythonVersion"
    }
    Write-Output "Python ready: $pythonVersion"
    if ($Series -notmatch '^3\.') {
        return
    }
    $pythonInfo = Get-Item -LiteralPath $pythonPath -Force
    if (($pythonInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Python command is a reparse point: $pythonPath"
    }
    foreach ($directory in @('C:\HerdrSandbox\tools', 'C:\HerdrSandbox\tools\python', $pythonAliasDirectory)) {
        if (-not (Test-Path -LiteralPath $directory)) {
            New-Item -ItemType Directory -Path $directory -Force | Out-Null
        }
        $directoryInfo = Get-Item -LiteralPath $directory -Force
        if (-not $directoryInfo.PSIsContainer -or
            ($directoryInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Python command directory is unsafe: $directory"
        }
    }
    $pythonHash = (Get-FileHash -LiteralPath $pythonPath -Algorithm SHA256).Hash
    foreach ($pythonCommand in @($pythonAlias, $python3)) {
        if (Test-Path -LiteralPath $pythonCommand) {
            $pythonCommandInfo = Get-Item -LiteralPath $pythonCommand -Force
            if ($pythonCommandInfo.PSIsContainer -or
                ($pythonCommandInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "Python command path is unsafe: $pythonCommand"
            }
        }
        $pythonCommandHash = if (Test-Path -LiteralPath $pythonCommand -PathType Leaf) {
            (Get-FileHash -LiteralPath $pythonCommand -Algorithm SHA256).Hash
        } else {
            ''
        }
        if ($pythonCommandHash -cne $pythonHash) {
            Copy-Item -LiteralPath $pythonPath -Destination $pythonCommand -Force
            $pythonCommandHash = (Get-FileHash -LiteralPath $pythonCommand -Algorithm SHA256).Hash
        }
        if ($pythonCommandHash -cne $pythonHash) {
            throw "Python command copy failed verification: $pythonCommand"
        }
    }
    Add-ProvisioningMachinePath -Directory $pythonAliasDirectory
    $resolvedPython = Wait-ProvisioningCommandAvailable -Role 'App-local Python command' -Name 'python.exe'
    if ([IO.Path]::GetFullPath($resolvedPython) -ine [IO.Path]::GetFullPath($pythonAlias)) {
        throw "Python command resolved from an unexpected path: $resolvedPython"
    }
    $pythonAliasVersion = Assert-ProvisioningCommand -Role 'App-local Python command' -Name 'python.exe' `
        -VersionArguments @('--version') -ExpectedPattern ('^' + [regex]::Escape($pythonVersion) + '$')
    $resolvedPython3 = Wait-ProvisioningCommandAvailable -Role 'Python 3 command' -Name 'python3.exe'
    if ([IO.Path]::GetFullPath($resolvedPython3) -ine [IO.Path]::GetFullPath($python3)) {
        throw "Python 3 command resolved from an unexpected path: $resolvedPython3"
    }
    $python3Version = Assert-ProvisioningCommand -Role 'Python 3 command' -Name 'python3.exe' `
        -VersionArguments @('--version') -ExpectedPattern ('^' + [regex]::Escape($pythonVersion) + '$')
    Write-Output "App-local Python command ready: $pythonAliasVersion"
    Write-Output "Python 3 command ready: $python3Version"
}

function Install-Uv {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^\d+\.\d+\.\d+$')]
        [string]$Version = ''
    )

    $Version = Get-ProvisioningToolVersion -Tool 'astral-sh.uv' -Requested $Version
    Write-Output 'Installing uv...'
    Install-ProvisioningWinGetPackage -Role 'uv' -Id 'astral-sh.uv' -Version $Version `
        -InstallerType 'zip' -Adapter 'Portable' -ExecutableName 'uv.exe'
    $uvPattern = if ([string]::IsNullOrWhiteSpace($Version)) {
        '^uv \d+\.\d+\.\d+(?: \([^)]+\))?$'
    } else {
        '^uv ' + [regex]::Escape($Version) + '(?: \([^)]+\))?$'
    }
    $uvVersion = Assert-ProvisioningCommand -Role 'uv' -Name 'uv.exe' `
        -VersionArguments @('--version') -ExpectedPattern $uvPattern

    $uvCacheRoot = 'C:\HerdrSandbox\cache\uv'
    Assert-ProvisioningCachePath -Path $uvCacheRoot
    if (-not (Test-Path -LiteralPath $uvCacheRoot)) {
        New-Item -ItemType Directory -Path $uvCacheRoot -Force | Out-Null
    }
    $uvCacheInfo = Get-Item -LiteralPath $uvCacheRoot -Force
    if (-not $uvCacheInfo.PSIsContainer -or
        ($uvCacheInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "uv cache directory is unsafe: $uvCacheRoot"
    }
    Assert-ProvisioningCacheTree -Path $uvCacheRoot

    $env:UV_CACHE_DIR = $uvCacheRoot
    $env:UV_NO_MANAGED_PYTHON = '1'
    [Environment]::SetEnvironmentVariable('UV_CACHE_DIR', $uvCacheRoot, 'Machine')
    [Environment]::SetEnvironmentVariable('UV_NO_MANAGED_PYTHON', '1', 'Machine')
    if ([Environment]::GetEnvironmentVariable('UV_CACHE_DIR', 'Machine') -cne $uvCacheRoot -or
        [Environment]::GetEnvironmentVariable('UV_NO_MANAGED_PYTHON', 'Machine') -cne '1') {
        throw 'uv environment verification failed.'
    }

    $uvCommand = (Get-Command 'uv.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
    $reportedCache = ((Invoke-ProvisioningNative -Role 'uv cache directory verification' `
                -FilePath $uvCommand -ArgumentList @('cache', 'dir')) -join [Environment]::NewLine).Trim()
    if ([string]::IsNullOrWhiteSpace($reportedCache) -or
        [IO.Path]::GetFullPath($reportedCache).TrimEnd('\') -ine $uvCacheRoot) {
        throw "uv cache directory is unexpected: $reportedCache"
    }
    Write-Output "uv ready: $uvVersion"
}

function Install-PythonAIStack {
    [CmdletBinding()]
    param()

    Install-PythonStack -Series '3.13'
    Install-Uv
    Write-Output 'Python AI development toolchain ready.'
}

function Install-ZigStack {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^\d+\.\d+\.\d+$')]
        [string]$Version = ''
    )

    $Version = Get-ProvisioningToolVersion -Tool 'zig.zig' -Requested $Version
    Write-Output 'Installing Zig...'
    Install-ProvisioningWinGetPackage -Role 'Zig' -Id 'zig.zig' -Version $Version `
        -InstallerType 'zip' -Adapter 'Portable' -ExecutableName 'zig.exe' `
        -PortableVersionArguments @('version')
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
        [string]$ProjectDirectory = '',
        [ValidatePattern('^$|^\d+\.\d+\.\d+$')]
        [string]$Toolchain = ''
    )

    $Toolchain = Get-ProvisioningToolVersion -Tool 'rust-toolchain' -Requested $Toolchain
    $versionSource = Get-ProvisioningToolVersionSource -Tool 'rust-toolchain'
    $exactToolchainPattern = '^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$'
    if ($versionSource -ceq 'project-version-file' -and -not [string]::IsNullOrWhiteSpace($ProjectDirectory)) {
        if (-not (Test-Path -LiteralPath $ProjectDirectory -PathType Container)) {
            throw "Rust project directory is missing: $ProjectDirectory"
        }
        $toolchainFile = Join-Path $ProjectDirectory 'rust-toolchain.toml'
        if (Test-Path -LiteralPath $toolchainFile -PathType Leaf) {
            $toolchainInfo = Get-Item -LiteralPath $toolchainFile -Force
            if (($toolchainInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
                $toolchainInfo.Length -le 0 -or $toolchainInfo.Length -gt 65536) {
                throw "Rust toolchain file is empty or unsafe: $toolchainFile"
            }
            $toolchainText = [IO.File]::ReadAllText($toolchainFile)
            $channelMatches = [regex]::Matches($toolchainText, '(?m)^\s*channel\s*=\s*"([^"]+)"\s*$')
            if ($channelMatches.Count -ne 1) {
                throw "Rust toolchain file must declare exactly one literal channel: $toolchainFile"
            }
            $projectToolchain = [string]$channelMatches[0].Groups[1].Value
            if ($projectToolchain -notmatch $exactToolchainPattern -or $projectToolchain -cne $Toolchain) {
                throw "Rust project toolchain changed after preflight: $toolchainFile"
            }
        }
    }
    $requestedChannel = if ([string]::IsNullOrWhiteSpace($Toolchain)) { 'stable' } else { $Toolchain }

$rustTriple = 'x86_64-pc-windows-msvc'
$rustDistribution = Resolve-StackRustDistribution -RequestedChannel $requestedChannel -Target $rustTriple
$Toolchain = [string]$rustDistribution.Toolchain
$null = Get-ProvisioningToolVersion -Tool 'rust-toolchain' -Requested $Toolchain
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

    $rustDistServer = ([Uri][IO.Path]::GetFullPath($rustMirrorRoot)).AbsoluteUri.TrimEnd('/')
    $rustDistURI = [Uri]$rustDistServer
    if ($rustDistURI.Scheme -cne 'file' -or
        [IO.Path]::GetFullPath($rustDistURI.LocalPath).TrimEnd('\') -cne
        [IO.Path]::GetFullPath($rustMirrorRoot).TrimEnd('\')) {
        throw "Rust distribution mirror file URI is invalid: $rustDistServer"
    }
    $env:RUSTUP_DIST_SERVER = $rustDistServer
    $env:RUSTUP_UPDATE_ROOT = "$rustDistServer/__self_update_disabled__"
    $env:RUSTUP_AUTO_INSTALL = '0'

    $rustupCommand = Get-Command 'rustup.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1
    $rustToolchainTask = [ordered]@{
        Role = 'Rust toolchain installation'
        FilePath = $rustupCommand.Source
        ArgumentList = @(
            'toolchain', 'install', $rustToolchain, '--profile', 'minimal', '--component', 'rustfmt',
            '--component', 'clippy', '--target', $rustTriple, '--no-self-update'
        )
        WorkingDirectory = $ProjectDirectory
        TimeoutSeconds = 1800
    }
    Write-Output 'Installing Visual Studio C++ Build Tools alongside the Rust toolchain...'
    Install-StackVisualStudioBuildTools -RustToolchainTask $rustToolchainTask
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

    $Version = Get-ProvisioningToolVersion -Tool 'nextest.cargo-nextest' -Requested $Version
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

    $Version = Get-ProvisioningToolVersion -Tool 'Casey.Just' -Requested $Version
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

function Install-BunStack {
    [CmdletBinding()]
    param(
        [ValidatePattern('^$|^\d+\.\d+\.\d+$')]
        [string]$Version = ''
    )

    $Version = Get-ProvisioningToolVersion -Tool 'Oven-sh.Bun' -Requested $Version
    Write-Output 'Installing Bun...'
    Install-ProvisioningWinGetPackage -Role 'Bun' -Id 'Oven-sh.Bun' -Version $Version `
        -InstallerType 'zip' -Adapter 'Portable' -ExecutableName 'bun.exe'
    $bunPattern = if ([string]::IsNullOrWhiteSpace($Version)) {
        '^\d+\.\d+\.\d+$'
    } else {
        '^' + [regex]::Escape($Version) + '$'
    }
    $bunVersion = Assert-ProvisioningCommand -Role 'Bun' -Name 'bun.exe' `
        -VersionArguments @('--version') -ExpectedPattern $bunPattern
    Write-Output "Bun ready: $bunVersion"
}

function Get-HandyWebView2Runtime {
    param(
        [Parameter(Mandatory = $true)]
        [Version]$MinimumVersion
    )

    $key = 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}'
    if (-not (Test-Path -LiteralPath $key)) {
        return $null
    }
    $registration = Get-ItemProperty -LiteralPath $key
    if ([string]$registration.name -cne 'Microsoft Edge WebView2 Runtime') {
        return $null
    }
    try {
        $version = [Version][string]$registration.pv
    } catch {
        return $null
    }
    if ($version -lt $MinimumVersion) {
        return $null
    }
    $expectedLocation = Join-Path ${env:ProgramFiles(x86)} 'Microsoft\EdgeWebView\Application'
    try {
        $location = [IO.Path]::GetFullPath([string]$registration.location).TrimEnd('\')
    } catch {
        return $null
    }
    if ($location -cne [IO.Path]::GetFullPath($expectedLocation).TrimEnd('\')) {
        return $null
    }
    $executable = Join-Path (Join-Path $location $version.ToString()) 'msedgewebview2.exe'
    if (-not (Test-Path -LiteralPath $executable -PathType Leaf)) {
        return $null
    }
    $file = Get-Item -LiteralPath $executable -Force
    if (($file.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
        [string]$file.VersionInfo.FileVersion -cne $version.ToString() -or
        [string]$file.VersionInfo.ProductVersion -cne $version.ToString()) {
        return $null
    }
    $signature = Get-AuthenticodeSignature -LiteralPath $executable
    if ($signature.Status -ne [System.Management.Automation.SignatureStatus]::Valid -or
        $null -eq $signature.SignerCertificate -or
        $signature.SignerCertificate.Subject -notmatch '(^|,\s*)O=Microsoft Corporation(,|$)') {
        return $null
    }
    return [pscustomobject]@{ Version = $version.ToString(); Executable = $executable }
}

function Write-HandySPIRVHeadersPackage {
    param(
        [Parameter(Mandatory = $true)]
        [string]$VulkanRoot
    )

    $vulkanBase = 'C:\VulkanSDK'
    $vulkanRootFull = [IO.Path]::GetFullPath($VulkanRoot).TrimEnd('\')
    if (-not $vulkanRootFull.StartsWith($vulkanBase + '\', [StringComparison]::Ordinal)) {
        throw "Handy Vulkan SDK root is outside the expected owner: $vulkanRootFull"
    }
    $include = Join-Path $vulkanRootFull 'Include'
    $spirvInclude = Join-Path $include 'spirv-headers'
    foreach ($directory in @($vulkanBase, $vulkanRootFull, $include, $spirvInclude)) {
        if (-not (Test-Path -LiteralPath $directory -PathType Container)) {
            throw "Handy Vulkan SDK directory is missing: $directory"
        }
        $item = Get-Item -LiteralPath $directory -Force
        if (-not $item.PSIsContainer -or
            ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Handy Vulkan SDK directory is unsafe: $directory"
        }
    }
    $header = Join-Path $spirvInclude 'spirv.hpp'
    if (-not (Test-Path -LiteralPath $header -PathType Leaf) -or
        ((Get-Item -LiteralPath $header -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Handy Vulkan SDK SPIRV headers are missing or unsafe: $header"
    }
    $toolsOwner = 'C:\HerdrSandbox'
    $toolsRoot = Join-Path $toolsOwner 'tools'
    $prefix = 'C:\HerdrSandbox\tools\handy-cmake-prefix'
    $configDirectory = Join-Path $prefix 'share\cmake\SPIRV-Headers'
    foreach ($directory in @($toolsOwner, $toolsRoot, $prefix, (Join-Path $prefix 'share'), (Join-Path $prefix 'share\cmake'), $configDirectory)) {
        if (-not (Test-Path -LiteralPath $directory)) {
            New-Item -ItemType Directory -Path $directory | Out-Null
        }
        $item = Get-Item -LiteralPath $directory -Force
        if (-not $item.PSIsContainer -or
            ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Handy CMake package directory is unsafe: $directory"
        }
    }
    $cmakeInclude = $include.Replace('\', '/')
    $contents = @"
if(NOT TARGET SPIRV-Headers::SPIRV-Headers)
  if(NOT EXISTS "$cmakeInclude/spirv-headers/spirv.hpp")
    message(FATAL_ERROR "Handy requires the Vulkan SDK SPIRV headers")
  endif()
  add_library(SPIRV-Headers::SPIRV-Headers INTERFACE IMPORTED)
  set_target_properties(SPIRV-Headers::SPIRV-Headers PROPERTIES
    INTERFACE_INCLUDE_DIRECTORIES "$cmakeInclude"
  )
endif()
"@
    $config = Join-Path $configDirectory 'SPIRV-HeadersConfig.cmake'
    if (Test-Path -LiteralPath $config) {
        $existingConfig = Get-Item -LiteralPath $config -Force
        if ($existingConfig.PSIsContainer -or
            ($existingConfig.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Handy SPIRV-Headers CMake package destination is unsafe: $config"
        }
    }
    [IO.File]::WriteAllText($config, $contents, (New-Object Text.UTF8Encoding($false)))
    $configItem = Get-Item -LiteralPath $config -Force
    if (($configItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or
        [IO.File]::ReadAllText($config) -cne $contents) {
        throw "Handy SPIRV-Headers CMake package verification failed: $config"
    }

    $existing = [string][Environment]::GetEnvironmentVariable('CMAKE_PREFIX_PATH', 'Machine')
    $entries = @($existing -split ';' | Where-Object {
            -not [string]::IsNullOrWhiteSpace($_) -and
            [IO.Path]::GetFullPath($_).TrimEnd('\') -ine $prefix
        })
    $combined = (@($prefix) + $entries) -join ';'
    $env:CMAKE_PREFIX_PATH = $combined
    [Environment]::SetEnvironmentVariable('CMAKE_PREFIX_PATH', $combined, 'Machine')
    if ([Environment]::GetEnvironmentVariable('CMAKE_PREFIX_PATH', 'Machine') -cne $combined) {
        throw 'Handy CMAKE_PREFIX_PATH verification failed.'
    }
    return $prefix
}

function Assert-HandyNativeToolchain {
    param(
        [Parameter(Mandatory = $true)]
        [string]$CMakePrefix,
        [Parameter(Mandatory = $true)]
        [string]$VulkanRoot
    )

    $cmake = Get-Command 'cmake.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1
    $stage = Join-Path 'C:\HerdrSandbox\staging' ('handy-cmake-probe-' + [Guid]::NewGuid().ToString('N'))
    $source = Join-Path $stage 'source'
    $build = Join-Path $stage 'build'
    New-Item -ItemType Directory -Path $source -Force | Out-Null
    try {
        $cmakeLists = @'
cmake_minimum_required(VERSION 3.24)
project(handy_stack_probe LANGUAGES NONE)
find_package(Vulkan COMPONENTS glslc REQUIRED)
find_package(SPIRV-Headers CONFIG REQUIRED)
if(NOT TARGET Vulkan::Headers OR NOT TARGET SPIRV-Headers::SPIRV-Headers)
  message(FATAL_ERROR "Handy CMake targets are unavailable")
endif()
'@
        $main = @'
#include <vulkan/vulkan.h>
#include <spirv-headers/spirv.hpp>
int main() { return VK_API_VERSION_1_0 == 0; }
'@
        [IO.File]::WriteAllText((Join-Path $source 'CMakeLists.txt'), $cmakeLists,
            (New-Object Text.UTF8Encoding($false)))
        [IO.File]::WriteAllText((Join-Path $source 'main.cpp'), $main,
            (New-Object Text.UTF8Encoding($false)))
        $environment = Enable-StackVisualStudioDeveloperEnvironment
        Invoke-ProvisioningNative -Role 'Handy native CMake configuration' -FilePath $cmake.Source `
            -ArgumentList @('-S', $source, '-B', $build, '-G', 'NMake Makefiles',
                "-DCMAKE_PREFIX_PATH=$CMakePrefix") -TimeoutSeconds 30 | Out-Null
        $vulkanInclude = Join-Path $VulkanRoot 'Include'
        $object = Join-Path $stage 'handy_stack_probe.obj'
        $probe = Join-Path $stage 'handy_stack_probe.exe'
        Invoke-ProvisioningNative -Role 'Handy native C++ compilation' -FilePath $environment.Compiler `
            -ArgumentList @('/nologo', '/W4', '/WX', '/Z7', '/EHsc', '/std:c++20', '/TP',
                '/c', (Join-Path $source 'main.cpp'), "/I$vulkanInclude", "/Fo:$object") `
            -WorkingDirectory $stage -TimeoutSeconds 60 -TerminateDescendantsAfterRootExit | Out-Null
        Invoke-ProvisioningNative -Role 'Handy native C++ linking' -FilePath $environment.Linker `
            -ArgumentList @('/NOLOGO', '/DEBUG:NONE', "/OUT:$probe", $object) `
            -WorkingDirectory $stage -TimeoutSeconds 30 -TerminateDescendantsAfterRootExit | Out-Null
        if (-not (Test-Path -LiteralPath $probe -PathType Leaf) -or
            ((Get-Item -LiteralPath $probe -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Handy native compiler probe did not produce one regular executable: $probe"
        }
        Invoke-ProvisioningNative -Role 'Handy native C++ execution' -FilePath $probe `
            -ArgumentList @() -WorkingDirectory $stage -TimeoutSeconds 30 | Out-Null
    } finally {
        if (Test-Path -LiteralPath $stage) {
            Remove-Item -LiteralPath $stage -Recurse -Force
        }
    }
}

function Install-HandyStack {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [ValidateNotNullOrEmpty()]
        [string]$ProjectDirectory
    )

    if (-not [IO.Path]::IsPathRooted($ProjectDirectory)) {
        throw "Handy project directory must be absolute: $ProjectDirectory"
    }
    $projectRoot = [IO.Path]::GetFullPath($ProjectDirectory).TrimEnd('\')
    $packagePath = Join-Path $projectRoot 'package.json'
    $bunLockPath = Join-Path $projectRoot 'bun.lock'
    $rustRoot = Join-Path $projectRoot 'src-tauri'
    $cargoPath = Join-Path $rustRoot 'Cargo.toml'
    $modelPath = Join-Path $rustRoot 'resources\models\silero_vad_v4.onnx'
    foreach ($path in @($packagePath, $bunLockPath, $cargoPath, $modelPath)) {
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            throw "Handy project input is missing: $path"
        }
        $item = Get-Item -LiteralPath $path -Force
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 -or $item.Length -le 0) {
            throw "Handy project input is empty or unsafe: $path"
        }
    }
    try {
        $package = [IO.File]::ReadAllText($packagePath) | ConvertFrom-Json
    } catch {
        throw "Handy package.json is invalid: $($_.Exception.Message)"
    }
    if ([string]$package.name -cne 'handy-app' -or
        $package.private -isnot [bool] -or -not [bool]$package.private) {
        throw "Handy package identity is unexpected: $packagePath"
    }
    $cargoText = [IO.File]::ReadAllText($cargoPath)
    if ([regex]::Matches($cargoText, '(?m)^\s*name\s*=\s*"handy"\s*$').Count -ne 1) {
        throw "Handy Cargo package identity is unexpected: $cargoPath"
    }

    $cmakeVersionRequest = Get-ProvisioningToolVersion -Tool 'Kitware.CMake'
    $cmakeMetadata = Get-ProvisioningWinGetMetadata -Role 'Handy CMake' -Id 'Kitware.CMake' -Version $cmakeVersionRequest `
        -InstallerType 'wix' -Scope 'machine'
    if ([string]$cmakeMetadata.Id -cne 'Kitware.CMake' -or
        [string]$cmakeMetadata.Version -notmatch '^\d+\.\d+\.\d+$') {
        throw "Handy CMake metadata is unexpected: $($cmakeMetadata.Id) $($cmakeMetadata.Version)"
    }
    Install-ProvisioningCachedPackage -Role 'Handy CMake' -Metadata $cmakeMetadata `
        -DownloadSource 'WinGet' -Adapter 'MSI' -ExecutableName 'cmake.exe' `
        -InstallerArguments @('ADD_CMAKE_TO_PATH=System') -RequireAuthenticodeSignature
    $cmakeDirectory = Join-Path $env:ProgramFiles 'CMake\bin'
    Add-ProvisioningMachinePath -Directory $cmakeDirectory
    $cmakeVersion = Assert-ProvisioningCommand -Role 'Handy CMake' -Name 'cmake.exe' `
        -VersionArguments @('--version') `
        -ExpectedPattern ('^cmake version ' + [regex]::Escape([string]$cmakeMetadata.Version) + '(?:\r?\n|$)')

    $vulkanVersion = '1.4.309.0'
    $vulkanMetadata = Get-ProvisioningWinGetMetadata -Role 'Handy Vulkan SDK' `
        -Id 'KhronosGroup.VulkanSDK' -Version $vulkanVersion -InstallerType 'exe' -Scope 'machine'
    if ([string]$vulkanMetadata.Id -cne 'KhronosGroup.VulkanSDK' -or
        [string]$vulkanMetadata.Version -cne $vulkanVersion) {
        throw "Handy Vulkan SDK metadata is unexpected: $($vulkanMetadata.Id) $($vulkanMetadata.Version)"
    }
    Install-ProvisioningCachedPackage -Role 'Handy Vulkan SDK' -Metadata $vulkanMetadata `
        -DownloadSource 'WinGet' -Adapter 'Exe' `
        -InstallerArguments @('--accept-licenses', '--default-answer', '--confirm-command', 'install') `
        -RequireAuthenticodeSignature
    $vulkanRoot = [string][Environment]::GetEnvironmentVariable('VULKAN_SDK', 'Machine')
    $expectedVulkanRoot = "C:\VulkanSDK\$vulkanVersion"
    if ([string]::IsNullOrWhiteSpace($vulkanRoot) -or
        [IO.Path]::GetFullPath($vulkanRoot).TrimEnd('\') -cne $expectedVulkanRoot) {
        throw "Handy Vulkan SDK environment is unexpected: $vulkanRoot"
    }
    $env:VULKAN_SDK = $vulkanRoot
    $vulkanBin = Join-Path $vulkanRoot 'Bin'
    Add-ProvisioningMachinePath -Directory $vulkanBin
    foreach ($name in @('glslc.exe', 'glslangValidator.exe')) {
        $command = Join-Path $vulkanBin $name
        if (-not (Test-Path -LiteralPath $command -PathType Leaf) -or
            ((Get-Item -LiteralPath $command -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Handy Vulkan SDK command is missing or unsafe: $command"
        }
        Invoke-ProvisioningNative -Role "Handy Vulkan SDK $name verification" `
            -FilePath $command -ArgumentList @('--version') | Out-Null
    }

    $webViewVersionRequest = Get-ProvisioningToolVersion -Tool 'Microsoft.EdgeWebView2Runtime'
    $webViewMetadata = Get-ProvisioningWinGetMetadata -Role 'Handy WebView2 Runtime' `
        -Id 'Microsoft.EdgeWebView2Runtime' -Version $webViewVersionRequest -InstallerType 'exe' -Scope 'machine'
    try {
        $minimumWebViewVersion = [Version][string]$webViewMetadata.Version
    } catch {
        throw "Handy WebView2 metadata version is invalid: $($webViewMetadata.Version)"
    }
    $webView = Get-HandyWebView2Runtime -MinimumVersion $minimumWebViewVersion
    if ($null -eq $webView) {
        Install-ProvisioningCachedPackage -Role 'Handy WebView2 Runtime' -Metadata $webViewMetadata `
            -DownloadSource 'WinGet' -Adapter 'Exe' -InstallerArguments @('/silent', '/install') `
            -RequireAuthenticodeSignature
        $webView = Get-HandyWebView2Runtime -MinimumVersion $minimumWebViewVersion
    }
    if ($null -eq $webView) {
        throw "Handy WebView2 Runtime $minimumWebViewVersion or newer is unavailable after installation."
    }

    Install-BunStack
    Install-RustMSVCStack -ProjectDirectory $rustRoot
    $cmakePrefix = Write-HandySPIRVHeadersPackage -VulkanRoot $vulkanRoot
    Assert-HandyNativeToolchain -CMakePrefix $cmakePrefix -VulkanRoot $vulkanRoot

    Write-Output "Handy CMake ready: $cmakeVersion"
    Write-Output "Handy Vulkan SDK ready: $vulkanVersion"
    Write-Output "Handy WebView2 ready: $($webView.Version)"
    Write-Output 'Handy development toolchain ready.'
}

function Install-HerdrStack {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)]
        [ValidateNotNullOrEmpty()]
        [string]$ProjectDirectory
    )

    if (-not [IO.Path]::IsPathRooted($ProjectDirectory)) {
        throw "Herdr project directory must be absolute: $ProjectDirectory"
    }
    $projectRoot = [IO.Path]::GetFullPath($ProjectDirectory).TrimEnd('\')
    if (-not (Test-Path -LiteralPath (Join-Path $projectRoot 'Cargo.toml') -PathType Leaf)) {
        throw "Herdr Cargo.toml is missing from mapped project: $projectRoot"
    }

    Install-PythonStack -Series '3.13'
    Install-ZigStack -Version '0.15.2'
    Install-RustMSVCStack -ProjectDirectory $projectRoot

    $expectedCargoTarget = 'C:\HerdrSandbox\build\cargo-target'
    if ([string]::IsNullOrWhiteSpace($env:CARGO_TARGET_DIR) -or
        [IO.Path]::GetFullPath($env:CARGO_TARGET_DIR).TrimEnd('\') -cne $expectedCargoTarget) {
        throw "Herdr Rust stack returned an unexpected CARGO_TARGET_DIR: $env:CARGO_TARGET_DIR"
    }
    $libghosttyOutput = Join-Path $expectedCargoTarget 'zig-out'
    if (-not (Test-Path -LiteralPath $libghosttyOutput)) {
        New-Item -ItemType Directory -Path $libghosttyOutput -Force | Out-Null
    }
    $libghosttyOutputInfo = Get-Item -LiteralPath $libghosttyOutput -Force
    if (-not $libghosttyOutputInfo.PSIsContainer -or
        ($libghosttyOutputInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Herdr libghostty output directory is unsafe: $libghosttyOutput"
    }
    $env:LIBGHOSTTY_VT_ZIG_OUT_DIR = $libghosttyOutput
    [Environment]::SetEnvironmentVariable('LIBGHOSTTY_VT_ZIG_OUT_DIR', $libghosttyOutput, 'Machine')
    if ([Environment]::GetEnvironmentVariable('LIBGHOSTTY_VT_ZIG_OUT_DIR', 'Machine') -cne $libghosttyOutput) {
        throw 'Herdr libghostty output environment verification failed.'
    }

    Install-BunStack
    Install-CargoNextest
    Install-Just
    Write-Output 'Herdr development toolchain ready.'
}
