package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/whalesalad/agentdrop-cli/internal/api"
	"github.com/whalesalad/agentdrop-cli/internal/auth"
	"github.com/whalesalad/agentdrop-cli/internal/update"
	"github.com/whalesalad/agentdrop-cli/internal/version"
)

func parseURL(raw string) (*url.URL, error) { return url.Parse(raw) }

func (s *session) inputBytes(target string) ([]byte, error) {
	if target != "" && target != "-" {
		info, err := os.Stat(target)
		if err != nil || !info.Mode().IsRegular() || info.Size() > api.MaxUploadBytes {
			return nil, api.Errorf("input", "Choose a regular file up to 50 MiB.")
		}
		f, err := os.Open(target)
		if err != nil {
			return nil, api.Errorf("input", "Could not read %s.", sanitize(target))
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, api.MaxUploadBytes+1))
		if err != nil {
			return nil, api.Errorf("input", "Could not read %s.", sanitize(target))
		}
		if len(data) > api.MaxUploadBytes {
			return nil, api.Errorf("input", "File exceeds 50 MiB.")
		}
		return data, nil
	}
	if s.env.StdinIsTerminal {
		return nil, api.Errorf("input", "Provide a file or pipe content into AgentDrop.")
	}
	data, err := io.ReadAll(io.LimitReader(s.env.Stdin, api.MaxUploadBytes+1))
	if err != nil {
		return nil, api.Errorf("input", "Could not read standard input.")
	}
	if len(data) > api.MaxUploadBytes {
		return nil, api.Errorf("input", "Input exceeds 50 MiB.")
	}
	return data, nil
}

func (s *session) upload(ctx context.Context, client *api.Client, command, target string, duration int) (int, error) {
	data, err := s.inputBytes(target)
	if err != nil {
		return 1, err
	}
	name := s.opts.name
	if name == "" {
		if target != "" && target != "-" {
			name = filepath.Base(target)
		} else {
			name = "response.md"
		}
	}
	contentType := s.opts.typ
	if contentType == "" {
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown") {
			contentType = "text/markdown"
		} else {
			contentType = "application/octet-stream"
		}
	}
	share := 0
	if command == "open" {
		share = duration
	}
	result, err := client.Upload(ctx, api.UploadRequest{Name: name, ContentType: contentType, ShareDurationSeconds: share, Source: s.sourceObject()}, data)
	if err != nil {
		return 1, err
	}
	fileID := stringField(result, "file", "id")
	if command == "put" {
		file, ok := field(result, "file")
		if !ok || fileID == "" {
			return 1, api.Errorf("api", "AgentDrop returned an invalid upload response.")
		}
		if s.opts.json {
			return 0, writeJSON(s.env.Stdout, file)
		}
		return 0, writeLine(s.env.Stdout, fileID)
	}
	viewURL := stringField(result, "share", "viewUrl")
	if viewURL == "" {
		return 1, &api.Error{Code: "share-missing", FileID: fileID,
			Message: fmt.Sprintf("Stored file %s, but no reader link was returned. Run agentdrop share %s.", fileID, fileID)}
	}
	if s.opts.json {
		if err := writeJSON(s.env.Stdout, result); err != nil {
			return 1, err
		}
	} else if err := writeLine(s.env.Stdout, viewURL); err != nil {
		return 1, err
	}
	s.notice("Link expires %s. File: %s", sanitize(stringField(result, "share", "expiresAt")), sanitize(fileID))
	if s.shouldOpenBrowser() {
		if !s.readerURL(viewURL) {
			s.notice("Reader link did not match the AgentDrop origin; not opening a browser.")
		} else if err := s.env.OpenBrowser(viewURL); err != nil {
			s.notice("Could not launch a browser. Open the URL above.")
		}
	}
	return 0, nil
}

