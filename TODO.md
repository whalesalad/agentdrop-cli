# AgentDrop CLI TODO

Prioritized queue. Keep entries actionable; move finished items to Done with the
date and evidence pointer.

## Now

- [x] 2026-09-11 — History purged of the copied application spec (owner ran
      the rewrite), repository made **public** (issues on, wiki off), anonymous
      downloads verified, `v0.3.0-alpha.2` released as latest (first build with
      `agentdrop update`). Residual: old commits remain fetchable by SHA on
      GitHub until it garbage-collects; owner accepted (design doc only, no
      secrets).
- [ ] Hand alpha testers the one-liners from https://agentdrop.lol/docs/cli and
      collect first feedback as test cases. Next fixes ship as `v0.3.0-alpha.N`;
      testers run `agentdrop update`.
- [x] 2026-09-11 — Site CLI guide rewritten for native installs, update and
      Windows guidance; live on agentdrop.lol/docs/cli (application main build).
- [ ] Windows 11 VM dogfood, remaining items (22/22 CLI checks passed
      2026-09-11, see results in `docs/windows-testing.md`): PowerShell 7 in the
      VM, Ctrl-C during login, WSL with the Linux binary, and the tester's agent
      extension in VS Code (needs their accounts).
- [x] 2026-09-11 — README Windows notes: execution-policy-safe install command,
      file arguments over PS 5.1 pipes, OEM-code-page caveat for `--json`.
- [ ] WSL browser-launch fallback: detect WSL (`WSL_DISTRO_NAME` or
      `/proc/version`) and try `wslview` or `cmd.exe /c start` before `xdg-open`.
- [ ] Windows DACL enforcement for the credential directory/file
      (`internal/auth/store_windows.go` is currently a placeholder that relies on
      `%APPDATA%` default ACL inheritance). Use `golang.org/x/sys/windows`.

## Next

- [ ] macOS arm64/amd64 native smoke (login browser launch via `open`,
      Gatekeeper prompt for the unsigned binary, existing Node profile reuse).
- [ ] Linux arm64 smoke and an Alpine (musl) container run of the same binary.
- [x] 2026-09-11 — Installers: `install.sh` (POSIX; latest via manifest,
      pin, checksum fail-closed, uninstall, PATH advice, Rosetta detection) and
      `install.ps1` (latest via manifest, upgrade in place, `-Uninstall`), both
      tested in CI on Linux, macOS and Windows against a local release server.
- [x] 2026-09-11 — `agentdrop update [--check] [--to vX]`: checksum-verified
      self-replacement with package-manager path refusal; unit tests plus a
      live self-replacement of a running binary on Linux.
- [ ] Later: serve the installers from `agentdrop.lol/install.sh` and
      `/install.ps1`; Homebrew tap, Scoop, apt/RPM/AUR using the same artifacts;
      cosign signatures for `checksums.txt`.
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
