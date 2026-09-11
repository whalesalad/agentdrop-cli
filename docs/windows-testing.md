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
`/dev/kvm` accessible to the user, QEMU 7.2 and OVMF present, KDE Plasma on
Wayland. Installed for this work: `libvirt-daemon-system virt-manager swtpm
swtpm-tools ovmf`. libvirt on Debian 12 is socket-activated, so `libvirtd`
showing `inactive` is normal.

ISO: Microsoft's consumer download page rejects scripted requests
("Sentinel marked this request as rejected"), so the VM uses the official
**Windows 11 Enterprise Evaluation** ISO (24H2, build 26100.1742, 5,387,960,320
bytes) from the Evaluation Center link `https://go.microsoft.com/fwlink/?linkid=2289031`,
saved as `~/Downloads/Win11_24H2_EnterpriseEval_x64_en-us.iso`. It runs
unactivated for 90 days by design and its setup permits a local account without
the consumer OOBE workaround. CLI code paths are identical to Home/Pro.
`~/Downloads/virtio-win.iso` (stable virtio-win) is attached as a second CD for
optional guest tools; setup itself uses SATA and e1000e and needs no drivers.

One-time host setup (already done on this machine):

```sh
virsh -c qemu:///system net-start default && virsh -c qemu:///system net-autostart default
virsh -c qemu:///system pool-define-as default dir --target /var/lib/libvirt/images
virsh -c qemu:///system pool-start default && virsh -c qemu:///system pool-autostart default
```

The VM as created (Debian 12's osinfo-db predates Windows 11, so `win10`
supplies the defaults):

```sh
virt-install --connect qemu:///system --name win11-agentdrop --osinfo win10 \
  --vcpus 4 --memory 8192 --cpu host-passthrough --machine q35 \
  --disk pool=default,size=64,format=qcow2,bus=sata \
  --cdrom ~/Downloads/Win11_24H2_EnterpriseEval_x64_en-us.iso \
  --disk ~/Downloads/virtio-win.iso,device=cdrom,bus=sata,readonly=on \
  --network network=default,model=e1000e \
  --boot loader=/usr/share/OVMF/OVMF_CODE_4M.ms.fd,loader.readonly=yes,loader.type=pflash,loader.secure=yes,nvram.template=/usr/share/OVMF/OVMF_VARS_4M.ms.fd \
  --features smm.state=on \
  --tpm backend.type=emulator,backend.version=2.0,model=tpm-crb \
  --graphics spice,listen=none --video qxl --channel spicevmc --input tablet \
  --controller usb,model=qemu-xhci --sound none --noautoconsole
```

Gotchas hit on first boot:

- The ISO's "Press any key to boot from CD or DVD" prompt expires in a few
  seconds; unattended, firmware reports "No bootable option" and the domain can
  end up shut off. Recover with `virsh start win11-agentdrop` followed
  immediately by a burst of `virsh send-key win11-agentdrop KEY_SPACE`.
- With `listen=none` the console must attach through libvirt:
  `virt-viewer --connect qemu:///system --attach --wait --reconnect win11-agentdrop`
  (or open it in virt-manager). Plain `virt-viewer` shows "can only be done
  with --attach".
- `virsh screenshot win11-agentdrop shot.ppm` gives a headless look at the
  console; convert with ImageMagick for viewing.

Setup notes: Enterprise Evaluation asks for no product key. Create a local
account (choose the domain-join/offline option if OOBE pushes a Microsoft
account). After first login: install VS Code (user installer), then snapshot:

```sh
virsh -c qemu:///system snapshot-create-as win11-agentdrop clean-vscode
virsh -c qemu:///system snapshot-revert win11-agentdrop clean-vscode
```

Move builds into the VM by downloading the release asset from GitHub inside the
guest. Do not copy credentials in.

## Lessons from the first VM build (2026-09-11), for the next one

- **Use a current ISO.** The Evaluation Center image is cut once per feature
  release (the 24H2 eval is build 26100.1742 from September 2024) and every
  fresh install then spends 20+ minutes downloading and, mostly, CPU-expanding a
  year of cumulative updates inside the VM. Microsoft's consumer download page
  re-cuts its ISO every few months with the current patch level, but it rejects
  scripted requests. Next time the owner fetches the ISO in their own browser
  (once a year, not worth automating) and we build from that. Owner decision.
- **Try LTSC for a second, minimal box** (Enterprise LTSC 2024 eval exists on
  the Evaluation Center, 5.1 GB, same 2024 base). No Store, Copilot, Widgets or
  winget. Keep the stock consumer-style VM as the "what the tester has" machine;
  do not debloat it, since SmartScreen/Defender/stock PowerShell behavior is the
  point of the test.
- **OOBE update stage looks like a slow download but is CPU-bound.** Guest NIC
  receive was zero while "Downloading 50%" showed and qemu ran ~5 cores; the
  network path (vnet on virbr0, e1000e) was fine. Give a fresh-install VM more
  vCPUs for the first boot, or install from a current ISO.
- **OOBE path that reached a local account on Enterprise:** Sign-in options →
  Domain join instead → name → empty password (skips security questions) →
  privacy Next/Accept. The consumer editions need `start ms-cxh:localonly`.
- **Pause or disable Windows Update before the base snapshot**, otherwise every
  snapshot revert is followed by update churn and a reboot prompt mid-test.
- **Driving the VM headlessly works well**: `virsh send-key` for keyboard and
  QMP `input-send-event` absolute tablet events for the mouse, with
  `virsh screenshot` as eyes. Helper lives in the session scratchpad; promote
  it to `scripts/vm/` if it keeps earning its keep.

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
