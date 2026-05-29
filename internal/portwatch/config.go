package portwatch

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/alecthomas/kong"
)

type configCLI struct {
	GluetunURL        string        `name:"gluetun-url" env:"GLUETUN_URL" help:"Gluetun control server URL"`
	QbitURL           string        `name:"qbit-url" env:"QBIT_URL" help:"qBittorrent web UI URL"`
	APIKeyFile        string        `name:"gluetun-api-key-file" env:"GLUETUN_API_KEY_FILE" help:"Gluetun API key file"`
	QbitAPIKeyFile    string        `name:"qbit-api-key-file" env:"QBIT_API_KEY_FILE" help:"qBittorrent API key file"`
	QbitUsername      string        `name:"qbit-username" env:"QBIT_USERNAME" help:"qBittorrent username"`
	QbitPasswordFile  string        `name:"qbit-password-file" env:"QBIT_PASSWORD_FILE" help:"qBittorrent password file"`
	Interval          time.Duration `name:"interval" env:"PORTWATCH_INTERVAL" default:"1m" help:"poll interval"`
	Failures          int           `name:"failures" env:"PORTWATCH_FAILURES" default:"5" help:"failure threshold"`
	Cooldown          time.Duration `name:"cooldown" env:"PORTWATCH_COOLDOWN" default:"3m" help:"failure cooldown"`
	HTTPTimeout       time.Duration `name:"http-timeout" env:"PORTWATCH_HTTP_TIMEOUT" default:"10s" help:"HTTP timeout"`
	QbitAuditInterval time.Duration `name:"qbit-audit-interval" env:"PORTWATCH_QBIT_AUDIT_INTERVAL" default:"30m" help:"qBittorrent audit interval"`
	RecoveryInterval  time.Duration `name:"recovery-interval" env:"PORTWATCH_RECOVERY_INTERVAL" default:"10s" help:"recovery poll interval after a missing port"`
	RecoveryDuration  time.Duration `name:"recovery-duration" env:"PORTWATCH_RECOVERY_DURATION" default:"3m" help:"recovery poll duration after a missing port"`
	QbitInterface     string        `name:"qbit-interface" env:"QBIT_INTERFACE" default:"tun0" help:"qBittorrent network interface"`
	Once              bool          `name:"once" help:"run once and exit"`
	DryRun            bool          `name:"dry-run" help:"log actions without changing state"`
}

func LoadConfig(args []string) (Config, error) {
	var cli configCLI
	parser, err := kong.New(&cli,
		kong.Name("gluetun-portwatch"),
		kong.Exit(func(int) {}),
	)
	if err != nil {
		return Config{}, err
	}
	if _, err := parser.Parse(args); err != nil {
		return Config{}, err
	}

	cfg := Config{
		GluetunURL:        cli.GluetunURL,
		QbitURL:           cli.QbitURL,
		APIKeyFile:        cli.APIKeyFile,
		QbitAPIKeyFile:    cli.QbitAPIKeyFile,
		QbitUsername:      cli.QbitUsername,
		QbitPasswordFile:  cli.QbitPasswordFile,
		Interval:          cli.Interval,
		Failures:          cli.Failures,
		Cooldown:          cli.Cooldown,
		HTTPTimeout:       cli.HTTPTimeout,
		QbitAuditInterval: cli.QbitAuditInterval,
		RecoveryInterval:  cli.RecoveryInterval,
		RecoveryDuration:  cli.RecoveryDuration,
		QbitInterface:     cli.QbitInterface,
		Once:              cli.Once,
		DryRun:            cli.DryRun,
	}
	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}
	cfg.GluetunURL = normalizeURL(cfg.GluetunURL)
	cfg.QbitURL = normalizeURL(cfg.QbitURL)
	cfg.QbitInterface = strings.TrimSpace(cfg.QbitInterface)

	return cfg, nil
}

func ReadAPIKey(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", errors.New("api key file is empty")
	}
	if !looksLikeAPIKeyTOML(content) {
		return content, nil
	}

	var decoded map[string]any
	if _, err := toml.Decode(content, &decoded); err != nil {
		return "", errors.New("api key file is not valid TOML")
	}
	keys := collectAPIKeys(decoded)
	if len(keys) != 1 {
		return "", fmt.Errorf("api key file must contain exactly one apikey, found %d", len(keys))
	}
	key := strings.TrimSpace(keys[0])
	if key == "" {
		return "", errors.New("api key file contains an empty apikey")
	}
	return key, nil
}

func looksLikeAPIKeyTOML(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "[[roles]]" || line == "[roles]" {
			return true
		}
		if strings.HasPrefix(line, "apikey") && strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(line, "apikey")), "=") {
			return true
		}
	}
	return false
}

func collectAPIKeys(value any) []string {
	var keys []string
	switch typed := value.(type) {
	case map[string]any:
		for k, v := range typed {
			if strings.EqualFold(k, "apikey") {
				if key, ok := v.(string); ok {
					keys = append(keys, key)
				}
				continue
			}
			keys = append(keys, collectAPIKeys(v)...)
		}
	case []map[string]any:
		for _, item := range typed {
			keys = append(keys, collectAPIKeys(item)...)
		}
	case []any:
		for _, item := range typed {
			keys = append(keys, collectAPIKeys(item)...)
		}
	}
	return keys
}

func validateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.GluetunURL) == "" {
		return errors.New("gluetun URL is required")
	}
	if err := validateHTTPURL("gluetun URL", cfg.GluetunURL); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.QbitURL) == "" {
		return errors.New("qbit URL is required")
	}
	if err := validateHTTPURL("qbit URL", cfg.QbitURL); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.APIKeyFile) == "" {
		return errors.New("gluetun API key file is required")
	}
	if strings.TrimSpace(cfg.QbitInterface) == "" {
		return errors.New("qbit interface is required")
	}
	if strings.TrimSpace(cfg.QbitAPIKeyFile) == "" && (strings.TrimSpace(cfg.QbitUsername) == "") != (strings.TrimSpace(cfg.QbitPasswordFile) == "") {
		return errors.New("qbit username and password file must both be set or both be empty")
	}
	if cfg.Failures <= 0 {
		return errors.New("failures must be greater than zero")
	}
	if cfg.Interval <= 0 {
		return errors.New("interval must be greater than zero")
	}
	if cfg.Cooldown <= 0 {
		return errors.New("cooldown must be greater than zero")
	}
	if cfg.HTTPTimeout <= 0 {
		return errors.New("http timeout must be greater than zero")
	}
	if cfg.QbitAuditInterval <= 0 {
		return errors.New("qbit audit interval must be greater than zero")
	}
	if cfg.RecoveryInterval <= 0 {
		return errors.New("recovery interval must be greater than zero")
	}
	if cfg.RecoveryDuration <= 0 {
		return errors.New("recovery duration must be greater than zero")
	}
	return nil
}

func validateHTTPURL(name, raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("%s is invalid", name)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", name)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s must include a host", name)
	}
	return nil
}

func normalizeURL(raw string) string {
	return strings.TrimSuffix(strings.TrimSpace(raw), "/")
}
