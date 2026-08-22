package apply

const webE2EReadme = `# Web contract smoke

This root-attached package runs the generated Web application against the
stable home marker and the /healthz endpoint. It does not contain domain
flows or application fixtures.

## Prerequisites

Apply only writes this package. Install the pinned Web dependencies, build the
Web runtime, install the required Playwright browser, and then run the lane:

~~~sh
cd web-app
asdf exec npm ci
asdf exec npm run build
cd ../e2e/web
asdf exec npm install
asdf exec npx playwright install chromium
cd ../..
~~~

Firefox and WebKit are optional explicit lanes:

~~~sh
cd e2e/web
asdf exec npx playwright install firefox
asdf exec npx playwright install webkit
cd ../..
~~~

Apply and the runner do not install package, browser, or SDK dependencies, or
contact external services.

## Run a lane

Chromium is the default. Select another browser explicitly when its browser
binary has been installed:

~~~sh
sh e2e/web/run.sh
sh e2e/web/run.sh --browser firefox
sh e2e/web/run.sh --browser webkit
~~~

The smoke asserts the stable data-smt-web-smoke="home" marker and GET
/healthz with HTTP 200 and status ok. Missing dependencies, build output, or
browser binaries are recorded as unavailable and return a non-zero status;
test output is never discarded.

Reports, logs, and status files are written to e2e/web/reports/. The Web
runtime starts through web-app's existing production start script.
`

const webE2EEnvExample = `# Optional default browser; run.sh --browser overrides this value.
SMT_E2E_BROWSER=chromium
`

const webE2EGitignore = `node_modules/
reports/
.env
`

const webE2EPackageJSON = `{
  "name": "smt-web-e2e",
  "private": true,
  "scripts": {
    "test": "playwright test",
    "test:chromium": "SMT_E2E_BROWSER=chromium playwright test --project=chromium",
    "test:firefox": "SMT_E2E_BROWSER=firefox playwright test --project=firefox",
    "test:webkit": "SMT_E2E_BROWSER=webkit playwright test --project=webkit"
  },
  "devDependencies": {
    "@playwright/test": "1.62.1"
  }
}
`

const webE2EPlaywrightConfig = `import { defineConfig, devices } from "@playwright/test";

const baseURL = "http://127.0.0.1:3000";
const requestedBrowser = process.env.SMT_E2E_BROWSER ?? "chromium";
const browserProjects = [
  {
    name: "chromium",
    use: { ...devices["Desktop Chrome"] },
  },
  {
    name: "firefox",
    use: { ...devices["Desktop Firefox"] },
  },
  {
    name: "webkit",
    use: { ...devices["Desktop Safari"] },
  },
];
const selectedProject = browserProjects.find((project) => project.name === requestedBrowser);
if (!selectedProject) {
  throw new Error("SMT_E2E_BROWSER must be chromium, firefox, or webkit");
}
const reportDir = process.env.SMT_E2E_REPORT_DIR ?? "reports";

export default defineConfig({
  testDir: "./tests",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  reporter: [
    ["list"],
    ["html", { open: "never", outputFolder: reportDir + "/html" }],
  ],
  outputDir: reportDir + "/test-results",
  use: {
    baseURL,
    trace: "on-first-retry",
  },
  projects: [selectedProject],
  webServer: {
    command: "cd ../../web-app && asdf exec npm run start",
    reuseExistingServer: false,
    stderr: "pipe",
    stdout: "pipe",
    timeout: 120000,
    url: baseURL,
  },
});
`

const webE2ESmokeSpec = `import { expect, test } from "@playwright/test";

test("Web home marker and health contract remain stable", async ({ page, request }) => {
  await page.goto("/");
  await expect(page.locator('[data-smt-web-smoke="home"]')).toBeVisible();

  const health = await request.get("/healthz");
  await expect(health).toBeOK();
  expect(await health.json()).toEqual({ status: "ok" });
});
`

