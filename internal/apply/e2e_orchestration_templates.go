package apply

import "strings"

const e2eOrchestrationTaskfile = `version: '3'

vars:
  BROWSER: '{{default "chromium" .BROWSER}}'
  PLATFORM: '{{default "" .PLATFORM}}'
  DEVICE: '{{default "" .DEVICE}}'
  API_BASE_URL: '{{default "" .API_BASE_URL}}'
  WEB_SELECTED: __WEB_SELECTED__
  MOBILE_SELECTED: __MOBILE_SELECTED__

tasks:
  e2e:web:
    cmds:
      - |
          if [ "{{.WEB_SELECTED}}" != "true" ]; then
            echo "status=unavailable: Web is not selected in this workspace; select Web before running this lane." >&2
            exit 3
          fi
          sh e2e/web/run.sh --browser "{{.BROWSER}}"

  e2e:mobile:
    cmds:
      - |
          if [ "{{.MOBILE_SELECTED}}" != "true" ]; then
            echo "status=unavailable: Mobile is not selected in this workspace; select Mobile before running this lane." >&2
            exit 3
          fi
          if [ -z "{{.PLATFORM}}" ] || [ -z "{{.DEVICE}}" ]; then
            echo "status=unavailable: mobile requires PLATFORM=android|ios and DEVICE=<id>." >&2
            exit 3
          fi
          if [ -n "{{.API_BASE_URL}}" ]; then
            SMT_API_BASE_URL="{{.API_BASE_URL}}" sh e2e/mobile/run.sh --platform "{{.PLATFORM}}" --device "{{.DEVICE}}"
          else
            SMT_API_BASE_URL= sh e2e/mobile/run.sh --platform "{{.PLATFORM}}" --device "{{.DEVICE}}"
          fi

  e2e:verify:
    cmds:
      - |
          set +e
          report_dir=e2e/reports
          mkdir -p "$report_dir"
          log_file="$report_dir/verify.log"
          status_file="$report_dir/verify.status"
          : > "$log_file"
          web_status=0
          mobile_status=0

          if [ "{{.WEB_SELECTED}}" = "true" ]; then
            echo "lane=web" >> "$log_file"
            sh e2e/web/run.sh --browser "{{.BROWSER}}" >> "$log_file" 2>&1
            web_status=$?
            if [ "$web_status" -eq 0 ]; then
              echo "lane=web status=passed" >> "$log_file"
            elif [ "$web_status" -eq 3 ]; then
              echo "lane=web status=unavailable" >> "$log_file"
            else
              echo "lane=web status=failed exit_code=$web_status" >> "$log_file"
            fi
          fi

          if [ "{{.MOBILE_SELECTED}}" = "true" ]; then
            echo "lane=mobile" >> "$log_file"
            if [ -z "{{.PLATFORM}}" ] || [ -z "{{.DEVICE}}" ]; then
              echo "status=unavailable: mobile requires PLATFORM=android|ios and DEVICE=<id>." >> "$log_file"
              mobile_status=3
            elif [ -n "{{.API_BASE_URL}}" ]; then
              SMT_API_BASE_URL="{{.API_BASE_URL}}" sh e2e/mobile/run.sh --platform "{{.PLATFORM}}" --device "{{.DEVICE}}" >> "$log_file" 2>&1
              mobile_status=$?
            else
              SMT_API_BASE_URL= sh e2e/mobile/run.sh --platform "{{.PLATFORM}}" --device "{{.DEVICE}}" >> "$log_file" 2>&1
              mobile_status=$?
            fi
            if [ "$mobile_status" -eq 0 ]; then
              echo "lane=mobile status=passed" >> "$log_file"
            elif [ "$mobile_status" -eq 3 ]; then
              echo "lane=mobile status=unavailable" >> "$log_file"
            elif [ "$mobile_status" -ne 0 ]; then
              echo "lane=mobile status=failed exit_code=$mobile_status" >> "$log_file"
            fi
          fi

          aggregate_status=passed
          if [ "$web_status" -ne 0 ] || [ "$mobile_status" -ne 0 ]; then
            aggregate_status=unavailable
          fi
          if { [ "$web_status" -ne 0 ] && [ "$web_status" -ne 3 ]; } ||
             { [ "$mobile_status" -ne 0 ] && [ "$mobile_status" -ne 3 ]; }; then
            aggregate_status=failed
          fi
          printf 'status=%s\n' "$aggregate_status" > "$status_file"
          echo "aggregate status=$aggregate_status" >> "$log_file"
          cat "$log_file"
          case "$aggregate_status" in
            passed) exit 0 ;;
            unavailable) exit 3 ;;
            failed) exit 1 ;;
          esac
`

