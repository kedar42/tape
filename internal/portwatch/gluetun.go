package portwatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxErrorBodySnippetBytes = 1024

type GluetunClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewGluetunClient(baseURL, apiKey string, httpClient *http.Client) *GluetunClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &GluetunClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: httpClient,
	}
}

func (c *GluetunClient) GetForwardedPort(ctx context.Context) (int, error) {
	status, err := c.GetForwardedPortStatus(ctx)
	if err != nil {
		return 0, err
	}
	return status.Port, nil
}

func (c *GluetunClient) GetForwardedPortStatus(ctx context.Context) (PortStatus, error) {
	var response struct {
		Port *int `json:"port"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/v1/portforward", nil, &response); err != nil {
		return PortStatus{}, err
	}
	if response.Port == nil {
		return PortStatus{Reason: "missing_port"}, nil
	}
	if *response.Port == 0 {
		return PortStatus{Port: 0, Reason: "zero_port"}, nil
	}

	return PortStatus{Port: *response.Port}, nil
}

func (c *GluetunClient) GetVPNStatus(ctx context.Context) (string, error) {
	var response struct {
		Status string `json:"status"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/v1/vpn/status", nil, &response); err != nil {
		return "", err
	}

	return response.Status, nil
}

func (c *GluetunClient) SetVPNStatus(ctx context.Context, status string) error {
	if status != "stopped" && status != "running" {
		return fmt.Errorf("invalid VPN status %q", status)
	}

	body, err := json.Marshal(struct {
		Status string `json:"status"`
	}{Status: status})
	if err != nil {
		return err
	}

	return c.doJSON(ctx, http.MethodPut, "/v1/vpn/status", body, nil)
}

func (c *GluetunClient) doJSON(ctx context.Context, method, path string, body []byte, out any) error {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("gluetun API %s %s request creation failed: %w", method, path, err)
	}
	req.Header.Set("X-API-Key", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gluetun API %s %s request failed: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySnippetBytes))
		return fmt.Errorf("gluetun API %s %s returned status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("gluetun API %s %s decode failed: %w", method, path, err)
	}

	return nil
}
