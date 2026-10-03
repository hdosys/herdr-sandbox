$ErrorActionPreference = 'Stop'
$path = Join-Path $env:APPDATA 'herdr\config.toml'
$current = $path
while (-not [string]::IsNullOrEmpty($current)) {
    if (Test-Path -LiteralPath $current) {
        $item = Get-Item -LiteralPath $current -Force
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw 'Guest Herdr configuration contains a reparse point.'
        }
    }
    $current = Split-Path -Parent $current
}
if (-not (Test-Path -LiteralPath $path)) {
    [Console]::Out.Write('missing')
    exit 0
}
$file = Get-Item -LiteralPath $path -Force
if ($file.PSIsContainer -or $file.Length -gt 1048576) {
    throw 'Guest Herdr configuration is not a bounded regular file.'
}
[Console]::Out.Write('present:' + [Convert]::ToBase64String([IO.File]::ReadAllBytes($path)))
