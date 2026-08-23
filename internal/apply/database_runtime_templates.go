package apply

import (
	"fmt"
	"path/filepath"
)

const databaseContainerfile = `FROM postgres:18-alpine

ENV POSTGRES_DB=smt
ENV POSTGRES_USER=smt

EXPOSE 5432
VOLUME ["/var/lib/postgresql"]
HEALTHCHECK --interval=5s --timeout=5s --start-period=5s --retries=10 CMD pg_isready -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"
`

const databaseEnvExample = `POSTGRES_DB=smt
POSTGRES_USER=smt
POSTGRES_PASSWORD=smt-dev-password
DATABASE_PORT=5432
DATABASE_VOLUME=smt-postgres-data
DATABASE_CONTAINER_NAME=smt-database
DATABASE_IMAGE=smt-database:local
`

const databaseTaskfile = `version: '3'

dotenv: ['.env']

tasks:
  build:
    preconditions:
      - sh: command -v podman
        msg: Podman is required; install and configure it before running task build
    cmds:
      - podman build --pull=missing --format=docker --file Containerfile --tag "${DATABASE_IMAGE:-smt-database:local}" .
  run:
    deps: [build]
    preconditions:
      - sh: test -n "${POSTGRES_PASSWORD:-}"
        msg: POSTGRES_PASSWORD is required; copy .env.example to .env and set a local value
    cmds:
      - |
        set -eu
        container="${DATABASE_CONTAINER_NAME:-smt-database}"
        image="${DATABASE_IMAGE:-smt-database:local}"
        port="${DATABASE_PORT:-5432}"
        volume="${DATABASE_VOLUME:-smt-postgres-data}"
        podman rm -f "$container" >/dev/null 2>&1 || true
        podman run --detach --name "$container" \
          --publish "127.0.0.1:${port}:5432" \
          --env "POSTGRES_DB=${POSTGRES_DB:-smt}" \
          --env "POSTGRES_USER=${POSTGRES_USER:-smt}" \
          --env "POSTGRES_PASSWORD=${POSTGRES_PASSWORD}" \
          --volume "${volume}:/var/lib/postgresql" \
          "$image"
  ready:
    preconditions:
      - sh: command -v podman
        msg: Podman is required; install and configure it before running task ready
    cmds:
      - |
        set -eu
        container="${DATABASE_CONTAINER_NAME:-smt-database}"
        database="${POSTGRES_DB:-smt}"
        user="${POSTGRES_USER:-smt}"
        if ! podman exec "$container" pg_isready -h 127.0.0.1 -U "$user" -d "$database"; then
          echo "PostgreSQL is not ready; inspect the container logs and verify the selected database settings" >&2
          exit 1
        fi
  psql:
    preconditions:
      - sh: command -v podman
        msg: Podman is required; install and configure it before running task diagnose
    cmds:
      - |
        set -eu
        container="${DATABASE_CONTAINER_NAME:-smt-database}"
        database="${POSTGRES_DB:-smt}"
        user="${POSTGRES_USER:-smt}"
        if ! output="$(podman exec --env "PGPASSWORD=${POSTGRES_PASSWORD:-}" "$container" psql --no-password --set=ON_ERROR_STOP=1 --tuples-only --host=127.0.0.1 --username="$user" --dbname="$database" --command='SELECT 1' 2>&1)"; then
          echo "PostgreSQL diagnostic failed; verify readiness and the local POSTGRES_* settings" >&2
          printf '%s\n' "$output" >&2
          exit 1
        fi
        if [ "$(printf '%s' "$output" | tr -d '[:space:]')" != "1" ]; then
          echo "PostgreSQL diagnostic returned an unexpected result" >&2
          printf '%s\n' "$output" >&2
          exit 1
        fi
  diagnose:
    deps: [psql]
  stop:
    preconditions:
      - sh: command -v podman
        msg: Podman is required; install and configure it before running task stop
    cmds:
      - podman stop --time 10 "${DATABASE_CONTAINER_NAME:-smt-database}"
  verify:
    deps: [run]
    cmds:
      - task: ready
      - task: diagnose
      - task: stop
`

const databaseReadme = `# PostgreSQL database

This repository is the independent PostgreSQL runtime for an SMT workspace. It
does not contain application schema, API code, or migration commands.

## Local runtime

The generated assets use PostgreSQL 18, expose port 5432 on localhost, and keep
data in the named Podman volume configured by DATABASE_VOLUME. The example
password is smt-dev-password for disposable local development only; replace
it outside a local environment. Copy the example file and run the readiness and
diagnostic checks explicitly:

~~~sh
cp .env.example .env
# review or replace POSTGRES_PASSWORD
task build
task run
task ready
task diagnose
task stop
~~~

The task ready lane uses pg_isready. The task diagnose lane runs a fail-fast
psql query with ON_ERROR_STOP=1; failures preserve the database command output and do
not attempt destructive recovery. The task verify lane runs the same checks and stops
the container afterward.

The workspace root owns Compose selection and API-to-Database dependencies.
This child only provides the database image and local readiness contract.
`

func databaseRuntimeFiles() map[string]string {
	return map[string]string{
		".env.example":  databaseEnvExample,
		"Containerfile": databaseContainerfile,
		"README.md":     databaseReadme,
		"Taskfile.yml":  databaseTaskfile,
	}
}

func writeDatabaseRuntimeFiles(directory string) error {
	for relative, contents := range databaseRuntimeFiles() {
		if err := writeFile(filepath.Join(directory, relative), contents); err != nil {
			return fmt.Errorf("write %s: %w", relative, err)
		}
	}
	return nil
}
