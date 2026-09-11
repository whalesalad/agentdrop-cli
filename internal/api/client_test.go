package api

import "testing"

func TestParseOrigin(t *testing.T) {
	good := map[string]string{
		"":                          "https://agentdrop.lol",
		"https://agentdrop.lol":     "https://agentdrop.lol",
		"https://AgentDrop.lol/":    "https://agentdrop.lol",
		"https://agentdrop.lol:443": "https://agentdrop.lol",
		"http://localhost:8787":     "http://localhost:8787",
		"http://127.0.0.1:8787/":    "http://127.0.0.1:8787",
		"http://[::1]:8787":         "http://[::1]:8787",
	}
	for in, want := range good {
		got, err := ParseOrigin(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{
		"http://agentdrop.lol", "https://user:pw@agentdrop.lol", "https://agentdrop.lol/api",
		"https://agentdrop.lol?x=1", "https://agentdrop.lol#frag", "ftp://agentdrop.lol", "agentdrop.lol", "https://",
	}
	for _, in := range bad {
		if _, err := ParseOrigin(in); err == nil {
			t.Errorf("%q: expected rejection", in)
		}
	}
}

func TestValidateID(t *testing.T) {
	for _, id := range []string{"", ".", "..", "a/b", "a b", "a\nb", "a\x7f"} {
		if ValidateID("File ID", id) == nil {
			t.Errorf("%q should be rejected", id)
		}
	}
	for _, id := range []string{"4k7m2p9q", "x%2Fy", "ünïcode"} {
		if err := ValidateID("File ID", id); err != nil {
			t.Errorf("%q should be accepted: %v", id, err)
		}
	}
}
