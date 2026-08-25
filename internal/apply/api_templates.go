package apply

import (
	"path/filepath"
	"strings"
)

const apiMainGo = `package main

import (
	"context"
	"log/slog"
	"os"

	"example.com/smt/apis/internal/server"
)

func main() {
	if err := server.Run(context.Background()); err != nil {
		logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
`

const apiServerGo = `package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humagin"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	requestIDHeader = "X-Request-ID"
	requestIDKey    = "smt.request_id"
)

type Config struct {
	HTTPAddr           string        ` + "`env:\"HTTP_ADDR\" envDefault:\":8080\"`" + `
	AppEnv             string        ` + "`env:\"APP_ENV\" envDefault:\"development\"`" + `
	LogLevel           slog.Level    ` + "`env:\"LOG_LEVEL\" envDefault:\"info\"`" + `
	ReadTimeout        time.Duration ` + "`env:\"HTTP_READ_TIMEOUT\" envDefault:\"15s\"`" + `
	ReadHeaderTimeout  time.Duration ` + "`env:\"HTTP_READ_HEADER_TIMEOUT\" envDefault:\"5s\"`" + `
	WriteTimeout       time.Duration ` + "`env:\"HTTP_WRITE_TIMEOUT\" envDefault:\"15s\"`" + `
	IdleTimeout        time.Duration ` + "`env:\"HTTP_IDLE_TIMEOUT\" envDefault:\"60s\"`" + `
	MaxHeaderBytes     int           ` + "`env:\"HTTP_MAX_HEADER_BYTES\" envDefault:\"1048576\"`" + `
	ShutdownTimeout    time.Duration ` + "`env:\"HTTP_SHUTDOWN_TIMEOUT\" envDefault:\"10s\"`" + `
	OIDCIssuerURL      string        ` + "`env:\"OIDC_ISSUER_URL\"`" + `
	OIDCDiscoveryURL   string        ` + "`env:\"OIDC_DISCOVERY_URL\"`" + `
	OIDCJWKSURL        string        ` + "`env:\"OIDC_JWKS_URL\"`" + `
	OIDCAudience       string        ` + "`env:\"OIDC_AUDIENCE\"`" + `
	OIDCRequiredScopes string        ` + "`env:\"OIDC_REQUIRED_SCOPES\" envDefault:\"openid,profile,email\"`" + `
}

func LoadConfig() (Config, error) {
	cfg := Config{}
	err := env.Parse(&cfg)
	return cfg, err
}

type Readiness struct {
	ready atomic.Bool
}

func (r *Readiness) MarkReady() {
	r.ready.Store(true)
}

func (r *Readiness) MarkNotReady() {
	r.ready.Store(false)
}

func (r *Readiness) Ready() bool {
	return r.ready.Load()
}

type statusBody struct {
	Status string ` + "`json:\"status\"`" + `
}

type statusOutput struct {
	Status int ` + "`json:\"-\"`" + `
	Body   statusBody
}

type emptyInput struct{}

type requestMetrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inflight *prometheus.GaugeVec
}

func newRequestMetrics(reg prometheus.Registerer) *requestMetrics {
	metrics := &requestMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "smt_http_requests_total",
			Help: "Total HTTP requests handled by the API.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "smt_http_request_duration_seconds",
			Help: "HTTP request duration in seconds.",
		}, []string{"method", "route"}),
		inflight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "smt_http_requests_in_flight",
			Help: "HTTP requests currently being handled.",
		}, []string{"route"}),
	}
	reg.MustRegister(metrics.requests, metrics.duration, metrics.inflight)
	return metrics
}

func requestMetricsMiddleware(metrics *requestMetrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		metrics.inflight.WithLabelValues("unmatched").Inc()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		metrics.inflight.WithLabelValues("unmatched").Dec()
		metrics.inflight.WithLabelValues(route).Add(0)
		method := c.Request.Method
		metrics.requests.WithLabelValues(method, route, strconv.Itoa(c.Writer.Status())).Inc()
		metrics.duration.WithLabelValues(method, route).Observe(time.Since(started).Seconds())
	}
}

func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(requestIDHeader)
		if !validRequestID(requestID) {
			requestID = newRequestID()
		}
		c.Set(requestIDKey, requestID)
		c.Header(requestIDHeader, requestID)
		c.Next()
	}
}

func validRequestID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == ':' {
			continue
		}
		return false
	}
	return true
}

func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return "smt-request"
}

func recoveryMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("panic recovered",
					"panic", fmt.Sprint(recovered),
					"stack", string(debug.Stack()),
					"route", c.FullPath(),
					"method", c.Request.Method,
					"request_id", requestID(c),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			}
		}()
		c.Next()
	}
}

func requestID(c *gin.Context) string {
	if value, exists := c.Get(requestIDKey); exists {
		if requestID, ok := value.(string); ok {
			return requestID
		}
	}
	return ""
}

type Application struct {
	Router    *gin.Engine
	API       huma.API
	Readiness *Readiness
}

type humaResponseWriter struct {
	context   huma.Context
	header    http.Header
	committed bool
}

func newHumaResponseWriter(context huma.Context) *humaResponseWriter {
	return &humaResponseWriter{context: context, header: make(http.Header)}
}

func (w *humaResponseWriter) Header() http.Header {
	return w.header
}

func (w *humaResponseWriter) WriteHeader(status int) {
	if w.committed {
		return
	}
	for name, values := range w.header {
		if len(values) > 0 {
			w.context.SetHeader(name, values[0])
		}
	}
	w.context.SetStatus(status)
	w.committed = true
}

func (w *humaResponseWriter) Write(contents []byte) (int, error) {
	if !w.committed {
		w.WriteHeader(http.StatusOK)
	}
	return w.context.BodyWriter().Write(contents)
}

func New(logger *slog.Logger) *Application {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	metrics := newRequestMetrics(registry)
	router.Use(requestIDMiddleware(), recoveryMiddleware(logger), requestMetricsMiddleware(metrics))
	readiness := &Readiness{}
	config := huma.DefaultConfig("SMT API", "v0.1.0")
	config.OpenAPIPath = "/openapi"
	config.DocsPath = "/docs"
	api := humagin.New(router, config)
	huma.Get(api, "/healthz", func(context.Context, *emptyInput) (*statusOutput, error) {
		return &statusOutput{Status: http.StatusOK, Body: statusBody{Status: "ok"}}, nil
	})
	huma.Get(api, "/readyz", func(context.Context, *emptyInput) (*statusOutput, error) {
		status := "not_ready"
		code := http.StatusServiceUnavailable
		if readiness.Ready() {
			status = "ready"
			code = http.StatusOK
		}
		return &statusOutput{Status: code, Body: statusBody{Status: status}}, nil
	})
	metricsOperation := &huma.Operation{
		OperationID: "metrics",
		Method:      http.MethodGet,
		Path:        "/metrics",
		Responses: map[string]*huma.Response{
			"200": {
				Description: "Prometheus metrics.",
				Content: map[string]*huma.MediaType{
					"text/plain": {Schema: &huma.Schema{Type: "string"}},
				},
			},
		},
	}
	api.OpenAPI().AddOperation(metricsOperation)
	api.Adapter().Handle(metricsOperation, func(context huma.Context) {
		writer := newHumaResponseWriter(context)
		promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	})
	return &Application{Router: router, API: api, Readiness: readiness}
}

func NewRouter(logger *slog.Logger) (*gin.Engine, *Readiness) {
	application := New(logger)
	return application.Router, application.Readiness
}

func Run(ctx context.Context) error {
	startupLogger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg, err := LoadConfig()
	if err != nil {
		startupLogger.Error("configuration load failed", "error", err)
		panic(err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	application := New(logger)
	router, readiness := application.Router, application.Readiness
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadTimeout:       cfg.ReadTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}
	signalContext, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	serverErrors := make(chan error, 1)
	go func() {
		readiness.MarkReady()
		serverErrors <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve API: %w", err)
	case <-signalContext.Done():
		readiness.MarkNotReady()
		shutdownContext, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shutdown API: %w", err)
		}
		if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve API: %w", err)
		}
		return nil
	}
}
`

