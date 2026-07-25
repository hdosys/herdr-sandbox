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

function Get-HerdrHostWebResponseText {
    param([Parameter(Mandatory = $true)][object]$Response)

    if ($null -eq $Response.Content) { throw 'Visual Studio channel response content is empty.' }
    if ($Response.Content -is [byte[]]) {
        return [Text.Encoding]::UTF8.GetString([byte[]]$Response.Content)
    }
    if ($Response.Content -is [string]) { return [string]$Response.Content }
    throw "Unexpected Visual Studio response content type: $($Response.Content.GetType().FullName)"
}

function Get-HerdrHostVisualStudioTargetFromChannel {
    param(
        [Parameter(Mandatory = $true)][object]$Channel,
        [Parameter(Mandatory = $true)][string]$SourceDescription
    )

    if ([string]$Channel.manifestVersion -cne '1.1' -or
        [string]$Channel.info.manifestName -cne 'VisualStudio.17.Release' -or
        [string]$Channel.info.manifestType -cne 'channel' -or
        [string]$Channel.info.productLine -cne 'Dev17' -or
        [string]$Channel.info.productLineVersion -cne '2022' -or
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
        [string]$_.id -ceq 'VisualStudio.17.Release.Bootstrappers.Setup'
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
        ChannelID = [string]$Channel.info.id
        BuildVersion = $buildVersion
        SemanticVersion = $semanticVersion
        ProductVersion = [string]$products[0].version
        CatalogSHA256 = ([string]$catalogPayloads[0].sha256).ToUpperInvariant()
        SetupVersion = [string]$setups[0].version
        SetupSHA256 = ([string]$setupPayloads[0].sha256).ToUpperInvariant()
    }
}

function Get-HerdrHostVisualStudioCurrentTarget {
    $uri = 'https://aka.ms/vs/17/release/channel'
    $response = Invoke-WebRequest -Uri $uri -UseBasicParsing -ErrorAction Stop
    $channel = (Get-HerdrHostWebResponseText -Response $response) | ConvertFrom-Json
    return Get-HerdrHostVisualStudioTargetFromChannel -Channel $channel -SourceDescription $uri
}

