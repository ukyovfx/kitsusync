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

function Resolve-NativeCommand([string]$Name) {
    foreach ($candidate in @("$Name.exe", $Name)) {
        $command = Get-Command $candidate -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($command) { return $command.Source }
    }
    throw "Required command is not installed: $Name"
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

$remotePreflightPython = @'
import hashlib, json, os, pwd, stat, sys

cfg = json.loads(sys.argv[1])
uid = os.getuid()
user = pwd.getpwuid(uid).pw_name
trusted = {name: pwd.getpwnam(name).pw_uid for name in ("ukyo_vfx", "vfx-breakglass")}
path = cfg["path"]
owner = "missing"
mode = "missing"
reason = "ok"

def report(ok, why):
    owner_class = ",".join(f"{name}:{value}" for name, value in trusted.items())
    print(f"STAGING_UPLOAD_PREFLIGHT={'PASS' if ok else 'FAIL'} phase={cfg['phase']} remote_user={user} remote_uid={uid} observed_owner_uid={owner} observed_mode={mode} expected_owner_class={owner_class} expected_mode=0700 reason={why}")

try:
    parent = os.lstat("/var/tmp")
    if not stat.S_ISDIR(parent.st_mode) or stat.S_ISLNK(parent.st_mode) or parent.st_uid != 0 or stat.S_IMODE(parent.st_mode) != 0o1777:
        reason = "var_tmp_metadata_invalid"
        raise ValueError
    if user not in trusted or uid not in trusted.values():
        reason = "ssh_user_not_trusted"
        raise ValueError
    try:
        directory = os.lstat(path)
    except FileNotFoundError:
        if not cfg.get("create"):
            reason = "directory_missing"
            raise ValueError
        os.mkdir(path, 0o700)
        directory = os.lstat(path)
    owner = str(directory.st_uid)
    mode = format(stat.S_IMODE(directory.st_mode), "04o")
    if not stat.S_ISDIR(directory.st_mode) or stat.S_ISLNK(directory.st_mode):
        reason = "directory_type_invalid"
        raise ValueError
    if directory.st_uid != uid or directory.st_uid not in trusted.values():
        reason = "directory_owner_invalid"
        raise ValueError
    if stat.S_IMODE(directory.st_mode) != 0o700:
        reason = "directory_mode_invalid"
        raise ValueError
    dfd = os.open(path, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0) | getattr(os, "O_NOFOLLOW", 0))
    try:
        names = set(os.listdir(dfd))
        expected_names = set(cfg["names"])
        if not names.issubset(expected_names) or (cfg.get("complete") and names != expected_names):
            reason = "directory_entries_invalid"
            raise ValueError
        for name in names:
            fd = os.open(name, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0), dir_fd=dfd)
            try:
                item = os.fstat(fd)
                if not stat.S_ISREG(item.st_mode) or item.st_uid != uid or item.st_nlink != 1 or stat.S_IMODE(item.st_mode) not in (0o600, 0o700):
                    reason = "file_metadata_invalid"
                    raise ValueError
                expected_hash = cfg.get("hashes", {}).get(name)
                if expected_hash:
                    digest = hashlib.sha256()
                    with os.fdopen(os.dup(fd), "rb") as stream:
                        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                            digest.update(chunk)
                    if digest.hexdigest() != expected_hash:
                        reason = "file_digest_invalid"
                        raise ValueError
            finally:
                os.close(fd)
    finally:
        os.close(dfd)
except Exception:
    report(False, reason)
    sys.exit(1)
report(True, "ok")
'@

function Invoke-RemotePreflight([string]$HostName, [string]$Path, [string[]]$Names, [hashtable]$Hashes, [string]$Phase, [bool]$Create, [bool]$Complete) {
    $configJson = @{ path = $Path; names = @($Names); hashes = $Hashes; phase = $Phase; create = $Create; complete = $Complete } | ConvertTo-Json -Compress
    $codeBase64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($remotePreflightPython))
    $configBase64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($configJson))
    $remoteCommand = 'python3 -c ''import base64,sys;sys.argv=[sys.argv[0],base64.b64decode("' + $configBase64 + '").decode()];exec(base64.b64decode("' + $codeBase64 + '"))'''
    $output = @(& $script:ssh @script:sshOptions $HostName $remoteCommand 2>&1)
    $status = $LASTEXITCODE
    foreach ($line in $output) { Write-Output $line }
    if ($status -ne 0 -or @($output | Where-Object { $_ -like 'STAGING_UPLOAD_PREFLIGHT=FAIL*' }).Count -gt 0) {
        throw "Remote staging preflight failed at phase '$Phase'."
    }
}

