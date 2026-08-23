package apply

import "strings"

const apiServerTestGo = `package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHealthzHandler(t *testing.T) {
	application := New(testLogger())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	application.Router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("health status=%d, want %d", recorder.Code, http.StatusOK)
	}
	var body struct{ Status string }
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "ok" {
		t.Fatalf("health status body=%q, want %q", body.Status, "ok")
	}
}

func TestReadinessHandler(t *testing.T) {
	application := New(testLogger())
	tests := []struct {
		name       string
		markReady  func()
		wantStatus int
		wantBody   string
	}{
		{name: "not ready", markReady: func() {}, wantStatus: http.StatusServiceUnavailable, wantBody: "not_ready"},
		{name: "ready", markReady: application.Readiness.MarkReady, wantStatus: http.StatusOK, wantBody: "ready"},
		{name: "not ready after shutdown", markReady: func() { application.Readiness.MarkReady(); application.Readiness.MarkNotReady() }, wantStatus: http.StatusServiceUnavailable, wantBody: "not_ready"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.markReady()
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
			application.Router.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("readiness status=%d, want %d", recorder.Code, test.wantStatus)
			}
			var body struct{ Status string }
			if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Status != test.wantBody {
				t.Fatalf("readiness status body=%q, want %q", body.Status, test.wantBody)
			}
		})
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "preserves valid request ID", want: "request-123"},
		{name: "replaces invalid request ID", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			application := New(testLogger())
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			if test.want != "" {
				request.Header.Set(requestIDHeader, test.want)
			} else {
				request.Header.Set(requestIDHeader, "invalid value")
			}
			application.Router.ServeHTTP(recorder, request)
			got := recorder.Header().Get(requestIDHeader)
			if test.want != "" && got != test.want {
				t.Fatalf("request ID=%q, want %q", got, test.want)
			}
			if test.want == "" && !validRequestID(got) {
				t.Fatalf("generated request ID=%q is invalid", got)
			}
		})
	}
}

func TestRunStopsOnContextCancellation(t *testing.T) {
	address := freeTestAddress(t)
	t.Setenv("HTTP_ADDR", address)
	t.Setenv("HTTP_SHUTDOWN_TIMEOUT", "2s")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx) }()

	client := &http.Client{Timeout: 500 * time.Millisecond}
	waitForTestHTTP(t, client, "http://"+address+"/healthz")
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error after cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}

func freeTestAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func waitForTestHTTP(t *testing.T, client *http.Client, endpoint string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(endpoint)
		if err == nil {
			response.Body.Close()
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("endpoint did not become ready: %s", endpoint)
}
`

const apiConfigTestGo = `package server

import (
	"log/slog"
	"testing"
	"time"
)

func clearConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"HTTP_ADDR",
		"APP_ENV",
		"LOG_LEVEL",
		"HTTP_READ_TIMEOUT",
		"HTTP_READ_HEADER_TIMEOUT",
		"HTTP_WRITE_TIMEOUT",
		"HTTP_IDLE_TIMEOUT",
		"HTTP_MAX_HEADER_BYTES",
		"HTTP_SHUTDOWN_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	clearConfigEnvironment(t)
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.HTTPAddr != ":8080" || config.AppEnv != "development" || config.LogLevel != slog.LevelInfo ||
		config.ReadTimeout != 15*time.Second || config.ReadHeaderTimeout != 5*time.Second ||
		config.WriteTimeout != 15*time.Second || config.IdleTimeout != 60*time.Second ||
		config.MaxHeaderBytes != 1048576 || config.ShutdownTimeout != 10*time.Second {
		t.Fatalf("unexpected defaults: %#v", config)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("APP_ENV", "test")
	t.Setenv("LOG_LEVEL", "warn")
	t.Setenv("HTTP_READ_TIMEOUT", "1s")
	t.Setenv("HTTP_READ_HEADER_TIMEOUT", "2s")
	t.Setenv("HTTP_WRITE_TIMEOUT", "3s")
	t.Setenv("HTTP_IDLE_TIMEOUT", "4s")
	t.Setenv("HTTP_MAX_HEADER_BYTES", "2048")
	t.Setenv("HTTP_SHUTDOWN_TIMEOUT", "5s")

	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.HTTPAddr != ":9090" || config.AppEnv != "test" || config.LogLevel != slog.LevelWarn ||
		config.ReadTimeout != time.Second || config.ReadHeaderTimeout != 2*time.Second ||
		config.WriteTimeout != 3*time.Second || config.IdleTimeout != 4*time.Second ||
		config.MaxHeaderBytes != 2048 || config.ShutdownTimeout != 5*time.Second {
		t.Fatalf("unexpected overrides: %#v", config)
	}
}

func TestLoadConfigRejectsMalformedValues(t *testing.T) {
	for _, test := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "log level", key: "LOG_LEVEL", value: "trace"},
		{name: "duration", key: "HTTP_READ_TIMEOUT", value: "nope"},
		{name: "integer", key: "HTTP_MAX_HEADER_BYTES", value: "nope"},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearConfigEnvironment(t)
			t.Setenv(test.key, test.value)
			if _, err := LoadConfig(); err == nil {
				t.Fatalf("LoadConfig succeeded for malformed %s=%q", test.key, test.value)
			}
		})
	}
}
`

const apiServerFuzzTestGo = `package server

import "testing"

func FuzzValidRequestID(f *testing.F) {
	f.Add("request-123")
	f.Add("invalid value")
	f.Add("")
	f.Fuzz(func(t *testing.T, value string) {
		_ = validRequestID(value)
	})
}
`

const apiDatabaseIntegrationTestGo = `//go:build integration

package server

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDatabaseConnection(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration tests; start the disposable PostgreSQL service and retry")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("database configuration failed; verify DATABASE_URL and the disposable PostgreSQL service")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal("database ping failed; verify DATABASE_URL and the disposable PostgreSQL service")
	}
}
`

func apiTaskfileYAML(databaseSelected bool) string {
	taskfile := apiTaskfileBaseYAML
	if !databaseSelected {
		return taskfile
	}
	integrationTask := `  test:integration:
    preconditions:
      - sh: test -n "$$DATABASE_URL"
        msg: DATABASE_URL is required; start the disposable PostgreSQL service before running task test:integration
    cmds:
      - go test -tags=integration ./...

`
	return strings.Replace(taskfile, "  verify:\n", integrationTask+"  verify:\n", 1)
}
