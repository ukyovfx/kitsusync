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

$v5 = 'STAGING_HELPER_CONTRACT=staging-v5 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass'
$v4 = 'STAGING_HELPER_CONTRACT=staging-v4 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass'
$v3 = 'STAGING_HELPER_CONTRACT=staging-v3 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass'

if ((Get-StagingHelperUpgradeAction 0 $v5) -ne 'ALREADY_CURRENT') { throw 'v5 helper was not recognized as current.' }
if ((Get-StagingHelperUpgradeAction 0 $v4) -ne 'UPGRADE') { throw 'Exact v4 helper was not recognized as an upgradable predecessor.' }
if ((Get-StagingHelperUpgradeAction 0 $v3) -ne 'UPGRADE') { throw 'Exact v3 helper was not recognized as an upgradable predecessor.' }
if ((Get-StagingHelperUpgradeAction 1 'STAGING_DEPLOY_ERROR=INVALID_ARGUMENT') -ne 'UPGRADE') { throw 'Legacy INVALID_ARGUMENT migration path was not preserved.' }

foreach ($case in @(
    @{ Status = 0; Output = 'STAGING_HELPER_CONTRACT=staging-v3'; Name = 'incomplete-v3' },
    @{ Status = 0; Output = 'STAGING_HELPER_CONTRACT=staging-v6 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass'; Name = 'unknown-version' },
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

Write-Output 'staging-helper-upgrade-contract=PASS'
