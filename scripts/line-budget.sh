#!/usr/bin/env bash
set -euo pipefail

limit="${CUTLINE_LINE_LIMIT:-14400}"
count="$(rg --files -g '*.go' -g '*.sql' -g '*.yaml' -g '*.yml' -g '*.md' -g '*.json' | xargs wc -l | tail -n 1 | awk '{print $1}')"
if (( count > limit )); then
	printf 'line budget exceeded: %s > %s\n' "$count" "$limit" >&2
	exit 1
fi
printf 'line budget: %s/%s\n' "$count" "$limit"
