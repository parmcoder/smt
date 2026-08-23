package apply

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/parmcoder/smt/internal/config"
)

func TestApplyGeneratesAPIStarterOnlyForAPISelection(t *testing.T) {
	tests := map[string]struct {
		raw      []byte
		wantAPI  bool
		database bool
	}{
		"api-only":     {raw: apiBlueprintBytes(), wantAPI: true},
		"api-database": {raw: fullMobileBlueprintBytes(), wantAPI: true, database: true},
		"without-api":  {raw: blueprintBytes()},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			destination := applyAPIWorkspace(t, tt.raw)
			apiRoot := filepath.Join(destination, "apis")
			if !tt.wantAPI {
				if _, err := os.Stat(apiRoot); !os.IsNotExist(err) {
					t.Fatalf("API output exists for no-API selection: %v", err)
				}
				if _, err := os.Stat(filepath.Join(destination, "openapi.yaml")); !os.IsNotExist(err) {
					t.Fatalf("root OpenAPI output exists for no-API selection: %v", err)
				}
				return
			}
			for _, relative := range []string{
				"main.go",
				"internal/server/server.go",
				"internal/server/server_test.go",
				"internal/server/config_test.go",
				"internal/server/server_fuzz_test.go",
				"cmd/openapi/main.go",
				".env.example",
				"openapi.yaml",
				"Taskfile.yml",
				"Containerfile",
				"README.md",
				".gitignore",
				"lefthook.yml",
				"go.mod",
				"go.sum",
			} {
				if _, err := os.Stat(filepath.Join(apiRoot, relative)); err != nil {
					t.Fatalf("missing generated API file %s: %v", relative, err)
				}
			}
			main := readGeneratedAPIFile(t, apiRoot, "main.go")
			server := readGeneratedAPIFile(t, apiRoot, filepath.Join("internal", "server", "server.go"))
			openapiMain := readGeneratedAPIFile(t, apiRoot, filepath.Join("cmd", "openapi", "main.go"))
			envExample := readGeneratedAPIFile(t, apiRoot, ".env.example")
			openapi := readGeneratedAPIFile(t, apiRoot, "openapi.yaml")

			for _, marker := range []string{
				"package main",
				"internal/server",
				"func main()",
			} {
				if !strings.Contains(main, marker) {
					t.Fatalf("main.go missing %q:\n%s", marker, main)
				}
			}
			for _, marker := range []string{
				"github.com/danielgtaylor/huma/v2",
				"github.com/danielgtaylor/huma/v2/adapters/humagin",
				"github.com/gin-gonic/gin",
				"github.com/prometheus/client_golang/prometheus",
				"github.com/prometheus/client_golang/prometheus/promhttp",
				"log/slog",
				"/healthz",
				"/readyz",
				"/metrics",
				"OpenAPIPath",
				"DocsPath",
				"HTTP_ADDR",
				"APP_ENV",
				"LOG_LEVEL",
				"HTTP_READ_TIMEOUT",
				"HTTP_READ_HEADER_TIMEOUT",
				"HTTP_WRITE_TIMEOUT",
				"HTTP_IDLE_TIMEOUT",
				"HTTP_MAX_HEADER_BYTES",
				"HTTP_SHUTDOWN_TIMEOUT",
				"http.Server",
				"Shutdown",
				"signal.NotifyContext",
				"recover()",
				"debug.Stack",
				"X-Request-ID",
				"status := \"not_ready\"",
			} {
				if !strings.Contains(server, marker) {
					t.Fatalf("server.go missing %q:\n%s", marker, server)
				}
			}
			for _, marker := range []string{"package main", "OpenAPI().YAML()", "example.com/smt/apis"} {
				if !strings.Contains(openapiMain, marker) {
					t.Fatalf("cmd/openapi/main.go missing %q:\n%s", marker, openapiMain)
				}
			}
			for _, want := range []string{
				"HTTP_ADDR=:8080",
				"APP_ENV=development",
				"LOG_LEVEL=info",
				"HTTP_READ_TIMEOUT=15s",
				"HTTP_READ_HEADER_TIMEOUT=5s",
				"HTTP_WRITE_TIMEOUT=15s",
				"HTTP_IDLE_TIMEOUT=60s",
				"HTTP_MAX_HEADER_BYTES=1048576",
				"HTTP_SHUTDOWN_TIMEOUT=10s",
				"OIDC_ISSUER_URL=",
				"OIDC_DISCOVERY_URL=",
				"OIDC_JWKS_URL=",
				"OIDC_AUDIENCE=",
				"OIDC_REQUIRED_SCOPES=openid,profile,email",
			} {
				if !strings.Contains(envExample, want) {
					t.Fatalf(".env.example missing %q:\n%s", want, envExample)
				}
			}
			for _, want := range []string{
				"openapi: 3.1.0",
				"title: SMT API",
				"version: v0.1.0",
				"/healthz:",
				"/readyz:",
				"/metrics:",
			} {
				if !strings.Contains(openapi, want) {
					t.Fatalf("openapi.yaml missing %q:\n%s", want, openapi)
				}
			}
			allSource := main + server + openapiMain + openapi
			for _, forbidden := range []string{"pgx", "migrate", "golang-migrate", "password", "secret", "token", "/users", "CRUD"} {
				if strings.Contains(strings.ToLower(allSource), strings.ToLower(forbidden)) {
					t.Fatalf("generated API source contains forbidden %q", forbidden)
				}
			}
			if tt.database {
				for _, forbidden := range []string{"pgx", "migrate", "golang-migrate"} {
					if strings.Contains(strings.ToLower(allSource), strings.ToLower(forbidden)) {
						t.Fatalf("API+Database source contains manifest-only dependency %q", forbidden)
					}
				}
			}
		})
	}
}

