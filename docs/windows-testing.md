# Windows test strategy

Goal: dogfood the Windows amd64 build locally before handing it to an alpha
tester who works in VS Code on Windows 11 and occasionally uses WSL.

## What CI already proves on Windows

The `windows` job in `.github/workflows/ci.yml` runs on `windows-latest`
(Windows Server, headless) with the pinned Go toolchain:

- the full `go test ./...` suite against the fake API: uploads, exact-byte
  downloads with temp-sibling replacement, list/info/share/revoke/delete, device
  login and profile isolation under `%APPDATA%`-style directories, `exec` token
  passing and exit codes via a helper process (no shell dependency);
- a native `agentdrop.exe` smoke from PowerShell and cmd.exe: `--version`,
  `--help`, token validation message, `logout` with no profile.

CI cannot cover: browser launch (`rundll32`), SmartScreen, PATH/UX, VS Code's
integrated terminal, a real device login, locked-file replacement, Windows 11
consumer OOBE, or WSL interop. Those need the VM below.

## Windows 10 vs 11

The client's Windows-specific behavior is identical on both: `%APPDATA%`
credential location, `rundll32 url.dll,FileProtocolHandler` browser launch,
`MoveFileEx` replacement semantics, console detection, PowerShell 5.1 by default.
Go 1.27 supports Windows 10 and later. Test on **Windows 11**, since that is what
the tester runs. Windows 10 adds no code-path coverage; only test it if a tester
actually uses it. An unactivated evaluation install is sufficient for this
purpose (watermark and personalization limits only).

## VM on the Debian 12 host (KVM)

Host facts verified 2026-09-11: 32 CPUs with virtualization flags, 125 GB RAM,
`/dev/kvm` accessible to the user, QEMU 7.2 and OVMF present, no libvirt,
virt-manager, or swtpm installed, Wayland desktop session, 583 GB free on `/`.
No Windows ISO was found on the mounted disks.

Windows 11 setup requires UEFI, Secure Boot capable firmware, and TPM 2.0.
Provide all three instead of using registry bypasses:

```sh
sudo apt install libvirt-daemon-system virt-manager swtpm swtpm-tools ovmf
sudo usermod -aG libvirt "$USER"    # then log out/in or `newgrp libvirt`
```

Create the VM with virt-manager (GUI) or `virt-install`. Keep it simple for a
throwaway test box: SATA disk and e1000e network so Windows needs no virtio
drivers during setup. Suggested shape: 4 vCPU, 8 GB RAM, 64 GB qcow2, UEFI with
Secure Boot (`OVMF_CODE_4M.ms.fd`), emulated TPM 2.0 (swtpm), SPICE display.

```sh
virt-install --name win11-agentdrop --os-variant win11 \
  --vcpus 4 --memory 8192 --cpu host-passthrough \
  --disk size=64,bus=sata --network network=default,model=e1000e \
  --boot uefi,loader=/usr/share/OVMF/OVMF_CODE_4M.ms.fd,loader.secure=yes,nvram.template=/usr/share/OVMF/OVMF_VARS_4M.ms.fd \
  --tpm backend.type=emulator,backend.version=2.0,model=tpm-crb \
  --graphics spice --video qxl --cdrom /path/to/Win11.iso
```

Setup snags to expect: Windows 11 Home/Pro OOBE demands a Microsoft account
online; press Shift+F10 at the sign-in step and run `start ms-cxh:localonly`
(older builds: `OOBE\BYPASSNRO`) to create a local account. Press a key
immediately at boot or the UEFI shell may appear before the ISO boots.

After first login: install VS Code (user installer), take a libvirt snapshot
named `clean-vscode`. Revert to it for every fresh-install test round:

```sh
virsh snapshot-create-as win11-agentdrop clean-vscode
virsh snapshot-revert win11-agentdrop clean-vscode
```

Move builds into the VM by downloading the release asset from GitHub inside the
guest, or via a `virtiofs`/SPICE folder share. Do not copy credentials in.

## Manual checklist (record results in the journal)

Install and launch:

1. Download `agentdrop-<version>-windows-amd64.zip` and `checksums.txt` in the
   guest browser. `Get-FileHash` matches. Note the SmartScreen wording on first
   run of an unsigned exe.
2. Extract to `%LOCALAPPDATA%\Programs\AgentDrop`, add to the user PATH, open a
   new PowerShell: `agentdrop --version --json` shows `windows/amd64`.
3. Same from cmd.exe and from PowerShell 7 if installed.

Login and storage:

4. `agentdrop login --name "Win11 VM"` from PowerShell: the browser opens the
   `/activate` page, the code is shown on stderr, approval completes, and
   `whoami` works. Confirm the credential file exists under
   `%APPDATA%\agentdrop\` and nowhere else, and that the file's Properties →
   Security tab shows only the user, SYSTEM, and Administrators.
5. `agentdrop login --no-open --profile second` and approve from the host
   browser. `whoami --profile second` works; `logout --profile second` removes
   only that file.

File operations:

6. `agentdrop put .\report.md`, `info`, `get ID -o .\copy.md`, `Get-FileHash`
   match. Repeat `get` onto an existing file: it is replaced.
7. Open `copy.md` in Notepad, run `get` again onto it: expect the
   "Could not replace the destination" error and an unchanged file, no
   `.agentdrop-*` leftovers. Also test with the file open in VS Code (VS Code
   does not lock; the replace should succeed and the editor should reload).
8. `agentdrop .\report.md` on a terminal opens the reader link in the default
   browser. `--no-open` and `--json` do not. Names with spaces and Unicode
   (`.\"my nötes.md"`) upload with the correct name.
9. Stdin paths: `Get-Content .\report.md | agentdrop` in PowerShell 5.1 and 7,
   and `type report.md | agentdrop` in cmd. Compare the uploaded bytes with the
   original; PowerShell 5.1 re-encodes piped text (`$OutputEncoding` is ASCII
   by default), so document the result and prefer file arguments in guidance.
10. `share`, `revoke --yes`, `delete --yes`, and a `--json` error for a missing
    ID. Ctrl-C during `login` exits promptly with a non-zero code.

VS Code and agents:

11. In VS Code's integrated terminal (PowerShell), run the same login and
    `put`/`open` flow. Confirm the browser launches from inside VS Code.
12. Install the tester's agent extension (Claude Code or Codex in VS Code), add
    the AgentDrop skill, and ask the agent to hand off a Markdown file with
    `agentdrop open response.md --no-open`. This is the real test case.
13. `agentdrop exec -- powershell -Command "echo $env:AGENTDROP_API_TOKEN"`
    shows a token in the child only; `exec -- cmd /c exit 7` returns 7.

WSL:

14. Install WSL (Ubuntu), use the **linux-amd64** archive inside it. `login`
    prints the URL (no browser launcher); complete approval from Windows.
    `put`/`get` work. Note whether `xdg-open` is missing so we can add a
    `wslview`/`cmd.exe /c start` fallback for WSL browser launch.

Anything that fails becomes a test in the fake-API suite where possible, then a
fix, then a new alpha tag.
