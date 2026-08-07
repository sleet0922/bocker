#!/usr/bin/env bash
set -euo pipefail

gui_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(cd "$gui_dir/.." && pwd)"
bundle_dir="$gui_dir/build/linux/x64/release/bundle"

(cd "$project_dir" && make build-cli)
(cd "$gui_dir" && flutter build linux --release)
install -m 0755 "$project_dir/bocker" "$bundle_dir/bocker"
install -m 0755 "$gui_dir/install_desktop.sh" "$bundle_dir/install_desktop.sh"
install -m 0644 "$project_dir/logo.png" "$bundle_dir/logo.png"

echo "Built $bundle_dir/bocker_gui with matching bundled $bundle_dir/bocker, logo, and desktop installer"
