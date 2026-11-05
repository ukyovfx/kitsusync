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
    $currentContract = 'STAGING_HELPER_CONTRACT=staging-v10 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=operators.conf'
    # Earlier helpers printed their owner account names in the contract line; accept any owners value for the exact predecessor versions.
    $predecessorPattern = '^STAGING_HELPER_CONTRACT=staging-v[3-9] incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=[A-Za-z0-9_,.-]+$'
    if ($Status -eq 0 -and $Output -ceq $currentContract) { return 'ALREADY_CURRENT' }
    if ($Status -eq 0 -and $Output -cmatch $predecessorPattern) { return 'UPGRADE' }
    if ($Status -eq 1 -and $Output -match '(?m)^STAGING_DEPLOY_ERROR=INVALID_ARGUMENT$') { return 'UPGRADE' }
    throw "Could not safely identify the installed Staging helper: $Output"
}

function Get-TrustedWorkflowRuns($Runs, [string]$Name, [string]$WorkflowPath, [string]$Repository, [string]$CommitSha) {
    # A run name alone is not an identity: require the canonical workflow file, the pull_request event and a same-repository head.
    # The Actions API may report the path with an "@ref" suffix.
    @($Runs | Where-Object {
        $_.name -ceq $Name -and (([string]$_.path) -split '@')[0] -ceq $WorkflowPath -and $_.event -ceq 'pull_request' -and
        $_.head_repository.full_name -ceq $Repository -and $_.head_sha -ceq $CommitSha -and
        $_.status -eq 'completed' -and $_.conclusion -eq 'success'
    } | Sort-Object run_number -Descending)
}

