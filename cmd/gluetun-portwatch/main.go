package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
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

	httpClient := &http.Client{Timeout: cfg.HTTPTimeout}
	gluetun := portwatch.NewGluetunClient(cfg.GluetunURL, apiKey, httpClient)
	qbit := portwatch.NewQbitClient(cfg.QbitURL, httpClient)
	logger := portwatch.NewLogger(os.Stdout, cfg.Name)
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
