$ErrorActionPreference = 'Stop'

$wrapperPath = Join-Path $PSScriptRoot 'upgrade-kitsusync-staging-helper.ps1'
$tokens = $null
$parseErrors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile($wrapperPath, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count -gt 0) { throw "Could not parse helper upgrade wrapper: $($parseErrors[0].Message)" }
$functionAst = $ast.Find({
    param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Get-StagingHelperUpgradeAction'
}, $true)
if (-not $functionAst) { throw 'Helper upgrade decision function was not found.' }
. ([scriptblock]::Create($functionAst.Extent.Text))

$upgradeRootPath = Join-Path $PSScriptRoot '..\deploy\kitsusync-staging-helper-upgrade-root.sh'
$upgradeRootSource = Get-Content -Raw -LiteralPath $upgradeRootPath
$deployedV7Sha = '63371b16e7f13af1e1d7046217c8ce0ae200b0e9b42ab1bf342b158af121e78d'
$allowlistMatch = [regex]::Match($upgradeRootSource, '(?s)EXPECTED_OLD_HELPER_SHAS=\((.*?)\n\)')
if (-not $allowlistMatch.Success -or [regex]::Matches($allowlistMatch.Groups[1].Value, "(?m)^\s*$deployedV7Sha\s*$").Count -ne 1) {
    throw 'The exact deployed staging-v7 helper identity is not allowed exactly once.'
}

$prefix = 'STAGING_HELPER_CONTRACT='
$suffix = ' incoming=/var/tmp/kitsusync-staging-candidate-<sha>'
$current = $prefix + 'staging-v10' + $suffix + ' owners=operators.conf'
# Earlier helpers printed their own owner list; any such value must still be recognized for the exact predecessor versions.
$v9 = $prefix + 'staging-v9' + $suffix + ' owners=legacy-a,legacy-b'
$v8 = $prefix + 'staging-v8' + $suffix + ' owners=legacy-a,legacy-b'
$v7 = $prefix + 'staging-v7' + $suffix + ' owners=legacy-a,legacy-b'
$v6 = $prefix + 'staging-v6' + $suffix + ' owners=legacy-a,legacy-b'
$v5 = $prefix + 'staging-v5' + $suffix + ' owners=legacy-a,legacy-b'
$v4 = $prefix + 'staging-v4' + $suffix + ' owners=legacy-a,legacy-b'
$v3 = $prefix + 'staging-v3' + $suffix + ' owners=legacy-a,legacy-b'

$deployedV9Sha = '4afd1d26869b89ab2844d4718ee9565d269683f3bec58e947c1e2e6631a7e7d7'
if ([regex]::Matches($allowlistMatch.Groups[1].Value, "(?m)^\s*$deployedV9Sha\s*$").Count -ne 1) {
    throw 'The exact deployed staging-v9 helper identity is not allowed exactly once.'
}

if ((Get-StagingHelperUpgradeAction 0 $current) -ne 'ALREADY_CURRENT') { throw 'v10 helper was not recognized as current.' }
if ((Get-StagingHelperUpgradeAction 0 $v9) -ne 'UPGRADE') { throw 'Exact v9 helper was not recognized as an upgradable predecessor.' }
if ((Get-StagingHelperUpgradeAction 0 $v8) -ne 'UPGRADE') { throw 'Exact v8 helper was not recognized as an upgradable predecessor.' }
if ((Get-StagingHelperUpgradeAction 0 $v7) -ne 'UPGRADE') { throw 'Exact v7 helper was not recognized as an upgradable predecessor.' }
if ((Get-StagingHelperUpgradeAction 0 $v6) -ne 'UPGRADE') { throw 'Exact v6 helper was not recognized as an upgradable predecessor.' }
if ((Get-StagingHelperUpgradeAction 0 $v5) -ne 'UPGRADE') { throw 'Exact v5 helper was not recognized as an upgradable predecessor.' }
if ((Get-StagingHelperUpgradeAction 0 $v4) -ne 'UPGRADE') { throw 'Exact v4 helper was not recognized as an upgradable predecessor.' }
if ((Get-StagingHelperUpgradeAction 0 $v3) -ne 'UPGRADE') { throw 'Exact v3 helper was not recognized as an upgradable predecessor.' }
if ((Get-StagingHelperUpgradeAction 1 'STAGING_DEPLOY_ERROR=INVALID_ARGUMENT') -ne 'UPGRADE') { throw 'Legacy INVALID_ARGUMENT migration path was not preserved.' }

foreach ($case in @(
    @{ Status = 0; Output = 'STAGING_HELPER_CONTRACT=staging-v3'; Name = 'incomplete-v3' },
    @{ Status = 0; Output = ($prefix + 'staging-v11' + $suffix + ' owners=operators.conf'); Name = 'unknown-version' },
    @{ Status = 0; Output = ($prefix + 'staging-v10' + $suffix + ' owners=other'); Name = 'current-with-unexpected-owners' },
    @{ Status = 0; Output = ($prefix + 'staging-v9' + $suffix + ' owners=a;rm -rf /'); Name = 'predecessor-with-metacharacters' },
    @{ Status = 1; Output = 'arbitrary error'; Name = 'unknown-error' }
)) {
    try {
        [void](Get-StagingHelperUpgradeAction $case.Status $case.Output)
        throw "Unknown helper output was accepted: $($case.Name)"
    }
    catch {
        if ($_.Exception.Message -like "Unknown helper output was accepted:*") { throw }
    }
}

# The maintenance wrapper takes every connection setting from the environment and fails before any network access when one is missing.
$names = @('KITSUSYNC_STAGING_SSH_HOST', 'KITSUSYNC_STAGING_SSH_USER', 'KITSUSYNC_STAGING_EMERGENCY_SSH_HOST', 'KITSUSYNC_STAGING_EMERGENCY_USER')
$saved = @{}
foreach ($name in $names) { $saved[$name] = [Environment]::GetEnvironmentVariable($name); [Environment]::SetEnvironmentVariable($name, $null) }
try {
    foreach ($index in 0..($names.Count - 1)) {
        for ($j = 0; $j -lt $index; $j++) { [Environment]::SetEnvironmentVariable($names[$j], 'fixture-value') }
        $env:KITSUSYNC_FIXTURE_SCRIPT = $wrapperPath
        $child = 'try { & $env:KITSUSYNC_FIXTURE_SCRIPT -CommitSha (''a'' * 40); exit 0 } catch { Write-Output (''MESSAGE='' + $_.Exception.Message); exit 3 }'
        $output = (& pwsh -NoProfile -Command $child 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $output -notmatch ('Required environment variable is not set: ' + $names[$index])) {
            throw "Upgrade wrapper did not fail closed when $($names[$index]) is missing: $output"
        }
    }
}
finally {
    foreach ($name in $names) { [Environment]::SetEnvironmentVariable($name, $saved[$name]) }
}

Write-Output 'staging-helper-upgrade-contract=PASS deployed-v7-63371b16=UPGRADE deployed-v9-4afd1d26=UPGRADE'

