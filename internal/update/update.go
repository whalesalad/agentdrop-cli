// Package update implements `agentdrop update`: resolve the latest (or a
// requested) release, download the archive for this OS/arch, verify its
// SHA-256 against the release checksums, and atomically replace the running
// executable. It uses the same release layout as the installers.
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/whalesalad/agentdrop-cli/internal/api"
)

const (
	// DefaultBaseURL is the GitHub releases download root.
	DefaultBaseURL = "https://github.com/whalesalad/agentdrop-cli/releases/download"
	latestManifest = "https://github.com/whalesalad/agentdrop-cli/releases/latest/download/manifest.json"
	maxArchive     = 64 << 20
	maxText        = 1 << 20
	timeout        = 120 * time.Second
)

// Options controls one update run.
type Options struct {
	// BaseURL is the release root: GitHub layout (…/download/vX/asset) when it
	// contains github.com, otherwise a flat directory of the same files.
	BaseURL string
	// Version pins a release ("v1.2.3" or "1.2.3"); empty means latest.
	Version string
	// Current is the running version (from internal/version).
	Current string
	// Executable is the file to replace; empty means os.Executable().
	Executable string
	// GOOS/GOARCH default to the runtime values.
	GOOS, GOARCH string
	// HTTP defaults to a redirect-following HTTPS-only client.
	HTTP *http.Client
}

// Result is the machine-readable outcome.
type Result struct {
	Current         string `json:"current"`
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Installed       bool   `json:"installed"`
	Path            string `json:"path,omitempty"`
}

func fail(format string, args ...any) error { return api.Errorf("update", format, args...) }

func (o *Options) defaults() {
	if o.BaseURL == "" {
		o.BaseURL = DefaultBaseURL
	}
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.GOARCH == "" {
		o.GOARCH = runtime.GOARCH
	}
	if o.HTTP == nil {
		o.HTTP = NewHTTPClient()
	}
}

// NewHTTPClient follows redirects (GitHub release assets redirect to a CDN)
// but only to HTTPS or loopback destinations, and never decompresses.
func NewHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" && !isLoopback(req.URL) {
				return errors.New("insecure redirect refused")
			}
			return nil
		},
	}
}

func isLoopback(u *url.URL) bool {
	h := strings.ToLower(u.Hostname())
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func (o *Options) isGitHub() bool { return strings.Contains(o.BaseURL, "github.com") }

func (o *Options) fetch(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fail("Invalid release URL.")
	}
	// The default GitHub root is HTTPS. Plain HTTP is only accepted for loopback
	// or when the caller explicitly pointed at another release root (a LAN
	// mirror or a test server): choosing that root is the trust decision.
	if u.Scheme != "https" && !(u.Scheme == "http" && (isLoopback(u) || o.BaseURL != DefaultBaseURL)) {
		return nil, fail("Refusing insecure release URL.")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fail("Invalid release URL.")
	}
	req.Header.Set("User-Agent", "agentdrop-update/"+o.Current)
	resp, err := o.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &api.Error{Code: "cancelled", Message: "Cancelled."}
		}
		return nil, fail("Could not reach the release server.")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, fail("Release file not found: %s", path.Base(u.Path))
	}
	if resp.StatusCode != 200 {
		return nil, fail("Release server returned %d for %s.", resp.StatusCode, path.Base(u.Path))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fail("Download interrupted.")
	}
	if int64(len(data)) > limit {
		return nil, fail("Release file is unexpectedly large.")
	}
	return data, nil
}

// resolveVersion returns the target version without a leading v.
func (o *Options) resolveVersion(ctx context.Context) (string, error) {
	if o.Version != "" {
		return strings.TrimPrefix(o.Version, "v"), nil
	}
	manifestURL := strings.TrimSuffix(o.BaseURL, "/") + "/manifest.json"
	if o.isGitHub() {
		manifestURL = latestManifest
	}
	data, err := o.fetch(ctx, manifestURL, maxText)
	if err != nil {
		return "", err
	}
	var m struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &m) != nil || m.Version == "" {
		return "", fail("The release manifest did not contain a version.")
	}
	return strings.TrimPrefix(m.Version, "v"), nil
}

func (o *Options) releaseURL(ver, name string) string {
	base := strings.TrimSuffix(o.BaseURL, "/")
	if o.isGitHub() {
		base += "/v" + ver
	}
	return base + "/" + name
}

// Check resolves the target version and reports whether it differs from Current.
func Check(ctx context.Context, o Options) (*Result, error) {
	o.defaults()
	ver, err := o.resolveVersion(ctx)
	if err != nil {
		return nil, err
	}
	exe := o.Executable
	if exe == "" {
		exe, _ = os.Executable()
	}
	return &Result{
		Current:         o.Current,
		Latest:          ver,
		UpdateAvailable: Compare(o.Current, ver) < 0 || o.Version != "" && o.Current != ver,
		Path:            exe,
	}, nil
}