func TestApplyAPIOutputIsByteStableAcrossFreshDestinations(t *testing.T) {
	var outputs [2]map[string][]byte
	for i := range outputs {
		destination := applyAPIWorkspace(t, apiBlueprintBytes())
		outputs[i] = make(map[string][]byte)
		apiRoot := filepath.Join(destination, "apis")
		for _, relative := range []string{"main.go", "internal/server/server.go", "cmd/openapi/main.go", ".env.example", "openapi.yaml", "Taskfile.yml"} {
			outputs[i][relative] = []byte(readGeneratedAPIFile(t, apiRoot, relative))
		}
		regenerated := runOpenAPICommand(t, apiRoot)
		if !bytes.Equal(regenerated, outputs[i]["openapi.yaml"]) {
			t.Fatalf("cmd/openapi output differs from applied openapi.yaml in destination %d", i)
		}
		outputs[i]["cmd/openapi.yaml"] = regenerated
	}
	for relative := range outputs[0] {
		if !bytes.Equal(outputs[0][relative], outputs[1][relative]) {
			t.Fatalf("generated API file %s is not byte-stable", relative)
		}
	}
}

func TestApplyGeneratesAPITaskfileAndEnvManifest(t *testing.T) {
	tests := map[string]struct {
		raw      []byte
		wantAPI  bool
		database bool
	}{
		"api-only":     {raw: apiBlueprintBytes(), wantAPI: true},
		"api-database": {raw: fullMobileBlueprintBytes(), wantAPI: true, database: true},
		"without-api":  {raw: blueprintBytes()},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			destination := applyAPIWorkspace(t, tt.raw)
			taskfilePath := filepath.Join(destination, "apis", "Taskfile.yml")
			if !tt.wantAPI {
				if _, err := os.Stat(taskfilePath); !os.IsNotExist(err) {
					t.Fatalf("API Taskfile exists for no-API selection: %v", err)
				}
				return
			}
			taskfile := readGeneratedAPIFile(t, filepath.Join(destination, "apis"), "Taskfile.yml")
			wantTasks := []string{"build", "run", "test", "coverage", "test:race", "test:fuzz", "format:check", "lint", "vuln", "vet", "mod", "openapi", "container:build", "container:build:production", "container:verify"}
			if tt.database {
				wantTasks = append(wantTasks, "test:integration")
			}
			wantTasks = append(wantTasks, "verify")
			if got := taskfileTaskNames(taskfile); strings.Join(got, ",") != strings.Join(wantTasks, ",") {
				t.Fatalf("Taskfile tasks=%v, want %v:\n%s", got, wantTasks, taskfile)
			}
			for _, want := range []string{
				"dotenv: ['.env']",
				"build:\n    cmds:\n      - mkdir -p bin && go build -trimpath -o bin/apis .",
				"run:\n    deps: [build]\n    cmds:\n      - ./bin/apis",
				"test:\n    cmds:\n      - go test ./...",
				"coverage:\n    cmds:\n      - go test ./... -coverprofile=coverage.out",
				"mod:\n    cmds:\n      - go mod verify",
				"go run ./cmd/openapi",
				"cmp -s",
				"verify:\n    deps: [format:check, lint, vuln, vet, build, test, mod, openapi, container:verify]\n",
			} {
				if !strings.Contains(taskfile, want) {
					t.Fatalf("Taskfile missing %q:\n%s", want, taskfile)
				}
			}
			if tt.database {
				for _, want := range []string{
					"test:integration:\n    preconditions:\n      - sh: test -n \"$DATABASE_URL\"",
					"go test -tags=integration ./...",
				} {
					if !strings.Contains(taskfile, want) {
						t.Fatalf("API+Database Taskfile missing %q:\n%s", want, taskfile)
					}
				}
			}
			verify := taskfileTaskBlock(taskfile, "verify")
			for _, forbidden := range []string{"go test ./...", "go mod verify", "go run ./cmd/openapi", "cp .env.example .env", "go install", "go get", "migrate"} {
				if forbidden == "go test ./..." || forbidden == "go mod verify" || forbidden == "go run ./cmd/openapi" {
					if strings.Contains(verify, forbidden) {
						t.Fatalf("verify duplicates dependency recipe %q:\n%s", forbidden, verify)
					}
					continue
				}
				if strings.Contains(taskfile, forbidden) {
					t.Fatalf("Taskfile contains forbidden %q:\n%s", forbidden, taskfile)
				}
			}
			if strings.Contains(taskfile, "- go run .\n") {
				t.Fatalf("Taskfile contains the obsolete standalone go run recipe:\n%s", taskfile)
			}
			server := readGeneratedAPIFile(t, filepath.Join(destination, "apis"), filepath.Join("internal", "server", "server.go"))
			if !strings.Contains(server, "github.com/caarlos0/env/v11") {
				t.Fatalf("generated server does not use caarlos0/env/v11")
			}
			manifest := readGeneratedAPIFile(t, filepath.Join(destination, "apis"), "go.mod")
			checksums := readGeneratedAPIFile(t, filepath.Join(destination, "apis"), "go.sum")
			for _, want := range []string{
				"github.com/caarlos0/env/v11 v11.4.1",
				"github.com/caarlos0/env/v11 v11.4.1 h1:fYwH0sWEsBSMPG7t4e/PEfTFzrWrpjyygXyUnWiSwEw=",
				"github.com/caarlos0/env/v11 v11.4.1/go.mod h1:qupehSf/Y0TUTsxKywqRt/vJjN5nz6vauiYEUUr8P4U=",
			} {
				contents := manifest
				if strings.Contains(want, " h1:") {
					contents = checksums
				}
				if !strings.Contains(contents, want) {
					t.Fatalf("generated API files missing %q", want)
				}
			}
		})
	}
}

