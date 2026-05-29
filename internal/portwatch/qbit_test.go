package portwatch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestQbitBypassModeSendsNoAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/app/preferences" {
			t.Fatalf("Path = %q, want %q", r.URL.Path, "/api/v2/app/preferences")
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization = %q, want empty", got)
		}
		if cookie := r.Header.Get("Cookie"); cookie != "" {
			t.Fatalf("Cookie = %q, want empty", cookie)
		}
		_, _ = w.Write([]byte(`{"listen_port":43532}`))
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{})
	if _, err := client.GetListenPort(context.Background()); err != nil {
		t.Fatalf("GetListenPort returned error: %v", err)
	}
}

func TestQbitAPIKeyAuthSendsBearerToken(t *testing.T) {
	loginCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/auth/login" {
			loginCount++
			t.Fatal("login endpoint called in API key mode")
		}
		if got := r.Header.Get("Authorization"); got != "Bearer qbit-key" {
			t.Fatalf("Authorization = %q, want Bearer qbit-key", got)
		}
		_, _ = w.Write([]byte(`{"listen_port":43532}`))
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{APIKey: "qbit-key"})
	if _, err := client.GetListenPort(context.Background()); err != nil {
		t.Fatalf("GetListenPort returned error: %v", err)
	}
	if loginCount != 0 {
		t.Fatalf("loginCount = %d, want 0", loginCount)
	}
}

func TestQbitCookieAuthLogsInAndReusesCookie(t *testing.T) {
	loginCount := 0
	protectedCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			loginCount++
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm: %v", err)
			}
			if r.PostForm.Get("username") != "admin" || r.PostForm.Get("password") != "secret" {
				t.Fatalf("login credentials = %q/%q, want admin/secret", r.PostForm.Get("username"), r.PostForm.Get("password"))
			}
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "cookie-session", Path: "/"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/preferences":
			protectedCount++
			cookie, err := r.Cookie("SID")
			if err != nil || cookie.Value != "cookie-session" {
				t.Fatalf("SID cookie = %v/%v, want cookie-session", cookie, err)
			}
			_, _ = w.Write([]byte(`{"listen_port":43532}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{Username: "admin", Password: "secret"})
	for i := 0; i < 2; i++ {
		if _, err := client.GetListenPort(context.Background()); err != nil {
			t.Fatalf("GetListenPort #%d returned error: %v", i+1, err)
		}
	}
	if loginCount != 1 {
		t.Fatalf("loginCount = %d, want 1", loginCount)
	}
	if protectedCount != 2 {
		t.Fatalf("protectedCount = %d, want 2", protectedCount)
	}
}

func TestQbitCookieAuthSendsOriginForCSRFProtection(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Origin"); got != server.URL {
			t.Fatalf("%s Origin = %q, want %q", r.URL.Path, got, server.URL)
		}

		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "cookie-session", Path: "/"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/setPreferences":
			if _, err := r.Cookie("SID"); err != nil {
				t.Fatalf("missing SID cookie: %v", err)
			}
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{Username: "admin", Password: "secret"})
	if err := client.SetListenPort(context.Background(), 43532, "tun0"); err != nil {
		t.Fatalf("SetListenPort returned error: %v", err)
	}
}

func TestQbitCookieAuthReloginsOnceAfterForbidden(t *testing.T) {
	loginCount := 0
	protectedCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			loginCount++
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: fmt.Sprintf("session-%d", loginCount), Path: "/"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/preferences":
			protectedCount++
			if protectedCount == 1 {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			cookie, err := r.Cookie("SID")
			if err != nil || cookie.Value != "session-2" {
				t.Fatalf("SID cookie = %v/%v, want session-2", cookie, err)
			}
			_, _ = w.Write([]byte(`{"listen_port":43532}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{Username: "admin", Password: "secret"})
	if _, err := client.GetListenPort(context.Background()); err != nil {
		t.Fatalf("GetListenPort returned error: %v", err)
	}
	if loginCount != 2 {
		t.Fatalf("loginCount = %d, want 2", loginCount)
	}
	if protectedCount != 2 {
		t.Fatalf("protectedCount = %d, want 2", protectedCount)
	}
}

func TestQbitCookieAuthCanRetrySetPreferencesBody(t *testing.T) {
	loginCount := 0
	protectedCount := 0
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			loginCount++
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: fmt.Sprintf("session-%d", loginCount), Path: "/"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/setPreferences":
			protectedCount++
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("ReadAll body: %v", err)
			}
			bodies = append(bodies, string(data))
			if protectedCount == 1 {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			if _, err := r.Cookie("SID"); err != nil {
				t.Fatalf("missing SID cookie on retry: %v", err)
			}
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{Username: "admin", Password: "secret"})
	if err := client.SetListenPort(context.Background(), 43532, "tun0"); err != nil {
		t.Fatalf("SetListenPort returned error: %v", err)
	}
	if loginCount != 2 {
		t.Fatalf("loginCount = %d, want 2", loginCount)
	}
	if protectedCount != 2 {
		t.Fatalf("protectedCount = %d, want 2", protectedCount)
	}
	if len(bodies) != 2 || bodies[0] == "" || bodies[0] != bodies[1] {
		t.Fatalf("bodies = %#v, want same non-empty body twice", bodies)
	}
}