func apiServerGoForSelection(databaseSelected bool) string {
	if !databaseSelected {
		return apiServerGo
	}

	source := apiServerGo
	source = strings.Replace(source, "\t\"strconv\"\n", "\t\"strconv\"\n\t\"strings\"\n", 1)
	source = strings.Replace(source,
		"\t\"github.com/gin-gonic/gin\"\n",
		"\t\"github.com/gin-gonic/gin\"\n\t\"github.com/jackc/pgx/v5/pgxpool\"\n",
		1,
	)
	source = strings.Replace(source,
		"\tOIDCRequiredScopes string        `env:\"OIDC_REQUIRED_SCOPES\" envDefault:\"openid,profile,email\"`\n",
		"\tOIDCRequiredScopes string        `env:\"OIDC_REQUIRED_SCOPES\" envDefault:\"openid,profile,email\"`\n\tDatabaseURL        string        `env:\"DATABASE_URL\"`\n",
		1,
	)
	source = strings.Replace(source, "type Readiness struct {", apiDatabaseRuntimeCode+"\ntype Readiness struct {", 1)
	runStart := strings.Index(source, "func Run(ctx context.Context) error {")
	if runStart < 0 {
		panic("generated API server template is missing Run")
	}
	return source[:runStart] + apiDatabaseRunCode
}

