package apply

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const webQualityESLintConfig = `import { defineConfig, globalIgnores } from "eslint/config";
import eslintConfigPrettier from "eslint-config-prettier/flat";
import nextTs from "eslint-config-next/typescript";
import nextVitals from "eslint-config-next/core-web-vitals";

export default defineConfig([
  ...nextVitals,
  ...nextTs,
  eslintConfigPrettier,
  globalIgnores([
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
    "playwright-report/**",
    "test-results/**",
  ]),
]);
`

const webQualityPrettierConfig = `{
  "printWidth": 100,
  "semi": true,
  "singleQuote": false,
  "trailingComma": "all"
}
`

const webQualityPrettierIgnore = `node_modules/
.next/
out/
build/
playwright-report/
test-results/
coverage/
next-env.d.ts
AGENTS.md
CLAUDE.md
`

const webQualityVitestConfig = `import react from "@vitejs/plugin-react";
import tsconfigPaths from "vite-tsconfig-paths";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react(), tsconfigPaths()],
  test: {
    environment: "jsdom",
    include: ["test/**/*.test.{ts,tsx}"],
    setupFiles: ["./test/setup.ts"],
  },
});
`

const webQualityTestSetup = `import "@testing-library/jest-dom/vitest";
`

const webQualitySmokeTest = `import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

describe("Web quality harness", () => {
  it("renders a React Testing Library smoke fixture in jsdom", () => {
    render(<main aria-label="quality harness">SMT quality harness</main>);

    expect(screen.getByRole("main", { name: "quality harness" })).toBeInTheDocument();
  });
});
`

const webQualityPlaywrightConfig = `import { defineConfig, devices } from "@playwright/test";

const baseURL = "http://127.0.0.1:3000";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  reporter: [["list"], ["html", { open: "never", outputFolder: "playwright-report" }]],
  use: {
    baseURL,
    trace: "on-first-retry",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
  webServer: {
    command: "npm run start",
    reuseExistingServer: !process.env.CI,
    timeout: 120000,
    url: baseURL,
  },
});
`

var webQualityScripts = map[string]string{
	"format:check": "prettier --check .",
	"format:write": "prettier --write .",
	"lint":         "eslint . --max-warnings=0",
	"typecheck":    "tsc --noEmit",
	"test":         "vitest run",
	"build":        "next build",
	"start":        "next start",
	"test:e2e":     "playwright test",
}

var webQualityDevDependencies = map[string]string{
	"@playwright/test":          "1.62.1",
	"@testing-library/dom":      "10.4.1",
	"@testing-library/jest-dom": "7.0.1",
	"@testing-library/react":    "16.3.2",
	"@vitejs/plugin-react":      "6.1.0",
	"eslint-config-prettier":    "10.1.8",
	"jsdom":                     "30.0.1",
	"prettier":                  "3.9.6",
	"vite-tsconfig-paths":       "6.1.1",
	"vitest":                    "4.1.11",
}

func webQualityFiles() map[string]string {
	return map[string]string{
		"eslint.config.mjs":                             webQualityESLintConfig,
		".prettierrc.json":                              webQualityPrettierConfig,
		".prettierignore":                               webQualityPrettierIgnore,
		"vitest.config.ts":                              webQualityVitestConfig,
		filepath.Join("test", "setup.ts"):               webQualityTestSetup,
		filepath.Join("test", "quality.smoke.test.tsx"): webQualitySmokeTest,
		"playwright.config.ts":                          webQualityPlaywrightConfig,
	}
}

func writeWebQualityFiles(directory string) error {
	if err := patchWebPackageJSON(filepath.Join(directory, "package.json")); err != nil {
		return err
	}
	for relative, contents := range webQualityFiles() {
		if err := writeFile(filepath.Join(directory, relative), contents); err != nil {
			return fmt.Errorf("write %s: %w", relative, err)
		}
	}
	return nil
}

func patchWebPackageJSON(path string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read package.json: %w", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(contents, &document); err != nil {
		return fmt.Errorf("decode package.json: %w", err)
	}

	scripts, err := packageStringMap(document, "scripts")
	if err != nil {
		return err
	}
	for name, command := range webQualityScripts {
		scripts[name] = command
	}
	if document["scripts"], err = json.Marshal(scripts); err != nil {
		return fmt.Errorf("encode scripts: %w", err)
	}

	devDependencies, err := packageStringMap(document, "devDependencies")
	if err != nil {
		return err
	}
	for name, version := range webQualityDevDependencies {
		devDependencies[name] = version
	}
	if document["devDependencies"], err = json.Marshal(devDependencies); err != nil {
		return fmt.Errorf("encode devDependencies: %w", err)
	}

	updated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode package.json: %w", err)
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return fmt.Errorf("write package.json: %w", err)
	}
	return nil
}

func packageStringMap(document map[string]json.RawMessage, field string) (map[string]string, error) {
	contents, ok := document[field]
	if !ok {
		return make(map[string]string), nil
	}
	values := make(map[string]string)
	if err := json.Unmarshal(contents, &values); err != nil {
		return nil, fmt.Errorf("decode package.json %s: %w", field, err)
	}
	return values, nil
}
