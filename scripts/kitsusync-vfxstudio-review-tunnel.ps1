$ErrorActionPreference = 'Stop'
$localPorts = @(18090, 18091)
$expectedForwards = @(
    '-L 127.0.0.1:18090:127.0.0.1:8090',
    '-L 127.0.0.1:18091:127.0.0.1:8091'
)
$logDir = Join-Path $env:LOCALAPPDATA 'KitsuSync\ReviewTunnel'
$logPath = Join-Path $logDir 'tunnel.log'
New-Item -ItemType Directory -Force -Path $logDir | Out-Null

function Write-TunnelLog([string]$Message) {
    Add-Content -LiteralPath $logPath -Value ('{0:u} {1}' -f [DateTime]::UtcNow, $Message)
}

$listeners = @(Get-NetTCPConnection -State Listen -LocalPort $localPorts -ErrorAction SilentlyContinue)
if ($listeners.Count -gt 0) {
    $owners = @($listeners | Select-Object -ExpandProperty OwningProcess -Unique)
    if ($owners.Count -eq 1) {
        $owner = Get-CimInstance Win32_Process -Filter "ProcessId = $($owners[0])"
        $matchesExpected = $owner.Name -ieq 'ssh.exe'
        foreach ($forward in $expectedForwards) { $matchesExpected = $matchesExpected -and $owner.CommandLine.Contains($forward) }
        if ($matchesExpected -and $listeners.Count -eq 2) { exit 0 }
    }
    Write-TunnelLog 'ERROR local review port is owned by an unexpected process; refusing to start SSH'
    exit 41
}

$ssh = (Get-Command ssh.exe -ErrorAction Stop).Source
$sshArgs = @(
    '-N', '-T',
    '-o', 'BatchMode=yes',
    '-o', 'ExitOnForwardFailure=yes',
    '-o', 'ServerAliveInterval=30',
    '-o', 'ServerAliveCountMax=3',
    '-o', 'StrictHostKeyChecking=yes',
    '-L', '127.0.0.1:18090:127.0.0.1:8090',
    '-L', '127.0.0.1:18091:127.0.0.1:8091',
    'vfxstudio'
)
Write-TunnelLog 'Starting the two fixed loopback review forwards'
$process = Start-Process -FilePath $ssh -ArgumentList $sshArgs -PassThru -NoNewWindow -RedirectStandardOutput (Join-Path $logDir 'ssh.stdout.log') -RedirectStandardError (Join-Path $logDir 'ssh.stderr.log')
$process.WaitForExit()
Write-TunnelLog "SSH tunnel exited with code $($process.ExitCode)"
exit $process.ExitCode
