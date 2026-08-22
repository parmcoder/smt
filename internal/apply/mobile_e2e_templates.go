package apply

const mobileE2EReadme = `# Mobile contract smoke

This root-attached package runs the native Flutter integration contract in
mobile-app/integration_test/app_test.dart. The smoke test checks the stable
mobile-home and api-status hooks and does not require an API to launch.

## Prerequisites

Install the Flutter version pinned by the workspace with asdf, then resolve
the Mobile app dependencies explicitly:

~~~sh
asdf install flutter 3.44.9-stable
cd mobile-app
asdf exec flutter pub get
asdf exec flutter devices
cd ..
~~~

Apply does not install or resolve anything. Mobile is a native target and
remains outside Compose.

## Run one explicit lane

Always provide both the platform label and the device ID. The runner does not
choose a device or fall back to another platform:

~~~sh
sh e2e/mobile/run.sh --platform android --device <device-id>
sh e2e/mobile/run.sh --platform ios --device <device-id>
~~~

The optional API base is passed to Flutter as a compile-time define:

~~~sh
SMT_API_BASE_URL=http://127.0.0.1:8080 \
  sh e2e/mobile/run.sh --platform android --device <device-id>
~~~

If the SDK, platform tools, simulator/emulator, or requested device is
unavailable, the lane is recorded as unavailable and exits non-zero. It is
never silently skipped. Test output and status files remain under
e2e/mobile/reports/.
`

const mobileE2EEnvExample = `# Optional API base; run.sh forwards this as --dart-define.
SMT_API_BASE_URL=
`

const mobileE2EGitignore = `reports/
.env
`

const mobileE2ERunSh = `#!/bin/sh
set -eu

usage() {
  echo "usage: sh e2e/mobile/run.sh --platform android|ios --device <device-id>" >&2
}

platform=
device=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --platform)
      if [ "$#" -lt 2 ]; then
        echo "--platform requires a value" >&2
        usage
        exit 2
      fi
      platform="$2"
      shift 2
      ;;
    --device)
      if [ "$#" -lt 2 ]; then
        echo "--device requires a value" >&2
        usage
        exit 2
      fi
      device="$2"
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

if [ -z "$platform" ] || [ -z "$device" ]; then
  echo "--platform and --device are required" >&2
  usage
  exit 2
fi

case "$platform" in
  android|ios) ;;
  *)
    echo "--platform must be android or ios" >&2
    usage
    exit 2
    ;;
esac

script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
mobile_dir="$script_dir/../../mobile-app"
report_dir="$script_dir/reports/"
log_file="$report_dir/$platform.log"
status_file="$report_dir/$platform.status"
mkdir -p "$report_dir"
: > "$log_file"

write_unavailable() {
  reason="$1"
  printf 'status=unavailable\nplatform=%s\ndevice=%s\nreason=%s\n' "$platform" "$device" "$reason" > "$status_file"
  printf 'mobile %s lane unavailable: %s; see %s\n' "$platform" "$reason" "$log_file" >&2
  exit 3
}

if ! command -v asdf >/dev/null 2>&1; then
  write_unavailable "asdf is unavailable; install Flutter 3.44.9-stable with asdf"
fi

if [ ! -f "$mobile_dir/integration_test/app_test.dart" ]; then
  write_unavailable "mobile-app/integration_test/app_test.dart is missing"
fi

if ! (cd "$mobile_dir" && asdf exec flutter devices) > "$log_file" 2>&1; then
  write_unavailable "Flutter devices failed; install Flutter 3.44.9-stable and the required platform SDK"
fi

if ! grep -F "$device" "$log_file" >/dev/null 2>&1; then
  write_unavailable "device $device was not listed by asdf exec flutter devices"
fi

printf '\n$ asdf exec flutter test integration_test/app_test.dart -d %s\n' "$device" >> "$log_file"
if [ -n "${SMT_API_BASE_URL:-}" ]; then
  if (cd "$mobile_dir" && asdf exec flutter test integration_test/app_test.dart -d "$device" "--dart-define=SMT_API_BASE_URL=$SMT_API_BASE_URL") >> "$log_file" 2>&1; then
    test_status=0
  else
    test_status=$?
  fi
else
  if (cd "$mobile_dir" && asdf exec flutter test integration_test/app_test.dart -d "$device") >> "$log_file" 2>&1; then
    test_status=0
  else
    test_status=$?
  fi
fi

if [ "$test_status" -ne 0 ]; then
  printf 'status=failed\nplatform=%s\ndevice=%s\nexit_code=%s\n' "$platform" "$device" "$test_status" > "$status_file"
  printf 'mobile %s contract smoke failed; see %s\n' "$platform" "$log_file" >&2
  exit "$test_status"
fi

printf 'status=passed\nplatform=%s\ndevice=%s\n' "$platform" "$device" > "$status_file"
printf 'mobile %s contract smoke passed; report: %s\n' "$platform" "$log_file"
`

func mobileE2EFiles() map[string]string {
	return map[string]string{
		"README.md":    mobileE2EReadme,
		".env.example": mobileE2EEnvExample,
		".gitignore":   mobileE2EGitignore,
		"run.sh":       mobileE2ERunSh,
	}
}