function Get-RemoteHelperContract {
    $output = @(& $script:ssh @script:sshOptions $script:remoteHost 'sudo -n /usr/local/sbin/kitsusync-staging-deploy --contract-info' 2>&1)
    return @{ Status = $LASTEXITCODE; Output = ($output -join "`n") }
}

function Assert-RoutineStagingHelperContract([int]$Status, [string]$Output) {
    $expected = 'STAGING_HELPER_CONTRACT=staging-v6 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass'
    if ($Status -ne 0 -or $Output -notmatch [regex]::Escape($expected)) {
        throw "STAGING_BOOTSTRAP_REQUIRED expected='$expected' observed_status=$Status observed_output='$Output'"
    }
}

$script:gh = Resolve-NativeCommand 'gh'
$script:ssh = Resolve-NativeCommand 'ssh'
$script:scp = Resolve-NativeCommand 'scp'
$script:remoteHost = if ([string]::IsNullOrWhiteSpace($env:KITSUSYNC_STAGING_SSH_HOST)) { 'vfxstudio' } else { $env:KITSUSYNC_STAGING_SSH_HOST }
$script:remoteUser = if ([string]::IsNullOrWhiteSpace($env:KITSUSYNC_STAGING_SSH_USER)) { 'ukyo_vfx' } else { $env:KITSUSYNC_STAGING_SSH_USER }
if ($script:remoteHost -notmatch '^[A-Za-z0-9.-]+$' -or $script:remoteUser -ne 'ukyo_vfx') {
    throw 'Staging SSH target must be a DNS/IP host reached as the unprivileged ukyo_vfx account.'
}
$script:remoteTarget = "$($script:remoteUser)@$($script:remoteHost)"
$script:sshOptions = @('-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes')
if (-not [string]::IsNullOrWhiteSpace($env:KITSUSYNC_SSH_KNOWN_HOSTS_FILE)) {
    $script:sshOptions += @('-o', "UserKnownHostsFile=$env:KITSUSYNC_SSH_KNOWN_HOSTS_FILE")
}
$script:scpOptions = @('-q', '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes')
if (-not [string]::IsNullOrWhiteSpace($env:KITSUSYNC_SSH_KNOWN_HOSTS_FILE)) {
    $script:scpOptions += @('-o', "UserKnownHostsFile=$env:KITSUSYNC_SSH_KNOWN_HOSTS_FILE")
}
$script:sshOptions += @('-l', $script:remoteUser)
$script:scpOptions += @('-o', "User=$($script:remoteUser)")
$contract = Get-RemoteHelperContract
Assert-RoutineStagingHelperContract $contract.Status $contract.Output

$tempRoot = Join-Path ([IO.Path]::GetTempPath()) ("kitsusync-staging-artifact-$CommitSha-" + [guid]::NewGuid().ToString('N'))
$zipPath = Join-Path $tempRoot 'candidate-artifact.zip'
$extractRoot = Join-Path $tempRoot 'extracted'
$tempRootCreated = $false

try {
    New-Item -ItemType Directory -Path $tempRoot | Out-Null
    $tempRootCreated = $true
    if ($IsWindows) {
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
    } else {
        & chmod 700 $tempRoot
        if ($LASTEXITCODE -ne 0) { throw 'Could not restrict the private artifact directory permissions.' }
    }
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

    & $script:gh api "repos/$repo/actions/artifacts/$($artifact.id)/zip" > $zipPath
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

    $stage = "/var/tmp/kitsusync-staging-candidate-$CommitSha"
    Invoke-RemotePreflight $script:remoteHost $stage $expectedFiles @{} 'candidate-before-upload' $true $false

    foreach ($name in $expectedFiles) {
        & $script:scp @script:scpOptions -- (Join-Path $bundle $name) "${script:remoteTarget}:${stage}/${name}"
        if ($LASTEXITCODE -ne 0) { throw "Candidate upload failed: $name" }
    }
    & $script:ssh @script:sshOptions $script:remoteHost "chmod 600 $stage/*"
    if ($LASTEXITCODE -ne 0) { throw 'Could not secure staged candidate file permissions.' }
    $candidateHashes = @{}
    foreach ($name in $expectedFiles) {
        $candidateHashes[$name] = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $bundle $name)).Hash.ToLowerInvariant()
    }
    Invoke-RemotePreflight $script:remoteHost $stage $expectedFiles $candidateHashes 'candidate-before-deploy' $false $true
    & $script:ssh @script:sshOptions $script:remoteHost "sudo -n /usr/local/sbin/kitsusync-staging-deploy $CommitSha"
    if ($LASTEXITCODE -ne 0) { throw 'Staging deployment or post-deployment verification failed.' }
} finally {
    if ($tempRootCreated -and (Test-Path -LiteralPath $tempRoot)) {
        Remove-Item -LiteralPath $tempRoot -Recurse -Force
    }
}
