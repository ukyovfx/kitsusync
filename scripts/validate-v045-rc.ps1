$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root
$Docker = 'C:\Users\mynti\AppData\Local\Programs\DockerDesktop\resources\bin\docker.exe'
$result = [ordered]@{ FULL_GO_TEST='FAIL'; GO_VET='FAIL'; GO_MOD_VERIFY='FAIL'; GOVULNCHECK='FAIL'; REACHABLE_VULNS=0; NON_REACHABLE_FINDINGS=0; COMPOSE='FAIL'; DOCKER_BUILD='FAIL'; CONTAINER_START='FAIL'; HEALTH='FAIL'; READINESS='FAIL'; UID_GID='FAIL'; MIGRATION='FAIL'; API_OVERRIDE_TEST='MISSING_TEST'; AUTH_REDIRECT_TEST='MISSING_TEST'; DNS_REBIND_TEST='MISSING_TEST'; PRIVATE_DNS_TEST='MISSING_TEST'; RATE_LIMIT_TEST='MISSING_TEST'; VERSION_045='FAIL'; DIFF_CHECK='FAIL'; GOFMT='FAIL'; SECRET_SCAN='FAIL'; FINAL='FAIL' }
$hardGates = @('FULL_GO_TEST','GO_VET','GO_MOD_VERIFY','GOVULNCHECK','COMPOSE','DOCKER_BUILD','CONTAINER_START','HEALTH','READINESS','UID_GID','MIGRATION','API_OVERRIDE_TEST','AUTH_REDIRECT_TEST','DNS_REBIND_TEST','PRIVATE_DNS_TEST','RATE_LIMIT_TEST','VERSION_045','DIFF_CHECK','GOFMT','SECRET_SCAN')
$failureDetails = [System.Collections.Generic.List[string]]::new()
$cleanupFailed = $false
$focusedAttempted = $false
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
  $dockerArguments = @('exec',$goContainer) + @($arguments)
  try {
    $process = Start-Process -FilePath $Docker -ArgumentList $dockerArguments -WorkingDirectory $Root -Wait -PassThru -NoNewWindow -RedirectStandardOutput $stdoutFile -RedirectStandardError $stderrFile
    $stdout = if(Test-Path -LiteralPath $stdoutFile){Get-Content $stdoutFile -Raw -ErrorAction SilentlyContinue}else{''}
    $stderr = if(Test-Path -LiteralPath $stderrFile){Get-Content $stderrFile -Raw -ErrorAction SilentlyContinue}else{''}
  } finally {
    Remove-Item -LiteralPath $stdoutFile,$stderrFile -Force -ErrorAction SilentlyContinue
  }
  [pscustomobject]@{ Executable=$Docker; Arguments=$dockerArguments; WorkingDirectory=$Root; Output=@($stdout -split "`r?`n" | Where-Object {$_ -ne ''}); Stdout=$stdout; Stderr=$stderr; Text=(($stdout,$stderr) -join "`n"); ExitCode=$process.ExitCode }
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
function Get-FinalValidationResult([System.Collections.IDictionary]$state,[bool]$cleanupSucceeded){
  if(-not $cleanupSucceeded){return 'FAIL'}
  foreach($gate in $hardGates){if($state[$gate] -ne 'PASS'){return 'FAIL'}}
  return 'PASS'
}
function Assert-FinalGateAggregation(){
  $allPass = [ordered]@{}
  foreach($gate in $hardGates){$allPass[$gate] = 'PASS'}
  if((Get-FinalValidationResult $allPass $true) -ne 'PASS'){throw 'validator self-check failed: all-pass state was rejected'}
  foreach($gate in @('API_OVERRIDE_TEST','AUTH_REDIRECT_TEST','DNS_REBIND_TEST','PRIVATE_DNS_TEST','RATE_LIMIT_TEST')){
    $case = [ordered]@{}
    foreach($name in $hardGates){$case[$name] = 'PASS'}
    $case[$gate] = if($gate -eq 'AUTH_REDIRECT_TEST'){'MISSING_TEST'}else{'FAIL'}
    if((Get-FinalValidationResult $case $true) -ne 'FAIL'){throw "validator self-check failed: $gate did not block final PASS"}
  }
}
function Get-LoggingCalls([string]$path){
  $lines = Get-Content -LiteralPath $path
  $calls = [System.Collections.Generic.List[object]]::new()
  $inCall = $false
  $depth = 0
  $startLine = 0
  $buffer = [System.Text.StringBuilder]::new()
  for($index = 0; $index -lt $lines.Count; $index++){
    $line = [string]$lines[$index]
    if(-not $inCall){
      $match = [regex]::Match($line, '(?i)(?:log|slog)\.[A-Za-z]+\s*\(')
      if(-not $match.Success){continue}
      $inCall = $true
      $startLine = $index + 1
      [void]$buffer.Clear()
      [void]$buffer.Append($line.Substring($match.Index))
    }else{
      [void]$buffer.Append("`n")
      [void]$buffer.Append($line)
    }
    $depth += ([regex]::Matches($line, '\(')).Count
    $depth -= ([regex]::Matches($line, '\)')).Count
    if($depth -le 0){
      [void]$calls.Add([pscustomobject]@{StartLine=$startLine;Text=$buffer.ToString()})
      $inCall = $false
      $depth = 0
    }
  }
  if($inCall){[void]$calls.Add([pscustomobject]@{StartLine=$startLine;Text=$buffer.ToString()})}
  return $calls
}
function Test-SafeSecretMetadata([string]$value){
  $trimmed = $value.Trim()
  if($trimmed -match '^(?:true|false|\d+)$'){return $true}
  if($trimmed.StartsWith('"') -or $trimmed.StartsWith("'")){return $true}
  if($trimmed -match '^(?i)(?:len\(|(?:token|password|secret|credential)\s*(?:!=|==)|strings?\.(?:TrimSpace|Contains)\()'){return $true}
  if($trimmed -match '(?i)(?:_present|Present|_configured|Configured|_valid|Valid|_enabled|Enabled|_count|Count|_length|Length|_attempted|Attempted|_success|Success)$'){return $true}
  return $false
}
function Test-LoggingCallSecretValue([string]$text){
  $labeledSecretValuePattern = '(?is),\s*"(?:password|token|access_token|accessToken|session_token|sessionToken|secret|ciphertext|authorization|cookie)"\s*,\s*(?<value>[^,\)]+)'
  $headerValuePattern = '(?is)Header\s*\.\s*Get\(\s*"(?:Authorization|Cookie)"\s*\)|(?:authorization|cookie)\s*[:=]\s*[^,\)]+'
  $variableValuePattern = '(?is),\s*(?<value>(?!(?:"|''))(?:(?:raw|plain|decrypted|stored|runtime|session|access|refresh|auth|bearer|credential|password|token|secret|cookie|authorization)[A-Za-z0-9_]*)(?:\s*,|\s*\)|\s*$))'
  foreach($match in [regex]::Matches($text, $labeledSecretValuePattern)){
    if(-not (Test-SafeSecretMetadata $match.Groups['value'].Value)){return $true}
  }
  if($text -match $headerValuePattern){return $true}
  foreach($match in [regex]::Matches($text, $variableValuePattern)){
    if(-not (Test-SafeSecretMetadata $match.Groups['value'].Value)){return $true}
  }
  return $false
}
function Get-GovulnFindingReachability([object]$finding){
  if($null -eq $finding){return 'none'}
  foreach($frame in @($finding.trace)){
    if($frame.function -and [string]$frame.function.Trim() -ne ''){return 'reachable'}
  }
  return 'nonreachable'
}
function Get-GoTestList([string]$package){
  try{return Invoke-GoProcess @('go','test','-list','^Test','-count=1',$package)}catch{
    [pscustomobject]@{Executable=$Docker;Arguments=@('exec',$goContainer,'go','test','-list','^Test','-count=1',$package);WorkingDirectory=$Root;Output=@();Stdout='';Stderr=$_.Exception.Message;Text=$_.Exception.Message;ExitCode=-1;InvocationError=$true}
  }
}
function Get-GoTestNames([string]$stdout){
  @($stdout -split "`r?`n" | ForEach-Object {$_.Trim()} | Where-Object {$_ -match '^Test[A-Za-z0-9_]+$'} | Select-Object -Unique)
}
function Assert-GoTestNameParser(){
  $sample = "TestAlpha`r`nTestBeta `r`nnot-a-test`r`nTestAlpha`r`n"
  $names = Get-GoTestNames $sample
  if(@($names).Count -ne 2 -or @($names | Where-Object {$_ -ceq 'TestAlpha'}).Count -ne 1 -or @($names | Where-Object {$_ -ceq 'TestBeta'}).Count -ne 1){throw 'validator self-check failed: go test list parser'}
}
function Write-TestDiscoveryDiagnostic([string]$name,[string]$package,[bool]$listed,[int]$exitCode){
  Write-Output 'TEST_DISCOVERY_DIAGNOSTIC'
  Write-Output $name
  Write-Output "package=$package"
  Write-Output "listed=$(if($listed){'YES'}else{'NO'})"
  Write-Output "list_command_exit=$exitCode"
  Write-Output "matched=$(if($listed){'YES'}else{'NO'})"
}
try {
  Assert-FinalGateAggregation
  Assert-GoTestNameParser
  if(-not(Test-Path -LiteralPath $Docker)){throw "Docker executable not found: $Docker"}; & $Docker version | Out-Null; if($LASTEXITCODE -ne 0){throw 'Docker version/info is unreachable'}
  & $Docker run -d --name $goContainer -v "${Root}:/src" -w /src -e CGO_ENABLED=1 golang:1.26-bookworm sleep infinity | Out-Null; if($LASTEXITCODE){throw 'Go validation container failed to start'}
  $goVersion = (& $Docker exec $goContainer go version | Out-String).Trim(); if($goVersion -notmatch '^go version go1\.26(?:\.\d+)?(?:\s|$)') { throw "Go validation preflight failed: $goVersion" }
  try{Run-GoExec @('go','test','./src/...','-count=1','-timeout=120s') | Out-Null;$result.FULL_GO_TEST='PASS'}catch{if($_.Exception.Message -like 'GO_INFRA_FAILURE:*'){$result.FULL_GO_TEST='INFRA_FAILURE'};Add-Failure 'FULL_GO_TEST' $_.Exception.Message}
  try{Run-GoExec @('go','vet','./src/...');$result.GO_VET='PASS'}catch{if($_.Exception.Message -like 'GO_INFRA_FAILURE:*'){$result.GO_VET='INFRA_FAILURE'};Add-Failure 'GO_VET' $_.Exception.Message}
  try{Run-GoExec @('go','mod','verify');$result.GO_MOD_VERIFY='PASS'}catch{if($_.Exception.Message -like 'GO_INFRA_FAILURE:*'){$result.GO_MOD_VERIFY='INFRA_FAILURE'};Add-Failure 'GO_MOD_VERIFY' $_.Exception.Message}
  try{
    Run-GoExec @('go','install','golang.org/x/vuln/cmd/govulncheck@v1.7.0') | Out-Null
    $scan = Invoke-GoProcess @('/go/bin/govulncheck','-json','./src/...')
    $findingIDs = [System.Collections.Generic.HashSet[string]]::new()
    $nonReachableIDs = [System.Collections.Generic.HashSet[string]]::new()
    foreach($line in $scan.Output){
      try{$record = ($line.ToString() | ConvertFrom-Json -ErrorAction Stop)}catch{continue}
      if($record.finding -and $record.finding.osv){
        $osvID = [string]$record.finding.osv
        if((Get-GovulnFindingReachability $record.finding) -eq 'reachable'){[void]$findingIDs.Add($osvID)}else{[void]$nonReachableIDs.Add($osvID)}
      }
    }
    $result.REACHABLE_VULNS = $findingIDs.Count
    $result.NON_REACHABLE_FINDINGS = @($nonReachableIDs | Where-Object {-not $findingIDs.Contains($_)}).Count
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
  $focusedAttempted = $true
  $focused = @(
    [pscustomobject]@{Gate='API_OVERRIDE_TEST';Tests=@(
      [pscustomobject]@{Package='./src/setup';Name='TestAPIOverridePersistsAndReadsBack'},
      [pscustomobject]@{Package='./src/api/kitsu';Name='TestExplicitAPIOverrideLeavesDisplayURLUnchanged'}
    )},
    [pscustomobject]@{Gate='AUTH_REDIRECT_TEST';Tests=@(
      [pscustomobject]@{Package='./src/setup';Name='TestAuth307DoesNotReplayCredentials'},
      [pscustomobject]@{Package='./src/setup';Name='TestAuth308DoesNotReplayCredentials'},
      [pscustomobject]@{Package='./src/setup';Name='TestRecoverRuntimeCredentialsRejectsAuthRedirect'},
      [pscustomobject]@{Package='./src/setup';Name='TestRecoverRuntimeTokenRejectsAuthRedirects'},
      [pscustomobject]@{Package='./src/setup';Name='TestTryKitsuLoginRejects307WithoutCredentialReplay'},
      [pscustomobject]@{Package='./src/setup';Name='TestTryKitsuLoginRejects308WithoutCredentialReplay'}
    )},
    [pscustomobject]@{Gate='DNS_REBIND_TEST';Tests=@(
      [pscustomobject]@{Package='./src/setup';Name='TestDNSChangeAfterVerificationBlocksCredentials'},
      [pscustomobject]@{Package='./src/setup';Name='TestTryKitsuLoginBlocksDNSChangeBeforeCredentialDelivery'},
      [pscustomobject]@{Package='./src/setup';Name='TestPublicToPrivateAndLoopbackRebindingFailsClosed'}
    )},
    [pscustomobject]@{Gate='PRIVATE_DNS_TEST';Tests=@(
      [pscustomobject]@{Package='./src/setup';Name='TestExplicitPrivateAndTailscaleDNSScopesAllowed'}
    )},
    [pscustomobject]@{Gate='RATE_LIMIT_TEST';Tests=@(
      [pscustomobject]@{Package='./src/setup';Name='TestSetupProbeRateLimitIsBoundedPerPeer'},
      [pscustomobject]@{Package='./src/setup';Name='TestSetupProbeRateLimitHasIndependentPeersAndExpires'}
    )}
  )
  $listCache = @{}
  foreach($group in $focused){
    $missing = [System.Collections.Generic.List[string]]::new()
    $discoveryFailure = $null
    foreach($test in $group.Tests){
      if(-not $listCache.ContainsKey($test.Package)){
        $listCache[$test.Package] = Get-GoTestList $test.Package
        $list = $listCache[$test.Package]
        Write-Output "FOCUSED_TEST_LIST package=$($test.Package) exit=$($list.ExitCode)"
        Write-Output "FOCUSED_TEST_EXECUTABLE=$($list.Executable)"
        Write-Output "FOCUSED_TEST_ARGUMENTS=$([string]::Join(' ',[string[]]$list.Arguments))"
        Write-Output "FOCUSED_TEST_WORKING_DIRECTORY=$($list.WorkingDirectory)"
        Write-Output 'FOCUSED_TEST_LIST_STDOUT_BEGIN'
        Write-Output $list.Stdout
        Write-Output 'FOCUSED_TEST_LIST_STDOUT_END'
        Write-Output 'FOCUSED_TEST_LIST_STDERR_BEGIN'
        Write-Output $list.Stderr
        Write-Output 'FOCUSED_TEST_LIST_STDERR_END'
      }
      $list = $listCache[$test.Package]
      if($list.ExitCode -ne 0){
        $kind = if($list.InvocationError -or (Test-GoInfrastructureFailure $list.Text)){'INFRA_FAILURE'}else{'FAIL'}
        $discoveryFailure = [pscustomobject]@{Kind=$kind;Detail="go test -list $($test.Package) exited $($list.ExitCode): $($list.Text.Trim())"}
        Write-TestDiscoveryDiagnostic $test.Name $test.Package $false $list.ExitCode
        continue
      }
      $listedNames = Get-GoTestNames $list.Stdout
      $listed = @($listedNames | Where-Object {$_ -ceq $test.Name}).Count -gt 0
      Write-TestDiscoveryDiagnostic $test.Name $test.Package $listed $list.ExitCode
      if(-not $listed){[void]$missing.Add($test.Name)}
    }
    if($discoveryFailure){$result[$group.Gate] = $discoveryFailure.Kind;Add-Failure "FOCUSED_$($group.Gate)" $discoveryFailure.Detail;continue}
    if($missing.Count -gt 0){$result[$group.Gate] = 'MISSING_TEST';Add-Failure "FOCUSED_$($group.Gate)" "missing test(s): $($missing -join ', ')";continue}
    $testFailed = $false
    $testInfraFailure = $false
    foreach($test in $group.Tests){
      $run = '^' + [regex]::Escape($test.Name) + '$'
      try{Run-GoExec @('go','test',$test.Package,'-run',$run,'-count=1') | Out-Null}
      catch{
        if($_.Exception.Message -like 'GO_INFRA_FAILURE:*'){$testInfraFailure = $true}else{$testFailed = $true}
        Add-Failure "FOCUSED_$($group.Gate)/$($test.Name)" $_.Exception.Message
      }
    }
    if($testFailed){$result[$group.Gate] = 'FAIL'}elseif($testInfraFailure){$result[$group.Gate] = 'INFRA_FAILURE'}else{$result[$group.Gate] = 'PASS'}
  }
  $version=Get-Content VERSION -Raw;$dockerfileVersion=Get-Content Dockerfile -Raw;$composeVersion=Get-Content docker-compose.yml -Raw;if($version.Trim() -eq '0.4.5' -and $dockerfileVersion -match 'tr -d' -and $dockerfileVersion -match '< VERSION' -and $composeVersion -match 'KITSUSYNC_APP_VERSION'){$result.VERSION_045='PASS'}else{Add-Failure 'VERSION_045' 'VERSION is not the authoritative release version source'}
  try{git diff --check;if($LASTEXITCODE){throw 'git diff --check failed'};$result.DIFF_CHECK='PASS'}catch{Add-Failure 'DIFF_CHECK' $_.Exception.Message}; $changedGo=git diff --name-only -- '*.go';$bad=if($changedGo){gofmt -l $changedGo}else{@()};if(-not $bad){$result.GOFMT='PASS'}else{Add-Failure 'GOFMT' ($bad -join ', ')}
  $unsafe = @()
  $secretKeywordPattern = '(?i)(password|secret|token|authorization|cookie|ciphertext|encryption|credential)'
  foreach($file in Get-ChildItem src -Recurse -Filter '*.go'){
    foreach($call in @(Get-LoggingCalls $file.FullName)){
      if($call.Text -notmatch $secretKeywordPattern){continue}
      $evidence = "$($file.FullName):$($call.StartLine)"
      Write-Output "SECRET_SCAN_REVIEW $evidence"
      if(Test-LoggingCallSecretValue $call.Text){$unsafe += "${evidence}: sensitive value argument"}
    }
  }
  if(-not $unsafe){$result.SECRET_SCAN='PASS'}else{Add-Failure 'SECRET_SCAN' ($unsafe -join ' | ')}
}catch{
  Add-Failure 'DOCKER_PRECHECK' $_.Exception.Message
  if(-not $focusedAttempted){
    foreach($gate in @('API_OVERRIDE_TEST','AUTH_REDIRECT_TEST','DNS_REBIND_TEST','PRIVATE_DNS_TEST','RATE_LIMIT_TEST')){
      if($result[$gate] -eq 'MISSING_TEST'){$result[$gate] = 'INFRA_FAILURE';Add-Failure "FOCUSED_$gate" 'focused discovery did not run because validation preflight failed'}
    }
  }
}
finally {
  if(Test-Path -LiteralPath $Docker){
    foreach($name in @($container,$goContainer)){if($name){Remove-Disposable @('rm','-f',$name) 'CONTAINER'}}
    Remove-Disposable @('compose','-p',$project,'-f','docker-compose.test.yml','down','-v','--remove-orphans') 'COMPOSE'
    Remove-Disposable @('image','rm',$image) 'IMAGE'
  }
  if(Test-Path $tempData){Remove-Item -LiteralPath $tempData -Recurse -Force -ErrorAction SilentlyContinue}
  $result.FINAL = Get-FinalValidationResult $result (-not $cleanupFailed); Write-Output 'KITSUSYNC_V045_RC_VALIDATION';foreach($k in $result.Keys){Write-Output "$k=$($result[$k])"};if($result.FINAL -eq 'FAIL'){Write-Output '';Write-Output 'FAILING_GATES';$failureDetails|ForEach-Object{Write-Output $_}}
}
