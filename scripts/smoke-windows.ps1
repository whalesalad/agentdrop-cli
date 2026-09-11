# Offline smoke for agentdrop.exe on Windows. Works in PowerShell 5.1 and 7.
# Usage: .\scripts\smoke-windows.ps1 [path\to\agentdrop.exe]
param([string]$Exe = ".\agentdrop.exe")
$ErrorActionPreference = 'Continue'
$PSNativeCommandUseErrorActionPreference = $false
$Exe = (Resolve-Path $Exe).Path

function Invoke-Cli([string[]]$CliArgs) {
    $out = & $Exe @CliArgs 2>&1 | Out-String
    return @{ code = $LASTEXITCODE; out = $out }
}
function Assert-True($Condition, [string]$Message, $Result) {
    if (-not $Condition) {
        Write-Host "FAIL: $Message"
        if ($Result) { Write-Host "exit=$($Result.code)"; Write-Host $Result.out }
        exit 1
    }
    Write-Host "ok: $Message"
}

$v = (& $Exe --version --json) | ConvertFrom-Json
Assert-True ($v.os -eq 'windows' -and $v.arch) "version reports windows/$($v.arch) $($v.version)"

$r = Invoke-Cli @('--help')
Assert-True ($r.code -eq 0 -and $r.out -match 'agentdrop put FILE') 'help text' $r

$env:AGENTDROP_API_TOKEN = 'ad-not-a-token'
$r = Invoke-Cli @('whoami')
Assert-True ($r.code -eq 1 -and $r.out -match 'must contain an API token') 'vault key rejected as token' $r

$env:AGENTDROP_API_TOKEN = ''
$env:APPDATA = Join-Path $PWD 'appdata-smoke'
$r = Invoke-Cli @('logout')
Assert-True ($r.code -eq 0 -and $r.out -match 'Removed this profile') 'logout without a profile' $r

$r = Invoke-Cli @('info', 'x', '--json')
Assert-True ($r.code -eq 1 -and $r.out -match '"code":"auth"') 'json auth error without credential' $r

$r = Invoke-Cli @('delete', 'x')
Assert-True ($r.code -eq 1 -and $r.out -match 'with --yes') 'destructive command requires --yes' $r

Write-Host 'exe smoke ok'
exit 0
