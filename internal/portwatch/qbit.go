package portwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type QbitClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewQbitClient(baseURL string, httpClient *http.Client) *QbitClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &QbitClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
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
	if body == nil {
		body = strings.NewReader("")
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("qBittorrent API %s %s create request: %w", method, path, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("qBittorrent API %s %s request failed: %w", method, path, err)
	}
	defer resp.Body.Close()

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
