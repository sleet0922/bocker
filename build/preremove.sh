#!/bin/sh
set -eu

# Keep /var/lib/bocker intact on package removal, but stop the service before
# its executable is replaced or removed. A purge can then remove the package
# without leaving a running binary from the old version.
if command -v systemctl >/dev/null 2>&1; then
    systemctl disable --now bocker.service >/dev/null 2>&1 || true
fi
