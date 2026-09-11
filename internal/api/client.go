// Package api is the thin HTTP adapter for the deployed AgentDrop API. It
// owns origin validation, bounded JSON handling, capability upload, and
// download streaming. Behavior mirrors the Node CLI's request() helper.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxUploadBytes bounds any single upload before preparation.
	MaxUploadBytes = 50 << 20
	maxJSONBytes   = 2 << 20
	maxErrorBytes  = 8 << 10
	// OperationTimeout bounds one file or control request.
	OperationTimeout = 120 * time.Second
	// DefaultOrigin is used when AGENTDROP_API_URL is unset.
	DefaultOrigin = "https://agentdrop.lol"
)

// Error is a CLI-facing failure with a stable local code.
type Error struct {
	Code       string
	Message    string
	HTTPStatus int
	APICode    string
	FileID     string
}

func (e *Error) Error() string { return e.Message }

// Errorf builds a local error.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ParseOrigin validates and canonicalizes the API origin. HTTPS is required
// except for loopback hosts.
func ParseOrigin(raw string) (string, error) {
	invalid := Errorf("usage", "AGENTDROP_API_URL must be an HTTPS origin (HTTP allowed for localhost).")
	if raw == "" {
		raw = DefaultOrigin
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", invalid
	}
	if u.Path != "" && u.Path != "/" {
		return "", invalid
	}
	host := strings.ToLower(u.Hostname())
	loopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !loopback {
			return "", invalid
		}
	default:
		return "", invalid
	}
	scheme := strings.ToLower(u.Scheme)
	hostport := strings.ToLower(u.Host)
	// Drop default ports so credentials for one origin canonicalize consistently.
	if (scheme == "https" && strings.HasSuffix(hostport, ":443")) || (scheme == "http" && strings.HasSuffix(hostport, ":80")) {
		hostport = hostport[:strings.LastIndex(hostport, ":")]
	}
	return scheme + "://" + hostport, nil
}

// Client performs authenticated and capability requests against one origin.
type Client struct {
	Origin      string
	Token       string
	UserAgent   string
	ClientLabel string
	HTTP        *http.Client
}

// New builds a client. token may be empty for unauthenticated use.
func New(origin, token, version, clientLabel string) *Client {
	return &Client{
		Origin:      origin,
		Token:       token,
		UserAgent:   "agentdrop/" + version,
		ClientLabel: clientLabel,
		HTTP:        NewHTTPClient(OperationTimeout),
	}
}

// NewHTTPClient returns a client that never follows redirects and never
// transparently decompresses bodies.
func NewHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableCompression = true
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// ValidateID rejects values that cannot be a single opaque URL segment.
func ValidateID(kind, id string) error {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\ \t\r\n") {
		return Errorf("usage", "%s must be a single opaque ID.", kind)
	}
	for _, r := range id {
		if r < 0x20 || r == 0x7f {
			return Errorf("usage", "%s must be a single opaque ID.", kind)
		}
	}
	return nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body []byte, contentType string, auth bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.Origin+path, bytes.NewReader(body))
	if err != nil {
		return nil, Errorf("usage", "Invalid request.")
	}
	// Never let the transport replay a request body: one-use PUTs and quota
	// consuming preparations must not be retried implicitly.
	req.GetBody = nil
	req.ContentLength = int64(len(body))
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("X-AgentDrop-Client", c.ClientLabel)
	if auth && c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, networkError(req.Context(), err)
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		resp.Body.Close()
		return nil, &Error{Code: "api", Message: "AgentDrop returned an unexpected redirect.", HTTPStatus: resp.StatusCode}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, apiError(resp)
	}
	return resp, nil
}

func networkError(ctx context.Context, err error) error {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return &Error{Code: "cancelled", Message: "Cancelled."}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: "network", Message: "AgentDrop did not respond in time."}
	}
	return &Error{Code: "network", Message: "Could not reach AgentDrop."}
}

func apiError(resp *http.Response) *Error {
	e := &Error{Code: "api", HTTPStatus: resp.StatusCode, Message: fmt.Sprintf("AgentDrop request failed (%d).", resp.StatusCode)}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes))
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		if envelope.Error.Code != "" {
			e.APICode = envelope.Error.Code
		}
		if envelope.Error.Message != "" {
			e.Message = envelope.Error.Message
		}
	}
	if retry := resp.Header.Get("Retry-After"); retry != "" && resp.StatusCode == 429 {
		if seconds, err := strconv.Atoi(retry); err == nil && seconds > 0 && seconds < 86400*7 {
			e.Message += fmt.Sprintf(" Retry after %d seconds.", seconds)
		}
	}
	return e
}

func readJSON(resp *http.Response) (json.RawMessage, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBytes+1))
	if err != nil {
		return nil, networkError(resp.Request.Context(), err)
	}
	if len(body) > maxJSONBytes {
		return nil, Errorf("api", "AgentDrop returned an oversized response.")
	}
	if !json.Valid(body) {
		return nil, Errorf("api", "AgentDrop returned an invalid response.")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, body); err != nil {
		return nil, Errorf("api", "AgentDrop returned an invalid response.")
	}
	return json.RawMessage(compact.Bytes()), nil
}

