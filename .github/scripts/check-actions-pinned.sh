#!/usr/bin/env bash

set -euo pipefail

if ! command -v yq >/dev/null 2>&1; then
  echo "yq is required to check Actions references" >&2
  exit 1
fi

if (( $# == 0 )); then
  if [[ ! -d .github/workflows ]]; then
    echo "workflow directory not found: .github/workflows" >&2
    exit 1
  fi

  shopt -s nullglob globstar
  files=(.github/workflows/**/*.yml .github/workflows/**/*.yaml)
  if (( ${#files[@]} == 0 )); then
    echo "no workflow files found in .github/workflows" >&2
    exit 1
  fi
else
  files=("$@")
fi

failed=0
for file in "${files[@]}"; do
  if ! output=$(yq -r '.. | select(tag == "!!map" and has("uses")) | .uses' "$file"); then
    echo "failed to parse workflow: $file" >&2
    exit 1
  fi

  while IFS= read -r reference; do
    [[ -n "$reference" ]] || continue

    case "$reference" in
      ./* | docker://*)
        continue
        ;;
    esac

    if [[ "$reference" =~ @[0-9a-fA-F]{40}$ ]] ||
      [[ "$reference" =~ @v?[0-9]+(\.[0-9]+){0,2}$ ]]; then
      continue
    fi

    printf '%s: mutable external Actions reference: %s\n' "$file" "$reference" >&2
    failed=1
  done <<<"$output"
done

exit "$failed"
