package portwatch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQbitGetListenPort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("Method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/api/v2/app/preferences" {
			t.Fatalf("Path = %q, want %q", r.URL.Path, "/api/v2/app/preferences")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"listen_port":43532}`))
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client())
	port, err := client.GetListenPort(context.Background())
	if err != nil {
		t.Fatalf("GetListenPort returned error: %v", err)
	}
	if port != 43532 {
		t.Fatalf("port = %d, want %d", port, 43532)
	}
}

func TestQbitGetListenPortRejectsMissingPort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client())
	_, err := client.GetListenPort(context.Background())
	if err == nil {
		t.Fatal("GetListenPort returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "listen_port") {
		t.Fatalf("error = %q, want it to contain listen_port", err.Error())
	}
}

func TestQbitGetListenPortRejectsInvalidPort(t *testing.T) {
	tests := []int{0, -1, 65536}
	for _, port := range tests {
		t.Run(fmt.Sprintf("port_%d", port), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"listen_port":%d}`, port)
			}))
			t.Cleanup(server.Close)

			client := NewQbitClient(server.URL, server.Client())
			_, err := client.GetListenPort(context.Background())
			if err == nil {
				t.Fatal("GetListenPort returned nil error, want error")
			}
		})
	}
}

func TestQbitMalformedJSONIncludesMethodPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"listen_port":`))
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client())
	_, err := client.GetListenPort(context.Background())
	if err == nil {
		t.Fatal("GetListenPort returned nil error, want error")
	}

	wantParts := []string{http.MethodGet, "/api/v2/app/preferences"}
	for _, part := range wantParts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), part)
		}
	}
}

func TestQbitSetListenPort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("Method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/api/v2/app/setPreferences" {
			t.Fatalf("Path = %q, want %q", r.URL.Path, "/api/v2/app/setPreferences")
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("Content-Type = %q, want %q", got, "application/x-www-form-urlencoded")
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}

		wantJSON := `{"current_network_interface":"tun0","listen_port":43532,"random_port":false,"upnp":false}`
		if got := r.PostForm.Get("json"); got != wantJSON {
			t.Fatalf("json form field = %q, want %q", got, wantJSON)
		}
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client())
	if err := client.SetListenPort(context.Background(), 43532, "tun0"); err != nil {
		t.Fatalf("SetListenPort returned error: %v", err)
	}
}

func TestQbitSetListenPortRejectsInvalidPort(t *testing.T) {
	tests := []int{0, -1, 65536}
	for _, port := range tests {
		t.Run(fmt.Sprintf("port_%d", port), func(t *testing.T) {
			requestCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++
			}))
			t.Cleanup(server.Close)

			client := NewQbitClient(server.URL, server.Client())
			err := client.SetListenPort(context.Background(), port, "tun0")
			if err == nil {
				t.Fatal("SetListenPort returned nil error, want error")
			}
			if requestCount != 0 {
				t.Fatalf("requestCount = %d, want 0", requestCount)
			}
		})
	}
}

func TestQbitSetListenPortRejectsEmptyInterface(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client())
	err := client.SetListenPort(context.Background(), 43532, "")
	if err == nil {
		t.Fatal("SetListenPort returned nil error, want error")
	}
	if requestCount != 0 {
		t.Fatalf("requestCount = %d, want 0", requestCount)
	}
}

func TestQbitNon2xxIncludesMethodPathStatusAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Forbidden", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client())
	_, err := client.GetListenPort(context.Background())
	if err == nil {
		t.Fatal("GetListenPort returned nil error, want error")
	}

	wantParts := []string{http.MethodGet, "/api/v2/app/preferences", "403", "Forbidden"}
	for _, part := range wantParts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), part)
		}
	}
}

func TestQbitNon2xxCoversUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client())
	_, err := client.GetListenPort(context.Background())
	if err == nil {
		t.Fatal("GetListenPort returned nil error, want error")
	}

	wantParts := []string{http.MethodGet, "/api/v2/app/preferences", "401", "Unauthorized"}
	for _, part := range wantParts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error = %q, want it to contain %q", err.Error(), part)
		}
	}
}

func TestNewQbitClientDefaultsHTTPClient(t *testing.T) {
	client := NewQbitClient("http://example.invalid", nil)
	if client == nil {
		t.Fatal("NewQbitClient returned nil, want client")
	}
	if client.httpClient != http.DefaultClient {
		t.Fatalf("httpClient = %p, want http.DefaultClient %p", client.httpClient, http.DefaultClient)
	}
}

func TestNewQbitClientTrimsBaseURLSlash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/app/preferences" {
			t.Fatalf("Path = %q, want %q", r.URL.Path, "/api/v2/app/preferences")
		}
		_, _ = w.Write([]byte(`{"listen_port":43532}`))
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL+"/", server.Client())
	_, err := client.GetListenPort(context.Background())
	if err != nil {
		t.Fatalf("GetListenPort returned error: %v", err)
	}
}
