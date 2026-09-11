package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/whalesalad/agentdrop-cli/internal/auth"
)

func TestVersionAndHelp(t *testing.T) {
	h := newHarness(t, newFakeAPI(t))
	r := h.run("--version")
	if r.code != 0 || strings.TrimSpace(r.stdout) != "dev" {
		t.Fatalf("version: %+v", r)
	}
	r = h.run("--version", "--json")
	m := mustJSON(t, r.stdout)
	if m["version"] != "dev" || m["os"] == "" {
		t.Fatalf("version json: %+v", m)
	}
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}} {
		r = h.run(args...)
		if r.code != 0 || !strings.Contains(r.stdout, "agentdrop put FILE") {
			t.Fatalf("help %v: %+v", args, r)
		}
	}
	h.stdinTT = true
	r = h.run()
	if r.code != 0 || !strings.Contains(r.stdout, "Options:") {
		t.Fatalf("bare terminal invocation should show help: %+v", r)
	}
}

func TestUsageErrors(t *testing.T) {
	h := newHarness(t, newFakeAPI(t))
	cases := map[string][]string{
		"One file or ID at a time":  {"a", "b"},
		"requires an ID":            {"info"},
		"does not take a filename":  {"list", "x"},
		"with --yes":                {"delete", "f1"},
		"--output is for get":       {"open", "x", "-o", "y"},
		"get returns file bytes":    {"get", "f1", "--json"},
		"Choose --expires":          {"share", "f1", "--expires", "2h"},
		"Unknown option":            {"--bogus"},
		"requires a value":          {"--name"},
		"profile name":              {"list", "--profile", "bad/name"},
		"single opaque ID":          {"info", ".."},
		"--limit must be":           {"list", "--limit", "500"},
		"exec [--profile NAME] --":  {"exec"},
		"exec [--profile NAME] -- ": {"exec", "sh"},
	}
	for want, args := range cases {
		r := h.run(args...)
		if r.code != 1 || !strings.Contains(r.stderr, want) {
			t.Errorf("%v: want %q got %+v", args, want, r)
		}
	}
	r := h.run("info", "f1", "--json")
	m := mustJSON(t, r.stderr)
	if r.code != 1 || m["error"].(map[string]any)["code"] != "api" {
		t.Fatalf("json api error: %+v", r)
	}
	if e := m["error"].(map[string]any); e["httpStatus"] != float64(404) || e["apiCode"] != "file-not-found" {
		t.Fatalf("json api error fields: %+v", e)
	}
}

func TestOriginAndTokenValidation(t *testing.T) {
	h := newHarness(t, newFakeAPI(t))
	h.env["AGENTDROP_API_URL"] = "http://example.com"
	if r := h.run("list"); r.code != 1 || !strings.Contains(r.stderr, "HTTPS origin") {
		t.Fatalf("plain http origin: %+v", r)
	}
	h.env["AGENTDROP_API_URL"] = "https://agentdrop.lol/path"
	if r := h.run("list"); r.code != 1 || !strings.Contains(r.stderr, "HTTPS origin") {
		t.Fatalf("path origin: %+v", r)
	}
	h.env["AGENTDROP_API_URL"] = h.api.origin()
	h.env["AGENTDROP_API_TOKEN"] = "ad-vaultkey-notatoken"
	if r := h.run("list"); r.code != 1 || !strings.Contains(r.stderr, "must contain an API token") {
		t.Fatalf("vault key rejected: %+v", r)
	}
	delete(h.env, "AGENTDROP_API_TOKEN")
	if r := h.run("list"); r.code != 1 || !strings.Contains(r.stderr, "Run agentdrop login") {
		t.Fatalf("missing credential: %+v", r)
	}
}

