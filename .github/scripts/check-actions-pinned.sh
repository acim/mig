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

    case "$reference" in
      ectobit/*)
        if [[ "$reference" == *@main ]]; then
          continue
        fi

        printf '%s: first-party ectobit reference must use @main: %s\n' "$file" "$reference" >&2
        failed=1
        ;;
      *)
        if [[ "$reference" =~ @v[0-9]+$ ]]; then
          continue
        fi

        printf '%s: third-party reference must use a major version tag: %s\n' "$file" "$reference" >&2
        failed=1
        ;;
    esac
  done <<<"$output"
done

exit "$failed"
