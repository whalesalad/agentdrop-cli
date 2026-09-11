package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "adapi-testid0001-secretsecretsecret"

type fakeFile struct {
	ID          string
	Name        string
	ContentType string
	Data        []byte
	Ready       bool
}

// fakeAPI is a small in-memory stand-in for the deployed AgentDrop API.
type fakeAPI struct {
	mu        sync.Mutex
	server    *httptest.Server
	files     map[string]*fakeFile
	uploads   map[string]string // token -> file id
	shares    map[string]string // share id -> file id
	deleted   []string
	revoked   []string
	requests  []*http.Request
	counter   int
	failPUT   bool
	crossPUT  bool
	approved  bool
	pollCount int
	seenUA    []string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{files: map[string]*fakeFile{}, uploads: map[string]string{}, shares: map[string]string{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeAPI) origin() string { return f.server.URL }

func (f *fakeAPI) next(prefix string) string {
	f.counter++
	return fmt.Sprintf("%s%04d", prefix, f.counter)
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": message, "requestId": "req-test"}})
}

func (f *fakeAPI) fileJSON(file *fakeFile) map[string]any {
	var size any
	if file.Ready {
		size = len(file.Data)
	}
	return map[string]any{
		"id": file.ID, "name": file.Name, "safeName": file.Name, "contentType": file.ContentType,
		"sizeBytes": size, "status": map[bool]string{true: "ready", false: "pending"}[file.Ready],
		"createdBy": "api", "createdAt": "2026-09-11T00:00:00Z", "detailUrl": f.origin() + "/files/" + file.ID,
		"futureField": map[string]any{"nested": true},
	}
}

func (f *fakeAPI) shareJSON(id, fileID string) map[string]any {
	return map[string]any{
		"id": id, "fileId": fileID, "url": f.origin() + "/f/tok-" + id + "/name", "viewUrl": f.origin() + "/s/tok-" + id,
		"state": "active", "createdAt": "2026-09-11T00:00:00Z", "expiresAt": "2026-09-11T01:00:00Z", "revokedAt": nil,
	}
}

func (f *fakeAPI) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r)
	f.seenUA = append(f.seenUA, r.Header.Get("User-Agent"))
	path := r.URL.Path
	// OAuth device grant
	if path == "/oauth/device/code" || path == "/oauth/token" {
		f.handleOAuth(w, r)
		return
	}
	// Capability upload
	if strings.HasPrefix(path, "/uploads/") {
		if r.Header.Get("Authorization") != "" {
			writeErr(w, 400, "invalid-request", "token forwarded to capability")
			return
		}
		if f.failPUT {
			hj, ok := w.(http.Hijacker)
			if ok {
				c, _, _ := hj.Hijack()
				c.Close()
				return
			}
		}
		token := strings.TrimPrefix(path, "/uploads/")
		id, ok := f.uploads[token]
		if !ok {
			writeErr(w, 404, "upload-not-found", "no such upload")
			return
		}
		delete(f.uploads, token)
		file := f.files[id]
		body, _ := io.ReadAll(r.Body)
		if r.ContentLength != int64(len(body)) || r.Header.Get("Content-Type") != file.ContentType {
			writeErr(w, 400, "invalid-request", "length/type mismatch")
			return
		}
		file.Data = body
		file.Ready = true
		resp := map[string]any{"file": f.fileJSON(file)}
		if strings.Contains(token, "share") {
			sid := f.next("sh")
			f.shares[sid] = id
			resp["share"] = f.shareJSON(sid, id)
		}
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(resp)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		writeErr(w, 401, "authentication-required", "Sign in first.")
		return
	}
	switch {
	case path == "/api/uploads" && r.Method == "POST":
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		size := int(req["sizeBytes"].(float64))
		if size > 50<<20 {
			writeErr(w, 413, "file-too-large", "too big")
			return
		}
		file := &fakeFile{ID: f.next("f"), Name: req["name"].(string), ContentType: req["contentType"].(string)}
		f.files[file.ID] = file
		token := "up-" + file.ID
		if d, _ := req["shareDurationSeconds"].(float64); d > 0 {
			token += "-share"
		}
		f.uploads[token] = file.ID
		base := f.origin()
		if f.crossPUT {
			base = "https://evil.example"
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{
			"file": f.fileJSON(file),
			"upload": map[string]any{"method": "PUT", "url": base + "/uploads/" + token, "expiresAt": "2026-09-11T00:10:00Z",
				"headers": map[string]string{"content-length": fmt.Sprint(size), "content-type": file.ContentType}},
		})
	case path == "/api/files" && r.Method == "GET":
		var files []any
		for _, file := range f.files {
			files = append(files, f.fileJSON(file))
		}
		resp := map[string]any{"files": files, "nextCursor": nil}
		if r.URL.Query().Get("limit") == "1" {
			resp["nextCursor"] = "cursor-next"
		}
		json.NewEncoder(w).Encode(resp)
	case path == "/api/account":
		json.NewEncoder(w).Encode(map[string]any{"account": map[string]any{"id": "acct1"}, "usage": map[string]any{"filesToday": 1}})
	case strings.HasPrefix(path, "/api/files/"):
		rest := strings.TrimPrefix(path, "/api/files/")
		id, suffix, _ := strings.Cut(rest, "/")
		file, ok := f.files[id]
		if !ok {
			writeErr(w, 404, "file-not-found", "That file does not exist.")
			return
		}
		switch {
		case suffix == "content":
			w.Header().Set("Content-Type", file.ContentType)
			w.Header().Set("Content-Length", fmt.Sprint(len(file.Data)))
			w.Write(file.Data)
		case suffix == "shares" && r.Method == "POST":
			sid := f.next("sh")
			f.shares[sid] = id
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]any{"share": f.shareJSON(sid, id)})
		case suffix == "" && r.Method == "DELETE":
			delete(f.files, id)
			f.deleted = append(f.deleted, id)
			json.NewEncoder(w).Encode(map[string]any{"deleted": true, "id": id})
		case suffix == "":
			json.NewEncoder(w).Encode(map[string]any{"file": f.fileJSON(file)})
		default:
			writeErr(w, 404, "not-found", "no route")
		}
	case strings.HasPrefix(path, "/api/shares/") && r.Method == "DELETE":
		id := strings.TrimPrefix(path, "/api/shares/")
		if _, ok := f.shares[id]; !ok {
			writeErr(w, 404, "share-not-found", "That share does not exist.")
			return
		}
		delete(f.shares, id)
		f.revoked = append(f.revoked, id)
		json.NewEncoder(w).Encode(map[string]any{"revoked": true, "id": id})
	default:
		writeErr(w, 404, "not-found", "no route")
	}
}

