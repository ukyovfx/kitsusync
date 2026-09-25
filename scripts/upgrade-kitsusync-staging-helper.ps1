[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidatePattern('^[0-9a-f]{40}$')][string]$CommitSha
)

$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSVersion -lt [Version]'7.4') { throw 'PowerShell 7.4 or newer is required.' }

function Resolve-NativeCommand([string]$Name) {
    foreach ($candidate in @("$Name.exe", $Name)) {
        $command = Get-Command $candidate -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($command) { return $command.Source }
    }
    throw "Required command is not installed: $Name"
}

function Get-StagingHelperUpgradeAction([int]$Status, [string]$Output) {
    $currentContract = 'STAGING_HELPER_CONTRACT=staging-v6 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass'
    $predecessorContracts = @(
        'STAGING_HELPER_CONTRACT=staging-v5 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass',
        'STAGING_HELPER_CONTRACT=staging-v4 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass',
        'STAGING_HELPER_CONTRACT=staging-v3 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass'
    )
    if ($Status -eq 0 -and $Output -ceq $currentContract) { return 'ALREADY_CURRENT' }
    if ($Status -eq 0 -and $Output -cin $predecessorContracts) { return 'UPGRADE' }
    if ($Status -eq 1 -and $Output -match '(?m)^STAGING_DEPLOY_ERROR=INVALID_ARGUMENT$') { return 'UPGRADE' }
    throw "Could not safely identify the installed Staging helper: $Output"
}

$git = Resolve-NativeCommand 'git'
$gh = Resolve-NativeCommand 'gh'
$ssh = Resolve-NativeCommand 'ssh'
$scp = Resolve-NativeCommand 'scp'
$repoRoot = (& $git -C $PSScriptRoot rev-parse --show-toplevel).Trim()
if ($LASTEXITCODE -ne 0) { throw 'Could not resolve the KitsuSync checkout.' }
$head = (& $git -C $repoRoot rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $head -ne $CommitSha) { throw 'The checked-out source must exactly match CommitSha.' }

$repo = 'ukyovfx/kitsusync'
$runs = & $gh api "repos/$repo/actions/runs?head_sha=$CommitSha&per_page=100" | ConvertFrom-Json
if ($LASTEXITCODE -ne 0) { throw 'Could not query exact-SHA GitHub checks.' }
foreach ($name in @('CI', 'Security Audit')) {
    if (@($runs.workflow_runs | Where-Object { $_.name -eq $name -and $_.head_sha -eq $CommitSha -and $_.status -eq 'completed' -and $_.conclusion -eq 'success' }).Count -eq 0) {
        throw "No successful '$name' run exists for $CommitSha."
    }
}