func TestApplyGeneratedAPIIgnoresCoverageArtifacts(t *testing.T) {
	destination := applyAPIWorkspace(t, apiBlueprintBytes())
	ignore := readGeneratedAPIFile(t, filepath.Join(destination, "apis"), ".gitignore")
	for _, want := range []string{"\nbin/\n", "\ntmp/\n", "\n.env\n", "\ncoverage.out\n", "\ncoverage.html\n"} {
		if !strings.Contains(ignore, want) {
			t.Fatalf("generated API .gitignore missing %q:\n%s", want, ignore)
		}
	}
}

func TestGeneratedAPITaskfileCommandsAndDotenvRuntime(t *testing.T) {
	taskPath := generatedTaskBinary(t)
	destination := applyAPIWorkspace(t, apiBlueprintBytes())
	apiRoot := filepath.Join(destination, "apis")
	taskEnv := generatedTaskEnvironment()
	for _, taskName := range []string{"build", "mod", "format:check", "test", "coverage", "openapi"} {
		runGeneratedTask(t, taskPath, apiRoot, taskName, taskEnv)
	}
	for _, relative := range []string{"bin/apis", "coverage.out"} {
		if _, err := os.Stat(filepath.Join(apiRoot, relative)); err != nil {
			t.Fatalf("task %s did not create %s: %v", taskNameForArtifact(relative), relative, err)
		}
	}
	openapiBefore, err := os.ReadFile(filepath.Join(apiRoot, "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	runGeneratedTask(t, taskPath, apiRoot, "openapi", taskEnv)
	openapiAfter, err := os.ReadFile(filepath.Join(apiRoot, "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(openapiBefore, openapiAfter) {
		t.Fatal("task openapi modified openapi.yaml")
	}

	port := reserveTCPPort(t)
	if err := os.WriteFile(filepath.Join(apiRoot, ".env"), []byte("HTTP_ADDR="+port+"\nAPP_ENV=test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, taskPath, "run")
	cmd.Dir = apiRoot
	cmd.Env = taskEnv
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start task run: %v", err)
	}
	defer func() {
		if err := stopGeneratedTask(t, cmd); err != nil {
			t.Errorf("stop task run: %v\n%s", err, output.String())
		}
	}()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	baseURL := "http://" + port
	waitForHTTP(t, client, baseURL+"/healthz")
	assertRuntimeResponse(t, client, baseURL+"/healthz", "", http.StatusOK, `"status":"ok"`)
	if output.Len() > 0 && strings.Contains(output.String(), ":8080") {
		t.Fatalf("task run appears to have used the default port instead of .env port %s:\n%s", port, output.String())
	}
	if err := stopGeneratedTask(t, cmd); err != nil {
		t.Fatalf("task run did not stop cleanly: %v\n%s", err, output.String())
	}
}

func generatedTaskBinary(t *testing.T) string {
	t.Helper()
	if asdfPath, err := exec.LookPath("asdf"); err == nil {
		cmd := exec.Command(asdfPath, "which", "task")
		if output, err := cmd.Output(); err == nil {
			if path := strings.TrimSpace(string(output)); path != "" {
				return path
			}
		}
	}
	path, err := exec.LookPath("task")
	if err != nil {
		t.Fatalf("task executable is required for generated Taskfile harness: %v", err)
	}
	return path
}

func generatedTaskEnvironment() []string {
	return generatedEnvironmentWithoutConfig()
}

func generatedEnvironmentWithoutConfig() []string {
	configKeys := map[string]struct{}{
		"HTTP_ADDR":                {},
		"APP_ENV":                  {},
		"LOG_LEVEL":                {},
		"HTTP_READ_TIMEOUT":        {},
		"HTTP_READ_HEADER_TIMEOUT": {},
		"HTTP_WRITE_TIMEOUT":       {},
		"HTTP_IDLE_TIMEOUT":        {},
		"HTTP_MAX_HEADER_BYTES":    {},
		"HTTP_SHUTDOWN_TIMEOUT":    {},
	}
	environment := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, excluded := configKeys[key]; excluded {
				continue
			}
		}
		if strings.HasPrefix(entry, "HTTP_ADDR=") || strings.HasPrefix(entry, "APP_ENV=") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, "GOCACHE=/private/tmp/smt-gocache", "GOPROXY=off", "GOSUMDB=off")
}

func runGeneratedTask(t *testing.T, taskPath, apiRoot, taskName string, environment []string) {
	t.Helper()
	cmd := exec.Command(taskPath, taskName)
	cmd.Dir = apiRoot
	cmd.Env = environment
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("task %s: %v\n%s", taskName, err, output)
	}
}

func stopGeneratedTask(t *testing.T, cmd *exec.Cmd) error {
	t.Helper()
	if cmd.Process == nil || cmd.ProcessState != nil {
		return nil
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return <-done
	}
}

func taskNameForArtifact(relative string) string {
	if relative == "bin/apis" {
		return "build"
	}
	return "coverage"
}

func TestGeneratedAPIConfigUsesTypedEnvironmentParsing(t *testing.T) {
	destination := applyAPIWorkspace(t, apiBlueprintBytes())
	apiRoot := filepath.Join(destination, "apis")
	server := readGeneratedAPIFile(t, apiRoot, filepath.Join("internal", "server", "server.go"))
	if !strings.Contains(server, "github.com/caarlos0/env/v11") {
		t.Fatalf("generated server does not use typed caarlos environment parsing")
	}
	configStart := strings.Index(server, "type Config struct {")
	if configStart == -1 {
		t.Fatalf("generated server has no public Config declaration")
	}
	configEnd := strings.Index(server[configStart:], "\n}")
	if configEnd == -1 {
		t.Fatalf("generated Config declaration is unterminated")
	}
	configBlock := server[configStart : configStart+configEnd]
	for _, tag := range []string{
		`HTTPAddr           string        ` + "`env:\"HTTP_ADDR\" envDefault:\":8080\"`" + ``,
		`AppEnv             string        ` + "`env:\"APP_ENV\" envDefault:\"development\"`" + ``,
		`LogLevel           slog.Level    ` + "`env:\"LOG_LEVEL\" envDefault:\"info\"`" + ``,
		`ReadTimeout        time.Duration ` + "`env:\"HTTP_READ_TIMEOUT\" envDefault:\"15s\"`" + ``,
		`ReadHeaderTimeout  time.Duration ` + "`env:\"HTTP_READ_HEADER_TIMEOUT\" envDefault:\"5s\"`" + ``,
		`WriteTimeout       time.Duration ` + "`env:\"HTTP_WRITE_TIMEOUT\" envDefault:\"15s\"`" + ``,
		`IdleTimeout        time.Duration ` + "`env:\"HTTP_IDLE_TIMEOUT\" envDefault:\"60s\"`" + ``,
		`MaxHeaderBytes     int           ` + "`env:\"HTTP_MAX_HEADER_BYTES\" envDefault:\"1048576\"`" + ``,
		`ShutdownTimeout    time.Duration ` + "`env:\"HTTP_SHUTDOWN_TIMEOUT\" envDefault:\"10s\"`" + ``,
		`OIDCIssuerURL      string        ` + "`env:\"OIDC_ISSUER_URL\"`" + ``,
		`OIDCDiscoveryURL   string        ` + "`env:\"OIDC_DISCOVERY_URL\"`" + ``,
		`OIDCJWKSURL        string        ` + "`env:\"OIDC_JWKS_URL\"`" + ``,
		`OIDCAudience       string        ` + "`env:\"OIDC_AUDIENCE\"`" + ``,
		`OIDCRequiredScopes string        ` + "`env:\"OIDC_REQUIRED_SCOPES\" envDefault:\"openid,profile,email\"`" + ``,
	} {
		if !strings.Contains(configBlock, tag) {
			t.Fatalf("generated Config is missing direct field tag %q:\n%s", tag, configBlock)
		}
	}
	if !strings.Contains(server, "func LoadConfig() (Config, error)") {
		t.Fatalf("generated server has the wrong LoadConfig signature")
	}
	for _, old := range []string{
		"func LoadConfig(getenv",
		"type configValues",
		"configEnvironment",
		`"reflect"`,
		"FuncMap",
		"configParseError",
		"parsePositiveDuration",
		"parsePositiveInt",
		"parseLogLevel",
	} {
		if strings.Contains(server, old) {
			t.Fatalf("generated server still contains removed config helper %q", old)
		}
	}
	if !strings.Contains(server, "return cfg, err") {
		t.Fatalf("generated LoadConfig should return the parsed cfg and error directly")
	}
	for _, marker := range []string{
		`Error("configuration load failed"`,
		"panic(err)",
	} {
		if !strings.Contains(server, marker) {
			t.Fatalf("generated server missing configuration failure behavior %q", marker)
		}
	}
	configTest := `package server

import (
	"log/slog"
	"testing"
	"time"
)

func TestGeneratedConfigContract(t *testing.T) {
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
	defaults, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if defaults.HTTPAddr != ":8080" || defaults.AppEnv != "development" || defaults.LogLevel != slog.LevelInfo ||
		defaults.ReadTimeout != 15*time.Second || defaults.ReadHeaderTimeout != 5*time.Second ||
		defaults.WriteTimeout != 15*time.Second || defaults.IdleTimeout != 60*time.Second ||
		defaults.MaxHeaderBytes != 1048576 || defaults.ShutdownTimeout != 10*time.Second {
		t.Fatalf("unexpected defaults: %#v", defaults)
	}
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("APP_ENV", "test")
	t.Setenv("LOG_LEVEL", "warn")
	t.Setenv("HTTP_READ_TIMEOUT", "1s")
	t.Setenv("HTTP_READ_HEADER_TIMEOUT", "2s")
	t.Setenv("HTTP_WRITE_TIMEOUT", "3s")
	t.Setenv("HTTP_IDLE_TIMEOUT", "4s")
	t.Setenv("HTTP_MAX_HEADER_BYTES", "2048")
	t.Setenv("HTTP_SHUTDOWN_TIMEOUT", "5s")
	override, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if override.HTTPAddr != ":9090" || override.AppEnv != "test" || override.LogLevel != slog.LevelWarn ||
		override.ReadTimeout != time.Second || override.ReadHeaderTimeout != 2*time.Second ||
		override.WriteTimeout != 3*time.Second || override.IdleTimeout != 4*time.Second ||
		override.MaxHeaderBytes != 2048 || override.ShutdownTimeout != 5*time.Second {
		t.Fatalf("unexpected overrides: %#v", override)
	}
	for _, test := range []struct {
		key, value string
	}{
		{"LOG_LEVEL", "trace"},
		{"HTTP_READ_TIMEOUT", "nope"},
		{"HTTP_MAX_HEADER_BYTES", "nope"},
	} {
		t.Run(test.key+"="+test.value, func(t *testing.T) {
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
			t.Setenv(test.key, test.value)
			if _, err := LoadConfig(); err == nil {
				t.Fatalf("LoadConfig succeeded for malformed %s=%q", test.key, test.value)
			}
		})
	}
}
`
	testPath := filepath.Join(apiRoot, "internal", "server", "generated_config_contract_test.go")
	if err := os.WriteFile(testPath, []byte(configTest), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "./internal/server", "-run", "TestGeneratedConfigContract", "-count=1")
	cmd.Dir = apiRoot
	cmd.Env = generatedEnvironmentWithoutConfig()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated config test: %v\n%s", err, output)
	}
}

