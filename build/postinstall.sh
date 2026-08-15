#!/bin/sh
set -u

# Initialize the root-owned runtime during package installation so users can
# run Bocker immediately after apt/dpkg completes.
binary=/usr/bin/bocker
if [ ! -x "$binary" ] && [ -x /usr/lib/bocker-gui/bocker ]; then
    binary=/usr/lib/bocker-gui/bocker
fi
if [ "$(id -u)" -eq 0 ] && [ -x "$binary" ]; then
    "$binary" container list --json >/dev/null 2>&1 || true
fi
