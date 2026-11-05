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
    artifact_kind = 'nonrelease'; source_commit = $CommitSha; source_id = $CommitSha
    release_commit = ''; release_tag = ''; image_ref = "kitsusync:ci-$CommitSha"
    image_archive_sha256 = 'a' * 64; image_config_digest = 'sha256:' + ('b' * 64)
    image_manifest_digest = 'sha256:' + ('c' * 64); image_content_digest = 'sha256:' + ('d' * 64)
}
Assert-CandidateProvenance $bundleProvenance $true
$bundleProvenance.artifact_kind = 'candidate'
Assert-CandidateProvenance $bundleProvenance $true
foreach ($case in @(
    @{ Name = 'release outer artifact'; Values = (@{ artifact_kind = 'release'; source_commit = $CommitSha; source_id = $CommitSha; release_commit = ''; release_tag = '' }); Bundle = $false },
    @{ Name = 'release bundle'; Values = (@{ artifact_kind = 'release'; source_commit = $CommitSha; source_id = $CommitSha; release_commit = ''; release_tag = ''; image_ref = "kitsusync:ci-$CommitSha"; image_archive_sha256 = 'a' * 64; image_config_digest = 'b' * 64; image_manifest_digest = 'c' * 64; image_content_digest = 'd' * 64 }); Bundle = $true },
    @{ Name = 'release identity on nonrelease artifact'; Values = (@{ artifact_kind = 'nonrelease'; source_commit = $CommitSha; source_id = $CommitSha; release_commit = $CommitSha; release_tag = 'v0.4.9' }); Bundle = $false }
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

$healthy = 'STAGING_HELPER_CONTRACT=staging-v10 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=operators.conf'
Assert-RoutineStagingHelperContract 0 $healthy

$outdatedFailedClosed = $false
try { Assert-RoutineStagingHelperContract 0 ($healthy.Replace('staging-v10', 'staging-v9')) }
catch { $outdatedFailedClosed = $_.Exception.Message -like 'STAGING_BOOTSTRAP_REQUIRED*' }
if (-not $outdatedFailedClosed) { throw 'Routine deploy accepted an outdated v9 helper contract.' }

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
foreach ($forbidden in @('KITSUSYNC_STAGING_EMERGENCY', 'upgrade-root.sh', 'Privileged staging helper upgrade')) {
    if ($source.Contains($forbidden)) { throw "Routine candidate deploy still contains infrastructure upgrade path: $forbidden" }
}
$gateIndex = $source.IndexOf('Assert-RoutineStagingHelperContract $contract.Status $contract.Output', [StringComparison]::Ordinal)
$tempIndex = $source.IndexOf('$tempRoot =', [StringComparison]::Ordinal)
$stageUploadIndex = $source.IndexOf('Candidate upload failed', [StringComparison]::Ordinal)
$candidateDeployIndex = $source.IndexOf('sudo -n /usr/local/sbin/kitsusync-staging-deploy $CommitSha', [StringComparison]::Ordinal)
if ($gateIndex -lt 0 -or $tempIndex -le $gateIndex -or $stageUploadIndex -le $tempIndex -or $candidateDeployIndex -le $stageUploadIndex) {
    throw 'Routine helper contract gate is not ordered before candidate upload and staging deployment.'
}
# Connection settings come only from the environment: nothing may be defaulted, and a missing value fails before any network access.
foreach ($name in @('KITSUSYNC_STAGING_SSH_HOST', 'KITSUSYNC_STAGING_SSH_USER')) {
    if ($source -notmatch [regex]::Escape("'$name'")) { throw "Routine candidate deploy does not require $name." }
}
if ($source -match 'IsNullOrWhiteSpace\(\$env:KITSUSYNC_STAGING_SSH_(HOST|USER)\)\) \{ ') { throw 'Routine candidate deploy still falls back to a built-in SSH default.' }
function Invoke-ScriptForMessage([string]$Path) {
    # Run the script in a child process and report only its exception message, so the check does not depend on error rendering.
    $env:KITSUSYNC_FIXTURE_SCRIPT = $Path
    try {
        $wrapper = 'try { & $env:KITSUSYNC_FIXTURE_SCRIPT -CommitSha (''a'' * 40); exit 0 } catch { Write-Output (''MESSAGE='' + $_.Exception.Message); exit 3 }'
        $text = (& pwsh -NoProfile -Command $wrapper 2>&1 | Out-String)
        return $text
    }
    finally { Remove-Item Env:KITSUSYNC_FIXTURE_SCRIPT -ErrorAction SilentlyContinue }
}
$tools = @('gh', 'ssh', 'scp') | ForEach-Object { [bool](Get-Command $_ -CommandType Application -ErrorAction SilentlyContinue) }
if ($tools -notcontains $false) {
    $saved = @{}
    foreach ($name in @('KITSUSYNC_STAGING_SSH_HOST', 'KITSUSYNC_STAGING_SSH_USER')) { $saved[$name] = [Environment]::GetEnvironmentVariable($name); [Environment]::SetEnvironmentVariable($name, $null) }
    try {
        $output = Invoke-ScriptForMessage $DeployScript
        if ($LASTEXITCODE -eq 0 -or $output -notmatch 'Required environment variable is not set: KITSUSYNC_STAGING_SSH_HOST') { throw "Deploy script did not fail closed without connection settings: $output" }
        [Environment]::SetEnvironmentVariable('KITSUSYNC_STAGING_SSH_HOST', 'staging.example.invalid')
        $output = Invoke-ScriptForMessage $DeployScript
        if ($LASTEXITCODE -eq 0 -or $output -notmatch 'Required environment variable is not set: KITSUSYNC_STAGING_SSH_USER') { throw "Deploy script did not require the operator account: $output" }
        [Environment]::SetEnvironmentVariable('KITSUSYNC_STAGING_SSH_USER', 'root')
        $output = Invoke-ScriptForMessage $DeployScript
        if ($LASTEXITCODE -eq 0 -or $output -notmatch 'configured unprivileged operator account') { throw "Deploy script accepted a root SSH account: $output" }
    }
    finally {
        foreach ($name in $saved.Keys) { [Environment]::SetEnvironmentVariable($name, $saved[$name]) }
    }
}
Write-Output 'staging-routine-deploy-contract=PASS'

