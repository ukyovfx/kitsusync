$ErrorActionPreference = 'Stop'
$taskName = 'KitsuSync VFXStudio Review Tunnels'
$runner = Join-Path $PSScriptRoot 'kitsusync-vfxstudio-review-tunnel.ps1'
if (-not (Test-Path -LiteralPath $runner -PathType Leaf)) { throw 'Tunnel runner is missing.' }

$busy = @(Get-NetTCPConnection -State Listen -LocalPort 18090, 18091 -ErrorAction SilentlyContinue)
if ($busy.Count -gt 0) {
    $busy | ForEach-Object {
        $proc = Get-CimInstance Win32_Process -Filter "ProcessId = $($_.OwningProcess)"
        throw "Local review port $($_.LocalPort) is already owned by PID $($_.OwningProcess) ($($proc.Name)); no task was installed."
    }
}
if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) {
    throw "Scheduled task '$taskName' already exists; inspect it before replacing anything."
}

$powershell = (Get-Command powershell.exe -ErrorAction Stop).Source
$arguments = '-NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "{0}"' -f $runner
$action = New-ScheduledTaskAction -Execute $powershell -Argument $arguments
$trigger = New-ScheduledTaskTrigger -AtLogOn -User ([Security.Principal.WindowsIdentity]::GetCurrent().Name)
$principal = New-ScheduledTaskPrincipal -UserId ([Security.Principal.WindowsIdentity]::GetCurrent().Name) -LogonType Interactive -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) -StartWhenAvailable
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Description 'Maintains the fixed SSH review forwards for vfxstudio KitsuSync Production and Staging.' | Out-Null
Start-ScheduledTask -TaskName $taskName
Start-Sleep -Seconds 3
$listeners = @(Get-NetTCPConnection -State Listen -LocalPort 18090, 18091 -ErrorAction SilentlyContinue)
if ($listeners.Count -ne 2) {
    Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
    throw 'SSH did not establish both local listeners; the new task was removed.'
}
Write-Output "TUNNEL_TASK=INSTALLED"
Write-Output "TUNNEL_TASK_NAME=$taskName"
Write-Output 'TUNNEL_LISTENERS=127.0.0.1:18090,127.0.0.1:18091'
