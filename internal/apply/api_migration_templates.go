package apply

const apiMigrationBaselineSQL = "SELECT 1;\n"

const apiValidateMigrationsSh = `#!/bin/sh
set -eu

: "${DATABASE_URL:?DATABASE_URL is required; set it in .env before validating migrations}"
go tool migrate -path migrations -database "$DATABASE_URL" up
go tool migrate -path migrations -database "$DATABASE_URL" version
`

func apiMigrationFiles() map[string]string {
	return map[string]string{
		"migrations/000001_baseline.up.sql":   apiMigrationBaselineSQL,
		"migrations/000001_baseline.down.sql": apiMigrationBaselineSQL,
		"scripts/validate-migrations.sh":      apiValidateMigrationsSh,
	}
}

func apiEnvExample(databaseSelected bool) string {
	if !databaseSelected {
		return apiEnvExampleBase
	}
	return "DATABASE_URL=\n" + apiEnvExampleBase
}