// Apply downloads, verifies, and installs the target version over the
// executable. It returns Installed=false without error when already current.
func Apply(ctx context.Context, o Options) (*Result, error) {
	o.defaults()
	res, err := Check(ctx, o)
	if err != nil {
		return nil, err
	}
	if !res.UpdateAvailable {
		return res, nil
	}
	exe := o.Executable
	if exe == "" {
		exe, err = os.Executable()
		if err != nil {
			return nil, fail("Could not locate the running executable.")
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
	}
	dir := filepath.Dir(exe)
	if managed(dir) {
		return nil, fail("agentdrop at %s looks package-manager managed; update it with that package manager.", exe)
	}
	ver := res.Latest
	ext := ".tar.gz"
	if o.GOOS == "windows" {
		ext = ".zip"
	}
	asset := fmt.Sprintf("agentdrop-%s-%s-%s%s", ver, o.GOOS, o.GOARCH, ext)

	sums, err := o.fetch(ctx, o.releaseURL(ver, "checksums.txt"), maxText)
	if err != nil {
		return nil, err
	}
	expected := lookupChecksum(string(sums), asset)
	if expected == "" {
		return nil, fail("checksums.txt for v%s does not list %s.", ver, asset)
	}
	archive, err := o.fetch(ctx, o.releaseURL(ver, asset), maxArchive)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(archive)
	if actual := hex.EncodeToString(sum[:]); actual != expected {
		return nil, fail("SHA-256 mismatch for %s. Nothing was changed.", asset)
	}
	binary, err := extract(archive, ext, o.GOOS)
	if err != nil {
		return nil, err
	}
	if err := replaceExecutable(exe, binary); err != nil {
		return nil, err
	}
	res.Installed = true
	res.Path = exe
	return res, nil
}

func managed(dir string) bool {
	d := filepath.ToSlash(dir)
	for _, p := range []string{"/usr/bin", "/usr/sbin", "/bin", "/opt/homebrew/", "/usr/local/Cellar", "/nix/store", "/snap/"} {
		if d == strings.TrimSuffix(p, "/") || strings.HasPrefix(d, p) {
			return true
		}
	}
	return false
}

func lookupChecksum(text, asset string) string {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if strings.TrimPrefix(fields[1], "*") == asset && len(fields[0]) == 64 {
			return strings.ToLower(fields[0])
		}
	}
	return ""
}

func extract(archive []byte, ext, goos string) ([]byte, error) {
	want := "agentdrop"
	if goos == "windows" {
		want = "agentdrop.exe"
	}
	if ext == ".zip" {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, fail("Release archive is not a valid zip.")
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) == want && !f.FileInfo().IsDir() {
				rc, err := f.Open()
				if err != nil {
					return nil, fail("Release archive could not be read.")
				}
				defer rc.Close()
				return readBounded(rc)
			}
		}
		return nil, fail("Release archive did not contain %s.", want)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fail("Release archive is not a valid tar.gz.")
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fail("Release archive could not be read.")
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == want {
			return readBounded(tr)
		}
	}
	return nil, fail("Release archive did not contain %s.", want)
}

func readBounded(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxArchive+1))
	if err != nil || len(data) > maxArchive || len(data) == 0 {
		return nil, fail("Release archive could not be read.")
	}
	return data, nil
}

// replaceExecutable writes the new binary beside the target and swaps it in.
// On Unix a rename over a running executable is fine. On Windows the running
// file cannot be overwritten but can be renamed, so the old one is moved aside
// and removed on a later run (see CleanupOld).
func replaceExecutable(exe string, binary []byte) error {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".agentdrop-update-*")
	if err != nil {
		return fail("Cannot write to %s. Reinstall with the installer or your package manager.", dir)
	}
	tmpName := tmp.Name()
	cleanup := func() { os.Remove(tmpName) }
	if _, err := tmp.Write(binary); err != nil {
		tmp.Close()
		cleanup()
		return fail("Could not write the new executable.")
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fail("Could not write the new executable.")
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		cleanup()
		return fail("Could not set permissions on the new executable.")
	}
	if runtime.GOOS == "windows" {
		old := exe + ".old"
		os.Remove(old)
		if err := os.Rename(exe, old); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanup()
			return fail("Could not move the current executable aside: %v", err)
		}
		if err := os.Rename(tmpName, exe); err != nil {
			_ = os.Rename(old, exe)
			cleanup()
			return fail("Could not install the new executable.")
		}
		return nil
	}
	if err := os.Rename(tmpName, exe); err != nil {
		cleanup()
		return fail("Could not replace %s.", exe)
	}
	return nil
}

// CleanupOld removes a leftover "<exe>.old" from a previous Windows update.
// Best effort; the file is still locked if the old process has not exited.
func CleanupOld() {
	if runtime.GOOS != "windows" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	_ = os.Remove(exe + ".old")
}

// Compare orders two versions like semver with numeric-aware prerelease
// identifiers: -1 if a < b, 0 if equal, 1 if a > b. "dev" sorts lowest.
func Compare(a, b string) int {
	a, b = strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v")
	if a == b {
		return 0
	}
	if a == "dev" || a == "" {
		return -1
	}
	if b == "dev" || b == "" {
		return 1
	}
	ca, pa := split(a)
	cb, pb := split(b)
	for i := 0; i < 3; i++ {
		if ca[i] != cb[i] {
			if ca[i] < cb[i] {
				return -1
			}
			return 1
		}
	}
	// A release outranks any prerelease of the same core.
	if pa == "" && pb != "" {
		return 1
	}
	if pa != "" && pb == "" {
		return -1
	}
	ia, ib := strings.Split(pa, "."), strings.Split(pb, ".")
	for i := 0; i < len(ia) && i < len(ib); i++ {
		na, ea := strconv.Atoi(ia[i])
		nb, eb := strconv.Atoi(ib[i])
		switch {
		case ea == nil && eb == nil:
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
		case ea == nil:
			return -1 // numeric identifiers sort before alphanumeric
		case eb == nil:
			return 1
		default:
			if ia[i] != ib[i] {
				if ia[i] < ib[i] {
					return -1
				}
				return 1
			}
		}
	}
	if len(ia) != len(ib) {
		if len(ia) < len(ib) {
			return -1
		}
		return 1
	}
	return 0
}

func split(v string) ([3]int, string) {
	var core [3]int
	v, _, _ = strings.Cut(v, "+")
	main, pre, _ := strings.Cut(v, "-")
	for i, part := range strings.SplitN(main, ".", 3) {
		if i < 3 {
			core[i], _ = strconv.Atoi(part)
		}
	}
	return core, pre
}