// JSON performs an authenticated control request and returns the compacted
// response body, preserving unknown fields.
func (c *Client) JSON(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var payload []byte
	contentType := ""
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, Errorf("usage", "Invalid request.")
		}
		contentType = "application/json"
	}
	req, err := c.newRequest(ctx, method, path, payload, contentType, true)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	return readJSON(resp)
}

// UploadRequest describes one prepared upload.
type UploadRequest struct {
	Name                 string
	ContentType          string
	ShareDurationSeconds int
	Source               map[string]any
}

// Upload prepares an upload and transfers data through the returned one-use
// capability. It returns the completion response (file plus optional share).
// Errors after preparation carry the pending file ID.
func (c *Client) Upload(ctx context.Context, req UploadRequest, data []byte) (json.RawMessage, error) {
	if len(data) > MaxUploadBytes {
		return nil, Errorf("input", "Input exceeds 50 MiB.")
	}
	body := map[string]any{
		"name":                 req.Name,
		"sizeBytes":            len(data),
		"contentType":          req.ContentType,
		"shareDurationSeconds": req.ShareDurationSeconds,
	}
	if req.Source != nil {
		body["source"] = req.Source
	}
	prepared, err := c.JSON(ctx, http.MethodPost, "/api/uploads", body)
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Code == "network" {
			e.Message += " If the upload was prepared, it may appear in agentdrop list; check before retrying."
		}
		return nil, err
	}
	var plan struct {
		File struct {
			ID string `json:"id"`
		} `json:"file"`
		Upload struct {
			Method  string            `json:"method"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"upload"`
	}
	if json.Unmarshal(prepared, &plan) != nil || plan.File.ID == "" {
		return nil, Errorf("api", "AgentDrop returned an invalid upload plan.")
	}
	fileID := plan.File.ID
	fail := func(code, message string) *Error {
		return &Error{Code: code, Message: message, FileID: fileID}
	}
	target, err := url.Parse(plan.Upload.URL)
	if err != nil || strings.ToLower(target.Scheme)+"://"+strings.ToLower(target.Host) != c.Origin ||
		!strings.HasPrefix(target.Path, "/uploads/") || target.User != nil || target.RawQuery != "" || target.Fragment != "" ||
		!strings.EqualFold(plan.Upload.Method, http.MethodPut) {
		return nil, fail("api", fmt.Sprintf("Invalid upload destination. Check agentdrop info %s before retrying.", fileID))
	}
	contentType := req.ContentType
	for key, value := range plan.Upload.Headers {
		switch strings.ToLower(key) {
		case "content-type":
			if value != req.ContentType {
				return nil, fail("api", fmt.Sprintf("Upload plan changed the media type. Check agentdrop info %s before retrying.", fileID))
			}
		case "content-length":
			if value != strconv.Itoa(len(data)) {
				return nil, fail("api", fmt.Sprintf("Upload plan changed the size. Check agentdrop info %s before retrying.", fileID))
			}
		}
	}
	put, err := c.newRequest(ctx, http.MethodPut, target.EscapedPath(), data, contentType, false)
	if err != nil {
		return nil, fail("usage", "Invalid upload destination.")
	}
	resp, err := c.do(put)
	if err != nil {
		var e *Error
		if errors.As(err, &e) {
			if e.Code == "cancelled" {
				return nil, fail("cancelled", fmt.Sprintf("Cancelled. Check agentdrop info %s before retrying.", fileID))
			}
			if e.Code == "api" {
				return nil, &Error{Code: "api", Message: fmt.Sprintf("%s Check agentdrop info %s before retrying.", e.Message, fileID), HTTPStatus: e.HTTPStatus, APICode: e.APICode, FileID: fileID}
			}
		}
		return nil, fail("upload-uncertain", fmt.Sprintf("Upload did not finish cleanly. Check agentdrop info %s before retrying.", fileID))
	}
	result, err := readJSON(resp)
	if err != nil {
		return nil, fail("upload-uncertain", fmt.Sprintf("Upload did not finish cleanly. Check agentdrop info %s before retrying.", fileID))
	}
	return result, nil
}

// Download opens the original bytes of a file. The caller must close the body.
func (c *Client) Download(ctx context.Context, id string) (*http.Response, error) {
	if err := ValidateID("File ID", id); err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, http.MethodGet, "/api/files/"+url.PathEscape(id)+"/content", nil, "", true)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

// FilePath returns the encoded API path for a file resource.
func FilePath(id string, suffix string) string {
	return "/api/files/" + url.PathEscape(id) + suffix
}

// SharePath returns the encoded API path for a share resource.
func SharePath(id string) string {
	return "/api/shares/" + url.PathEscape(id)
}
