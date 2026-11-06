param([switch]$ShowDialog, [switch]$UserScope)

function ConvertFrom-PlaywrightExtensionTokenInput {
    param([AllowEmptyString()][string]$Value)

    $token = $Value.Trim()
    $prefix = 'PLAYWRIGHT_MCP_EXTENSION_TOKEN='
    if ($token.StartsWith($prefix, [StringComparison]::Ordinal)) {
        $token = $token.Substring($prefix.Length).Trim()
    }
    if ([string]::IsNullOrWhiteSpace($token)) { return '' }
    if ($token.Length -gt 512 -or $token -match '[\x00-\x1F\x7F]') {
        throw 'The Playwright Extension token must be one bounded line.'
    }
    return $token
}

function Set-PlaywrightExtensionToken {
    param([Parameter(Mandatory = $true)][string]$Value,
        [ValidateSet('Machine', 'User')][string]$EnvironmentTarget = 'Machine')

    $token = ConvertFrom-PlaywrightExtensionTokenInput -Value $Value
    if ([string]::IsNullOrWhiteSpace($token)) {
        throw 'Paste the token value or the complete PLAYWRIGHT_MCP_EXTENSION_TOKEN assignment.'
    }
    $variableName = 'PLAYWRIGHT_MCP_EXTENSION_TOKEN'
    [Environment]::SetEnvironmentVariable($variableName, $token, $EnvironmentTarget)
    [Environment]::SetEnvironmentVariable($variableName, $token, 'Process')
    if ([Environment]::GetEnvironmentVariable($variableName, $EnvironmentTarget) -cne $token -or
        [Environment]::GetEnvironmentVariable($variableName, 'Process') -cne $token) {
        throw 'Playwright Extension token environment publication failed.'
    }
}

function Initialize-PlaywrightExtensionToken {
    param([Parameter(Mandatory = $true)][bool]$Enabled)

    if (-not $Enabled) { return '' }
    $variableName = 'PLAYWRIGHT_MCP_EXTENSION_TOKEN'
    $token = ConvertFrom-PlaywrightExtensionTokenInput -Value `
        ([string][Environment]::GetEnvironmentVariable($variableName, 'Machine'))
    if ([string]::IsNullOrWhiteSpace($token)) {
        $token = ConvertFrom-PlaywrightExtensionTokenInput -Value `
            ([string][Environment]::GetEnvironmentVariable($variableName, 'Process'))
    }
    if ([string]::IsNullOrWhiteSpace($token)) {
        Write-Warning 'Playwright browser setup is optional. Use the Playwright browser access taskbar icon when ready; provisioning will continue.'
        return ''
    }
    Set-PlaywrightExtensionToken -Value $token
    return $token
}

function Show-PlaywrightAccessDialog {
    param([switch]$UserScope)
    Add-Type -AssemblyName Microsoft.VisualBasic
    Add-Type -AssemblyName System.Windows.Forms
    $title = 'Playwright browser access'
    $inputValue = [Microsoft.VisualBasic.Interaction]::InputBox(
        'Enable Playwright MCP Bridge in Edge and copy its token. Paste the complete PLAYWRIGHT_MCP_EXTENSION_TOKEN line or just the value. Cancel leaves settings unchanged.',
        $title, '')
    if ([string]::IsNullOrWhiteSpace($inputValue)) { return }
    try {
        $target = if ($UserScope) { 'User' } else { 'Machine' }
        Set-PlaywrightExtensionToken -Value $inputValue -EnvironmentTarget $target
        $message = if ($UserScope) {
            'Token saved only for this Windows user. Start the agent from a new standalone terminal application. Existing Herdr servers and agents keep their previous environment; they were not restarted.'
        } else {
            'Token saved for this Sandbox. Open a new Herdr terminal tab before starting your agent. Already-running agents keep their previous token.'
        }
        [Windows.Forms.MessageBox]::Show(
            $message,
            $title, [Windows.Forms.MessageBoxButtons]::OK, [Windows.Forms.MessageBoxIcon]::Information) | Out-Null
    } catch {
        [Windows.Forms.MessageBox]::Show(
            ('The token could not be saved: ' + $_.Exception.Message + ' Reopen Playwright browser access to try again.'),
            $title, [Windows.Forms.MessageBoxButtons]::OK, [Windows.Forms.MessageBoxIcon]::Error) | Out-Null
    }
}

if ($ShowDialog) { Show-PlaywrightAccessDialog -UserScope:$UserScope }