func TestQbitCookieAuthDoesNotReuseSuppliedJar(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "qbit-session", Path: "/"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/preferences":
			cookie, err := r.Cookie("SID")
			if err != nil || cookie.Value != "qbit-session" {
				t.Fatalf("SID cookie = %v/%v, want qbit-session", cookie, err)
			}
			_, _ = w.Write([]byte(`{"listen_port":43532}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	sharedJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	sharedClient := server.Client()
	sharedClient.Jar = sharedJar

	client := NewQbitClient(server.URL, sharedClient, QbitAuth{Username: "admin", Password: "secret"})
	if _, err := client.GetListenPort(context.Background()); err != nil {
		t.Fatalf("GetListenPort returned error: %v", err)
	}
	if client.httpClient.Jar == sharedJar {
		t.Fatal("qBit client reused supplied cookie jar")
	}
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	if cookies := sharedJar.Cookies(serverURL); len(cookies) != 0 {
		t.Fatalf("shared jar cookies = %v, want empty", cookies)
	}
}

func TestQbitCookieAuthRejectsFailedLoginBody(t *testing.T) {
	protectedCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Fails."))
		case "/api/v2/app/preferences":
			protectedCount++
			_, _ = w.Write([]byte(`{"listen_port":43532}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{Username: "admin", Password: "secret"})
	_, err := client.GetListenPort(context.Background())
	if err == nil {
		t.Fatal("GetListenPort returned nil error, want auth error")
	}
	if protectedCount != 0 {
		t.Fatalf("protectedCount = %d, want 0", protectedCount)
	}
	for _, secret := range []string{"admin", "secret", "Fails."} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error = %q, want it not to contain %q", err.Error(), secret)
		}
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("error = %q, want authentication failed", err.Error())
	}
}

func TestQbitCookieAuthConcurrentRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "qbit-session", Path: "/"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/preferences":
			_, _ = w.Write([]byte(`{"listen_port":43532}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{Username: "admin", Password: "secret"})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.GetListenPort(context.Background()); err != nil {
				t.Errorf("GetListenPort returned error: %v", err)
			}
		}()
	}
	wg.Wait()
}

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

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{})
	port, err := client.GetListenPort(context.Background())
	if err != nil {
		t.Fatalf("GetListenPort returned error: %v", err)
	}
	if port != 43532 {
		t.Fatalf("port = %d, want %d", port, 43532)
	}
}

func TestQbitGetPreferencesParsesSafetyFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("Method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/api/v2/app/preferences" {
			t.Fatalf("Path = %q, want %q", r.URL.Path, "/api/v2/app/preferences")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"listen_port":43532,"current_network_interface":"tun0","random_port":false,"upnp":false}`))
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{})
	prefs, err := client.GetPreferences(context.Background())
	if err != nil {
		t.Fatalf("GetPreferences returned error: %v", err)
	}
	if prefs.ListenPort != 43532 {
		t.Fatalf("ListenPort = %d, want %d", prefs.ListenPort, 43532)
	}
	if prefs.CurrentNetworkInterface != "tun0" {
		t.Fatalf("CurrentNetworkInterface = %q, want tun0", prefs.CurrentNetworkInterface)
	}
	if prefs.RandomPort {
		t.Fatal("RandomPort = true, want false")
	}
	if prefs.UPnP {
		t.Fatal("UPnP = true, want false")
	}
}

func TestQbitGetListenPortRejectsMissingPort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{})
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

			client := NewQbitClient(server.URL, server.Client(), QbitAuth{})
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

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{})
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

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{})
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

			client := NewQbitClient(server.URL, server.Client(), QbitAuth{})
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

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{})
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

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{})
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

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{})
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
	client := NewQbitClient("http://example.invalid", nil, QbitAuth{})
	if client == nil {
		t.Fatal("NewQbitClient returned nil, want client")
	}
	if client.httpClient != http.DefaultClient {
		t.Fatalf("httpClient = %p, want http.DefaultClient %p", client.httpClient, http.DefaultClient)
	}
}

func TestNewQbitClientUsesBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/app/preferences" {
			t.Fatalf("Path = %q, want %q", r.URL.Path, "/api/v2/app/preferences")
		}
		_, _ = w.Write([]byte(`{"listen_port":43532}`))
	}))
	t.Cleanup(server.Close)

	client := NewQbitClient(server.URL, server.Client(), QbitAuth{})
	_, err := client.GetListenPort(context.Background())
	if err != nil {
		t.Fatalf("GetListenPort returned error: %v", err)
	}
}
