#!/usr/bin/env bash
# Fail if the built c-shared library needs a newer glibc than CPA can provide.
#
# The plugin is dlopen'ed by the CLIProxyAPI process. If the build environment
# has a newer glibc than the runtime, the symbols get versioned upward and the
# load fails at runtime with "version GLIBC_2.xx not found" -- which looks like
# a broken plugin, not a broken build. So we assert the ceiling in CI instead of
# discovering it in production.
#
# The cli-proxy-api image (eceasy/cli-proxy-api:latest) is Debian 12 "bookworm"
# with glibc 2.36. The known-good published 0.7.10 artifact requires at most
# GLIBC_2.34, so a correct build should land at or below that.
#
# Usage: check-abi.sh <file.so> [max-glibc]     default max is 2.36
set -euo pipefail

SO="${1:?usage: check-abi.sh <file.so> [max-glibc]}"
MAX="${2:-2.36}"
EXPECTED_TYPICAL="2.34"

if [ ! -f "$SO" ]; then
  echo "FAIL: $SO does not exist"
  exit 1
fi

echo "== object =="
readelf -h "$SO" | sed -n 's/^  \(Type\|Machine\|Class\):/\1:/p'
echo

echo "== shared libraries required (DT_NEEDED) =="
readelf -d "$SO" | grep NEEDED || echo "  (none)"
echo

versions=$(readelf -V "$SO" | grep -o 'GLIBC_[0-9][0-9.]*' | sed 's/GLIBC_//' | sort -u -V)
if [ -z "$versions" ]; then
  echo "FAIL: no GLIBC version requirements found; is $SO really a glibc-linked ELF?"
  exit 1
fi
highest=$(printf '%s\n' "$versions" | tail -1)

echo "== glibc symbol versioning =="
printf '  required          : %s\n' "$(printf '%s ' $versions)"
printf '  highest required  : GLIBC_%s\n' "$highest"
printf '  ceiling enforced  : GLIBC_%s\n' "$MAX"
printf '  reference         : CPA runtime has glibc 2.36; published 0.7.10 needed GLIBC_%s\n' \
  "$EXPECTED_TYPICAL"
echo

# "highest" must not sort after "MAX"
if [ "$(printf '%s\n%s\n' "$highest" "$MAX" | sort -V | tail -1)" != "$MAX" ]; then
  cat <<EOF
FAIL: this build requires GLIBC_$highest, above the enforced ceiling GLIBC_$MAX.
Loading it in the CPA process would fail with "version GLIBC_$highest not found".
Build inside an image whose glibc is <= $MAX; golang:1.25-bookworm (glibc 2.36)
matches the eceasy/cli-proxy-api runtime and is what the workflows use.
EOF
  exit 1
fi

if [ "$highest" != "$EXPECTED_TYPICAL" ]; then
  echo "NOTE: highest requirement is GLIBC_$highest, while 0.7.10 needed GLIBC_$EXPECTED_TYPICAL."
  echo "      Still within the runtime's GLIBC_$MAX, so this passes, but the toolchain moved."
fi

echo "ABI check passed: requires at most GLIBC_$highest, loadable by a glibc $MAX runtime."
