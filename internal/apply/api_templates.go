package apply

import "path/filepath"

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
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	requestIDHeader = "X-Request-ID"
	requestIDKey    = "smt.request_id"
)

type Config struct {
	HTTPAddr          string        ` + "`env:\"HTTP_ADDR\" envDefault:\":8080\"`" + `
	AppEnv            string        ` + "`env:\"APP_ENV\" envDefault:\"development\"`" + `
	LogLevel          slog.Level    ` + "`env:\"LOG_LEVEL\" envDefault:\"info\"`" + `
	ReadTimeout       time.Duration ` + "`env:\"HTTP_READ_TIMEOUT\" envDefault:\"15s\"`" + `
	ReadHeaderTimeout time.Duration ` + "`env:\"HTTP_READ_HEADER_TIMEOUT\" envDefault:\"5s\"`" + `
	WriteTimeout      time.Duration ` + "`env:\"HTTP_WRITE_TIMEOUT\" envDefault:\"15s\"`" + `
	IdleTimeout       time.Duration ` + "`env:\"HTTP_IDLE_TIMEOUT\" envDefault:\"60s\"`" + `
	MaxHeaderBytes    int           ` + "`env:\"HTTP_MAX_HEADER_BYTES\" envDefault:\"1048576\"`" + `
	ShutdownTimeout   time.Duration ` + "`env:\"HTTP_SHUTDOWN_TIMEOUT\" envDefault:\"10s\"`" + `
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
	requests  *prometheus.CounterVec
	duration  *prometheus.HistogramVec
	inflight  *prometheus.GaugeVec
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
	registry.MustRegister(prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
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

const apiEnvExample = `HTTP_ADDR=:8080
APP_ENV=development
LOG_LEVEL=info
HTTP_READ_TIMEOUT=15s
HTTP_READ_HEADER_TIMEOUT=5s
HTTP_WRITE_TIMEOUT=15s
HTTP_IDLE_TIMEOUT=60s
HTTP_MAX_HEADER_BYTES=1048576
HTTP_SHUTDOWN_TIMEOUT=10s
`

const apiTaskfileYAML = `version: '3'

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
  mod:
    cmds:
      - go mod verify
  openapi:
    cmds:
      - tmp="$$(mktemp)" && trap 'rm -f "$$tmp"' EXIT && GOPROXY=off GOSUMDB=off go run ./cmd/openapi > "$$tmp" && cmp -s "$$tmp" openapi.yaml
  verify:
    deps: [build, test, mod, openapi]
    cmds:
      - go vet ./...
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
	return map[string]string{
		"main.go":                   apiMainGo,
		"internal/server/server.go": apiServerGo,
		"cmd/openapi/main.go":       apiOpenAPICommandGo,
		".env.example":              apiEnvExample,
		"Taskfile.yml":              apiTaskfileYAML,
		"openapi.yaml":              apiOpenAPIYAML,
		"go.mod":                    goMod,
		"go.sum":                    goSum,
	}
}

func writeAPISourceFiles(bootstrap string, databaseSelected bool) error {
	for relative, contents := range apiSourceFiles(databaseSelected) {
		if err := writeFile(filepath.Join(bootstrap, relative), contents); err != nil {
			return err
		}
	}
	return nil
}