foreach ($requiredVariable in @('KITSUSYNC_STAGING_SSH_HOST', 'KITSUSYNC_STAGING_SSH_USER', 'KITSUSYNC_STAGING_EMERGENCY_SSH_HOST', 'KITSUSYNC_STAGING_EMERGENCY_USER')) {
    if ([string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($requiredVariable))) {
        throw "Required environment variable is not set: $requiredVariable"
    }
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
# The helper source must be accepted code: the commit has to be an ancestor of the freshly fetched origin/master.
& $git -C $repoRoot fetch --quiet origin master
if ($LASTEXITCODE -ne 0) { throw 'Could not refresh origin/master to verify the commit is accepted.' }
& $git -C $repoRoot merge-base --is-ancestor $CommitSha refs/remotes/origin/master
if ($LASTEXITCODE -ne 0) { throw "Commit $CommitSha is not an ancestor of origin/master." }
$runs = & $gh api "repos/$repo/actions/runs?head_sha=$CommitSha&per_page=100" | ConvertFrom-Json
if ($LASTEXITCODE -ne 0) { throw 'Could not query exact-SHA GitHub checks.' }
foreach ($gate in @(@{ Name = 'CI'; Path = '.github/workflows/build.yml' }, @{ Name = 'Security Audit'; Path = '.github/workflows/security-audit.yml' })) {
    if ((Get-TrustedWorkflowRuns $runs.workflow_runs $gate.Name $gate.Path $repo $CommitSha).Count -eq 0) {
        throw "No successful '$($gate.Name)' run exists for $CommitSha."
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

    $stagingHost = $env:KITSUSYNC_STAGING_SSH_HOST
    $operatorUser = $env:KITSUSYNC_STAGING_SSH_USER
    $emergencyHost = $env:KITSUSYNC_STAGING_EMERGENCY_SSH_HOST
    $emergencyUser = $env:KITSUSYNC_STAGING_EMERGENCY_USER
    if ($stagingHost -notmatch '^[A-Za-z0-9.-]+$') { throw 'Staging host must be a DNS/IP host.' }
    if ($operatorUser -notmatch '^[a-z_][a-z0-9_-]{0,31}$' -or $operatorUser -eq 'root') { throw 'Staging operator account name is invalid.' }
    if ($emergencyHost -notmatch '^[A-Za-z0-9.-]+$') { throw 'Staging emergency SSH host must be a DNS/IP host or SSH alias.' }
    if ($emergencyUser -notmatch '^[a-z_][a-z0-9_-]{0,31}$' -or $emergencyUser -eq 'root' -or $emergencyUser -eq $operatorUser) { throw 'Staging emergency account name is invalid.' }
    $sshArgs = @('-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-l',$operatorUser)
    $scpArgs = @('-q','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o',"User=$operatorUser")
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
 assert user==c['operator']
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
        $json = @{ path=$remoteDir; create=$Create; complete=$Complete; hashes=$Hashes; operator=$operatorUser } | ConvertTo-Json -Compress
        $config = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json))
        $remoteCommand = 'python3 -c ''import base64,sys;sys.argv=[sys.argv[0],base64.b64decode("' + $config + '").decode()];exec(base64.b64decode("' + $code + '"))'''
        $out = @(& $ssh @sshArgs $stagingHost $remoteCommand 2>&1)
        if ($LASTEXITCODE -ne 0) { throw "Staging helper upload preflight failed: $($out -join ' ')" }
        $out | ForEach-Object { Write-Output $_ }
    }
    Invoke-UpgradePreflight $true $false @{}
    foreach ($entry in @(@{Path=$helperPath;Name='kitsusync-staging-deploy'},@{Path=$upgradePath;Name='upgrade-root.sh'})) {
        & $scp @scpArgs -- $entry.Path "${operatorUser}@${stagingHost}:${remoteDir}/$($entry.Name)"
        if ($LASTEXITCODE -ne 0) { throw "Staging infrastructure helper upload failed: $($entry.Name)" }
    }
    & $ssh @sshArgs $stagingHost "chmod 600 $remoteDir/kitsusync-staging-deploy $remoteDir/upgrade-root.sh"
    if ($LASTEXITCODE -ne 0) { throw 'Could not secure uploaded Staging helper files.' }
    Invoke-UpgradePreflight $false $true @{ 'kitsusync-staging-deploy'=$helperSha; 'upgrade-root.sh'=$upgradeSha }

    $emergency = @(& $ssh -o BatchMode=yes -o StrictHostKeyChecking=yes -l $emergencyUser $emergencyHost 'id -un; id -u' 2>&1)
    $emergencyStatus = $LASTEXITCODE
    $expectedEmergencyUid = @(& $ssh @sshArgs $stagingHost "id -u $emergencyUser" 2>&1)
    $expectedEmergencyStatus = $LASTEXITCODE
    if ($emergencyStatus -ne 0 -or $expectedEmergencyStatus -ne 0 -or $expectedEmergencyUid.Count -ne 1 -or $emergency.Count -lt 2 -or $emergency[0].Trim() -ne $emergencyUser -or
        $emergency[1].Trim() -notmatch '^[0-9]+$' -or $emergency[1].Trim() -ne $expectedEmergencyUid[0].Trim()) {
        throw "Emergency account identity mismatch: $($emergency -join ' ')"
    }
    & $ssh -tt -o BatchMode=yes -o StrictHostKeyChecking=yes -l $emergencyUser $emergencyHost "sudo /bin/bash $remoteDir/upgrade-root.sh $CommitSha $helperSha $upgradeSha"
    if ($LASTEXITCODE -ne 0) { throw 'Privileged Staging helper upgrade failed.' }
    $installed = @(& $ssh @sshArgs $stagingHost 'sudo -n /usr/local/sbin/kitsusync-staging-deploy --contract-info' 2>&1)
    if ($LASTEXITCODE -ne 0 -or ($installed -join "`n") -notmatch [regex]::Escape('STAGING_HELPER_CONTRACT=staging-v10 incoming=/var/tmp/kitsusync-staging-candidate-<sha> owners=operators.conf')) {
        throw "Installed Staging helper contract verification failed: $($installed -join ' ')"
    }
    Write-Output "STAGING_HELPER_UPGRADE=PASS source_sha=$CommitSha helper_sha256=$helperSha"
} finally {
    Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
}