const webE2ERunSh = `#!/bin/sh
set -eu

usage() {
  echo "usage: sh e2e/web/run.sh [--browser chromium|firefox|webkit]" >&2
}

browser=${SMT_E2E_BROWSER:-chromium}
while [ "$#" -gt 0 ]; do
  case "$1" in
    --browser)
      if [ "$#" -lt 2 ]; then
        echo "--browser requires a value" >&2
        usage
        exit 2
      fi
      browser="$2"
      shift 2
      ;;
    --help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 2
      ;;
  esac
done

case "$browser" in
  chromium|firefox|webkit) ;;
  *)
    echo "--browser must be chromium, firefox, or webkit" >&2
    usage
    exit 2
    ;;
esac

script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
root_dir="$script_dir/../.."
web_dir="$root_dir/web-app"
report_dir="$script_dir/reports"
log_file="$script_dir/reports/$browser.log"
status_file="$script_dir/reports/$browser.status"
mkdir -p "$report_dir"
: > "$log_file"

write_unavailable() {
  reason="$1"
  printf 'status=unavailable\nbrowser=%s\nreason=%s\n' "$browser" "$reason" > "$status_file"
  printf 'reason=%s\n' "$reason" >> "$log_file"
  printf 'Web %s lane unavailable: %s; see %s\n' "$browser" "$reason" "$log_file" >&2
  exit 3
}

if ! command -v asdf >/dev/null 2>&1; then
  write_unavailable "asdf is unavailable; install Node.js 24.18.0 with asdf"
fi

if [ ! -f "$web_dir/package.json" ] || [ ! -f "$web_dir/package-lock.json" ] || \
   [ ! -x "$web_dir/node_modules/.bin/next" ] || [ ! -f "$web_dir/.next/BUILD_ID" ]; then
  write_unavailable "Web runtime is not installed and built; run (cd web-app && asdf exec npm ci && asdf exec npm run build)"
fi

if [ ! -f "$script_dir/package.json" ] || [ ! -d "$script_dir/node_modules/@playwright/test" ]; then
  write_unavailable "E2E dependencies are missing; run (cd e2e/web && asdf exec npm install)"
fi

if ! (cd "$script_dir" && asdf exec npm --version) >> "$log_file" 2>&1; then
  write_unavailable "npm is unavailable through asdf; install Node.js 24.18.0 with asdf"
fi

browser_path=
if ! browser_path=$(cd "$script_dir" && asdf exec node -e '
const browser = process.argv[1];
const playwright = require("@playwright/test");
const browsers = {
  chromium: playwright.chromium,
  firefox: playwright.firefox,
  webkit: playwright.webkit,
};
if (!browsers[browser]) process.exit(2);
process.stdout.write(browsers[browser].executablePath());
' "$browser" 2>> "$log_file"); then
  write_unavailable "Playwright browser support is unavailable; run (cd e2e/web && asdf exec npm install)"
fi
if [ ! -x "$browser_path" ]; then
  write_unavailable "browser $browser is not installed; run (cd e2e/web && asdf exec npx playwright install $browser)"
fi

printf '$ SMT_E2E_BROWSER=%s asdf exec npm test -- --project=%s\n' "$browser" "$browser" >> "$log_file"
if (cd "$script_dir" && SMT_E2E_BROWSER="$browser" asdf exec npm test -- --project="$browser") >> "$log_file" 2>&1; then
  printf 'status=passed\nbrowser=%s\n' "$browser" > "$status_file"
  printf 'Web %s contract smoke passed; report: %s\n' "$browser" "$log_file"
  exit 0
else
  test_status=$?
fi

if grep -Eiq 'Executable doesn.t exist|browserType.launch|playwright install|Cannot find module|webServer|EADDRINUSE|ECONNREFUSED' "$log_file"; then
  write_unavailable "Web runtime or browser startup failed; see $log_file"
fi

printf 'status=failed\nbrowser=%s\nexit_code=%s\n' "$browser" "$test_status" > "$status_file"
printf 'Web %s contract smoke failed; see %s\n' "$browser" "$log_file" >&2
exit "$test_status"
`

func webE2EFiles() map[string]string {
	return map[string]string{
		"README.md":                    webE2EReadme,
		".env.example":                 webE2EEnvExample,
		".gitignore":                   webE2EGitignore,
		"package.json":                 webE2EPackageJSON,
		"playwright.config.ts":         webE2EPlaywrightConfig,
		"run.sh":                       webE2ERunSh,
		"tests/contract.smoke.spec.ts": webE2ESmokeSpec,
	}
}
