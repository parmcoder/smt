package apply

import "strings"

const rootReadmeBase = "# Platform workspace\n\nStart with [the documentation workspace](docs/README.md). Agents also read `AGENTS.md`.\n\nBeads configuration is tracked with the workspace; its embedded Dolt database and local runtime files stay on this machine and are ignored by Git.\n"

const rootComposeTaskfileBase = `version: '3'

tasks:
`

const rootComposeTaskfileTasks = `  compose:config:
    preconditions:
      - sh: test -f "{{.ROOT_DIR}}/.env"
        msg: root .env is required; copy .env.example to .env and set the local Compose values before running this task
      - sh: command -v podman
        msg: Podman is required; install and configure it before running the root Compose task
    cmds:
      - podman compose --env-file "{{.ROOT_DIR}}/.env" -f "{{.ROOT_DIR}}/compose.yaml" config
  compose:build:
    preconditions:
      - sh: test -f "{{.ROOT_DIR}}/.env"
        msg: root .env is required; copy .env.example to .env and set the local Compose values before running this task
      - sh: command -v podman
        msg: Podman is required; install and configure it before running the root Compose task
    cmds:
      - podman compose --env-file "{{.ROOT_DIR}}/.env" -f "{{.ROOT_DIR}}/compose.yaml" build
  compose:up:
    preconditions:
      - sh: test -f "{{.ROOT_DIR}}/.env"
        msg: root .env is required; copy .env.example to .env and set the local Compose values before running this task
      - sh: command -v podman
        msg: Podman is required; install and configure it before running the root Compose task
__DATABASE_PASSWORD_PRECONDITION__    cmds:
      - podman compose --env-file "{{.ROOT_DIR}}/.env" -f "{{.ROOT_DIR}}/compose.yaml" up -d
  compose:down:
    preconditions:
      - sh: test -f "{{.ROOT_DIR}}/.env"
        msg: root .env is required; copy .env.example to .env and set the local Compose values before running this task
      - sh: command -v podman
        msg: Podman is required; install and configure it before running the root Compose task
    cmds:
      - podman compose --env-file "{{.ROOT_DIR}}/.env" -f "{{.ROOT_DIR}}/compose.yaml" down
  compose:ps:
    preconditions:
      - sh: test -f "{{.ROOT_DIR}}/.env"
        msg: root .env is required; copy .env.example to .env and set the local Compose values before running this task
      - sh: command -v podman
        msg: Podman is required; install and configure it before running the root Compose task
    cmds:
      - podman compose --env-file "{{.ROOT_DIR}}/.env" -f "{{.ROOT_DIR}}/compose.yaml" ps
`

func rootReadme(ociSelected bool) string {
	if !ociSelected {
		return rootReadmeBase
	}
	return rootReadmeBase + "\n## Local Compose\n\nThe root Compose workflow uses the operator-managed `.env` explicitly. Apply\ndoes not generate `.env`; initialize it from the examples. The example password\n`smt-dev-password` is for disposable local development only and must be replaced\nfor any shared or non-local environment:\n\n~~~sh\ncp .env.example .env\n# review or replace DATABASE_PASSWORD, then run:\ntask compose:config\ntask compose:build\ntask compose:up\n~~~\n\nUse `task compose:down` to stop and remove the Compose application. The root\n`.env` remains ignored and local-only.\n"
}

func rootComposeTaskfile(databaseSelected bool) string {
	return rootComposeTaskfileBase + rootComposeTaskfileTasksForDatabase(databaseSelected)
}

func rootComposeTaskfileTasksForDatabase(databaseSelected bool) string {
	passwordPrecondition := ""
	if databaseSelected {
		passwordPrecondition = `      - sh: grep -Eq '^[[:space:]]*DATABASE_PASSWORD[[:space:]]*=[[:space:]]*[^[:space:]#]' "{{.ROOT_DIR}}/.env"
        msg: root .env must set a non-empty DATABASE_PASSWORD before starting the database Compose service
`
	}
	return strings.ReplaceAll(rootComposeTaskfileTasks, "__DATABASE_PASSWORD_PRECONDITION__", passwordPrecondition)
}

func addRootComposeTasks(taskfile string, databaseSelected bool) string {
	return strings.Replace(taskfile, "\ntasks:\n", "\ntasks:\n"+rootComposeTaskfileTasksForDatabase(databaseSelected), 1)
}
