# Round 2: authenticated file operations against production with disposable records.
$ErrorActionPreference = 'Continue'
$ProgressPreference = 'SilentlyContinue'
$PSNativeCommandUseErrorActionPreference = $false
$srv = 'http://192.168.122.1:8000'
$run = 'r2d'
$results = @()
function Ping($tag) { try { Invoke-WebRequest "$srv/signal-$tag" -UseBasicParsing -TimeoutSec 5 | Out-Null } catch {} }
function Check([string]$Name, [bool]$Ok, [string]$Note = '') {
    $script:results += [pscustomobject]@{ check = $Name; ok = $Ok; note = $Note }
    Write-Host ("{0,-4} {1} {2}" -f ($(if ($Ok) { 'ok' } else { 'FAIL' })), $Name, $Note)
    Ping ("$run-" + $Name + "-" + $(if ($Ok) { 'ok' } else { 'fail' }))
}
# Plain function (advanced functions reject --flags); named AdRun because "cli" is a built-in alias for Clear-Item.
function AdRun { $o = & agentdrop $args 2>&1 | Out-String; return @{ code = $LASTEXITCODE; out = $o.Trim() } }
# For --json error checks: cmd merges stderr as plain text (PowerShell 5.1 wraps native stderr in ErrorRecords).
function AdRunCmd([string]$ArgLine) { $o = cmd /c "agentdrop $ArgLine 2>&1" | Out-String; return @{ code = $LASTEXITCODE; out = $o.Trim() } }
function Hash($p) { (Get-FileHash -Algorithm SHA256 $p).Hash.ToLower() }

$work = Join-Path $env:USERPROFILE 'agentdrop-test'
New-Item -ItemType Directory -Force -Path $work | Out-Null
Set-Location $work
$ids = @()

# whoami
$r = AdRun whoami --json
$who = $null; try { $who = $r.out | ConvertFrom-Json } catch {}
Check 'whoami' ($r.code -eq 0 -and $who.account.id) "vault $($who.account.id)"

# put a UTF-8 markdown file with non-ASCII content and CRLF
$content = "# Windows test`r`n`r`nnötes with ünïcode and emoji 🚀`r`n"
[IO.File]::WriteAllBytes("$work\report.md", [Text.Encoding]::UTF8.GetBytes($content))
$r = AdRun put .\report.md
$id = $r.out; $ids += $id
Check 'put' ($r.code -eq 0 -and $id -match '^[A-Za-z0-9]+$') "id=$id"
$r = AdRun info $id --json
$info = $null; try { $info = ($r.out | ConvertFrom-Json).file } catch {}
Check 'info' ($r.code -eq 0 -and $info.name -eq 'report.md' -and $info.sizeBytes -eq (Get-Item .\report.md).Length -and $info.contentType -eq 'text/markdown') "size=$($info.sizeBytes) type=$($info.contentType)"

# get to new file, hash match
$r = AdRun get $id -o .\copy.md
Check 'get-new' ($r.code -eq 0 -and (Hash .\report.md) -eq (Hash .\copy.md)) 'sha256 match'
# get onto existing file: replaced
Set-Content .\copy.md 'stale'
$r = AdRun get $id -o .\copy.md
Check 'get-replace' ($r.code -eq 0 -and (Hash .\report.md) -eq (Hash .\copy.md))
# get onto a file locked by another handle: refused, unchanged, no temp leftovers
$before = Hash .\copy.md
$fs = [IO.File]::Open("$work\copy.md", 'Open', 'Read', 'None')
$r = AdRun get $id -o .\copy.md
$fs.Close()
$leftover = @(Get-ChildItem $work -Force -Filter '.agentdrop-*')
Check 'get-locked' ($r.code -eq 1 -and $r.out -match 'Could not replace' -and (Hash .\copy.md) -eq $before -and $leftover.Count -eq 0) $r.out
# stdout download in a pipe, byte-exact
& agentdrop get $id -o - | Out-Null   # PowerShell would re-encode; use cmd for raw bytes
cmd /c "agentdrop get $id -o - > raw.bin"
Check 'get-stdout-cmd' ((Hash .\raw.bin) -eq (Hash .\report.md))

# unicode + space filename
Copy-Item .\report.md ".\my nötes.md"
$r = AdRun put ".\my nötes.md"
$id2 = $r.out; $ids += $id2
$r2 = AdRun info $id2 --json
$n = $null; try { $n = ($r2.out | ConvertFrom-Json).file.name } catch {}
Check 'put-unicode-name' ($r.code -eq 0 -and $n -eq 'my nötes.md') "name=[$n]"

