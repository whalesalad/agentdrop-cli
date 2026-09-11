<#
.SYNOPSIS
  Install or upgrade the AgentDrop CLI for the current user (alpha).
.DESCRIPTION
  Downloads agentdrop-<version>-windows-<arch>.zip and checksums.txt, verifies
  the SHA-256, installs agentdrop.exe into a per-user directory, and optionally
  adds that directory to the user PATH. No elevation required. Credentials are
  never touched. Works in Windows PowerShell 5.1 and PowerShell 7.
.NOTES
  Windows client editions default to the Restricted execution policy. Either
  pipe it:  irm https://raw.githubusercontent.com/whalesalad/agentdrop-cli/main/scripts/install.ps1 | iex
  or run a downloaded copy with:  powershell -ExecutionPolicy Bypass -File .\install.ps1
  Omit -Version to install the latest release.
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File .\install.ps1
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File .\install.ps1 -Version v0.3.0-alpha.1
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File .\install.ps1 -Version v0.3.0-alpha.1 -BaseUrl http://192.168.122.1:8000
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File .\install.ps1 -Uninstall
#>
[CmdletBinding()]
param(
    [string]$Version = "",
    [string]$BaseUrl = "https://github.com/whalesalad/agentdrop-cli/releases/download",
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA "Programs\AgentDrop"),
    [switch]$NoPath,
    [switch]$Uninstall
)
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false

function Get-Arch {
    try {
        $a = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
        if ($a -eq 'Arm64') { return 'arm64' }
        if ($a -eq 'X64') { return 'amd64' }
    } catch {}
    $p = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    switch ($p) { 'ARM64' { return 'arm64' } 'AMD64' { return 'amd64' } }
    throw "Unsupported Windows architecture '$p'. Supported: amd64, arm64."
}

function Update-UserPath([string]$Dir, [switch]$Remove) {
    $current = [Environment]::GetEnvironmentVariable('Path', 'User')
    $parts = @()
    if ($current) { $parts = $current -split ';' | Where-Object { $_ -ne '' } }
    $has = $parts | Where-Object { $_.TrimEnd('\') -ieq $Dir.TrimEnd('\') }
    if ($Remove) {
        if ($has) {
            $parts = $parts | Where-Object { $_.TrimEnd('\') -ine $Dir.TrimEnd('\') }
            [Environment]::SetEnvironmentVariable('Path', ($parts -join ';'), 'User')
            Write-Host "Removed $Dir from your user PATH."
        }
        return
    }
    if (-not $has) {
        [Environment]::SetEnvironmentVariable('Path', (($parts + $Dir) -join ';'), 'User')
        Write-Host "Added $Dir to your user PATH. Open a new terminal to use 'agentdrop'."
    }
    if (($env:Path -split ';') -notcontains $Dir) { $env:Path = "$env:Path;$Dir" }
}

$exePath = Join-Path $InstallDir 'agentdrop.exe'

if ($Uninstall) {
    if (Test-Path $exePath) { Remove-Item -Force $exePath; Write-Host "Removed $exePath" }
    Get-ChildItem -Path $InstallDir -Filter 'agentdrop*.old' -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue
    if ((Test-Path $InstallDir) -and -not (Get-ChildItem $InstallDir -Force)) { Remove-Item $InstallDir }
    Update-UserPath $InstallDir -Remove
    Write-Host "Saved credentials were left in place. To forget them: run 'agentdrop logout' before uninstalling, or delete $env:APPDATA\agentdrop. Revoke the token in your vault's API tokens page."
    exit 0
}

try { [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12 } catch {}
$ProgressPreference = 'SilentlyContinue'
$base = $BaseUrl.TrimEnd('/')
$isGitHub = $base -match 'github\.com'

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("agentdrop-install-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    if (-not $Version) {
        # Latest release: manifest.json has a stable name, so no API call or token is needed.
        $manifestUrl = if ($isGitHub) { "https://github.com/whalesalad/agentdrop-cli/releases/latest/download/manifest.json" } else { "$base/manifest.json" }
        Invoke-WebRequest -Uri $manifestUrl -OutFile (Join-Path $tmp 'manifest.json') -UseBasicParsing
        $Version = (Get-Content (Join-Path $tmp 'manifest.json') -Raw | ConvertFrom-Json).version
        if (-not $Version) { throw "manifest.json did not contain a version" }
    }
    $ver = $Version.TrimStart('v')
    $arch = Get-Arch
    $asset = "agentdrop-$ver-windows-$arch.zip"
    if ($isGitHub) { $base = "$base/v$ver" }

    Write-Host "Downloading $asset from $base"
    Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') -UseBasicParsing
    Invoke-WebRequest -Uri "$base/$asset" -OutFile (Join-Path $tmp $asset) -UseBasicParsing

    $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match "\s\*?$([regex]::Escape($asset))$" } | Select-Object -First 1
    if (-not $line) { throw "checksums.txt does not list $asset" }
    $expected = ($line -split '\s+')[0].ToLower()
    $actual = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $asset)).Hash.ToLower()
    if ($expected -ne $actual) { throw "SHA-256 mismatch for $asset. Expected $expected, got $actual. Nothing was installed." }
    Write-Host "Checksum verified."

    Expand-Archive -Path (Join-Path $tmp $asset) -DestinationPath (Join-Path $tmp 'x') -Force
    $exe = Get-ChildItem -Path (Join-Path $tmp 'x') -Recurse -Filter 'agentdrop.exe' | Select-Object -First 1
    if (-not $exe) { throw "agentdrop.exe not found inside $asset" }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    if (Test-Path $exePath) {
        # A running executable cannot be overwritten but can be renamed.
        $old = Join-Path $InstallDir ("agentdrop-" + [Guid]::NewGuid().ToString('N') + ".old")
        Move-Item -Force $exePath $old
        Remove-Item -Force $old -ErrorAction SilentlyContinue
    }
    Copy-Item $exe.FullName $exePath
    Get-ChildItem -Path $InstallDir -Filter 'agentdrop*.old' -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue

    $installed = & $exePath --version
    if ($LASTEXITCODE -ne 0 -or -not $installed) { throw "Installed executable did not report a version." }
    Write-Host "Installed agentdrop $installed to $exePath"

    if (-not $NoPath) { Update-UserPath $InstallDir }

    $others = Get-Command agentdrop -All -ErrorAction SilentlyContinue | Where-Object { $_.Source -and ($_.Source -ine $exePath) }
    foreach ($o in $others) { Write-Warning "Another 'agentdrop' is also on PATH: $($o.Source). Remove it or reorder PATH if the wrong one runs." }
    Write-Host "Next: agentdrop login"
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