$temp = Join-Path ([IO.Path]::GetTempPath()) ("kitsusync-staging-helper-$CommitSha-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $temp | Out-Null
try {
    if ($IsWindows) {
        & icacls.exe $temp /inheritance:r /grant:r "${env:USERNAME}:(OI)(CI)F" 'SYSTEM:(OI)(CI)F' | Out-Null
        if ($LASTEXITCODE -ne 0) { throw 'Could not secure the temporary helper directory ACL.' }
    }
    else { & chmod 700 $temp; if ($LASTEXITCODE -ne 0) { throw 'Could not secure the temporary helper directory.' } }
    $helperPath = Join-Path $temp 'kitsusync-staging-deploy'
    $upgradePath = Join-Path $temp 'upgrade-root.sh'
    foreach ($source in @(
        @{ Spec = 'deploy/kitsusync-staging-deploy'; Path = $helperPath },
        @{ Spec = 'deploy/kitsusync-staging-helper-upgrade-root.sh'; Path = $upgradePath }
    )) {
        $blob = (& $git -C $repoRoot rev-parse "${CommitSha}:$($source.Spec)").Trim()
        if ($LASTEXITCODE -ne 0 -or $blob -notmatch '^[0-9a-f]{40}$') { throw "Exact candidate source is missing: $($source.Spec)" }
        & $git -C $repoRoot cat-file blob $blob > $source.Path
        if ($LASTEXITCODE -ne 0 -or (& $git -C $repoRoot hash-object $source.Path).Trim() -ne $blob) { throw "Could not materialize exact candidate source: $($source.Spec)" }
    }
    $helperSha = (Get-FileHash -Algorithm SHA256 -LiteralPath $helperPath).Hash.ToLowerInvariant()
    $upgradeSha = (Get-FileHash -Algorithm SHA256 -LiteralPath $upgradePath).Hash.ToLowerInvariant()

    $stagingHost = if ([string]::IsNullOrWhiteSpace($env:KITSUSYNC_STAGING_SSH_HOST)) { 'vfxstudio' } else { $env:KITSUSYNC_STAGING_SSH_HOST }
    if ($stagingHost -notmatch '^[A-Za-z0-9.-]+$') { throw 'Staging host must be a DNS/IP host.' }
    $sshArgs = @('-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-l','ukyo_vfx')
    $scpArgs = @('-q','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o','User=ukyo_vfx')
    if (-not [string]::IsNullOrWhiteSpace($env:KITSUSYNC_SSH_KNOWN_HOSTS_FILE)) {
        $sshArgs += @('-o', "UserKnownHostsFile=$env:KITSUSYNC_SSH_KNOWN_HOSTS_FILE")
        $scpArgs += @('-o', "UserKnownHostsFile=$env:KITSUSYNC_SSH_KNOWN_HOSTS_FILE")
    }
    $contract = @(& $ssh @sshArgs $stagingHost 'sudo -n /usr/local/sbin/kitsusync-staging-deploy --contract-info' 2>&1)
    $contractStatus = $LASTEXITCODE
    $upgradeAction = Get-StagingHelperUpgradeAction $contractStatus ($contract -join "`n")
    if ($upgradeAction -eq 'ALREADY_CURRENT') {
        Write-Output 'STAGING_HELPER_UPGRADE=ALREADY_CURRENT'
        exit 0
    }

    $remoteDir = "/var/tmp/kitsusync-staging-helper-upgrade-$CommitSha"
    $preflight = @'
import json,os,pwd,stat,sys
c=json.loads(sys.argv[1]); uid=os.getuid(); user=pwd.getpwuid(uid).pw_name; p=c['path']
try:
 s=os.lstat('/var/tmp'); assert stat.S_ISDIR(s.st_mode) and not stat.S_ISLNK(s.st_mode) and s.st_uid==0 and stat.S_IMODE(s.st_mode)==0o1777
 assert user=='ukyo_vfx'
 try: d=os.lstat(p)
 except FileNotFoundError:
  assert c['create']; os.mkdir(p,0o700); d=os.lstat(p)
 assert stat.S_ISDIR(d.st_mode) and not stat.S_ISLNK(d.st_mode) and d.st_uid==uid and stat.S_IMODE(d.st_mode)==0o700
 dfd=os.open(p,os.O_RDONLY|getattr(os,'O_DIRECTORY',0)|getattr(os,'O_NOFOLLOW',0))
 try:
  names=set(os.listdir(dfd)); expected={'kitsusync-staging-deploy','upgrade-root.sh'}; assert names<=expected and (not c['complete'] or names==expected)
  for n in names:
   f=os.open(n,os.O_RDONLY|getattr(os,'O_NOFOLLOW',0),dir_fd=dfd)
   try:
    x=os.fstat(f); assert stat.S_ISREG(x.st_mode) and x.st_uid==uid and x.st_nlink==1 and stat.S_IMODE(x.st_mode)==0o600
    if n in c['hashes']:
     import hashlib
     h=hashlib.sha256()
     with os.fdopen(os.dup(f),'rb') as r:
      for b in iter(lambda:r.read(1048576),b''): h.update(b)
     assert h.hexdigest()==c['hashes'][n]
   finally: os.close(f)
 finally: os.close(dfd)
 print('STAGING_HELPER_UPLOAD_PREFLIGHT=PASS')
except Exception:
 print(f"STAGING_HELPER_UPLOAD_PREFLIGHT=FAIL user={user} uid={uid} owner={locals().get('d').st_uid if 'd' in locals() else 'missing'} mode={format(stat.S_IMODE(d.st_mode),'04o') if 'd' in locals() else 'missing'}")
 sys.exit(1)
'@
    $code = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($preflight))
    function Invoke-UpgradePreflight([bool]$Create, [bool]$Complete, [hashtable]$Hashes) {
        $json = @{ path=$remoteDir; create=$Create; complete=$Complete; hashes=$Hashes } | ConvertTo-Json -Compress
        $config = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json))
        $remoteCommand = 'python3 -c ''import base64,sys;sys.argv=[sys.argv[0],base64.b64decode("' + $config + '").decode()];exec(base64.b64decode("' + $code + '"))'''
        $out = @(& $ssh @sshArgs $stagingHost $remoteCommand 2>&1)
        if ($LASTEXITCODE -ne 0) { throw "Staging helper upload preflight failed: $($out -join ' ')" }
        $out | ForEach-Object { Write-Output $_ }
    }
    Invoke-UpgradePreflight $true $false @{}
    foreach ($entry in @(@{Path=$helperPath;Name='kitsusync-staging-deploy'},@{Path=$upgradePath;Name='upgrade-root.sh'})) {
        & $scp @scpArgs -- $entry.Path "ukyo_vfx@${stagingHost}:${remoteDir}/$($entry.Name)"
        if ($LASTEXITCODE -ne 0) { throw "Staging infrastructure helper upload failed: $($entry.Name)" }
    }
    & $ssh @sshArgs $stagingHost "chmod 600 $remoteDir/kitsusync-staging-deploy $remoteDir/upgrade-root.sh"
    if ($LASTEXITCODE -ne 0) { throw 'Could not secure uploaded Staging helper files.' }
    Invoke-UpgradePreflight $false $true @{ 'kitsusync-staging-deploy'=$helperSha; 'upgrade-root.sh'=$upgradeSha }

    $breakglass = @(& $ssh -o BatchMode=yes -o StrictHostKeyChecking=yes vfxstudio-breakglass 'id -un; id -u' 2>&1)
    $breakglassStatus = $LASTEXITCODE
    $expectedBreakglassUid = @(& $ssh @sshArgs $stagingHost 'id -u vfx-breakglass' 2>&1)
    $expectedBreakglassStatus = $LASTEXITCODE
    if ($breakglassStatus -ne 0 -or $expectedBreakglassStatus -ne 0 -or $expectedBreakglassUid.Count -ne 1 -or $breakglass.Count -lt 2 -or $breakglass[0].Trim() -ne 'vfx-breakglass' -or
        $breakglass[1].Trim() -notmatch '^[0-9]+$' -or $breakglass[1].Trim() -ne $expectedBreakglassUid[0].Trim()) {
        throw "Breakglass identity mismatch: $($breakglass -join ' ')"
    }
    & $ssh -tt -o BatchMode=yes -o StrictHostKeyChecking=yes vfxstudio-breakglass "sudo /bin/bash $remoteDir/upgrade-root.sh $CommitSha $helperSha $upgradeSha"
    if ($LASTEXITCODE -ne 0) { throw 'Privileged Staging helper upgrade failed.' }
    $installed = @(& $ssh @sshArgs $stagingHost 'sudo -n /usr/local/sbin/kitsusync-staging-deploy --contract-info' 2>&1)
    if ($LASTEXITCODE -ne 0 -or ($installed -join "`n") -notmatch 'STAGING_HELPER_CONTRACT=staging-v6 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=ukyo_vfx,vfx-breakglass') {
        throw "Installed Staging helper contract verification failed: $($installed -join ' ')"
    }
    Write-Output "STAGING_HELPER_UPGRADE=PASS source_sha=$CommitSha helper_sha256=$helperSha"
} finally {
    Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
}