const apiDatabaseRuntimeCode = `const (
	databasePingInterval = time.Second
	databasePingTimeout  = 2 * time.Second
)

type databasePinger interface {
	Ping(context.Context) error
	Close()
}

type databaseReadinessMonitor struct {
	readiness *Readiness
	pinger    databasePinger
	logger    *slog.Logger
	interval  time.Duration
	timeout   time.Duration
	done      chan struct{}
}

func newDatabaseReadinessMonitor(readiness *Readiness, pinger databasePinger, logger *slog.Logger, interval, timeout time.Duration) *databaseReadinessMonitor {
	if interval <= 0 {
		interval = databasePingInterval
	}
	if timeout <= 0 {
		timeout = databasePingTimeout
	}
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	return &databaseReadinessMonitor{
		readiness: readiness,
		pinger:    pinger,
		logger:    logger,
		interval:  interval,
		timeout:   timeout,
		done:      make(chan struct{}),
	}
}

func (m *databaseReadinessMonitor) run(ctx context.Context) {
	defer close(m.done)
	m.readiness.MarkNotReady()
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.readiness.MarkNotReady()
			return
		case <-ticker.C:
			m.check(ctx)
		}
	}
}

func (m *databaseReadinessMonitor) wait() {
	<-m.done
}

func (m *databaseReadinessMonitor) check(ctx context.Context) {
	pingContext, cancel := context.WithTimeout(ctx, m.timeout)
	err := m.pinger.Ping(pingContext)
	cancel()
	if err != nil {
		m.readiness.MarkNotReady()
		m.logger.Warn("database readiness check failed", "status", "not_ready")
		return
	}
	m.readiness.MarkReady()
}

func openDatabasePool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return nil, errors.New("DATABASE_URL is required for API+Database runtime")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("DATABASE_URL is invalid; use a PostgreSQL connection URL")
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("database pool could not be created")
	}
	return pool, nil
}
`

const apiDatabaseRunCode = `func Run(ctx context.Context) error {
	startupLogger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	cfg, err := LoadConfig()
	if err != nil {
		startupLogger.Error("configuration load failed", "error", err)
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	pool, err := openDatabasePool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database configuration failed", "error", err)
		return err
	}
	defer pool.Close()
	application := New(logger)
	router, readiness := application.Router, application.Readiness
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadTimeout:       cfg.ReadTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}
	signalContext, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	monitorContext, stopMonitor := context.WithCancel(signalContext)
	defer stopMonitor()
	monitor := newDatabaseReadinessMonitor(readiness, pool, logger, databasePingInterval, databasePingTimeout)
	serverErrors := make(chan error, 1)
	go monitor.run(monitorContext)
	go func() {
		serverErrors <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-serverErrors:
		readiness.MarkNotReady()
		stopMonitor()
		monitor.wait()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve API: %w", err)
	case <-signalContext.Done():
		readiness.MarkNotReady()
		stopMonitor()
		monitor.wait()
		shutdownContext, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shutdown API: %w", err)
		}
		if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve API: %w", err)
		}
		return nil
	}
}
`

const apiOpenAPICommandGo = `package main

import (
	"log/slog"
	"os"

	"example.com/smt/apis/internal/server"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	application := server.New(logger)
	contents, err := application.API.OpenAPI().YAML()
	if err != nil {
		logger.Error("render OpenAPI", "error", err)
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(contents)
}
`

