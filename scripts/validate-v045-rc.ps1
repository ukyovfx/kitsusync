$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root
$Docker = 'C:\Users\mynti\AppData\Local\Programs\DockerDesktop\resources\bin\docker.exe'
$result = [ordered]@{ FULL_GO_TEST='FAIL'; GO_VET='FAIL'; GO_MOD_VERIFY='FAIL'; GOVULNCHECK='FAIL'; REACHABLE_VULNS=0; NON_REACHABLE_FINDINGS=0; COMPOSE='FAIL'; DOCKER_BUILD='FAIL'; CONTAINER_START='FAIL'; HEALTH='FAIL'; READINESS='FAIL'; UID_GID='FAIL'; MIGRATION='FAIL'; API_OVERRIDE_TEST='MISSING_TEST'; AUTH_REDIRECT_TEST='MISSING_TEST'; DNS_REBIND_TEST='MISSING_TEST'; PRIVATE_DNS_TEST='MISSING_TEST'; RATE_LIMIT_TEST='MISSING_TEST'; VERSION_045='FAIL'; DIFF_CHECK='FAIL'; GOFMT='FAIL'; SECRET_SCAN='FAIL'; FINAL='FAIL' }
$failureDetails = [System.Collections.Generic.List[string]]::new()
$cleanupFailed = $false
$project = 'kitsusync-v045-rc-' + ([guid]::NewGuid().ToString('N').Substring(0,8)); $image = "$project`:candidate"; $container = "$project-app"; $goContainer = "$project-go"; $tempData = Join-Path ([IO.Path]::GetTempPath()) $project
function Add-Failure([string]$name,[string]$detail){$failureDetails.Add("$name`: $detail")}
function Test-GoInfrastructureFailure([string]$text){
  # Progress such as `go: downloading <module>` is not evidence of failure.
  # Require an explicit network, proxy, TLS, DNS, or download error instead.
  return $text -match '(?im)(no such host|server misbehaving|temporary failure in name resolution|temporary network failure|i/o timeout|timed out|connection reset|connection refused|network is unreachable|proxyconnect.*(?:error|failed|refused|timeout)|(?:tls handshake.*(?:timeout|failed|error)|x509:|proxy error)|(?:download|fetch|module(?: lookup)?|tool|sumdb).*(?:failed|failure|error|unable|disabled)|(?:failed|failure|error|unable).*(?:download|fetch|module|tool))'
}
function Invoke-GoProcess([string[]]$arguments){
  $stdoutFile = Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid().ToString('N') + '.out')
  $stderrFile = Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid().ToString('N') + '.err')
  try {
    $process = Start-Process -FilePath $Docker -ArgumentList (@('exec',$goContainer) + $arguments) -Wait -PassThru -NoNewWindow -RedirectStandardOutput $stdoutFile -RedirectStandardError $stderrFile
    $stdout = if(Test-Path -LiteralPath $stdoutFile){Get-Content $stdoutFile -Raw -ErrorAction SilentlyContinue}else{''}
    $stderr = if(Test-Path -LiteralPath $stderrFile){Get-Content $stderrFile -Raw -ErrorAction SilentlyContinue}else{''}
  } finally {
    Remove-Item -LiteralPath $stdoutFile,$stderrFile -Force -ErrorAction SilentlyContinue
  }
  [pscustomobject]@{ Output=@($stdout -split "`r?`n" | Where-Object {$_ -ne ''}); Stdout=$stdout; Stderr=$stderr; Text=(($stdout,$stderr) -join "`n"); ExitCode=$process.ExitCode }
}
function Remove-Disposable([string[]]$arguments,[string]$label){
  $stdoutFile = Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid().ToString('N') + '.out')
  $stderrFile = Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid().ToString('N') + '.err')
  try {
    $process = Start-Process -FilePath $Docker -ArgumentList $arguments -Wait -PassThru -NoNewWindow -RedirectStandardOutput $stdoutFile -RedirectStandardError $stderrFile
    $output = ((Get-Content $stdoutFile -Raw -ErrorAction SilentlyContinue), (Get-Content $stderrFile -Raw -ErrorAction SilentlyContinue) -join "`n")
  } finally {
    Remove-Item -LiteralPath $stdoutFile,$stderrFile -Force -ErrorAction SilentlyContinue
  }
  if($process.ExitCode -ne 0 -and $output -notmatch '(?i)(no such container|no such image|no such volume|does not exist|not found|already removed)'){
    $script:cleanupFailed = $true
    $failureDetails.Add("CLEANUP_$label`: $($output.Trim())")
  }
}
function Run-GoExec([string[]]$arguments){
  $run = Invoke-GoProcess $arguments
  if($run.ExitCode -ne 0){
    $kind = if(Test-GoInfrastructureFailure $run.Text){'GO_INFRA_FAILURE'}else{'GO_COMMAND_FAILURE'}
    throw "$kind`: $($run.Text.Trim())"
  }
  return $run.Output
}
function Test-Source([string[]]$patterns){$files=Get-ChildItem src -Recurse -Filter '*_test.go';foreach($p in $patterns){if($files|Select-String -Pattern $p){return $true}};return $false}
try {
  if(-not(Test-Path -LiteralPath $Docker)){throw "Docker executable not found: $Docker"}; & $Docker version | Out-Null; if($LASTEXITCODE -ne 0){throw 'Docker version/info is unreachable'}
  & $Docker run -d --name $goContainer -v "${Root}:/src" -w /src -e CGO_ENABLED=1 golang:1.26-bookworm sleep infinity | Out-Null; if($LASTEXITCODE){throw 'Go validation container failed to start'}
  $goVersion = (& $Docker exec $goContainer go version | Out-String).Trim(); if($goVersion -notmatch '^go version go1\.26(?:\.\d+)?(?:\s|$)') { throw "Go validation preflight failed: $goVersion" }
  try{Run-GoExec @('go','test','./src/...','-count=1','-timeout=120s') | Out-Null;$result.FULL_GO_TEST='PASS'}catch{if($_.Exception.Message -like 'GO_INFRA_FAILURE:*'){$result.FULL_GO_TEST='INFRA_FAILURE'};Add-Failure 'FULL_GO_TEST' $_.Exception.Message}
  try{Run-GoExec @('go','vet','./src/...');$result.GO_VET='PASS'}catch{if($_.Exception.Message -like 'GO_INFRA_FAILURE:*'){$result.GO_VET='INFRA_FAILURE'};Add-Failure 'GO_VET' $_.Exception.Message}
  try{Run-GoExec @('go','mod','verify');$result.GO_MOD_VERIFY='PASS'}catch{if($_.Exception.Message -like 'GO_INFRA_FAILURE:*'){$result.GO_MOD_VERIFY='INFRA_FAILURE'};Add-Failure 'GO_MOD_VERIFY' $_.Exception.Message}
  try{
    Run-GoExec @('go','install','golang.org/x/vuln/cmd/govulncheck@v1.7.0') | Out-Null
    $scan = Invoke-GoProcess @('/go/bin/govulncheck','-json','./src/...')
    $osvIDs = [System.Collections.Generic.HashSet[string]]::new()
    $findingIDs = [System.Collections.Generic.HashSet[string]]::new()
    foreach($line in $scan.Output){
      try{$record = ($line.ToString() | ConvertFrom-Json -ErrorAction Stop)}catch{continue}
      if($record.osv -and $record.osv.id){[void]$osvIDs.Add([string]$record.osv.id)}
      if($record.finding -and $record.finding.osv){[void]$findingIDs.Add([string]$record.finding.osv)}
    }
    $result.REACHABLE_VULNS = $findingIDs.Count
    $result.NON_REACHABLE_FINDINGS = @($osvIDs | Where-Object {-not $findingIDs.Contains($_)}).Count
    if($result.REACHABLE_VULNS -gt 0){throw "GO_REACHABLE_VULNS`: $($result.REACHABLE_VULNS) reachable vulnerabilities"}
    if($scan.ExitCode -ne 0){
      $kind = if(Test-GoInfrastructureFailure $scan.Text){'GO_INFRA_FAILURE'}else{'GO_COMMAND_FAILURE'}
      throw "$kind`: govulncheck exited $($scan.ExitCode): $($scan.Text.Trim())"
    }
    $result.GOVULNCHECK='PASS'
  }catch{if($_.Exception.Message -like 'GO_INFRA_FAILURE:*'){$result.GOVULNCHECK='INFRA_FAILURE'};Add-Failure 'GOVULNCHECK' $_.Exception.Message}
  try{& $Docker compose -p $project -f docker-compose.test.yml config -q;if($LASTEXITCODE){throw 'compose config failed'};$result.COMPOSE='PASS'}catch{Add-Failure 'COMPOSE' $_.Exception.Message}
  try{& $Docker build --pull -t $image .;if($LASTEXITCODE){throw 'image build failed'};$result.DOCKER_BUILD='PASS'}catch{Add-Failure 'DOCKER_BUILD' $_.Exception.Message}
  New-Item -ItemType Directory -Force -Path $tempData | Out-Null
  & $Docker run -d --name $container --env-file .env.local -e APP_ENV=development -p 127.0.0.1:18092:8090 -v "${tempData}:/app/data" -v "${Root}\conf.toml:/app/conf.toml:ro" -v "${Root}\tpl:/app/tpl:ro" $image | Out-Null; if($LASTEXITCODE){throw 'disposable container failed to start'}; $result.CONTAINER_START='PASS'; Start-Sleep 3
  $logs=& $Docker logs $container 2>&1|Out-String; if($logs -notmatch '(?i)migration.*(fail|error)|schema.*(fail|error)'){$result.MIGRATION='PASS'}else{Add-Failure 'MIGRATION' 'startup logs report migration/schema failure'}
  for($i=0;$i -lt 20 -and $result.HEALTH -ne 'PASS';$i++){try{$h=Invoke-WebRequest 'http://127.0.0.1:18092/health' -UseBasicParsing -TimeoutSec 3;if($h.StatusCode -eq 200){$result.HEALTH='PASS'}}catch{Start-Sleep 2}}; if($result.HEALTH -ne 'PASS'){Add-Failure 'HEALTH' 'health endpoint did not return HTTP 200'}
  try{$s=Invoke-WebRequest 'http://127.0.0.1:18092/api/setup/status' -UseBasicParsing -TimeoutSec 3;if($s.StatusCode -in 200,401,403){$result.READINESS='PASS'}}catch{if($_.Exception.Response.StatusCode.value__ -in 401,403){$result.READINESS='PASS'}}; if($result.READINESS -ne 'PASS'){Add-Failure 'READINESS' 'setup status endpoint was not reachable/protected as expected'}
  $uid=& $Docker exec $container id -u 2>$null;$gid=& $Docker exec $container id -g 2>$null;if($uid.Trim() -eq '10001' -and $gid.Trim() -eq '10001'){$result.UID_GID='PASS'}else{Add-Failure 'UID_GID' "expected 10001:10001, got $uid`:$gid"}
  $focused = @(@('TestAPIOverridePersistsAndReadsBack','TestExplicitAPIOverrideLeavesDisplayURLUnchanged'),@('TestAuth307DoesNotReplayCredentials'),@('TestAuth308DoesNotReplayCredentials'),@('TestRecoverRuntimeCredentialsRejectsAuthRedirect'),@('TestDNSChangeAfterVerificationBlocksCredentials'),@('TestPublicToPrivateAndLoopbackRebindingFailsClosed'),@('TestExplicitPrivateAndTailscaleDNSScopesAllowed'),@('TestSetupProbeRateLimitIsBoundedPerPeer','TestSetupProbeRateLimitHasIndependentPeersAndExpires'))
  foreach($group in $focused){$run = '^(?:' + ($group -join '|') + ')$';$args=@('go','test','./src/setup','./src/api/kitsu','-run',$run,'-count=1');try{Run-GoExec $args;if($group[0] -like 'TestAPI*'){$result.API_OVERRIDE_TEST='PASS'}elseif($group[0] -like 'TestAuth*' -or $group[0] -like 'TestRecover*'){$result.AUTH_REDIRECT_TEST='PASS'}elseif($group[0] -like 'TestDNS*' -or $group[0] -like 'TestPublic*'){$result.DNS_REBIND_TEST='PASS'}elseif($group[0] -like 'TestExplicitPrivate*'){$result.PRIVATE_DNS_TEST='PASS'}else{$result.RATE_LIMIT_TEST='PASS'}}catch{Add-Failure "FOCUSED_$($group[0])" $_.Exception.Message}}
  if(-not(Test-Source @('TestAPIOverridePersistsAndReadsBack','TestExplicitAPIOverrideLeavesDisplayURLUnchanged'))){$result.API_OVERRIDE_TEST='MISSING_TEST'}
  $buildVersion=Get-Content src/build_info.go -Raw;$dockerfileVersion=Get-Content Dockerfile -Raw;$composeVersion=Get-Content docker-compose.yml -Raw;if($buildVersion -match 'BuildVersion\s*=\s*"0\.4\.5"' -and $dockerfileVersion -match 'ARG APP_VERSION=0\.4\.5' -and $composeVersion -match 'APP_VERSION:\s*"0\.4\.5"'){$result.VERSION_045='PASS'}else{Add-Failure 'VERSION_045' 'active version source is not consistently 0.4.5'}
  try{git diff --check;if($LASTEXITCODE){throw 'git diff --check failed'};$result.DIFF_CHECK='PASS'}catch{Add-Failure 'DIFF_CHECK' $_.Exception.Message}; $changedGo=git diff --name-only -- '*.go';$bad=if($changedGo){gofmt -l $changedGo}else{@()};if(-not $bad){$result.GOFMT='PASS'}else{Add-Failure 'GOFMT' ($bad -join ', ')}
  $secretHits=Get-ChildItem src -Recurse -Filter '*.go'|Select-String -Pattern '(?i)(log|slog)\.[A-Za-z]+\([^\r\n]*(password|secret|token|authorization|cookie|ciphertext|encryption)';$unsafe=@();$bareSecretValuePattern='(?i)(?:,\s*)(?:password|token|accessToken|sessionToken|secret|ciphertext|authorization|cookie)\s*(?:,|\)|$)';$labeledSecretValuePattern='(?i)"(?:password|token|access_token|authorization|cookie|ciphertext|session_token)"\s*,\s*(?<value>[^,\)]+)';$headerValuePattern='(?i)(?:Authorization|Cookie)\s*[:=]\s*(?<value>[^,\)]+)';if($secretHits){Write-Output 'SECRET_SCAN_REVIEW';foreach($hit in $secretHits){$line="$($hit.Path):$($hit.LineNumber): $($hit.Line.Trim())";Write-Output $line;$actualValue=$false;if($hit.Line -match $bareSecretValuePattern){$actualValue=$true};$labeled=[regex]::Match($hit.Line,$labeledSecretValuePattern);if($labeled.Success){$value=$labeled.Groups['value'].Value.Trim();if(-not $value.StartsWith('"') -and -not $value.StartsWith("'") -and $value -notmatch '^(?i:true|false)$'){$actualValue=$true}};$header=[regex]::Match($hit.Line,$headerValuePattern);if($header.Success){$value=$header.Groups['value'].Value.Trim();if(-not $value.StartsWith('"') -and -not $value.StartsWith("'")){$actualValue=$true}};if($actualValue){$unsafe+=$line}}};if(-not $unsafe){$result.SECRET_SCAN='PASS'}else{Add-Failure 'SECRET_SCAN' ($unsafe -join ' | ')}
}catch{Add-Failure 'DOCKER_PRECHECK' $_.Exception.Message}
finally {
  if(Test-Path -LiteralPath $Docker){
    foreach($name in @($container,$goContainer)){if($name){Remove-Disposable @('rm','-f',$name) 'CONTAINER'}}
    Remove-Disposable @('compose','-p',$project,'-f','docker-compose.test.yml','down','-v','--remove-orphans') 'COMPOSE'
    Remove-Disposable @('image','rm',$image) 'IMAGE'
  }
  if(Test-Path $tempData){Remove-Item -LiteralPath $tempData -Recurse -Force -ErrorAction SilentlyContinue}
  $core=@('FULL_GO_TEST','GO_VET','GO_MOD_VERIFY','GOVULNCHECK','COMPOSE','DOCKER_BUILD','CONTAINER_START','HEALTH','READINESS','UID_GID','MIGRATION','VERSION_045','DIFF_CHECK','GOFMT','SECRET_SCAN');$result.FINAL=if((-not $cleanupFailed) -and (($core|Where-Object{$result[$_] -ne 'PASS'}).Count -eq 0)){'PASS'}else{'FAIL'}; Write-Output 'KITSUSYNC_V045_RC_VALIDATION';foreach($k in $result.Keys){Write-Output "$k=$($result[$k])"};if($result.FINAL -eq 'FAIL'){Write-Output '';Write-Output 'FAILING_GATES';$failureDetails|ForEach-Object{Write-Output $_}}
}
