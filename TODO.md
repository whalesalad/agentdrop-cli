# AgentDrop CLI TODO

Prioritized queue. Keep entries actionable; move finished items to Done with the
date and evidence pointer.

## Now

- [ ] Cut `v0.3.0-alpha.1` from main (tag → release workflow) and hand
      `linux-amd64` / `windows-amd64` archives to alpha testers. Collect first
      dogfood feedback as test cases.
- [ ] Windows 11 VM dogfood per [docs/windows-testing.md](docs/windows-testing.md).
      VM `win11-agentdrop` exists on the owner's host with Windows 11 Enterprise
      Evaluation 24H2 setup started 2026-09-11. Finish setup, install VS Code,
      snapshot `clean-vscode`, then run the 14-step checklist against
      `v0.3.0-alpha.1` and record results in the journal.
- [ ] WSL browser-launch fallback: detect WSL (`WSL_DISTRO_NAME` or
      `/proc/version`) and try `wslview` or `cmd.exe /c start` before `xdg-open`.
- [ ] Windows DACL enforcement for the credential directory/file
      (`internal/auth/store_windows.go` is currently a placeholder that relies on
      `%APPDATA%` default ACL inheritance). Use `golang.org/x/sys/windows`.

## Next

- [ ] macOS arm64/amd64 native smoke (login browser launch via `open`,
      Gatekeeper prompt for the unsigned binary, existing Node profile reuse).
- [ ] Linux arm64 smoke and an Alpine (musl) container run of the same binary.
- [ ] Installers: alpha `scripts/install.ps1` exists (pinned version, SHA-256
      check against `checksums.txt`, per-user dir, user PATH, `-Uninstall`);
      needs the VM run, then CI coverage on the Windows job, `install.sh`, and
      "latest" resolution once releases are public.
- [ ] Release hardening: pin GitHub Actions by SHA, `govulncheck` in CI, cosign
      checksum signing, SBOM/provenance. Then macOS notarization and Windows
      Authenticode for the stable channel.
- [ ] Application repo cutover: link native downloads from the site CLI guide
      and README; mark `cli/` legacy once parity is verified on all six targets.
- [ ] Decide public visibility for this repository (currently private; MIT
      license already committed). Friends need either public release assets or
      directly shared archives.

## Later

- [ ] Second Windows VM from a current consumer ISO fetched in the owner's
      browser, and optionally an LTSC 2024 eval VM; see the lessons section in
      `docs/windows-testing.md`. Once a year cadence; not urgent.

- [ ] Homebrew tap, Scoop bucket, winget, `.deb`/`.rpm`.
- [ ] Expansion targets (linux/arm, 386, riscv64, FreeBSD/OpenBSD) as
      experimental build-only artifacts.
- [ ] OS keychain storage as an isolated optional backend.
- [ ] Spooling stdin to a protected temp file instead of memory, if 50 MiB
      in-memory buffering proves a problem on small machines.

## Done

- [x] 2026-09-11 — Repository created, Go implementation at parity with Node
      CLI 0.2.0 (all commands, flags, JSON shapes, credential layout), fake-API
      test suite, six-target cross-build script, CI and tag-release workflows.
      See [journal](docs/journal/2026-09-11.md).
