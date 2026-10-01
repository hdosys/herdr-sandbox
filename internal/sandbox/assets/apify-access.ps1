param([switch]$ShowDialog)

function Assert-ApifyRegularPath {
    param([Parameter(Mandatory = $true)][string]$Path)
    $current = [IO.Path]::GetFullPath($Path)
    while (-not [string]::IsNullOrEmpty($current)) {
        if ((Test-Path -LiteralPath $current) -and
            ((Get-Item -LiteralPath $current -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw 'The Apify token path must not contain reparse points.'
        }
        $current = [IO.Path]::GetDirectoryName($current)
    }
}

function Protect-ApifyTokenFile {
    param([Parameter(Mandatory = $true)][string]$Path)
    Assert-ApifyRegularPath -Path $Path
    $directory = Split-Path -Parent $Path
    New-Item -ItemType Directory -Path $directory -Force | Out-Null
    $gitDirectory = Join-Path $directory '.git'
    if (Test-Path -LiteralPath $gitDirectory) {
        if (-not (Test-Path -LiteralPath $gitDirectory -PathType Container)) {
            throw 'Apify token storage requires a regular configuration checkout, not a linked worktree.'
        }
        $exclude = Join-Path $gitDirectory 'info\exclude'
        Assert-ApifyRegularPath -Path $exclude
        New-Item -ItemType Directory -Path (Split-Path -Parent $exclude) -Force | Out-Null
        $text = if (Test-Path -LiteralPath $exclude -PathType Leaf) { [IO.File]::ReadAllText($exclude) } else { '' }
        foreach ($pattern in @('/.usage-status.env', '/.usage-status.env.*.tmp')) {
            if (@($text -split '\r?\n') -cnotcontains $pattern) {
                [IO.File]::AppendAllText($exclude, ("`n" + $pattern + "`n"), (New-Object Text.UTF8Encoding($false)))
            }
        }
    }
}

function Set-ApifyToken {
    param(
        [AllowEmptyString()][string]$Value,
        [string]$Path = (Join-Path $env:USERPROFILE '.config\opencode\.usage-status.env')
    )
    $token = $Value.Trim()
    if ([string]::IsNullOrEmpty($token)) { return }
    if ($token.Length -gt 512 -or $token -cnotmatch '\A[A-Za-z0-9_-]+\z') {
        throw 'Paste only the Apify API token, without an assignment, quotes or line breaks.'
    }
    Protect-ApifyTokenFile -Path $Path
    $temporary = $Path + '.' + [Guid]::NewGuid().ToString('N') + '.tmp'
    try {
        [IO.File]::WriteAllText($temporary, ('APIFY_TOKEN="' + $token + '"' + "`n"), (New-Object Text.UTF8Encoding($false)))
        if (Test-Path -LiteralPath $Path -PathType Leaf) {
            [IO.File]::Replace($temporary, $Path, $null)
        } else {
            [IO.File]::Move($temporary, $Path)
        }
    } finally {
        if (Test-Path -LiteralPath $temporary) { [IO.File]::Delete($temporary) }
    }
}

function Show-ApifyAccessDialog {
    Add-Type -AssemblyName System.Windows.Forms
    $form = New-Object Windows.Forms.Form
    $form.Text = 'Apify access'
    $form.ClientSize = New-Object Drawing.Size(470, 200)
    $form.StartPosition = 'CenterScreen'
    $form.FormBorderStyle = 'FixedDialog'
    $form.MaximizeBox = $false
    $form.MinimizeBox = $false
    $label = New-Object Windows.Forms.Label
    $label.SetBounds(16, 16, 438, 80)
    $label.Text = 'Paste your Apify API token. This saves it only in this Sandbox. For reuse after Sandbox replacement, keep the token on the host and enable credentialSync.apify. A later host transfer replaces this guest value.'
    $inputBox = New-Object Windows.Forms.TextBox
    $inputBox.SetBounds(16, 100, 438, 24)
    $inputBox.UseSystemPasswordChar = $true
    $inputBox.MaxLength = 32767
    $inputBox.TabIndex = 0
    $save = New-Object Windows.Forms.Button
    $save.Text = '&Save'
    $save.SetBounds(278, 150, 80, 28)
    $save.TabIndex = 1
    $cancel = New-Object Windows.Forms.Button
    $cancel.Text = '&Cancel'
    $cancel.SetBounds(374, 150, 80, 28)
    $cancel.TabIndex = 2
    $cancel.DialogResult = [Windows.Forms.DialogResult]::Cancel
    $form.AcceptButton = $save
    $form.CancelButton = $cancel
    $save.Add_Click({
        try {
            Set-ApifyToken -Value $inputBox.Text
            $form.DialogResult = [Windows.Forms.DialogResult]::OK
            $form.Close()
        } catch {
            [Windows.Forms.MessageBox]::Show($form, 'The token could not be saved. Paste only the token and ensure the local configuration directory is writable and not linked.', 'Apify access', 'OK', 'Error') | Out-Null
        }
    })
    $form.Controls.AddRange(@($label, $inputBox, $save, $cancel))
    try { $null = $form.ShowDialog() } finally { $inputBox.Clear(); $form.Dispose() }
}

if ($ShowDialog) { Show-ApifyAccessDialog }
