param(
    [string[]]$Scripts = @(
        (Join-Path $PSScriptRoot 'deploy-kitsusync-staging-candidate.ps1'),
        (Join-Path $PSScriptRoot 'upgrade-kitsusync-staging-helper.ps1')
    )
)
$ErrorActionPreference = 'Stop'

$repo = 'ukyovfx/kitsusync'
$sha = '811b52c9ec825c3757923f37ff25d23b87a8fe08'
$ciPath = '.github/workflows/build.yml'

function New-Run([hashtable]$Override = @{}) {
    $run = @{
        name = 'CI'; path = $ciPath; event = 'pull_request'; head_sha = $sha; status = 'completed'
        conclusion = 'success'; run_number = 1; head_repository = @{ full_name = $repo }
    }
    foreach ($key in $Override.Keys) { $run[$key] = $Override[$key] }
    [pscustomobject]$run
}

foreach ($script in $Scripts) {
    $tokens = $null; $parseErrors = $null
    $ast = [System.Management.Automation.Language.Parser]::ParseFile((Resolve-Path $script), [ref]$tokens, [ref]$parseErrors)
    if ($parseErrors) { throw ($parseErrors | Out-String) }
    $functionAst = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Get-TrustedWorkflowRuns' }, $true)
    if (-not $functionAst) { throw "Get-TrustedWorkflowRuns is missing from $script" }
    . ([scriptblock]::Create($functionAst.Extent.Text))
    $label = Split-Path -Leaf $script

    function Assert-Count([string]$Case, $Runs, [int]$Expected) {
        $actual = (Get-TrustedWorkflowRuns $Runs 'CI' $ciPath $repo $sha).Count
        if ($actual -ne $Expected) { throw "${label}: $Case expected $Expected trusted run(s), got $actual." }
    }

    Assert-Count 'canonical run' @(New-Run) 1
    Assert-Count 'path with @ref suffix' @(New-Run @{ path = "$ciPath@refs/pull/7/merge" }) 1
    Assert-Count 'fake workflow with a CI name' @(New-Run @{ path = '.github/workflows/fake.yml' }) 0
    Assert-Count 'path prefix lookalike' @(New-Run @{ path = "$ciPath.evil" }) 0
    Assert-Count 'missing path' @(New-Run @{ path = $null }) 0
    Assert-Count 'push event' @(New-Run @{ event = 'push' }) 0
    Assert-Count 'fork head repository' @(New-Run @{ head_repository = @{ full_name = 'attacker/kitsusync' } }) 0
    Assert-Count 'missing head repository' @(New-Run @{ head_repository = $null }) 0
    Assert-Count 'other commit' @(New-Run @{ head_sha = '0' * 40 }) 0
    Assert-Count 'failed run' @(New-Run @{ conclusion = 'failure' }) 0
    Assert-Count 'unfinished run' @(New-Run @{ status = 'in_progress'; conclusion = $null }) 0
    Assert-Count 'fake run beside a real run' @((New-Run @{ path = '.github/workflows/fake.yml'; run_number = 9 }), (New-Run)) 1
    $newest = Get-TrustedWorkflowRuns @((New-Run @{ run_number = 2 }), (New-Run)) 'CI' $ciPath $repo $sha
    if ($newest[0].run_number -ne 2) { throw "${label}: trusted runs are not newest-first." }
}

$upgrade = Get-Content -Raw -LiteralPath (Join-Path $PSScriptRoot 'upgrade-kitsusync-staging-helper.ps1')
if ($upgrade -notmatch 'merge-base --is-ancestor \$CommitSha refs/remotes/origin/master') { throw 'Upgrade wrapper does not require the commit to be an ancestor of origin/master.' }
if ($upgrade.IndexOf('merge-base --is-ancestor') -gt $upgrade.IndexOf('actions/runs?head_sha=')) { throw 'Ancestor check must run before the run lookup.' }

$workflow = Get-Content -Raw -LiteralPath (Join-Path $PSScriptRoot '..\.github\workflows\staging-deploy.yml')
foreach ($needle in @("github.event.workflow_run.path == '.github/workflows/security-audit.yml'", '.head_repository.full_name == env.REPOSITORY', 'split("@")[0]) == ".github/workflows/build.yml"')) {
    if (-not $workflow.Contains($needle)) { throw "staging-deploy.yml is missing the gate: $needle" }
}
Write-Output 'staging-workflow-run-gate=PASS'
