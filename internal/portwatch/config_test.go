package portwatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigDefaultsAndEnv(t *testing.T) {
	t.Setenv("PORTWATCH_NAME", "private")
	t.Setenv("GLUETUN_URL", "http://gluetun-private:8000")
	t.Setenv("QBIT_URL", "http://gluetun-private:8080")
	t.Setenv("GLUETUN_API_KEY_FILE", "/tmp/key")

	cfg, err := LoadConfig([]string{})
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.Name != "private" {
		t.Fatalf("Name = %q, want %q", cfg.Name, "private")
	}
	if cfg.GluetunURL != "http://gluetun-private:8000" {
		t.Fatalf("GluetunURL = %q", cfg.GluetunURL)
	}
	if cfg.QbitURL != "http://gluetun-private:8080" {
		t.Fatalf("QbitURL = %q", cfg.QbitURL)
	}
	if cfg.APIKeyFile != "/tmp/key" {
		t.Fatalf("APIKeyFile = %q", cfg.APIKeyFile)
	}
	if cfg.Interval != time.Minute {
		t.Fatalf("Interval = %s, want %s", cfg.Interval, time.Minute)
	}
	if cfg.Failures != 5 {
		t.Fatalf("Failures = %d, want %d", cfg.Failures, 5)
	}
	if cfg.Cooldown != 3*time.Minute {
		t.Fatalf("Cooldown = %s, want %s", cfg.Cooldown, 3*time.Minute)
	}
	if cfg.QbitAuditInterval != 30*time.Minute {
		t.Fatalf("QbitAuditInterval = %s, want %s", cfg.QbitAuditInterval, 30*time.Minute)
	}
	if cfg.QbitInterface != "tun0" {
		t.Fatalf("QbitInterface = %q, want %q", cfg.QbitInterface, "tun0")
	}
	if cfg.HTTPTimeout != 10*time.Second {
		t.Fatalf("HTTPTimeout = %s, want %s", cfg.HTTPTimeout, 10*time.Second)
	}
}

func TestLoadConfigFlagsOverrideEnv(t *testing.T) {
	t.Setenv("PORTWATCH_NAME", "private")
	t.Setenv("GLUETUN_URL", "http://gluetun-private:8000")
	t.Setenv("QBIT_URL", "http://gluetun-private:8080")
	t.Setenv("GLUETUN_API_KEY_FILE", "/tmp/key")

	cfg, err := LoadConfig([]string{
		"--name", "flag-name",
		"--gluetun-url", "http://flag-gluetun:8000",
		"--qbit-url", "http://flag-qbit:8080",
		"--gluetun-api-key-file", "/tmp/flag-key",
		"--interval", "2m",
		"--failures", "7",
		"--cooldown", "5m",
		"--qbit-audit-interval", "15m",
		"--http-timeout", "4s",
		"--qbit-interface", "wg0",
		"--once",
		"--dry-run",
	})
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.Name != "flag-name" {
		t.Fatalf("Name = %q", cfg.Name)
	}
	if cfg.GluetunURL != "http://flag-gluetun:8000" {
		t.Fatalf("GluetunURL = %q", cfg.GluetunURL)
	}
	if cfg.QbitURL != "http://flag-qbit:8080" {
		t.Fatalf("QbitURL = %q", cfg.QbitURL)
	}
	if cfg.APIKeyFile != "/tmp/flag-key" {
		t.Fatalf("APIKeyFile = %q", cfg.APIKeyFile)
	}
	if cfg.Interval != 2*time.Minute {
		t.Fatalf("Interval = %s", cfg.Interval)
	}
	if cfg.Failures != 7 {
		t.Fatalf("Failures = %d", cfg.Failures)
	}
	if cfg.Cooldown != 5*time.Minute {
		t.Fatalf("Cooldown = %s", cfg.Cooldown)
	}
	if cfg.QbitAuditInterval != 15*time.Minute {
		t.Fatalf("QbitAuditInterval = %s", cfg.QbitAuditInterval)
	}
	if cfg.HTTPTimeout != 4*time.Second {
		t.Fatalf("HTTPTimeout = %s", cfg.HTTPTimeout)
	}
	if cfg.QbitInterface != "wg0" {
		t.Fatalf("QbitInterface = %q", cfg.QbitInterface)
	}
	if !cfg.Once {
		t.Fatal("Once = false, want true")
	}
	if !cfg.DryRun {
		t.Fatal("DryRun = false, want true")
	}
}

