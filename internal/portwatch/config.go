package portwatch

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var apiKeyPattern = regexp.MustCompile(`(?m)^\s*apikey\s*=\s*"([^"]+)"`)
var apiKeyAssignmentPattern = regexp.MustCompile(`(?m)^\s*apikey\s*=`)

func LoadConfig(args []string) (Config, error) {
	var cfg Config
	var err error

	cfg.Name = os.Getenv("PORTWATCH_NAME")
	cfg.GluetunURL = os.Getenv("GLUETUN_URL")
	cfg.QbitURL = os.Getenv("QBIT_URL")
	cfg.APIKeyFile = os.Getenv("GLUETUN_API_KEY_FILE")
	cfg.QbitInterface = envString("QBIT_INTERFACE", "tun0")

	if cfg.Interval, err = envDuration("PORTWATCH_INTERVAL", time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.Cooldown, err = envDuration("PORTWATCH_COOLDOWN", 3*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.HTTPTimeout, err = envDuration("PORTWATCH_HTTP_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.QbitAuditInterval, err = envDuration("PORTWATCH_QBIT_AUDIT_INTERVAL", 30*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.Failures, err = envInt("PORTWATCH_FAILURES", 5); err != nil {
		return Config{}, err
	}

	fs := flag.NewFlagSet("gluetun-portwatch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.Name, "name", cfg.Name, "watcher name")
	fs.StringVar(&cfg.GluetunURL, "gluetun-url", cfg.GluetunURL, "Gluetun control server URL")
	fs.StringVar(&cfg.QbitURL, "qbit-url", cfg.QbitURL, "qBittorrent web UI URL")
	fs.StringVar(&cfg.APIKeyFile, "gluetun-api-key-file", cfg.APIKeyFile, "Gluetun API key file")
	fs.DurationVar(&cfg.Interval, "interval", cfg.Interval, "poll interval")
	fs.IntVar(&cfg.Failures, "failures", cfg.Failures, "failure threshold")
	fs.DurationVar(&cfg.Cooldown, "cooldown", cfg.Cooldown, "failure cooldown")
	fs.DurationVar(&cfg.HTTPTimeout, "http-timeout", cfg.HTTPTimeout, "HTTP timeout")
	fs.DurationVar(&cfg.QbitAuditInterval, "qbit-audit-interval", cfg.QbitAuditInterval, "qBittorrent audit interval")
	fs.StringVar(&cfg.QbitInterface, "qbit-interface", cfg.QbitInterface, "qBittorrent network interface")
	fs.BoolVar(&cfg.Once, "once", cfg.Once, "run once and exit")
	fs.BoolVar(&cfg.DryRun, "dry-run", cfg.DryRun, "log actions without changing state")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}

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
	if match := apiKeyPattern.FindStringSubmatch(content); len(match) == 2 {
		return match[1], nil
	}
	if apiKeyAssignmentPattern.MatchString(content) {
		return "", errors.New("api key not found")
	}
	return content, nil
}

func envString(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return duration, nil
}

func envInt(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return n, nil
}

func validateConfig(cfg Config) error {
	if cfg.Name == "" {
		return errors.New("name is required")
	}
	if cfg.GluetunURL == "" {
		return errors.New("gluetun URL is required")
	}
	if cfg.QbitURL == "" {
		return errors.New("qbit URL is required")
	}
	if cfg.APIKeyFile == "" {
		return errors.New("gluetun API key file is required")
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
	return nil
}
