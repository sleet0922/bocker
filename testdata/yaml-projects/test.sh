#!/usr/bin/env bash
set -euo pipefail

project_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
bocker_bin=${BOCKER_BIN:-bocker}

hello_image=bocker-yaml-e2e-hello
multi_image=bocker-yaml-e2e-multi
runtime_image=bocker-yaml-e2e-runtime
hello_container=${hello_image}-check
multi_container=${multi_image}-check
runtime_container=${runtime_image}-check

cleanup() {
	for container in "$hello_container" "$multi_container" "$runtime_container"; do
		"$bocker_bin" container remove "$container" </dev/null >/dev/null 2>&1 || true
	done
	for image in "$hello_image" "$multi_image" "$runtime_image"; do
		"$bocker_bin" image remove "$image" </dev/null >/dev/null 2>&1 || true
	done
}
trap cleanup EXIT

cleanup

"$bocker_bin" image build --name "$hello_image" "$project_dir/hello/Incusfile.yaml"
"$bocker_bin" image run "$hello_image" --name "$hello_container"
test "$("$bocker_bin" container exec "$hello_container" cat /hello.txt)" = "hello from yaml"

"$bocker_bin" image build --name "$multi_image" "$project_dir/multi-stage/Incusfile.yaml"
"$bocker_bin" image run "$multi_image" --name "$multi_container"
test "$("$bocker_bin" container exec "$multi_container" cat /result.txt)" = "multi-stage yaml"

"$bocker_bin" image build --name "$runtime_image" "$project_dir/runtime/Incusfile.yaml"
"$bocker_bin" image run "$runtime_image" --name "$runtime_container"
"$bocker_bin" container exec "$runtime_container" sh -c \
	'test -d /opt/yaml-runtime/logs && test -x /etc/init.d/bocker-entrypoint && grep -Fx '\''APP_ENV="production"'\'' /etc/environment'

echo "YAML Incusfile end-to-end tests passed"