func TestLoadConfigRequiresCoreValues(t *testing.T) {
	_, err := LoadConfig([]string{})
	if err == nil {
		t.Fatal("LoadConfig returned nil error, want error")
	}
}

func TestReadAPIKey(t *testing.T) {
	rawPath := filepath.Join(t.TempDir(), "raw-key")
	if err := os.WriteFile(rawPath, []byte("abc123\n"), 0600); err != nil {
		t.Fatalf("write raw key: %v", err)
	}

	key, err := ReadAPIKey(rawPath)
	if err != nil {
		t.Fatalf("ReadAPIKey raw returned error: %v", err)
	}
	if key != "abc123" {
		t.Fatalf("raw key = %q, want %q", key, "abc123")
	}

	rawWithAPIKeyPath := filepath.Join(t.TempDir(), "raw-apikey-token")
	if err := os.WriteFile(rawWithAPIKeyPath, []byte("my-apikey-token\n"), 0600); err != nil {
		t.Fatalf("write raw apikey token: %v", err)
	}

	key, err = ReadAPIKey(rawWithAPIKeyPath)
	if err != nil {
		t.Fatalf("ReadAPIKey raw apikey token returned error: %v", err)
	}
	if key != "my-apikey-token" {
		t.Fatalf("raw apikey token = %q, want %q", key, "my-apikey-token")
	}

	tomlPath := filepath.Join(t.TempDir(), "auth.toml")
	toml := "[[roles]]\nname = \"portwatch\"\napikey = \"tomlkey\"\n"
	if err := os.WriteFile(tomlPath, []byte(toml), 0600); err != nil {
		t.Fatalf("write toml key: %v", err)
	}

	key, err = ReadAPIKey(tomlPath)
	if err != nil {
		t.Fatalf("ReadAPIKey toml returned error: %v", err)
	}
	if key != "tomlkey" {
		t.Fatalf("toml key = %q, want %q", key, "tomlkey")
	}

	tomlWithCommentPath := filepath.Join(t.TempDir(), "auth-comment.toml")
	tomlWithComment := "[[roles]]\nname = \"portwatch\"\napikey = \"commentkey\" # trailing comment\n"
	if err := os.WriteFile(tomlWithCommentPath, []byte(tomlWithComment), 0600); err != nil {
		t.Fatalf("write commented toml key: %v", err)
	}

	key, err = ReadAPIKey(tomlWithCommentPath)
	if err != nil {
		t.Fatalf("ReadAPIKey commented toml returned error: %v", err)
	}
	if key != "commentkey" {
		t.Fatalf("commented toml key = %q, want %q", key, "commentkey")
	}

	emptyPath := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(emptyPath, nil, 0600); err != nil {
		t.Fatalf("write empty key: %v", err)
	}
	if _, err := ReadAPIKey(emptyPath); err == nil {
		t.Fatal("ReadAPIKey empty returned nil error, want error")
	}

	malformedPath := filepath.Join(t.TempDir(), "malformed.toml")
	if err := os.WriteFile(malformedPath, []byte("apikey =\n"), 0600); err != nil {
		t.Fatalf("write malformed toml key: %v", err)
	}
	if _, err := ReadAPIKey(malformedPath); err == nil {
		t.Fatal("ReadAPIKey malformed toml returned nil error, want error")
	}
}