# stdin pipelines
$r = AdRun list --json
Check 'list-json' ($r.code -eq 0 -and ($r.out | ConvertFrom-Json).files.Count -ge 2)
$p = & { Get-Content .\report.md -Raw | agentdrop put } 2>&1 | Out-String
$id3 = $p.Trim(); $ids += $id3
cmd /c "agentdrop get $id3 -o - > ps51pipe.bin"
$ps51same = (Hash .\ps51pipe.bin) -eq (Hash .\report.md)
Check 'stdin-ps51-info' $true "byte-exact=$ps51same ($((Get-Item .\ps51pipe.bin).Length) vs $((Get-Item .\report.md).Length) bytes); PS 5.1 pipes re-encode text, prefer file arguments"
$id4 = (cmd /c "type report.md | agentdrop put").Trim(); $ids += $id4
cmd /c "agentdrop get $id4 -o - > cmdpipe.bin"
Check 'stdin-cmd' ((Hash .\cmdpipe.bin) -eq (Hash .\report.md)) "bytes $((Get-Item .\cmdpipe.bin).Length)"

# share / revoke
$r = AdRun share $id --expires 5m --json
$sh = $null; try { $sh = $r.out | ConvertFrom-Json } catch {}
Check 'share' ($r.code -eq 0 -and $sh.viewUrl -like 'https://agentdrop.lol/s/*' -and $sh.id) "share=$($sh.id)"
$r = AdRun revoke $sh.id --yes
Check 'revoke' ($r.code -eq 0 -and $r.out -match 'Share revoked')
$r = AdRunCmd "revoke $($sh.id) --yes --json"
$e = $null; try { $e = ($r.out | ConvertFrom-Json).error } catch {}
Check 'revoke-twice-json-error' ($r.code -eq 1 -and $e.code -eq 'api' -and $e.httpStatus) "apiCode=$($e.apiCode)"

# exec: token only in the child, exit code propagated
$len = (& agentdrop exec -- powershell -NoProfile -Command '$env:AGENTDROP_API_TOKEN.Length' 2>&1 | Out-String).Trim()
Check 'exec-token-length' ([int]$len -gt 20) "token length $len (never printed)"
& agentdrop exec -- cmd /c exit 7 | Out-Null
Check 'exec-exit-code' ($LASTEXITCODE -eq 7) "exit=$LASTEXITCODE"
$hostTok = [string]$env:AGENTDROP_API_TOKEN
Check 'exec-no-leak-to-parent' ($hostTok -eq '')

# credential storage location and ACL
$credDir = Join-Path $env:APPDATA 'agentdrop'
$files = @(Get-ChildItem $credDir -Filter '*.json' -ErrorAction SilentlyContinue)
$acl = if ($files) { (Get-Acl $files[0].FullName).Access | ForEach-Object { "$($_.IdentityReference)=$($_.FileSystemRights)" } } else { @() }
$broad = $acl | Where-Object { $_ -match 'Everyone|BUILTIN\\Users|Authenticated Users' }
Check 'cred-location' ($files.Count -eq 1 -and $files[0].Name -match '^[0-9a-f]{24}\.json$') "$credDir\$($files[0].Name)"
Check 'cred-acl-no-broad' ($acl.Count -gt 0 -and -not $broad) ($acl -join '; ')
$legacy = Test-Path (Join-Path $env:USERPROFILE '.config\agentdrop')
Check 'cred-not-in-userprofile-config' (-not $legacy)

# cleanup all disposable files
foreach ($x in $ids) { if ($x) { & agentdrop delete $x --yes 2>&1 | Out-Null } }
$r = AdRunCmd "info $id --json"
$e = $null; try { $e = ($r.out | ConvertFrom-Json).error } catch {}
Check 'delete-then-404' ($r.code -eq 1 -and $e.httpStatus -eq 404) "apiCode=$($e.apiCode)"

Write-Host ''
Write-Host ("SUMMARY: {0} ok, {1} fail" -f @($results | Where-Object ok).Count, @($results | Where-Object { -not $_.ok }).Count)
$results | Where-Object { -not $_.ok } | Format-Table -AutoSize | Out-String | Write-Host
Ping "$run-done"
