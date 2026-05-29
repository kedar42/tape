package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"gluetun-portwatch/internal/portwatch"
)

func TestRunErrorContextCancelExits0(t *testing.T) {
	if code := exitCodeForRunError(context.Canceled); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestGracefulShutdownErrorClassification(t *testing.T) {
	stopErr := errors.New("gluetun stop failed")
	tests := []struct {
		name     string
		err      error
		graceful bool
	}{
		{name: "plain canceled", err: context.Canceled, graceful: true},
		{name: "wrapped canceled only", err: fmt.Errorf("wrap: %w", context.Canceled), graceful: true},
		{name: "joined canceled with real error", err: errors.Join(stopErr, context.Canceled), graceful: false},
		{name: "unrelated", err: errors.New("network failed"), graceful: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGracefulShutdownError(tt.err); got != tt.graceful {
				t.Fatalf("isGracefulShutdownError(%v) = %v, want %v", tt.err, got, tt.graceful)
			}
		})
	}
}

func TestMainReadAPIKeyErrorExits2(t *testing.T) {
	if os.Getenv("PORTWATCH_TEST_MAIN") == "1" {
		os.Args = []string{os.Args[0]}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMainReadAPIKeyErrorExits2")
	cmd.Env = append(os.Environ(),
		"PORTWATCH_TEST_MAIN=1",
		"GLUETUN_URL=http://gluetun:8000",
		"QBIT_URL=http://gluetun:8080",
		"GLUETUN_API_KEY_FILE=/path/that/does/not/exist",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("main exited successfully, want exit 2; output=%s", out)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("error = %T %v, want ExitError", err, err)
	}
	if exitErr.ExitCode() != 2 {
		t.Fatalf("exit code = %d, want 2; output=%s", exitErr.ExitCode(), out)
	}
	if !strings.Contains(string(out), "api key error:") {
		t.Fatalf("output = %q, want api key error", out)
	}
}

func TestQbitAuthFromConfigReadsAPIKeyFile(t *testing.T) {
	path := writeSecretFile(t, " qbit-api-key\n")

	auth, err := qbitAuthFromConfig(portwatch.Config{QbitAPIKeyFile: path})
	if err != nil {
		t.Fatalf("qbitAuthFromConfig returned error: %v", err)
	}
	if auth.APIKey != "qbit-api-key" {
		t.Fatalf("APIKey = %q, want qbit-api-key", auth.APIKey)
	}
	if auth.Username != "" || auth.Password != "" {
		t.Fatalf("username/password = %q/%q, want empty", auth.Username, auth.Password)
	}
}

func TestQbitAuthFromConfigReadsUsernamePasswordFile(t *testing.T) {
	path := writeSecretFile(t, " secret-password\n")

	auth, err := qbitAuthFromConfig(portwatch.Config{QbitUsername: "admin", QbitPasswordFile: path})
	if err != nil {
		t.Fatalf("qbitAuthFromConfig returned error: %v", err)
	}
	if auth.APIKey != "" {
		t.Fatalf("APIKey = %q, want empty", auth.APIKey)
	}
	if auth.Username != "admin" || auth.Password != "secret-password" {
		t.Fatalf("username/password = %q/%q, want admin/secret-password", auth.Username, auth.Password)
	}
}

func TestQbitAuthFromConfigRejectsIncompleteAuth(t *testing.T) {
	_, err := qbitAuthFromConfig(portwatch.Config{QbitUsername: "admin"})
	if err == nil {
		t.Fatal("qbitAuthFromConfig returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "qbit username and password file") {
		t.Fatalf("error = %q, want qbit username and password file", err.Error())
	}
}

func writeSecretFile(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/secret"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}
