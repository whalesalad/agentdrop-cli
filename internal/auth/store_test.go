package auth

import (
	"os"
	"path/filepath"
	"testing"
)

// The Node CLI computed sha256("https://agentdrop.lol\ndefault")[:24] for the
// default profile filename. Existing profiles must resolve to the same path.
func TestPathMatchesNodeLayout(t *testing.T) {
	s := Store{Dir: "/x"}
	got, err := s.Path("https://agentdrop.lol", "default")
	if err != nil || got != filepath.Join("/x", "62b84e7deef95276e0efdd50.json") {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := s.Path("https://agentdrop.lol", "bad name"); err == nil {
		t.Fatal("invalid profile accepted")
	}
}

func TestResolveDir(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	env["AGENTDROP_CREDENTIAL_DIR"] = "relative/dir"
	if _, err := ResolveDir(getenv); err == nil {
		t.Fatal("relative dir accepted")
	}
	env["AGENTDROP_CREDENTIAL_DIR"] = "/explicit"
	if d, _ := ResolveDir(getenv); d != "/explicit" {
		t.Fatalf("explicit: %q", d)
	}
	delete(env, "AGENTDROP_CREDENTIAL_DIR")
	env["XDG_CONFIG_HOME"] = "/xdg"
	if d, _ := ResolveDir(getenv); d != filepath.Join("/xdg", "agentdrop") {
		t.Fatalf("xdg: %q", d)
	}
}

func TestSaveLoadRemove(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "creds")
	s := Store{Dir: dir}
	if err := s.Prepare(); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(dir)
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %o", info.Mode().Perm())
	}
	cred := Credential{Origin: "https://agentdrop.lol", APIToken: "adapi-abc-def", APITokenID: "t1", VaultID: "v1"}
	if err := s.Save("default", cred); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("default", cred); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	got, err := s.Load("https://agentdrop.lol", "default")
	if err != nil || *got != cred {
		t.Fatalf("load: %+v %v", got, err)
	}
	if _, err := s.Load("https://other.example", "default"); err == nil {
		t.Fatal("credential leaked across origins")
	}
	pending, _ := filepath.Glob(filepath.Join(dir, ".pending-*"))
	if len(pending) != 0 {
		t.Fatalf("temp files left: %v", pending)
	}
	if err := s.Remove("https://agentdrop.lol", "default"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("https://agentdrop.lol", "default"); err == nil {
		t.Fatal("removed credential still loads")
	}
}
