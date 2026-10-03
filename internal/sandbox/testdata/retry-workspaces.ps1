$workspaces = @(
    [pscustomobject]@{ name = 'one'; directory = 'C:\Workspaces\one' },
    [pscustomobject]@{ name = 'two'; directory = 'C:\Workspaces\two' }
)
$manifest = [pscustomobject]@{ activeWorkspace = 'C:\Workspaces\two' }
$workspacePath = 'C:\Tools'
$herdrExecutable = 'C:\Tools\herdr.exe'
$playwrightExtensionToken = ''
$script:created = New-Object Collections.ArrayList
$script:failSecond = $true
$script:focused = ''

function Invoke-WorkspaceCommand {
    param([string[]]$Arguments)
    $action = ($Arguments[0..1] -join ' ')
    switch ($action) {
        'workspace list' {
            return [pscustomobject]@{ result = [pscustomobject]@{ type = 'workspace_list'; workspaces = @($script:created) } }
        }
        'workspace create' {
            $directory = $Arguments[3]
            $name = $Arguments[5]
            if ($name -ceq 'two' -and $script:failSecond) { throw 'injected second-workspace failure' }
            if (@($script:created | Where-Object { $_.label -ceq $name }).Count -ne 0) { throw 'Existing workspace was recreated.' }
            $workspace = [pscustomobject]@{ label = $name; workspace_id = 'workspace-' + $name; directory = $directory }
            $null = $script:created.Add($workspace)
            return [pscustomobject]@{ result = [pscustomobject]@{ workspace = $workspace; root_pane = [pscustomobject]@{ pane_id = 'pane-' + $name } } }
        }
        'pane list' {
            $workspace = @($script:created | Where-Object { $_.workspace_id -ceq $Arguments[3] })
            return [pscustomobject]@{ result = [pscustomobject]@{ type = 'pane_list'; panes = @([pscustomobject]@{
                workspace_id = $workspace[0].workspace_id; cwd = $workspace[0].directory
            }) } }
        }
        'workspace focus' {
            $script:focused = $Arguments[2]
            return [pscustomobject]@{ result = [pscustomobject]@{} }
        }
        default { throw "Unexpected workspace operation: $action" }
    }
}
$failed = $false
try { Initialize-GuestWorkspaces } catch {
    if ($_.Exception.Message -cne 'injected second-workspace failure') { throw }
    $failed = $true
}
if (-not $failed -or $script:created.Count -ne 1) { throw 'Partial workspace creation was not preserved.' }
$script:failSecond = $false
Initialize-GuestWorkspaces
Initialize-GuestWorkspaces
if ($script:created.Count -ne 2 -or $script:focused -cne 'workspace-two') {
    throw 'Workspace retry did not converge without duplicates and with the active project focused.'
}
Write-Output 'verified'
