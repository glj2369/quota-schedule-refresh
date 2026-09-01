#!/usr/bin/env bash
# Fail unless the release version is declared identically everywhere.
#
# A release ships one version string, but it is written down in three places
# and a mismatch produces a plugin the store will not upgrade cleanly. The tag
# is the source of truth; this script refuses to publish when anything disagrees.
#
# Usage: check-version.sh <version>      (version without the leading "v")
set -euo pipefail

VERSION="${1:?usage: check-version.sh <version-without-leading-v>}"

runtime_go="internal/runtime/runtime.go"
registry="registry.json"
readme="README.md"

# const pluginVersion = "x.y.z"
go_ver=$(sed -n 's/.*pluginVersion[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$runtime_go" | sed -n '1p')
# "version": "x.y.z"
reg_ver=$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$registry" | sed -n '1p')
# quota-schedule-refresh_x.y.z_linux_amd64.zip
readme_ver=$(sed -n 's/.*quota-schedule-refresh_\([^_]*\)_linux_amd64\.zip.*/\1/p' "$readme" | sed -n '1p')

printf 'expected version (from tag): %s\n\n' "$VERSION"
printf '  %-32s %-14s %s\n' FILE FOUND STATUS

fail=0
check() {
  local file="$1" found="$2" status
  if [ -z "$found" ]; then
    status="FAIL - no version found"
    fail=1
  elif [ "$found" != "$VERSION" ]; then
    status="FAIL - expected $VERSION"
    fail=1
  else
    status="ok"
  fi
  printf '  %-32s %-14s %s\n' "$file" "${found:-<none>}" "$status"
}

check "$runtime_go" "$go_ver"
check "$registry" "$reg_ver"
check "$readme" "$readme_ver"

if [ "$fail" -ne 0 ]; then
  cat <<EOF

Version declarations disagree with the tag.

Fix all three, commit, delete the bad tag and tag again:
  - $runtime_go   const pluginVersion = "$VERSION"
  - $registry     "version": "$VERSION"
  - $readme       quota-schedule-refresh_${VERSION}_linux_amd64.zip
EOF
  exit 1
fi

echo
echo "All version declarations agree."
