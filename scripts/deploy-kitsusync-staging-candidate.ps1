[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidatePattern('^[0-9a-f]{40}$')][string]$CommitSha,
    [Parameter(Mandatory)][ValidateScript({ Test-Path -LiteralPath $_ -PathType Container })][string]$BundleDirectory
)
$ErrorActionPreference = 'Stop'
$repo = 'ukyovfx/kitsusync'
$bundle = (Resolve-Path -LiteralPath $BundleDirectory).Path
$expectedFiles = @(
    'deployment-mode', 'docker-compose.yml', 'kitsusync-bootstrap', 'kitsusync-deploy',
    'kitsusync-deploy-transaction', 'kitsusync-image-identity', 'kitsusync-image.tar',
    'kitsusync-inspect', 'kitsusync-preview-deploy', 'kitsusync-restore-state',
    'kitsusync-runtime-state', 'kitsusync-sqlite-backup', 'provenance.txt'
)

$items = @(Get-ChildItem -Force -LiteralPath $bundle)
if ($items.Count -ne $expectedFiles.Count -or @($items.Name | Where-Object { $_ -notin $expectedFiles }).Count -gt 0) {
    throw 'Candidate bundle contains missing or unexpected entries.'
}
foreach ($item in $items) {
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -or $item.PSIsContainer) {
        throw "Candidate bundle entry is not a regular file: $($item.Name)"
    }
}

$provenance = @{}
foreach ($line in Get-Content -LiteralPath (Join-Path $bundle 'provenance.txt')) {
    if ($line -match '^([a-z_]+)=(.*)$') { $provenance[$Matches[1]] = $Matches[2] }
}
if ($provenance.source_commit -ne $CommitSha -or $provenance.source_id -ne $CommitSha -or
    $provenance.artifact_kind -ne 'candidate' -or $provenance.image_ref -ne "kitsusync:ci-$CommitSha" -or
    $provenance.release_commit -or $provenance.release_tag) {
    throw 'Candidate provenance does not match the exact requested SHA or is release-labelled.'
}
$digestMap = @{
    'kitsusync-image.tar' = 'image_archive_sha256'
    'docker-compose.yml' = 'compose_sha256'
    'kitsusync-deploy' = 'deployment_tool_sha256'
    'kitsusync-preview-deploy' = 'preview_deployment_tool_sha256'
    'kitsusync-deploy-transaction' = 'deployment_core_sha256'
    'kitsusync-inspect' = 'inspection_tool_sha256'
    'kitsusync-sqlite-backup' = 'sqlite_backup_tool_sha256'
    'kitsusync-bootstrap' = 'bootstrap_tool_sha256'
    'kitsusync-image-identity' = 'image_identity_tool_sha256'
    'kitsusync-runtime-state' = 'runtime_state_tool_sha256'
    'kitsusync-restore-state' = 'restore_state_tool_sha256'
}
foreach ($name in $digestMap.Keys) {
    $expected = [string]$provenance[$digestMap[$name]]
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $bundle $name)).Hash.ToLowerInvariant()
    if ($expected -notmatch '^[0-9a-f]{64}$' -or $expected -ne $actual) { throw "Candidate bundle digest mismatch: $name" }
}

$runsUri = "https://api.github.com/repos/$repo/actions/runs?head_sha=$CommitSha&per_page=100"
$runs = Invoke-RestMethod -Uri $runsUri -Headers @{ Accept = 'application/vnd.github+json'; 'X-GitHub-Api-Version' = '2022-11-28'; 'User-Agent' = 'KitsuSync-Staging-Deploy' }
foreach ($workflowName in @('CI', 'Security Audit')) {
    $matching = @($runs.workflow_runs | Where-Object { $_.name -eq $workflowName -and $_.head_sha -eq $CommitSha })
    if ($matching.Count -eq 0 -or @($matching | Where-Object { $_.status -eq 'completed' -and $_.conclusion -eq 'success' }).Count -eq 0) {
        throw "No successful completed '$workflowName' run exists for $CommitSha."
    }
}

$ssh = (Get-Command ssh.exe -ErrorAction Stop).Source
$scp = (Get-Command scp.exe -ErrorAction Stop).Source
$stage = "/var/tmp/kitsusync-preview-candidate-$CommitSha"
& $ssh -o BatchMode=yes -o StrictHostKeyChecking=yes vfxstudio "umask 077 && mkdir -m 700 -- $stage"
if ($LASTEXITCODE -ne 0) { throw 'Could not create the private candidate staging directory.' }
foreach ($name in $expectedFiles) {
    & $scp -q -- (Join-Path $bundle $name) "vfxstudio:${stage}/${name}"
    if ($LASTEXITCODE -ne 0) { throw "Candidate upload failed: $name" }
}
& $ssh -o BatchMode=yes -o StrictHostKeyChecking=yes vfxstudio "chmod 600 $stage/*"
if ($LASTEXITCODE -ne 0) { throw 'Could not secure staged candidate file permissions.' }
& $ssh -o BatchMode=yes -o StrictHostKeyChecking=yes vfxstudio "sudo -n /usr/local/sbin/kitsusync-staging-deploy $CommitSha"
if ($LASTEXITCODE -ne 0) { throw 'Staging deployment or post-deployment verification failed.' }
