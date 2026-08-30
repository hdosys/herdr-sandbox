param(
    [Parameter(Mandatory = $true)]
    [string]$CacheDirectory,
    [int]$TimeoutSeconds = 5400
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
Set-StrictMode -Version 2.0

if (-not [IO.Path]::IsPathRooted($CacheDirectory)) {
    throw "Visual Studio cache directory is not absolute: $CacheDirectory"
}
if ($TimeoutSeconds -lt 60 -or $TimeoutSeconds -gt 21600) {
    throw "Visual Studio layout timeout is outside 60..21600 seconds: $TimeoutSeconds"
}
$script:CacheRoot = [IO.Path]::GetFullPath($CacheDirectory).TrimEnd('\')

Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;

namespace HerdrSandbox {
    public static class WinTrust {
        [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
        private struct WINTRUST_FILE_INFO {
            public uint cbStruct;
            public IntPtr pcwszFilePath;
            public IntPtr hFile;
            public IntPtr pgKnownSubject;
        }

        [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
        private struct WINTRUST_DATA {
            public uint cbStruct;
            public IntPtr pPolicyCallbackData;
            public IntPtr pSIPClientData;
            public uint dwUIChoice;
            public uint fdwRevocationChecks;
            public uint dwUnionChoice;
            public IntPtr pFile;
            public uint dwStateAction;
            public IntPtr hWVTStateData;
            public IntPtr pwszURLReference;
            public uint dwProvFlags;
            public uint dwUIContext;
        }

        [DllImport("wintrust.dll", ExactSpelling = true, SetLastError = true, CharSet = CharSet.Unicode)]
        private static extern int WinVerifyTrust(
            IntPtr hwnd,
            [MarshalAs(UnmanagedType.LPStruct)] Guid action,
            ref WINTRUST_DATA data);

        public static int Verify(string path) {
            IntPtr pathPointer = IntPtr.Zero;
            IntPtr fileInfoPointer = IntPtr.Zero;
            try {
                pathPointer = Marshal.StringToCoTaskMemUni(path);
                WINTRUST_FILE_INFO fileInfo = new WINTRUST_FILE_INFO();
                fileInfo.cbStruct = (uint)Marshal.SizeOf(typeof(WINTRUST_FILE_INFO));
                fileInfo.pcwszFilePath = pathPointer;
                fileInfo.hFile = IntPtr.Zero;
                fileInfo.pgKnownSubject = IntPtr.Zero;
                fileInfoPointer = Marshal.AllocHGlobal(Marshal.SizeOf(typeof(WINTRUST_FILE_INFO)));
                Marshal.StructureToPtr(fileInfo, fileInfoPointer, false);

                WINTRUST_DATA data = new WINTRUST_DATA();
                data.cbStruct = (uint)Marshal.SizeOf(typeof(WINTRUST_DATA));
                data.dwUIChoice = 2; // WTD_UI_NONE
                data.fdwRevocationChecks = 1; // WTD_REVOKE_WHOLECHAIN
                data.dwUnionChoice = 1; // WTD_CHOICE_FILE
                data.pFile = fileInfoPointer;
                data.dwStateAction = 0; // WTD_STATEACTION_IGNORE
                data.dwProvFlags = 0x80; // WTD_REVOCATION_CHECK_CHAIN_EXCLUDE_ROOT
                data.dwUIContext = 0; // WTD_UICONTEXT_EXECUTE
                Guid action = new Guid("00AAC56B-CD44-11D0-8CC2-00C04FC295EE");
                return WinVerifyTrust(new IntPtr(-1), action, ref data);
            } finally {
                if (fileInfoPointer != IntPtr.Zero) {
                    Marshal.DestroyStructure(fileInfoPointer, typeof(WINTRUST_FILE_INFO));
                    Marshal.FreeHGlobal(fileInfoPointer);
                }
                if (pathPointer != IntPtr.Zero) {
                    Marshal.FreeCoTaskMem(pathPointer);
                }
            }
        }
    }
}
'@

function Assert-HerdrHostCachePath {
    param([Parameter(Mandatory = $true)][string]$Path)

    $fullPath = [IO.Path]::GetFullPath($Path).TrimEnd('\')
    if ($fullPath -ine $script:CacheRoot -and
        -not $fullPath.StartsWith($script:CacheRoot + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw "Visual Studio path escapes the configured cache: $fullPath"
    }
    $current = $fullPath
    while (-not [string]::IsNullOrWhiteSpace($current)) {
        if (Test-Path -LiteralPath $current) {
            $item = Get-Item -LiteralPath $current -Force
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "Visual Studio cache path contains a reparse point: $current"
            }
        }
        if ($current -ieq $script:CacheRoot) { break }
        $parent = Split-Path -Parent $current
        if ([string]::IsNullOrWhiteSpace($parent) -or $parent -ieq $current) { break }
        $current = $parent
    }
}

function Assert-HerdrHostCacheTree {
    param([Parameter(Mandatory = $true)][string]$Path)

    Assert-HerdrHostCachePath -Path $Path
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
            Assert-HerdrHostCachePath -Path $item.FullName
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "Visual Studio cache tree contains a reparse point: $($item.FullName)"
            }
            if ($item.PSIsContainer) {
                $pending.Add($item.FullName) | Out-Null
            }
        }
    }
}

function Get-HerdrHostSHA256 {
    param([Parameter(Mandatory = $true)][string]$Path)

    $stream = [IO.File]::Open($Path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
    $sha256 = [Security.Cryptography.SHA256]::Create()
    try {
        return [BitConverter]::ToString($sha256.ComputeHash($stream)).Replace('-', '')
    } finally {
        $sha256.Dispose()
        $stream.Dispose()
    }
}

function Invoke-HerdrHostNative {
    param(
        [Parameter(Mandatory = $true)][string]$Role,
        [Parameter(Mandatory = $true)][string]$FilePath,
        [string[]]$ArgumentList = @()
    )

    $stopwatch = [Diagnostics.Stopwatch]::StartNew()
    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $output = @(& $FilePath @ArgumentList 2>&1 | ForEach-Object { [string]$_ })
        $exitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previousErrorActionPreference
        $stopwatch.Stop()
    }
    Write-Host ('[timing] {0}: {1:N1}s' -f $Role, $stopwatch.Elapsed.TotalSeconds)
    if ($exitCode -ne 0) {
        $detail = ($output -join [Environment]::NewLine).Trim()
        if ($detail.Length -gt 1600) { $detail = $detail.Substring($detail.Length - 1600) }
        throw "$Role failed with exit code $exitCode. $detail"
    }
    return $output
}

function Get-HerdrHostVisualStudioTargetFromChannel {
    param(
        [Parameter(Mandatory = $true)][object]$Channel,
        [Parameter(Mandatory = $true)][string]$SourceDescription
    )

    $channelID = [string]$Channel.info.id
    if ($channelID -notmatch '^VisualStudio\.(?<major>\d+)\.Release(?:/.+)?$') {
        throw "Visual Studio channel identity is unexpected: $SourceDescription"
    }
    $productMajor = [string]$Matches['major']
    $channelName = ($channelID -split '/', 2)[0]
    if ([string]$Channel.manifestVersion -cne '1.1' -or
        [string]$Channel.info.manifestName -cne $channelName -or
        [string]$Channel.info.manifestType -cne 'channel' -or
        [string]$Channel.info.productLine -cne "Dev$productMajor" -or
        [string]$Channel.info.productLineVersion -notmatch '^[1-9][0-9]*$' -or
        [string]$Channel.info.productMilestone -cne 'RTW' -or
        [string]$Channel.info.productMilestoneIsPreRelease -cne 'False') {
        throw "Visual Studio channel metadata is unexpected: $SourceDescription"
    }
    $products = @($Channel.channelItems | Where-Object {
        [string]$_.type -ceq 'ChannelProduct' -and
        [string]$_.id -ceq 'Microsoft.VisualStudio.Product.BuildTools'
    })
    $manifests = @($Channel.channelItems | Where-Object {
        [string]$_.type -ceq 'Manifest' -and
        [string]$_.id -ceq 'Microsoft.VisualStudio.Manifests.VisualStudio'
    })
    $setups = @($Channel.channelItems | Where-Object {
        [string]$_.type -ceq 'Bootstrapper' -and
        [string]$_.id -ceq "$channelName.Bootstrappers.Setup"
    })
    if ($products.Count -ne 1 -or $manifests.Count -ne 1 -or $setups.Count -ne 1) {
        throw "Visual Studio channel selection is ambiguous: $SourceDescription"
    }
    $catalogPayloads = @($manifests[0].payloads | Where-Object { [string]$_.fileName -ceq 'VisualStudio.vsman' })
    $setupPayloads = @($setups[0].payloads | Where-Object { [string]$_.fileName -ceq 'vs_Setup.exe' })
    if ($catalogPayloads.Count -ne 1 -or $setupPayloads.Count -ne 1) {
        throw "Visual Studio channel payload selection is ambiguous: $SourceDescription"
    }
    $buildVersion = [string]$Channel.info.buildVersion
    $semanticVersion = [string]$Channel.info.productSemanticVersion
    if ([string]::IsNullOrWhiteSpace($buildVersion) -or
        [string]::IsNullOrWhiteSpace($semanticVersion) -or
        [string]$products[0].version -cne $buildVersion -or
        [string]$manifests[0].version -cne $buildVersion) {
        throw "Visual Studio channel versions disagree: $SourceDescription"
    }
    foreach ($payload in @($catalogPayloads[0], $setupPayloads[0])) {
        $uri = [Uri][string]$payload.url
        if ($uri.Scheme -cne 'https' -or $uri.Host -cne 'download.visualstudio.microsoft.com' -or
            [string]$payload.sha256 -notmatch '^[A-Fa-f0-9]{64}$') {
            throw "Visual Studio channel payload is unsafe: $($payload.fileName)"
        }
    }
    return [pscustomobject]@{
        ChannelID = $channelID
        ProductLine = [string]$Channel.info.productLine
        ProductLineVersion = [string]$Channel.info.productLineVersion
        BuildVersion = $buildVersion
        SemanticVersion = $semanticVersion
        ProductVersion = [string]$products[0].version
        CatalogSHA256 = ([string]$catalogPayloads[0].sha256).ToUpperInvariant()
        SetupVersion = [string]$setups[0].version
        SetupSHA256 = ([string]$setupPayloads[0].sha256).ToUpperInvariant()
    }
}

function Test-HerdrHostVisualStudioTargetEqual {
    param([object]$Left, [object]$Right)

    return [string]$Left.ChannelID -ceq [string]$Right.ChannelID -and
        [string]$Left.ProductLine -ceq [string]$Right.ProductLine -and
        [string]$Left.ProductLineVersion -ceq [string]$Right.ProductLineVersion -and
        [string]$Left.BuildVersion -ceq [string]$Right.BuildVersion -and
        [string]$Left.SemanticVersion -ceq [string]$Right.SemanticVersion -and
        [string]$Left.ProductVersion -ceq [string]$Right.ProductVersion -and
        [string]$Left.CatalogSHA256 -ceq [string]$Right.CatalogSHA256 -and
        [string]$Left.SetupVersion -ceq [string]$Right.SetupVersion -and
        [string]$Left.SetupSHA256 -ceq [string]$Right.SetupSHA256
}

function Get-HerdrHostVisualStudioTargetFromDescriptor {
    param(
        [Parameter(Mandatory = $true)][object]$Descriptor,
        [string]$Slot = ''
    )

    if ([int]$Descriptor.schemaVersion -eq 2) {
        if ([string]::IsNullOrWhiteSpace($Slot)) {
            throw 'Visual Studio schema-2 descriptor requires its cache slot.'
        }
        $channelPath = Join-Path (Join-Path $Slot 'layout') 'ChannelManifest.json'
        $target = Get-HerdrHostVisualStudioTargetFromChannel `
            -Channel ([IO.File]::ReadAllText($channelPath) | ConvertFrom-Json) -SourceDescription $channelPath
        if ([string]$Descriptor.channelID -cne $target.ChannelID -or
            [string]$Descriptor.buildVersion -cne $target.BuildVersion -or
            [string]$Descriptor.semanticVersion -cne $target.SemanticVersion -or
            [string]$Descriptor.productVersion -cne $target.ProductVersion -or
            [string]$Descriptor.catalogSHA256 -cne $target.CatalogSHA256 -or
            [string]$Descriptor.setupVersion -cne $target.SetupVersion -or
            [string]$Descriptor.setupSHA256 -cne $target.SetupSHA256) {
            throw 'Visual Studio schema-2 descriptor does not match its signed channel.'
        }
        return $target
    }
    if ([int]$Descriptor.schemaVersion -ne 3) {
        throw "Visual Studio descriptor schema is unsupported: $($Descriptor.schemaVersion)"
    }

    return [pscustomobject]@{
        ChannelID = [string]$Descriptor.channelID
        ProductLine = [string]$Descriptor.productLine
        ProductLineVersion = [string]$Descriptor.productLineVersion
        BuildVersion = [string]$Descriptor.buildVersion
        SemanticVersion = [string]$Descriptor.semanticVersion
        ProductVersion = [string]$Descriptor.productVersion
        CatalogSHA256 = [string]$Descriptor.catalogSHA256
        SetupVersion = [string]$Descriptor.setupVersion
        SetupSHA256 = [string]$Descriptor.setupSHA256
    }
}

function Assert-HerdrHostVisualStudioBootstrapper {
    param([string]$Path, [string]$ExpectedSHA256)

    $actualHash = Get-HerdrHostSHA256 -Path $Path
    if ($actualHash -cne $ExpectedSHA256) { throw "Visual Studio bootstrapper hash mismatch: $actualHash" }
    $trustStatus = [HerdrSandbox.WinTrust]::Verify($Path)
    if ($trustStatus -ne 0) {
        throw ('Visual Studio bootstrapper WinVerifyTrust failed: 0x{0:X8}' -f $trustStatus)
    }
    $signedFileCertificate = [Security.Cryptography.X509Certificates.X509Certificate]::CreateFromSignedFile($Path)
    $certificate = New-Object -TypeName Security.Cryptography.X509Certificates.X509Certificate2 `
        -ArgumentList @($signedFileCertificate)
    try {
        $publisher = $certificate.GetNameInfo(
            [Security.Cryptography.X509Certificates.X509NameType]::SimpleName, $false)
        if ($publisher -cne 'Microsoft Corporation' -or
            $certificate.Subject -notmatch '(^|,\s*)O=Microsoft Corporation(,|$)') {
            throw "Unexpected Visual Studio bootstrapper publisher: $publisher"
        }
        $eku = @($certificate.Extensions |
            Where-Object { $_ -is [Security.Cryptography.X509Certificates.X509EnhancedKeyUsageExtension] } |
            ForEach-Object { $_.EnhancedKeyUsages } | ForEach-Object { $_.Value })
        if ('1.3.6.1.5.5.7.3.3' -notin $eku) {
            throw 'Visual Studio bootstrapper certificate lacks the Code Signing EKU.'
        }
    } finally {
        $certificate.Dispose()
        $signedFileCertificate.Dispose()
    }
}

function Get-HerdrHostVisualStudioPackageMetadata {
    $winget = Get-Command 'winget.exe' -CommandType Application -ErrorAction Stop | Select-Object -First 1
    $lines = @(Invoke-HerdrHostNative -Role 'Visual Studio Build Tools metadata resolution' `
        -FilePath ([string]$winget.Source) -ArgumentList @(
            'show', '--id', 'Microsoft.VisualStudio.BuildTools', '--exact', '--source', 'winget',
            '--architecture', 'x64', '--accept-source-agreements', '--disable-interactivity'))
    $versions = @($lines | Where-Object { $_ -match '^Version:\s*(\S+)\s*$' } | ForEach-Object { $Matches[1] })
    $urls = @($lines | Where-Object { $_ -match '^\s*Installer Url:\s*(\S+)\s*$' } | ForEach-Object { $Matches[1] })
    $digests = @($lines | Where-Object { $_ -match '^\s*Installer SHA256:\s*([A-Fa-f0-9]{64})\s*$' } |
        ForEach-Object { $Matches[1].ToUpperInvariant() })
    if ($versions.Count -ne 1 -or $urls.Count -ne 1 -or $digests.Count -ne 1 -or
        [string]$versions[0] -notmatch '^\d+\.\d+\.\d+$') {
        throw 'Visual Studio Build Tools metadata is incomplete.'
    }
    $uri = [Uri][string]$urls[0]
    if ($uri.Scheme -cne 'https' -or $uri.Host -cne 'download.visualstudio.microsoft.com' -or
        $uri.AbsolutePath -notmatch '/[A-Fa-f0-9]{64}/vs_BuildTools\.exe$') {
        throw "Visual Studio Build Tools metadata URL is unsafe: $uri"
    }
    return [pscustomobject]@{ Version = [string]$versions[0]; Url = [string]$uri.AbsoluteUri; SHA256 = [string]$digests[0] }
}

function Save-HerdrHostVisualStudioBootstrapper {
    param([string]$Destination, [Parameter(Mandatory = $true)][object]$Metadata)

    Invoke-WebRequest -Uri ([string]$Metadata.Url) -OutFile $Destination -UseBasicParsing
    Assert-HerdrHostVisualStudioBootstrapper -Path $Destination -ExpectedSHA256 ([string]$Metadata.SHA256)
    return [pscustomobject]@{ Version = [string]$Metadata.Version; Url = [string]$Metadata.Url; SHA256 = [string]$Metadata.SHA256 }
}

function Publish-HerdrHostVisualStudioBootstrapper {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination,
        [Parameter(Mandatory = $true)][string]$ExpectedSHA256
    )

    Assert-HerdrHostVisualStudioBootstrapper -Path $Source -ExpectedSHA256 $ExpectedSHA256
    $directory = Split-Path -Parent $Destination
    New-Item -ItemType Directory -Path $directory -Force | Out-Null
    Assert-HerdrHostCachePath -Path $directory
    Assert-HerdrHostCachePath -Path $Destination
    if (Test-Path -LiteralPath $Destination -PathType Leaf) {
        try {
            Assert-HerdrHostVisualStudioBootstrapper -Path $Destination -ExpectedSHA256 $ExpectedSHA256
            return $Destination
        } catch {
            # Replace only after a complete staged copy below passes the same trust checks.
        }
    } elseif (Test-Path -LiteralPath $Destination) {
        throw "Stable Visual Studio bootstrapper path is not a file: $Destination"
    }

    $temporary = $Destination + '.new-' + [Guid]::NewGuid().ToString('N')
    $backup = $null
    Assert-HerdrHostCachePath -Path $temporary
    try {
        [IO.File]::Copy($Source, $temporary, $false)
        Assert-HerdrHostVisualStudioBootstrapper -Path $temporary -ExpectedSHA256 $ExpectedSHA256
        if (Test-Path -LiteralPath $Destination -PathType Leaf) {
            $backup = $Destination + '.old-' + [Guid]::NewGuid().ToString('N')
            Assert-HerdrHostCachePath -Path $backup
            [IO.File]::Replace($temporary, $Destination, $backup, $true)
        } else {
            [IO.File]::Move($temporary, $Destination)
        }
        Assert-HerdrHostVisualStudioBootstrapper -Path $Destination -ExpectedSHA256 $ExpectedSHA256
        return $Destination
    } finally {
        if (Test-Path -LiteralPath $temporary) {
            Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
        }
        if (-not [string]::IsNullOrWhiteSpace($backup) -and (Test-Path -LiteralPath $backup)) {
            Remove-Item -LiteralPath $backup -Force -ErrorAction SilentlyContinue
        }
    }
}

function Get-HerdrHostVisualStudioRequiredArtifacts {
    return @(
        'vs_BuildTools.exe', 'layout.json', 'response.json', 'Catalog.json', 'ChannelManifest.json',
        'vs_installer.opc', 'vs_installer.version.json',
        'Certificates\manifestRootCertificate.cer',
        'Certificates\manifestCounterSignRootCertificate.cer',
        'Certificates\vs_installer_opc.RootCertificate.cer'
    )
}

function Get-HerdrHostVisualStudioComponentIDs {
    param([Parameter(Mandatory = $true)][string]$CatalogPath)

    if (-not (Test-Path -LiteralPath $CatalogPath -PathType Leaf)) {
        throw "Visual Studio catalog is missing: $CatalogPath"
    }
    $catalog = [IO.File]::ReadAllText($CatalogPath) | ConvertFrom-Json
    $sdkIDs = @($catalog.packages | ForEach-Object { [string]$_.id } |
        Where-Object { $_ -match '^Microsoft\.VisualStudio\.Component\.Windows11SDK\.\d+$' } |
        Sort-Object -Unique)
    $ranked = @($sdkIDs | ForEach-Object {
            if ($_ -match '\.(?<build>\d+)$') {
                [pscustomobject]@{ ID = $_; Build = [long]$Matches['build'] }
            }
        } | Sort-Object @{ Expression = 'Build'; Descending = $true }, @{ Expression = 'ID'; Descending = $true })
    if ($ranked.Count -eq 0) { throw 'Visual Studio catalog contains no stable Windows 11 SDK component.' }
    return @(
        'Microsoft.VisualStudio.Component.VC.Tools.x86.x64',
        [string]$ranked[0].ID
    )
}

function Assert-HerdrHostVisualStudioLayoutIdentity {
    param([string]$Layout, [object]$Target)

    Assert-HerdrHostCachePath -Path $Layout
    $catalogPath = Join-Path $Layout 'Catalog.json'
    $channelPath = Join-Path $Layout 'ChannelManifest.json'
    $layoutPath = Join-Path $Layout 'layout.json'
    foreach ($path in @($catalogPath, $channelPath, $layoutPath)) {
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Visual Studio identity file is missing: $path" }
        Assert-HerdrHostCachePath -Path $path
    }
    $localChannel = [IO.File]::ReadAllText($channelPath) | ConvertFrom-Json
    $localTarget = Get-HerdrHostVisualStudioTargetFromChannel -Channel $localChannel -SourceDescription $channelPath
    if (-not (Test-HerdrHostVisualStudioTargetEqual -Left $Target -Right $localTarget)) {
        throw 'Visual Studio local channel does not match Current.'
    }
    $catalog = [IO.File]::ReadAllText($catalogPath) | ConvertFrom-Json
    if ([string]$catalog.info.manifestName -cne 'VisualStudio' -or
        [string]$catalog.info.manifestType -cne 'installer' -or
        [string]::IsNullOrWhiteSpace([string]$catalog.engineVersion) -or
        [string]$catalog.info.buildVersion -cne $Target.BuildVersion -or
        [string]$catalog.info.productSemanticVersion -cne $Target.SemanticVersion -or
        [string]$catalog.info.productLine -cne $Target.ProductLine -or
        [string]$catalog.info.productLineVersion -cne $Target.ProductLineVersion -or
        [string]$catalog.info.productMilestone -cne 'RTW' -or
        [string]$catalog.info.productMilestoneIsPreRelease -cne 'False') {
        throw 'Visual Studio catalog identity is unexpected.'
    }
    $layoutText = [IO.File]::ReadAllText($layoutPath)
    $layoutConfig = $layoutText | ConvertFrom-Json
    $archProperty = $layoutConfig.PSObject.Properties['arch']
    $targetChannelName = ([string]$Target.ChannelID -split '/', 2)[0]
    $expectedComponents = @(Get-HerdrHostVisualStudioComponentIDs -CatalogPath $catalogPath | Sort-Object)
    $actualComponents = @(@($layoutConfig.add) | ForEach-Object { [string]$_ } | Sort-Object)
    if ([string]$layoutConfig.channelId -cne $targetChannelName -or
        [string]$layoutConfig.productId -cne 'Microsoft.VisualStudio.Product.BuildTools' -or
        ($null -ne $archProperty -and [string]$archProperty.Value -cne 'x64') -or
        ($actualComponents -join '|') -cne ($expectedComponents -join '|') -or
        $layoutText -match 'Microsoft\.VisualStudio\.Workload\.' -or
        $layoutText -match 'includeRecommended|includeOptional') {
        throw 'Visual Studio layout configuration is unexpected.'
    }
}

function Test-HerdrHostVisualStudioLayoutSlot {
    param([string]$Slot, [object]$Target, [switch]$WarningOnFailure)

    try {
        $layout = Join-Path $Slot 'layout'
        $descriptorPath = Join-Path $Slot 'complete.json'
        if (-not (Test-Path -LiteralPath $descriptorPath -PathType Leaf)) { return $false }
        Assert-HerdrHostCachePath -Path $descriptorPath
        $descriptor = [IO.File]::ReadAllText($descriptorPath) | ConvertFrom-Json
        $currentProperties = @('artifacts', 'bootstrapperSHA256', 'bootstrapperURL', 'buildVersion',
            'catalogSHA256', 'channelID', 'componentIDs', 'packageVersion', 'productID', 'productLine',
            'productLineVersion', 'productVersion', 'schemaVersion', 'semanticVersion', 'setupSHA256', 'setupVersion')
        $previousProperties = @('artifacts', 'bootstrapperSHA256', 'bootstrapperURL', 'buildVersion',
            'catalogSHA256', 'channelID', 'componentIDs', 'productID', 'productVersion', 'schemaVersion',
            'semanticVersion', 'setupSHA256', 'setupVersion')
        $properties = if ([int]$descriptor.schemaVersion -eq 3) { $currentProperties } elseif ([int]$descriptor.schemaVersion -eq 2) { $previousProperties } else { @() }
        $descriptorTarget = Get-HerdrHostVisualStudioTargetFromDescriptor -Descriptor $descriptor -Slot $Slot
        $expectedComponents = @(Get-HerdrHostVisualStudioComponentIDs -CatalogPath (Join-Path $layout 'Catalog.json') | Sort-Object)
        $actualComponents = @(@($descriptor.componentIDs) | ForEach-Object { [string]$_ } | Sort-Object)
        if ($properties.Count -eq 0 -or
            (@($descriptor.PSObject.Properties.Name | Sort-Object) -join '|') -cne (($properties | Sort-Object) -join '|') -or
            -not (Test-HerdrHostVisualStudioTargetEqual -Left $descriptorTarget -Right $Target) -or
            [string]$descriptor.productID -cne 'Microsoft.VisualStudio.Product.BuildTools' -or
            ($actualComponents -join '|') -cne ($expectedComponents -join '|')) { return $false }
        $required = @(Get-HerdrHostVisualStudioRequiredArtifacts)
        if (@($descriptor.artifacts.PSObject.Properties).Count -ne $required.Count) { return $false }
        foreach ($relativePath in $required) {
            $path = Join-Path $layout $relativePath
            if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { return $false }
            Assert-HerdrHostCachePath -Path $path
            $property = $descriptor.artifacts.PSObject.Properties[$relativePath]
            if ($null -eq $property) { return $false }
            if ((Get-HerdrHostSHA256 -Path $path) -cne [string]$property.Value) {
                return $false
            }
        }
        Assert-HerdrHostVisualStudioBootstrapper -Path (Join-Path $layout 'vs_BuildTools.exe') `
            -ExpectedSHA256 ([string]$descriptor.bootstrapperSHA256)
        Assert-HerdrHostVisualStudioLayoutIdentity -Layout $layout -Target $Target
        return $true
    } catch {
        if ($WarningOnFailure) { Write-Warning "Visual Studio cache slot is unusable: $Slot`: $($_.Exception.Message)" }
        return $false
    }
}

function Test-HerdrHostStoredVisualStudioLayoutSlot {
    param([string]$Slot, [switch]$WarningOnFailure)

    try {
        $descriptorPath = Join-Path $Slot 'complete.json'
        if (-not (Test-Path -LiteralPath $descriptorPath -PathType Leaf)) { return $false }
        $descriptor = [IO.File]::ReadAllText($descriptorPath) | ConvertFrom-Json
        $target = Get-HerdrHostVisualStudioTargetFromDescriptor -Descriptor $descriptor -Slot $Slot
        return Test-HerdrHostVisualStudioLayoutSlot -Slot $Slot -Target $target -WarningOnFailure:$WarningOnFailure
    } catch {
        if ($WarningOnFailure) { Write-Warning "Visual Studio cache slot descriptor is unusable: $Slot`: $($_.Exception.Message)" }
        return $false
    }
}

function Wait-HerdrHostVisualStudioLayoutFiles {
    param([string]$Layout, [DateTime]$Deadline)

    $required = @(Get-HerdrHostVisualStudioRequiredArtifacts)
    do {
        $complete = $true
        foreach ($relativePath in $required) {
            $path = Join-Path $Layout $relativePath
            if (-not (Test-Path -LiteralPath $path -PathType Leaf) -or (Get-Item -LiteralPath $path).Length -eq 0) {
                $complete = $false
                break
            }
        }
        if ($complete) {
            Start-Sleep -Seconds 2
            $stable = $true
            foreach ($relativePath in $required) {
                $path = Join-Path $Layout $relativePath
                if (-not (Test-Path -LiteralPath $path -PathType Leaf) -or (Get-Item -LiteralPath $path).Length -eq 0) {
                    $stable = $false
                    break
                }
            }
            if ($stable) { return }
        }
        Start-Sleep -Seconds 1
    } while ([DateTime]::UtcNow -lt $Deadline)
    throw "Visual Studio layout files did not complete within $TimeoutSeconds seconds."
}

$cacheRoot = Join-Path $script:CacheRoot 'vsbt'
New-Item -ItemType Directory -Path $cacheRoot -Force | Out-Null
Assert-HerdrHostCachePath -Path $cacheRoot
$stableBootstrapper = Join-Path $cacheRoot 'bootstrapper\vs_BuildTools.exe'
$lockPath = Join-Path $cacheRoot '.lock'
$slotA = Join-Path $cacheRoot 'a'
$slotB = Join-Path $cacheRoot 'b'
$lock = $null
$stage = Join-Path $env:TEMP ('herdr-sandbox-vsbt-' + [Guid]::NewGuid().ToString('N'))
try {
    $lock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate,
        [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
    $packageMetadata = Get-HerdrHostVisualStudioPackageMetadata
    $matching = @(@($slotA, $slotB) | Where-Object {
        if (-not (Test-HerdrHostStoredVisualStudioLayoutSlot -Slot $_)) { return $false }
        $descriptor = [IO.File]::ReadAllText((Join-Path $_ 'complete.json')) | ConvertFrom-Json
        if ([int]$descriptor.schemaVersion -ne 3) { return $false }
        return [string]$descriptor.packageVersion -ceq $packageMetadata.Version -and
            [string]$descriptor.bootstrapperURL -ceq $packageMetadata.Url -and
            [string]$descriptor.bootstrapperSHA256 -ceq $packageMetadata.SHA256
    })
    if ($matching.Count -gt 0) {
        $selectedLayout = Join-Path $matching[0] 'layout'
        $selectedDescriptor = [IO.File]::ReadAllText((Join-Path $matching[0] 'complete.json')) | ConvertFrom-Json
        $target = Get-HerdrHostVisualStudioTargetFromDescriptor -Descriptor $selectedDescriptor
        $stableBootstrapper = Publish-HerdrHostVisualStudioBootstrapper `
            -Source (Join-Path $selectedLayout 'vs_BuildTools.exe') -Destination $stableBootstrapper `
            -ExpectedSHA256 ([string]$selectedDescriptor.bootstrapperSHA256)
        Invoke-HerdrHostNative -Role 'Visual Studio host cached layout verification' `
            -FilePath $stableBootstrapper `
            -ArgumentList @('--layout', $selectedLayout, '--verify', '--passive', '--wait') | Out-Null
        Assert-HerdrHostVisualStudioLayoutIdentity -Layout $selectedLayout -Target $target
        Write-Host "Visual Studio Build Tools host layout cache hit: $($target.BuildVersion)"
        exit 0
    }

    Write-Host "Visual Studio Build Tools host layout cache miss: $($packageMetadata.Version)"
    New-Item -ItemType Directory -Path $stage | Out-Null
    $downloadedBootstrapper = Join-Path $stage 'vs_BuildTools.exe'
    $bootstrapperInfo = Save-HerdrHostVisualStudioBootstrapper -Destination $downloadedBootstrapper `
        -Metadata $packageMetadata
    $stableBootstrapper = Publish-HerdrHostVisualStudioBootstrapper -Source $downloadedBootstrapper `
        -Destination $stableBootstrapper -ExpectedSHA256 $bootstrapperInfo.SHA256

    $aValid = Test-HerdrHostStoredVisualStudioLayoutSlot -Slot $slotA
    $bValid = Test-HerdrHostStoredVisualStudioLayoutSlot -Slot $slotB
    $selectedSlot = if (-not (Test-Path -LiteralPath $slotA)) {
        $slotA
    } elseif (-not (Test-Path -LiteralPath $slotB)) {
        $slotB
    } elseif ($aValid) {
        $slotB
    } elseif ($bValid) {
        $slotA
    } else {
        $slotA
    }
    if (Test-Path -LiteralPath $selectedSlot) {
        Assert-HerdrHostCacheTree -Path $selectedSlot
        Remove-Item -LiteralPath $selectedSlot -Recurse -Force
    }
    $layout = Join-Path $selectedSlot 'layout'
    New-Item -ItemType Directory -Path $layout -Force | Out-Null
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    $initialArguments = @('--layout', $layout, '--add', 'Microsoft.VisualStudio.Component.VC.Tools.x86.x64',
        '--lang', 'en-US', '--passive', '--wait')
    Invoke-HerdrHostNative -Role 'Visual Studio host catalog layout download' -FilePath $stableBootstrapper `
        -ArgumentList $initialArguments | Out-Null
    $layoutBootstrapper = Join-Path $layout 'vs_BuildTools.exe'
    if (-not (Test-Path -LiteralPath $layoutBootstrapper -PathType Leaf)) {
        Copy-Item -LiteralPath $stableBootstrapper -Destination $layoutBootstrapper
    }
    Wait-HerdrHostVisualStudioLayoutFiles -Layout $layout -Deadline $deadline
    Assert-HerdrHostVisualStudioBootstrapper -Path $layoutBootstrapper -ExpectedSHA256 $bootstrapperInfo.SHA256
    $target = Get-HerdrHostVisualStudioTargetFromChannel `
        -Channel ([IO.File]::ReadAllText((Join-Path $layout 'ChannelManifest.json')) | ConvertFrom-Json) `
        -SourceDescription (Join-Path $layout 'ChannelManifest.json')
    $componentIDs = @(Get-HerdrHostVisualStudioComponentIDs -CatalogPath (Join-Path $layout 'Catalog.json'))
    $layoutArguments = @('--layout', $layout)
    foreach ($componentID in $componentIDs) { $layoutArguments += @('--add', $componentID) }
    $layoutArguments += @('--lang', 'en-US', '--passive', '--wait')
    Invoke-HerdrHostNative -Role 'Visual Studio host complete layout download' -FilePath $stableBootstrapper `
        -ArgumentList $layoutArguments | Out-Null
    Wait-HerdrHostVisualStudioLayoutFiles -Layout $layout -Deadline $deadline
    Assert-HerdrHostVisualStudioLayoutIdentity -Layout $layout -Target $target
    Invoke-HerdrHostNative -Role 'Visual Studio host layout verification' -FilePath $stableBootstrapper `
        -ArgumentList @('--layout', $layout, '--verify', '--passive', '--wait') | Out-Null
    Assert-HerdrHostVisualStudioLayoutIdentity -Layout $layout -Target $target
    $packageAfterDownload = Get-HerdrHostVisualStudioPackageMetadata
    if ([string]$packageMetadata.Version -cne [string]$packageAfterDownload.Version -or
        [string]$packageMetadata.Url -cne [string]$packageAfterDownload.Url -or
        [string]$packageMetadata.SHA256 -cne [string]$packageAfterDownload.SHA256) {
        throw 'Visual Studio Current changed while the host layout was downloading.'
    }
    $artifactHashes = [ordered]@{}
    foreach ($relativePath in @(Get-HerdrHostVisualStudioRequiredArtifacts)) {
        $path = Join-Path $layout $relativePath
        $artifactHashes[$relativePath] = Get-HerdrHostSHA256 -Path $path
    }
    $descriptor = [ordered]@{
        schemaVersion = 3; packageVersion = $packageMetadata.Version; channelID = $target.ChannelID
        productLine = $target.ProductLine; productLineVersion = $target.ProductLineVersion
        buildVersion = $target.BuildVersion
        semanticVersion = $target.SemanticVersion; productVersion = $target.ProductVersion
        catalogSHA256 = $target.CatalogSHA256; setupVersion = $target.SetupVersion
        setupSHA256 = $target.SetupSHA256; bootstrapperURL = $bootstrapperInfo.Url
        bootstrapperSHA256 = $bootstrapperInfo.SHA256; productID = 'Microsoft.VisualStudio.Product.BuildTools'
        componentIDs = $componentIDs; artifacts = $artifactHashes
    } | ConvertTo-Json -Depth 4 -Compress
    $temporaryDescriptor = Join-Path $selectedSlot 'complete.json.tmp'
    [IO.File]::WriteAllText($temporaryDescriptor, $descriptor, (New-Object Text.UTF8Encoding($false)))
    Move-Item -LiteralPath $temporaryDescriptor -Destination (Join-Path $selectedSlot 'complete.json')
    if (-not (Test-HerdrHostVisualStudioLayoutSlot -Slot $selectedSlot -Target $target)) {
        throw 'Published host Visual Studio layout validation failed.'
    }
    Write-Host "Visual Studio Build Tools host layout ready: $($target.BuildVersion)"
} catch {
    $fallbackSlots = @(@($slotA, $slotB) | Where-Object { Test-HerdrHostStoredVisualStudioLayoutSlot -Slot $_ -WarningOnFailure })
    if ($fallbackSlots.Count -gt 0) {
        $fallbackDescriptor = [IO.File]::ReadAllText((Join-Path $fallbackSlots[0] 'complete.json')) | ConvertFrom-Json
        Write-Warning "Visual Studio Current layout preparation failed; provisioning will continue with verified cached layout $($fallbackDescriptor.buildVersion): $($_.Exception.Message)"
        return
    }
    throw
} finally {
    if ($null -ne $lock) { $lock.Dispose() }
    if (Test-Path -LiteralPath $stage) {
        Remove-Item -LiteralPath $stage -Recurse -Force -ErrorAction SilentlyContinue
    }
}