const apiEnvExampleBase = `HTTP_ADDR=:8080
APP_ENV=development
LOG_LEVEL=info
HTTP_READ_TIMEOUT=15s
HTTP_READ_HEADER_TIMEOUT=5s
HTTP_WRITE_TIMEOUT=15s
HTTP_IDLE_TIMEOUT=60s
HTTP_MAX_HEADER_BYTES=1048576
HTTP_SHUTDOWN_TIMEOUT=10s
OIDC_ISSUER_URL=
OIDC_DISCOVERY_URL=
OIDC_JWKS_URL=
OIDC_AUDIENCE=
OIDC_REQUIRED_SCOPES=openid,profile,email
`

const apiTaskfileBaseYAML = `version: '3'

dotenv: ['.env']

tasks:
  build:
    cmds:
      - mkdir -p bin && go build -trimpath -o bin/apis .
  run:
    deps: [build]
    cmds:
      - ./bin/apis
  test:
    cmds:
      - go test ./...
  coverage:
    cmds:
      - go test ./... -coverprofile=coverage.out
  test:race:
    cmds:
      - go test -race ./...
  test:fuzz:
    cmds:
      - go test -run=^$ -fuzz=FuzzValidRequestID -fuzztime=30s ./internal/server
  format:check:
    cmds:
      - |
        files="$(gofmt -l $(find . -type f -name '*.go' -not -path './vendor/*' -print))"
        if [ -n "$files" ]; then
          printf '%s\n' "$files" >&2
          exit 1
        fi
  lint:
    cmds:
      - |
        set -eu
        tool_dir="$(mktemp -d)"
        trap 'rm -rf "$tool_dir"' EXIT
        cp go.mod "$tool_dir/go.mod"
        cp go.sum "$tool_dir/go.sum"
        go mod tidy -modfile="$tool_dir/go.mod"
        go tool -modfile="$tool_dir/go.mod" golangci-lint run ./...
  vuln:
    cmds:
      - |
        set -eu
        tool_dir="$(mktemp -d)"
        trap 'rm -rf "$tool_dir"' EXIT
        cp go.mod "$tool_dir/go.mod"
        cp go.sum "$tool_dir/go.sum"
        go mod tidy -modfile="$tool_dir/go.mod"
        go tool -modfile="$tool_dir/go.mod" govulncheck ./...
  vet:
    cmds:
      - go vet ./...
  mod:
    cmds:
      - go mod verify
  openapi:
    cmds:
      - tmp="$(mktemp)" && trap 'rm -f "$tmp"' EXIT && GOPROXY=off GOSUMDB=off go run ./cmd/openapi > "$tmp" && cmp -s "$tmp" openapi.yaml
  container:build:
    preconditions:
      - sh: command -v podman
        msg: Podman is required; install and configure it before running task container:build
    cmds:
      - podman build --pull=missing --format=oci --build-arg TARGETOS="${SMT_API_TARGETOS:-linux}" --build-arg TARGETARCH="${SMT_API_TARGETARCH:-}" --file Containerfile --tag smt-api:local .
  container:build:production:
    preconditions:
      - sh: command -v podman
        msg: Podman is required; install and configure it before running task container:build:production
    cmds:
      - podman build --pull=never --format=oci --build-arg TARGETOS="${SMT_API_TARGETOS:-linux}" --build-arg TARGETARCH="${SMT_API_TARGETARCH:-}" --file Containerfile --tag "${SMT_API_PRODUCTION_IMAGE:-smt-api:production}" .
  container:verify:
    deps: [container:build]
    preconditions:
      - sh: command -v podman
        msg: Podman is required; install and configure it before running task container:verify
    cmds:
      - |
        set -eu
        container="${SMT_API_CONTAINER_NAME:-smt-api-verify}"
        image="${SMT_API_IMAGE:-smt-api:local}"
        host_port="${SMT_API_HOST_PORT:-18080}"
        cleanup() {
          podman rm -f "$container" >/dev/null 2>&1 || true
        }
        verify() {
          podman rm -f "$container" >/dev/null 2>&1 || true
          podman run --detach --name "$container" --publish "127.0.0.1:${host_port}:8080" "$image"
          test "$(podman exec "$container" id -u)" = "10001"
          healthy=false
          for attempt in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30; do
            if podman exec "$container" wget -q -O - http://127.0.0.1:8080/healthz >/dev/null && podman exec "$container" wget -q -O - http://127.0.0.1:8080/readyz >/dev/null; then
              healthy=true
              break
            fi
            sleep 1
          done
          if [ "$healthy" != true ]; then
            echo "API container did not become healthy; inspect Podman logs and verify the generated image" >&2
            return 1
          fi
          podman stop --time 10 "$container"
          test "$(podman wait "$container")" = "0"
        }
        if verify; then
          status=0
        else
          status=$?
        fi
        cleanup
        exit "$status"
  verify:
    deps: [format:check, lint, vuln, vet, build, test, mod, openapi, container:verify]
`

