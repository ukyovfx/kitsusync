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

$v8 = 'STAGING_HELPER_CONTRACT=staging-v8 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass'
$v7 = 'STAGING_HELPER_CONTRACT=staging-v7 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass'
$v6 = 'STAGING_HELPER_CONTRACT=staging-v6 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass'
$v5 = 'STAGING_HELPER_CONTRACT=staging-v5 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass'
$v4 = 'STAGING_HELPER_CONTRACT=staging-v4 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass'
$v3 = 'STAGING_HELPER_CONTRACT=staging-v3 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass'

if ((Get-StagingHelperUpgradeAction 0 $v8) -ne 'ALREADY_CURRENT') { throw 'v8 helper was not recognized as current.' }
if ((Get-StagingHelperUpgradeAction 0 $v7) -ne 'UPGRADE') { throw 'Exact v7 helper was not recognized as an upgradable predecessor.' }
if ((Get-StagingHelperUpgradeAction 0 $v6) -ne 'UPGRADE') { throw 'Exact v6 helper was not recognized as an upgradable predecessor.' }
if ((Get-StagingHelperUpgradeAction 0 $v5) -ne 'UPGRADE') { throw 'Exact v5 helper was not recognized as an upgradable predecessor.' }
if ((Get-StagingHelperUpgradeAction 0 $v4) -ne 'UPGRADE') { throw 'Exact v4 helper was not recognized as an upgradable predecessor.' }
if ((Get-StagingHelperUpgradeAction 0 $v3) -ne 'UPGRADE') { throw 'Exact v3 helper was not recognized as an upgradable predecessor.' }
if ((Get-StagingHelperUpgradeAction 1 'STAGING_DEPLOY_ERROR=INVALID_ARGUMENT') -ne 'UPGRADE') { throw 'Legacy INVALID_ARGUMENT migration path was not preserved.' }

foreach ($case in @(
    @{ Status = 0; Output = 'STAGING_HELPER_CONTRACT=staging-v3'; Name = 'incomplete-v3' },
    @{ Status = 0; Output = 'STAGING_HELPER_CONTRACT=staging-v9 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass'; Name = 'unknown-version' },
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

Write-Output 'staging-helper-upgrade-contract=PASS deployed-v7-63371b16=UPGRADE'
