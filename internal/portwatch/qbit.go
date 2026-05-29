package portwatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
)

type QbitClient struct {
	baseURL    string
	httpClient *http.Client
	auth       QbitAuth
	loginMu    sync.Mutex
	loggedIn   bool
}

func NewQbitClient(baseURL string, httpClient *http.Client, auth QbitAuth) *QbitClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if auth.APIKey == "" && auth.Username != "" && auth.Password != "" {
		cloned := *httpClient
		jar, _ := cookiejar.New(nil)
		cloned.Jar = jar
		httpClient = &cloned
	}

	return &QbitClient{
		baseURL:    baseURL,
		httpClient: httpClient,
		auth:       auth,
	}
}

func (c *QbitClient) GetListenPort(ctx context.Context) (int, error) {
	prefs, err := c.GetPreferences(ctx)
	if err != nil {
		return 0, err
	}
	return prefs.ListenPort, nil
}

func (c *QbitClient) GetPreferences(ctx context.Context) (QbitPreferences, error) {
	var response struct {
		ListenPort              *int   `json:"listen_port"`
		CurrentNetworkInterface string `json:"current_network_interface"`
		RandomPort              bool   `json:"random_port"`
		UPnP                    bool   `json:"upnp"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v2/app/preferences", nil, "", &response); err != nil {
		return QbitPreferences{}, err
	}
	if response.ListenPort == nil {
		return QbitPreferences{}, fmt.Errorf("qBittorrent API %s %s missing listen_port", http.MethodGet, "/api/v2/app/preferences")
	}
	if !ValidPort(*response.ListenPort) {
		return QbitPreferences{}, fmt.Errorf("qBittorrent API %s %s invalid listen_port %d", http.MethodGet, "/api/v2/app/preferences", *response.ListenPort)
	}

	return QbitPreferences{
		ListenPort:              *response.ListenPort,
		CurrentNetworkInterface: response.CurrentNetworkInterface,
		RandomPort:              response.RandomPort,
		UPnP:                    response.UPnP,
	}, nil
}

func (c *QbitClient) SetListenPort(ctx context.Context, port int, iface string) error {
	if !ValidPort(port) {
		return fmt.Errorf("invalid qBittorrent listen port %d", port)
	}
	if iface == "" {
		return fmt.Errorf("qBittorrent network interface cannot be empty")
	}

	prefs, err := json.Marshal(struct {
		CurrentNetworkInterface string `json:"current_network_interface"`
		ListenPort              int    `json:"listen_port"`
		RandomPort              bool   `json:"random_port"`
		UPnP                    bool   `json:"upnp"`
	}{
		CurrentNetworkInterface: iface,
		ListenPort:              port,
		RandomPort:              false,
		UPnP:                    false,
	})
	if err != nil {
		return err
	}

	form := url.Values{}
	form.Set("json", string(prefs))
	return c.do(ctx, http.MethodPost, "/api/v2/app/setPreferences", strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", nil)
}

func (c *QbitClient) do(ctx context.Context, method, path string, body io.Reader, contentType string, out any) error {
	bodyBytes := []byte{}
	if body != nil {
		var err error
		bodyBytes, err = io.ReadAll(body)
		if err != nil {
			return fmt.Errorf("qBittorrent API %s %s read request body: %w", method, path, err)
		}
	}

	if c.usesCookieAuth() {
		if err := c.ensureLoggedIn(ctx); err != nil {
			return err
		}
	}

	resp, err := c.doOnce(ctx, method, path, bodyBytes, contentType)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if c.usesCookieAuth() && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err := c.relogin(ctx); err != nil {
			return err
		}
		resp, err = c.doOnce(ctx, method, path, bodyBytes, contentType)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySnippetBytes))
		return fmt.Errorf("qBittorrent API %s %s returned status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("qBittorrent API %s %s decode response: %w", method, path, err)
	}

	return nil
}

func (c *QbitClient) ensureLoggedIn(ctx context.Context) error {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	if c.loggedIn {
		return nil
	}
	return c.loginLocked(ctx)
}

func (c *QbitClient) relogin(ctx context.Context) error {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	c.loggedIn = false
	return c.loginLocked(ctx)
}

func (c *QbitClient) doOnce(ctx context.Context, method, path string, body []byte, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("qBittorrent API %s %s create request: %w", method, path, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.auth.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.auth.APIKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qBittorrent API %s %s request failed: %w", method, path, err)
	}
	return resp, nil
}

func (c *QbitClient) loginLocked(ctx context.Context) error {
	form := url.Values{}
	form.Set("username", c.auth.Username)
	form.Set("password", c.auth.Password)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("qBittorrent API %s %s create request: %w", http.MethodPost, "/api/v2/auth/login", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("qBittorrent API %s %s request failed: %w", http.MethodPost, "/api/v2/auth/login", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySnippetBytes))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("qBittorrent API %s %s authentication failed with status %d", http.MethodPost, "/api/v2/auth/login", resp.StatusCode)
	}
	if strings.TrimSpace(string(body)) != "Ok." {
		return fmt.Errorf("qBittorrent API %s %s authentication failed", http.MethodPost, "/api/v2/auth/login")
	}
	c.loggedIn = true
	return nil
}

func (c *QbitClient) usesCookieAuth() bool {
	return c.auth.APIKey == "" && c.auth.Username != "" && c.auth.Password != ""
}
