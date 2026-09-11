# Round 1: install the CLI with the alpha installer, run the offline smoke, report.
$ErrorActionPreference = 'Continue'
$ProgressPreference = 'SilentlyContinue'
$srv = 'http://192.168.122.1:8000'
function Ping($tag) { try { Invoke-WebRequest "$srv/signal-$tag" -UseBasicParsing -TimeoutSec 5 | Out-Null } catch {} }
$work = Join-Path $env:USERPROFILE 'agentdrop-test'
New-Item -ItemType Directory -Force -Path $work | Out-Null
Set-Location $work
Invoke-WebRequest "$srv/install.ps1" -OutFile install.ps1 -UseBasicParsing
Invoke-WebRequest "$srv/smoke-windows.ps1" -OutFile smoke-windows.ps1 -UseBasicParsing

Write-Host '== install (fresh)'
powershell -ExecutionPolicy Bypass -File .\install.ps1 -Version v0.3.0-alpha.1 -BaseUrl $srv
$installRc = $LASTEXITCODE
Write-Host "install exit=$installRc"
Write-Host '== install again (upgrade in place over existing exe)'
powershell -ExecutionPolicy Bypass -File .\install.ps1 -Version v0.3.0-alpha.1 -BaseUrl $srv
Write-Host "reinstall exit=$LASTEXITCODE"

$exe = Join-Path $env:LOCALAPPDATA 'Programs\AgentDrop\agentdrop.exe'
Write-Host "== on PATH in a NEW process?"
$onPath = powershell -NoProfile -Command "(Get-Command agentdrop -ErrorAction SilentlyContinue).Source"
Write-Host "new-process resolves agentdrop -> [$onPath]"
Write-Host '== version'
& $exe --version --json
Write-Host '== offline smoke'
powershell -ExecutionPolicy Bypass -File .\smoke-windows.ps1 $exe
Write-Host "smoke exit=$LASTEXITCODE"
if ($installRc -eq 0 -and $LASTEXITCODE -eq 0 -and $onPath) { Ping 'round1-ok' } else { Ping 'round1-fail' }
Write-Host '== round1 done'
