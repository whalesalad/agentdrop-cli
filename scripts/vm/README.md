# VM test tooling

Host-side and guest-side helpers used to dogfood the Windows build in the
`win11-agentdrop` KVM guest described in `docs/windows-testing.md`.

- `vm.py` — drive the guest headlessly from the host: `shot NAME` (PNG
  screenshot to `$VM_SHOTS`, default `~/vm-shots`), `click X Y`, `dbl X Y`,
  `key KEY_NAME...` (virsh send-key names), `type "text"`, `geo`, `state`.
  Mouse uses QMP `input-send-event` absolute tablet events; coordinates are
  guest pixels from the most recent screenshot, and the guest resolution is
  re-read before each click, so viewer zoom or resize does not matter. Keep
  typed lines under ~150 characters and split long commands: long bursts drop
  keystrokes in the guest. Requires `virsh` access to `qemu:///system` and
  ImageMagick's `convert` on the host.
- `guest-bootstrap.ps1` — run **elevated** once on a fresh install: disables
  automatic Windows Update by policy, installs VS Code silently, installs the
  virtio-win guest tools from the attached CD. Pings the host file server on
  completion (`/signal-*` URLs show up in the server log).
- `guest-round1-install.ps1` — installs the CLI with `scripts/install.ps1`
  from the host file server, upgrades in place, prints version, runs
  `scripts/smoke-windows.ps1`.
- `guest-round2-files.ps1` — authenticated file-operation checklist against
  production with disposable records; every check pings the host server as
  `signal-<run>-<check>-ok|fail`, and all created files are deleted at the end.

Serve the guest scripts and release assets to the VM with
`python3 -m http.server 8000 --bind 192.168.122.1 --directory <dir>`; the
guest reaches the host at `http://192.168.122.1:8000`. PowerShell scripts must
be saved as UTF-8 **with BOM** or Windows PowerShell 5.1 reads them as ANSI.
Do not name helper functions `cli`: it is a built-in alias for `Clear-Item`.
Advanced functions (`[Parameter()]` attributes) reject `--flags` and `-o` as
unknown parameters; use plain `$args` functions to wrap the CLI. PowerShell
5.1 wraps native stderr in ErrorRecords under `2>&1`, so capture `--json`
error output through `cmd /c "... 2>&1"` when you need to parse it. The full
session recipe lives in `docs/windows-testing.md`.