func (s *session) get(ctx context.Context, client *api.Client, id string) (int, error) {
	output := s.opts.output
	if output == "" && s.env.StdoutIsTerminal {
		return 1, usage("Choose a destination: agentdrop get ID -o FILE (or -o - for stdout).")
	}
	resp, err := client.Download(ctx, id)
	if err != nil {
		return 1, err
	}
	defer resp.Body.Close()
	if output == "" || output == "-" {
		n, err := io.Copy(s.env.Stdout, resp.Body)
		if err != nil {
			if isBrokenPipe(err) {
				return 0, err
			}
			return 1, api.Errorf("output", "Download interrupted.")
		}
		if resp.ContentLength >= 0 && n != resp.ContentLength {
			return 1, api.Errorf("output", "Download ended early.")
		}
		return 0, nil
	}
	destination, err := filepath.Abs(output)
	if err != nil {
		return 1, api.Errorf("output", "Invalid output path.")
	}
	if info, err := os.Lstat(destination); err == nil {
		if !info.Mode().IsRegular() {
			return 1, api.Errorf("output", "Output must be a regular file, not a symlink or directory.")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return 1, api.Errorf("output", "Could not inspect the output path.")
	}
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])
	temp := filepath.Join(filepath.Dir(destination), ".agentdrop-"+hex.EncodeToString(nonce[:]))
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 1, api.Errorf("output", "Could not create a temporary file beside the destination.")
	}
	defer os.Remove(temp)
	n, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		return 1, api.Errorf("output", "Download interrupted; the destination was not changed.")
	}
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		return 1, api.Errorf("output", "Download ended early; the destination was not changed.")
	}
	if err := os.Rename(temp, destination); err != nil {
		return 1, api.Errorf("output", "Could not replace the destination. Is it open in another program?")
	}
	s.notice("Saved %s", sanitize(destination))
	return 0, nil
}

func (s *session) share(ctx context.Context, client *api.Client, id string, duration int) (int, error) {
	result, err := client.JSON(ctx, http.MethodPost, api.FilePath(id, "/shares"), map[string]any{
		"durationSeconds": duration,
		"source":          s.sourceObject(),
	})
	if err != nil {
		return 1, err
	}
	share, ok := field(result, "share")
	viewURL := stringField(result, "share", "viewUrl")
	if !ok || viewURL == "" {
		return 1, api.Errorf("api", "AgentDrop returned an invalid share response.")
	}
	if s.opts.json {
		if err := writeJSON(s.env.Stdout, share); err != nil {
			return 1, err
		}
	} else if err := writeLine(s.env.Stdout, viewURL); err != nil {
		return 1, err
	}
	s.notice("Link expires %s. Share: %s", sanitize(stringField(result, "share", "expiresAt")), sanitize(stringField(result, "share", "id")))
	return 0, nil
}

func (s *session) list(ctx context.Context, client *api.Client) (int, error) {
	limit, err := parseLimit(s.opts.limit)
	if err != nil {
		return 1, err
	}
	query := url.Values{"limit": {limit}}
	if s.opts.cursor != "" {
		query.Set("cursor", s.opts.cursor)
	}
	result, err := client.JSON(ctx, http.MethodGet, "/api/files?"+query.Encode(), nil)
	if err != nil {
		return 1, err
	}
	var page struct {
		Files []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			SizeBytes *int64 `json:"sizeBytes"`
		} `json:"files"`
		NextCursor string `json:"nextCursor"`
	}
	if json.Unmarshal(result, &page) != nil {
		return 1, api.Errorf("api", "AgentDrop returned an invalid list response.")
	}
	if s.opts.json {
		if err := writeJSON(s.env.Stdout, result); err != nil {
			return 1, err
		}
	} else if s.env.StdoutIsTerminal {
		tw := tabwriter.NewWriter(s.env.Stdout, 0, 4, 2, ' ', 0)
		for _, f := range page.Files {
			size := "pending"
			if f.SizeBytes != nil {
				size = humanSize(*f.SizeBytes)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", sanitize(f.ID), sanitize(f.Name), size)
		}
		if err := tw.Flush(); err != nil {
			return 1, err
		}
	} else {
		for _, f := range page.Files {
			if err := writeLine(s.env.Stdout, f.ID); err != nil {
				return 1, err
			}
		}
	}
	if page.NextCursor != "" {
		s.notice("More: agentdrop list --cursor=%s", sanitize(page.NextCursor))
	}
	return 0, nil
}

func (s *session) show(ctx context.Context, client *api.Client, path string) (int, error) {
	result, err := client.JSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return 1, err
	}
	if s.opts.json {
		return 0, writeJSON(s.env.Stdout, result)
	}
	return 0, writePretty(s.env.Stdout, result)
}

func (s *session) remove(ctx context.Context, client *api.Client, path, message string) (int, error) {
	result, err := client.JSON(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return 1, err
	}
	if s.opts.json {
		return 0, writeJSON(s.env.Stdout, result)
	}
	s.notice("%s", message)
	return 0, nil
}

