$ErrorActionPreference = 'Stop'
if ([int][char]'Grüße'[2] -ne 252) { throw 'Launcher UTF-8 text was corrupted.' }
$inputStream = [Console]::OpenStandardInput()
$outputStream = New-Object IO.MemoryStream
$sha256 = [Security.Cryptography.SHA256]::Create()
try {
    $inputStream.CopyTo($outputStream)
    $data = $outputStream.ToArray()
    $digest = ([BitConverter]::ToString($sha256.ComputeHash($data))).Replace('-', '').ToLowerInvariant()
    [Console]::Out.WriteLine(('{0} {1}' -f $data.Length, $digest))
} finally {
    $sha256.Dispose()
    $outputStream.Dispose()
}
exit 0
