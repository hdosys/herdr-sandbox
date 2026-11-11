$env:PSModulePath = $PSHOME + '\Modules'
$LocalTarget = $true
$script:CopiedConfigurationFiles = 0
$script:Utf8NoBom = New-Object Text.UTF8Encoding($false)
$root = $PSScriptRoot
$source = Join-Path $root 'fixture.json'
[IO.File]::WriteAllText($source, '{"fixture":true}')
foreach ($name in @('CLAUDE_CONFIG_DIR','CODEX_HOME','COPILOT_HOME','PI_CODING_AGENT_DIR','HERDR_CONFIG_PATH')) {
    $custom = Join-Path $root ('custom-' + $name)
    if ($name -eq 'HERDR_CONFIG_PATH') { $custom = Join-Path $custom 'config.toml' }
    [Environment]::SetEnvironmentVariable($name,$custom,'Process')
    $default = Join-Path $root ('default-' + $name)
    $selected = Resolve-ConfigurationTargetPath $name $default
    if ($selected -cne $custom) { throw "Wrong target for $name" }
    $destination = if ($name -eq 'HERDR_CONFIG_PATH') { $selected } else { Join-Path $selected 'auth.json' }
    # Uses the same file publication owner as credential-only imports.
    Set-AtomicConfigurationFile -Source $source -Destination $destination
    if ([IO.File]::ReadAllText($destination) -cne '{"fixture":true}' -or (Test-Path -LiteralPath $default)) {
        throw "Configuration escaped selected target $name"
    }
    $LocalTarget = $false
    if ((Resolve-ConfigurationTargetPath $name $default) -cne $default) { throw 'Managed guest followed a local override.' }
    $LocalTarget = $true
    foreach ($invalid in @('relative','C:relative','\root-relative')) {
        [Environment]::SetEnvironmentVariable($name,$invalid,'Process')
        $rejected = $false
        try { $null = Resolve-ConfigurationTargetPath $name $default } catch { $rejected=$true }
        if (-not $rejected) { throw "Relative target accepted: $name" }
    }
}
Write-Output 'verified selected local configuration targets'
