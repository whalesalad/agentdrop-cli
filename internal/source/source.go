// Package source builds the optional upload provenance object that the CLI
// sends with uploads and shares. It mirrors the Node CLI's source.mjs.
package source

import (
	"os"
	"os/user"
	"runtime"
	"strings"
)

// Build returns the source object for the current invocation. Collection is
// opt-in through AGENTDROP_SOURCE; failures never break an upload.
func Build(getenv func(string) string, version string) map[string]any {
	source := map[string]any{
		"transport": "cli",
		"client":    "agentdrop-cli",
		"version":   version,
	}
	if v := getenv("AGENTDROP_CLIENT"); v != "" {
		source["agent"] = truncate(v, 160)
	}
	if v := getenv("AGENTDROP_CLIENT_VERSION"); v != "" {
		source["agentVersion"] = truncate(v, 160)
	}
	switch mode := getenv("AGENTDROP_SOURCE"); {
	case mode == "auto":
		collect(source)
	case mode != "":
		source["label"] = truncate(mode, 160)
	}
	return source
}

func collect(source map[string]any) {
	if u, err := user.Current(); err == nil && u.Username != "" {
		source["username"] = u.Username
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		source["hostname"] = h
	}
	source["os"] = runtime.GOOS
	if runtime.GOOS != "linux" {
		return
	}
	text, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(text), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"`)
		switch key {
		case "NAME":
			if value != "" {
				source["os"] = value
			}
		case "VERSION_ID":
			if value != "" {
				source["osVersion"] = value
			}
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
