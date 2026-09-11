# One-shot VM base preparation. Run elevated.
$ErrorActionPreference = 'Continue'
$ProgressPreference = 'SilentlyContinue'
$host_ = 'http://192.168.122.1:8000'
function Ping($tag) { try { Invoke-WebRequest "$host_/signal-$tag" -UseBasicParsing -TimeoutSec 5 | Out-Null } catch {} }
try { [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12 } catch {}

Write-Host '== 1/3 Disable automatic Windows Update (policy)'
New-Item -Path 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\WindowsUpdate\AU' -Force | Out-Null
Set-ItemProperty -Path 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\WindowsUpdate\AU' -Name NoAutoUpdate -Type DWord -Value 1
Write-Host ('NoAutoUpdate=' + (Get-ItemProperty 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\WindowsUpdate\AU').NoAutoUpdate)

Write-Host '== 2/3 VS Code (user installer, silent)'
$out = Join-Path $env:TEMP 'VSCodeUserSetup.exe'
Invoke-WebRequest 'https://update.code.visualstudio.com/latest/win32-x64-user/stable' -OutFile $out -UseBasicParsing
Start-Process -Wait -FilePath $out -ArgumentList '/VERYSILENT', '/NORESTART', '/MERGETASKS=!runcode,addcontextmenufiles,addcontextmenufolders,addtopath'
$code = Join-Path $env:LOCALAPPDATA 'Programs\Microsoft VS Code\Code.exe'
if (Test-Path $code) { Write-Host ('VS Code ' + (Get-Item $code).VersionInfo.ProductVersion + ' installed'); Ping 'vscode-ok' } else { Write-Host 'VS Code NOT installed'; Ping 'vscode-fail' }

Write-Host '== 3/3 virtio-win guest tools (SPICE agent, drivers)'
$vol = Get-Volume | Where-Object { $_.FileSystemLabel -like 'virtio-win*' } | Select-Object -First 1
if ($vol) {
    $tools = "$($vol.DriveLetter):\virtio-win-guest-tools.exe"
    if (Test-Path $tools) {
        Start-Process -Wait -FilePath $tools -ArgumentList '/passive', '/norestart'
        Write-Host "guest tools exit code $LASTEXITCODE"; Ping 'guesttools-ok'
    } else { Write-Host "no $tools"; Ping 'guesttools-missing' }
} else { Write-Host 'virtio-win volume not found'; Ping 'guesttools-novolume' }

Write-Host '== done'
Ping 'bootstrap-done'
