package apply

import (
	"fmt"
	"strings"
)

const (
	osvScannerVersion = "2.4.0"
	gitleaksVersion   = "8.30.1"
)

func rootSecurityTaskfile(webSelected, mobileSelected, apiSelected, databaseSelected, identitySelected bool) string {
	var builder strings.Builder
	builder.WriteString("  security:\n")
	builder.WriteString("    deps: [security:static, security:dependencies, security:secrets]\n")

	builder.WriteString("  security:static:\n")
	builder.WriteString("    preconditions:\n")
	builder.WriteString("      - sh: test -f \"{{.ROOT_DIR}}/scripts/verify-security.sh\"\n")
	builder.WriteString("        msg: generated security verification script is required before running security checks\n")
	writeSecurityStaticPreconditions(&builder, webSelected, mobileSelected, apiSelected, databaseSelected, identitySelected)
	builder.WriteString("    cmds:\n")
	builder.WriteString("      - sh \"{{.ROOT_DIR}}/scripts/verify-security.sh\"\n")

	builder.WriteString("  security:dependencies:\n")
	if webSelected || mobileSelected || apiSelected {
		builder.WriteString("    preconditions:\n")
		builder.WriteString("      - sh: command -v osv-scanner\n")
		builder.WriteString("        msg: OSV-Scanner is required; install version " + osvScannerVersion + " before running task security\n")
		builder.WriteString("      - sh: osv-scanner --version 2>&1 | grep -Eq '(^|[^0-9])" + osvScannerVersion + "([^0-9]|$)'\n")
		builder.WriteString("        msg: OSV-Scanner " + osvScannerVersion + " is required; upgrade the active binary before running task security\n")
	}
	builder.WriteString("    cmds:\n")
	dependencyCommands := 0
	if webSelected {
		writeTaskCommand(&builder, `osv-scanner scan source --lockfile="{{.ROOT_DIR}}/web-app/pnpm-lock.yaml"`)
		dependencyCommands++
	}
	if mobileSelected {
		writeTaskCommand(&builder, `osv-scanner scan source --lockfile="{{.ROOT_DIR}}/mobile-app/pubspec.lock"`)
		dependencyCommands++
	}
	if apiSelected {
		writeTaskCommand(&builder, `osv-scanner scan source --lockfile="{{.ROOT_DIR}}/apis/go.mod"`)
		dependencyCommands++
	}
	if dependencyCommands == 0 {
		builder.WriteString("      - echo \"no application dependency lockfiles selected\"\n")
	}

	builder.WriteString("  security:secrets:\n")
	builder.WriteString("    preconditions:\n")
	builder.WriteString("      - sh: command -v gitleaks\n")
	builder.WriteString("        msg: Gitleaks is required; install version " + gitleaksVersion + " before running task security\n")
	builder.WriteString("      - sh: gitleaks version 2>&1 | grep -Eq '(^|[^0-9])" + gitleaksVersion + "([^0-9]|$)'\n")
	builder.WriteString("        msg: Gitleaks " + gitleaksVersion + " is required; upgrade the active binary before running task security\n")
	builder.WriteString("    cmds:\n")
	builder.WriteString("      - gitleaks dir --redact --no-banner --exit-code 1 \"{{.ROOT_DIR}}\"\n")
	return builder.String()
}

func writeSecurityStaticPreconditions(builder *strings.Builder, webSelected, mobileSelected, apiSelected, databaseSelected, identitySelected bool) {
	if webSelected {
		builder.WriteString("      - sh: test -f \"{{.ROOT_DIR}}/web-app/pnpm-lock.yaml\"\n")
		builder.WriteString("        msg: Web pnpm-lock.yaml is required for security verification\n")
		builder.WriteString("      - sh: test -f \"{{.ROOT_DIR}}/web-app/Containerfile\"\n")
		builder.WriteString("        msg: Web Containerfile is required for security verification\n")
	}
	if mobileSelected {
		builder.WriteString("      - sh: test -f \"{{.ROOT_DIR}}/mobile-app/pubspec.lock\"\n")
		builder.WriteString("        msg: Mobile pubspec.lock is required for security verification\n")
	}
	if apiSelected {
		builder.WriteString("      - sh: test -f \"{{.ROOT_DIR}}/apis/go.mod\"\n")
		builder.WriteString("        msg: API go.mod is required for security verification\n")
		builder.WriteString("      - sh: test -f \"{{.ROOT_DIR}}/apis/go.sum\"\n")
		builder.WriteString("        msg: API go.sum is required for security verification\n")
		builder.WriteString("      - sh: test -f \"{{.ROOT_DIR}}/apis/Containerfile\"\n")
		builder.WriteString("        msg: API Containerfile is required for security verification\n")
	}
	if databaseSelected {
		builder.WriteString("      - sh: test -f \"{{.ROOT_DIR}}/database/Containerfile\"\n")
		builder.WriteString("        msg: Database Containerfile is required for security verification\n")
	}
	if webSelected || apiSelected || databaseSelected || identitySelected {
		builder.WriteString("      - sh: test -f \"{{.ROOT_DIR}}/compose.yaml\"\n")
		builder.WriteString("        msg: root compose.yaml is required for security verification\n")
	}
}

