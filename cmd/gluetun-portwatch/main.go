package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"gluetun-portwatch/internal/portwatch"
)

func main() {
	cfg, err := portwatch.LoadConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(2)
	}

	apiKey, err := portwatch.ReadAPIKey(cfg.APIKeyFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "api key error: %v\n", err)
		os.Exit(2)
	}
	qbitAuth, err := qbitAuthFromConfig(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "qbit auth error: %v\n", err)
		os.Exit(2)
	}

	httpClient := &http.Client{Timeout: cfg.HTTPTimeout}
	gluetun := portwatch.NewGluetunClient(cfg.GluetunURL, apiKey, httpClient)
	qbit := portwatch.NewQbitClient(cfg.QbitURL, httpClient, qbitAuth)
	logger := portwatch.NewLogger(os.Stdout)
	runner := portwatch.NewRunner(cfg, gluetun, qbit, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := runner.Run(ctx); err != nil {
		if exitCodeForRunError(err) == 0 {
			return
		}
		fmt.Fprintf(os.Stderr, "watcher error: %v\n", err)
		os.Exit(1)
	}
}

func qbitAuthFromConfig(cfg portwatch.Config) (portwatch.QbitAuth, error) {
	if strings.TrimSpace(cfg.QbitAPIKeyFile) != "" {
		apiKey, err := readTrimmedSecretFile(cfg.QbitAPIKeyFile)
		if err != nil {
			return portwatch.QbitAuth{}, fmt.Errorf("read qbit api key file: %w", err)
		}
		return portwatch.QbitAuth{APIKey: apiKey}, nil
	}

	username := strings.TrimSpace(cfg.QbitUsername)
	passwordFile := strings.TrimSpace(cfg.QbitPasswordFile)
	if username == "" && passwordFile == "" {
		return portwatch.QbitAuth{}, nil
	}
	if username == "" || passwordFile == "" {
		return portwatch.QbitAuth{}, errors.New("qbit username and password file must both be set or both be empty")
	}
	password, err := readTrimmedSecretFile(passwordFile)
	if err != nil {
		return portwatch.QbitAuth{}, fmt.Errorf("read qbit password file: %w", err)
	}
	return portwatch.QbitAuth{Username: username, Password: password}, nil
}

func readTrimmedSecretFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	secret := strings.TrimSpace(string(data))
	if secret == "" {
		return "", errors.New("secret file is empty")
	}
	return secret, nil
}

func exitCodeForRunError(err error) int {
	if isGracefulShutdownError(err) {
		return 0
	}
	return 1
}

func isGracefulShutdownError(err error) bool {
	if err == nil {
		return false
	}
	if err == context.Canceled {
		return true
	}
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		children := multi.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !isGracefulShutdownError(child) {
				return false
			}
		}
		return true
	}
	if single, ok := err.(interface{ Unwrap() error }); ok {
		return isGracefulShutdownError(single.Unwrap())
	}
	return false
}