const e2eOrchestrationReadme = `# End-to-end verification

This root coordinator runs the selected Web and Mobile contract lanes. It is
generated only when the root blueprint declares the e2e module and selects at
least one of Web or Mobile. An e2e-only root remains metadata-only.
The task names are task e2e:web, task e2e:mobile, and task e2e:verify.

## Pinned setup

Use Task 3.52.0 and the versions pinned in .tool-versions. The Web lane uses
Node.js 24.18.0 and its setup is documented in e2e/web/README.md. The Mobile
lane uses Flutter 3.44.9-stable and its native setup is documented in
e2e/mobile/README.md. Resolve each child application's dependencies
manually before invoking a lane. Apply and this coordinator do not perform
setup or dependency resolution.

## Explicit lane selection

Web defaults to Chromium; select another supported browser explicitly:

~~~sh
task e2e:web
BROWSER=firefox task e2e:web
~~~

Mobile always requires both a platform and a device ID:

~~~sh
PLATFORM=android DEVICE=<device-id> task e2e:mobile
PLATFORM=ios DEVICE=<device-id> task e2e:mobile
~~~

The optional API value is caller-owned and is forwarded to the native Mobile
runner:

~~~sh
PLATFORM=android DEVICE=<device-id> API_BASE_URL=http://127.0.0.1:8080 task e2e:mobile
~~~

## Verify selected lanes

The e2e:verify task runs every selected target, keeps going after a failed or
unavailable lane, and returns a non-zero result when any selected lane is not
passed:

~~~sh
BROWSER=chromium PLATFORM=android DEVICE=<device-id> task e2e:verify
BROWSER=chromium PLATFORM=android DEVICE=<device-id> API_BASE_URL=http://127.0.0.1:8080 task e2e:verify
~~~

For a Web-only workspace, omit the Mobile variables. For a Mobile-only
workspace, the explicit Mobile variables are still required.

The Web and Mobile child reports remain under e2e/web/reports/ and
e2e/mobile/reports/. The coordinator writes the aggregate log and status to
e2e/reports/verify.log and e2e/reports/verify.status. Aggregate precedence is
failed, then unavailable, then passed.

Start any required local API or Compose services yourself before a lane and
provide API_BASE_URL when the Mobile contract needs it. This coordinator
does not start or stop application services, choose devices, install tools, or
contact external execution services. Mobile remains outside Compose.
`

const e2eOrchestrationGitignore = `reports/
`

func e2eOrchestrationFiles(webSelected, mobileSelected bool) map[string]string {
	webValue := "false"
	if webSelected {
		webValue = "true"
	}
	mobileValue := "false"
	if mobileSelected {
		mobileValue = "true"
	}
	taskfile := strings.ReplaceAll(e2eOrchestrationTaskfile, "__WEB_SELECTED__", webValue)
	taskfile = strings.ReplaceAll(taskfile, "__MOBILE_SELECTED__", mobileValue)
	return map[string]string{
		"Taskfile.yml":   taskfile,
		"e2e/README.md":  e2eOrchestrationReadme,
		"e2e/.gitignore": e2eOrchestrationGitignore,
	}
}
