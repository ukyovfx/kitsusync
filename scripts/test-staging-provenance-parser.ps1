[CmdletBinding()]
param(
    [string]$DeployScript = (Join-Path $PSScriptRoot 'deploy-kitsusync-staging-candidate.ps1')
)

$source = Get-Content -Raw -LiteralPath $DeployScript
$tokens = $null
$parseErrors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseInput($source, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -gt 0) { throw 'Deploy script does not parse.' }
$functionAst = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Read-Provenance' }, $true)
if (-not $functionAst) { throw 'Read-Provenance function was not found.' }
$reader = [scriptblock]::Create($functionAst.Extent.Text + "`nRead-Provenance -Path `$args[0]")
$tempRoot = Join-Path ([IO.Path]::GetTempPath()) ('kitsusync-provenance-test-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tempRoot | Out-Null
try {
    $validPath = Join-Path $tempRoot 'valid.txt'
    [IO.File]::WriteAllText($validPath, "artifact_kind=candidate`nimage_archive_sha256=$('a' * 64)`ncompose_sha256=$('b' * 64)`ndeployment_tool_sha256=$('c' * 64)`n")
    $parsed = & $reader $validPath
    foreach ($key in @('image_archive_sha256','compose_sha256','deployment_tool_sha256')) {
        if (-not $parsed.ContainsKey($key)) { throw "sha256-style provenance key was not parsed: $key" }
    }

    $malformedPath = Join-Path $tempRoot 'malformed.txt'
    [IO.File]::WriteAllText($malformedPath, "image_archive_sha256=$('a' * 64)`nBad-Key=value`n")
    $malformedRejected = $false
    try { $null = & $reader $malformedPath } catch { $malformedRejected = $true }
    if (-not $malformedRejected) { throw 'Malformed provenance key was accepted.' }

    $uppercasePath = Join-Path $tempRoot 'uppercase.txt'
    [IO.File]::WriteAllText($uppercasePath, "Compose_sha256=$('b' * 64)`n")
    $uppercaseRejected = $false
    try { $null = & $reader $uppercasePath } catch { $uppercaseRejected = $true }
    if (-not $uppercaseRejected) { throw 'Uppercase provenance key was accepted.' }

    $duplicatePath = Join-Path $tempRoot 'duplicate.txt'
    [IO.File]::WriteAllText($duplicatePath, "compose_sha256=$('b' * 64)`ncompose_sha256=$('c' * 64)`n")
    $duplicateRejected = $false
    try { $null = & $reader $duplicatePath } catch { $duplicateRejected = $true }
    if (-not $duplicateRejected) { throw 'Duplicate provenance key was accepted.' }
} finally {
    Remove-Item -LiteralPath $tempRoot -Recurse -Force
}

Write-Output 'staging-provenance-parser=PASS'