func TestPutGetRoundTrip(t *testing.T) {
	api := newFakeAPI(t)
	h := newHarness(t, api)
	dir := t.TempDir()
	payload := append([]byte("binary\x00\xff\n"), bytes.Repeat([]byte{0x00, 0x01}, 1000)...)
	src := filepath.Join(dir, "weird name ünï.bin")
	os.WriteFile(src, payload, 0o644)

	r := h.run("put", src)
	if r.code != 0 {
		t.Fatalf("put: %+v", r)
	}
	id := strings.TrimSpace(r.stdout)
	file := api.files[id]
	if file == nil || file.Name != "weird name ünï.bin" || file.ContentType != "application/octet-stream" || sha(file.Data) != sha(payload) {
		t.Fatalf("stored file mismatch: %+v", file)
	}
	if len(r.stderr) != 0 {
		t.Fatalf("put should be quiet on stderr: %q", r.stderr)
	}

	// Empty file uploads with explicit zero content length.
	empty := filepath.Join(dir, "empty.md")
	os.WriteFile(empty, nil, 0o644)
	r = h.run("put", empty, "--json")
	m := mustJSON(t, r.stdout)
	if r.code != 0 || m["contentType"] != "text/markdown" || m["sizeBytes"] != float64(0) || m["futureField"] == nil {
		t.Fatalf("empty md put json (unknown fields preserved): %+v %+v", r, m)
	}

	// get to file, hash compare, existing destination replaced.
	dest := filepath.Join(dir, "out.bin")
	os.WriteFile(dest, []byte("old"), 0o644)
	r = h.run("get", id, "-o", dest)
	got, _ := os.ReadFile(dest)
	if r.code != 0 || sha(got) != sha(payload) || !strings.Contains(r.stderr, "Saved ") {
		t.Fatalf("get -o: %+v", r)
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".agentdrop-*"))
	if len(leftovers) != 0 {
		t.Fatalf("temp files left: %v", leftovers)
	}
	// get to non-terminal stdout, exact bytes.
	r = h.run("get", id)
	if r.code != 0 || sha([]byte(r.stdout)) != sha(payload) {
		t.Fatalf("get stdout: code=%d", r.code)
	}
	r = h.run("get", id, "-o", "-")
	if r.code != 0 || sha([]byte(r.stdout)) != sha(payload) {
		t.Fatalf("get -o -: code=%d", r.code)
	}
	h.tty = true
	if r = h.run("get", id); r.code != 1 || !strings.Contains(r.stderr, "Choose a destination") {
		t.Fatalf("get to terminal refused: %+v", r)
	}
	h.tty = false
	// Directory destination refused; symlink destination refused where symlinks exist.
	if r = h.run("get", id, "-o", dir); r.code != 1 || !strings.Contains(r.stderr, "regular file") {
		t.Fatalf("directory dest: %+v", r)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dest, link); err == nil {
		if r = h.run("get", id, "-o", link); r.code != 1 || !strings.Contains(r.stderr, "regular file") {
			t.Fatalf("symlink dest: %+v", r)
		}
	}
}

