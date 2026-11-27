$ErrorActionPreference = 'Stop'
$script:CopiedConfigurationFiles = 0
$env:GIT_CONFIG_GLOBAL = Join-Path $fixtureRoot 'guest.gitconfig'
$env:GIT_CONFIG_NOSYSTEM = '1'
$gitCommand = (Get-Command git.exe -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$githubCommand = (Get-Command gh.exe -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
$key = 'credential.https://github.com.helper'
$expected = "!'" + $githubCommand + "' auth git-credential"
& $gitCommand config --global --add $key '""'
if ($LASTEXITCODE -ne 0) { throw 'Could not arrange helper reset.' }
& $gitCommand config --global --add $key $expected
if ($LASTEXITCODE -ne 0) { throw 'Could not arrange guest helper.' }
& $gitCommand config --global --add 'credential.https://other.example.helper' 'manager'
if ($LASTEXITCODE -ne 0) { throw 'Could not arrange unrelated host helper.' }
$preserved = @(Get-GuestGitHubCredentialHelpers -GitCommand $gitCommand -GitHubCommand $githubCommand)
if ($preserved.Count -ne 1 -or [string]$preserved[0] -cne $key) { throw 'Did not select only the exact guest GitHub helper.' }
$hostConfiguration = Join-Path $fixtureRoot 'host.gitconfig'
[IO.File]::WriteAllText($hostConfiguration, "[user]`n`tname = imported`n", (New-Object Text.UTF8Encoding($false)))
Copy-VerifiedConfigurationFile -Source $hostConfiguration -Destination $env:GIT_CONFIG_GLOBAL
$lostHelper = @(& $gitCommand config --global --get-all $key)
if ($LASTEXITCODE -ne 1 -or $lostHelper.Count -ne 0) { throw 'Negative control did not reproduce the overwritten helper.' }
Restore-GuestGitHubCredentialHelpers -GitCommand $gitCommand -GitHubCommand $githubCommand -Keys $preserved
$actual = @(& $gitCommand config --global --get-all $key)
if ($LASTEXITCODE -ne 0 -or $actual.Count -ne 2 -or [string]$actual[0] -cne '' -or [string]$actual[1] -cne $expected) {
    throw 'Configuration refresh lost authenticated Git integration.'
}
$importedName = & $gitCommand config --global --get user.name
if ($LASTEXITCODE -ne 0 -or [string]$importedName -cne 'imported') { throw 'Helper restoration discarded imported Git configuration.' }
Write-Output 'verified preserved GitHub helper without credential transfer'
