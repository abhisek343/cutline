#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 ARCHIVE_DIRECTORY VERSION" >&2
  exit 2
fi
archives="$(cd "$1" && pwd)"
version="$2"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
(cd "$archives" && sha256sum --check checksums.txt)
tar -xzf "$archives/cutline_${version}_linux_amd64.tar.gz" -C "$work"
binary="$work/cutline_${version}_linux_amd64/cutline"
test -x "$binary"
test -s "$work/cutline_${version}_linux_amd64/BUILDINFO"
reported_version="$("$binary" --version)"
test "$reported_version" = "cutline version $version"
grep -Fx "version=$version" "$work/cutline_${version}_linux_amd64/BUILDINFO"
printf '%s\n' "$reported_version"
cd "$root"
"$binary" run --campaign test/fixtures/checkout/campaign-clean.yaml --json > "$work/clean.json"
set +e
"$binary" run --campaign test/fixtures/checkout/campaign-faulty.yaml --json > "$work/faulty.json"
status=$?
set -e
test "$status" -eq 2
python3 - "$work/clean.json" "$work/faulty.json" <<'PY'
import json, sys
clean, faulty = (json.load(open(path)) for path in sys.argv[1:])
assert clean["status"] == "pass", clean
assert faulty["status"] == "violation", faulty
assert any(e["contract"] == "no-charge-after-cancel" and e["status"] == "violation"
           for s in faulty["schedules"] for e in s["evaluations"]), faulty
PY
echo "Unpacked Linux CLI: clean pass and named faulty violation verified"
