package apply

import "strings"

func rootTaskfileForSelection(webSelected, mobileSelected, apiSelected, databaseSelected bool, identitySelected ...bool) string {
	taskfile := rootComposeTaskfileBase + rootVerificationTaskfile(webSelected, mobileSelected, apiSelected, databaseSelected)
	if webSelected || apiSelected || databaseSelected || (len(identitySelected) > 0 && identitySelected[0]) {
		taskfile += rootComposeTaskfileTasksForDatabase(databaseSelected, identitySelected...)
	}
	return taskfile
}

func rootVerificationTaskfile(webSelected, mobileSelected, apiSelected, databaseSelected bool) string {
	var builder strings.Builder
	builder.WriteString("  verify:fast:\n")
	writeVerificationPreconditions(&builder, webSelected, mobileSelected, apiSelected, false)
	builder.WriteString("    cmds:\n")
	fastCommands := 0
	if webSelected {
		writeTaskCommand(&builder, `cd "{{.ROOT_DIR}}/web-app" && asdf exec pnpm run verify:fast`)
		fastCommands++
	}
	if mobileSelected {
		writeTaskCommand(&builder, `cd "{{.ROOT_DIR}}/mobile-app" && asdf exec dart format --output=none --set-exit-if-changed lib test integration_test`)
		writeTaskCommand(&builder, `cd "{{.ROOT_DIR}}/mobile-app" && asdf exec flutter analyze`)
		fastCommands += 2
	}
	if apiSelected {
		writeTaskCommand(&builder, `cd "{{.ROOT_DIR}}/apis" && task verify:fast`)
		fastCommands++
	}
	if fastCommands == 0 {
		builder.WriteString("      - echo \"no fast component checks selected\"\n")
	}

	builder.WriteString("  verify:\n")
	writeVerificationPreconditions(&builder, webSelected, mobileSelected, apiSelected, databaseSelected)
	builder.WriteString("    cmds:\n")
	fullCommands := 0
	if webSelected {
		writeTaskCommand(&builder, `cd "{{.ROOT_DIR}}/web-app" && asdf exec pnpm run verify`)
		fullCommands++
	}
	if mobileSelected {
		writeTaskCommand(&builder, `cd "{{.ROOT_DIR}}/mobile-app" && asdf exec dart format --output=none --set-exit-if-changed lib test integration_test`)
		writeTaskCommand(&builder, `cd "{{.ROOT_DIR}}/mobile-app" && asdf exec flutter analyze`)
		writeTaskCommand(&builder, `cd "{{.ROOT_DIR}}/mobile-app" && asdf exec flutter test`)
		fullCommands += 3
	}
	if apiSelected {
		writeTaskCommand(&builder, `cd "{{.ROOT_DIR}}/apis" && task verify`)
		fullCommands++
	}
	if databaseSelected {
		writeTaskCommand(&builder, `cd "{{.ROOT_DIR}}/database" && task verify`)
		fullCommands++
	}
	if fullCommands == 0 {
		builder.WriteString("      - echo \"no full component checks selected\"\n")
	}
	return builder.String()
}

func writeVerificationPreconditions(builder *strings.Builder, webSelected, mobileSelected, apiSelected, databaseSelected bool) {
	preconditions := make([]string, 0, 8)
	if webSelected {
		preconditions = append(preconditions,
			`      - sh: test -f "{{.ROOT_DIR}}/web-app/package.json"`+"\n"+"        msg: Web package.json is required before running verification\n",
			`      - sh: test -f "{{.ROOT_DIR}}/web-app/pnpm-lock.yaml"`+"\n"+"        msg: Web pnpm-lock.yaml is required; rerun smt apply or run (cd web-app && asdf exec pnpm install)\n",
			`      - sh: test -d "{{.ROOT_DIR}}/web-app/node_modules"`+"\n"+"        msg: Web dependencies are missing; rerun smt apply or run (cd web-app && asdf exec pnpm install)\n",
			"      - sh: asdf exec pnpm --version\n"+"        msg: pnpm is required through asdf; install or activate Node.js 24.18.0 before verification\n",
		)
	}
	if mobileSelected {
		preconditions = append(preconditions,
			"      - sh: test -d \"{{.ROOT_DIR}}/mobile-app\"\n"+"        msg: Mobile repository is required before running verification\n",
			"      - sh: asdf exec dart --version\n"+"        msg: Dart is required through asdf; install or activate Flutter 3.44.9-stable before verification\n",
			"      - sh: asdf exec flutter --version\n"+"        msg: Flutter is required through asdf; install or activate Flutter 3.44.9-stable before verification\n",
		)
	}
	if apiSelected {
		preconditions = append(preconditions,
			"      - sh: test -f \"{{.ROOT_DIR}}/apis/Taskfile.yml\"\n"+"        msg: API Taskfile is required before running verification\n",
			"      - sh: command -v task\n"+"        msg: Task is required to run API verification\n",
		)
	}
	if databaseSelected {
		preconditions = append(preconditions,
			"      - sh: test -f \"{{.ROOT_DIR}}/database/Taskfile.yml\"\n"+"        msg: Database Taskfile is required before running verification\n",
			"      - sh: command -v task\n"+"        msg: Task is required to run Database verification\n",
		)
	}
	if len(preconditions) == 0 {
		return
	}
	builder.WriteString("    preconditions:\n")
	for _, precondition := range preconditions {
		builder.WriteString(precondition)
	}
}

func writeTaskCommand(builder *strings.Builder, command string) {
	builder.WriteString("      - ")
	builder.WriteString(command)
	builder.WriteByte('\n')
}

func mergeTaskfile(base, extension string) string {
	if vars := taskfileVars(extension); vars != "" && !strings.Contains(base, "\nvars:\n") {
		marker := "\ntasks:\n"
		if index := strings.Index(base, marker); index >= 0 {
			base = base[:index] + "\nvars:\n" + vars + base[index:]
		}
	}
	return appendTaskfileTasks(base, extension)
}

func appendTaskfileTasks(base, extension string) string {
	marker := "\ntasks:\n"
	baseIndex := strings.Index(base, marker)
	if baseIndex < 0 {
		return base
	}
	baseBodyStart := baseIndex + len(marker)
	extensionBodyStart := 0
	if extensionIndex := strings.Index(extension, marker); extensionIndex >= 0 {
		extensionBodyStart = extensionIndex + len(marker)
	}
	extensionBody := extension[extensionBodyStart:]
	if strings.TrimSpace(extensionBody) == "" {
		return base
	}
	if !strings.HasSuffix(base, "\n") {
		base += "\n"
	}
	return base[:baseBodyStart] + base[baseBodyStart:] + extensionBody
}

func taskfileVars(taskfile string) string {
	varsMarker := "\nvars:\n"
	tasksMarker := "\ntasks:\n"
	varsIndex := strings.Index(taskfile, varsMarker)
	tasksIndex := strings.Index(taskfile, tasksMarker)
	if varsIndex < 0 || tasksIndex < varsIndex {
		return ""
	}
	return taskfile[varsIndex+len(varsMarker) : tasksIndex]
}
