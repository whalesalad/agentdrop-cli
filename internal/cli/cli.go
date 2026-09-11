// Package cli parses arguments, dispatches commands, and shapes output. It is a
// thin adapter over internal/api and internal/auth; API behavior is
// authoritative.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/whalesalad/agentdrop-cli/internal/api"
	"github.com/whalesalad/agentdrop-cli/internal/auth"
	"github.com/whalesalad/agentdrop-cli/internal/source"
	"github.com/whalesalad/agentdrop-cli/internal/update"
	"github.com/whalesalad/agentdrop-cli/internal/version"
)

// Env is everything the CLI touches from the process, so tests can substitute it.
type Env struct {
	Args             []string
	Stdin            io.Reader
	Stdout, Stderr   io.Writer
	StdinIsTerminal  bool
	StdoutIsTerminal bool
	Getenv           func(string) string
	Environ          func() []string
	OpenBrowser      func(string) error
	// Sleep overrides login polling waits (tests); nil uses real timers.
	Sleep func(context.Context, time.Duration) error
	// RunChild runs the exec child; nil uses the real implementation.
	RunChild func(ctx context.Context, command string, args []string, env []string, stdin io.Reader, stdout, stderr io.Writer) (int, error)
}

var durations = map[string]int{"5m": 300, "30m": 1800, "1h": 3600, "1d": 86400, "7d": 604800}

// Run executes the CLI and returns the process exit code.
func Run(ctx context.Context, env *Env) int {
	if env.Getenv == nil {
		env.Getenv = os.Getenv
	}
	if env.Environ == nil {
		env.Environ = os.Environ
	}
	opts, err := parse(env.Args)
	var code int
	if err == nil {
		code, err = run(ctx, env, opts)
	}
	if err == nil {
		return code
	}
	if isBrokenPipe(err) {
		return 0
	}
	var e *api.Error
	if !errors.As(err, &e) {
		e = &api.Error{Code: "internal", Message: err.Error()}
	}
	if e.Code == "cancelled" && ctx.Err() != nil {
		// The signal handler in main decides the exit status.
		return 130
	}
	if opts != nil && opts.json {
		fmt.Fprintln(env.Stderr, errorJSON(e))
	} else {
		fmt.Fprintf(env.Stderr, "agentdrop: %s\n", e.Message)
	}
	return 1
}

type session struct {
	env    *Env
	opts   *options
	origin string
}

func run(ctx context.Context, env *Env, opts *options) (int, error) {
	update.CleanupOld()
	if len(opts.positionals) > 0 && opts.positionals[0] == "update" {
		if len(opts.positionals) > 1 {
			return 1, usage("update does not take a filename.")
		}
		return runUpdate(ctx, env, opts)
	}
	if opts.version {
		if opts.json {
			return 0, writeJSON(env.Stdout, map[string]any{
				"version": version.Version, "commit": version.Commit, "buildDate": version.Date,
				"goVersion": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
			})
		}
		return 0, writeLine(env.Stdout, version.Version)
	}
	if opts.exec {
		return runExec(ctx, env, opts)
	}
	command := "open"
	if len(opts.positionals) > 0 && commandNames[opts.positionals[0]] {
		command = opts.positionals[0]
		opts.positionals = opts.positionals[1:]
	}
	if opts.help || command == "help" || (len(opts.positionals) == 0 && command == "open" && env.StdinIsTerminal) {
		_, err := io.WriteString(env.Stdout, helpText)
		return 0, err
	}
	if len(opts.positionals) > 1 {
		return 1, usage("One file or ID at a time. Run agentdrop --help.")
	}
	var target string
	if len(opts.positionals) == 1 {
		target = opts.positionals[0]
	}
	if !auth.ValidProfile(opts.profile) {
		return 1, usage("Use a profile name with 1–64 letters, numbers or hyphens.")
	}
	origin, err := api.ParseOrigin(env.Getenv("AGENTDROP_API_URL"))
	if err != nil {
		return 1, err
	}
	expires := opts.expires
	if expires == "" {
		expires = "1h"
	}
	duration, ok := durations[expires]
	if !ok {
		return 1, usage("Choose --expires=5m, 30m, 1h, 1d, or 7d.")
	}
	if opts.output != "" && command != "get" {
		return 1, usage("--output is for get. open creates a cloud reader link.")
	}
	if opts.json && command == "get" {
		return 1, usage("get returns file bytes, not JSON.")
	}
	switch command {
	case "get", "info", "share", "delete", "revoke":
		if target == "" {
			return 1, usage("%s requires an ID.", command)
		}
		kind := "File ID"
		if command == "revoke" {
			kind = "Share ID"
		}
		if err := api.ValidateID(kind, target); err != nil {
			return 1, err
		}
	case "list", "login", "logout", "whoami":
		if target != "" {
			return 1, usage("%s does not take a filename.", command)
		}
	}
	if (command == "delete" || command == "revoke") && !opts.yes {
		return 1, usage("Run %s with --yes to confirm.", command)
	}
	s := &session{env: env, opts: opts, origin: origin}
	switch command {
	case "login":
		return s.login(ctx)
	case "logout":
		return s.logout()
	}
	token, err := auth.ResolveToken(env.Getenv, origin, opts.profile)
	if err != nil {
		return 1, err
	}
	client := api.New(origin, token, version.Version, clientLabel(env.Getenv))
	switch command {
	case "open", "put":
		return s.upload(ctx, client, command, target, duration)
	case "get":
		return s.get(ctx, client, target)
	case "share":
		return s.share(ctx, client, target, duration)
	case "list":
		return s.list(ctx, client)
	case "info":
		return s.show(ctx, client, api.FilePath(target, ""))
	case "whoami":
		return s.show(ctx, client, "/api/account")
	case "delete":
		return s.remove(ctx, client, api.FilePath(target, ""), "File deleted.")
	case "revoke":
		return s.remove(ctx, client, api.SharePath(target), "Share revoked.")
	}
	return 1, usage("Unknown command. Run agentdrop --help.")
}

func clientLabel(getenv func(string) string) string {
	v := getenv("AGENTDROP_CLIENT")
	if len(v) == 0 || len(v) > 40 {
		return "agentdrop-cli"
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return "agentdrop-cli"
		}
	}
	return v
}

func (s *session) sourceObject() map[string]any {
	return source.Build(s.env.Getenv, version.Version)
}

func (s *session) notice(format string, args ...any) {
	if s.opts.json {
		return
	}
	fmt.Fprintf(s.env.Stderr, format+"\n", args...)
}

func (s *session) shouldOpenBrowser() bool {
	return !s.opts.noOpen && !s.opts.json && s.env.StdoutIsTerminal && s.env.Getenv("SSH_CONNECTION") == "" && s.env.OpenBrowser != nil
}

// readerURL validates a reader link before handing it to a browser.
func (s *session) readerURL(raw string) bool {
	u, err := parseURL(raw)
	return err == nil && strings.ToLower(u.Scheme)+"://"+strings.ToLower(u.Host) == s.origin &&
		strings.HasPrefix(u.Path, "/s/") && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func parseLimit(raw string) (string, error) {
	if raw == "" {
		return "20", nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 100 {
		return "", usage("--limit must be between 1 and 100.")
	}
	return strconv.Itoa(n), nil
}
