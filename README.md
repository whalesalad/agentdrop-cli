# AgentDrop CLI

Standalone, single-executable client for [AgentDrop](https://agentdrop.lol):
upload a file or piped text, get a temporary reader link, and manage files in
your personal vault. Written in Go with no runtime dependencies. No Node, npm,
Go, or daemon is required to use a release binary.

Status: **alpha**. Binaries are unsigned. Linux amd64 and Windows amd64 are the
targets being dogfooded first; macOS and arm64 builds are produced but have not
yet been exercised on real hardware.

## Install (alpha)

**Linux and macOS**

```sh
curl -fsSL https://raw.githubusercontent.com/whalesalad/agentdrop-cli/main/scripts/install.sh | sh
```

Installs `~/.local/bin/agentdrop` for the current user after verifying the
release SHA-256. No sudo. It prints the PATH line to add if needed. Pin or roll
back with `sh -s -- --version v0.3.0-alpha.1`, choose a directory with
`--dir`, remove with `--uninstall`. Read the script first if you like: it is
about 150 lines of POSIX sh.

**Windows (PowerShell)**

```powershell
irm https://raw.githubusercontent.com/whalesalad/agentdrop-cli/main/scripts/install.ps1 | iex
```

Installs `%LOCALAPPDATA%\Programs\AgentDrop\agentdrop.exe` and adds that folder
to your user PATH; open a new terminal afterwards. If you download the script
instead of piping it, run it with `powershell -ExecutionPolicy Bypass -File
.\install.ps1` because Windows blocks scripts by default. Pass `-Version
vX.Y.Z` to pin, `-Uninstall` to remove.

**Manual**: download the archive for your platform from the
[releases page](https://github.com/whalesalad/agentdrop-cli/releases), verify it
against `checksums.txt` (`sha256sum -c checksums.txt --ignore-missing` or
`Get-FileHash`), extract, and put `agentdrop` on your PATH.

**Upgrading**: `agentdrop update` downloads the latest release for your
platform, verifies its checksum, and replaces itself in place. `agentdrop update
--check` only reports; `--to v0.3.0-alpha.2` pins or rolls back. Rerunning the
installer does the same thing from outside.

Binaries are unsigned during the alpha. Running `agentdrop` from a terminal
does not trigger SmartScreen; double-clicking the exe shows an Unknown
Publisher warning.

**Windows notes**: pass files as arguments (`agentdrop put .\report.md`).
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
agentdrop update                # self-update to the latest release
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
