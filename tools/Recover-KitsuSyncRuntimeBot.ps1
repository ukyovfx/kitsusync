param(
    [string]$KitsuHost = "http://127.0.0.1:8080",
    [string]$BotEmail = "kitsusync-bot@google.com",
    [string]$Container = "kitsulocal-app"
)

$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo

if ($BotEmail -ne "kitsusync-bot@google.com") {
    Fail "Recovery stopped: only the owned KitsuSync runtime identity is allowed."
}

function Fail([string]$Message) {
    throw $Message
}

$sql = "SELECT count(*) || '|' || coalesce(string_agg(format('%s/%s/%s/%s', active, archived, role, is_bot), ','),'none') FROM person WHERE lower(coalesce(email,'')) = lower('$BotEmail');"
$raw = docker exec -u postgres $Container psql -d zoudb -F '|' -Atc $sql
if ($LASTEXITCODE -ne 0) { Fail "Kitsu bot ownership check failed." }
$parts = $raw.Trim().Split('|', 2)
if ($parts.Count -ne 2 -or $parts[0] -ne "1" -or $parts[1] -ne "t/f/admin/t") {
    Fail "Recovery stopped: expected exactly one active, unarchived owned bot." 
}

$bytes = New-Object byte[] 32
$rng = [Security.Cryptography.RandomNumberGenerator]::Create()
try {
    $rng.GetBytes($bytes)
}
finally {
    $rng.Dispose()
}
$newPassword = "Ksr-" + ([Convert]::ToBase64String($bytes).TrimEnd('=').Replace('+','-').Replace('/','_'))
$bytes = $null

try {
    # Password is sent through stdin, never as a docker or Zou CLI argument.
    $newPassword | docker exec -i $Container sh -lc '/opt/zou/env/bin/zou change-password "$1" --password "$(cat)"' sh $BotEmail | Out-Null
    if ($LASTEXITCODE -ne 0) { Fail "Zou password reset failed." }

    # The Go helper authenticates first, then encrypts and verifies persistence.
    if (Get-Command go -ErrorAction SilentlyContinue) {
        $newPassword | go run .\tools\runtime-recovery --db data/sqlite.db --host $KitsuHost --email $BotEmail | Out-Null
    } else {
        $dockerHost = $KitsuHost.Replace("127.0.0.1", "host.docker.internal").Replace("localhost", "host.docker.internal")
        $newPassword | docker run --rm -i -v "${repo}:/app" -w /app golang:1.21-bookworm go run ./tools/runtime-recovery --db data/sqlite.db --host $dockerHost --email $BotEmail | Out-Null
    }
    if ($LASTEXITCODE -ne 0) { Fail "Runtime authentication or credential persistence failed." }

    docker compose restart app | Out-Null
    if ($LASTEXITCODE -ne 0) { Fail "KitsuSync restart failed." }
    $deadline = (Get-Date).AddSeconds(30)
    do {
        Start-Sleep -Seconds 2
        $health = curl.exe -fsS http://127.0.0.1:8090/health 2>$null
    } while ($LASTEXITCODE -ne 0 -and (Get-Date) -lt $deadline)
    if ($LASTEXITCODE -ne 0 -or $health -notmatch '"mode":"configured"' -or $health -notmatch '"kitsu":"connected"') {
        Fail "KitsuSync did not restore configured state after restart."
    }
    Write-Output "Runtime bot recovery succeeded; configured state restored."
}
finally {
    $newPassword = $null
    [GC]::Collect()
    [GC]::WaitForPendingFinalizers()
}
