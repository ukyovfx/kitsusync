[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidatePattern('^[0-9a-f]{40}$')][string]$CommitSha
)

$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSVersion -lt [Version]'7.4') { throw 'PowerShell 7.4 or newer is required for byte-preserving artifact ZIP hashing.' }
$repo = 'ukyovfx/kitsusync'
$artifactName = "kitsusync-deployment-$CommitSha"
$expectedFiles = @(
    'deployment-mode', 'docker-compose.yml', 'kitsusync-bootstrap', 'kitsusync-deploy',
    'kitsusync-image-identity', 'kitsusync-image.tar', 'kitsusync-inspect', 'kitsusync-restore-state',
    'kitsusync-runtime-state', 'kitsusync-sqlite-backup', 'provenance.txt'
)
$expectedArchiveEntries = @('provenance.txt') + @($expectedFiles | ForEach-Object { "deployment-bundle/$_" })
$digestMap = @{
    'kitsusync-image.tar' = 'image_archive_sha256'
    'docker-compose.yml' = 'compose_sha256'
    'kitsusync-deploy' = 'deployment_tool_sha256'
    'kitsusync-inspect' = 'inspection_tool_sha256'
    'kitsusync-sqlite-backup' = 'sqlite_backup_tool_sha256'
    'kitsusync-bootstrap' = 'bootstrap_tool_sha256'
    'kitsusync-image-identity' = 'image_identity_tool_sha256'
    'kitsusync-runtime-state' = 'runtime_state_tool_sha256'
    'kitsusync-restore-state' = 'restore_state_tool_sha256'
}

function Invoke-GhJson([string]$Endpoint) {
    $response = & $script:gh api $Endpoint
    if ($LASTEXITCODE -ne 0) { throw "GitHub API request failed: $Endpoint" }
    return (($response -join "`n") | ConvertFrom-Json)
}

function Read-Provenance([string]$Path) {
    $values = @{}
    foreach ($line in Get-Content -LiteralPath $Path) {
        if ($line -cnotmatch '^([a-z_][a-z0-9_]*)=(.*)$') { throw "Malformed provenance line in $([IO.Path]::GetFileName($Path))." }
        $key = $Matches[1]
        if ($values.ContainsKey($key)) { throw "Duplicate provenance key: $key" }
        $values[$key] = $Matches[2]
    }
    return $values
}

function Assert-CandidateProvenance($Values, [bool]$BundleProvenance) {
    if ($Values.artifact_kind -ne 'candidate' -or $Values.source_commit -ne $CommitSha -or
        $Values.source_id -ne $CommitSha -or $Values.release_commit -or $Values.release_tag) {
        throw 'Artifact provenance does not identify the exact non-release candidate SHA.'
    }
    if ($BundleProvenance) {
        if ($Values.image_ref -ne "kitsusync:ci-$CommitSha") { throw 'Bundle image reference does not match the exact candidate SHA.' }
        foreach ($key in @('image_archive_sha256','image_config_digest','image_manifest_digest','image_content_digest')) {
            if ([string]$Values[$key] -notmatch '^(sha256:)?[0-9a-f]{64}$') { throw "Invalid required provenance digest: $key" }
        }
    }
}

$ghCommand = Get-Command gh.exe -ErrorAction Stop
$script:gh = $ghCommand.Source
$tempRoot = Join-Path ([IO.Path]::GetTempPath()) ("kitsusync-staging-artifact-$CommitSha-" + [guid]::NewGuid().ToString('N'))
$zipPath = Join-Path $tempRoot 'candidate-artifact.zip'
$extractRoot = Join-Path $tempRoot 'extracted'
$tempRootCreated = $false

