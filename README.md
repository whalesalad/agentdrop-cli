# AgentDrop CLI

Standalone, single-executable client for [AgentDrop](https://agentdrop.lol):
upload a file or piped text, get a temporary reader link, and manage files in
your personal vault. Written in Go with no runtime dependencies. No Node, npm,
Go, or daemon is required to use a release binary.

Status: **alpha**. Binaries are unsigned. Linux amd64 and Windows amd64 are the
targets being dogfooded first; macOS and arm64 builds are produced but have not
yet been exercised on real hardware.

## Install (alpha)

**Windows (PowerShell):** download `install.ps1` from the release, then run it
through `powershell -ExecutionPolicy Bypass`. Windows client editions ship with
the Restricted execution policy, so a plain `.\install.ps1` fails with
"running scripts is disabled on this system". The command below works without
changing your policy:

```powershell
powershell -ExecutionPolicy Bypass -File .\install.ps1 -Version v0.3.0-alpha.1
```

It verifies the SHA-256 against `checksums.txt`, installs
`%LOCALAPPDATA%\Programs\AgentDrop\agentdrop.exe`, and adds that folder to
your user PATH. Open a **new** terminal afterwards. `-Uninstall` removes it.

**Manual (any platform):**

1. Download the archive for your platform from the
   [releases page](https://github.com/whalesalad/agentdrop-cli/releases):
   `agentdrop-<version>-linux-amd64.tar.gz`, `agentdrop-<version>-windows-amd64.zip`, etc.
2. Verify it against `checksums.txt` from the same release:
   `sha256sum -c checksums.txt --ignore-missing` (Linux/macOS) or
   `Get-FileHash .\agentdrop-<version>-windows-amd64.zip` (PowerShell).
3. Extract and put `agentdrop` (or `agentdrop.exe`) on your PATH, for example
   `~/.local/bin` or `%LOCALAPPDATA%\Programs\AgentDrop`.

Binaries are unsigned during the alpha. Running `agentdrop` from a terminal
does not trigger SmartScreen; double-clicking the exe may.

**Windows notes:** pass files as arguments (`agentdrop put .\report.md`).
Windows PowerShell 5.1 re-encodes text piped into native programs and decodes
their output with the OEM code page, so piped uploads are not byte-exact and
non-ASCII names in captured `--json` output look garbled. PowerShell 7 and
`cmd` are exact.

## Use

```sh
agentdrop login                 # approve a named API token in your browser
agentdrop whoami                # vault, entitlements, usage
echo "# hi" | agentdrop         # upload stdin, print a one-hour reader link
agentdrop report.md             # same, from a file (opens the browser on a terminal)
agentdrop put notes.pdf         # private upload; prints the file ID
agentdrop get ID -o notes.pdf   # download original bytes
agentdrop list                  # recent files
agentdrop share ID --expires 1d # new reader link
agentdrop revoke SHARE-ID --yes
agentdrop delete ID --yes
agentdrop exec -- some-agent    # run a child with AGENTDROP_API_TOKEN set
```

Add `--json` to any command for one machine-readable object on stdout; errors
become `{"error":{...}}` on stderr. `agentdrop --help` lists every option.

Environment: `AGENTDROP_API_TOKEN` overrides the saved profile,
`AGENTDROP_API_URL` selects another HTTPS origin, `AGENTDROP_CREDENTIAL_DIR`
relocates saved profiles. Saved credentials live in `~/.config/agentdrop`
(`%APPDATA%\agentdrop` on Windows), compatible with the Node CLI's layout.

## Develop

Requires the Go toolchain declared in `go.mod` (the `go` command downloads it
automatically with `GOTOOLCHAIN=auto`).

```sh
make check      # gofmt, vet (linux/windows/darwin), race tests
make build      # ./agentdrop for the host
make dist       # dist/ archives, checksums.txt, manifest.json for six targets
```

See [docs/compat.md](docs/compat.md) for the frozen API/CLI contract and the
documented differences from the Node CLI, [docs/release.md](docs/release.md)
for the release runbook, and [TODO.md](TODO.md) for the queue.

MIT licensed; see [LICENSE](LICENSE).
