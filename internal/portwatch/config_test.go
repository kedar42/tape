package portwatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigDefaultsAndEnv(t *testing.T) {
	t.Setenv("GLUETUN_URL", " http://gluetun-private:8000/ ")
	t.Setenv("QBIT_URL", " http://gluetun-private:8080/ ")
	t.Setenv("GLUETUN_API_KEY_FILE", "/tmp/key")
	t.Setenv("QBIT_API_KEY_FILE", "/tmp/qbit-key")

	cfg, err := LoadConfig([]string{})
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
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
	if cfg.QbitAPIKeyFile != "/tmp/qbit-key" {
		t.Fatalf("QbitAPIKeyFile = %q", cfg.QbitAPIKeyFile)
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
	t.Setenv("GLUETUN_URL", "http://gluetun-private:8000")
	t.Setenv("QBIT_URL", "http://gluetun-private:8080")
	t.Setenv("GLUETUN_API_KEY_FILE", "/tmp/key")

	cfg, err := LoadConfig([]string{
		"--gluetun-url", "http://flag-gluetun:8000",
		"--qbit-url", "http://flag-qbit:8080",
		"--gluetun-api-key-file", "/tmp/flag-key",
		"--qbit-api-key-file", "/tmp/flag-qbit-key",
		"--qbit-username", "flag-user",
		"--qbit-password-file", "/tmp/flag-qbit-password",
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

	if cfg.GluetunURL != "http://flag-gluetun:8000" {
		t.Fatalf("GluetunURL = %q", cfg.GluetunURL)
	}
	if cfg.QbitURL != "http://flag-qbit:8080" {
		t.Fatalf("QbitURL = %q", cfg.QbitURL)
	}
	if cfg.APIKeyFile != "/tmp/flag-key" {
		t.Fatalf("APIKeyFile = %q", cfg.APIKeyFile)
	}
	if cfg.QbitAPIKeyFile != "/tmp/flag-qbit-key" {
		t.Fatalf("QbitAPIKeyFile = %q", cfg.QbitAPIKeyFile)
	}
	if cfg.QbitUsername != "flag-user" {
		t.Fatalf("QbitUsername = %q", cfg.QbitUsername)
	}
	if cfg.QbitPasswordFile != "/tmp/flag-qbit-password" {
		t.Fatalf("QbitPasswordFile = %q", cfg.QbitPasswordFile)
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

func TestLoadConfigValidatesURLsAndInterface(t *testing.T) {
	t.Setenv("GLUETUN_URL", "ftp://gluetun-private:8000")
	t.Setenv("QBIT_URL", "http://gluetun-private:8080")
	t.Setenv("GLUETUN_API_KEY_FILE", "/tmp/key")
	if _, err := LoadConfig([]string{}); err == nil {
		t.Fatal("LoadConfig with unsupported gluetun URL scheme returned nil error, want error")
	}

	t.Setenv("GLUETUN_URL", "http://gluetun-private:8000")
	if _, err := LoadConfig([]string{"--qbit-url", "http:///missing-host"}); err == nil {
		t.Fatal("LoadConfig with missing qbit URL host returned nil error, want error")
	}

	if _, err := LoadConfig([]string{"--qbit-interface", "  "}); err == nil {
		t.Fatal("LoadConfig with blank qbit interface returned nil error, want error")
	}
}

func TestLoadConfigQbitAuthModes(t *testing.T) {
	t.Setenv("GLUETUN_URL", "http://gluetun-private:8000")
	t.Setenv("QBIT_URL", "http://gluetun-private:8080")
	t.Setenv("GLUETUN_API_KEY_FILE", "/tmp/key")

	if _, err := LoadConfig([]string{"--qbit-username", "user"}); err == nil {
		t.Fatal("LoadConfig with qbit username but no password file returned nil error, want error")
	}
	if _, err := LoadConfig([]string{"--qbit-password-file", "/tmp/qbit-password"}); err == nil {
		t.Fatal("LoadConfig with qbit password file but no username returned nil error, want error")
	}

	cfg, err := LoadConfig([]string{"--qbit-username", "user", "--qbit-password-file", "/tmp/qbit-password"})
	if err != nil {
		t.Fatalf("LoadConfig with qbit username/password returned error: %v", err)
	}
	if cfg.QbitUsername != "user" || cfg.QbitPasswordFile != "/tmp/qbit-password" {
		t.Fatalf("qbit username/password = %q/%q", cfg.QbitUsername, cfg.QbitPasswordFile)
	}

	cfg, err = LoadConfig([]string{"--qbit-api-key-file", "/tmp/qbit-api-key", "--qbit-username", "user"})
	if err != nil {
		t.Fatalf("LoadConfig with qbit api key and partial username/password returned error: %v", err)
	}
	if cfg.QbitAPIKeyFile != "/tmp/qbit-api-key" || cfg.QbitUsername != "user" {
		t.Fatalf("qbit api key/username = %q/%q", cfg.QbitAPIKeyFile, cfg.QbitUsername)
	}
}

func TestLoadConfigRejectsNonPositiveValues(t *testing.T) {
	t.Setenv("GLUETUN_URL", "http://gluetun-private:8000")
	t.Setenv("QBIT_URL", "http://gluetun-private:8080")
	t.Setenv("GLUETUN_API_KEY_FILE", "/tmp/key")

	tests := []struct {
		name string
		args []string
	}{
		{name: "failures", args: []string{"--failures", "0"}},
		{name: "interval", args: []string{"--interval", "0s"}},
		{name: "cooldown", args: []string{"--cooldown", "0s"}},
		{name: "http timeout", args: []string{"--http-timeout", "0s"}},
		{name: "qbit audit interval", args: []string{"--qbit-audit-interval", "0s"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := LoadConfig(tt.args); err == nil {
				t.Fatal("LoadConfig returned nil error, want error")
			}
		})
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

	rawWithEqualsPath := filepath.Join(t.TempDir(), "raw-equals-key")
	if err := os.WriteFile(rawWithEqualsPath, []byte("abc123==\n"), 0600); err != nil {
		t.Fatalf("write raw key with equals: %v", err)
	}

	key, err = ReadAPIKey(rawWithEqualsPath)
	if err != nil {
		t.Fatalf("ReadAPIKey raw key with equals returned error: %v", err)
	}
	if key != "abc123==" {
		t.Fatalf("raw key with equals = %q, want %q", key, "abc123==")
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

	multiplePath := filepath.Join(t.TempDir(), "multiple.toml")
	multiple := "[[roles]]\nname = \"one\"\napikey = \"first\"\n[[roles]]\nname = \"two\"\napikey = \"second\"\n"
	if err := os.WriteFile(multiplePath, []byte(multiple), 0600); err != nil {
		t.Fatalf("write multiple toml keys: %v", err)
	}
	if _, err := ReadAPIKey(multiplePath); err == nil {
		t.Fatal("ReadAPIKey multiple toml keys returned nil error, want error")
	}
}