function Test-HerdrHostVisualStudioTargetEqual {
    param([object]$Left, [object]$Right)

    return [string]$Left.ChannelID -ceq [string]$Right.ChannelID -and
        [string]$Left.BuildVersion -ceq [string]$Right.BuildVersion -and
        [string]$Left.SemanticVersion -ceq [string]$Right.SemanticVersion -and
        [string]$Left.ProductVersion -ceq [string]$Right.ProductVersion -and
        [string]$Left.CatalogSHA256 -ceq [string]$Right.CatalogSHA256 -and
        [string]$Left.SetupVersion -ceq [string]$Right.SetupVersion -and
        [string]$Left.SetupSHA256 -ceq [string]$Right.SetupSHA256
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

function Save-HerdrHostVisualStudioBootstrapper {
    param([string]$Destination)

    $request = [Net.HttpWebRequest]::Create('https://aka.ms/vs/17/release/vs_buildtools.exe')
    $request.AllowAutoRedirect = $true
    $request.MaximumAutomaticRedirections = 5
    $request.UserAgent = 'herdr-sandbox'
    $response = $null
    $inputStream = $null
    $outputStream = $null
    try {
        $response = $request.GetResponse()
        $finalURI = [Uri]$response.ResponseUri
        if ($finalURI.Scheme -cne 'https' -or $finalURI.Host -cne 'download.visualstudio.microsoft.com' -or
            $finalURI.AbsolutePath -notmatch '/([A-Fa-f0-9]{64})/vs_BuildTools\.exe$') {
            throw "Visual Studio evergreen bootstrapper redirected to an unsafe URI: $finalURI"
        }
        $expectedHash = $Matches[1].ToUpperInvariant()
        $inputStream = $response.GetResponseStream()
        $outputStream = [IO.File]::Open($Destination, [IO.FileMode]::CreateNew,
            [IO.FileAccess]::Write, [IO.FileShare]::None)
        $inputStream.CopyTo($outputStream)
        $outputStream.Flush()
    } finally {
        if ($null -ne $outputStream) { $outputStream.Dispose() }
        if ($null -ne $inputStream) { $inputStream.Dispose() }
        if ($null -ne $response) { $response.Dispose() }
    }
    Assert-HerdrHostVisualStudioBootstrapper -Path $Destination -ExpectedSHA256 $expectedHash
    return [pscustomobject]@{ Url = [string]$finalURI.AbsoluteUri; SHA256 = $expectedHash }
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
    Assert-HerdrHostCachePath -Path $temporary
    try {
        [IO.File]::Copy($Source, $temporary, $false)
        Assert-HerdrHostVisualStudioBootstrapper -Path $temporary -ExpectedSHA256 $ExpectedSHA256
        if (Test-Path -LiteralPath $Destination -PathType Leaf) {
            [IO.File]::Replace($temporary, $Destination, $null, $true)
        } else {
            [IO.File]::Move($temporary, $Destination)
        }
        Assert-HerdrHostVisualStudioBootstrapper -Path $Destination -ExpectedSHA256 $ExpectedSHA256
        return $Destination
    } finally {
        if (Test-Path -LiteralPath $temporary) {
            Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
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
    return @(
        'Microsoft.VisualStudio.Component.VC.Tools.x86.x64',
        'Microsoft.VisualStudio.Component.Windows11SDK.26100'
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
        [string]$catalog.info.productLine -cne 'Dev17' -or
        [string]$catalog.info.productLineVersion -cne '2022' -or
        [string]$catalog.info.productMilestone -cne 'RTW' -or
        [string]$catalog.info.productMilestoneIsPreRelease -cne 'False') {
        throw 'Visual Studio catalog identity is unexpected.'
    }
    $layoutText = [IO.File]::ReadAllText($layoutPath)
    $layoutConfig = $layoutText | ConvertFrom-Json
    $archProperty = $layoutConfig.PSObject.Properties['arch']
    $expectedComponents = @(Get-HerdrHostVisualStudioComponentIDs | Sort-Object)
    $actualComponents = @(@($layoutConfig.add) | ForEach-Object { [string]$_ } | Sort-Object)
    if ([string]$layoutConfig.channelId -cne 'VisualStudio.17.Release' -or
        [string]$layoutConfig.productId -cne 'Microsoft.VisualStudio.Product.BuildTools' -or
        ($null -ne $archProperty -and [string]$archProperty.Value -cne 'x64') -or
        ($actualComponents -join '|') -cne ($expectedComponents -join '|') -or
        $layoutText -match 'Microsoft\.VisualStudio\.Workload\.' -or
        $layoutText -match 'includeRecommended|includeOptional') {
        throw 'Visual Studio layout configuration is unexpected.'
    }
}

function Test-HerdrHostVisualStudioLayoutSlot {
    param([string]$Slot, [object]$Target)

    try {
        $layout = Join-Path $Slot 'layout'
        $descriptorPath = Join-Path $Slot 'complete.json'
        if (-not (Test-Path -LiteralPath $descriptorPath -PathType Leaf)) { return $false }
        Assert-HerdrHostCachePath -Path $descriptorPath
        $descriptor = [IO.File]::ReadAllText($descriptorPath) | ConvertFrom-Json
        $properties = @('artifacts', 'bootstrapperSHA256', 'bootstrapperURL', 'buildVersion',
            'catalogSHA256', 'channelID', 'componentIDs', 'productID', 'productVersion',
            'schemaVersion', 'semanticVersion', 'setupSHA256', 'setupVersion')
        $expectedComponents = @(Get-HerdrHostVisualStudioComponentIDs | Sort-Object)
        $actualComponents = @(@($descriptor.componentIDs) | ForEach-Object { [string]$_ } | Sort-Object)
        if ((@($descriptor.PSObject.Properties.Name | Sort-Object) -join '|') -cne (($properties | Sort-Object) -join '|') -or
            [int]$descriptor.schemaVersion -ne 2 -or [string]$descriptor.channelID -cne $Target.ChannelID -or
            [string]$descriptor.buildVersion -cne $Target.BuildVersion -or
            [string]$descriptor.semanticVersion -cne $Target.SemanticVersion -or
            [string]$descriptor.productVersion -cne $Target.ProductVersion -or
            [string]$descriptor.catalogSHA256 -cne $Target.CatalogSHA256 -or
            [string]$descriptor.setupVersion -cne $Target.SetupVersion -or
            [string]$descriptor.setupSHA256 -cne $Target.SetupSHA256 -or
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
    } catch { return $false }
}

function Test-HerdrHostUnpublishedVisualStudioLayoutSlot {
    param([string]$Slot, [object]$Target)

    try {
        if (Test-Path -LiteralPath (Join-Path $Slot 'complete.json')) { return $false }
        $layout = Join-Path $Slot 'layout'
        foreach ($relativePath in @(Get-HerdrHostVisualStudioRequiredArtifacts)) {
            $path = Join-Path $layout $relativePath
            if (-not (Test-Path -LiteralPath $path -PathType Leaf) -or (Get-Item -LiteralPath $path).Length -eq 0) {
                return $false
            }
            Assert-HerdrHostCachePath -Path $path
        }
        Assert-HerdrHostVisualStudioLayoutIdentity -Layout $layout -Target $Target
        return $true
    } catch { return $false }
}

function Test-HerdrHostStoredVisualStudioLayoutSlot {
    param([string]$Slot)

    try {
        $descriptorPath = Join-Path $Slot 'complete.json'
        if (-not (Test-Path -LiteralPath $descriptorPath -PathType Leaf)) { return $false }
        $descriptor = [IO.File]::ReadAllText($descriptorPath) | ConvertFrom-Json
        $target = [pscustomobject]@{
            ChannelID = [string]$descriptor.channelID; BuildVersion = [string]$descriptor.buildVersion
            SemanticVersion = [string]$descriptor.semanticVersion; ProductVersion = [string]$descriptor.productVersion
            CatalogSHA256 = [string]$descriptor.catalogSHA256; SetupVersion = [string]$descriptor.setupVersion
            SetupSHA256 = [string]$descriptor.setupSHA256
        }
        return Test-HerdrHostVisualStudioLayoutSlot -Slot $Slot -Target $target
    } catch { return $false }
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
$lock = $null
$stage = Join-Path $env:TEMP ('herdr-sandbox-vsbt-' + [Guid]::NewGuid().ToString('N'))
try {
    $lock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate,
        [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
    $target = Get-HerdrHostVisualStudioCurrentTarget
    $slotA = Join-Path $cacheRoot 'a'
    $slotB = Join-Path $cacheRoot 'b'
    $matching = @(@($slotA, $slotB) | Where-Object {
        Test-HerdrHostVisualStudioLayoutSlot -Slot $_ -Target $target
    })
    if ($matching.Count -gt 0) {
        $selectedLayout = Join-Path $matching[0] 'layout'
        $selectedDescriptor = [IO.File]::ReadAllText((Join-Path $matching[0] 'complete.json')) | ConvertFrom-Json
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

    Write-Host "Visual Studio Build Tools host layout cache miss: $($target.BuildVersion)"
    New-Item -ItemType Directory -Path $stage | Out-Null
    $downloadedBootstrapper = Join-Path $stage 'vs_BuildTools.exe'
    $bootstrapperInfo = Save-HerdrHostVisualStudioBootstrapper -Destination $downloadedBootstrapper
    $stableBootstrapper = Publish-HerdrHostVisualStudioBootstrapper -Source $downloadedBootstrapper `
        -Destination $stableBootstrapper -ExpectedSHA256 $bootstrapperInfo.SHA256
    $recoverable = @(@($slotA, $slotB) | Where-Object {
        Test-HerdrHostUnpublishedVisualStudioLayoutSlot -Slot $_ -Target $target
    })
    if ($recoverable.Count -gt 0) {
        $selectedSlot = $recoverable[0]
        $layout = Join-Path $selectedSlot 'layout'
        $layoutBootstrapper = Join-Path $layout 'vs_BuildTools.exe'
        Assert-HerdrHostVisualStudioBootstrapper -Path $layoutBootstrapper -ExpectedSHA256 $bootstrapperInfo.SHA256
        Write-Host "Recovering completed unpublished Visual Studio layout: $($target.BuildVersion)"
        Invoke-HerdrHostNative -Role 'Visual Studio host recovered layout verification' -FilePath $stableBootstrapper `
            -ArgumentList @('--layout', $layout, '--verify', '--passive', '--wait') | Out-Null
    } else {
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
            throw 'Both Visual Studio layout slots exist but neither matches the active component contract.'
        }
        if (Test-Path -LiteralPath $selectedSlot) {
            Assert-HerdrHostCachePath -Path $selectedSlot
            Remove-Item -LiteralPath $selectedSlot -Recurse -Force
        }
        $layout = Join-Path $selectedSlot 'layout'
        New-Item -ItemType Directory -Path $layout -Force | Out-Null
        $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
        $layoutArguments = @('--layout', $layout)
        foreach ($componentID in @(Get-HerdrHostVisualStudioComponentIDs)) {
            $layoutArguments += @('--add', $componentID)
        }
        $layoutArguments += @('--lang', 'en-US', '--passive', '--wait')
        Invoke-HerdrHostNative -Role 'Visual Studio host layout download' -FilePath $stableBootstrapper `
            -ArgumentList $layoutArguments | Out-Null
        $layoutBootstrapper = Join-Path $layout 'vs_BuildTools.exe'
        if (-not (Test-Path -LiteralPath $layoutBootstrapper -PathType Leaf)) {
            Copy-Item -LiteralPath $stableBootstrapper -Destination $layoutBootstrapper
        }
        Wait-HerdrHostVisualStudioLayoutFiles -Layout $layout -Deadline $deadline
        Assert-HerdrHostVisualStudioBootstrapper -Path $layoutBootstrapper -ExpectedSHA256 $bootstrapperInfo.SHA256
        Assert-HerdrHostVisualStudioLayoutIdentity -Layout $layout -Target $target
        Invoke-HerdrHostNative -Role 'Visual Studio host layout verification' -FilePath $stableBootstrapper `
            -ArgumentList @('--layout', $layout, '--verify', '--passive', '--wait') | Out-Null
    }
    Assert-HerdrHostVisualStudioLayoutIdentity -Layout $layout -Target $target
    $currentAfterDownload = Get-HerdrHostVisualStudioCurrentTarget
    if (-not (Test-HerdrHostVisualStudioTargetEqual -Left $target -Right $currentAfterDownload)) {
        throw 'Visual Studio Current changed while the host layout was downloading.'
    }
    $artifactHashes = [ordered]@{}
    foreach ($relativePath in @(Get-HerdrHostVisualStudioRequiredArtifacts)) {
        $path = Join-Path $layout $relativePath
        $artifactHashes[$relativePath] = Get-HerdrHostSHA256 -Path $path
    }
    $descriptor = [ordered]@{
        schemaVersion = 2; channelID = $target.ChannelID; buildVersion = $target.BuildVersion
        semanticVersion = $target.SemanticVersion; productVersion = $target.ProductVersion
        catalogSHA256 = $target.CatalogSHA256; setupVersion = $target.SetupVersion
        setupSHA256 = $target.SetupSHA256; bootstrapperURL = $bootstrapperInfo.Url
        bootstrapperSHA256 = $bootstrapperInfo.SHA256; productID = 'Microsoft.VisualStudio.Product.BuildTools'
        componentIDs = @(Get-HerdrHostVisualStudioComponentIDs); artifacts = $artifactHashes
    } | ConvertTo-Json -Depth 4 -Compress
    $temporaryDescriptor = Join-Path $selectedSlot 'complete.json.tmp'
    [IO.File]::WriteAllText($temporaryDescriptor, $descriptor, (New-Object Text.UTF8Encoding($false)))
    Move-Item -LiteralPath $temporaryDescriptor -Destination (Join-Path $selectedSlot 'complete.json')
    if (-not (Test-HerdrHostVisualStudioLayoutSlot -Slot $selectedSlot -Target $target)) {
        throw 'Published host Visual Studio layout validation failed.'
    }
    Write-Host "Visual Studio Build Tools host layout ready: $($target.BuildVersion)"
} finally {
    if ($null -ne $lock) { $lock.Dispose() }
    if (Test-Path -LiteralPath $stage) {
        Remove-Item -LiteralPath $stage -Recurse -Force -ErrorAction SilentlyContinue
    }
}
