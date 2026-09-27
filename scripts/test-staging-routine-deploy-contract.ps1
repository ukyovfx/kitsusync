param(
    [string]$DeployScript = (Join-Path $PSScriptRoot 'deploy-kitsusync-staging-candidate.ps1')
)
$ErrorActionPreference = 'Stop'

$tokens = $null
$parseErrors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile((Resolve-Path $DeployScript), [ref]$tokens, [ref]$parseErrors)
if ($parseErrors) { throw ($parseErrors | Out-String) }
$assertAst = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Assert-RoutineStagingHelperContract' }, $true)
if (-not $assertAst) { throw 'Routine staging helper contract gate is missing.' }
. ([scriptblock]::Create($assertAst.Extent.Text))

$candidateProvenanceAst = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Assert-CandidateProvenance' }, $true)
if (-not $candidateProvenanceAst) { throw 'Candidate provenance gate is missing.' }
. ([scriptblock]::Create($candidateProvenanceAst.Extent.Text))
$CommitSha = '811b52c9ec825c3757923f37ff25d23b87a8fe08'
$outerProvenance = @{
    artifact_kind = 'nonrelease'; source_commit = $CommitSha; source_id = $CommitSha
    release_commit = ''; release_tag = ''
}
Assert-CandidateProvenance $outerProvenance $false
$outerProvenance.artifact_kind = 'candidate'
Assert-CandidateProvenance $outerProvenance $false
$bundleProvenance = @{
    artifact_kind = 'candidate'; source_commit = $CommitSha; source_id = $CommitSha
    release_commit = ''; release_tag = ''; image_ref = "kitsusync:ci-$CommitSha"
    image_archive_sha256 = 'a' * 64; image_config_digest = 'sha256:' + ('b' * 64)
    image_manifest_digest = 'sha256:' + ('c' * 64); image_content_digest = 'sha256:' + ('d' * 64)
}
Assert-CandidateProvenance $bundleProvenance $true
foreach ($case in @(
    @{ Name = 'release outer artifact'; Values = (@{ artifact_kind = 'release'; source_commit = $CommitSha; source_id = $CommitSha; release_commit = ''; release_tag = '' }); Bundle = $false },
    @{ Name = 'nonrelease bundle'; Values = (@{ artifact_kind = 'nonrelease'; source_commit = $CommitSha; source_id = $CommitSha; release_commit = ''; release_tag = ''; image_ref = "kitsusync:ci-$CommitSha"; image_archive_sha256 = 'a' * 64; image_config_digest = 'b' * 64; image_manifest_digest = 'c' * 64; image_content_digest = 'd' * 64 }); Bundle = $true }
)) {
    $rejected = $false
    try { Assert-CandidateProvenance $case.Values $case.Bundle }
    catch { $rejected = $_.Exception.Message -like 'Artifact provenance does not identify*' }
    if (-not $rejected) { throw "Candidate provenance gate accepted $($case.Name)." }
}
$wrongSha = $outerProvenance.Clone()
$wrongSha.source_id = '0' * 40
$rejectedWrongSha = $false
try { Assert-CandidateProvenance $wrongSha $false }
catch { $rejectedWrongSha = $_.Exception.Message -like 'Artifact provenance does not identify*' }
if (-not $rejectedWrongSha) { throw 'Candidate provenance gate accepted the wrong source SHA.' }

$healthy = 'STAGING_HELPER_CONTRACT=staging-v7 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass'
Assert-RoutineStagingHelperContract 0 $healthy

$outdatedFailedClosed = $false
try { Assert-RoutineStagingHelperContract 0 ($healthy.Replace('staging-v7', 'staging-v6')) }
catch { $outdatedFailedClosed = $_.Exception.Message -like 'STAGING_BOOTSTRAP_REQUIRED*' }
if (-not $outdatedFailedClosed) { throw 'Routine deploy accepted an outdated v3 helper contract.' }

foreach ($case in @(
    @{ Status = 1; Output = 'STAGING_DEPLOY_ERROR=INVALID_ARGUMENT'; Name = 'outdated' },
    @{ Status = 127; Output = 'sudo: command not found'; Name = 'missing' }
)) {
    $failedClosed = $false
    try { Assert-RoutineStagingHelperContract $case.Status $case.Output }
    catch {
        $failedClosed = $_.Exception.Message -like 'STAGING_BOOTSTRAP_REQUIRED*'
    }
    if (-not $failedClosed) { throw "Routine deploy did not fail with the bootstrap-required marker for $($case.Name) helper." }
}

$source = Get-Content -Raw -LiteralPath $DeployScript
foreach ($forbidden in @('vfxstudio-breakglass', 'upgrade-root.sh', 'Privileged staging helper upgrade')) {
    if ($source.Contains($forbidden)) { throw "Routine candidate deploy still contains infrastructure upgrade path: $forbidden" }
}
$gateIndex = $source.IndexOf('Assert-RoutineStagingHelperContract $contract.Status $contract.Output', [StringComparison]::Ordinal)
$tempIndex = $source.IndexOf('$tempRoot =', [StringComparison]::Ordinal)
$stageUploadIndex = $source.IndexOf('Candidate upload failed', [StringComparison]::Ordinal)
$candidateDeployIndex = $source.IndexOf('sudo -n /usr/local/sbin/kitsusync-staging-deploy $CommitSha', [StringComparison]::Ordinal)
if ($gateIndex -lt 0 -or $tempIndex -le $gateIndex -or $stageUploadIndex -le $tempIndex -or $candidateDeployIndex -le $stageUploadIndex) {
    throw 'Routine helper contract gate is not ordered before candidate upload and staging deployment.'
}
Write-Output 'staging-routine-deploy-contract=PASS'
