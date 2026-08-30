#!/usr/bin/env bash
set -euo pipefail

project_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
bocker_bin=${BOCKER_BIN:-bocker}

languages=(go node python)
containers=()
images=()
cleanup() {
	for container in "${containers[@]}"; do
		"$bocker_bin" container remove "$container" </dev/null >/dev/null 2>&1 || true
	done
	for image in "${images[@]}"; do
		"$bocker_bin" image remove "$image" </dev/null >/dev/null 2>&1 || true
	done
	find "$project_dir" -type f \( -name check.txt -o -name result.txt \) -delete
}
trap cleanup EXIT

for language in "${languages[@]}"; do
	image="bocker-language-${language}-mount-check"
	container="${image}-container"
	images+=("$image")
	containers+=("$container")
	"$bocker_bin" image build --name "$image" "$project_dir/$language/Incusfile"
	"$bocker_bin" image run "$image" --name "$container"
done

"$bocker_bin" container exec "${containers[0]}" sh -c 'test "$(cat /etc/go-mount-message)" = "hello from go mount" && echo go-writable > /var/lib/go-mount-app/check.txt'
"$bocker_bin" container exec "${containers[0]}" test -f /var/lib/go-mount-app/check.txt
"$bocker_bin" container exec "${containers[1]}" sh -c 'test "$(cat /etc/node-mount-message)" = "hello from node mount" && echo node-writable > /var/lib/node-mount-app/check.txt'
"$bocker_bin" container exec "${containers[1]}" test -f /var/lib/node-mount-app/check.txt
"$bocker_bin" container exec "${containers[2]}" sh -c 'test "$(cat /etc/python-mount-message)" = "hello from python mount" && echo python-writable > /var/lib/python-mount-app/check.txt'
"$bocker_bin" container exec "${containers[2]}" test -f /var/lib/python-mount-app/check.txt

echo "Language Incusfile runtime.mounts checks passed"