func securityVerificationScript(webSelected, mobileSelected, apiSelected, databaseSelected, identitySelected bool) string {
	var builder strings.Builder
	builder.WriteString(`#!/bin/sh
set -eu

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

fail() {
  printf 'security: %s\n' "$1" >&2
  exit 1
}

require_file() {
  file=$1
  label=$2
  test -f "$file" || fail "$label is required"
}

check_containerfile() {
  file=$1
  expected_user=$2
  label=$3
  require_file "$file" "$label Containerfile"
  if grep -Eq '^[[:space:]]*USER[[:space:]]+root([[:space:]]|$)' "$file"; then
    fail "$label Containerfile must not run as root"
  fi
	if [ -n "$expected_user" ] && ! grep -Eq "^[[:space:]]*USER[[:space:]]+$expected_user([[:space:]]|$)" "$file"; then
    fail "$label Containerfile must declare runtime user $expected_user"
  fi
  if awk '
    $1 == "FROM" {
      image = $2
      if (image == "scratch") next
      if (image !~ /@sha256:/ && image !~ /:[0-9]/) exit 1
    }
  ' "$file"; then
    :
  else
    fail "$label Containerfile contains an unpinned image reference"
  fi
}

check_compose_images() {
  file=$1
  if awk '
    $1 == "image:" {
      image = $2
	      gsub(/"/, "", image)
      if (image ~ /:latest([}]|$)/ || (image !~ /@sha256:/ && image !~ /:[vV]?[0-9]/)) exit 1
    }
  ' "$file"; then
    :
  else
    fail "Compose contains an unpinned image reference"
  fi
}

`)

	if webSelected {
		builder.WriteString("require_file \"$root_dir/web-app/pnpm-lock.yaml\" \"Web pnpm-lock.yaml\"\n")
		builder.WriteString("check_containerfile \"$root_dir/web-app/Containerfile\" nextjs Web\n")
	}
	if mobileSelected {
		builder.WriteString("require_file \"$root_dir/mobile-app/pubspec.lock\" \"Mobile pubspec.lock\"\n")
	}
	if apiSelected {
		builder.WriteString("require_file \"$root_dir/apis/go.mod\" \"API go.mod\"\n")
		builder.WriteString("require_file \"$root_dir/apis/go.sum\" \"API go.sum\"\n")
		builder.WriteString("check_containerfile \"$root_dir/apis/Containerfile\" '10001:10001' API\n")
	}
	if databaseSelected {
		builder.WriteString("check_containerfile \"$root_dir/database/Containerfile\" '' Database\n")
	}

	ociSelected := webSelected || apiSelected || databaseSelected || identitySelected
	if ociSelected {
		services := 0
		if webSelected {
			services++
		}
		if apiSelected {
			services++
		}
		if databaseSelected {
			services++
		}
		if identitySelected {
			services += 4
		}
		builder.WriteString("require_file \"$root_dir/compose.yaml\" \"root compose.yaml\"\n")
		builder.WriteString("check_compose_images \"$root_dir/compose.yaml\"\n")
		builder.WriteString(`if grep -Eq '^[[:space:]]*privileged:[[:space:]]*' "$root_dir/compose.yaml"; then
  fail "Compose must not declare privileged services"
fi
if grep -Eq '^[[:space:]]*user:[[:space:]]*root([[:space:]]|$)' "$root_dir/compose.yaml"; then
  fail "Compose services must not run as root"
fi
security_opt_count=$(grep -c 'no-new-privileges:true' "$root_dir/compose.yaml" || true)
test "$security_opt_count" -eq `)
		builder.WriteString(fmt.Sprintf("%d", services))
		builder.WriteString(` || fail "every generated Compose service must require no-new-privileges"
`)
	}

	builder.WriteString("printf 'security static checks passed\\n'\n")
	builder.WriteString("# Secret findings are produced by Gitleaks with --redact in task security:secrets.\n")
	return builder.String()
}
