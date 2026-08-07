#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -eq 0 ]]; then
  echo "Run this script as the desktop user, not as root or through sudo." >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project_dir=""
if [[ -x "$script_dir/bocker_gui" && -x "$script_dir/bocker" ]]; then
  bundle_dir="$script_dir"
else
  project_dir="$(cd "$script_dir/.." && pwd)"
  bundle_dir="$script_dir/build/linux/x64/release/bundle"
fi
app_id="io.bocker.bocker_gui"
install_dir="${BOCKER_GUI_INSTALL_DIR:-$HOME/.local/opt/bocker-gui}"
data_dir="${XDG_DATA_HOME:-$HOME/.local/share}"
applications_dir="$data_dir/applications"

if [[ ! -x "$bundle_dir/bocker_gui" || ! -x "$bundle_dir/bocker" ]]; then
  if [[ -z "$project_dir" ]]; then
    echo "GUI bundle is incomplete: $bundle_dir" >&2
    exit 1
  fi
  make -C "$project_dir" build-gui
fi

if [[ ! -x "$bundle_dir/bocker_gui" || ! -x "$bundle_dir/bocker" ]]; then
  echo "GUI bundle is incomplete: $bundle_dir" >&2
  exit 1
fi

desktop_dir=""
if command -v xdg-user-dir >/dev/null 2>&1; then
  desktop_dir="$(xdg-user-dir DESKTOP 2>/dev/null || true)"
fi
if [[ -z "$desktop_dir" ]]; then
  desktop_dir="$HOME/Desktop"
fi

write_desktop_file() {
  local target="$1"
  {
    printf '%s\n' '[Desktop Entry]'
    printf '%s\n' 'Version=1.0'
    printf '%s\n' 'Type=Application'
    printf '%s\n' 'Name=Bocker GUI'
    printf '%s\n' 'Comment=Manage Bocker containers'
    printf 'Exec=%s\n' "$install_dir/bocker_gui"
    printf 'TryExec=%s\n' "$install_dir/bocker_gui"
    printf 'Path=%s\n' "$install_dir"
    printf '%s\n' 'Icon=application-x-executable'
    printf '%s\n' 'Terminal=false'
    printf '%s\n' 'Categories=System;'
    printf '%s\n' 'Keywords=container;incus;bocker;'
    printf '%s\n' 'StartupNotify=true'
  } >"$target"
  chmod 0755 "$target"
}

install -d -m 0755 "$install_dir" "$applications_dir" "$desktop_dir"
cp -a "$bundle_dir/." "$install_dir/"

application_file="$applications_dir/$app_id.desktop"
desktop_file="$desktop_dir/Bocker GUI.desktop"
write_desktop_file "$application_file"
write_desktop_file "$desktop_file"

if command -v desktop-file-validate >/dev/null 2>&1; then
  desktop-file-validate "$application_file"
  desktop-file-validate "$desktop_file"
fi
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$applications_dir"
fi
if command -v gio >/dev/null 2>&1; then
  gio set "$desktop_file" metadata::trusted true 2>/dev/null || true
fi

printf 'Installed Bocker GUI to %s\n' "$install_dir"
printf 'Application menu entry: %s\n' "$application_file"
printf 'Desktop launcher: %s\n' "$desktop_file"