func TestOpenFromStdinAndFlags(t *testing.T) {
	api := newFakeAPI(t)
	h := newHarness(t, api)
	h.stdin = strings.NewReader("# hello\n")
	r := h.run()
	if r.code != 0 || !strings.HasPrefix(r.stdout, api.origin()+"/s/") || !strings.Contains(r.stderr, "Link expires") {
		t.Fatalf("implicit open: %+v", r)
	}
	var stored *fakeFile
	for _, f := range api.files {
		stored = f
	}
	if stored.Name != "response.md" || stored.ContentType != "text/markdown" {
		t.Fatalf("stdin defaults: %+v", stored)
	}
	if len(h.opened) != 0 {
		t.Fatalf("browser must not open without a terminal")
	}

	// Terminal + open → browser launched with validated reader URL; SSH suppresses.
	h.tty = true
	h.stdin = strings.NewReader("x")
	r = h.run("open", "-", "--name", "note.txt", "--type", "text/plain", "--expires=7d")
	if r.code != 0 || len(h.opened) != 1 || !strings.HasPrefix(h.opened[0], api.origin()+"/s/") {
		t.Fatalf("open with browser: %+v opened=%v", r, h.opened)
	}
	h.opened = nil
	h.env["SSH_CONNECTION"] = "1"
	h.stdin = strings.NewReader("x")
	r = h.run("open", "-")
	if r.code != 0 || len(h.opened) != 0 {
		t.Fatalf("ssh suppresses browser: %+v", r)
	}
	delete(h.env, "SSH_CONNECTION")
	h.stdin = strings.NewReader("x")
	r = h.run("open", "-", "--no-open")
	if r.code != 0 || len(h.opened) != 0 {
		t.Fatalf("--no-open: %+v", r)
	}

	// --json returns the completion response {file, share} and suppresses notices.
	h.tty = false
	h.stdin = strings.NewReader("x")
	r = h.run("--json")
	m := mustJSON(t, r.stdout)
	if r.code != 0 || m["file"] == nil || m["share"] == nil || r.stderr != "" || len(h.opened) != 0 {
		t.Fatalf("open json: %+v", r)
	}

	// Filename that equals a command, and a dash-prefixed filename via --.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "list"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "-dash.md"), []byte("b"), 0o644)
	if r = h.run("open", filepath.Join(dir, "list")); r.code != 0 {
		t.Fatalf("open ./list: %+v", r)
	}
	if r = h.run("put", "--", filepath.Join(dir, "-dash.md")); r.code != 0 {
		t.Fatalf("put -- -dash: %+v", r)
	}
	if r = h.run("put", filepath.Join(dir, "missing")); r.code != 1 || !strings.Contains(r.stderr, "regular file up to 50 MiB") {
		t.Fatalf("missing input: %+v", r)
	}
	if r = h.run("put", dir); r.code != 1 {
		t.Fatalf("directory input: %+v", r)
	}
	h.stdinTT = true
	if r = h.run("put"); r.code != 1 || !strings.Contains(r.stderr, "pipe content") {
		t.Fatalf("terminal stdin: %+v", r)
	}
}

func TestOversizedStdinRejectedBeforePreparation(t *testing.T) {
	api := newFakeAPI(t)
	h := newHarness(t, api)
	h.stdin = &zeroReader{n: 50<<20 + 1}
	r := h.run("put")
	if r.code != 1 || !strings.Contains(r.stderr, "50 MiB") || len(api.requests) != 0 {
		t.Fatalf("oversized: %+v requests=%d", r, len(api.requests))
	}
	h.stdin = &zeroReader{n: 50 << 20}
	if r = h.run("put"); r.code != 0 {
		t.Fatalf("exact limit should upload: %+v", r)
	}
}

type zeroReader struct{ n int }

func (z *zeroReader) Read(p []byte) (int, error) {
	if z.n == 0 {
		return 0, io.EOF
	}
	if len(p) > z.n {
		p = p[:z.n]
	}
	z.n -= len(p)
	return len(p), nil
}

func TestUploadFailures(t *testing.T) {
	api := newFakeAPI(t)
	h := newHarness(t, api)
	api.crossPUT = true
	h.stdin = strings.NewReader("x")
	r := h.run("put", "--json")
	m := mustJSON(t, r.stderr)
	e := m["error"].(map[string]any)
	if r.code != 1 || !strings.Contains(e["message"].(string), "Invalid upload destination") || e["fileId"] == nil {
		t.Fatalf("cross-origin upload URL: %+v", r)
	}
	api.crossPUT = false
	api.failPUT = true
	h.stdin = strings.NewReader("x")
	r = h.run("put", "--json")
	e = mustJSON(t, r.stderr)["error"].(map[string]any)
	if r.code != 1 || e["code"] != "upload-uncertain" || !strings.Contains(e["message"].(string), "agentdrop info "+e["fileId"].(string)) {
		t.Fatalf("uncertain upload: %+v", r)
	}
}

