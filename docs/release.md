# Release runbook (alpha)

1. `make check` on main; confirm CI is green.
2. Tag: `git tag v0.3.0-alpha.N && git push origin v0.3.0-alpha.N`.
3. The `release` workflow runs `make check`, `scripts/build.sh`, and creates a
   release (marked latest, not a GitHub "prerelease", so that
   `releases/latest/download/manifest.json` resolves for the installers and
   `agentdrop update`) with six archives, `checksums.txt`, and `manifest.json`.
4. Download `checksums.txt` and one archive, verify the hash, run
   `agentdrop --version --json`, and confirm `version`/`commit` match the tag.
5. Smoke against production with a disposable file: `whoami`, `put`, `info`,
   `get -o` (compare SHA-256), `share`, `revoke`, `delete`.
6. Record the evidence in `docs/journal/` and update TODO.

Local equivalent without CI: `./scripts/build.sh v0.3.0-alpha.N` produces the
same `dist/` layout. Published artifacts are immutable; fix forward with a new
tag. Binaries are unsigned during alpha; signing, provenance, and installers
are tracked in TODO.
