# AgentDrop CLI

Standalone, single-executable client for [AgentDrop](https://agentdrop.lol):
upload a file or piped text, get a temporary reader link, and manage files in
your personal vault. Written in Go with no runtime dependencies. No Node, npm,
Go, or daemon is required to use a release binary.

Status: **alpha**. Binaries are unsigned. Linux amd64 and Windows amd64 are the
targets being dogfooded first; macOS and arm64 builds are produced but have not
yet been exercised on real hardware.

## Install (alpha)

1. Download the archive for your platform from the
   [releases page](https://github.com/whalesalad/agentdrop-cli/releases):
   `agentdrop-<version>-linux-amd64.tar.gz`, `agentdrop-<version>-windows-amd64.zip`, etc.
2. Verify it against `checksums.txt` from the same release:
   `sha256sum -c checksums.txt --ignore-missing` (Linux/macOS) or
   `Get-FileHash .\agentdrop-<version>-windows-amd64.zip` (PowerShell).
3. Extract and put `agentdrop` (or `agentdrop.exe`) on your PATH, for example
   `~/.local/bin` or `%LOCALAPPDATA%\Programs\AgentDrop`.

Windows SmartScreen will warn about an unsigned executable during the alpha.

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