func TestListInfoShareDeleteRevokeWhoami(t *testing.T) {
	api := newFakeAPI(t)
	h := newHarness(t, api)
	h.stdin = strings.NewReader("a")
	id := strings.TrimSpace(h.run("put", "--name", "evil\x1b[31mname.md").stdout)

	// list: pipe → IDs; terminal → sanitized table; json → raw.
	r := h.run("list")
	if r.code != 0 || strings.TrimSpace(r.stdout) != id {
		t.Fatalf("list pipe: %+v", r)
	}
	h.tty = true
	r = h.run("list", "--limit", "1")
	if r.code != 0 || strings.Contains(r.stdout, "\x1b") || !strings.Contains(r.stdout, "1 bytes") || !strings.Contains(r.stderr, "--cursor=cursor-next") {
		t.Fatalf("list terminal: %+v", r)
	}
	h.tty = false
	r = h.run("list", "--json", "--cursor", "abc")
	m := mustJSON(t, r.stdout)
	if r.code != 0 || m["files"] == nil || r.stderr != "" {
		t.Fatalf("list json: %+v", r)
	}
	last := api.requests[len(api.requests)-1]
	if last.URL.Query().Get("cursor") != "abc" || last.URL.Query().Get("limit") != "20" {
		t.Fatalf("list query: %s", last.URL.RawQuery)
	}

	r = h.run("info", id)
	if r.code != 0 || !strings.Contains(r.stdout, "\n  \"file\": {") {
		t.Fatalf("info pretty: %+v", r)
	}
	r = h.run("info", id, "--json")
	if mustJSON(t, r.stdout)["file"] == nil {
		t.Fatalf("info json: %+v", r)
	}
	r = h.run("whoami")
	if r.code != 0 || !strings.Contains(r.stdout, "\"usage\"") {
		t.Fatalf("whoami: %+v", r)
	}

	r = h.run("share", id, "--expires", "5m")
	if r.code != 0 || !strings.HasPrefix(r.stdout, api.origin()+"/s/") || !strings.Contains(r.stderr, "Share: sh") {
		t.Fatalf("share: %+v", r)
	}
	r = h.run("share", id, "--json")
	share := mustJSON(t, r.stdout)
	if share["id"] == nil || share["viewUrl"] == nil || share["url"] == nil {
		t.Fatalf("share json: %+v", share)
	}
	shareID := share["id"].(string)

	r = h.run("revoke", shareID, "--yes")
	if r.code != 0 || r.stdout != "" || !strings.Contains(r.stderr, "Share revoked.") || api.revoked[0] != shareID {
		t.Fatalf("revoke: %+v", r)
	}
	r = h.run("revoke", shareID, "-y", "--json")
	if r.code != 1 || mustJSON(t, r.stderr)["error"].(map[string]any)["apiCode"] != "share-not-found" {
		t.Fatalf("revoke twice: %+v", r)
	}
	r = h.run("delete", id, "-y", "--json")
	if r.code != 0 || mustJSON(t, r.stdout)["deleted"] != true || api.deleted[0] != id {
		t.Fatalf("delete json: %+v", r)
	}
	// Encoded IDs stay a single segment.
	r = h.run("info", "a b%2F..c", "--json")
	if r.code != 1 || !strings.Contains(r.stderr, "single opaque ID") {
		t.Fatalf("id with space rejected: %+v", r)
	}
	r = h.run("info", "x%2F..y")
	if last := api.requests[len(api.requests)-1]; last.URL.Path != "/api/files/x%2F..y" && last.URL.EscapedPath() != "/api/files/x%252F..y" {
		t.Fatalf("id encoding: %s", last.URL.EscapedPath())
	}
}