func TestGeneratedAPIConfigFailurePanicsAndLogs(t *testing.T) {
	destination := applyAPIWorkspace(t, apiBlueprintBytes())
	apiRoot := filepath.Join(destination, "apis")
	binary := buildGeneratedAPIBinary(t, apiRoot)
	cmd := exec.Command(binary)
	cmd.Env = append(generatedEnvironmentWithoutConfig(), "LOG_LEVEL=trace")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("generated API unexpectedly started with invalid configuration: %s", output)
	}
	for _, want := range []string{
		`"msg":"configuration load failed"`,
		"LogLevel",
		"panic: env: parse error",
	} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("invalid generated configuration output missing %q:\n%s", want, output)
		}
	}
}

func taskfileTaskNames(contents string) []string {
	var names []string
	inTasks := false
	for _, line := range strings.Split(contents, "\n") {
		if line == "tasks:" {
			inTasks = true
			continue
		}
		if !inTasks || !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "    ") || !strings.HasSuffix(line, ":") {
			continue
		}
		names = append(names, strings.TrimSuffix(strings.TrimSpace(line), ":"))
	}
	return names
}

func taskfileTaskBlock(contents, name string) string {
	lines := strings.Split(contents, "\n")
	start := -1
	for index, line := range lines {
		if line == "  "+name+":" {
			start = index
			break
		}
	}
	if start == -1 {
		return ""
	}
	end := len(lines)
	for index := start + 1; index < len(lines); index++ {
		if strings.HasPrefix(lines[index], "  ") && !strings.HasPrefix(lines[index], "    ") && strings.HasSuffix(lines[index], ":") {
			end = index
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

func TestApplyAPIDatabaseManifestBuildsOffline(t *testing.T) {
	destination := applyAPIWorkspace(t, fullMobileBlueprintBytes())
	apiRoot := filepath.Join(destination, "apis")
	manifest, err := os.ReadFile(filepath.Join(apiRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(manifest, []byte("github.com/jackc/pgx/v5 v5.10.0")) {
		t.Fatalf("API+Database manifest is missing pgx: %s", manifest)
	}
	if !bytes.Contains(manifest, []byte("github.com/golang-migrate/migrate/v4 v4.19.1")) {
		t.Fatalf("API+Database manifest is missing migrate: %s", manifest)
	}
	for _, module := range []string{
		"github.com/jackc/pgpassfile",
		"github.com/jackc/pgservicefile",
	} {
		count := 0
		for _, line := range strings.Split(string(manifest), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), module+" ") {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("API+Database manifest requires %s %d times, want exactly once:\n%s", module, count, manifest)
		}
	}
	_ = buildGeneratedAPIBinary(t, apiRoot)
}

func TestOpenAPICommandRegeneratesFromSharedHumaAPI(t *testing.T) {
	destination := applyAPIWorkspace(t, apiBlueprintBytes())
	apiRoot := filepath.Join(destination, "apis")
	committed, err := os.ReadFile(filepath.Join(apiRoot, "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	generated := runOpenAPICommand(t, apiRoot)
	if !bytes.Equal(generated, committed) {
		t.Fatalf("cmd/openapi output differs from openapi.yaml:\nwant=%s\ngot=%s", committed, generated)
	}
	mutated := append([]byte(nil), committed...)
	mutated = append(mutated, []byte("# mutation must not be copied\n")...)
	if err := os.WriteFile(filepath.Join(apiRoot, "openapi.yaml"), mutated, 0o644); err != nil {
		t.Fatal(err)
	}
	regenerated := runOpenAPICommand(t, apiRoot)
	if bytes.Contains(regenerated, []byte("mutation must not be copied")) {
		t.Fatalf("cmd/openapi copied openapi.yaml instead of regenerating it: %s", regenerated)
	}
	if !bytes.Equal(regenerated, generated) {
		t.Fatalf("cmd/openapi output changed after mutating openapi.yaml")
	}
}

func TestGeneratedHumaOpenAPIDocumentsMetricsAsText(t *testing.T) {
	destination := applyAPIWorkspace(t, apiBlueprintBytes())
	apiRoot := filepath.Join(destination, "apis")
	port := reserveTCPPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	binary := buildGeneratedAPIBinary(t, apiRoot)
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "HTTP_ADDR="+port)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start generated API: %v", err)
	}
	defer stopGeneratedAPI(t, cmd)

	client := &http.Client{Timeout: 500 * time.Millisecond}
	baseURL := "http://" + port
	waitForHTTP(t, client, baseURL+"/healthz")
	request, err := http.NewRequest(http.MethodGet, baseURL+"/openapi.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var document struct {
		Paths map[string]struct {
			Get struct {
				Responses map[string]struct {
					Content map[string]json.RawMessage `json:"content"`
				} `json:"responses"`
			} `json:"get"`
		} `json:"paths"`
	}
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		t.Fatal(err)
	}
	metrics, ok := document.Paths["/metrics"]
	if !ok {
		t.Fatalf("generated Huma OpenAPI document has no /metrics operation: %#v", document.Paths)
	}
	if _, ok := metrics.Get.Responses["200"].Content["text/plain"]; !ok {
		t.Fatalf("generated /metrics operation has no text/plain response: %#v", metrics.Get.Responses)
	}
}

func runOpenAPICommand(t *testing.T, apiRoot string) []byte {
	t.Helper()
	cmd := exec.Command("go", "run", "./cmd/openapi")
	cmd.Dir = apiRoot
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run cmd/openapi: %v\n%s", err, output)
	}
	return output
}

func TestGeneratedAPIRuntimeHarness(t *testing.T) {
	destination := applyAPIWorkspace(t, apiBlueprintBytes())
	apiRoot := filepath.Join(destination, "apis")
	if _, err := os.Stat(filepath.Join(apiRoot, "main.go")); err != nil {
		t.Fatalf("generated API main.go is missing: %v", err)
	}
	port := reserveTCPPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	binary := buildGeneratedAPIBinary(t, apiRoot)
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = append(os.Environ(),
		"PATH="+os.Getenv("PATH"),
		"GOPROXY=off",
		"GOSUMDB=off",
		"HTTP_ADDR="+port,
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start generated API: %v", err)
	}
	defer stopGeneratedAPI(t, cmd)

	baseURL := "http://" + port
	client := &http.Client{Timeout: 500 * time.Millisecond}
	waitForHTTP(t, client, baseURL+"/healthz")
	assertRuntimeResponse(t, client, baseURL+"/healthz", "", http.StatusOK, `"status":"ok"`)
	assertRuntimeResponse(t, client, baseURL+"/readyz", "", http.StatusOK, `"status":"ready"`)
	assertRuntimeResponse(t, client, baseURL+"/metrics", "", http.StatusOK, "go_", "smt_http_")
	assertRuntimeResponse(t, client, baseURL+"/docs", "", http.StatusOK, "SMT API")
	assertRuntimeResponse(t, client, baseURL+"/openapi.json", "request-123", http.StatusOK, "SMT API", "request-123")
	assertRuntimeResponse(t, client, baseURL+"/openapi.yaml", "", http.StatusOK, "openapi: 3.1.0")

	if err := stopGeneratedAPI(t, cmd); err != nil {
		t.Fatalf("generated API did not shut down cleanly: %v\n%s", err, output.String())
	}
}

func buildGeneratedAPIBinary(t *testing.T, apiRoot string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "smt-api")
	cmd := exec.Command("go", "build", "-o", binary, ".")
	cmd.Dir = apiRoot
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build generated API: %v\n%s", err, output)
	}
	return binary
}

