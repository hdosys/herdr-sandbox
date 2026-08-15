$ErrorActionPreference = 'Stop'
$startMarker = '<!-- herdr-sandbox:worktrees:start -->'
$endMarker = '<!-- herdr-sandbox:worktrees:end -->'
$text = [Console]::In.ReadToEnd()
$startMatches = [regex]::Matches($text, [regex]::Escape($startMarker))
$endMatches = [regex]::Matches($text, [regex]::Escape($endMarker))

if ($startMatches.Count -eq 0 -and $endMatches.Count -eq 0) {
    [Console]::Out.Write($text)
    exit 0
}
if ($startMatches.Count -ne 1 -or $endMatches.Count -ne 1 -or
    $startMatches[0].Index -ne 0 -or $endMatches[0].Index -le $startMatches[0].Index) {
    throw 'Managed Herdr Sandbox worktree instructions are malformed.'
}

$remainderIndex = $endMatches[0].Index + $endMatches[0].Length
$remainder = $text.Substring($remainderIndex)
if ($remainder -ceq "`n" -or $remainder -ceq "`r`n") {
    $remainder = ''
} elseif ($remainder.StartsWith("`n`n", [StringComparison]::Ordinal)) {
    $remainder = $remainder.Substring(2)
} elseif ($remainder.StartsWith("`r`n`r`n", [StringComparison]::Ordinal)) {
    $remainder = $remainder.Substring(4)
} else {
    throw 'Managed Herdr Sandbox worktree instructions have an invalid boundary.'
}
if ($remainder.IndexOf($startMarker, [StringComparison]::Ordinal) -ge 0 -or
    $remainder.IndexOf($endMarker, [StringComparison]::Ordinal) -ge 0) {
    throw 'Managed Herdr Sandbox worktree instructions remain after filtering.'
}
[Console]::Out.Write($remainder)
