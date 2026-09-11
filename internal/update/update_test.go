package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.3.0", "0.3.0", 0}, {"v0.3.0", "0.3.0", 0},
		{"0.3.0-alpha.1", "0.3.0-alpha.2", -1}, {"0.3.0-alpha.10", "0.3.0-alpha.9", 1},
		{"0.3.0-alpha.2", "0.3.0", -1}, {"0.3.0", "0.3.0-rc.1", 1},
		{"0.3.0", "0.4.0", -1}, {"1.0.0", "0.9.9", 1},
		{"dev", "0.3.0-alpha.1", -1}, {"0.3.0-alpha.1", "dev", 1},
		{"0.3.0-alpha", "0.3.0-alpha.1", -1}, {"0.3.0-alpha.1", "0.3.0-beta.1", -1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func tarGz(t *testing.T, name string, data []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "agentdrop-x/" + name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	tw.Write(data)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func zipArchive(t *testing.T, name string, data []byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("agentdrop-x/" + name)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(data)
	zw.Close()
	return buf.Bytes()
}

// release serves a flat release directory for one version.
type release struct {
	files map[string][]byte
	hits  []string
}

func newRelease(t *testing.T, version string, goos, goarch string, binary []byte, corrupt bool) (*release, *httptest.Server) {
	r := &release{files: map[string][]byte{}}
	ext, archive := ".tar.gz", tarGz(t, "agentdrop", binary)
	if goos == "windows" {
		ext, archive = ".zip", zipArchive(t, "agentdrop.exe", binary)
	}
	asset := fmt.Sprintf("agentdrop-%s-%s-%s%s", version, goos, goarch, ext)
	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])
	if corrupt {
		digest = strings.Repeat("0", 64)
	}
	r.files[asset] = archive
	r.files["checksums.txt"] = []byte(digest + "  " + asset + "\nabc  other.tar.gz\n")
	r.files["manifest.json"] = []byte(`{"version":"` + version + `","status":"alpha"}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.hits = append(r.hits, req.URL.Path)
		data, ok := r.files[strings.TrimPrefix(req.URL.Path, "/")]
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return r, srv
}

func TestApplyReplacesExecutable(t *testing.T) {
	goos := runtime.GOOS
	newBinary := []byte("#!/bin/sh\necho new\n")
	_, srv := newRelease(t, "0.3.0-alpha.2", goos, runtime.GOARCH, newBinary, false)
	dir := t.TempDir()
	exe := filepath.Join(dir, "agentdrop")
	if goos == "windows" {
		exe += ".exe"
	}
	os.WriteFile(exe, []byte("old"), 0o755)

	res, err := Apply(context.Background(), Options{BaseURL: srv.URL, Current: "0.3.0-alpha.1", Executable: exe})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Installed || res.Latest != "0.3.0-alpha.2" || !res.UpdateAvailable || res.Path != exe {
		t.Fatalf("result: %+v", res)
	}
	got, _ := os.ReadFile(exe)
	if !bytes.Equal(got, newBinary) {
		t.Fatalf("executable not replaced: %q", got)
	}
	if goos != "windows" {
		if info, _ := os.Stat(exe); info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("not executable: %v", info.Mode())
		}
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".agentdrop-update-*"))
	if len(leftovers) != 0 {
		t.Fatalf("temp files left: %v", leftovers)
	}

	// Already current: nothing downloaded beyond the manifest.
	r2, srv2 := newRelease(t, "0.3.0-alpha.2", goos, runtime.GOARCH, newBinary, false)
	res, err = Apply(context.Background(), Options{BaseURL: srv2.URL, Current: "0.3.0-alpha.2", Executable: exe})
	if err != nil || res.Installed || res.UpdateAvailable {
		t.Fatalf("current: %+v %v", res, err)
	}
	if len(r2.hits) != 1 || r2.hits[0] != "/manifest.json" {
		t.Fatalf("unexpected requests when current: %v", r2.hits)
	}
	// Explicit pin to an older version is allowed (rollback).
	res, err = Apply(context.Background(), Options{BaseURL: srv2.URL, Current: "0.4.0", Version: "v0.3.0-alpha.2", Executable: exe})
	if err != nil || !res.Installed {
		t.Fatalf("rollback: %+v %v", res, err)
	}
}

func TestApplyChecksumMismatchLeavesExecutable(t *testing.T) {
	_, srv := newRelease(t, "0.3.0-alpha.2", runtime.GOOS, runtime.GOARCH, []byte("evil"), true)
	exe := filepath.Join(t.TempDir(), "agentdrop")
	os.WriteFile(exe, []byte("old"), 0o755)
	_, err := Apply(context.Background(), Options{BaseURL: srv.URL, Current: "0.3.0-alpha.1", Executable: exe})
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("expected checksum failure, got %v", err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "old" {
		t.Fatalf("executable changed on failure: %q", got)
	}
}

func TestApplyMissingAssetAndManagedPath(t *testing.T) {
	_, srv := newRelease(t, "0.3.0-alpha.2", "plan9", "mips", []byte("x"), false)
	exe := filepath.Join(t.TempDir(), "agentdrop")
	os.WriteFile(exe, []byte("old"), 0o755)
	_, err := Apply(context.Background(), Options{BaseURL: srv.URL, Current: "0.1.0", Executable: exe})
	if err == nil || !strings.Contains(err.Error(), "does not list") {
		t.Fatalf("expected missing asset error, got %v", err)
	}
	_, err = Apply(context.Background(), Options{BaseURL: srv.URL, Current: "0.1.0", Executable: "/usr/bin/agentdrop"})
	if err == nil || !strings.Contains(err.Error(), "package-manager") {
		t.Fatalf("expected managed-path refusal, got %v", err)
	}
}

func TestCheckOnlyFetchesManifest(t *testing.T) {
	r, srv := newRelease(t, "0.3.0-alpha.2", runtime.GOOS, runtime.GOARCH, []byte("x"), false)
	res, err := Check(context.Background(), Options{BaseURL: srv.URL, Current: "0.3.0-alpha.1"})
	if err != nil || !res.UpdateAvailable || res.Latest != "0.3.0-alpha.2" || len(r.hits) != 1 {
		t.Fatalf("check: %+v %v hits=%v", res, err, r.hits)
	}
}

func TestInsecureDefaultRootRefused(t *testing.T) {
	// A plain-HTTP non-loopback URL is refused unless the caller chose that root.
	o := Options{BaseURL: DefaultBaseURL, Current: "0.1.0"}
	o.defaults()
	if _, err := o.fetch(context.Background(), "http://example.com/manifest.json", maxText); err == nil || !strings.Contains(err.Error(), "insecure") {
		t.Fatalf("expected insecure refusal, got %v", err)
	}
}

func TestExtractWindowsZip(t *testing.T) {
	bin, err := extract(zipArchive(t, "agentdrop.exe", []byte("exe")), ".zip", "windows")
	if err != nil || string(bin) != "exe" {
		t.Fatalf("zip extract: %q %v", bin, err)
	}
	if _, err := extract(zipArchive(t, "other.exe", []byte("exe")), ".zip", "windows"); err == nil {
		t.Fatal("missing exe in zip should fail")
	}
}
