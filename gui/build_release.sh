#!/usr/bin/env bash
set -euo pipefail

gui_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(cd "$gui_dir/.." && pwd)"
bundle_dir="$gui_dir/build/linux/x64/release/bundle"

(cd "$project_dir" && make build)
(cd "$gui_dir" && flutter build linux --release)
if [[ -e "$bundle_dir/bocker" ]]; then
  unlink "$bundle_dir/bocker"
fi

echo "Built $bundle_dir/bocker_gui (uses /usr/local/bin/bocker)"
