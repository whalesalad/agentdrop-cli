# Compatibility contract

This client freezes the deployed AgentDrop route subset and the Node CLI 0.2.0
command surface. Unknown JSON fields are preserved wherever a service object is
passed through. Additive server fields are fine; a breaking change needs a
versioned route or a migration window. The client never depends on a
simultaneous server deploy.

## Routes used

| Workflow | Operation |
| --- | --- |
| Upload | `POST /api/uploads` (bearer) then `PUT /uploads/{token}` (capability only, no bearer) |
| List / detail / account | `GET /api/files?limit&cursor`, `GET /api/files/{id}`, `GET /api/account` |
| Download | `GET /api/files/{id}/content` |
| Share | `POST /api/files/{id}/shares` `{durationSeconds, source}` |
| Delete / revoke | `DELETE /api/files/{id}`, `DELETE /api/shares/{id}` |
| Login | `POST /oauth/device/code` (`client_id=agentdrop-cli`, `name`), `POST /oauth/token` (device grant) |

Requests send `User-Agent: agentdrop/<version>` and `X-AgentDrop-Client`
(`AGENTDROP_CLIENT` if it matches `^[a-z0-9-]{1,40}$`, else `agentdrop-cli`).
Redirects are never followed. Response decompression is disabled so downloaded
bytes are exact. Control responses are bounded to 2 MiB, error bodies to 8 KiB.
Request bodies are never replayed by the transport (`GetBody` is cleared).

## Command surface

Identical to the Node CLI: `open` (default, also implicit for piped stdin),
`put`, `get`, `list`, `info`, `share`, `delete`, `revoke`, `login`, `logout`,
`whoami`, `exec`, `help`. New in the native client: `update [--check] [--to vX]`
replaces the running executable with a release from GitHub after verifying the
SHA-256 in `checksums.txt`; `--json` returns
`{"current","latest","updateAvailable","installed","path"}`. It refuses
package-manager locations (`/usr/bin`, Homebrew Cellar, Nix, Snap) and never
runs automatically or phones home on other commands.
`AGENTDROP_UPDATE_BASE_URL` points it at a mirror with the same file layout. Flags: `-h/--help`, `-o/--output`, `-n/--name`,
`--type`, `--expires` (`5m|30m|1h|1d|7d`, default `1h`), `--no-open`, `--json`,
`-y/--yes`, `--limit` (1–100, default 20), `--cursor`, `--profile`
(`[a-zA-Z0-9-]{1,64}`, default `default`), `--version`. `--flag=value` and
`--flag value` both work; `--` ends option parsing; `exec` requires `--`.

Stdout carries only results. Notices, login instructions, pagination hints and
errors go to stderr. Exit 0 on success, 1 on CLI failure, child exit code for
`exec`, `128+signal` on Unix signal exits.

## Credential storage

Directory: `AGENTDROP_CREDENTIAL_DIR`, else `$XDG_CONFIG_HOME/agentdrop`, else
`~/.config/agentdrop` (Unix) or `%APPDATA%\agentdrop` (Windows). File name is
the first 24 hex characters of SHA-256 over `origin + "\n" + profile` plus
`.json`; fields `origin`, `apiToken`, `apiTokenId`, `vaultId`. Unix requires
0700/0600 and current-user ownership; symlinks are rejected; reads are bounded
to 8 KiB. Existing Node CLI profiles on Linux/macOS load without a new login.
A Windows Node profile under `%USERPROFILE%\.config\agentdrop` is not imported;
log in again or point `AGENTDROP_CREDENTIAL_DIR` at it.

## Intentional differences from Node CLI 0.2.0

- `login --json` prints a stderr event `{"login":{"verificationUrl","userCode"}}`
  and a stdout result `{"profile","vaultId","apiTokenId"}` (no secrets).
- `logout --json` prints `{"profile","loggedOut":true,"remoteRevoked":false}`.
- `--version --json` prints version, commit, build date, Go version, os, arch.
- `--json` errors are one stderr object
  `{"error":{"code","message","httpStatus"?,"apiCode"?,"fileId"?}}`.
- `exec` requires the `--` separator (Node tolerated omitting it).
- `list` on a terminal is a tab-aligned table; terminal control characters in
  names are replaced. Pipes still get one ID per line.
- A definitive HTTP error from the capability PUT is reported with the API
  message plus the pending file ID, rather than the generic "did not finish
  cleanly" text. Connection-level failures keep the generic text.
- `get -o FILE` reports "Could not replace the destination" if the rename fails
  (for example a locked file on Windows); the old destination is preserved.
- Reader links are validated (origin match, `/s/` path) before browser launch.

## Local error codes

`usage`, `input`, `output`, `auth`, `storage`, `login`, `network`, `api`,
`upload-uncertain`, `share-missing`, `exec`, `cancelled`, `internal`.
