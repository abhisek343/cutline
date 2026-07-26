#!/usr/bin/env bash
set -euo pipefail

limit="${CUTLINE_LINE_LIMIT:-14400}"
if command -v rg >/dev/null 2>&1; then
	files="$(rg --files -g '*.go' -g '*.sql' -g '*.yaml' -g '*.yml' -g '*.md' -g '*.json')"
else
	files="$(find . -type f \
		\( -name '*.go' -o -name '*.sql' -o -name '*.yaml' -o -name '*.yml' -o -name '*.md' -o -name '*.json' \) \
		-not -path './.git/*' -print | sed 's#^./##')"
fi
count="$(printf '%s\n' "$files" | xargs wc -l | tail -n 1 | awk '{print $1}')"
if (( count > limit )); then
	printf 'line budget exceeded: %s > %s\n' "$count" "$limit" >&2
	exit 1
fi
printf 'line budget: %s/%s\n' "$count" "$limit"
