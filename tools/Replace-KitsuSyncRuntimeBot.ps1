param(
    [ValidateSet("Prepare", "Finalize")]
    [string]$Phase = "Prepare",
    [string]$KitsuHost = "http://127.0.0.1:8080",
    [string]$Container = "kitsulocal-app"
)

$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo
$canonicalEmail = "kitsusync-bot@google.com"
$statePath = Join-Path $repo "data\runtime-replacement-state.json"

function Fail([string]$Message) { throw $Message }
function Read-PlainPassword([string]$Prompt) {
    $secure = Read-Host -Prompt $Prompt -AsSecureString
    $ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
    try { return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr) }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr) }
}
function Invoke-RecoveryGo([string[]]$Arguments, [string]$InputText) {
    $dockerArgs = @("run", "--rm", "-i", "-v", "${repo}:/app", "-w", "/app", "golang:1.21-bookworm", "go", "run", "./tools/runtime-recovery") + $Arguments
    if (Get-Command go -ErrorAction SilentlyContinue) {
        $InputText | go run .\tools\runtime-recovery @Arguments
    } else {
        $InputText | docker @dockerArgs
    }
    if ($LASTEXITCODE -ne 0) { Fail "Recovery phase failed." }
}
function New-Backup {
    $backup = Join-Path $repo ("data\recovery-backups\" + (Get-Date -Format "yyyyMMdd-HHmmss"))
    New-Item -ItemType Directory -Path $backup -Force | Out-Null
    Get-ChildItem data\sqlite.db* -ErrorAction SilentlyContinue | Copy-Item -Destination $backup
    Copy-Item data\runtime-secret.key $backup
    Write-Output "Backup created: $backup"
}
function Restart-And-Verify {
    docker compose restart app | Out-Null
    if ($LASTEXITCODE -ne 0) { Fail "KitsuSync restart failed." }
    $deadline = (Get-Date).AddSeconds(30)
    do { Start-Sleep -Seconds 2; $health = curl.exe -fsS http://127.0.0.1:8090/health 2>$null } while ($LASTEXITCODE -ne 0 -and (Get-Date) -lt $deadline)
    if ($LASTEXITCODE -ne 0 -or $health -notmatch '"mode":"configured"' -or $health -notmatch '"kitsu":"connected"') { Fail "Configured state was not restored." }
}

if ($Phase -eq "Prepare") {
    if (Test-Path $statePath) { Fail "Prepare state already exists; inspect it before retrying." }
    $lookup = docker exec -u postgres $Container psql -d zoudb -F '|' -Atc "SELECT count(*) || '|' || coalesce(string_agg(id || '/' || active || '/' || archived || '/' || role || '/' || is_bot, ','),'none') FROM person WHERE lower(email)=lower('$canonicalEmail');"
    if ($LASTEXITCODE -ne 0) { Fail "Ownership lookup failed." }
    $parts = $lookup.Trim().Split('|', 2)
    if ($parts.Count -ne 2 -or $parts[0] -ne "1" -or $parts[1] -notmatch '/t/f/admin/t$') { Fail "Expected exactly one owned canonical bot." }
    $oldID = ($parts[1] -split '/')[0]
    New-Backup
    $adminEmail = Read-Host "Kitsu admin email"
    $adminPassword = Read-PlainPassword "Kitsu admin password"
    $tempEmail = "kitsusync-recovery-$([Guid]::NewGuid().ToString('N'))@google.com"
    $bytes = New-Object byte[] 32
    $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
    $tempPassword = "Ksr-" + ([Convert]::ToBase64String($bytes).TrimEnd('=').Replace('+','-').Replace('/','_'))
    $bytes = $null
    try {
        $hostForHelper = $KitsuHost
        if (-not (Get-Command go -ErrorAction SilentlyContinue)) { $hostForHelper = $KitsuHost.Replace('127.0.0.1','host.docker.internal').Replace('localhost','host.docker.internal') }
        $replacementID = (Invoke-RecoveryGo @("-phase", "prepare", "-db", "data/sqlite.db", "-host", $hostForHelper, "-email", $adminEmail, "-temp-email", $tempEmail) "$adminPassword`n$tempPassword").Trim()
        if ([string]::IsNullOrWhiteSpace($replacementID)) { Fail "Replacement ID was not returned." }
        @{ old_id=$oldID; replacement_id=$replacementID; replacement_email=$tempEmail; canonical_email=$canonicalEmail } | ConvertTo-Json | Set-Content -Encoding UTF8 $statePath
        Restart-And-Verify
        Write-Output "Phase 1 prepared. Stop here and explicitly approve Phase 2."
    } finally { $adminPassword=$null; $tempPassword=$null; [GC]::Collect() }
} else {
    if (-not (Test-Path $statePath)) { Fail "Prepare state is missing." }
    $state = Get-Content $statePath -Raw | ConvertFrom-Json
    New-Backup
    $adminEmail = Read-Host "Kitsu admin email"
    $adminPassword = Read-PlainPassword "Kitsu admin password"
    try {
        $hostForHelper = $KitsuHost
        if (-not (Get-Command go -ErrorAction SilentlyContinue)) { $hostForHelper = $KitsuHost.Replace('127.0.0.1','host.docker.internal').Replace('localhost','host.docker.internal') }
        Invoke-RecoveryGo @("-phase", "finalize", "-db", "data/sqlite.db", "-host", $hostForHelper, "-email", $adminEmail, "-old-id", $state.old_id, "-temp-id", $state.replacement_id, "-temp-email", $state.replacement_email) "$adminPassword`n"
        Restart-And-Verify
        Remove-Item $statePath
        Write-Output "Phase 2 finalized. Replacement now uses the canonical identity."
    } finally { $adminPassword=$null; [GC]::Collect() }
}