try {
    New-Item -ItemType Directory -Path $tempRoot | Out-Null
    $tempRootCreated = $true
    $acl = [System.Security.AccessControl.DirectorySecurity]::new()
    $acl.SetAccessRuleProtection($true, $false)
    foreach ($sid in @(
        [Security.Principal.WindowsIdentity]::GetCurrent().User,
        [Security.Principal.SecurityIdentifier]::new('S-1-5-18'),
        [Security.Principal.SecurityIdentifier]::new('S-1-5-32-544')
    )) {
        $rule = [System.Security.AccessControl.FileSystemAccessRule]::new(
            $sid,
            [System.Security.AccessControl.FileSystemRights]::FullControl,
            [System.Security.AccessControl.InheritanceFlags]::ContainerInherit -bor [System.Security.AccessControl.InheritanceFlags]::ObjectInherit,
            [System.Security.AccessControl.PropagationFlags]::None,
            [System.Security.AccessControl.AccessControlType]::Allow
        )
        [void]$acl.AddAccessRule($rule)
    }
    Set-Acl -LiteralPath $tempRoot -AclObject $acl
    New-Item -ItemType Directory -Path $extractRoot | Out-Null

    $runs = Invoke-GhJson "repos/$repo/actions/runs?head_sha=$CommitSha&per_page=100"
    $ciRuns = @($runs.workflow_runs | Where-Object {
        $_.name -eq 'CI' -and $_.head_sha -eq $CommitSha -and $_.status -eq 'completed' -and $_.conclusion -eq 'success'
    } | Sort-Object run_number -Descending)
    if ($ciRuns.Count -eq 0) { throw "No successful completed CI run exists for $CommitSha." }
    foreach ($workflowName in @('Security Audit')) {
        if (@($runs.workflow_runs | Where-Object {
            $_.name -eq $workflowName -and $_.head_sha -eq $CommitSha -and $_.status -eq 'completed' -and $_.conclusion -eq 'success'
        }).Count -eq 0) { throw "No successful completed '$workflowName' run exists for $CommitSha." }
    }

    $artifact = $null
    $ciRun = $null
    foreach ($run in $ciRuns) {
        $artifacts = Invoke-GhJson "repos/$repo/actions/runs/$($run.id)/artifacts"
        $matchingArtifacts = @($artifacts.artifacts | Where-Object { $_.name -eq $artifactName -and -not $_.expired })
        if ($matchingArtifacts.Count -gt 0) { $artifact = $matchingArtifacts[0]; $ciRun = $run; break }
    }
    if (-not $artifact) { throw "No unexpired exact candidate artifact '$artifactName' exists on a successful CI run." }
    if ([string]$artifact.digest -notmatch '^sha256:[0-9a-f]{64}$') { throw 'GitHub artifact ZIP digest is missing or malformed.' }

    & $ghCommand.Source api "repos/$repo/actions/artifacts/$($artifact.id)/zip" > $zipPath
    if ($LASTEXITCODE -ne 0) { throw 'GitHub artifact ZIP download failed.' }
    $actualZipDigest = 'sha256:' + (Get-FileHash -Algorithm SHA256 -LiteralPath $zipPath).Hash.ToLowerInvariant()
    if ($actualZipDigest -ne $artifact.digest) { throw 'Downloaded artifact ZIP digest does not match GitHub artifact metadata.' }
    Write-Output "CANDIDATE_ARTIFACT_RUN=$($ciRun.id)"
    Write-Output 'CANDIDATE_ARTIFACT_ZIP_DIGEST=PASS'

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $archive = [IO.Compression.ZipFile]::OpenRead($zipPath)
    try {
        $seen = [Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
        foreach ($entry in $archive.Entries) {
            $entryName = $entry.FullName.Replace('\', '/')
            if ([IO.Path]::IsPathRooted($entryName) -or $entryName -match '(^|/)\.\.?(/|$)' -or $entryName.Contains(':')) {
                throw 'Artifact ZIP contains a rooted or traversing path.'
            }
            if ($entryName.EndsWith('/')) { throw 'Artifact ZIP contains an unexpected directory entry.' }
            $mode = ($entry.ExternalAttributes -shr 16) -band 0xF000
            if ($mode -eq 0xA000) { throw "Artifact ZIP contains a symlink: $entryName" }
            if ($mode -ne 0 -and $mode -ne 0x8000) { throw "Artifact ZIP contains a non-regular file: $entryName" }
            if ($entryName -notin $expectedArchiveEntries -or -not $seen.Add($entryName)) {
                throw "Artifact ZIP contains an unexpected or duplicate entry: $entryName"
            }
        }
        if ($seen.Count -ne $expectedArchiveEntries.Count) { throw 'Artifact ZIP file set is incomplete.' }

        foreach ($entry in $archive.Entries) {
            $entryName = $entry.FullName.Replace('/', [IO.Path]::DirectorySeparatorChar)
            $destination = [IO.Path]::GetFullPath((Join-Path $extractRoot $entryName))
            $prefix = [IO.Path]::GetFullPath($extractRoot) + [IO.Path]::DirectorySeparatorChar
            if (-not $destination.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) {
                throw 'Artifact ZIP extraction path escaped its private directory.'
            }
            $parent = Split-Path -Parent $destination
            New-Item -ItemType Directory -Path $parent -Force | Out-Null
            $inputStream = $entry.Open()
            try {
                $outputStream = [IO.File]::Open($destination, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
                try { $inputStream.CopyTo($outputStream) } finally { $outputStream.Dispose() }
            } finally { $inputStream.Dispose() }
        }
    } finally { $archive.Dispose() }

    $outerProvenance = Read-Provenance (Join-Path $extractRoot 'provenance.txt')
    Assert-CandidateProvenance $outerProvenance $false
    $bundle = Join-Path $extractRoot 'deployment-bundle'
    $items = @(Get-ChildItem -Force -LiteralPath $bundle)
    if ($items.Count -ne $expectedFiles.Count -or @($items.Name | Where-Object { $_ -notin $expectedFiles }).Count -gt 0) {
        throw 'Candidate bundle contains missing or unexpected entries.'
    }
    foreach ($item in $items) {
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -or $item.PSIsContainer) {
            throw "Candidate bundle entry is not a regular file: $($item.Name)"
        }
    }

    $provenance = Read-Provenance (Join-Path $bundle 'provenance.txt')
    Assert-CandidateProvenance $provenance $true
    foreach ($name in $digestMap.Keys) {
        $expected = [string]$provenance[$digestMap[$name]]
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $bundle $name)).Hash.ToLowerInvariant()
        if ($expected -notmatch '^[0-9a-f]{64}$' -or $expected -ne $actual) { throw "Candidate bundle digest mismatch: $name" }
    }
    Write-Output "CANDIDATE_SOURCE_SHA=$CommitSha"
    Write-Output 'CANDIDATE_PROVENANCE=PASS'
    Write-Output 'CANDIDATE_FILE_DIGESTS=PASS'

    $ssh = (Get-Command ssh.exe -ErrorAction Stop).Source
    $scp = (Get-Command scp.exe -ErrorAction Stop).Source
    $stage = "/var/tmp/kitsusync-staging-candidate-$CommitSha"
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
} finally {
    if ($tempRootCreated -and (Test-Path -LiteralPath $tempRoot)) {
        Remove-Item -LiteralPath $tempRoot -Recurse -Force
    }
}