func (f *fakeAPI) handleOAuth(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/oauth/device/code" {
		if r.Form.Get("client_id") != "agentdrop-cli" {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]any{"error": "invalid_client", "error_description": "bad client"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"device_code": "dev-code-secret", "user_code": "ABCD-1234",
			"verification_uri": f.origin() + "/activate", "verification_uri_complete": f.origin() + "/activate?user_code=ABCD-1234",
			"expires_in": 600, "interval": 5,
		})
		return
	}
	f.pollCount++
	if r.Form.Get("device_code") != "dev-code-secret" {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant", "error_description": "unknown device"})
		return
	}
	if f.pollCount == 1 {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending", "error_description": "pending"})
		return
	}
	if f.pollCount == 2 {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]any{"error": "slow_down", "error_description": "slow"})
		return
	}
	if !f.approved {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]any{"error": "access_denied", "error_description": "The request was denied."})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"access_token": testToken, "token_type": "Bearer", "scope": "vault:files", "expires_in": 3600,
		"vault_id": "vault-1", "api_token_id": "tok-1",
	})
}

// harness runs the CLI with a captured environment.
type harness struct {
	t       *testing.T
	api     *fakeAPI
	env     map[string]string
	stdin   io.Reader
	tty     bool
	stdinTT bool
	opened  []string
	timeout time.Duration
}

func newHarness(t *testing.T, api *fakeAPI) *harness {
	dir := t.TempDir()
	os.Chmod(dir, 0o700) // t.TempDir creates 0755; the store requires owner-only
	return &harness{t: t, api: api, env: map[string]string{
		"AGENTDROP_API_URL":        api.origin(),
		"AGENTDROP_API_TOKEN":      testToken,
		"AGENTDROP_CREDENTIAL_DIR": dir,
	}, timeout: 30 * time.Second}
}

type result struct {
	code   int
	stdout string
	stderr string
}

func (h *harness) run(args ...string) result {
	var out, errb strings.Builder
	stdin := h.stdin
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
	defer cancel()
	code := Run(ctx, &Env{
		Args: args, Stdin: stdin, Stdout: &out, Stderr: &errb,
		StdinIsTerminal: h.stdinTT, StdoutIsTerminal: h.tty,
		Getenv: func(k string) string { return h.env[k] },
		Environ: func() []string {
			var e []string
			for k, v := range h.env {
				e = append(e, k+"="+v)
			}
			return e
		},
		OpenBrowser: func(u string) error { h.opened = append(h.opened, u); return nil },
		Sleep:       func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
	})
	return result{code: code, stdout: out.String(), stderr: errb.String()}
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func mustJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &m); err != nil {
		t.Fatalf("invalid JSON %q: %v", s, err)
	}
	if strings.Count(strings.TrimSpace(s), "\n") != 0 {
		t.Fatalf("expected exactly one JSON line, got %q", s)
	}
	return m
}
