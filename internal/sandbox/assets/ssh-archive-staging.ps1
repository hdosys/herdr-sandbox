function Assert-GuestArchivePath {
    param([Parameter(Mandatory = $true)][string]$Path)
    $current = [IO.Path]::GetFullPath($Path)
    while ($true) {
        $item = Get-Item -LiteralPath $current -Force -ErrorAction SilentlyContinue
        if ($null -ne $item) {
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "$stagingRole staging contains a reparse point: $current"
            }
        }
        $parent = [IO.Directory]::GetParent($current)
        if ($null -eq $parent) { break }
        $current = $parent.FullName
    }
}
function Assert-GuestArchiveTree {
    Assert-GuestArchivePath -Path $transferRoot
    if (Test-Path -LiteralPath $transferRoot) {
        $item = Get-Item -LiteralPath $transferRoot -Force -ErrorAction Stop
        if (-not $item.PSIsContainer) { throw "$stagingRole staging root is not a directory." }
        $pending = New-Object 'System.Collections.Generic.List[string]'
        $pending.Add([IO.Path]::GetFullPath($transferRoot)) | Out-Null
        while ($pending.Count -gt 0) {
            $index = $pending.Count - 1
            $directory = $pending[$index]
            $pending.RemoveAt($index)
            foreach ($child in @(Get-ChildItem -LiteralPath $directory -Force -ErrorAction Stop)) {
                if (($child.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                    throw "$stagingRole staging tree contains a reparse point: $($child.FullName)"
                }
                if ($child.PSIsContainer) { $pending.Add($child.FullName) | Out-Null }
            }
        }
    }
}
function Remove-GuestArchiveStaging {
    Assert-GuestArchivePath -Path $stagingRoot
    $rootItem = Get-Item -LiteralPath $transferRoot -Force -ErrorAction SilentlyContinue
    if ($null -eq $rootItem) { return }
    if (($rootItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        if ($rootItem.PSIsContainer) {
            [IO.Directory]::Delete($rootItem.FullName, $false)
        } else {
            [IO.File]::Delete($rootItem.FullName)
        }
    } elseif (-not $rootItem.PSIsContainer) {
        [IO.File]::SetAttributes($rootItem.FullName, [IO.FileAttributes]::Normal)
        [IO.File]::Delete($rootItem.FullName)
    } else {
        $pending = New-Object 'System.Collections.Generic.List[string]'
        $directories = New-Object 'System.Collections.Generic.List[string]'
        $pending.Add($rootItem.FullName) | Out-Null
        while ($pending.Count -gt 0) {
            $index = $pending.Count - 1
            $directory = $pending[$index]
            $pending.RemoveAt($index)
            $item = Get-Item -LiteralPath $directory -Force -ErrorAction Stop
            if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                [IO.Directory]::Delete($item.FullName, $false)
                continue
            }
            if (-not $item.PSIsContainer) { throw "$stagingRole staging directory changed type: $directory" }
            $directories.Add($item.FullName) | Out-Null
            foreach ($child in @(Get-ChildItem -LiteralPath $item.FullName -Force -ErrorAction Stop)) {
                if (($child.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
                    if ($child.PSIsContainer) {
                        [IO.Directory]::Delete($child.FullName, $false)
                    } else {
                        [IO.File]::Delete($child.FullName)
                    }
                } elseif ($child.PSIsContainer) {
                    $pending.Add($child.FullName) | Out-Null
                } else {
                    [IO.File]::SetAttributes($child.FullName, [IO.FileAttributes]::Normal)
                    [IO.File]::Delete($child.FullName)
                }
            }
        }
        for ($index = $directories.Count - 1; $index -ge 0; $index--) {
            [IO.Directory]::Delete($directories[$index], $false)
        }
    }
    if ($null -ne (Get-Item -LiteralPath $transferRoot -Force -ErrorAction SilentlyContinue)) {
        throw "$stagingRole staging cleanup did not remove all input."
    }
}
Assert-GuestArchivePath -Path $stagingRoot
if (-not (Test-Path -LiteralPath $stagingRoot -PathType Container)) {
    New-Item -ItemType Directory -Path $stagingRoot -ErrorAction Stop | Out-Null
}
Assert-GuestArchivePath -Path $stagingRoot
Remove-GuestArchiveStaging
New-Item -ItemType Directory -Path $transferRoot -ErrorAction Stop | Out-Null
Assert-GuestArchiveTree
