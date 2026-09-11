# AgentDrop CLI Working Agreement

Resume from the repository, not chat memory: read [TODO.md](TODO.md), the latest
[journal](docs/journal/) entry, and [docs/compat.md](docs/compat.md) before
changing code. The application repository (`agentdrop`) owns the HTTP/auth
contract; this repository owns the client implementation and its releases.

Ship in small evidence-backed slices: implement, `make check`, exercise the real
binary against the deployed API with disposable records, update TODO/journal,
one coherent commit. Prefer the standard library; keep `cmd/agentdrop` thin and
behavior in `internal/*`. Do not create an SDK, plugin system, or DI framework.

Protect credentials: never print tokens, device codes, or capability URLs in
output, tests, logs, or journals. Tests use fake tokens and a fake HTTP server.
Never weaken origin checks, redirect refusal, or private credential storage for
convenience. Releases stay `CGO_ENABLED=0`; published artifacts are immutable.

Distinguish implemented, tested, and dogfooded in every handoff, and record the
exact next action.