func stopGeneratedAPI(t *testing.T, cmd *exec.Cmd) error {
	t.Helper()
	if cmd.Process == nil || cmd.ProcessState != nil {
		return nil
	}
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		return <-done
	}
}

func applyAPIWorkspace(t *testing.T, raw []byte) string {
	t.Helper()
	if bytes.Contains(raw, []byte("mobile: flutter")) {
		installFakeASDF(t, false)
	} else if bytes.Contains(raw, []byte("web: nextjs")) {
		installFakeNextASDF(t, false)
	}
	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	cfg, err := config.LoadBytes(raw, filepath.Join(parent, "blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		Config:        *cfg,
		Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
		Beads:         initializerFunc(func(context.Context, string) error { return nil }),
	}
	if err := service.Apply(context.Background(), destination, raw); err != nil {
		t.Fatal(err)
	}
	return destination
}

func readGeneratedAPIFile(t *testing.T, apiRoot, relative string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(apiRoot, relative))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func reserveTCPPort(t *testing.T) string {
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

func waitForHTTP(t *testing.T, client *http.Client, endpoint string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		response, err := client.Get(endpoint)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			return
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("generated API did not become reachable: %v", lastErr)
}

func assertRuntimeResponse(t *testing.T, client *http.Client, endpoint, requestID string, wantStatus int, wants ...string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	if requestID != "" {
		request.Header.Set("X-Request-ID", requestID)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("GET %s: %v", endpoint, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("GET %s status=%d, want %d body=%s", endpoint, response.StatusCode, wantStatus, body)
	}
	for _, want := range wants {
		if !bytes.Contains(body, []byte(want)) && response.Header.Get("X-Request-ID") != want {
			t.Fatalf("GET %s missing %q in body/header: body=%s headers=%v", endpoint, want, body, response.Header)
		}
	}
	if requestID != "" && response.Header.Get("X-Request-ID") != requestID {
		t.Fatalf("GET %s request ID=%q, want %q", endpoint, response.Header.Get("X-Request-ID"), requestID)
	}
}
