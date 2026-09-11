package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/whalesalad/agentdrop-cli/internal/api"
)

const (
	clientID       = "agentdrop-cli"
	deviceGrant    = "urn:ietf:params:oauth:grant-type:device_code"
	oauthTimeout   = 15 * time.Second
	maxLoginWindow = 600 * time.Second
	minInterval    = 5 * time.Second
	maxBackoff     = 60 * time.Second
)

var userCodeRe = regexp.MustCompile(`^[A-Z0-9]{4}-[A-Z0-9]{4}$`)

// LoginOptions controls one device login.
type LoginOptions struct {
	Name    string
	Profile string
	// Instructions receives the verification URL and user code before polling.
	Instructions func(verificationURL, userCode string)
	// OpenBrowser, when non-nil, is invoked best-effort with the complete URL.
	OpenBrowser func(url string) error
	// HTTP overrides the OAuth client (tests). Sleep overrides waiting (tests).
	HTTP  *http.Client
	Sleep func(context.Context, time.Duration) error
}

// LoginResult is the secret-free outcome of a login.
type LoginResult struct {
	Profile    string `json:"profile"`
	VaultID    string `json:"vaultId"`
	APITokenID string `json:"apiTokenId"`
}

type oauthResponse struct {
	ok   bool
	body map[string]any
}

func (o oauthResponse) str(key string) string {
	v, _ := o.body[key].(string)
	return v
}

func (o oauthResponse) num(key string) (float64, bool) {
	v, ok := o.body[key].(float64)
	return v, ok
}

func oauth(ctx context.Context, client *http.Client, origin, path string, fields url.Values) (oauthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+path, strings.NewReader(fields.Encode()))
	if err != nil {
		return oauthResponse{}, api.Errorf("login", "Client login could not start.")
	}
	req.GetBody = nil
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "agentdrop-cli")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return oauthResponse{}, &api.Error{Code: "cancelled", Message: "Cancelled."}
		}
		return oauthResponse{}, &api.Error{Code: "network", Message: "Could not reach AgentDrop."}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return oauthResponse{}, &api.Error{Code: "network", Message: "Could not reach AgentDrop."}
	}
	var body map[string]any
	if json.Unmarshal(data, &body) != nil || body == nil {
		return oauthResponse{}, api.Errorf("login", "Client login received an invalid response.")
	}
	return oauthResponse{ok: resp.StatusCode >= 200 && resp.StatusCode < 300, body: body}, nil
}

// Login runs the device authorization grant and saves the issued token.
func Login(ctx context.Context, origin string, store Store, opts LoginOptions) (*LoginResult, error) {
	if opts.Name == "" {
		opts.Name = "AgentDrop CLI"
	}
	if opts.Profile == "" {
		opts.Profile = "default"
	}
	if opts.HTTP == nil {
		opts.HTTP = api.NewHTTPClient(oauthTimeout)
	}
	if opts.Sleep == nil {
		opts.Sleep = sleep
	}
	if _, err := store.Path(origin, opts.Profile); err != nil {
		return nil, err
	}
	if err := store.Prepare(); err != nil {
		return nil, err
	}
	started, err := oauth(ctx, opts.HTTP, origin, "/oauth/device/code", url.Values{"client_id": {clientID}, "name": {opts.Name}})
	if err != nil {
		return nil, err
	}
	if !started.ok {
		msg := started.str("error_description")
		if msg == "" {
			msg = "Client login could not start."
		}
		return nil, api.Errorf("login", "%s", msg)
	}
	verification, err := url.Parse(started.str("verification_uri_complete"))
	deviceCode := started.str("device_code")
	userCode := started.str("user_code")
	expiresIn, okExpires := started.num("expires_in")
	interval, okInterval := started.num("interval")
	if err != nil || strings.ToLower(verification.Scheme)+"://"+strings.ToLower(verification.Host) != origin ||
		verification.Path != "/activate" || verification.User != nil || verification.Fragment != "" ||
		deviceCode == "" || len(deviceCode) > 128 || !userCodeRe.MatchString(userCode) || !okExpires || !okInterval {
		return nil, api.Errorf("login", "Client login received an invalid verification request.")
	}
	if opts.Instructions != nil {
		opts.Instructions(origin+"/activate", userCode)
	}
	if opts.OpenBrowser != nil {
		_ = opts.OpenBrowser(verification.String())
	}
	window := time.Duration(expiresIn) * time.Second
	if window > maxLoginWindow || window <= 0 {
		window = maxLoginWindow
	}
	deadline := time.Now().Add(window)
	wait := time.Duration(interval) * time.Second
	if wait < minInterval {
		wait = minInterval
	}
	for time.Now().Before(deadline) {
		if err := opts.Sleep(ctx, wait); err != nil {
			return nil, &api.Error{Code: "cancelled", Message: "Cancelled."}
		}
		if !time.Now().Before(deadline) {
			break
		}
		result, err := oauth(ctx, opts.HTTP, origin, "/oauth/token", url.Values{
			"client_id":   {clientID},
			"device_code": {deviceCode},
			"grant_type":  {deviceGrant},
		})
		if err != nil {
			var e *api.Error
			if isCode(err, &e, "cancelled") {
				return nil, err
			}
			wait *= 2
			if wait > maxBackoff {
				wait = maxBackoff
			}
			continue
		}
		if !result.ok {
			switch result.str("error") {
			case "authorization_pending":
				continue
			case "slow_down":
				wait += 5 * time.Second
				continue
			}
			msg := result.str("error_description")
			if msg == "" {
				msg = "Client approval failed. Start login again."
			}
			return nil, api.Errorf("login", "%s", msg)
		}
		token := result.str("access_token")
		if !IsAPIToken(token) || result.str("token_type") != "Bearer" {
			return nil, api.Errorf("login", "Client login returned an invalid API token.")
		}
		cred := Credential{Origin: origin, APIToken: token, APITokenID: result.str("api_token_id"), VaultID: result.str("vault_id")}
		if err := store.Save(opts.Profile, cred); err != nil {
			return nil, api.Errorf("storage", "The API token was issued but could not be saved. Revoke this client (%s) in your vault before starting login again.", opts.Name)
		}
		return &LoginResult{Profile: opts.Profile, VaultID: cred.VaultID, APITokenID: cred.APITokenID}, nil
	}
	return nil, api.Errorf("login", "Client approval expired. Run agentdrop login again.")
}

func isCode(err error, target **api.Error, code string) bool {
	e, ok := err.(*api.Error)
	if !ok {
		return false
	}
	*target = e
	return e.Code == code
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
