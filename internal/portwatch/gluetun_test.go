package portwatch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type errorRoundTripper struct{}

func (errorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport failed")
}

func TestGluetunGetForwardedPort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("Method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/v1/portforward" {
			t.Fatalf("Path = %q, want %q", r.URL.Path, "/v1/portforward")
		}
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Fatalf("X-API-Key = %q, want %q", r.Header.Get("X-API-Key"), "test-key")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"port":43532}`))
	}))
	t.Cleanup(server.Close)

	client := NewGluetunClient(server.URL, "test-key", server.Client())
	port, err := client.GetForwardedPort(context.Background())
	if err != nil {
		t.Fatalf("GetForwardedPort returned error: %v", err)
	}
	if port != 43532 {
		t.Fatalf("port = %d, want %d", port, 43532)
	}
}

func TestGluetunGetForwardedPortAllowsZero(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"port":0}`))
	}))
	t.Cleanup(server.Close)

	client := NewGluetunClient(server.URL, "test-key", server.Client())
	port, err := client.GetForwardedPort(context.Background())
	if err != nil {
		t.Fatalf("GetForwardedPort returned error: %v", err)
	}
	if port != 0 {
		t.Fatalf("port = %d, want 0", port)
	}
}

func TestGluetunGetForwardedPortStatusMissingPortReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	client := NewGluetunClient(server.URL, "test-key", server.Client())
	status, err := client.GetForwardedPortStatus(context.Background())
	if err != nil {
		t.Fatalf("GetForwardedPortStatus returned error: %v", err)
	}
	if status.Port != 0 || status.Reason != "missing_port" {
		t.Fatalf("status = %+v, want port 0 reason missing_port", status)
	}
}

func TestGluetunGetForwardedPortMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"port":`))
	}))
	t.Cleanup(server.Close)

	client := NewGluetunClient(server.URL, "test-key", server.Client())
	_, err := client.GetForwardedPort(context.Background())
	if err == nil {
		t.Fatal("GetForwardedPort returned nil error, want error")
	}
}

func TestGluetunMalformedJSONIncludesMethodPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"port":`))
	}))
	t.Cleanup(server.Close)

	client := NewGluetunClient(server.URL, "test-key", server.Client())
	_, err := client.GetForwardedPort(context.Background())
	if err == nil {
		t.Fatal("GetForwardedPort returned nil error, want error")
	}

	wantParts := []string{http.MethodGet, "/v1/portforward"}
	for _, part := range wantParts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), part)
		}
	}
}

func TestGluetunTransportErrorIncludesMethodPath(t *testing.T) {
	client := NewGluetunClient("http://example.invalid", "test-key", &http.Client{
		Transport: errorRoundTripper{},
	})
	_, err := client.GetForwardedPort(context.Background())
	if err == nil {
		t.Fatal("GetForwardedPort returned nil error, want error")
	}

	wantParts := []string{http.MethodGet, "/v1/portforward"}
	for _, part := range wantParts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), part)
		}
	}
}

func TestGluetunRequestErrorIncludesMethodPath(t *testing.T) {
	client := NewGluetunClient("http://%", "test-key", http.DefaultClient)
	_, err := client.GetForwardedPort(context.Background())
	if err == nil {
		t.Fatal("GetForwardedPort returned nil error, want error")
	}

	wantParts := []string{http.MethodGet, "/v1/portforward"}
	for _, part := range wantParts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), part)
		}
	}
}

func TestGluetunNon2xxIncludesStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusTeapot)
	}))
	t.Cleanup(server.Close)

	client := NewGluetunClient(server.URL, "test-key", server.Client())
	_, err := client.GetForwardedPort(context.Background())
	if err == nil {
		t.Fatal("GetForwardedPort returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "418") {
		t.Fatalf("error = %q, want status code 418", err.Error())
	}
}

func TestGluetunNon2xxIncludesMethodPathStatusAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gluetun exploded", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	client := NewGluetunClient(server.URL, "test-key", server.Client())
	_, err := client.GetForwardedPort(context.Background())
	if err == nil {
		t.Fatal("GetForwardedPort returned nil error, want error")
	}

	wantParts := []string{http.MethodGet, "/v1/portforward", "500", "gluetun exploded"}
	for _, part := range wantParts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), part)
		}
	}
}

func TestGluetunGetVPNStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("Method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/v1/vpn/status" {
			t.Fatalf("Path = %q, want %q", r.URL.Path, "/v1/vpn/status")
		}
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Fatalf("X-API-Key = %q, want %q", r.Header.Get("X-API-Key"), "test-key")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"running"}`))
	}))
	t.Cleanup(server.Close)

	client := NewGluetunClient(server.URL, "test-key", server.Client())
	status, err := client.GetVPNStatus(context.Background())
	if err != nil {
		t.Fatalf("GetVPNStatus returned error: %v", err)
	}
	if status != "running" {
		t.Fatalf("status = %q, want %q", status, "running")
	}
}

func TestGluetunSetVPNStatus(t *testing.T) {
	wantStatuses := []string{"stopped", "running"}
	var gotStatuses []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("Method = %q, want %q", r.Method, http.MethodPut)
		}
		if r.URL.Path != "/v1/vpn/status" {
			t.Fatalf("Path = %q, want %q", r.URL.Path, "/v1/vpn/status")
		}
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Fatalf("X-API-Key = %q, want %q", r.Header.Get("X-API-Key"), "test-key")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type = %q, want %q", r.Header.Get("Content-Type"), "application/json")
		}

		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		gotStatuses = append(gotStatuses, body.Status)
	}))
	t.Cleanup(server.Close)

	client := NewGluetunClient(server.URL, "test-key", server.Client())
	for _, status := range wantStatuses {
		if err := client.SetVPNStatus(context.Background(), status); err != nil {
			t.Fatalf("SetVPNStatus(%q) returned error: %v", status, err)
		}
	}

	if len(gotStatuses) != len(wantStatuses) {
		t.Fatalf("got %d calls, want %d", len(gotStatuses), len(wantStatuses))
	}
	for i := range wantStatuses {
		if gotStatuses[i] != wantStatuses[i] {
			t.Fatalf("request %d status = %q, want %q", i, gotStatuses[i], wantStatuses[i])
		}
	}
}

func TestGluetunSetVPNStatusRejectsInvalidStatus(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
	}))
	t.Cleanup(server.Close)

	client := NewGluetunClient(server.URL, "test-key", server.Client())
	err := client.SetVPNStatus(context.Background(), "runing")
	if err == nil {
		t.Fatal("SetVPNStatus returned nil error, want error")
	}
	if requestCount != 0 {
		t.Fatalf("requestCount = %d, want 0", requestCount)
	}
}

func TestNewGluetunClientTrimsBaseURLSlash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/portforward" {
			t.Fatalf("Path = %q, want %q", r.URL.Path, "/v1/portforward")
		}
		_, _ = w.Write([]byte(`{"port":43532}`))
	}))
	t.Cleanup(server.Close)

	client := NewGluetunClient(server.URL+"/", "test-key", server.Client())
	_, err := client.GetForwardedPort(context.Background())
	if err != nil {
		t.Fatalf("GetForwardedPort returned error: %v", err)
	}
}

func TestNewGluetunClientDefaultsNilHTTPClient(t *testing.T) {
	client := NewGluetunClient("http://example.invalid", "secret", nil)
	if client == nil {
		t.Fatal("NewGluetunClient returned nil, want client")
	}
	if client.httpClient != http.DefaultClient {
		t.Fatalf("httpClient = %p, want http.DefaultClient %p", client.httpClient, http.DefaultClient)
	}
}