func TestLoginLogoutAndProfiles(t *testing.T) {
	api := newFakeAPI(t)
	api.approved = true
	h := newHarness(t, api)
	delete(h.env, "AGENTDROP_API_TOKEN")
	h.tty = true

	r := h.run("login", "--name", "Alpha Box")
	if r.code != 0 || !strings.Contains(r.stderr, "Enter code: ABCD-1234") || !strings.Contains(r.stderr, "Connected to personal vault vault-1") || strings.Contains(r.stderr, testToken) {
		t.Fatalf("login: %+v", r)
	}
	if len(h.opened) != 1 || h.opened[0] != api.origin()+"/activate?user_code=ABCD-1234" {
		t.Fatalf("login browser: %v", h.opened)
	}
	if api.pollCount < 3 {
		t.Fatalf("expected pending/slow_down handling, polls=%d", api.pollCount)
	}
	// Saved file: name hash matches the Node layout, permissions are private.
	store := auth.Store{Dir: h.env["AGENTDROP_CREDENTIAL_DIR"]}
	path, _ := store.Path(api.origin(), "default")
	info, err := os.Stat(path)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("saved credential: %v %v", err, info)
	}
	cred, err := store.Load(api.origin(), "default")
	if err != nil || cred.APIToken != testToken || cred.VaultID != "vault-1" || cred.APITokenID != "tok-1" {
		t.Fatalf("load: %v %+v", err, cred)
	}
	// Saved profile now authenticates API commands.
	if r = h.run("whoami"); r.code != 0 {
		t.Fatalf("whoami via profile: %+v", r)
	}
	// Other profile is isolated.
	if r = h.run("whoami", "--profile", "work"); r.code != 1 || !strings.Contains(r.stderr, "Run agentdrop login") {
		t.Fatalf("profile isolation: %+v", r)
	}
	// JSON login emits an event on stderr, result on stdout, no browser.
	h.opened = nil
	api.pollCount = 0
	r = h.run("login", "--json", "--profile", "work")
	if r.code != 0 || len(h.opened) != 0 {
		t.Fatalf("json login: %+v", r)
	}
	if ev := mustJSON(t, r.stderr)["login"].(map[string]any); ev["userCode"] != "ABCD-1234" || ev["verificationUrl"] != api.origin()+"/activate" {
		t.Fatalf("login event: %+v", ev)
	}
	if res := mustJSON(t, r.stdout); res["profile"] != "work" || res["vaultId"] != "vault-1" || res["apiTokenId"] != "tok-1" || res["apiToken"] != nil {
		t.Fatalf("login result: %+v", res)
	}
	// logout removes only the selected profile.
	r = h.run("logout", "--profile", "work", "--json")
	if r.code != 0 || mustJSON(t, r.stdout)["loggedOut"] != true {
		t.Fatalf("logout json: %+v", r)
	}
	if r = h.run("whoami", "--profile", "work"); r.code != 1 {
		t.Fatalf("logged out profile still works: %+v", r)
	}
	if r = h.run("whoami"); r.code != 0 {
		t.Fatalf("default profile was disturbed: %+v", r)
	}
	if r = h.run("logout"); r.code != 0 || !strings.Contains(r.stderr, "Removed this profile") {
		t.Fatalf("logout human: %+v", r)
	}
	if r = h.run("logout"); r.code != 0 {
		t.Fatalf("logout idempotent: %+v", r)
	}

	// Denied approval.
	api.approved = false
	api.pollCount = 0
	if r = h.run("login", "--no-open"); r.code != 1 || !strings.Contains(r.stderr, "denied") || len(h.opened) != 0 {
		t.Fatalf("denied login: %+v", r)
	}
	// Env token wins over saved profiles and needs no store.
	h.env["AGENTDROP_API_TOKEN"] = testToken
	h.env["AGENTDROP_CREDENTIAL_DIR"] = filepath.Join(t.TempDir(), "does-not-exist")
	if r = h.run("whoami"); r.code != 0 {
		t.Fatalf("env token precedence: %+v", r)
	}
}

func TestUnsafeCredentialFilesRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission model; Windows DACL checks are tracked in TODO")
	}
	api := newFakeAPI(t)
	h := newHarness(t, api)
	delete(h.env, "AGENTDROP_API_TOKEN")
	store := auth.Store{Dir: h.env["AGENTDROP_CREDENTIAL_DIR"]}
	path, _ := store.Path(api.origin(), "default")
	os.WriteFile(path, []byte(`{"origin":"`+api.origin()+`","apiToken":"`+testToken+`"}`), 0o644)
	if r := h.run("whoami"); r.code != 1 || !strings.Contains(r.stderr, "private regular file") {
		t.Fatalf("world-readable credential: %+v", r)
	}
	os.Chmod(path, 0o600)
	if r := h.run("whoami"); r.code != 0 {
		t.Fatalf("private credential accepted: %+v", r)
	}
	// Wrong origin inside the record is rejected.
	os.WriteFile(path, []byte(`{"origin":"https://other.example","apiToken":"`+testToken+`"}`), 0o600)
	if r := h.run("whoami"); r.code != 1 || !strings.Contains(r.stderr, "invalid. Run agentdrop login") {
		t.Fatalf("origin mismatch: %+v", r)
	}
	// Symlinked credential is rejected.
	os.Remove(path)
	real := filepath.Join(t.TempDir(), "real.json")
	os.WriteFile(real, []byte(`{"origin":"`+api.origin()+`","apiToken":"`+testToken+`"}`), 0o600)
	os.Symlink(real, path)
	if r := h.run("whoami"); r.code != 1 || !strings.Contains(r.stderr, "private regular file") {
		t.Fatalf("symlink credential: %+v", r)
	}
}

func TestExecPassesTokenAndExitCode(t *testing.T) {
	api := newFakeAPI(t)
	h := newHarness(t, api)
	child, err := os.Executable()
	if err != nil {
		t.Skip("no test executable path")
	}
	h.env["AGENTDROP_ACCESS_KEY"] = "legacy"
	h.env["AGENTDROP_CLI_TEST_CHILD"] = "1"
	h.env["AGENTDROP_CLI_TEST_EXIT"] = "3"
	r := h.run("exec", "--", child, "--json", "-o", "x y")
	if r.code != 3 || r.stdout != testToken+"||--json,-o,x y" {
		t.Fatalf("exec: %+v", r)
	}
	h.env["AGENTDROP_CLI_TEST_EXIT"] = "0"
	if r = h.run("exec", "--profile", "default", "--", child); r.code != 0 || !strings.HasPrefix(r.stdout, testToken+"|") {
		t.Fatalf("exec profile flag: %+v", r)
	}
	if r = h.run("exec", "--", "definitely-not-a-command-xyz"); r.code != 1 || !strings.Contains(r.stderr, "Could not launch") {
		t.Fatalf("exec missing command: %+v", r)
	}
	delete(h.env, "AGENTDROP_API_TOKEN")
	if r = h.run("exec", "--", child); r.code != 1 || !strings.Contains(r.stderr, "Run agentdrop login") || r.stdout != "" {
		t.Fatalf("exec without credential must not run child: %+v", r)
	}
}

func TestClientLabelAndUserAgent(t *testing.T) {
	api := newFakeAPI(t)
	h := newHarness(t, api)
	h.env["AGENTDROP_CLIENT"] = "Bad Label!"
	h.run("whoami")
	last := api.requests[len(api.requests)-1]
	if last.Header.Get("X-AgentDrop-Client") != "agentdrop-cli" || last.Header.Get("User-Agent") != "agentdrop/dev" {
		t.Fatalf("headers: %v", last.Header)
	}
	h.env["AGENTDROP_CLIENT"] = "claude-code"
	h.run("whoami")
	if api.requests[len(api.requests)-1].Header.Get("X-AgentDrop-Client") != "claude-code" {
		t.Fatalf("client label not honored")
	}
}