func (s *session) login(ctx context.Context) (int, error) {
	dir, err := auth.ResolveDir(s.env.Getenv)
	if err != nil {
		return 1, err
	}
	name := s.opts.name
	if name == "" {
		name = "AgentDrop CLI"
	}
	opts := auth.LoginOptions{Name: name, Profile: s.opts.profile, Sleep: s.env.Sleep}
	opts.Instructions = func(verificationURL, userCode string) {
		if s.opts.json {
			_ = writeJSON(s.env.Stderr, map[string]any{"login": map[string]string{"verificationUrl": verificationURL, "userCode": userCode}})
			return
		}
		fmt.Fprintf(s.env.Stderr, "Open %s\nEnter code: %s\nSign in with your vault key and approve this client. Waiting for approval…\n", verificationURL, userCode)
	}
	if !s.opts.noOpen && !s.opts.json && s.env.Getenv("SSH_CONNECTION") == "" && s.env.OpenBrowser != nil {
		opts.OpenBrowser = s.env.OpenBrowser
	}
	result, err := auth.Login(ctx, s.origin, auth.Store{Dir: dir}, opts)
	if err != nil {
		return 1, err
	}
	if s.opts.json {
		return 0, writeJSON(s.env.Stdout, result)
	}
	fmt.Fprintf(s.env.Stderr, "Connected to personal vault %s. Saved API token for profile %s.\n", sanitize(result.VaultID), result.Profile)
	return 0, nil
}

func (s *session) logout() (int, error) {
	dir, err := auth.ResolveDir(s.env.Getenv)
	if err != nil {
		return 1, err
	}
	if err := (auth.Store{Dir: dir}).Remove(s.origin, s.opts.profile); err != nil {
		return 1, err
	}
	if s.opts.json {
		return 0, writeJSON(s.env.Stdout, map[string]any{"profile": s.opts.profile, "loggedOut": true, "remoteRevoked": false})
	}
	fmt.Fprintln(s.env.Stderr, "Removed this profile’s saved API token. Environment tokens are unchanged; revoke remote access in your vault’s API tokens page.")
	return 0, nil
}

func runExec(ctx context.Context, env *Env, opts *options) (int, error) {
	if len(opts.execArgs) == 0 {
		return 1, usage("Use agentdrop exec [--profile NAME] -- COMMAND [ARGS].")
	}
	if !auth.ValidProfile(opts.profile) {
		return 1, usage("Use a profile name with 1–64 letters, numbers or hyphens.")
	}
	origin, err := api.ParseOrigin(env.Getenv("AGENTDROP_API_URL"))
	if err != nil {
		return 1, err
	}
	token, err := auth.ResolveToken(env.Getenv, origin, opts.profile)
	if err != nil {
		return 1, err
	}
	var childEnv []string
	for _, kv := range env.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.EqualFold(key, "AGENTDROP_API_TOKEN") || strings.EqualFold(key, "AGENTDROP_ACCESS_KEY") {
			continue
		}
		childEnv = append(childEnv, kv)
	}
	childEnv = append(childEnv, "AGENTDROP_API_TOKEN="+token)
	runner := env.RunChild
	if runner == nil {
		runner = runChild
	}
	code, err := runner(ctx, opts.execArgs[0], opts.execArgs[1:], childEnv, env.Stdin, env.Stdout, env.Stderr)
	if err != nil {
		return 1, api.Errorf("exec", "Could not launch the requested client.")
	}
	return code, nil
}

func runUpdate(ctx context.Context, env *Env, opts *options) (int, error) {
	o := update.Options{
		BaseURL: env.Getenv("AGENTDROP_UPDATE_BASE_URL"),
		Version: opts.to,
		Current: version.Version,
	}
	var res *update.Result
	var err error
	if opts.check {
		res, err = update.Check(ctx, o)
	} else {
		res, err = update.Apply(ctx, o)
	}
	if err != nil {
		return 1, err
	}
	if opts.json {
		return 0, writeJSON(env.Stdout, res)
	}
	switch {
	case opts.check && res.UpdateAvailable:
		fmt.Fprintf(env.Stderr, "Update available: %s -> %s. Run agentdrop update.\n", res.Current, res.Latest)
	case opts.check:
		fmt.Fprintf(env.Stderr, "agentdrop %s is current.\n", res.Current)
	case res.Installed:
		fmt.Fprintf(env.Stderr, "Updated agentdrop %s -> %s at %s\n", res.Current, res.Latest, res.Path)
	default:
		fmt.Fprintf(env.Stderr, "agentdrop %s is already current.\n", res.Current)
	}
	return 0, nil
}
