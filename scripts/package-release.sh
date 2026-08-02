#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 || -z "$1" ]]; then
  echo "usage: $0 VERSION" >&2
  exit 2
fi

version="$1"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out_dir="${OUT_DIR:-"$root/dist"}"

if [[ -e "$out_dir" ]]; then
  echo "refusing to overwrite existing output directory: $out_dir" >&2
  echo "set OUT_DIR to a new empty path" >&2
  exit 2
fi

mkdir -p "$out_dir"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

commit="$(git -C "$root" rev-parse --verify HEAD 2>/dev/null || printf unknown)"
go_version="$(go version)"

for arch in amd64 arm64; do
  package_root="$work_dir/cutline_${version}_linux_${arch}"
  mkdir -p "$package_root"

  (
    cd "$root"
    GOOS=linux GOARCH="$arch" CGO_ENABLED=0 \
      go build -trimpath -o "$package_root/cutline" ./cmd/cutline
  )

  cp "$root/README.md" "$root/LICENSE" "$package_root/"
  printf 'version=%s\ncommit=%s\ngo=%s\n' "$version" "$commit" "$go_version" \
    >"$package_root/BUILDINFO"

  archive="$out_dir/cutline_${version}_linux_${arch}.tar.gz"
  tar -C "$work_dir" -czf "$archive" "$(basename "$package_root")"
done

(
  cd "$out_dir"
  sha256sum ./*.tar.gz >checksums.txt
)
