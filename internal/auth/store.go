// Package auth owns local credential profiles, the device login grant, and
// token resolution. Storage follows the Node CLI layout so existing protected
// profiles keep working.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"

	"github.com/whalesalad/agentdrop-cli/internal/api"
)

const maxCredentialBytes = 8192

var (
	profileRe = regexp.MustCompile(`^[a-zA-Z0-9-]{1,64}$`)
	tokenRe   = regexp.MustCompile(`^adapi-[a-zA-Z0-9]+-[a-zA-Z0-9]+$`)
)

// IsAPIToken reports whether value looks like an AgentDrop API token.
func IsAPIToken(value string) bool {
	return len(value) <= 4096 && tokenRe.MatchString(value)
}

// ValidProfile reports whether name is an acceptable profile name.
func ValidProfile(name string) bool { return profileRe.MatchString(name) }

// Credential is the saved profile record. Field names are frozen.
type Credential struct {
	Origin     string `json:"origin"`
	APIToken   string `json:"apiToken"`
	APITokenID string `json:"apiTokenId,omitempty"`
	VaultID    string `json:"vaultId,omitempty"`
}

// Store is a credential directory.
type Store struct{ Dir string }

// ResolveDir picks the credential directory from the environment.
func ResolveDir(getenv func(string) string) (string, error) {
	dir := getenv("AGENTDROP_CREDENTIAL_DIR")
	if dir == "" {
		if xdg := getenv("XDG_CONFIG_HOME"); xdg != "" {
			dir = filepath.Join(xdg, "agentdrop")
		} else if runtime.GOOS == "windows" {
			appdata := getenv("APPDATA")
			if appdata == "" {
				return "", api.Errorf("storage", "APPDATA is not set; set AGENTDROP_CREDENTIAL_DIR to a private directory.")
			}
			dir = filepath.Join(appdata, "agentdrop")
		} else {
			home, err := os.UserHomeDir()
			if err != nil || home == "" {
				return "", api.Errorf("storage", "Could not find your home directory; set AGENTDROP_CREDENTIAL_DIR.")
			}
			dir = filepath.Join(home, ".config", "agentdrop")
		}
	}
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) {
		return "", api.Errorf("storage", "The AgentDrop credential directory must be an absolute path.")
	}
	return dir, nil
}

// Path returns the credential file for one origin and profile.
func (s Store) Path(origin, profile string) (string, error) {
	if !ValidProfile(profile) {
		return "", api.Errorf("usage", "Use a profile name with 1–64 letters, numbers or hyphens.")
	}
	sum := sha256.Sum256([]byte(origin + "\n" + profile))
	return filepath.Join(s.Dir, hex.EncodeToString(sum[:])[:24]+".json"), nil
}

var errNotPrivate = errors.New("not private")

// Prepare creates the directory with owner-only permissions and verifies it.
func (s Store) Prepare() error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return api.Errorf("storage", "Could not create the AgentDrop credential directory.")
	}
	info, err := os.Lstat(s.Dir)
	if err != nil || !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 || checkPrivate(info) != nil {
		return api.Errorf("storage", "The AgentDrop credential directory must be private to your user (mode 700 on Unix).")
	}
	return nil
}

// Load reads and validates the saved credential for origin/profile.
func (s Store) Load(origin, profile string) (*Credential, error) {
	path, err := s.Path(origin, profile)
	if err != nil {
		return nil, err
	}
	invalid := api.Errorf("auth", "Saved client credential is invalid. Run agentdrop login again.")
	unsafe := api.Errorf("storage", "Saved API token must be a private regular file (mode 600 on Unix).")
	linfo, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, api.Errorf("auth", "Run agentdrop login, or configure AGENTDROP_API_TOKEN. Vault keys are only used in the browser.")
		}
		return nil, unsafe
	}
	if linfo.Mode()&fs.ModeSymlink != 0 {
		return nil, unsafe
	}
	f, err := openNoFollow(path)
	if err != nil {
		return nil, unsafe
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxCredentialBytes || checkPrivate(info) != nil {
		return nil, unsafe
	}
	data, err := io.ReadAll(io.LimitReader(f, maxCredentialBytes+1))
	if err != nil || len(data) > maxCredentialBytes {
		return nil, unsafe
	}
	var cred Credential
	if json.Unmarshal(data, &cred) != nil || cred.Origin != origin || !IsAPIToken(cred.APIToken) {
		return nil, invalid
	}
	return &cred, nil
}

// Save writes the credential atomically with owner-only permissions.
func (s Store) Save(profile string, cred Credential) error {
	path, err := s.Path(cred.Origin, profile)
	if err != nil {
		return err
	}
	data, err := json.Marshal(cred)
	if err != nil {
		return api.Errorf("storage", "Could not encode the credential.")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return api.Errorf("storage", "Could not create a temporary credential file.")
	}
	temp := filepath.Join(s.Dir, ".pending-"+hex.EncodeToString(nonce[:]))
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// Remove deletes the credential for origin/profile if present.
func (s Store) Remove(origin, profile string) error {
	path, err := s.Path(origin, profile)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return api.Errorf("storage", "Could not remove the saved API token.")
	}
	return nil
}

// ResolveToken returns the API token from the environment or the saved profile.
func ResolveToken(getenv func(string) string, origin, profile string) (string, error) {
	if env := getenv("AGENTDROP_API_TOKEN"); env != "" {
		if !IsAPIToken(env) {
			return "", api.Errorf("auth", "AGENTDROP_API_TOKEN must contain an API token. Vault keys are for browser sign-in.")
		}
		return env, nil
	}
	dir, err := ResolveDir(getenv)
	if err != nil {
		return "", err
	}
	cred, err := Store{Dir: dir}.Load(origin, profile)
	if err != nil {
		return "", err
	}
	return cred.APIToken, nil
}