const apiOpenAPIYAML = `components:
  schemas:
    ErrorDetail:
      additionalProperties: false
      properties:
        location:
          description: Where the error occurred, e.g. 'body.items[3].tags' or 'path.thing-id'
          type: string
        message:
          description: Error message text
          type: string
        value:
          description: The value at the given location
      type: object
    ErrorModel:
      additionalProperties: false
      properties:
        $schema:
          description: A URL to the JSON Schema for this object.
          examples:
            - https://example.com/schemas/ErrorModel.json
          format: uri
          readOnly: true
          type: string
        detail:
          description: A human-readable explanation specific to this occurrence of the problem.
          examples:
            - Property foo is required but is missing.
          type: string
        errors:
          description: Optional list of individual error details
          items:
            $ref: "#/components/schemas/ErrorDetail"
          type:
            - array
            - "null"
        instance:
          description: A URI reference that identifies the specific occurrence of the problem.
          examples:
            - https://example.com/error-log/abc123
          format: uri
          type: string
        status:
          description: HTTP status code
          examples:
            - 400
          format: int64
          type: integer
        title:
          description: A short, human-readable summary of the problem type. This value should not change between occurrences of the error.
          examples:
            - Bad Request
          type: string
        type:
          default: about:blank
          description: A URI reference to human-readable documentation for the error.
          examples:
            - https://example.com/errors/example
          format: uri
          type: string
      type: object
    StatusBody:
      additionalProperties: false
      properties:
        $schema:
          description: A URL to the JSON Schema for this object.
          examples:
            - https://example.com/schemas/StatusBody.json
          format: uri
          readOnly: true
          type: string
        status:
          type: string
      required:
        - status
      type: object
info:
  title: SMT API
  version: v0.1.0
openapi: 3.1.0
paths:
  /healthz:
    get:
      operationId: get-healthz
      responses:
        "200":
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/StatusBody"
          description: OK
        default:
          content:
            application/problem+json:
              schema:
                $ref: "#/components/schemas/ErrorModel"
          description: Error
      summary: Get healthz
  /metrics:
    get:
      operationId: metrics
      responses:
        "200":
          content:
            text/plain:
              schema:
                type: string
          description: Prometheus metrics.
  /readyz:
    get:
      operationId: get-readyz
      responses:
        "200":
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/StatusBody"
          description: OK
        default:
          content:
            application/problem+json:
              schema:
                $ref: "#/components/schemas/ErrorModel"
          description: Error
      summary: Get readyz
`

func apiSourceFiles(databaseSelected bool) map[string]string {
	goMod, goSum := apiManifests(databaseSelected)
	files := map[string]string{
		"main.go":                             apiMainGo,
		"internal/server/server.go":           apiServerGoForSelection(databaseSelected),
		"internal/server/server_test.go":      apiServerTestGo,
		"internal/server/config_test.go":      apiConfigTestGo,
		"internal/server/server_fuzz_test.go": apiServerFuzzTestGo,
		"cmd/openapi/main.go":                 apiOpenAPICommandGo,
		".env.example":                        apiEnvExample(databaseSelected),
		"Taskfile.yml":                        apiTaskfileYAML(databaseSelected),
		"Containerfile":                       apiContainerfile,
		"openapi.yaml":                        apiOpenAPIYAML,
		"go.mod":                              goMod,
		"go.sum":                              goSum,
	}
	if databaseSelected {
		for relative, contents := range apiMigrationFiles() {
			files[relative] = contents
		}
		files["internal/server/database_integration_test.go"] = apiDatabaseIntegrationTestGo
		files["internal/server/database_readiness_test.go"] = apiDatabaseReadinessTestGo
	}
	return files
}

func writeAPISourceFiles(bootstrap string, databaseSelected bool) error {
	for relative, contents := range apiSourceFiles(databaseSelected) {
		if err := writeFile(filepath.Join(bootstrap, relative), contents); err != nil {
			return err
		}
	}
	return nil
}
